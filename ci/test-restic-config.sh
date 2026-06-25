#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "[restic-config] missing required command: $cmd" >&2
    exit 1
  fi
}

require_cmd restic
require_cmd ssh
require_cmd sshd
require_cmd ssh-keygen

tmp_dir="$(mktemp -d /tmp/admin-restic-ci.XXXXXX)"
sshd_pid_file="$tmp_dir/sshd.pid"
sshd_log="$tmp_dir/sshd.log"

cleanup() {
  if [[ -f "$sshd_pid_file" ]]; then
    kill "$(cat "$sshd_pid_file")" >/dev/null 2>&1 || true
  fi
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

mkdir -p "$tmp_dir/source" "$tmp_dir/local-repo" "$tmp_dir/sftp-repo" "$tmp_dir/home/.ssh"
printf 'backup-data\n' > "$tmp_dir/source/value.txt"

ssh-keygen -q -t ed25519 -N '' -f "$tmp_dir/client_key"
ssh-keygen -q -t ed25519 -N '' -f "$tmp_dir/ssh_host_ed25519_key"
cp "$tmp_dir/client_key.pub" "$tmp_dir/authorized_keys"
chmod 0700 "$tmp_dir/home/.ssh"
chmod 0600 "$tmp_dir/client_key" "$tmp_dir/authorized_keys"

port=22222
cat > "$tmp_dir/sshd_config" <<EOF
Port $port
ListenAddress 127.0.0.1
HostKey $tmp_dir/ssh_host_ed25519_key
PidFile $sshd_pid_file
AuthorizedKeysFile $tmp_dir/authorized_keys
PasswordAuthentication no
PermitRootLogin yes
PubkeyAuthentication yes
StrictModes no
Subsystem sftp internal-sftp
LogLevel ERROR
EOF

cat > "$tmp_dir/home/.ssh/config" <<EOF
Host restic-ci
  HostName 127.0.0.1
  Port $port
  User $(id -un)
  IdentityFile $tmp_dir/client_key
  StrictHostKeyChecking no
  UserKnownHostsFile $tmp_dir/home/.ssh/known_hosts
  IdentitiesOnly yes
EOF
chmod 0600 "$tmp_dir/home/.ssh/config"

sftp_enabled=true
if ! mkdir -p /run/sshd 2>/dev/null; then
  sftp_enabled=false
  echo "[restic-config] skipping SFTP runtime check: cannot create /run/sshd" >&2
fi

repo_list="local"
if [[ "$sftp_enabled" == "true" ]]; then
  sshd -f "$tmp_dir/sshd_config" -E "$sshd_log"

  for _ in $(seq 1 30); do
    if HOME="$tmp_dir/home" ssh -F "$tmp_dir/home/.ssh/config" restic-ci true >/dev/null 2>&1; then
      break
    fi
    sleep 1
  done
  HOME="$tmp_dir/home" ssh -F "$tmp_dir/home/.ssh/config" restic-ci true >/dev/null
  repo_list="local sftp"
fi

cat > "$tmp_dir/backup.env" <<EOF
RESTIC_REPOSITORIES="$repo_list"
RESTIC_INIT_REPOSITORIES="true"
RESTIC_DEFAULT_FORGET_ARGS="--keep-last 2 --prune"
RESTIC_REQUIRE_SECURE_REPOSITORIES="true"
RESTIC_REPOSITORY_LOCAL="$tmp_dir/local-repo"
RESTIC_PASSWORD_LOCAL="ci-local-restic-password"
EOF

if [[ "$sftp_enabled" == "true" ]]; then
  cat >> "$tmp_dir/backup.env" <<EOF
RESTIC_REPOSITORY_SFTP="sftp:restic-ci:$tmp_dir/sftp-repo"
RESTIC_PASSWORD_SFTP="ci-sftp-restic-password"
EOF
fi

HOME="$tmp_dir/home" \
RESTIC_BACKUP_ENV_FILE="$tmp_dir/backup.env" \
RESTIC_BACKUP_PATHS="$tmp_dir/source" \
  "$REPO_ROOT/scripts/restic-backup-repositories.sh"

RESTIC_REPOSITORY="$tmp_dir/local-repo" RESTIC_PASSWORD="ci-local-restic-password" restic snapshots >/dev/null
if [[ "$sftp_enabled" == "true" ]]; then
  HOME="$tmp_dir/home" RESTIC_REPOSITORY="sftp:restic-ci:$tmp_dir/sftp-repo" RESTIC_PASSWORD="ci-sftp-restic-password" restic snapshots >/dev/null
fi

cat > "$tmp_dir/insecure.env" <<EOF
RESTIC_REPOSITORIES="bad"
RESTIC_REPOSITORY_BAD="ftp://127.0.0.1/restic"
RESTIC_PASSWORD_BAD="ci-password"
EOF

if RESTIC_BACKUP_ENV_FILE="$tmp_dir/insecure.env" RESTIC_BACKUP_PATHS="$tmp_dir/source" "$REPO_ROOT/scripts/restic-backup-repositories.sh" >/dev/null 2>&1; then
  echo "[restic-config] expected ftp:// repository to be rejected" >&2
  exit 1
fi

if [[ "$sftp_enabled" == "true" ]]; then
  echo "[restic-config] local and SFTP restic configuration passed"
else
  echo "[restic-config] local restic configuration passed; SFTP runtime check skipped"
fi
