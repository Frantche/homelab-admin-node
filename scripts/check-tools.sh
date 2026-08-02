#!/usr/bin/env bash
set -euo pipefail

missing=()
for tool in "$@"; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    missing+=("$tool")
  fi
done

if ((${#missing[@]} > 0)); then
  printf 'Missing required tools: %s\n' "${missing[*]}" >&2
  exit 1
fi
