#!/usr/bin/env bash
set -euo pipefail

MODE_FILE=/etc/admin-node/mode

if [[ $# -ne 1 ]]; then
  echo "Usage: $0 <locked|init|normal|restore|restore_failed>" >&2
  exit 1
fi

NEW_MODE="$1"
case "$NEW_MODE" in
  locked|init|normal|restore|restore_failed) ;;
  *)
    echo "Invalid mode: $NEW_MODE" >&2
    exit 1
    ;;
esac

install -d -m 0755 /etc/admin-node
echo "$NEW_MODE" > "$MODE_FILE"
echo "Mode set to $NEW_MODE"
