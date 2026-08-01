#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
scenario="$repo_root/ci/scenarios/main-to-candidate-disaster-recovery.sh"

while IFS= read -r action; do
  if ! grep -Eq "^[[:space:]]{2}${action}\\)" "$scenario"; then
    echo "Orchestrator references unsupported action: $action" >&2
    exit 1
  fi
done < <("$repo_root/ci/run-disaster-recovery.sh" --list-actions)
