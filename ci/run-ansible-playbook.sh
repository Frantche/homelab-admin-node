#!/usr/bin/env bash
set -euo pipefail

# Runs the Ansible playbook with CI-specific extra vars.
# Usage: ci/run-ansible-playbook.sh [extra ansible-playbook args...]
#
# This wraps ansible-playbook with the correct inventory and CI overrides
# so that the playbook can be tested in the CI environment.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Install required Ansible collections
if [[ -f "$REPO_ROOT/ansible/requirements.yml" ]]; then
  echo "[ansible] Installing required collections..."
  ansible-galaxy collection install -r "$REPO_ROOT/ansible/requirements.yml" --force 2>/dev/null || true
fi

# Build CI extra vars (secrets that would normally come from SOPS)
CI_EXTRA_VARS=$(cat <<EOF
{
  "admin": {
    "traefik_dashboard_basic_auth": "admin:\$\$apr1\$\$fakehash"
  },
  "cloudflare": {
    "tunnel_id": "fake-tunnel-id",
    "tunnel_token": "eyJhIjoiZmFrZSIsInQiOiJmYWtlIiwicyI6ImZha2UifQ==",
    "account_id": "fake-account-id",
    "dns_api_token": "fake-dns-token",
    "credentials_json": "{}"
  },
  "keycloak": {
    "db_password": "ci-keycloak-db-pass",
    "admin_user": "admin",
    "admin_password": "ci-keycloak-admin-pass"
  },
  "harbor": {
    "admin_password": "ci-Harbor-admin-p4ss",
    "db_password": "ci-harbor-db-pass"
  },
  "openbao": {
    "root_token": "${OPENBAO_TOKEN:-}"
  },
  "backup": {
    "restic_repository": "/srv/admin/backups/restic",
    "restic_password": "ci-restic-pass"
  },
  "ci_mode": true
}
EOF
)

echo "[ansible] Running playbook..."
ansible-playbook \
  -i "$REPO_ROOT/ansible/inventory.ini" \
  "$REPO_ROOT/ansible/site.yml" \
  --extra-vars "$CI_EXTRA_VARS" \
  "$@"

# After playbook, docker may have been restarted (daemon.json change triggers handler),
# which seals OpenBao. Re-unseal if needed.
echo "[ansible] Checking if OpenBao needs re-unsealing after playbook..."
for _i in $(seq 1 30); do
  bao_out=""
  if bao_out="$(docker exec -e BAO_ADDR=http://127.0.0.1:8200 openbao bao status -format=json 2>/dev/null)"; then
    break
  fi
  # Exit code 2 means sealed but reachable
  if [[ $? -eq 2 ]] || echo "$bao_out" | grep -q "sealed"; then
    break
  fi
  sleep 2
done

bao_status=""
bao_rc=0
bao_status="$(docker exec -e BAO_ADDR=http://127.0.0.1:8200 openbao bao status -format=json 2>/dev/null)" || bao_rc=$?
if [[ $bao_rc -eq 2 ]] || (echo "$bao_status" | python3 -c 'import json,sys; d=json.load(sys.stdin); exit(0 if d.get("sealed") else 1)' 2>/dev/null); then
  echo "[ansible] OpenBao is sealed, re-unsealing..."
  SECRETS_FILE=/opt/homelab-admin-node/secrets/openbao-unseal.sops.yaml
  if [[ -f "$SECRETS_FILE" ]]; then
    python3 -c "
import yaml, sys
d = yaml.safe_load(open(sys.argv[1]))
active = d['openbao']['active_keyset']
ks = d['openbao']['keysets'][active]
threshold = int(ks['threshold'])
for k in ks['unseal_keys'][:threshold]:
    print(k)
" "$SECRETS_FILE" | while IFS= read -r key; do
      docker exec -e BAO_ADDR=http://127.0.0.1:8200 openbao bao operator unseal "$key" >/dev/null 2>&1 || true
    done
    echo "[ansible] OpenBao re-unsealed"
  else
    echo "[ansible] WARNING: Cannot re-unseal OpenBao (no secrets file)"
  fi
else
  echo "[ansible] OpenBao is already unsealed"
fi
