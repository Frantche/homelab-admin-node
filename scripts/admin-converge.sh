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

if [[ ! -d "$REPO_DIR/.git" ]]; then
  echo "[admin-converge] git repository not found in $REPO_DIR"
  echo "[admin-converge] ensure initial clone is completed by cloud-init"
  exit 1
fi

echo "[admin-converge] updating git repository in $REPO_DIR"
if ! git -C "$REPO_DIR" pull --ff-only; then
  echo "[admin-converge] git pull failed in $REPO_DIR"
  echo "[admin-converge] run: git -C $REPO_DIR status"
  echo "[admin-converge] then resolve/stash local changes and retry"
  exit 1
fi

if [[ ! -f "$PLAYBOOK_PATH" ]]; then
  echo "[admin-converge] playbook not found: $PLAYBOOK_PATH"
  echo "[admin-converge] check cloud-init first clone and repository content"
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
