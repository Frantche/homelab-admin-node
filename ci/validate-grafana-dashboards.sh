#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dashboard_dir="${DASHBOARD_DIR:-$repo_root/stacks/observability/grafana/dashboards}"

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required" >&2
  exit 1
fi

mapfile -t dashboards < <(find "$dashboard_dir" -maxdepth 1 -type f -name '*.json' | sort)
if [[ "${#dashboards[@]}" -eq 0 ]]; then
  echo "no dashboard JSON files found in $dashboard_dir" >&2
  exit 1
fi

for dashboard in "${dashboards[@]}"; do
  jq empty "$dashboard"
done
