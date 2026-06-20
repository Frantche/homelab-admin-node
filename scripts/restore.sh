#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
KEYCLOAK_DOMAIN="${KEYCLOAK_DOMAIN:-$("$REPO_ROOT/ci/service-domains.py" get keycloak)}"
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
docker compose --env-file /srv/admin/env/traefik.env -f /srv/admin/stacks/traefik/compose.yaml down 2>/dev/null
docker compose --env-file /srv/admin/env/keycloak.env -f /srv/admin/stacks/keycloak/compose.yaml down 2>/dev/null
docker compose -f /srv/admin/stacks/openbao/compose.yaml down 2>/dev/null
docker compose --env-file /srv/admin/env/harbor.env -f /srv/admin/stacks/harbor/compose.yaml down 2>/dev/null
if [[ -f /srv/admin/env/gitea.env && -f /srv/admin/stacks/gitea/compose.yaml ]]; then
  docker compose --env-file /srv/admin/env/gitea.env -f /srv/admin/stacks/gitea/compose.yaml down 2>/dev/null
fi
if [[ "${CI_MOCK_CLOUDFLARE_TUNNEL:-false}" != "true" ]]; then
  docker compose --env-file /srv/admin/env/cloudflared.env -f /srv/admin/stacks/cloudflared/compose.yaml down 2>/dev/null
fi
set -e

if command -v restic &>/dev/null && [[ -n "${RESTIC_REPOSITORY:-}" ]]; then
  restic restore latest --target /
else
  echo "[restore] restic not configured, restoring from local backup only"
fi

if [[ -f "$restore_path/keycloak.sql" ]]; then
  docker compose --env-file /srv/admin/env/keycloak.env -f /srv/admin/stacks/keycloak/compose.yaml up -d keycloak-db
  echo "[restore] waiting for keycloak-db..."
  for _ in $(seq 1 30); do
    if docker exec keycloak-db pg_isready -U keycloak &>/dev/null; then break; fi
    sleep 1
  done
  docker exec -i keycloak-db psql -U keycloak keycloak < "$restore_path/keycloak.sql"
  docker compose --env-file /srv/admin/env/keycloak.env -f /srv/admin/stacks/keycloak/compose.yaml down 2>/dev/null
fi

if [[ -d "$restore_path/gitea-data" ]]; then
  rm -rf /srv/admin/data/gitea
  cp -a "$restore_path/gitea-data" /srv/admin/data/gitea
fi

if [[ -f "$restore_path/gitea.sql" && -f /srv/admin/env/gitea.env && -f /srv/admin/stacks/gitea/compose.yaml ]]; then
  docker compose --env-file /srv/admin/env/gitea.env -f /srv/admin/stacks/gitea/compose.yaml up -d gitea-db
  echo "[restore] waiting for gitea-db..."
  for _ in $(seq 1 30); do
    if docker exec gitea-db pg_isready -U gitea &>/dev/null; then break; fi
    sleep 1
  done
  docker exec gitea-db psql -U gitea gitea -c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;' >/dev/null
  docker exec -i gitea-db psql -U gitea gitea < "$restore_path/gitea.sql"
  docker compose --env-file /srv/admin/env/gitea.env -f /srv/admin/stacks/gitea/compose.yaml down 2>/dev/null
fi

