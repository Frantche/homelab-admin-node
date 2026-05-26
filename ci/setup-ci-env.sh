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
mkdir -p /srv/admin/data/traefik/letsencrypt
mkdir -p /srv/admin/certs

# --- Generate self-signed CA and wildcard cert for *.example.com (CI only) ---
echo "[ci-setup] Generating self-signed TLS certificate..."
_cert_dir="$(mktemp -d)"
trap 'rm -rf "$_cert_dir"' EXIT

openssl genrsa -out "$_cert_dir/ca.key" 4096 2>/dev/null
openssl req -new -x509 -days 3650 -key "$_cert_dir/ca.key" -out "$_cert_dir/ca.crt" \
  -subj "/CN=Admin Node CI CA" 2>/dev/null

openssl genrsa -out "$_cert_dir/server.key" 2048 2>/dev/null
openssl req -new -key "$_cert_dir/server.key" -out "$_cert_dir/server.csr" \
  -subj "/CN=*.example.com" 2>/dev/null

cat > "$_cert_dir/san.ext" <<'EOT'
[SAN]
subjectAltName=DNS:*.example.com,DNS:localhost,IP:127.0.0.1
EOT

openssl x509 -req -days 3650 \
  -in "$_cert_dir/server.csr" \
  -CA "$_cert_dir/ca.crt" -CAkey "$_cert_dir/ca.key" -CAcreateserial \
  -out "$_cert_dir/server.crt" \
  -extfile "$_cert_dir/san.ext" -extensions SAN 2>/dev/null

cp "$_cert_dir/server.crt" /srv/admin/certs/cert.pem
cp "$_cert_dir/server.key" /srv/admin/certs/key.pem
chmod 0600 /srv/admin/certs/key.pem

# Add CA to the system trust store (Arch Linux)
if command -v update-ca-trust &>/dev/null; then
  cp "$_cert_dir/ca.crt" /etc/ca-certificates/trust-source/anchors/ci-admin-ca.crt
  update-ca-trust
elif command -v update-ca-certificates &>/dev/null; then
  cp "$_cert_dir/ca.crt" /usr/local/share/ca-certificates/ci-admin-ca.crt
  update-ca-certificates
fi

echo "[ci-setup] TLS certificate generated and CA trusted"

# --- Add /etc/hosts entries for service domains ---
for domain in keycloak.example.com bao.example.com harbor.example.com traefik.example.com; do
  if ! grep -qF "$domain" /etc/hosts; then
    echo "127.0.0.1 $domain" >> /etc/hosts
  fi
done

# Deploy compose stacks to the expected runtime location
cp -a "$REPO_ROOT/stacks/traefik" /srv/admin/stacks/traefik
cp -a "$REPO_ROOT/stacks/keycloak" /srv/admin/stacks/keycloak
cp -a "$REPO_ROOT/stacks/openbao" /srv/admin/stacks/openbao
cp -a "$REPO_ROOT/stacks/harbor" /srv/admin/stacks/harbor
cp -a "$REPO_ROOT/stacks/cloudflared" /srv/admin/stacks/cloudflared

# In CI, use the CI-specific Traefik config (no ACME Let's Encrypt)
cp "$REPO_ROOT/stacks/traefik/traefik-ci.yml" /srv/admin/stacks/traefik/traefik.yml

# Create CI TLS dynamic config (tells Traefik to use the self-signed cert as default)
cat > /srv/admin/stacks/traefik/dynamic/tls-ci.yml <<'EOF'
tls:
  stores:
    default:
      defaultCertificate:
        certFile: /certs/cert.pem
        keyFile: /certs/key.pem
EOF

