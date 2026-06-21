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
  expected_ssh_options=(
    "permitrootlogin no"
    "passwordauthentication no"
    "kbdinteractiveauthentication no"
    "pubkeyauthentication yes"
    "allowtcpforwarding no"
    "allowagentforwarding no"
    "clientalivecountmax 2"
    "loglevel VERBOSE"
    "maxauthtries 3"
    "maxsessions 2"
    "tcpkeepalive no"
  )
  for expected_ssh_option in "${expected_ssh_options[@]}"; do
    grep -qx "$expected_ssh_option" <<<"$sshd_effective" || fail "sshd option mismatch: expected $expected_ssh_option"
  done
else
  warn "sshd command not found; skipping SSH effective config checks"
fi

if command -v sysctl >/dev/null 2>&1; then
  expected_sysctls=(
    "dev.tty.ldisc_autoload = 0"
    "fs.protected_fifos = 2"
    "fs.protected_regular = 2"
    "fs.suid_dumpable = 0"
    "kernel.sysrq = 0"
    "kernel.kptr_restrict = 2"
    "kernel.dmesg_restrict = 1"
    "kernel.unprivileged_bpf_disabled = 1"
    "net.core.bpf_jit_harden = 2"
    "net.ipv4.conf.all.log_martians = 1"
    "net.ipv4.conf.default.log_martians = 1"
  )
  for expected_sysctl in "${expected_sysctls[@]}"; do
    sysctl -n "${expected_sysctl%% = *}" | grep -qx "${expected_sysctl##* = }" || fail "sysctl mismatch: expected $expected_sysctl"
  done
else
  fail "sysctl command not found"
fi

[[ -f /etc/security/limits.d/90-admin-core-dumps.conf ]] || fail "core dump limits drop-in is missing"
[[ -f /etc/modprobe.d/90-admin-hardening.conf ]] || fail "modprobe hardening drop-in is missing"
[[ -f /etc/issue.net ]] || fail "/etc/issue.net login banner is missing"

grep -Eq '^\s*UMASK\s+027\s*$' /etc/login.defs || fail "/etc/login.defs UMASK is not 027"
grep -Eq '^\s*PASS_MIN_DAYS\s+1\s*$' /etc/login.defs || fail "/etc/login.defs PASS_MIN_DAYS is not 1"
grep -Eq '^\s*PASS_MAX_DAYS\s+365\s*$' /etc/login.defs || fail "/etc/login.defs PASS_MAX_DAYS is not 365"

for disabled_module in usb-storage firewire-ohci dccp sctp rds tipc; do
  grep -Eq "^install ${disabled_module} /bin/false$" /etc/modprobe.d/90-admin-hardening.conf || fail "module is not disabled: $disabled_module"
done

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
