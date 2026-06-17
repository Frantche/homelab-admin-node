#!/usr/bin/env bash
set -euo pipefail

# Install the CI mock config repo into the same layout used by admin-converge.
# This simulates a private config repo populated by an admin after cloud-init.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MOCK_SRC="$SCRIPT_DIR/mock-config-repo"
CONFIG_REPO_DIR="${CONFIG_REPO_DIR:-/etc/admin-config/homelab-node-admin-config}"

if [[ ! -f "$MOCK_SRC/hosts" || ! -f "$MOCK_SRC/group_vars/all.yml" ]]; then
  echo "[setup-ci-config-repo] invalid mock config source: $MOCK_SRC" >&2
  exit 1
fi

echo "[setup-ci-config-repo] installing mock config repo to $CONFIG_REPO_DIR"

mkdir -p "$CONFIG_REPO_DIR/hosts/group_vars"
cp "$MOCK_SRC/hosts" "$CONFIG_REPO_DIR/hosts/inventory.ini"
cp "$MOCK_SRC/group_vars/all.yml" "$CONFIG_REPO_DIR/hosts/group_vars/all.yml"

if command -v git >/dev/null 2>&1; then
  git -C "$CONFIG_REPO_DIR" init >/dev/null
  git -C "$CONFIG_REPO_DIR" checkout -B main >/dev/null
  git -C "$CONFIG_REPO_DIR" add hosts/inventory.ini hosts/group_vars/all.yml
  git -C "$CONFIG_REPO_DIR" \
    -c user.name='CI Admin' \
    -c user.email='ci@example.com' \
    commit -m 'Initial CI admin config' >/dev/null || true
fi

echo "[setup-ci-config-repo] mock config repo ready at $CONFIG_REPO_DIR"
