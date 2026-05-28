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
