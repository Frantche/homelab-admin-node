#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
"$REPO_ROOT/scripts/build-admin-node.sh" >/dev/null
exec "$REPO_ROOT/bin/admin-node" ci install-mock-config-repo "$@"
