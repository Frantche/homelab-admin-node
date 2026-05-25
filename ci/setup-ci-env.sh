#!/usr/bin/env bash
set -euo pipefail

# Set up the CI environment with required directory structures.
# This deploys compose stacks and env files as Ansible would on a real node,
# then starts all services with Docker Compose.
# Designed to run inside an Arch Linux VM or any Docker-capable host.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Create /srv/admin directory structure
mkdir -p /srv/admin/stacks /srv/admin/env /srv/admin/data /srv/admin/backups/local
mkdir -p /srv/admin/data/keycloak/postgres
mkdir -p /srv/admin/data/openbao
mkdir -p /srv/admin/data/harbor/postgres /srv/admin/data/harbor/registry /srv/admin/data/harbor/core

# Deploy compose stacks to the expected runtime location
cp -a "$REPO_ROOT/stacks/traefik" /srv/admin/stacks/traefik
cp -a "$REPO_ROOT/stacks/keycloak" /srv/admin/stacks/keycloak
cp -a "$REPO_ROOT/stacks/openbao" /srv/admin/stacks/openbao
cp -a "$REPO_ROOT/stacks/harbor" /srv/admin/stacks/harbor
cp -a "$REPO_ROOT/stacks/cloudflared" /srv/admin/stacks/cloudflared

# Create env files with real credentials for CI
cat > /srv/admin/env/keycloak.env <<'EOF'
KEYCLOAK_DB_PASSWORD=ci-keycloak-db-pass
KEYCLOAK_ADMIN=admin
KEYCLOAK_ADMIN_PASSWORD=ci-keycloak-admin-pass
EOF
cat > /srv/admin/env/harbor.env <<'EOF'
HARBOR_ADMIN_PASSWORD=Harbor12345
HARBOR_DB_PASSWORD=harbor-ci-db
HARBOR_CORE_SECRET=harbor-ci-core-secret
HARBOR_JOBSERVICE_SECRET=harbor-ci-job-secret
HARBOR_REGISTRY_PASSWORD=harbor-ci-registry
EOF
cat > /srv/admin/env/cloudflared.env <<'EOF'
CLOUDFLARE_TUNNEL_TOKEN=eyJhIjoiZmFrZSIsInQiOiJmYWtlIiwicyI6ImZha2UifQ==
EOF
cat > /srv/admin/env/traefik.env <<'EOF'
TRAEFIK_DASHBOARD_BASIC_AUTH=admin:$apr1$xyz$fakehashforci
EOF
chmod 0600 /srv/admin/env/*.env

# Create /etc/admin-node directory
mkdir -p /etc/admin-node

echo "[ci-setup] Environment prepared, starting services..."

# Create shared Docker network
docker network create admin-net 2>/dev/null || true

# Start all stacks
docker compose --env-file /srv/admin/env/traefik.env -f /srv/admin/stacks/traefik/compose.yaml up -d
docker compose --env-file /srv/admin/env/keycloak.env -f /srv/admin/stacks/keycloak/compose.yaml up -d
docker compose -f /srv/admin/stacks/openbao/compose.yaml up -d
docker compose --env-file /srv/admin/env/harbor.env -f /srv/admin/stacks/harbor/compose.yaml up -d

# Only start cloudflared if not mocking
if [[ "${CI_MOCK_CLOUDFLARE_TUNNEL:-false}" != "true" ]]; then
  docker compose --env-file /srv/admin/env/cloudflared.env -f /srv/admin/stacks/cloudflared/compose.yaml up -d
else
  echo "[ci-setup] Skipping cloudflared (CI_MOCK_CLOUDFLARE_TUNNEL=true)"
  # Create a dummy container so validate-cloudflare-tunnel.sh can find it
  docker run -d --name cloudflared --network admin-net --restart no alpine sleep 3600 2>/dev/null || true
fi

echo "[ci-setup] Waiting for services to become healthy..."

# Wait for OpenBao API to be reachable (it will be uninitialized at this point - that's OK)
echo "[ci-setup] Waiting for OpenBao API..."
for i in $(seq 1 60); do
  http_code="$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8200/v1/sys/health 2>/dev/null || echo "000")"
  if [[ "$http_code" != "000" ]]; then
    echo "[ci-setup] OpenBao API is responding (HTTP $http_code)"
    break
  fi
  if [[ $i -eq 60 ]]; then
    echo "[ci-setup] ERROR: OpenBao did not become reachable" >&2
    docker logs openbao 2>&1 | tail -20
    exit 1
  fi
  sleep 2
done

# Wait for Keycloak
echo "[ci-setup] Waiting for Keycloak..."
for i in $(seq 1 120); do
  if curl -fsS http://127.0.0.1:9000/health/ready 2>/dev/null; then
    echo "[ci-setup] Keycloak is ready"
    break
  fi
  if [[ $i -eq 120 ]]; then
    echo "[ci-setup] ERROR: Keycloak did not become ready" >&2
    docker compose --env-file /srv/admin/env/keycloak.env -f /srv/admin/stacks/keycloak/compose.yaml logs 2>&1 | tail -30
    exit 1
  fi
  sleep 3
done

# Wait for Traefik
echo "[ci-setup] Waiting for Traefik..."
for i in $(seq 1 30); do
  routers="$(curl -s http://127.0.0.1:8080/api/http/routers 2>/dev/null || echo "")"
  if echo "$routers" | grep -q "keycloak" && echo "$routers" | grep -q "harbor" && echo "$routers" | grep -q "openbao"; then
    echo "[ci-setup] Traefik is ready"
    break
  fi
  if [[ $i -eq 30 ]]; then
    echo "[ci-setup] ERROR: Traefik did not load all routes" >&2
    echo "[ci-setup] Current routers:" >&2
    curl -s http://127.0.0.1:8080/api/http/routers 2>/dev/null || true
    echo "[ci-setup] Traefik logs:" >&2
    docker logs traefik 2>&1 | tail -30
    exit 1
  fi
  sleep 2
done

# Wait for Harbor core (minimum viable: core + database)
echo "[ci-setup] Waiting for Harbor..."
for i in $(seq 1 120); do
  health="$(curl -s http://127.0.0.1:8082/api/v2.0/health 2>/dev/null || echo "")"
  # Check that at least core and database are healthy
  core_ok="$(echo "$health" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(any(c["name"]=="core" and c["status"]=="healthy" for c in d.get("components",[])))' 2>/dev/null || echo "False")"
  db_ok="$(echo "$health" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(any(c["name"]=="database" and c["status"]=="healthy" for c in d.get("components",[])))' 2>/dev/null || echo "False")"
  if [[ "$core_ok" == "True" && "$db_ok" == "True" ]]; then
    echo "[ci-setup] Harbor core is ready"
    break
  fi
  if [[ $i -eq 120 ]]; then
    echo "[ci-setup] WARNING: Harbor did not become fully ready (non-fatal)" >&2
    docker compose --env-file /srv/admin/env/harbor.env -f /srv/admin/stacks/harbor/compose.yaml logs 2>&1 | tail -30
  fi
  sleep 3
done

echo "[ci-setup] All services started"
