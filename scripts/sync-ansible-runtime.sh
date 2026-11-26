#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
RUNTIME_DIR="${ADMIN_NODE_ANSIBLE_RUNTIME_DIR:-/var/lib/admin-node/ansible-runtime}"
VENV_DIR="$RUNTIME_DIR/venv"
COLLECTIONS_DIR="$RUNTIME_DIR/collections"

if [[ ! -x "$VENV_DIR/bin/python" ]]; then
  install -d -m 0750 "$RUNTIME_DIR"
  python3 -m venv "$VENV_DIR"
fi

install -d -m 0750 "$COLLECTIONS_DIR"
"$VENV_DIR/bin/python" -m pip install \
  --disable-pip-version-check \
  --upgrade \
  --requirement "$REPO_DIR/ansible/requirements-python.txt"
"$VENV_DIR/bin/ansible-galaxy" collection install \
  --force \
  --requirements-file "$REPO_DIR/ansible/requirements.yml" \
  --collections-path "$COLLECTIONS_DIR"
