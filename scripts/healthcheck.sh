#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
"$REPO_ROOT/scripts/build-admin-node.sh" >/dev/null
"$REPO_ROOT/bin/admin-node" validate apis
"$REPO_ROOT/bin/admin-node" validate dns
"$REPO_ROOT/bin/admin-node" validate tunnel
