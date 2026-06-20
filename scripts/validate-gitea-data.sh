#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GITEA_DOMAIN="${GITEA_DOMAIN:-$("$REPO_ROOT/ci/service-domains.py" get gitea)}"
GITEA_URL="${GITEA_URL:-https://${GITEA_DOMAIN}}"
GITEA_ADMIN_USER="${GITEA_ADMIN_USER:-admin}"
GITEA_ADMIN_PASSWORD="${GITEA_ADMIN_PASSWORD:-}"
GITEA_VALIDATION_REPO="${GITEA_VALIDATION_REPO:-admin-node-validation}"
GITEA_VALIDATION_ISSUE_TITLE="${GITEA_VALIDATION_ISSUE_TITLE:-Backup restore sentinel}"
GITEA_VALIDATION_CREATE="${GITEA_VALIDATION_CREATE:-true}"
export GITEA_VALIDATION_REPO GITEA_VALIDATION_ISSUE_TITLE

if [[ -z "$GITEA_ADMIN_PASSWORD" && -f /srv/admin/env/gitea.env ]]; then
  GITEA_ADMIN_PASSWORD="$(sed -n 's/^GITEA_ADMIN_PASSWORD=//p' /srv/admin/env/gitea.env | head -1)"
fi

if [[ -z "$GITEA_ADMIN_PASSWORD" ]]; then
  echo "[validate-gitea-data] GITEA_ADMIN_PASSWORD is required" >&2
  exit 1
fi

api() {
  local method="$1"
  local path="$2"
  local data="${3:-}"
  if [[ -n "$data" ]]; then
    curl -fsS -u "${GITEA_ADMIN_USER}:${GITEA_ADMIN_PASSWORD}" \
      -H "Content-Type: application/json" \
      -X "$method" \
      -d "$data" \
      "${GITEA_URL}${path}"
  else
    curl -fsS -u "${GITEA_ADMIN_USER}:${GITEA_ADMIN_PASSWORD}" \
      -X "$method" \
      "${GITEA_URL}${path}"
  fi
}

api_status() {
  local method="$1"
  local path="$2"
  curl -ksS -o /dev/null -w '%{http_code}' -u "${GITEA_ADMIN_USER}:${GITEA_ADMIN_PASSWORD}" \
    -X "$method" \
    "${GITEA_URL}${path}"
}

ensure_admin_auth() {
  local status
  status="$(api_status GET /api/v1/user)"
  if [[ "$status" == "200" ]]; then
    return 0
  fi

  if [[ "$status" != "401" && "$status" != "403" ]]; then
    echo "[validate-gitea-data] Gitea admin API check returned HTTP $status" >&2
    return 1
  fi

  if ! docker ps --format '{{.Names}}' | grep -qx gitea; then
    echo "[validate-gitea-data] Gitea admin API auth failed and container is unavailable" >&2
    return 1
  fi

  docker exec --user git gitea gitea admin user create \
    --admin \
    --must-change-password=false \
    --username "$GITEA_ADMIN_USER" \
    --password "$GITEA_ADMIN_PASSWORD" \
    --email "${GITEA_ADMIN_EMAIL:-admin@example.com}" \
    --config /data/gitea/conf/app.ini >/dev/null 2>&1 || true

  docker exec --user git gitea gitea admin user change-password \
    --username "$GITEA_ADMIN_USER" \
    --password "$GITEA_ADMIN_PASSWORD" \
    --must-change-password=false \
    --config /data/gitea/conf/app.ini >/dev/null

  status="$(api_status GET /api/v1/user)"
  if [[ "$status" != "200" ]]; then
    echo "[validate-gitea-data] Gitea admin API auth still failed after CLI reset: HTTP $status" >&2
    return 1
  fi
}

echo "[validate-gitea-data] checking Gitea API..."
for _ in $(seq 1 40); do
  if curl -fsS "${GITEA_URL}/api/v1/version" >/dev/null 2>&1; then
    break
  fi
  sleep 3
done
curl -fsS "${GITEA_URL}/api/v1/version" >/dev/null
ensure_admin_auth

repo_path="/api/v1/repos/${GITEA_ADMIN_USER}/${GITEA_VALIDATION_REPO}"
if ! api GET "$repo_path" >/dev/null 2>&1; then
  if [[ "$GITEA_VALIDATION_CREATE" != "true" ]]; then
    echo "[validate-gitea-data] validation repository not found: ${GITEA_ADMIN_USER}/${GITEA_VALIDATION_REPO}" >&2
    exit 1
  fi
  echo "[validate-gitea-data] creating validation repository..."
  api POST /api/v1/user/repos "$(python3 -c 'import json,os; print(json.dumps({"name": os.environ["GITEA_VALIDATION_REPO"], "private": True, "auto_init": True, "description": "Admin node backup/restore validation repository"}))')" >/dev/null
fi

api GET "$repo_path" >/dev/null

issues_json="$(api GET "${repo_path}/issues?state=all&limit=100")"
issue_exists="$(python3 -c 'import json,os,sys; title=os.environ["GITEA_VALIDATION_ISSUE_TITLE"]; print(any(i.get("title") == title for i in json.load(sys.stdin)))' <<< "$issues_json")"
if [[ "$issue_exists" != "True" ]]; then
  if [[ "$GITEA_VALIDATION_CREATE" != "true" ]]; then
    echo "[validate-gitea-data] validation issue not found: ${GITEA_VALIDATION_ISSUE_TITLE}" >&2
    exit 1
  fi
  echo "[validate-gitea-data] creating validation issue..."
  api POST "${repo_path}/issues" "$(python3 -c 'import json,os; print(json.dumps({"title": os.environ["GITEA_VALIDATION_ISSUE_TITLE"], "body": "Sentinel issue used to validate Gitea backup and restore."}))')" >/dev/null
fi

issues_json="$(api GET "${repo_path}/issues?state=all&limit=100")"
issue_exists="$(python3 -c 'import json,os,sys; title=os.environ["GITEA_VALIDATION_ISSUE_TITLE"]; print(any(i.get("title") == title for i in json.load(sys.stdin)))' <<< "$issues_json")"
if [[ "$issue_exists" != "True" ]]; then
  echo "[validate-gitea-data] validation issue not found after create/read" >&2
  exit 1
fi

echo "Gitea validation passed"
