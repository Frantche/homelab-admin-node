#!/usr/bin/env bash
set -euo pipefail

# Verify that OIDC has been correctly configured on OpenBao and Harbor.
# Requires OPENBAO_TOKEN to be set.

HARBOR_ADMIN_PASSWORD="${HARBOR_ADMIN_PASSWORD:-ci-Harbor-admin-p4ss}"
OPENBAO_TOKEN="${OPENBAO_TOKEN:-}"

if [[ -z "$OPENBAO_TOKEN" ]]; then
  echo "[verify-oidc] ERROR: OPENBAO_TOKEN is not set" >&2
  exit 1
fi

echo "[verify-oidc] Checking OpenBao OIDC auth method..."
bao_auth_methods="$(curl -sf -k \
  -H "X-Vault-Token: ${OPENBAO_TOKEN}" \
  https://bao.example.com/v1/sys/auth)"

if ! echo "$bao_auth_methods" | python3 -c "
import json, sys
d = json.load(sys.stdin)
methods = d.get('data', d)
if 'oidc/' not in methods:
    print('ERROR: oidc/ auth method not found in OpenBao', file=sys.stderr)
    sys.exit(1)
print('OK: oidc/ auth method present')
"; then
  echo "[verify-oidc] ERROR: OpenBao OIDC auth method check failed" >&2
  exit 1
fi

echo "[verify-oidc] Checking OpenBao OIDC configuration..."
bao_oidc_config="$(curl -sf -k \
  -H "X-Vault-Token: ${OPENBAO_TOKEN}" \
  https://bao.example.com/v1/auth/oidc/config)"

python3 -c "
import json, sys
d = json.load(sys.stdin).get('data', {})
issuer = d.get('oidc_discovery_url', '')
client_id = d.get('oidc_client_id', '')
if 'keycloak' not in issuer:
    print(f'ERROR: unexpected oidc_discovery_url: {issuer}', file=sys.stderr)
    sys.exit(1)
if client_id != 'openbao':
    print(f'ERROR: unexpected oidc_client_id: {client_id}', file=sys.stderr)
    sys.exit(1)
print(f'OK: OpenBao OIDC configured (issuer={issuer}, client_id={client_id})')
" <<< "$bao_oidc_config"

echo "[verify-oidc] Checking Harbor OIDC configuration..."
harbor_config_resp="$(curl -sf -k \
  -u "admin:${HARBOR_ADMIN_PASSWORD}" \
  https://harbor.example.com/api/v2.0/configurations)"

python3 -c "
import json, sys
d = json.load(sys.stdin)
auth_mode = d.get('auth_mode', {}).get('value', '')
oidc_name = d.get('oidc_name', {}).get('value', '')
oidc_endpoint = d.get('oidc_endpoint', {}).get('value', '')
oidc_client_id = d.get('oidc_client_id', {}).get('value', '')
if auth_mode != 'oidc_auth':
    print(f'ERROR: Harbor auth_mode is {auth_mode!r}, expected oidc_auth', file=sys.stderr)
    sys.exit(1)
if 'keycloak' not in oidc_endpoint:
    print(f'ERROR: unexpected oidc_endpoint: {oidc_endpoint}', file=sys.stderr)
    sys.exit(1)
if oidc_client_id != 'harbor':
    print(f'ERROR: unexpected oidc_client_id: {oidc_client_id}', file=sys.stderr)
    sys.exit(1)
print(f'OK: Harbor OIDC configured (auth_mode={auth_mode}, oidc_name={oidc_name}, endpoint={oidc_endpoint})')
" <<< "$harbor_config_resp"

echo "[verify-oidc] All OIDC checks passed"
