#!/usr/bin/env bash
set -euo pipefail

BACKUP_ROOT=/srv/admin/backups/local
mkdir -p "$BACKUP_ROOT"
mapfile -t old_backups < <(find "$BACKUP_ROOT" -mindepth 1 -maxdepth 1 -type d -printf '%T@ %p\n' | sort -nr | awk 'NR>3 {print $2}')
if ((${#old_backups[@]})); then
  rm -rf "${old_backups[@]}"
fi
