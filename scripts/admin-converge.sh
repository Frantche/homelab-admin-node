#!/usr/bin/env bash
set -euo pipefail

LOCK_FILE=/run/admin-converge.lock
REPO_DIR="${REPO_DIR:-/opt/homelab-admin-node}"
INVENTORY_PATH="${INVENTORY_PATH:-/etc/admin-config/hosts}"
PLAYBOOK_PATH="${PLAYBOOK_PATH:-$REPO_DIR/ansible/site.yml}"

mkdir -p /run
echo "[admin-converge] starting"
echo "[admin-converge] playbook=$PLAYBOOK_PATH inventory=$INVENTORY_PATH"

exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  echo "[admin-converge] another run is in progress, exiting"
  exit 0
fi

echo "[admin-converge] lock acquired"

if [[ ! -f "$PLAYBOOK_PATH" ]]; then
  echo "[admin-converge] playbook not found: $PLAYBOOK_PATH"
  echo "[admin-converge] clone the repository manually via git CLI in $REPO_DIR"
  exit 1
fi

if [[ ! -f "$INVENTORY_PATH" ]]; then
  echo "[admin-converge] inventory not found: $INVENTORY_PATH"
  echo "[admin-converge] copy an inventory file to $INVENTORY_PATH (example source: $REPO_DIR/ansible/inventory.ini)"
  exit 1
fi

ansible-playbook \
  -i "$INVENTORY_PATH" \
  "$PLAYBOOK_PATH"

echo "[admin-converge] completed"
