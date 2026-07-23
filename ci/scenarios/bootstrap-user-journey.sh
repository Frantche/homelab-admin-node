#!/usr/bin/env bash
set -euo pipefail

# Bootstrap user journey scenario — runs inside the VM after cloud-init completes.
# The repository is already cloned to /opt/homelab-admin-node by cloud-init.
# /etc/admin-config/homelab-node-admin-config must be populated before this script runs.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$REPO_ROOT/ci/assertions.sh"

CONFIG_REPO_DIR="${CONFIG_REPO_DIR:-/etc/admin-config/homelab-node-admin-config}"
CI_BOOTSTRAP_VARS="$CONFIG_REPO_DIR/hosts/group_vars/ci-bootstrap-vars.yml"

export CI_MOCK_PIHOLE="${CI_MOCK_PIHOLE:-true}"
export CI_MOCK_CLOUDFLARE_TUNNEL="${CI_MOCK_CLOUDFLARE_TUNNEL:-true}"
export CI_SKIP_PUBLIC_URL_VALIDATION="${CI_SKIP_PUBLIC_URL_VALIDATION:-true}"
export SKIP_PUBLIC_URL_VALIDATION="${SKIP_PUBLIC_URL_VALIDATION:-true}"
export CI_OTEL_MOCK_STATE_DIR="${CI_OTEL_MOCK_STATE_DIR:-/tmp/admin-node-otel-mock-bootstrap-user-journey}"

stop_auto_converge() {
  systemctl disable --now admin-converge.timer admin-unlock.path 2>/dev/null || true
  systemctl stop admin-converge.service 2>/dev/null || true
}

stop_otel_mock() {
  docker rm -f "${CI_OTEL_MOCK_CONTAINER_NAME:-otel-mock-backend}" >/dev/null 2>&1 || true
}

start_otel_mock() {
  rm -rf "$CI_OTEL_MOCK_STATE_DIR"
  mkdir -p "$CI_OTEL_MOCK_STATE_DIR"
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
  local extra_args="${1:-}"
  local validate_mock_all="${2:-false}"
  local output_file status
  output_file="$(mktemp)"
  set +e
  ADMIN_CONVERGE_SKIP_GIT_PULL=true \
    ADMIN_NODE_VALIDATE_MOCK_ALL="$validate_mock_all" \
    ANSIBLE_EXTRA_ARGS="-e @$CI_BOOTSTRAP_VARS ${extra_args}" \
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

verify_btrfs_storage_isolation() {
  echo "=== Verifying Btrfs storage isolation ==="
  if [[ "$(findmnt -no FSTYPE -T /srv/admin)" != "btrfs" ]]; then
    echo "ERROR: /srv/admin is not on a Btrfs filesystem" >&2
    findmnt -T /srv/admin >&2
    exit 1
  fi

  local paths=(
    /srv/admin/data/keycloak
    /srv/admin/data/openbao
    /srv/admin/data/gitea-stack
    /srv/admin/data/harbor
    /srv/admin/data/traefik
    /srv/admin/data/cloudflared
    /srv/admin/backups
  )

  local path
  for path in "${paths[@]}"; do
    if ! btrfs subvolume show "$path" >/dev/null 2>&1; then
      echo "ERROR: expected Btrfs subvolume at $path" >&2
      exit 1
    fi
    if ! btrfs qgroup show -reF "$path" | grep -Eq '^[[:space:]]*[0-9]+/[0-9]+'; then
      echo "ERROR: expected Btrfs qgroup quota output for $path" >&2
      btrfs qgroup show -reF "$path" >&2 || true
      exit 1
    fi
    echo "Btrfs quota present for $path"
  done
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
  eval "$(
    SOPS_AGE_KEY_FILE=/etc/sops/age/keys.txt \
      python3 "$REPO_ROOT/ci/read-bootstrap-oidc-user.py" \
      "$CONFIG_REPO_DIR/hosts/group_vars/secrets.sops.yaml"
  )"
  CI=true \
    NODE_EXTRA_CA_CERTS=/srv/admin/certs/ca.pem \
    SSL_CERT_FILE=/srv/admin/certs/ca.pem \
    PLAYWRIGHT_CHROMIUM_EXECUTABLE="${PLAYWRIGHT_CHROMIUM_EXECUTABLE:-/usr/bin/chromium}" \
    OIDC_TEST_USERNAME="$OIDC_TEST_USERNAME" \
    OIDC_TEST_PASSWORD="$OIDC_TEST_PASSWORD" \
    npm test
  popd >/dev/null
}

commit_config_repo_changes() {
  local message="$1"
  if [[ ! -d "$CONFIG_REPO_DIR/.git" ]]; then
    return 0
  fi
  git -C "$CONFIG_REPO_DIR" add hosts/group_vars/secrets.sops.yaml
  if ! git -C "$CONFIG_REPO_DIR" diff --cached --quiet; then
    git -C "$CONFIG_REPO_DIR" \
      -c user.name="CI Admin" \
      -c user.email="ci@example.com" \
      commit -m "$message"
  fi
}

setup_prerequisites() {
  echo "=== Preparing bootstrap CI prerequisites ==="
  "$REPO_ROOT/ci/setup-ci-env.sh"
  "$REPO_ROOT/scripts/build-admin-node.sh"
  cmp -s "$REPO_ROOT/examples/admin-config/group_vars/all.yml.example" "$CONFIG_REPO_DIR/hosts/group_vars/all.yml"
  start_otel_mock
  stop_auto_converge
}

run_init_phase() {
  echo "=== Running init phase ==="
  "$REPO_ROOT/bin/admin-node" mode set init
  assert_contains /etc/admin-node/mode "init"

  echo "=== Running convergence (init mode) via admin-node ==="
  run_converge "-e harbor_test_mode=true -e gitea_test_mode=true" "true"
  verify_btrfs_storage_isolation
  stop_auto_converge
}

initialize_openbao_for_normal_mode() {
  echo "=== Initializing OpenBao and updating encrypted config repo secrets ==="
  "$REPO_ROOT/bin/admin-node" ci init-openbao
  OPENBAO_TOKEN="$(cat "$REPO_ROOT/secrets/openbao-root-token")"
  export OPENBAO_TOKEN

  install -m 0600 /dev/null "$CONFIG_REPO_DIR/hosts/group_vars/ci-openbao-token.yml"
  "$REPO_ROOT/bin/admin-node" ci update-openbao-token --config-path "$CONFIG_REPO_DIR/hosts/group_vars/ci-openbao-token.yml"
  commit_config_repo_changes "Update OpenBao token after CI initialization"
}

run_normal_phase() {
  echo "=== Running normal phase ==="
  stop_auto_converge
  "$REPO_ROOT/bin/admin-node" mode set normal
  assert_contains /etc/admin-node/mode "normal"
  stop_auto_converge

  echo "=== Running convergence (normal mode) via admin-node ==="
  run_converge
  stop_auto_converge
}

run_openbao_config_phase() {
  echo "=== Re-running normal convergence with OpenBao config validated ==="
  stop_auto_converge

  echo "=== Running convergence (normal mode with OpenBao config) via admin-node ==="
  run_converge
  stop_auto_converge
  "$REPO_ROOT/bin/admin-node" validate observability
}

trap dump_debug ERR
trap stop_otel_mock EXIT

setup_prerequisites
run_init_phase
initialize_openbao_for_normal_mode
run_normal_phase
run_openbao_config_phase

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
