#!/usr/bin/env bash
set -euo pipefail

# Set up CI-specific prerequisites that the Ansible playbook cannot handle itself.
# This only does: CI age key setup, /etc/hosts entries, mode directory.
# All actual deployment is done by the Ansible playbook.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Create required directories
mkdir -p /etc/admin-node

# --- Install CI-only age key for SOPS secrets ---
if [[ ! -f /etc/sops/age/keys.txt ]]; then
  echo "[ci-setup] Generating CI-only age key..."
  install -d -m 0700 /etc/sops/age
  tmp_age_dir="$(mktemp -d /tmp/ci-age-key.XXXXXX)"
  tmp_age_key="$tmp_age_dir/keys.txt"
  age-keygen -o "$tmp_age_key" >/dev/null
  install -m 0400 "$tmp_age_key" /etc/sops/age/keys.txt
  rm -rf "$tmp_age_dir"
fi

mapfile -t service_domains < <("$REPO_ROOT/ci/service-domains.py" list)
if [[ ${#service_domains[@]} -eq 0 ]]; then
  service_domains=(keycloak.example.com bao.example.com harbor.example.com traefik.example.com)
fi

# --- Add /etc/hosts entries for service domains ---
for domain in "${service_domains[@]}"; do
  if ! grep -qF "$domain" /etc/hosts; then
    echo "127.0.0.1 $domain" >> /etc/hosts
  fi
done

# --- Install required Ansible collections ---
if [[ -f "$REPO_ROOT/ansible/requirements.yml" ]]; then
  echo "[ci-setup] Installing required Ansible collections..."
  ansible-galaxy collection install -r "$REPO_ROOT/ansible/requirements.yml" --force 2>/dev/null || true
fi

echo "[ci-setup] CI prerequisites ready"
