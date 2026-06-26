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

# --- Start in init mode ---
./bin/admin-node mode set init
assert_contains /etc/admin-node/mode "init"

# --- Deploy via Ansible playbook with config repo (init mode) ---
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

# --- Create data and backup ---
./bin/admin-node ci create-sentinel
assert_file_exists /srv/admin/data/sentinel/value.txt

./bin/admin-node backup run

# --- Simulate branch upgrade: set new git-ref ---
echo "${GITHUB_HEAD_REF:-test-branch}" > /etc/admin-node/git-ref
assert_file_exists /etc/admin-node/git-ref

# --- Re-run Ansible playbook after upgrade ---
echo "=== Running Ansible playbook after branch upgrade ==="
ansible-playbook \
  -i "$CI_INVENTORY" \
  "$REPO_ROOT/ansible/site.yml" \
  --extra-vars "{\"openbao\": {\"root_token\": \"${OPENBAO_TOKEN}\"}}"

# --- Backup after upgrade ---
./bin/admin-node backup run

echo "=== upgrade-main-to-branch scenario PASSED ==="
