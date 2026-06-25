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
./ci/setup-ci-config-repo.sh

# --- Init and set up OpenBao ---
./bin/admin-node mode set init
assert_contains /etc/admin-node/mode "init"

# --- Deploy via Ansible playbook with config repo (init mode) ---
echo "=== Running Ansible playbook (init mode) ==="
ansible-playbook \
  -i "$CI_INVENTORY" \
  "$REPO_ROOT/ansible/site.yml"

# --- Initialize and unseal OpenBao ---
./ci/init-openbao-ci.sh
OPENBAO_TOKEN="$(cat /opt/homelab-admin-node/secrets/openbao-root-token)"
export OPENBAO_TOKEN

# --- Normal mode, create data ---
./bin/admin-node mode set normal
./ci/create-sentinel-data.sh
assert_file_exists /srv/admin/data/sentinel/value.txt

# --- Backup ---
./bin/admin-node backup run

BACKUP_COUNT="$(find /srv/admin/backups/local -mindepth 1 -maxdepth 1 -type d | wc -l)"
if [[ "$BACKUP_COUNT" -lt 1 ]]; then
  echo "Expected at least 1 backup, found $BACKUP_COUNT" >&2
  exit 1
fi

# --- Restore flow ---
./bin/admin-node mode set restore
assert_contains /etc/admin-node/mode "restore"

./bin/admin-node restore run

# restore should set mode to normal on success
assert_contains /etc/admin-node/mode "normal"

# --- Post-restore: re-run playbook to validate ---
echo "=== Running Ansible playbook (post-restore) ==="
ansible-playbook \
  -i "$CI_INVENTORY" \
  "$REPO_ROOT/ansible/site.yml" \
  --extra-vars "{\"openbao\": {\"root_token\": \"${OPENBAO_TOKEN}\"}}"

echo "=== restore-main-backup-with-branch scenario PASSED ==="
