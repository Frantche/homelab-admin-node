#!/usr/bin/env bash
set -euo pipefail

assert_file_exists() {
  [[ -f "$1" ]] || { echo "Missing file: $1" >&2; exit 1; }
}

assert_contains() {
  local file="$1" needle="$2"
  grep -q "$needle" "$file" || { echo "Missing '$needle' in $file" >&2; exit 1; }
}
