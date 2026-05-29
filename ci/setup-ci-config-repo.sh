#!/usr/bin/env bash
set -euo pipefail

# Install the CI mock config repo into /etc/admin-config.
# This simulates a real private config repo without requiring git authentication
# or SOPS encryption, so the CI can test the config-repo-based ansible-playbook flow.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MOCK_SRC="$SCRIPT_DIR/mock-config-repo"
CONFIG_REPO_DIR="${CONFIG_REPO_DIR:-/etc/admin-config}"

echo "[setup-ci-config-repo] installing mock config repo to $CONFIG_REPO_DIR"

mkdir -p "$CONFIG_REPO_DIR"

cp -r "$MOCK_SRC/." "$CONFIG_REPO_DIR/"

echo "[setup-ci-config-repo] mock config repo ready at $CONFIG_REPO_DIR"
