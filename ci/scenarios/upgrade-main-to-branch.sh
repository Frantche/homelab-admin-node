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

# --- Run Ansible playbook to converge the node ---
echo "=== Running Ansible playbook deployment ==="
./ci/run-ansible-playbook.sh

# --- Create data and backup ---
./ci/create-sentinel-data.sh
assert_file_exists /srv/admin/data/sentinel/value.txt

./scripts/backup.sh

# --- Simulate branch upgrade: set new git-ref ---
echo "${GITHUB_HEAD_REF:-test-branch}" > /etc/admin-node/git-ref
assert_file_exists /etc/admin-node/git-ref

# --- Re-run Ansible playbook after upgrade ---
echo "=== Running Ansible playbook after branch upgrade ==="
./ci/run-ansible-playbook.sh

# --- Validate after upgrade (services still running) ---
./scripts/validate-apis.sh
./scripts/validate-dns.sh
./scripts/validate-cloudflare-tunnel.sh

# --- Backup after upgrade ---
./scripts/backup.sh

echo "=== upgrade-main-to-branch scenario PASSED ==="
