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

image="${GITEA_PROCESS_BACKUP_IMAGE:-ghcr.io/frantche/gitea-backup-restore-process:0.3.18@sha256:a0bc24193cf2364bde0550ceba5e27ef47148086235e7e0906e11a3ea3dd8975}"
network="${GITEA_PROCESS_BACKUP_NETWORK:-admin-edge}"
backup_tmp="${BACKUP_TMP_FOLDER:-/tmp/backup}"
restore_tmp="${RESTORE_TMP_FOLDER:-/tmp/restore}"

scratch_parent() {
  local path="$1"
  local parent
  if [[ "$path" != /* ]]; then
    echo "[gitea-process-backup] scratch path must be absolute: $path" >&2
    exit 1
  fi
  parent="$(dirname -- "$path")"
  if [[ "$parent" == "/" ]]; then
    echo "[gitea-process-backup] refusing to mount filesystem root for scratch path: $path" >&2
    exit 1
  fi
  printf '%s\n' "$parent"
}

backup_tmp_parent="$(scratch_parent "$backup_tmp")"
restore_tmp_parent="$(scratch_parent "$restore_tmp")"
scratch_mounts=(-v "$backup_tmp_parent:$backup_tmp_parent")
if [[ "$restore_tmp_parent" != "$backup_tmp_parent" ]]; then
  scratch_mounts+=(-v "$restore_tmp_parent:$restore_tmp_parent")
fi

docker run --rm \
  --network "$network" \
  --env-file /srv/admin/env/gitea-process-backup.env \
  -v /srv/admin/data/gitea-stack/gitea:/data:ro \
  "${scratch_mounts[@]}" \
  "$image" \
  gitea-backup
