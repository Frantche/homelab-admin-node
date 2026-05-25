#!/usr/bin/env bash
set -euo pipefail

source ./ci/assertions.sh

export CI=true

# --- Setup ---
./ci/setup-ci-env.sh
./ci/generate-fake-sops.sh

# --- Start in init mode, do first converge ---
./scripts/set-mode.sh init
assert_contains /etc/admin-node/mode "init"

./scripts/admin-converge.sh
./scripts/set-mode.sh normal
assert_contains /etc/admin-node/mode "normal"

# --- Create data and backup ---
./ci/create-sentinel-data.sh
assert_file_exists /srv/admin/data/sentinel/value.txt
./scripts/backup.sh

# --- Simulate branch upgrade: set new git-ref and reconverge ---
echo "${GITHUB_HEAD_REF:-test-branch}" > /etc/admin-node/git-ref
assert_file_exists /etc/admin-node/git-ref

./scripts/admin-converge.sh

# --- Validate after upgrade ---
./scripts/validate-apis.sh
./scripts/validate-dns.sh
./scripts/validate-cloudflare-tunnel.sh

# --- Backup after upgrade ---
./scripts/backup.sh

echo "=== upgrade-main-to-branch scenario PASSED ==="