# Create env files with real credentials for CI
cat > /srv/admin/env/keycloak.env <<'EOF'
KEYCLOAK_DB_PASSWORD=ci-keycloak-db-pass
KEYCLOAK_ADMIN=admin
KEYCLOAK_ADMIN_PASSWORD=ci-keycloak-admin-pass
EOF
cat > /srv/admin/env/harbor.env <<'EOF'
HARBOR_ADMIN_PASSWORD=ci-Harbor-admin-p4ss
HARBOR_DB_PASSWORD=ci-harbor-db-pass
HARBOR_CORE_SECRET=ci-harbor-core-secret
HARBOR_JOBSERVICE_SECRET=ci-harbor-job-secret
HARBOR_REGISTRY_PASSWORD=ci-harbor-registry
EOF
cat > /srv/admin/env/cloudflared.env <<'EOF'
CLOUDFLARE_TUNNEL_TOKEN=eyJhIjoiZmFrZSIsInQiOiJmYWtlIiwicyI6ImZha2UifQ==
EOF
# Generate a random htpasswd entry for the Traefik dashboard in CI
_dash_pass="$(openssl rand -hex 16)"
_dash_hash="$(openssl passwd -apr1 "$_dash_pass")"
cat > /srv/admin/env/traefik.env <<EOF
TRAEFIK_DASHBOARD_BASIC_AUTH=admin:${_dash_hash}
CF_DNS_API_TOKEN=ci-not-used
EOF
chmod 0600 /srv/admin/env/*.env

# Render the Traefik dynamic config template (substitute TRAEFIK_DASHBOARD_BASIC_AUTH)
python3 -c "
import sys
content = open(sys.argv[1]).read()
content = content.replace('{{ TRAEFIK_DASHBOARD_BASIC_AUTH }}', 'admin:' + sys.argv[2])
open(sys.argv[1], 'w').write(content)
" /srv/admin/stacks/traefik/dynamic/config.yml "$_dash_hash"

# Create /etc/admin-node directory
mkdir -p /etc/admin-node

echo "[ci-setup] Environment prepared, starting services..."

# Create shared Docker network
docker network create admin-net 2>/dev/null || true

# Start all stacks
docker compose --env-file /srv/admin/env/traefik.env -f /srv/admin/stacks/traefik/compose.yaml up -d --force-recreate
docker compose --env-file /srv/admin/env/keycloak.env -f /srv/admin/stacks/keycloak/compose.yaml up -d --force-recreate
docker compose -f /srv/admin/stacks/openbao/compose.yaml up -d --force-recreate
docker compose --env-file /srv/admin/env/harbor.env -f /srv/admin/stacks/harbor/compose.yaml up -d --force-recreate

# Only start cloudflared if not mocking
if [[ "${CI_MOCK_CLOUDFLARE_TUNNEL:-false}" != "true" ]]; then
  docker compose --env-file /srv/admin/env/cloudflared.env -f /srv/admin/stacks/cloudflared/compose.yaml up -d --force-recreate
else
  echo "[ci-setup] Skipping cloudflared (CI_MOCK_CLOUDFLARE_TUNNEL=true)"
  # Create a dummy container so validate-cloudflare-tunnel.sh can find it
  docker rm -f cloudflared 2>/dev/null || true
  docker run -d --name cloudflared --network admin-net --restart no alpine sleep 3600
fi

echo "[ci-setup] Waiting for services to become healthy..."

# Wait for OpenBao API (check via docker exec – no host port exposed)
echo "[ci-setup] Waiting for OpenBao API..."
for i in $(seq 1 60); do
  if docker exec openbao bao status 2>&1 | grep -q "Initialized"; then
    echo "[ci-setup] OpenBao API is responding"
    break
  fi
  if [[ $i -eq 60 ]]; then
    echo "[ci-setup] ERROR: OpenBao did not become reachable" >&2
    docker logs openbao 2>&1 | tail -20
    exit 1
  fi
  sleep 2
done

# Wait for Keycloak (health endpoint on port 9000, still exposed on host)
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

# Wait for Traefik (check internal ping endpoint and route loading via docker exec)
echo "[ci-setup] Waiting for Traefik..."
for i in $(seq 1 30); do
  routers="$(docker exec traefik wget -qO- http://localhost:8080/api/http/routers 2>/dev/null || echo "")"
  if echo "$routers" | grep -q "keycloak" && echo "$routers" | grep -q "harbor" && echo "$routers" | grep -q "openbao"; then
    echo "[ci-setup] Traefik is ready"
    break
  fi
  if [[ $i -eq 30 ]]; then
    echo "[ci-setup] ERROR: Traefik did not load all routes" >&2
    echo "[ci-setup] Traefik logs:" >&2
    docker logs traefik 2>&1 | tail -30
    exit 1
  fi
  sleep 2
done

# Wait for Harbor core (check via HTTPS through Traefik – validates the full TLS stack)
echo "[ci-setup] Waiting for Harbor..."
for i in $(seq 1 120); do
  health="$(curl -fsS https://harbor.example.com/api/v2.0/health 2>/dev/null || echo "")"
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
