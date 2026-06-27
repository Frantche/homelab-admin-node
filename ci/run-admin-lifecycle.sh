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

CI_OTEL_MOCK_STATE_DIR="${CI_OTEL_MOCK_STATE_DIR:-/tmp/admin-node-otel-mock-${scenario}}"
export CI_OTEL_MOCK_STATE_DIR
rm -rf "$CI_OTEL_MOCK_STATE_DIR"
mkdir -p "$CI_OTEL_MOCK_STATE_DIR"

python3 ./ci/otel-mock-backend.py --port 43190 --state-dir "$CI_OTEL_MOCK_STATE_DIR" &
otel_mock_pid="$!"
trap 'kill "$otel_mock_pid" 2>/dev/null || true' EXIT

"./ci/scenarios/${scenario}.sh"
