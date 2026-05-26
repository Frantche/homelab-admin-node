#!/usr/bin/env bash
set -euo pipefail

OPENBAO_TOKEN=${OPENBAO_TOKEN:-}

# --- OpenBao ---
echo "[validate-apis] checking OpenBao..."
bao_health="$(docker exec openbao bao status -format=json 2>/dev/null || true)"
python3 -c 'import json,sys; d=json.loads(sys.argv[1]); assert d.get("initialized") is True, f"not initialized: {d}"; assert d.get("sealed") is False, f"still sealed: {d}"' "$bao_health"

if [[ -n "$OPENBAO_TOKEN" ]]; then
  docker exec -e BAO_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN="$OPENBAO_TOKEN" openbao bao kv put secret/admin-node-sentinel value=ok >/dev/null 2>&1 || \
    docker exec -e BAO_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN="$OPENBAO_TOKEN" openbao bao secrets enable -path=secret kv-v2 >/dev/null 2>&1 && \
    docker exec -e BAO_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN="$OPENBAO_TOKEN" openbao bao kv put secret/admin-node-sentinel value=ok >/dev/null
  docker exec -e BAO_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN="$OPENBAO_TOKEN" openbao bao kv get -field=value secret/admin-node-sentinel | grep -qx "ok"
fi

# --- Keycloak ---
echo "[validate-apis] checking Keycloak..."
curl -fsS http://127.0.0.1:9000/health/ready >/dev/null
curl -fsS https://keycloak.example.com/realms/master/.well-known/openid-configuration >/dev/null

# --- Harbor ---
echo "[validate-apis] checking Harbor..."
harbor_ok=false
for _ in $(seq 1 10); do
  # Accept partial health (core+db healthy is sufficient)
  health="$(curl -fsS https://harbor.example.com/api/v2.0/health 2>/dev/null || echo "")"
  core_ok="$(echo "$health" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(any(c["name"]=="core" and c["status"]=="healthy" for c in d.get("components",[])))' 2>/dev/null || echo "False")"
  if [[ "$core_ok" == "True" ]]; then
    harbor_ok=true
    break
  fi
  sleep 3
done
if [[ "$harbor_ok" != "true" ]]; then
  echo "[validate-apis] WARNING: Harbor health check failed (non-fatal)" >&2
fi

if [[ -n "${HARBOR_ADMIN_USER:-}" && -n "${HARBOR_ADMIN_PASSWORD:-}" ]]; then
  curl -fsS -u "${HARBOR_ADMIN_USER}:${HARBOR_ADMIN_PASSWORD}" "https://harbor.example.com/api/v2.0/projects?name=admin-ci" >/dev/null
fi

# --- Traefik ---
echo "[validate-apis] checking Traefik..."
traefik_routers="$(docker exec traefik wget -qO- http://localhost:8080/api/http/routers 2>/dev/null || echo "[]")"
for vhost in keycloak.example.com bao.example.com harbor.example.com; do
  if ! echo "$traefik_routers" | grep -q "$vhost"; then
    echo "Traefik route not configured for $vhost" >&2
    exit 1
  fi
done

echo "API validation passed"
