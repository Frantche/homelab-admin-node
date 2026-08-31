#!/usr/bin/env bash
set -euo pipefail

pacman_bin="${ADMIN_SYSTEM_UPDATE_PACMAN_BIN:-/usr/bin/pacman}"
systemctl_bin="${ADMIN_SYSTEM_UPDATE_SYSTEMCTL_BIN:-/usr/bin/systemctl}"
auto_reboot="${ADMIN_SYSTEM_UPDATE_AUTO_REBOOT:-true}"

if [[ ! -x "$pacman_bin" ]]; then
  printf 'pacman executable not found: %s\n' "$pacman_bin" >&2
  exit 1
fi

case "$auto_reboot" in
  true|false) ;;
  *)
    printf 'ADMIN_SYSTEM_UPDATE_AUTO_REBOOT must be true or false, got: %s\n' "$auto_reboot" >&2
    exit 1
    ;;
esac

package_state_before="$($pacman_bin --query | sha256sum | awk '{print $1}')"

printf 'Starting full Arch Linux system upgrade\n'
"$pacman_bin" --sync --refresh --sysupgrade --noconfirm

package_state_after="$($pacman_bin --query | sha256sum | awk '{print $1}')"
if [[ "$package_state_before" == "$package_state_after" ]]; then
  printf 'System packages were already up to date\n'
  exit 0
fi

printf 'System package versions changed successfully\n'
if [[ "$auto_reboot" == "false" ]]; then
  printf 'Automatic reboot is disabled; reboot the node to activate all updates\n'
  exit 0
fi

if [[ ! -x "$systemctl_bin" ]]; then
  printf 'systemctl executable not found after package upgrade: %s\n' "$systemctl_bin" >&2
  exit 1
fi

printf 'Scheduling reboot to activate the updated system\n'
"$systemctl_bin" reboot
