#!/usr/bin/env bash
set -euo pipefail

source ./ci/assertions.sh

# Export CI env vars
export CI_MOCK_PIHOLE="${CI_MOCK_PIHOLE:-true}"
export CI_MOCK_CLOUDFLARE_TUNNEL="${CI_MOCK_CLOUDFLARE_TUNNEL:-true}"
export CI_SKIP_PUBLIC_URL_VALIDATION="${CI_SKIP_PUBLIC_URL_VALIDATION:-true}"
export SKIP_PUBLIC_URL_VALIDATION="${SKIP_PUBLIC_URL_VALIDATION:-true}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CI_INVENTORY="${CI_INVENTORY:-/etc/admin-config/homelab-node-admin-config/hosts/inventory.ini}"

# --- CI prerequisites (TLS certs, /etc/hosts, ansible collections) ---
./ci/setup-ci-env.sh
./scripts/build-admin-node.sh

# --- Install mock config repo (demonstrates the config-repo pattern) ---
./bin/admin-node ci install-mock-config-repo

# --- Init phase ---
./bin/admin-node mode set locked
assert_file_exists /etc/admin-node/mode
assert_contains /etc/admin-node/mode "locked"

./bin/admin-node mode set init
assert_contains /etc/admin-node/mode "init"

# --- Deploy via Ansible playbook with config repo (init mode - starts services) ---
echo "=== Running Ansible playbook (init mode) ==="
ansible-playbook \
  -i "$CI_INVENTORY" \
  "$REPO_ROOT/ansible/site.yml"

# --- Initialize and unseal OpenBao ---
./bin/admin-node ci init-openbao
OPENBAO_TOKEN="$(cat /opt/homelab-admin-node/secrets/openbao-root-token)"
export OPENBAO_TOKEN

# --- Normal mode ---
./bin/admin-node mode set normal
assert_contains /etc/admin-node/mode "normal"

# --- Re-run playbook in normal mode (validates, backs up) ---
echo "=== Running Ansible playbook (normal mode) ==="
ansible-playbook \
  -i "$CI_INVENTORY" \
  "$REPO_ROOT/ansible/site.yml" \
  --extra-vars "{\"openbao\": {\"root_token\": \"${OPENBAO_TOKEN}\"}}"

./bin/admin-node validate observability

# --- Create sentinel data + additional backups ---
./bin/admin-node ci create-sentinel
assert_file_exists /srv/admin/data/sentinel/value.txt

./bin/admin-node backup run
# Verify backup directory was created
BACKUP_COUNT="$(find /srv/admin/backups/local -mindepth 1 -maxdepth 1 -type d | wc -l)"
if [[ "$BACKUP_COUNT" -lt 1 ]]; then
  echo "Expected at least 1 backup directory, found $BACKUP_COUNT" >&2
  exit 1
fi

# Run multiple backups to test retention
sleep 1 && ./bin/admin-node backup run
sleep 1 && ./bin/admin-node backup run
sleep 1 && ./bin/admin-node backup run

# Verify retention keeps max 3 local backups
BACKUP_COUNT="$(find /srv/admin/backups/local -mindepth 1 -maxdepth 1 -type d | wc -l)"
if [[ "$BACKUP_COUNT" -gt 3 ]]; then
  echo "Expected at most 3 backup directories (retention), found $BACKUP_COUNT" >&2
  exit 1
fi

# --- Restore ---
./bin/admin-node mode set restore
assert_contains /etc/admin-node/mode "restore"

./bin/admin-node restore run
assert_contains /etc/admin-node/mode "normal"

echo "=== fresh-branch scenario PASSED ==="
