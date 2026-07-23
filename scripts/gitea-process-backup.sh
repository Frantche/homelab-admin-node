#!/usr/bin/env bash
set -euo pipefail

container_health() {
  docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}missing{{end}}' "$1" 2>/dev/null || true
}

require_healthy() {
  local name="$1"
  local health
  health="$(container_health "$name")"
  if [[ "$health" != "healthy" ]]; then
    echo "[gitea-process-backup] skipping backup: container '$name' health is '$health'" >&2
    exit 0
  fi
}

require_healthy gitea-db
require_healthy gitea

image="${GITEA_PROCESS_BACKUP_IMAGE:-ghcr.io/frantche/gitea-backup-restore-process:0.3.6}"
network="${GITEA_PROCESS_BACKUP_NETWORK:-admin-edge}"
backup_tmp="${BACKUP_TMP_FOLDER:-/tmp/backup}"
restore_tmp="${RESTORE_TMP_FOLDER:-/tmp/restore}"

docker run --rm \
  --network "$network" \
  --env-file /srv/admin/env/gitea-process-backup.env \
  -v /srv/admin/data/gitea-stack/gitea:/data:ro \
  -v "$backup_tmp:$backup_tmp" \
  -v "$restore_tmp:$restore_tmp" \
  "$image" \
  gitea-backup
