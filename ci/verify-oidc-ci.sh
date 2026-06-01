#!/usr/bin/env bash
set -euo pipefail

# Validate SSO by performing actual authentication through the OIDC chain:
#  1. Obtain an ID token from Keycloak via Resource Owner Password Credentials grant
#  2. Authenticate to OpenBao using that ID token (JWT auth via the OIDC auth method)
#  3. Verify Harbor can reach Keycloak's OIDC endpoint (ping test from Harbor)
#
# All calls use HTTPS through Traefik. The CI certificate is self-signed so
# curl uses -k and services are configured with verify_cert=false.

KEYCLOAK_URL="https://keycloak.example.com"
OPENBAO_URL="https://bao.example.com"
HARBOR_URL="https://harbor.example.com"

HARBOR_ADMIN_PASSWORD="${HARBOR_ADMIN_PASSWORD:-ci-Harbor-admin-p4ss}"
OPENBAO_TOKEN="${OPENBAO_TOKEN:-}"

KEYCLOAK_REALM="homelab"
OPENBAO_CLIENT_ID="openbao"
OPENBAO_CLIENT_SECRET="ci-openbao-client-secret"
CI_USER="ci-admin"
CI_PASSWORD="ci-admin-pass"
HARBOR_OIDC_ENDPOINT="${KEYCLOAK_URL}/realms/${KEYCLOAK_REALM}"

if [[ -z "$OPENBAO_TOKEN" ]]; then
  echo "[verify-oidc] ERROR: OPENBAO_TOKEN is not set" >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Step 1: Authenticate ci-admin with Keycloak (Resource Owner Password Grant)
# ---------------------------------------------------------------------------
echo "[verify-oidc] Step 1: Authenticating ${CI_USER} with Keycloak..."
keycloak_response=$(curl -sf -k \
  -X POST \
  "${KEYCLOAK_URL}/realms/${KEYCLOAK_REALM}/protocol/openid-connect/token" \
  -d "grant_type=password" \
  -d "client_id=${OPENBAO_CLIENT_ID}" \
  -d "client_secret=${OPENBAO_CLIENT_SECRET}" \
  -d "username=${CI_USER}" \
  -d "password=${CI_PASSWORD}" \
  -d "scope=openid profile")

id_token=$(echo "$keycloak_response" | python3 -c "
import json, sys
d = json.load(sys.stdin)
t = d.get('id_token', '')
if not t:
    print('ERROR: no id_token in Keycloak response', file=sys.stderr)
    sys.exit(1)
print(t)
")
echo "[verify-oidc] OK: Keycloak issued ID token for ${CI_USER}"

# ---------------------------------------------------------------------------
# Step 2: Authenticate with OpenBao using the Keycloak ID token (JWT login)
# ---------------------------------------------------------------------------
echo "[verify-oidc] Step 2: Authenticating with OpenBao via Keycloak ID token..."
openbao_login=$(curl -sf -k \
  -X POST \
  "${OPENBAO_URL}/v1/auth/oidc/login" \
  -H "Content-Type: application/json" \
  -d "{\"role\": \"default\", \"jwt\": \"${id_token}\"}")

openbao_entity=$(echo "$openbao_login" | python3 -c "
import json, sys
d = json.load(sys.stdin)
token = d.get('auth', {}).get('client_token', '')
if not token:
    print('ERROR: no client_token in OpenBao login response', file=sys.stderr)
    sys.exit(1)
username = d.get('auth', {}).get('metadata', {}).get('username', '?')
print(username)
")
echo "[verify-oidc] OK: OpenBao authenticated '${openbao_entity}' via Keycloak OIDC"

# ---------------------------------------------------------------------------
# Step 3: Verify Harbor can reach Keycloak's OIDC endpoint (connectivity ping)
# ---------------------------------------------------------------------------
echo "[verify-oidc] Step 3: Testing Harbor OIDC connectivity to Keycloak..."
curl -sf -k \
  -X POST \
  -u "admin:${HARBOR_ADMIN_PASSWORD}" \
  -H "Content-Type: application/json" \
  "${HARBOR_URL}/api/v2.0/system/oidc/ping" \
  -d "{\"url\": \"${HARBOR_OIDC_ENDPOINT}\", \"verify_cert\": false}" \
  > /dev/null
echo "[verify-oidc] OK: Harbor successfully reached Keycloak OIDC endpoint"

echo "[verify-oidc] All SSO authentication checks passed"
