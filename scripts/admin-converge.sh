#!/usr/bin/env bash
set -euo pipefail

LOCK_FILE=/run/admin-converge.lock
LOG_FILE=/var/log/admin-converge.log
MODE_FILE=/etc/admin-config/mode
REPO_DIR=/opt/homelab-admin-node
INVENTORY_FILE=/etc/admin-config/inventory.ini
PLAYBOOK_FILE="$REPO_DIR/ansible/site.yml"

REQUIRED_FILES=(
  /etc/sops/age/keys.txt
  /etc/admin-config/inventory.ini
  /etc/admin-config/group_vars/all.yml
  /etc/admin-config/group_vars/secrets.sops.yaml
)

log() {
  echo "[admin-converge] $*"
}

setup_logging() {
  if install -d -m 0755 /var/log 2>/dev/null && touch "$LOG_FILE" 2>/dev/null; then
    chmod 0644 "$LOG_FILE" 2>/dev/null || true
    exec > >(tee -a "$LOG_FILE") 2>&1
  fi
}

ensure_mode_file() {
  install -d -m 0755 /etc/admin-config
  if [[ ! -f "$MODE_FILE" ]]; then
    echo "locked" > "$MODE_FILE"
  fi
}

print_required_files() {
  log "required files:"
  for file in "${REQUIRED_FILES[@]}"; do
    echo "- $file"
  done
}

print_unlock_instructions() {
  cat <<'MSG'
Deposit the required secrets and configuration, then run:
sudo adminctl mode init
sudo adminctl converge
MSG
}

collect_missing_files() {
  if [[ $# -ne 1 ]]; then
    log "collect_missing_files requires one argument"
    return 1
  fi

  local -n missing_ref=$1
  missing_ref=()
  for required_file in "${REQUIRED_FILES[@]}"; do
    if [[ ! -f "$required_file" ]]; then
      missing_ref+=("$required_file")
    fi
  done
}

setup_logging
ensure_mode_file

install -d -m 0755 /run
log "starting"

exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  log "another run is in progress, exiting"
  exit 0
fi

log "lock acquired"
mode="$(tr -d '[:space:]' < "$MODE_FILE")"

case "$mode" in
  locked)
    log "mode is locked"
    print_required_files
    print_unlock_instructions
    exit 0
    ;;
  init|normal)
    missing_files=()
    collect_missing_files missing_files
    if ((${#missing_files[@]} > 0)); then
      echo "locked" > "$MODE_FILE"
      log "missing required files, mode switched to locked"
      for missing in "${missing_files[@]}"; do
        echo "- $missing"
      done
      print_unlock_instructions
      exit 0
    fi
    ;;
  restore|restore_failed)
    log "mode is $mode"
    ;;
  *)
    log "unknown mode '$mode', forcing locked"
    echo "locked" > "$MODE_FILE"
    print_required_files
    print_unlock_instructions
    exit 0
    ;;
esac

ansible-playbook -i "$INVENTORY_FILE" "$PLAYBOOK_FILE"

if [[ "$mode" == "init" ]]; then
  echo "normal" > "$MODE_FILE"
  log "init convergence succeeded, mode switched to normal"
fi

log "completed"
