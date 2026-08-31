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
    echo "[gitea-process-backup] backup failed: container '$name' health is '$health'" >&2
    exit 1
  fi
}

require_healthy gitea-db
require_healthy gitea

image="${GITEA_PROCESS_BACKUP_IMAGE:-ghcr.io/frantche/gitea-backup-restore-process:0.3.46@sha256:83c1d3422a644e446031dd27174f8584710f004d5eaed40af2544fb4c1bca1a6}"
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

status_root="${BACKUP_STATUS_ROOT:-/srv/admin/backups/status}"
status_path="$status_root/gitea-process.json"
install -d -m 0700 "$status_root"
status_tmp="$(mktemp "$status_root/.gitea-process.XXXXXX.tmp")"
cleanup_status() {
  rm -f -- "$status_tmp"
}
trap cleanup_status EXIT
printf '{"kind":"gitea-process","completed_at":"%s"}\n' "$(date --utc +%Y-%m-%dT%H:%M:%SZ)" > "$status_tmp"
chmod 0600 "$status_tmp"
mv -f -- "$status_tmp" "$status_path"
trap - EXIT
