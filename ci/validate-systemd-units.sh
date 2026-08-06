#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

for tool in ansible-playbook systemd-analyze; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    printf 'required tool not found: %s\n' "$tool" >&2
    exit 1
  fi
done

validation_root="$(mktemp -d /tmp/admin-node-systemd-validation.XXXXXX)"
trap 'rm -rf "$validation_root"' EXIT

ansible-playbook "$repo_root/ci/playbooks/render-validation-artifacts.yml" \
  -e "validation_output=$validation_root" >/dev/null

for unit in "$validation_root"/systemd/*.service; do
  sed -i 's#/opt/homelab-admin-node/bin/admin-node#/bin/true#g' "$unit"
done

if rg -n '/opt/homelab-admin-node/bin/admin-node' "$validation_root/systemd"; then
  echo "rendered systemd validation still depends on the deployed admin-node binary" >&2
  exit 1
fi

systemd-analyze verify "$validation_root"/systemd/*
