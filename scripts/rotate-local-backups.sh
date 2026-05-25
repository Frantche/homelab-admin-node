#!/usr/bin/env bash
set -euo pipefail

BACKUP_ROOT=/srv/admin/backups/local
mkdir -p "$BACKUP_ROOT"
ls -1dt "$BACKUP_ROOT"/* 2>/dev/null | tail -n +4 | xargs -r rm -rf
