#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODE_FILE=/etc/admin-node/mode
BACKUP_ROOT=/srv/admin/backups/local
STAMP="$(date +%Y%m%d-%H%M%S)"
TARGET="$BACKUP_ROOT/$STAMP"

mode="$(cat "$MODE_FILE" 2>/dev/null || echo locked)"
if [[ "$mode" == "locked" ]]; then
  echo "Refusing backup in locked mode" >&2
  exit 1
fi

"$SCRIPT_DIR/validate-apis.sh"
"$SCRIPT_DIR/validate-dns.sh"
"$SCRIPT_DIR/validate-cloudflare-tunnel.sh"

mkdir -p "$TARGET"

docker exec keycloak-db pg_dump -U keycloak keycloak > "$TARGET/keycloak.sql"
if docker ps --format '{{.Names}}' | grep -qx gitea-db; then
  docker exec gitea-db pg_dump -U gitea gitea > "$TARGET/gitea.sql"
else
  echo "[backup] WARNING: gitea-db is not running, skipping Gitea database dump" >&2
fi

# OpenBao raft snapshot requires authentication
BAO_TOKEN="${OPENBAO_TOKEN:-}"
if [[ -z "$BAO_TOKEN" && -f /opt/homelab-admin-node/secrets/openbao-root-token ]]; then
  BAO_TOKEN="$(cat /opt/homelab-admin-node/secrets/openbao-root-token)"
fi
# Try loading backup token from SOPS secrets file
SECRETS_FILE=/opt/homelab-admin-node/secrets/openbao-unseal.sops.yaml
if [[ -z "$BAO_TOKEN" && -f /etc/sops/age/keys.txt && -f "$SECRETS_FILE" ]] && command -v sops &>/dev/null; then
  sops_json="$(SOPS_AGE_KEY_FILE=/etc/sops/age/keys.txt sops --decrypt --output-type json "$SECRETS_FILE")"
  BAO_TOKEN="$(python3 -c 'import json,sys; print(json.loads(sys.stdin.read())["openbao"]["backup_token"])' <<< "$sops_json" 2>/dev/null)" || {
    echo "[backup] WARNING: backup_token not found in SOPS secrets, skipping raft snapshot" >&2
    BAO_TOKEN=""
  }
fi
if [[ -n "$BAO_TOKEN" ]]; then
  docker exec -e BAO_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN="$BAO_TOKEN" openbao bao operator raft snapshot save /tmp/openbao.snap >/dev/null
  docker cp openbao:/tmp/openbao.snap "$TARGET/openbao.snap"
else
  echo "[backup] WARNING: No OpenBao token available, skipping raft snapshot" >&2
fi

cp -a /srv/admin/stacks "$TARGET/stacks"
cp -a /srv/admin/env "$TARGET/env"
if [[ -d /srv/admin/data/gitea ]]; then
  cp -a /srv/admin/data/gitea "$TARGET/gitea-data"
fi

if command -v restic &>/dev/null && [[ -n "${RESTIC_REPOSITORY:-}" ]]; then
  restic backup /srv/admin/stacks /srv/admin/env /srv/admin/data
  restic forget --keep-last 3 --prune
else
  echo "[backup] restic not configured, skipping remote backup"
fi

mapfile -t old_backups < <(find "$BACKUP_ROOT" -mindepth 1 -maxdepth 1 -type d -printf '%T@ %p\n' | sort -nr | awk 'NR>3 {print $2}')
if ((${#old_backups[@]})); then
  rm -rf "${old_backups[@]}"
fi

echo "Backup completed: $TARGET"
