#!/usr/bin/env bash
set -euo pipefail

scenario="${1:-fresh-branch}"
case "$scenario" in
  fresh-branch|upgrade-main-to-branch|restore-main-backup-with-branch) ;;
  *) echo "Unknown scenario: $scenario" >&2; exit 1 ;;
esac

# Change to the repo root (supports running from anywhere, e.g. inside a VM)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

SKIP_PUBLIC_URL_VALIDATION=${SKIP_PUBLIC_URL_VALIDATION:-true}
export SKIP_PUBLIC_URL_VALIDATION

"./ci/scenarios/${scenario}.sh"
