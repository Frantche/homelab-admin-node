#!/usr/bin/env bash
set -euo pipefail

# Bootstrap user journey scenario — runs inside the VM after cloud-init completes.
# The repository is already cloned to /opt/homelab-admin-node by cloud-init.
# /etc/admin-config/homelab-node-admin-config must be populated before this script runs.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$REPO_ROOT/ci/assertions.sh"

export CI_MOCK_PIHOLE="${CI_MOCK_PIHOLE:-true}"
export CI_MOCK_CLOUDFLARE_TUNNEL="${CI_MOCK_CLOUDFLARE_TUNNEL:-true}"
export CI_SKIP_PUBLIC_URL_VALIDATION="${CI_SKIP_PUBLIC_URL_VALIDATION:-true}"
export SKIP_PUBLIC_URL_VALIDATION="${SKIP_PUBLIC_URL_VALIDATION:-true}"

# --- CI prerequisites (TLS certs, /etc/hosts, ansible collections) ---
"$REPO_ROOT/ci/setup-ci-env.sh"

# Prevent the auto-converge timer from interfering with our manual operations
systemctl stop admin-converge.timer admin-converge.service 2>/dev/null || true

# --- Set mode to init via adminctl ---
"$REPO_ROOT/scripts/adminctl" set-mode init
assert_contains /etc/admin-node/mode "init"

# --- Run convergence via adminctl converge (init mode: starts services) ---
echo "=== Running convergence (init mode) via adminctl ==="
ADMIN_CONVERGE_SKIP_GIT_PULL=true "$REPO_ROOT/scripts/adminctl" converge

# --- Initialize and unseal OpenBao ---
"$REPO_ROOT/ci/init-openbao-ci.sh"
OPENBAO_TOKEN="$(cat "$REPO_ROOT/secrets/openbao-root-token")"
export OPENBAO_TOKEN

# Inject the root token into the mock config repo so the normal-mode playbook can use it
"$REPO_ROOT/ci/update-openbao-token.py"

# --- Set mode to normal via adminctl ---
"$REPO_ROOT/scripts/adminctl" set-mode normal
assert_contains /etc/admin-node/mode "normal"

# --- Run convergence via adminctl converge (normal mode: validate + backup) ---
echo "=== Running convergence (normal mode) via adminctl ==="
ADMIN_CONVERGE_SKIP_GIT_PULL=true "$REPO_ROOT/scripts/adminctl" converge

# --- Verify final mode is normal ---
assert_contains /etc/admin-node/mode "normal"

# --- Verify Docker Compose services ---
echo "=== Verifying Docker Compose services ==="
for svc in traefik keycloak openbao harbor-core; do
  if ! docker ps --filter "name=^${svc}$" --filter "status=running" --format '{{.Names}}' | grep -q "^${svc}$"; then
    echo "ERROR: Service ${svc} is not running" >&2
    docker ps -a
    exit 1
  fi
  echo "Service ${svc} is running"
done

# --- Minimal backup/restore ---
echo "=== Running backup ==="
"$REPO_ROOT/ci/create-sentinel-data.sh"
assert_file_exists /srv/admin/data/sentinel/value.txt

"$REPO_ROOT/scripts/backup.sh"
BACKUP_COUNT="$(find /srv/admin/backups/local -mindepth 1 -maxdepth 1 -type d | wc -l)"
if [[ "$BACKUP_COUNT" -lt 1 ]]; then
  echo "ERROR: Expected at least 1 backup directory, found $BACKUP_COUNT" >&2
  exit 1
fi

echo "=== Running restore ==="
"$REPO_ROOT/scripts/adminctl" set-mode restore
assert_contains /etc/admin-node/mode "restore"

"$REPO_ROOT/scripts/restore.sh"
assert_contains /etc/admin-node/mode "normal"

echo "=== bootstrap-user-journey scenario PASSED ==="
