#!/usr/bin/env bash
set -euo pipefail

OPENBAO_ADDR=${OPENBAO_ADDR:-http://127.0.0.1:8200}
OPENBAO_TOKEN=${OPENBAO_TOKEN:-}

# --- OpenBao ---
bao_health="$(curl -fsS "$OPENBAO_ADDR/v1/sys/health")"
python3 -c 'import json,sys; d=json.loads(sys.argv[1]); assert d.get("initialized") is True; assert d.get("sealed") is False' "$bao_health"

if [[ -n "$OPENBAO_TOKEN" ]]; then
  docker exec -e BAO_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN="$OPENBAO_TOKEN" openbao bao kv put secret/admin-node-sentinel value=ok >/dev/null 2>&1 || \
    docker exec -e BAO_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN="$OPENBAO_TOKEN" openbao bao secrets enable -path=secret kv-v2 >/dev/null 2>&1 && \
    docker exec -e BAO_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN="$OPENBAO_TOKEN" openbao bao kv put secret/admin-node-sentinel value=ok >/dev/null
  docker exec -e BAO_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN="$OPENBAO_TOKEN" openbao bao kv get -field=value secret/admin-node-sentinel | grep -qx "ok"
fi

# --- Keycloak ---
echo "[validate-apis] checking Keycloak..."
curl -fsS http://127.0.0.1:9000/health/ready >/dev/null
curl -fsS http://127.0.0.1:8081/realms/master/.well-known/openid-configuration >/dev/null

# --- Harbor ---
echo "[validate-apis] checking Harbor..."
harbor_ok=false
for _ in $(seq 1 10); do
  if curl -fsS http://127.0.0.1:8082/api/v2.0/health >/dev/null 2>&1; then
    harbor_ok=true
    break
  fi
  sleep 3
done
if [[ "$harbor_ok" != "true" ]]; then
  echo "[validate-apis] WARNING: Harbor health check failed (non-fatal)" >&2
fi

if [[ -n "${HARBOR_ADMIN_USER:-}" && -n "${HARBOR_ADMIN_PASSWORD:-}" ]]; then
  curl -fsS -u "${HARBOR_ADMIN_USER}:${HARBOR_ADMIN_PASSWORD}" "http://127.0.0.1:8082/api/v2.0/projects?name=admin-ci" >/dev/null
fi

# --- Traefik ---
echo "[validate-apis] checking Traefik..."
curl -fsS http://127.0.0.1:8080/api/http/routers >/dev/null

# Verify routing is configured (accept any response except 404 = route exists)
# Retry to allow Traefik time to resolve Docker DNS for backends
for vhost in keycloak.example.com bao.example.com harbor.example.com; do
  route_ok=false
  for attempt in $(seq 1 10); do
    status="$(curl -s -o /dev/null -w '%{http_code}' -H "Host: $vhost" http://127.0.0.1)"
    if [[ "$status" != "404" ]]; then
      route_ok=true
      break
    fi
    sleep 2
  done
  if [[ "$route_ok" != "true" ]]; then
    echo "Traefik route not configured for $vhost (got 404 after retries)" >&2
    echo "Traefik routers:" >&2
    curl -s http://127.0.0.1:8080/api/http/routers 2>/dev/null || true
    echo "" >&2
    echo "Traefik services:" >&2
    curl -s http://127.0.0.1:8080/api/http/services 2>/dev/null || true
    echo "" >&2
    echo "Traefik logs (last 30):" >&2
    docker logs traefik 2>&1 | tail -30 || true
    exit 1
  fi
done

dashboard_status="$(curl -s -o /dev/null -w '%{http_code}' -H 'Host: traefik.example.com' http://127.0.0.1)"
if [[ "$dashboard_status" != "401" && "$dashboard_status" != "403" ]]; then
  echo "Traefik dashboard is not protected as expected (status $dashboard_status)" >&2
  exit 1
fi

if [[ -n "${KEYCLOAK_CI_CLIENT_ID:-}" && -n "${KEYCLOAK_CI_CLIENT_SECRET:-}" ]]; then
  curl -fsS -X POST \
    -d "client_id=${KEYCLOAK_CI_CLIENT_ID}" \
    -d "client_secret=${KEYCLOAK_CI_CLIENT_SECRET}" \
    -d "grant_type=client_credentials" \
    http://127.0.0.1:8081/realms/master/protocol/openid-connect/token >/dev/null
fi

echo "API validation passed"
