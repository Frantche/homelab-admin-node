#!/usr/bin/env bash
set -euo pipefail

source ./ci/assertions.sh

# --- Setup: deploy stacks and start services ---
./ci/setup-ci-env.sh

# --- Start in init mode ---
./scripts/set-mode.sh init
assert_contains /etc/admin-node/mode "init"

# --- Initialize and unseal OpenBao ---
./ci/init-openbao-ci.sh
OPENBAO_TOKEN="$(cat /opt/homelab-admin-node/secrets/openbao-root-token)"
export OPENBAO_TOKEN

# --- Normal mode ---
./scripts/set-mode.sh normal
assert_contains /etc/admin-node/mode "normal"

# --- Create data and backup ---
./ci/create-sentinel-data.sh
assert_file_exists /srv/admin/data/sentinel/value.txt

export SKIP_PUBLIC_URL_VALIDATION=true
./scripts/backup.sh

# --- Simulate branch upgrade: set new git-ref ---
echo "${GITHUB_HEAD_REF:-test-branch}" > /etc/admin-node/git-ref
assert_file_exists /etc/admin-node/git-ref

# --- Validate after upgrade (services still running) ---
./scripts/validate-apis.sh
./scripts/validate-dns.sh
./scripts/validate-cloudflare-tunnel.sh

# --- Backup after upgrade ---
./scripts/backup.sh

echo "=== upgrade-main-to-branch scenario PASSED ==="
