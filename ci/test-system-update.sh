#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_root="$(mktemp -d /tmp/admin-node-system-update-test.XXXXXX)"
trap 'rm -rf "$test_root"' EXIT

mock_pacman="$test_root/pacman"
mock_systemctl="$test_root/systemctl"
pacman_state="$test_root/pacman-state"
systemctl_log="$test_root/systemctl.log"

cat >"$mock_pacman" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--query" ]]; then
  printf 'example %s\n' "$(<"$MOCK_PACMAN_STATE")"
  exit 0
fi
if [[ "$*" == "--sync --refresh --sysupgrade --noconfirm" ]]; then
  if [[ "${MOCK_PACMAN_FAIL:-false}" == "true" ]]; then
    exit 42
  fi
  if [[ "${MOCK_PACMAN_CHANGE:-true}" == "true" ]]; then
    printf '2.0\n' >"$MOCK_PACMAN_STATE"
  fi
  exit 0
fi
exit 2
EOF

cat >"$mock_systemctl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$MOCK_SYSTEMCTL_LOG"
EOF
chmod +x "$mock_pacman" "$mock_systemctl"

run_update() {
  printf '1.0\n' >"$pacman_state"
  : >"$systemctl_log"
  MOCK_PACMAN_STATE="$pacman_state" \
    MOCK_SYSTEMCTL_LOG="$systemctl_log" \
    ADMIN_SYSTEM_UPDATE_PACMAN_BIN="$mock_pacman" \
    ADMIN_SYSTEM_UPDATE_SYSTEMCTL_BIN="$mock_systemctl" \
    "$repo_root/scripts/system-update.sh"
}

ADMIN_SYSTEM_UPDATE_AUTO_REBOOT=true MOCK_PACMAN_CHANGE=true run_update
rg -Fx 'reboot' "$systemctl_log" >/dev/null

ADMIN_SYSTEM_UPDATE_AUTO_REBOOT=true MOCK_PACMAN_CHANGE=false run_update
if [[ -s "$systemctl_log" ]]; then
  echo "unchanged packages must not trigger a reboot" >&2
  exit 1
fi

ADMIN_SYSTEM_UPDATE_AUTO_REBOOT=false MOCK_PACMAN_CHANGE=true run_update
if [[ -s "$systemctl_log" ]]; then
  echo "disabled automatic reboot must be honored" >&2
  exit 1
fi

if ADMIN_SYSTEM_UPDATE_AUTO_REBOOT=true MOCK_PACMAN_FAIL=true run_update; then
  echo "pacman failure must fail the system update" >&2
  exit 1
fi
if [[ -s "$systemctl_log" ]]; then
  echo "failed package updates must not trigger a reboot" >&2
  exit 1
fi
