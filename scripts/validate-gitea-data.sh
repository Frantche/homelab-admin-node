#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GITEA_DOMAIN="${GITEA_DOMAIN:-$("$REPO_ROOT/ci/service-domains.py" get gitea)}"
GITEA_URL="${GITEA_URL:-https://${GITEA_DOMAIN}}"
GITEA_ADMIN_USER="${GITEA_ADMIN_USER:-admin}"
GITEA_ADMIN_PASSWORD="${GITEA_ADMIN_PASSWORD:-}"
GITEA_VALIDATION_REPO="${GITEA_VALIDATION_REPO:-admin-node-validation}"
GITEA_VALIDATION_ISSUE_TITLE="${GITEA_VALIDATION_ISSUE_TITLE:-Backup restore sentinel}"

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

echo "[validate-gitea-data] checking Gitea API..."
for _ in $(seq 1 40); do
  if curl -fsS "${GITEA_URL}/api/v1/version" >/dev/null 2>&1; then
    break
  fi
  sleep 3
done
curl -fsS "${GITEA_URL}/api/v1/version" >/dev/null

repo_path="/repos/${GITEA_ADMIN_USER}/${GITEA_VALIDATION_REPO}"
if ! api GET "$repo_path" >/dev/null 2>&1; then
  echo "[validate-gitea-data] creating validation repository..."
  api POST /user/repos "$(python3 -c 'import json,os; print(json.dumps({"name": os.environ["GITEA_VALIDATION_REPO"], "private": True, "auto_init": True, "description": "Admin node backup/restore validation repository"}))')" >/dev/null
fi

api GET "$repo_path" >/dev/null

issues_json="$(api GET "${repo_path}/issues?state=all&limit=100")"
issue_exists="$(python3 -c 'import json,os,sys; title=os.environ["GITEA_VALIDATION_ISSUE_TITLE"]; print(any(i.get("title") == title for i in json.load(sys.stdin)))' <<< "$issues_json")"
if [[ "$issue_exists" != "True" ]]; then
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
