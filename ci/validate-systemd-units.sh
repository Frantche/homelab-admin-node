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

systemd-analyze verify "$validation_root"/systemd/*
