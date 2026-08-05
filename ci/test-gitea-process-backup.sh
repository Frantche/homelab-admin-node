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
  "$test_root/restore-parent/restore"
docker_log="$test_root/docker.log"

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
  "$repo_root/scripts/gitea-process-backup.sh"

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
BACKUP_TMP_FOLDER="$test_root/backup-parent/backup" \
RESTORE_TMP_FOLDER="$test_root/restore-parent/restore" \
  "$repo_root/scripts/gitea-process-backup.sh"

for parent in "$test_root/backup-parent" "$test_root/restore-parent"; do
  if [[ "$(grep -Fxc "$parent:$parent" "$docker_log")" -ne 1 ]]; then
    echo "expected one mount for scratch parent $parent" >&2
    exit 1
  fi
done

echo "Gitea process backup scratch mounts passed"
