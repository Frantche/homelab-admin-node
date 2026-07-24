#!/usr/bin/env bash
set -euo pipefail

mode="${1:-}"
config_repo="${2:-/etc/admin-config/homelab-node-admin-config}"
audit_file="${3:-/tmp/admin-node-secret-rotation-audit.json}"
encrypted_file="$config_repo/hosts/group_vars/secrets.sops.yaml"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ "$mode" != "prepare" && "$mode" != "finalize" ]]; then
  echo "usage: rotate-bootstrap-config.sh <prepare|finalize> [config-repo] [audit-file]" >&2
  exit 2
fi

plain_file="$(mktemp /tmp/admin-node-rotated-secrets.XXXXXX.yaml)"
encrypted_tmp="$(mktemp "$config_repo/hosts/group_vars/.secrets.sops.XXXXXX.yaml")"
trap 'rm -f "$plain_file" "$encrypted_tmp"' EXIT
chmod 0600 "$plain_file" "$encrypted_tmp"

SOPS_AGE_KEY_FILE=/etc/sops/age/keys.txt \
  sops --decrypt --output-type yaml "$encrypted_file" >"$plain_file"
python3 "$repo_root/ci/rotate-bootstrap-secrets.py" \
  "$mode" "$plain_file" --audit-file "$audit_file"

(
  cd "$config_repo"
  SOPS_AGE_KEY_FILE=/etc/sops/age/keys.txt \
    sops --config .sops.yaml \
      --encrypt \
      --input-type yaml \
      --output-type yaml \
      --filename-override hosts/group_vars/secrets.sops.yaml \
      "$plain_file" >"$encrypted_tmp"
)
install -m 0600 "$encrypted_tmp" "$encrypted_file"

git -C "$config_repo" add hosts/group_vars/secrets.sops.yaml
if ! git -C "$config_repo" diff --cached --quiet; then
  git -C "$config_repo" \
    -c user.name="CI Admin" \
    -c user.email="ci@example.com" \
    commit -m "CI ${mode} technical secret rotation"
fi
