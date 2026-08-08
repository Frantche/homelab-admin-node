#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
coverage_file="$(mktemp /tmp/admin-node-go-coverage.XXXXXX)"
trap 'rm -f "$coverage_file"' EXIT

cd "$repo_root"
go test -race -coverprofile="$coverage_file" ./... >/dev/null

total="$(go tool cover -func="$coverage_file" | awk '/^total:/ {gsub(/%/, "", $3); print $3}')"
minimum="${GO_COVERAGE_MINIMUM:-50}"
awk -v total="$total" -v minimum="$minimum" 'BEGIN {
  if (total + 0 < minimum + 0) {
    printf "Go coverage %.1f%% is below required %.1f%%\n", total, minimum > "/dev/stderr"
    exit 1
  }
  printf "Go coverage: %.1f%% (minimum %.1f%%)\n", total, minimum
}'
