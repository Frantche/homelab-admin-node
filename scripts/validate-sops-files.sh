#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

status=0
while IFS= read -r path; do
  [[ "$path" == *.example ]] && continue
  if ! grep -q '^sops:' "$path"; then
    echo "$path is tracked as a SOPS file but has no SOPS metadata" >&2
    status=1
  fi
done < <(git ls-files 'secrets/*.sops.yaml' 'ansible/group_vars/secrets.sops.yaml')

if git ls-files --error-unmatch secrets/openbao-root-token >/dev/null 2>&1; then
  echo "secrets/openbao-root-token must never be tracked" >&2
  status=1
fi

exit "$status"
