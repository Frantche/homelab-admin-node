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
export CI_OTEL_MOCK_STATE_DIR="${CI_OTEL_MOCK_STATE_DIR:-/tmp/admin-node-otel-mock-bootstrap-user-journey}"

otel_mock_pid=""

stop_auto_converge() {
  systemctl disable --now admin-converge.timer admin-unlock.path 2>/dev/null || true
  systemctl stop admin-converge.service 2>/dev/null || true
}

stop_otel_mock() {
  if [[ -n "$otel_mock_pid" ]]; then
    kill "$otel_mock_pid" 2>/dev/null || true
  fi
}

start_otel_mock() {
  rm -rf "$CI_OTEL_MOCK_STATE_DIR"
  mkdir -p "$CI_OTEL_MOCK_STATE_DIR"
  python3 "$REPO_ROOT/ci/otel-mock-backend.py" --port 43190 --state-dir "$CI_OTEL_MOCK_STATE_DIR" &
  otel_mock_pid="$!"
}

dump_debug() {
  local status=$?
  echo "=== bootstrap-user-journey debug: status=$status ===" >&2
  echo "--- admin mode ---" >&2
  cat /etc/admin-node/mode >&2 2>/dev/null || true
  echo "--- admin-converge units ---" >&2
  systemctl --no-pager --full status admin-converge.service admin-converge.timer admin-unlock.path >&2 2>/dev/null || true
  echo "--- admin-converge journal ---" >&2
  journalctl -u admin-converge.service --no-pager -n 80 >&2 2>/dev/null || true
  echo "--- docker ps ---" >&2
  docker ps -a >&2 2>/dev/null || true
  for svc in traefik keycloak keycloak-db openbao harbor-core harbor-db gitea gitea-db cloudflared otel-collector; do
    echo "--- docker logs: $svc ---" >&2
    docker logs "$svc" 2>&1 | tail -80 >&2 || true
  done
  exit "$status"
}

run_converge() {
  local output_file status
  output_file="$(mktemp)"
  set +e
  ADMIN_CONVERGE_SKIP_GIT_PULL=true \
    ANSIBLE_EXTRA_ARGS="-e admin_ci_disable_auto_converge=true" \
    "$REPO_ROOT/bin/admin-node" converge run 2>&1 | tee "$output_file"
  status="${PIPESTATUS[0]}"
  set -e
  if [[ "$status" -ne 0 ]]; then
    rm -f "$output_file"
    return 1
  fi
  if grep -q "another run is in progress" "$output_file"; then
    rm -f "$output_file"
    echo "ERROR: admin-converge lock was already held during a manual CI convergence" >&2
    return 1
  fi
  rm -f "$output_file"
}

run_oidc_user_journey() {
  echo "=== Running OIDC user journey via Playwright ==="
  if command -v pacman >/dev/null 2>&1; then
    pacman -Sy --noconfirm --needed nodejs npm chromium nss
  fi
  if [[ ! -f /srv/admin/certs/ca.pem ]]; then
    echo "ERROR: Expected local CA at /srv/admin/certs/ca.pem" >&2
    return 1
  fi
  pushd "$REPO_ROOT/ci/oidc-user-journey" >/dev/null
  if [[ -f package-lock.json ]]; then
    npm ci --no-audit --no-fund
  else
    npm install --no-audit --no-fund
  fi
  CI=true \
    NODE_EXTRA_CA_CERTS=/srv/admin/certs/ca.pem \
    SSL_CERT_FILE=/srv/admin/certs/ca.pem \
    PLAYWRIGHT_CHROMIUM_EXECUTABLE="${PLAYWRIGHT_CHROMIUM_EXECUTABLE:-/usr/bin/chromium}" \
    OIDC_TEST_USERNAME="${OIDC_TEST_USERNAME:-ci-sso-user}" \
    OIDC_TEST_PASSWORD="${OIDC_TEST_PASSWORD:-ci-sso-user-password}" \
    npm test
  popd >/dev/null
}

trap dump_debug ERR
trap stop_otel_mock EXIT

# --- CI prerequisites (TLS certs, /etc/hosts, ansible collections) ---
"$REPO_ROOT/ci/setup-ci-env.sh"
"$REPO_ROOT/scripts/build-admin-node.sh"
start_otel_mock

# Prevent the auto-converge timer from interfering with our manual operations
stop_auto_converge

# --- Set mode to init via admin-node ---
"$REPO_ROOT/bin/admin-node" mode set init
assert_contains /etc/admin-node/mode "init"

# --- Run convergence via admin-node (init mode: starts services) ---
echo "=== Running convergence (init mode) via admin-node ==="
run_converge
stop_auto_converge

# --- Initialize and unseal OpenBao ---
"$REPO_ROOT/bin/admin-node" ci init-openbao
OPENBAO_TOKEN="$(cat "$REPO_ROOT/secrets/openbao-root-token")"
export OPENBAO_TOKEN

# Inject the root token into the mock config repo so the normal-mode playbook can use it
"$REPO_ROOT/bin/admin-node" ci update-openbao-token

# --- Set mode to normal via admin-node ---
stop_auto_converge
"$REPO_ROOT/bin/admin-node" mode set normal
assert_contains /etc/admin-node/mode "normal"
stop_auto_converge

# --- Run convergence via admin-node (normal mode: validate + backup) ---
echo "=== Running convergence (normal mode) via admin-node ==="
run_converge
stop_auto_converge
CI_OTEL_MOCK_STATE_DIR= "$REPO_ROOT/bin/admin-node" validate observability

# --- Verify final mode is normal ---
assert_contains /etc/admin-node/mode "normal"

# --- Verify Docker Compose services ---
echo "=== Verifying Docker Compose services ==="
for svc in traefik keycloak openbao harbor-core gitea; do
  if ! docker ps --filter "name=^${svc}$" --filter "status=running" --format '{{.Names}}' | grep -q "^${svc}$"; then
    echo "ERROR: Service ${svc} is not running" >&2
    docker ps -a
    exit 1
  fi
  echo "Service ${svc} is running"
done

# --- Validate real OIDC browser login ---
run_oidc_user_journey

# --- Minimal backup/restore ---
echo "=== Running backup ==="
"$REPO_ROOT/bin/admin-node" ci create-sentinel
assert_file_exists /srv/admin/data/sentinel/value.txt

"$REPO_ROOT/bin/admin-node" backup run
BACKUP_COUNT="$(find /srv/admin/backups/local -mindepth 1 -maxdepth 1 -type d | wc -l)"
if [[ "$BACKUP_COUNT" -lt 1 ]]; then
  echo "ERROR: Expected at least 1 backup directory, found $BACKUP_COUNT" >&2
  exit 1
fi

echo "=== Running restore ==="
stop_auto_converge
"$REPO_ROOT/bin/admin-node" mode set restore
assert_contains /etc/admin-node/mode "restore"
stop_auto_converge

"$REPO_ROOT/bin/admin-node" restore run
assert_contains /etc/admin-node/mode "normal"

echo "=== bootstrap-user-journey scenario PASSED ==="
