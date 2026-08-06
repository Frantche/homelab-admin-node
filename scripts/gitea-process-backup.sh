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

image="${GITEA_PROCESS_BACKUP_IMAGE:-ghcr.io/frantche/gitea-backup-restore-process:0.3.21@sha256:fa400b039e740d1a84d3bb9af76a1d69276f1abfaf995db06d10dc813954ec52}"
database_network="${GITEA_PROCESS_BACKUP_NETWORK:-gitea-db}"
egress_network="${GITEA_PROCESS_BACKUP_EGRESS_NETWORK:-admin-edge}"
backup_tmp="${BACKUP_TMP_FOLDER:-/tmp/backup}"
restore_tmp="${RESTORE_TMP_FOLDER:-/tmp/restore}"
backup_file_log="${BACKUP_FILE_LOG:-/srv/admin/backups/gitea-process/history/backupFileLog.txt}"

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

scratch_mounts=()
scratch_parents=()
add_scratch_mount() {
  local path="$1"
  local parent
  local existing
  parent="$(scratch_parent "$path")"
  for existing in "${scratch_parents[@]}"; do
    if [[ "$existing" == "$parent" ]]; then
      return
    fi
  done
  scratch_parents+=("$parent")
  scratch_mounts+=(-v "$parent:$parent")
}

add_scratch_mount "$backup_tmp"
add_scratch_mount "$restore_tmp"
add_scratch_mount "$backup_file_log"

network_args=(--network "$database_network")
if [[ "$egress_network" != "$database_network" ]]; then
  network_args+=(--network "$egress_network")
fi

docker run --rm \
  "${network_args[@]}" \
  --env-file /srv/admin/env/gitea-process-backup.env \
  -v /srv/admin/data/gitea-stack/gitea:/data:ro \
  "${scratch_mounts[@]}" \
  "$image" \
  gitea-backup
