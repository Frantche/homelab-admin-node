#!/usr/bin/env bash
set -euo pipefail

scenario="${1:-fresh-branch}"
case "$scenario" in
  fresh-branch|upgrade-main-to-branch|restore-main-backup-with-branch) ;;
  *) echo "Unknown scenario: $scenario" >&2; exit 1 ;;
esac

SKIP_PUBLIC_URL_VALIDATION=${SKIP_PUBLIC_URL_VALIDATION:-true}
export SKIP_PUBLIC_URL_VALIDATION

"./ci/scenarios/${scenario}.sh"
