#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODE_FILE=/etc/admin-node/mode
RESTORE_ID_FILE=/etc/admin-node/restore-id
BACKUP_ROOT=/srv/admin/backups/local

restore_id="latest"
if [[ -f "$RESTORE_ID_FILE" ]]; then
  restore_id="$(tr -d '[:space:]' < "$RESTORE_ID_FILE")"
fi

if [[ "$restore_id" == "latest" ]]; then
  restore_path="$(find "$BACKUP_ROOT" -mindepth 1 -maxdepth 1 -type d -printf '%T@ %p\n' | sort -nr | awk 'NR==1 {print $2}')"
else
  restore_path="$BACKUP_ROOT/$restore_id"
fi

if [[ -z "$restore_path" || ! -d "$restore_path" ]]; then
  echo "Restore source not found" >&2
  echo "restore_failed" > "$MODE_FILE"
  exit 1
fi

set +e
if [[ "${CI:-false}" != "true" ]]; then
  docker compose -f /srv/admin/stacks/traefik/compose.yaml down
  docker compose -f /srv/admin/stacks/keycloak/compose.yaml down
  docker compose -f /srv/admin/stacks/openbao/compose.yaml down
  docker compose -f /srv/admin/stacks/harbor/compose.yaml down
  docker compose -f /srv/admin/stacks/cloudflared/compose.yaml down
fi
set -e

if [[ "${CI:-false}" == "true" ]]; then
  echo "[restore] CI mode: skipping restic restore"
else
  restic restore latest --target /
fi

if [[ -f "$restore_path/keycloak.sql" ]]; then
  if [[ "${CI:-false}" != "true" ]]; then
    docker exec -i keycloak-db psql -U keycloak keycloak < "$restore_path/keycloak.sql"
  else
    echo "[restore] CI mode: skipping keycloak DB restore"
  fi
fi
if [[ -f "$restore_path/openbao.snap" ]]; then
  if [[ "${CI:-false}" != "true" ]]; then
    docker cp "$restore_path/openbao.snap" openbao:/tmp/openbao.snap
    docker exec openbao bao operator raft snapshot restore -force /tmp/openbao.snap
  else
    echo "[restore] CI mode: skipping openbao snapshot restore"
  fi
fi

if [[ "${CI:-false}" != "true" ]]; then
  docker compose -f /srv/admin/stacks/traefik/compose.yaml up -d
  docker compose -f /srv/admin/stacks/keycloak/compose.yaml up -d
  docker compose -f /srv/admin/stacks/openbao/compose.yaml up -d
  docker compose -f /srv/admin/stacks/harbor/compose.yaml up -d
  docker compose -f /srv/admin/stacks/cloudflared/compose.yaml up -d
fi

if [[ "${CI:-false}" == "true" ]]; then
  echo "[restore] CI mode: skipping post-restore validation (mocked)"
  "$SCRIPT_DIR/validate-apis.sh"
  "$SCRIPT_DIR/validate-dns.sh"
  "$SCRIPT_DIR/validate-cloudflare-tunnel.sh"
else
  if ! "$SCRIPT_DIR/openbao-unseal.sh" || ! "$SCRIPT_DIR/validate-apis.sh" || ! "$SCRIPT_DIR/validate-dns.sh" || ! "$SCRIPT_DIR/validate-cloudflare-tunnel.sh"; then
    echo "restore_failed" > "$MODE_FILE"
    echo "Restore validation failed" >&2
    exit 1
  fi
fi

echo "normal" > "$MODE_FILE"
echo "Restore completed and mode set to normal"
