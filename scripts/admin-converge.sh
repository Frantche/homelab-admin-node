#!/usr/bin/env bash
set -euo pipefail

LOCK_FILE=/run/admin-converge.lock
MODE_FILE=/etc/admin-node/mode
REF_FILE=/etc/admin-node/git-ref
REF="main"

if [[ -f "$REF_FILE" ]]; then
  REF="$(tr -d '[:space:]' < "$REF_FILE")"
fi

mkdir -p /run
echo "[admin-converge] starting with ref=$REF"

flock -n "$LOCK_FILE" bash -c '
  set -euo pipefail
  echo "[admin-converge] lock acquired"
  ansible-pull \
    -U "${ADMIN_REPO_URL:-ssh://git@example.com/homelab/homelab-admin-node.git}" \
    -i localhost, \
    -c local \
    --checkout "'"$REF"'" \
    /opt/homelab-admin-node/ansible/site.yml
  echo "[admin-converge] completed"
'
