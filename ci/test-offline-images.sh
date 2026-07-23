#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE="${ADMIN_NODE_OFFLINE_TEST_IMAGE:-busybox:latest}"
REAL_DOCKER="${REAL_DOCKER:-$(command -v docker)}"

if [[ -z "$REAL_DOCKER" || ! -x "$REAL_DOCKER" ]]; then
  echo "[offline-images] docker is required" >&2
  exit 1
fi

tmp_dir="$(mktemp -d /tmp/admin-offline-images.XXXXXX)"
cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

admin_root="$tmp_dir/admin"
backup_root="$tmp_dir/backups"
mode_file="$tmp_dir/mode"
fake_bin="$tmp_dir/bin"
fake_docker="$fake_bin/docker"
fake_restic="$fake_bin/restic"
fake_systemctl="$fake_bin/systemctl"
repo_root="$tmp_dir/repo"
backup_env="$tmp_dir/backup.env"

mkdir -p "$admin_root/stacks/test" "$admin_root/env" "$admin_root/data/gitea" "$fake_bin" "$repo_root" "$backup_root"
printf 'normal\n' > "$mode_file"
printf 'services:\n  app:\n    image: %s\n' "$IMAGE" > "$admin_root/stacks/test/compose.yaml"
printf 'GITEA_ADMIN_PASSWORD=test\n' > "$admin_root/env/gitea.env"
printf 'gitea-data\n' > "$admin_root/data/gitea/value.txt"
cat > "$backup_env" <<EOF
RESTIC_REPOSITORY="$tmp_dir/restic-repo"
RESTIC_PASSWORD="secret"
RESTIC_INIT_REPOSITORIES="true"
RESTIC_DEFAULT_FORGET_ARGS="none"
EOF

cat > "$fake_docker" <<EOF
#!/usr/bin/env bash
set -euo pipefail
real_docker="$REAL_DOCKER"
image="$IMAGE"

if [[ "\${1:-}" == "compose" && "\${*: -2}" == "config --images" ]]; then
  echo "\$image"
  exit 0
fi
if [[ "\${1:-}" == "compose" ]]; then
  exit 0
fi
if [[ "\${1:-}" == "exec" && "\${2:-}" == "keycloak-db" && "\${3:-}" == "pg_dump" ]]; then
  echo "keycloak-dump"
  exit 0
fi
if [[ "\${1:-}" == "exec" && "\${2:-}" == "keycloak-db" && "\${3:-}" == "pg_isready" ]]; then
  exit 0
fi
if [[ "\${1:-}" == "exec" && "\${2:-}" == "keycloak-db" && "\${3:-}" == "psql" ]]; then
  cat >/dev/null
  exit 0
fi
if [[ "\${1:-}" == "exec" && "\${2:-}" == "-i" && "\${3:-}" == "keycloak-db" && "\${4:-}" == "psql" ]]; then
  cat >/dev/null
  exit 0
fi
if [[ "\${1:-}" == "exec" && "\${2:-}" == "-i" && "\${3:-}" == "keycloak-db" && "\${4:-}" == "pg_restore" ]]; then
  cat >/dev/null
  exit 0
fi
if [[ "\${1:-}" == "ps" && "\${2:-}" == "--format" ]]; then
  exit 0
fi
if [[ "\${1:-}" == "inspect" ]]; then
  echo "true healthy"
  exit 0
fi
if [[ "\${1:-}" == "exec" ]]; then
  exit 0
fi
if [[ "\${1:-}" == "save" || "\${1:-}" == "load" || "\${1:-}" == "pull" || "\${1:-}" == "image" ]]; then
  exec "\$real_docker" "\$@"
fi
echo "[offline-images] unexpected docker args: \$*" >&2
exit 1
EOF
chmod +x "$fake_docker"

cat > "$fake_restic" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$*" == "cat config" ]]; then
  exit 1
fi
exit 0
EOF
chmod +x "$fake_restic"

cat > "$fake_systemctl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in
  is-active|is-enabled) exit 1 ;;
  *) exit 0 ;;
esac
EOF
chmod +x "$fake_systemctl"

echo "[offline-images] pulling $IMAGE"
"$REAL_DOCKER" pull "$IMAGE" >/dev/null

PATH="$fake_bin:$PATH" \
ADMIN_NODE_REPO_ROOT="$repo_root" \
ADMIN_NODE_ROOT="$admin_root" \
ADMIN_BACKUP_ROOT="$backup_root" \
ADMIN_MODE_FILE="$mode_file" \
ADMIN_OPERATION_LOCK="$tmp_dir/operation.lock" \
GITEA_STACK_PATH="$admin_root/data/gitea-stack" \
RESTIC_BACKUP_ENV_FILE="$backup_env" \
ADMIN_NODE_VALIDATE_MOCK_ALL=true \
  "$REPO_ROOT/bin/admin-node" backup run --include-images >/dev/null

backup_id="$(find "$backup_root" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | sort | tail -1)"
backup_dir="$backup_root/$backup_id"
offline_tar="$backup_dir/offline-images.tar"
if [[ ! -s "$offline_tar" ]]; then
  echo "[offline-images] expected non-empty offline image archive: $offline_tar" >&2
  exit 1
fi

echo "[offline-images] removing local image $IMAGE"
"$REAL_DOCKER" image rm "$IMAGE" >/dev/null 2>&1 || true
if "$REAL_DOCKER" image inspect "$IMAGE" >/dev/null 2>&1; then
  echo "[offline-images] image still present after remove: $IMAGE" >&2
  exit 1
fi

printf 'restore\n' > "$mode_file"

PATH="$fake_bin:$PATH" \
ADMIN_NODE_REPO_ROOT="$repo_root" \
ADMIN_NODE_ROOT="$admin_root" \
ADMIN_BACKUP_ROOT="$backup_root" \
ADMIN_MODE_FILE="$mode_file" \
ADMIN_OPERATION_LOCK="$tmp_dir/operation.lock" \
GITEA_STACK_PATH="$admin_root/data/gitea-stack" \
RESTIC_BACKUP_ENV_FILE="$backup_env" \
ADMIN_NODE_VALIDATE_MOCK_ALL=true \
  "$REPO_ROOT/bin/admin-node" restore run --id "$backup_id" >/dev/null

if ! "$REAL_DOCKER" image inspect "$IMAGE" >/dev/null 2>&1; then
  echo "[offline-images] image was not restored by docker load: $IMAGE" >&2
  exit 1
fi

echo "[offline-images] offline image backup and restore passed for $IMAGE"
