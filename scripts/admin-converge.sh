#!/usr/bin/env bash
set -euo pipefail

LOCK_FILE=/run/admin-converge.lock
REF_FILE=/etc/admin-node/git-ref
REF="main"
ADMIN_REPO_URL="${ADMIN_REPO_URL:-ssh://git@example.com/homelab/homelab-admin-node.git}"
CONFIG_REPO_URL="${CONFIG_REPO_URL:-}"
CONFIG_REPO_BRANCH="${CONFIG_REPO_BRANCH:-main}"
CONFIG_REPO_DIR="${CONFIG_REPO_DIR:-/etc/admin-config}"
REPO_DIR=/opt/homelab-admin-node

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

if [[ -n "$CONFIG_REPO_URL" ]]; then
  # --- Config repo mode: git clone/pull + ansible-playbook with both inventories ---
  echo "[admin-converge] config repo mode: $CONFIG_REPO_URL"

  # Pull or clone the config repo
  if [[ -d "$CONFIG_REPO_DIR/.git" ]]; then
    echo "[admin-converge] updating config repo in $CONFIG_REPO_DIR"
    git -C "$CONFIG_REPO_DIR" fetch --prune origin
    git -C "$CONFIG_REPO_DIR" checkout "$CONFIG_REPO_BRANCH"
    echo "[admin-converge] resetting $CONFIG_REPO_DIR to origin/$CONFIG_REPO_BRANCH (local changes will be discarded)"
    git -C "$CONFIG_REPO_DIR" reset --hard "origin/$CONFIG_REPO_BRANCH"
  else
    echo "[admin-converge] cloning config repo to $CONFIG_REPO_DIR"
    git clone --branch "$CONFIG_REPO_BRANCH" "$CONFIG_REPO_URL" "$CONFIG_REPO_DIR"
  fi

  # Pull or clone the main repo
  if [[ -d "$REPO_DIR/.git" ]]; then
    echo "[admin-converge] updating main repo in $REPO_DIR"
    git -C "$REPO_DIR" fetch --prune origin
    git -C "$REPO_DIR" checkout "$REF"
    echo "[admin-converge] resetting $REPO_DIR to origin/$REF (local changes will be discarded)"
    git -C "$REPO_DIR" reset --hard "origin/$REF"
  else
    echo "[admin-converge] cloning main repo to $REPO_DIR"
    git clone --branch "$REF" "$ADMIN_REPO_URL" "$REPO_DIR"
  fi

  ansible-playbook \
    -i "$REPO_DIR/ansible/inventory.ini" \
    -i "$CONFIG_REPO_DIR/" \
    "$REPO_DIR/ansible/site.yml"
else
  # --- Default mode: ansible-pull ---
  ansible-pull \
    -U "$ADMIN_REPO_URL" \
    --directory "$REPO_DIR" \
    -i localhost, \
    -c local \
    --checkout "$REF" \
    ansible/site.yml
fi

echo "[admin-converge] completed"
