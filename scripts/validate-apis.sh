#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OPENBAO_TOKEN=${OPENBAO_TOKEN:-}
KEYCLOAK_DOMAIN="${KEYCLOAK_DOMAIN:-$("$REPO_ROOT/ci/service-domains.py" get keycloak)}"
HARBOR_DOMAIN="${HARBOR_DOMAIN:-$("$REPO_ROOT/ci/service-domains.py" get harbor)}"
GITEA_DOMAIN="${GITEA_DOMAIN:-$("$REPO_ROOT/ci/service-domains.py" get gitea)}"
TRAEFIK_DOMAIN="${TRAEFIK_DOMAIN:-$("$REPO_ROOT/ci/service-domains.py" get traefik)}"
OPENBAO_DOMAIN="${OPENBAO_DOMAIN:-$("$REPO_ROOT/ci/service-domains.py" get openbao)}"

# Load Traefik dashboard credentials if available
if [[ -f /etc/admin-node/traefik-dashboard-creds ]]; then
  source /etc/admin-node/traefik-dashboard-creds
fi

# --- OpenBao ---
echo "[validate-apis] checking OpenBao..."
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
keycloak_ok=false
for _ in $(seq 1 20); do
  if curl -fsS "https://${KEYCLOAK_DOMAIN}/realms/master/.well-known/openid-configuration" >/dev/null 2>&1; then
    keycloak_ok=true
    break
  fi
  sleep 3
done
if [[ "$keycloak_ok" != "true" ]]; then
  echo "[validate-apis] ERROR: Keycloak health check failed" >&2
  exit 1
fi
curl -fsS "https://${KEYCLOAK_DOMAIN}/realms/master/.well-known/openid-configuration" >/dev/null

# --- Harbor ---
echo "[validate-apis] checking Harbor..."
harbor_ok=false
for _ in $(seq 1 40); do
  health="$(curl -fsS "https://${HARBOR_DOMAIN}/api/v2.0/health" 2>/dev/null)" || { sleep 3; continue; }
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
  curl -fsS -u "${HARBOR_ADMIN_USER}:${HARBOR_ADMIN_PASSWORD}" "https://${HARBOR_DOMAIN}/api/v2.0/projects?name=admin-ci" >/dev/null
fi

# --- Gitea ---
echo "[validate-apis] checking Gitea..."
"$SCRIPT_DIR/validate-gitea-data.sh"

# --- Traefik ---
echo "[validate-apis] checking Traefik..."
if [[ -n "${TRAEFIK_DASHBOARD_USER:-}" && -n "${TRAEFIK_DASHBOARD_PASS:-}" ]]; then
  traefik_routers="$(curl -fsS -u "${TRAEFIK_DASHBOARD_USER}:${TRAEFIK_DASHBOARD_PASS}" "https://${TRAEFIK_DOMAIN}/api/http/routers" 2>/dev/null)"
  if [[ -z "$traefik_routers" ]]; then
    echo "Traefik API not reachable via HTTPS" >&2
    exit 1
  fi
  for vhost in "$KEYCLOAK_DOMAIN" "$OPENBAO_DOMAIN" "$HARBOR_DOMAIN" "$GITEA_DOMAIN"; do
    if ! echo "$traefik_routers" | grep -q "$vhost"; then
      echo "Traefik route not configured for $vhost" >&2
      exit 1
    fi
  done
else
  echo "[validate-apis] WARN: Traefik dashboard credentials not available, skipping route validation" >&2
fi

echo "API validation passed"
