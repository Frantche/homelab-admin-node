#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_root="$(mktemp -d /tmp/admin-gitea-process-backup-test.XXXXXX)"

cleanup() {
  rm -rf "$test_root"
}
trap cleanup EXIT

mkdir -p \
  "$test_root/bin" \
  "$test_root/scratch/backup" \
  "$test_root/scratch/restore" \
  "$test_root/backup-parent/backup" \
  "$test_root/restore-parent/restore" \
  "$test_root/history-parent"
docker_log="$test_root/docker.log"
export BACKUP_STATUS_ROOT="$test_root/status"
service_unit="$repo_root/systemd/admin-gitea-process-backup.service"

assert_docker_arg_pair() {
  local first="$1"
  local second="$2"
  if ! awk -v first="$first" -v second="$second" \
    'previous == first && $0 == second { found = 1 } { previous = $0 } END { exit !found }' \
    "$docker_log"; then
    echo "expected Docker arguments '$first' followed by '$second'" >&2
    exit 1
  fi
}

assert_network_count() {
  local expected="$1"
  if [[ "$(grep -Fxc -- "--network" "$docker_log")" -ne "$expected" ]]; then
    echo "expected $expected Docker network attachments" >&2
    exit 1
  fi
}

wait_for_file() {
  local path="$1"
  local _
  for _ in {1..100}; do
    if [[ -e "$path" ]]; then
      return 0
    fi
    sleep 0.01
  done
  echo "timed out waiting for test marker $path" >&2
  return 1
}

test_service_lock_contention() {
  local exec_start lock_file marker output ready holder_pid start_ns elapsed_ns
  local -a service_command

  exec_start="$(sed -n 's/^ExecStart=//p' "$service_unit")"
  read -r -a service_command <<< "$exec_start"
  if [[ "${service_command[*]}" != "/usr/bin/flock --verbose --wait 1800 /run/admin-node-operation.lock /opt/homelab-admin-node/scripts/gitea-process-backup.sh" ]]; then
    echo "Gitea process backup service must wait visibly for the operation lock" >&2
    exit 1
  fi
  if ! grep -Fqx "TimeoutStartSec=45min" "$service_unit"; then
    echo "Gitea process backup service must bound lock wait and execution time" >&2
    exit 1
  fi
  if ! grep -Fqx "OnFailure=admin-backup-failure@%n.service" "$service_unit"; then
    echo "Gitea process backup service must retain failure notification" >&2
    exit 1
  fi

  lock_file="$test_root/operation.lock"
  marker="$test_root/lock-backup-ran"
  ready="$test_root/lock-holder-ready"
  cat > "$test_root/bin/lock-backup" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
touch "${LOCK_BACKUP_MARKER:?}"
EOF
  cat > "$test_root/bin/hold-lock" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
touch "${LOCK_HOLDER_READY:?}"
sleep "${LOCK_HOLDER_SLEEP:?}"
EOF
  chmod +x "$test_root/bin/lock-backup" "$test_root/bin/hold-lock"
  service_command[3]=3
  service_command[4]="$lock_file"
  service_command[5]="$test_root/bin/lock-backup"

  LOCK_HOLDER_READY="$ready" LOCK_HOLDER_SLEEP=0.4 \
    flock "$lock_file" "$test_root/bin/hold-lock" &
  holder_pid=$!
  wait_for_file "$ready"
  start_ns="$(date +%s%N)"
  output="$(LOCK_BACKUP_MARKER="$marker" "${service_command[@]}" 2>&1)"
  elapsed_ns=$(( $(date +%s%N) - start_ns ))
  wait "$holder_pid"
  if [[ ! -e "$marker" || "$elapsed_ns" -lt 250000000 ]]; then
    echo "Gitea process backup did not wait for and acquire the contended lock" >&2
    exit 1
  fi
  if [[ "$output" != *"getting lock took"* ]]; then
    echo "successful Gitea process backup lock wait was not visible" >&2
    exit 1
  fi

  rm -f "$marker" "$ready"
  service_command[3]=0.05
  LOCK_HOLDER_READY="$ready" LOCK_HOLDER_SLEEP=0.4 \
    flock "$lock_file" "$test_root/bin/hold-lock" &
  holder_pid=$!
  wait_for_file "$ready"
  if output="$(LOCK_BACKUP_MARKER="$marker" "${service_command[@]}" 2>&1)"; then
    echo "Gitea process backup must fail when the bounded lock wait expires" >&2
    exit 1
  fi
  wait "$holder_pid"
  if [[ -e "$marker" || "$output" != *"timeout while waiting to get lock"* ]]; then
    echo "Gitea process backup lock timeout was not enforced or visible" >&2
    exit 1
  fi
}

test_service_lock_contention

cat > "$test_root/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "inspect" ]]; then
  printf 'healthy\n'
  exit 0
fi
printf '%s\n' "$@" > "${DOCKER_LOG:?}"
EOF
chmod +x "$test_root/bin/docker"

