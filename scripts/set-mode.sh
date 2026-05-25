#!/usr/bin/env bash
set -euo pipefail

MODE_FILE=/etc/admin-node/mode
VALID_MODES=(locked init normal restore restore_failed)

if [[ $# -ne 1 ]]; then
  echo "Usage: $0 <locked|init|normal|restore|restore_failed>" >&2
  exit 1
fi

NEW_MODE="$1"
if [[ ! " ${VALID_MODES[*]} " =~ " ${NEW_MODE} " ]]; then
  echo "Invalid mode: $NEW_MODE" >&2
  exit 1
fi

install -d -m 0755 /etc/admin-node
echo "$NEW_MODE" > "$MODE_FILE"
echo "Mode set to $NEW_MODE"
