#!/usr/bin/env bash
set -euo pipefail

LOCK_FILE=/run/admin-converge.lock
REF_FILE=/etc/admin-node/git-ref
REF="main"

if [[ -f "$REF_FILE" ]]; then
  REF="$(tr -d '[:space:]' < "$REF_FILE")"
fi

mkdir -p /run
echo "[admin-converge] starting with ref=$REF"

exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  echo "[admin-converge] another run is in progress, exiting"
  exit 0
fi

echo "[admin-converge] lock acquired"
ansible-pull \
  -U "${ADMIN_REPO_URL:-ssh://git@example.com/homelab/homelab-admin-node.git}" \
  -i localhost, \
  -c local \
  --checkout "$REF" \
  /opt/homelab-admin-node/ansible/site.yml
echo "[admin-converge] completed"