PATH="$test_root/bin:$PATH" \
DOCKER_LOG="$docker_log" \
BACKUP_TMP_FOLDER="$test_root/scratch/backup" \
RESTORE_TMP_FOLDER="$test_root/scratch/restore" \
BACKUP_FILE_LOG="$test_root/scratch/backupFileLog.txt" \
  "$repo_root/scripts/gitea-process-backup.sh"

assert_docker_arg_pair "--network" "gitea-db"
assert_docker_arg_pair "--network" "admin-edge"
assert_network_count 2

if [[ ! -s "$BACKUP_STATUS_ROOT/gitea-process.json" ]]; then
  echo "successful Gitea process backup did not write its status marker" >&2
  exit 1
fi

scratch_mount="$test_root/scratch:$test_root/scratch"
if [[ "$(grep -Fxc "$scratch_mount" "$docker_log")" -ne 1 ]]; then
  echo "expected exactly one shared scratch parent mount" >&2
  exit 1
fi
if grep -Fqx "$test_root/scratch/backup:$test_root/scratch/backup" "$docker_log"; then
  echo "backup scratch deletion target was mounted directly" >&2
  exit 1
fi
if grep -Fqx "$test_root/scratch/restore:$test_root/scratch/restore" "$docker_log"; then
  echo "restore scratch deletion target was mounted directly" >&2
  exit 1
fi

PATH="$test_root/bin:$PATH" \
DOCKER_LOG="$docker_log" \
GITEA_PROCESS_BACKUP_NETWORK="custom-gitea-db" \
GITEA_PROCESS_BACKUP_EGRESS_NETWORK="custom-egress" \
BACKUP_TMP_FOLDER="$test_root/backup-parent/backup" \
RESTORE_TMP_FOLDER="$test_root/restore-parent/restore" \
BACKUP_FILE_LOG="$test_root/history-parent/backupFileLog.txt" \
  "$repo_root/scripts/gitea-process-backup.sh"

assert_docker_arg_pair "--network" "custom-gitea-db"
assert_docker_arg_pair "--network" "custom-egress"
assert_network_count 2

for parent in "$test_root/backup-parent" "$test_root/restore-parent" "$test_root/history-parent"; do
  if [[ "$(grep -Fxc "$parent:$parent" "$docker_log")" -ne 1 ]]; then
    echo "expected one mount for scratch parent $parent" >&2
    exit 1
  fi
done

PATH="$test_root/bin:$PATH" \
DOCKER_LOG="$docker_log" \
GITEA_PROCESS_BACKUP_NETWORK="shared-network" \
GITEA_PROCESS_BACKUP_EGRESS_NETWORK="shared-network" \
BACKUP_TMP_FOLDER="$test_root/scratch/backup" \
RESTORE_TMP_FOLDER="$test_root/scratch/restore" \
BACKUP_FILE_LOG="$test_root/scratch/backupFileLog.txt" \
  "$repo_root/scripts/gitea-process-backup.sh"

assert_docker_arg_pair "--network" "shared-network"
assert_network_count 1

if ! grep -Fqx \
  "GITEA_PROCESS_BACKUP_NETWORK={{ backup.gitea_process.network | default('gitea-db') }}" \
  "$repo_root/ansible/roles/backup/templates/gitea-process-backup-env.j2"; then
  echo "Ansible template does not use the isolated Gitea database network by default" >&2
  exit 1
fi

if ! grep -Fq \
  'envValue(env, "GITEA_PROCESS_BACKUP_EGRESS_NETWORK", "admin-edge")' \
  "$repo_root/cmd/admin-node/main.go"; then
  echo "Gitea restore fallback does not use the egress network" >&2
  exit 1
fi

for expected in \
  "GITEA_PROCESS_BACKUP_EGRESS_NETWORK={{ backup.gitea_process.egress_network | default('admin-edge') }}" \
  "BACKUP_FILE_LOG={{ backup.gitea_process.backup_file_log | default('/srv/admin/backups/gitea-process/history/backupFileLog.txt') }}" \
  "S3_MULTIPART_ENABLED={{ (backup.gitea_process.s3_multipart_enabled | default(true) | bool) | ternary('true', 'false') }}"; do
  if ! grep -Fqx "$expected" "$repo_root/ansible/roles/backup/templates/gitea-process-backup-env.j2"; then
    echo "Ansible template is missing runtime default: $expected" >&2
    exit 1
  fi
done

if grep -Fqx "/srv/admin/data/gitea-stack/gitea:/data" "$docker_log"; then
  echo "Gitea data must remain read-only during backup" >&2
  exit 1
fi

cat > "$test_root/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "inspect" ]]; then
  printf 'unhealthy\n'
  exit 0
fi
exit 0
EOF
chmod +x "$test_root/bin/docker"
if PATH="$test_root/bin:$PATH" \
  BACKUP_TMP_FOLDER="$test_root/scratch/backup" \
  RESTORE_TMP_FOLDER="$test_root/scratch/restore" \
  BACKUP_FILE_LOG="$test_root/scratch/backupFileLog.txt" \
  "$repo_root/scripts/gitea-process-backup.sh" >/dev/null 2>&1; then
  echo "unhealthy Gitea containers must fail the backup service" >&2
  exit 1
fi

echo "Gitea process backup networks, multipart, history, and scratch mounts passed"
