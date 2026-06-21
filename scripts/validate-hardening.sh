#!/usr/bin/env bash
set -euo pipefail

fail() {
  echo "[hardening] ERROR: $*" >&2
  exit 1
}

warn() {
  echo "[hardening] WARNING: $*" >&2
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing command: $1"
}

require_cmd systemctl
require_cmd ss

if command -v sshd >/dev/null 2>&1; then
  sshd_effective="$(sshd -T 2>/dev/null || true)"
  grep -qx "permitrootlogin no" <<<"$sshd_effective" || fail "sshd PermitRootLogin is not no"
  grep -qx "passwordauthentication no" <<<"$sshd_effective" || fail "sshd PasswordAuthentication is not no"
  grep -qx "kbdinteractiveauthentication no" <<<"$sshd_effective" || fail "sshd KbdInteractiveAuthentication is not no"
  grep -qx "pubkeyauthentication yes" <<<"$sshd_effective" || fail "sshd PubkeyAuthentication is not yes"
else
  warn "sshd command not found; skipping SSH effective config checks"
fi

if command -v nft >/dev/null 2>&1; then
  if systemctl list-unit-files nftables.service >/dev/null 2>&1; then
    systemctl is-enabled --quiet nftables || warn "nftables service is not enabled"
  fi
  nft_ruleset="$(nft list table inet admin_filter)"
  grep -q "hook input priority filter; policy drop;" <<<"$nft_ruleset" || fail "nftables input policy is not drop"
  grep -q "tcp dport 22 accept" <<<"$nft_ruleset" || fail "nftables does not allow SSH"
  grep -q "tcp dport 443 accept" <<<"$nft_ruleset" || fail "nftables does not allow HTTPS"
else
  fail "nft command not found"
fi

[[ -d /var/log/journal ]] || fail "persistent journal directory is missing"
systemctl is-active --quiet systemd-journald || fail "systemd-journald is not active"

if systemctl list-unit-files auditd.service >/dev/null 2>&1; then
  systemctl is-active --quiet auditd || fail "auditd is not active"
  [[ -f /etc/audit/rules.d/90-admin-node.rules ]] || fail "admin audit rules are missing"
else
  warn "auditd.service not installed; skipping auditd checks"
fi

if systemctl list-unit-files fail2ban.service >/dev/null 2>&1; then
  systemctl is-active --quiet fail2ban || fail "fail2ban is not active"
  [[ -f /etc/fail2ban/jail.d/sshd.local ]] || fail "fail2ban sshd jail is missing"
else
  warn "fail2ban.service not installed; skipping fail2ban checks"
fi

if [[ -f /etc/sops/age/keys.txt ]]; then
  age_mode="$(stat -c '%a %U %G' /etc/sops/age/keys.txt)"
  [[ "$age_mode" == "400 root root" ]] || fail "/etc/sops/age/keys.txt permissions are $age_mode, expected 400 root root"
fi

if [[ -d /srv/admin/env ]]; then
  env_mode="$(stat -c '%a %U %G' /srv/admin/env)"
  [[ "$env_mode" == "700 root root" ]] || fail "/srv/admin/env permissions are $env_mode, expected 700 root root"
  while IFS= read -r env_file; do
    file_mode="$(stat -c '%a %U %G' "$env_file")"
    [[ "$file_mode" == "600 root root" ]] || fail "$env_file permissions are $file_mode, expected 600 root root"
  done < <(find /srv/admin/env -maxdepth 1 -type f -name '*.env' | sort)
fi

unexpected_ports=()
while read -r proto state _ _ local_addr _; do
  [[ "$state" == "LISTEN" ]] || continue
  host_port="${local_addr##*:}"
  host="${local_addr%:*}"
  case "$host_port" in
    22|443) ;;
    1514)
      [[ "$host" == "127.0.0.1" || "$host" == "[::1]" || "$host" == "::1" ]] || unexpected_ports+=("$proto $local_addr")
      ;;
    *) unexpected_ports+=("$proto $local_addr") ;;
  esac
done < <(ss -H -tuln)

if ((${#unexpected_ports[@]})); then
  printf '[hardening] WARNING: unexpected listening ports blocked by default-deny firewall:\n' >&2
  printf '  %s\n' "${unexpected_ports[@]}" >&2
fi

echo "[hardening] validation passed"