if [[ -f "$restore_path/openbao.snap" ]]; then
  docker compose -f /srv/admin/stacks/openbao/compose.yaml up -d
  echo "[restore] waiting for openbao..."
  for _ in $(seq 1 30); do
    # bao status exits non-zero when sealed or starting up; capture output regardless
    bao_out=""
    if ! bao_out="$(docker exec -e BAO_ADDR=http://127.0.0.1:8200 openbao bao status 2>&1)"; then
      :
    fi
    if echo "$bao_out" | grep -q "Initialized"; then break; fi
    sleep 1
  done
  # Raft snapshot restore requires the vault to be unsealed first
  BAO_TOKEN="${OPENBAO_TOKEN:-}"
  if [[ -z "$BAO_TOKEN" && -f "$REPO_ROOT/secrets/openbao-root-token" ]]; then
    BAO_TOKEN="$(cat "$REPO_ROOT/secrets/openbao-root-token")"
  fi
  # Unseal before restore using openbao-unseal.sh (handles SOPS decryption)
  SECRETS_FILE="$REPO_ROOT/secrets/openbao-unseal.sops.yaml"
  if [[ -f /etc/sops/age/keys.txt && -f "$SECRETS_FILE" ]] && command -v sops &>/dev/null; then
    SECRETS_FILE="$SECRETS_FILE" "$SCRIPT_DIR/openbao-unseal.sh" || {
      echo "[restore] WARNING: openbao-unseal.sh failed during pre-restore unseal, continuing..." >&2
    }
  elif [[ -f "$SECRETS_FILE" ]]; then
    # Plain-text secrets file (CI) - extract unseal keys with grep/sed
    pre_threshold="$(grep 'threshold:' "$SECRETS_FILE" | head -1 | awk '{print $2}')"
    pre_threshold="${pre_threshold:-3}"
    mapfile -t pre_keys < <(grep -E '^\s+- "' "$SECRETS_FILE" | sed 's/.*"\(.*\)".*/\1/' | head -"$pre_threshold")
    if [[ ${#pre_keys[@]} -gt 0 ]]; then
      for key in "${pre_keys[@]}"; do
        unseal_rc=0
        docker exec -e BAO_ADDR=http://127.0.0.1:8200 openbao bao operator unseal "$key" >/dev/null 2>&1 || unseal_rc=$?
        if [[ $unseal_rc -ne 0 && $unseal_rc -ne 2 ]]; then
          echo "[restore] WARNING: unseal key application failed (exit $unseal_rc)" >&2
        fi
      done
    fi
  fi
  docker cp "$restore_path/openbao.snap" openbao:/tmp/openbao.snap
  if [[ -n "$BAO_TOKEN" ]]; then
    docker exec -e BAO_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN="$BAO_TOKEN" openbao bao operator raft snapshot restore -force /tmp/openbao.snap
  fi
  docker compose -f /srv/admin/stacks/openbao/compose.yaml down 2>/dev/null
fi

docker compose -f /srv/admin/stacks/openbao/compose.yaml up -d
docker compose --env-file /srv/admin/env/traefik.env -f /srv/admin/stacks/traefik/compose.yaml up -d
docker compose --env-file /srv/admin/env/keycloak.env -f /srv/admin/stacks/keycloak/compose.yaml up -d
docker compose --env-file /srv/admin/env/harbor.env -f /srv/admin/stacks/harbor/compose.yaml up -d
if [[ -f /srv/admin/env/gitea.env && -f /srv/admin/stacks/gitea/compose.yaml ]]; then
  docker compose --env-file /srv/admin/env/gitea.env -f /srv/admin/stacks/gitea/compose.yaml up -d
fi
if [[ "${CI_MOCK_CLOUDFLARE_TUNNEL:-false}" != "true" ]]; then
  docker compose --env-file /srv/admin/env/cloudflared.env -f /srv/admin/stacks/cloudflared/compose.yaml up -d
fi

echo "[restore] waiting for services to be ready..."
for _ in $(seq 1 60); do
  # bao status exits non-zero when sealed or starting up; capture output regardless
  bao_out=""
  if ! bao_out="$(docker exec -e BAO_ADDR=http://127.0.0.1:8200 openbao bao status 2>&1)"; then
    :
  fi
  if echo "$bao_out" | grep -q "Initialized"; then break; fi
  sleep 2
done

# Unseal OpenBao: try SOPS-based unseal first, fall back to CI secrets file
SECRETS_FILE="$REPO_ROOT/secrets/openbao-unseal.sops.yaml"
unseal_ok=false
if [[ -f /etc/sops/age/keys.txt && -f "$SECRETS_FILE" ]] && command -v sops &>/dev/null; then
  if SECRETS_FILE="$SECRETS_FILE" "$SCRIPT_DIR/openbao-unseal.sh"; then
    unseal_ok=true
  fi
elif [[ -f "$SECRETS_FILE" ]]; then
  # Plain-text secrets file - extract unseal keys with grep/sed
  threshold="$(grep 'threshold:' "$SECRETS_FILE" | head -1 | awk '{print $2}')"
  threshold="${threshold:-3}"
  mapfile -t keys < <(grep -E '^\s+- "' "$SECRETS_FILE" | sed 's/.*"\(.*\)".*/\1/' | head -"$threshold")
  if [[ ${#keys[@]} -gt 0 ]]; then
    for key in "${keys[@]}"; do
      unseal_rc=0
      docker exec -e BAO_ADDR=http://127.0.0.1:8200 openbao bao operator unseal "$key" >/dev/null 2>&1 || unseal_rc=$?
      if [[ $unseal_rc -ne 0 && $unseal_rc -ne 2 ]]; then
        echo "[restore] unseal key application failed (exit $unseal_rc)" >&2
      fi
    done
    unseal_ok=true
  fi
fi

if [[ "$unseal_ok" != "true" ]]; then
  # Check if already unsealed
  sealed_rc=0
  sealed_json="$(docker exec -e BAO_ADDR=http://127.0.0.1:8200 openbao bao status -format=json 2>/dev/null)" || sealed_rc=$?
  if [[ $sealed_rc -eq 0 ]]; then
    sealed="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1]).get("sealed", True))' "$sealed_json" 2>/dev/null)"
    if [[ "$sealed" == "False" ]]; then
      unseal_ok=true
    fi
  fi
fi

if [[ "$unseal_ok" != "true" ]]; then
  echo "restore_failed" > "$MODE_FILE"
  echo "OpenBao unseal failed during restore" >&2
  exit 1
fi

# Wait for Keycloak
for _ in $(seq 1 60); do
  if curl -fsS "https://${KEYCLOAK_DOMAIN}/realms/master/.well-known/openid-configuration" &>/dev/null; then break; fi
  sleep 2
done

if ! "$SCRIPT_DIR/validate-apis.sh" || ! "$SCRIPT_DIR/validate-dns.sh" || ! "$SCRIPT_DIR/validate-cloudflare-tunnel.sh"; then
  echo "restore_failed" > "$MODE_FILE"
  echo "Restore validation failed" >&2
  exit 1
fi

echo "normal" > "$MODE_FILE"
echo "Restore completed and mode set to normal"
