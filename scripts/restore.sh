#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ADMIN_NODE_BIN="${ADMIN_NODE_BIN:-$REPO_ROOT/bin/admin-node}"

if [[ ! -x "$ADMIN_NODE_BIN" ]]; then
  echo "Missing admin-node binary at $ADMIN_NODE_BIN. Run: make build-admin-node" >&2
  exit 127
fi

exec "$ADMIN_NODE_BIN" restore run "$@"
