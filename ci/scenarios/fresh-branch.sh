#!/usr/bin/env bash
set -euo pipefail

source ./ci/assertions.sh

# Export CI env vars for child scripts
export CI_MOCK_PIHOLE="${CI_MOCK_PIHOLE:-true}"
export CI_MOCK_CLOUDFLARE_TUNNEL="${CI_MOCK_CLOUDFLARE_TUNNEL:-true}"
export CI_SKIP_PUBLIC_URL_VALIDATION="${CI_SKIP_PUBLIC_URL_VALIDATION:-true}"
export SKIP_PUBLIC_URL_VALIDATION="${SKIP_PUBLIC_URL_VALIDATION:-true}"

# --- Setup: deploy stacks and start services ---
./ci/setup-ci-env.sh

# --- Init phase ---
./scripts/set-mode.sh locked
assert_file_exists /etc/admin-node/mode
assert_contains /etc/admin-node/mode "locked"

./scripts/set-mode.sh init
assert_contains /etc/admin-node/mode "init"

# --- Initialize and unseal OpenBao ---
./ci/init-openbao-ci.sh
OPENBAO_TOKEN="$(cat /opt/homelab-admin-node/secrets/openbao-root-token)"
export OPENBAO_TOKEN

# --- Normal mode ---
./scripts/set-mode.sh normal
assert_contains /etc/admin-node/mode "normal"

# --- Run Ansible playbook to converge the node ---
echo "=== Running Ansible playbook deployment ==="
./ci/run-ansible-playbook.sh

# --- Validate APIs (real services) ---
./scripts/validate-apis.sh
./scripts/validate-dns.sh
./scripts/validate-cloudflare-tunnel.sh

# --- Create sentinel data + backup ---
./ci/create-sentinel-data.sh
assert_file_exists /srv/admin/data/sentinel/value.txt

./scripts/backup.sh
# Verify backup directory was created
BACKUP_COUNT="$(find /srv/admin/backups/local -mindepth 1 -maxdepth 1 -type d | wc -l)"
if [[ "$BACKUP_COUNT" -lt 1 ]]; then
  echo "Expected at least 1 backup directory, found $BACKUP_COUNT" >&2
  exit 1
fi

# Run multiple backups to test retention
sleep 1 && ./scripts/backup.sh
sleep 1 && ./scripts/backup.sh
sleep 1 && ./scripts/backup.sh

# Verify retention keeps max 3 local backups
BACKUP_COUNT="$(find /srv/admin/backups/local -mindepth 1 -maxdepth 1 -type d | wc -l)"
if [[ "$BACKUP_COUNT" -gt 3 ]]; then
  echo "Expected at most 3 backup directories (retention), found $BACKUP_COUNT" >&2
  exit 1
fi

# --- Restore ---
./scripts/set-mode.sh restore
assert_contains /etc/admin-node/mode "restore"

./scripts/restore.sh
assert_contains /etc/admin-node/mode "normal"

echo "=== fresh-branch scenario PASSED ==="
