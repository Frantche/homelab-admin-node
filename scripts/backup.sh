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

if [[ "${CI:-false}" == "true" ]]; then
  echo "[backup] CI mode: skipping docker/restic operations"
  cp -a /srv/admin/stacks "$TARGET/stacks"
  cp -a /srv/admin/env "$TARGET/env"
  echo "-- CI mock dump" > "$TARGET/keycloak.sql"
  echo "CI mock snapshot" > "$TARGET/openbao.snap"
else
  docker exec keycloak-db pg_dump -U keycloak keycloak > "$TARGET/keycloak.sql"
  docker exec openbao bao operator raft snapshot save /tmp/openbao.snap >/dev/null
  docker cp openbao:/tmp/openbao.snap "$TARGET/openbao.snap"
  cp -a /srv/admin/stacks "$TARGET/stacks"
  cp -a /srv/admin/env "$TARGET/env"

  restic backup /srv/admin/stacks /srv/admin/env /srv/admin/data
  restic forget --keep-last 3 --prune
fi

mapfile -t old_backups < <(find "$BACKUP_ROOT" -mindepth 1 -maxdepth 1 -type d -printf '%T@ %p\n' | sort -nr | awk 'NR>3 {print $2}')
if ((${#old_backups[@]})); then
  rm -rf "${old_backups[@]}"
fi

echo "Backup completed: $TARGET"
