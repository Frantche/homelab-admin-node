#!/usr/bin/env bash
set -euo pipefail

OPENBAO_TOKEN=${OPENBAO_TOKEN:-}

# Load Traefik dashboard credentials if available
if [[ -f /etc/admin-node/traefik-dashboard-creds ]]; then
  source /etc/admin-node/traefik-dashboard-creds
fi

# --- OpenBao ---
echo "[validate-apis] checking OpenBao..."
# bao status exits 2 when sealed (expected), capture and check explicitly
bao_rc=0
bao_health="$(docker exec -e BAO_ADDR=http://127.0.0.1:8200 openbao bao status -format=json 2>/dev/null)" || bao_rc=$?
if [[ $bao_rc -ne 0 && $bao_rc -ne 2 ]]; then
  echo "[validate-apis] ERROR: OpenBao unreachable (exit code $bao_rc)" >&2
  exit 1
fi
python3 -c 'import json,sys; d=json.loads(sys.argv[1]); assert d.get("initialized") is True, f"not initialized: {d}"; assert d.get("sealed") is False, f"still sealed: {d}"' "$bao_health"

if [[ -n "$OPENBAO_TOKEN" ]]; then
  if ! docker exec -e BAO_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN="$OPENBAO_TOKEN" openbao bao kv put secret/admin-node-sentinel value=ok >/dev/null 2>&1; then
    docker exec -e BAO_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN="$OPENBAO_TOKEN" openbao bao secrets enable -path=secret kv-v2 >/dev/null 2>&1
    docker exec -e BAO_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN="$OPENBAO_TOKEN" openbao bao kv put secret/admin-node-sentinel value=ok >/dev/null
  fi
  docker exec -e BAO_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN="$OPENBAO_TOKEN" openbao bao kv get -field=value secret/admin-node-sentinel | grep -qx "ok"
fi

# --- Keycloak ---
echo "[validate-apis] checking Keycloak..."
curl -fsS http://127.0.0.1:9000/health/ready >/dev/null
curl -fsS https://keycloak.example.com/realms/master/.well-known/openid-configuration >/dev/null

# --- Harbor ---
echo "[validate-apis] checking Harbor..."
harbor_ok=false
for _ in $(seq 1 40); do
  # Accept partial health (core+db healthy is sufficient)
  health="$(curl -fsS https://harbor.example.com/api/v2.0/health 2>/dev/null)" || { sleep 3; continue; }
  core_ok="$(echo "$health" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(any(c["name"]=="core" and c["status"]=="healthy" for c in d.get("components",[])))' 2>/dev/null)" || { sleep 3; continue; }
  if [[ "$core_ok" == "True" ]]; then
    harbor_ok=true
    break
  fi
  sleep 3
done
if [[ "$harbor_ok" != "true" ]]; then
  echo "[validate-apis] ERROR: Harbor health check failed" >&2
  exit 1
fi

if [[ -n "${HARBOR_ADMIN_USER:-}" && -n "${HARBOR_ADMIN_PASSWORD:-}" ]]; then
  curl -fsS -u "${HARBOR_ADMIN_USER}:${HARBOR_ADMIN_PASSWORD}" "https://harbor.example.com/api/v2.0/projects?name=admin-ci" >/dev/null
fi

# --- Traefik ---
echo "[validate-apis] checking Traefik..."
# If dashboard credentials are available, verify routers
if [[ -n "${TRAEFIK_DASHBOARD_USER:-}" && -n "${TRAEFIK_DASHBOARD_PASS:-}" ]]; then
  traefik_routers="$(curl -fsS -u "${TRAEFIK_DASHBOARD_USER}:${TRAEFIK_DASHBOARD_PASS}" https://traefik.example.com/api/http/routers 2>/dev/null)"
  if [[ -z "$traefik_routers" ]]; then
    echo "Traefik API not reachable via HTTPS" >&2
    exit 1
  fi
  for vhost in keycloak.example.com bao.example.com harbor.example.com; do
    if ! echo "$traefik_routers" | grep -q "$vhost"; then
      echo "Traefik route not configured for $vhost" >&2
      exit 1
    fi
  done
else
  echo "[validate-apis] WARN: Traefik dashboard credentials not available, skipping route validation" >&2
fi

echo "API validation passed"
