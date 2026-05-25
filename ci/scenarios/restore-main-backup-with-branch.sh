#!/usr/bin/env bash
set -euo pipefail

source ./ci/assertions.sh

export CI=true

# --- Setup ---
./ci/setup-ci-env.sh
./ci/generate-fake-sops.sh

# --- Init and create data ---
./scripts/set-mode.sh init
assert_contains /etc/admin-node/mode "init"

./scripts/set-mode.sh normal
./ci/create-sentinel-data.sh
assert_file_exists /srv/admin/data/sentinel/value.txt

# --- Backup ---
./scripts/backup.sh

BACKUP_COUNT="$(find /srv/admin/backups/local -mindepth 1 -maxdepth 1 -type d | wc -l)"
if [[ "$BACKUP_COUNT" -lt 1 ]]; then
  echo "Expected at least 1 backup, found $BACKUP_COUNT" >&2
  exit 1
fi

# --- Restore flow ---
./scripts/set-mode.sh restore
assert_contains /etc/admin-node/mode "restore"

./scripts/restore.sh

# restore.sh should set mode to normal on success
assert_contains /etc/admin-node/mode "normal"

# --- Post-restore validation ---
./scripts/validate-apis.sh
./scripts/validate-dns.sh
./scripts/validate-cloudflare-tunnel.sh

echo "=== restore-main-backup-with-branch scenario PASSED ==="
