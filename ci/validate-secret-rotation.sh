#!/usr/bin/env bash
set -euo pipefail

audit_file="${1:-/tmp/admin-node-secret-rotation-audit.json}"
ca_file="${ADMIN_NODE_CA_FILE:-/srv/admin/certs/ca.pem}"

secret() {
  local generation="$1"
  local path="$2"
  jq -er --arg generation "$generation" --arg path "$path" \
    '.[$generation][$path]' "$audit_file"
}

expect_http_auth() {
  local label="$1"
  local url="$2"
  local user="$3"
  local old_password="$4"
  local new_password="$5"
  local old_status new_status

  old_status="$(curl --cacert "$ca_file" -sS -o /dev/null -w '%{http_code}' -u "$user:$old_password" "$url" || true)"
  new_status="$(curl --cacert "$ca_file" -sS -o /dev/null -w '%{http_code}' -u "$user:$new_password" "$url" || true)"
  if [[ "$old_status" != "401" && "$old_status" != "403" ]]; then
    echo "ERROR: $label still accepts the previous administrator password (HTTP $old_status)" >&2
    return 1
  fi
  if [[ ! "$new_status" =~ ^2 ]]; then
    echo "ERROR: $label rejects the rotated administrator password (HTTP $new_status)" >&2
    return 1
  fi
}

expect_db_password() {
  local label="$1"
  local container="$2"
  local network="$3"
  local user="$4"
  local database="$5"
  local old_password="$6"
  local new_password="$7"
  local image

  image="$(docker inspect --format '{{.Config.Image}}' "$container")"

  if docker run --rm --network "$network" -e "PGPASSWORD=$old_password" "$image" \
    psql -h "$container" -U "$user" -d "$database" -Atqc "select 1" >/dev/null 2>&1; then
    echo "ERROR: $label still accepts the previous database password" >&2
    return 1
  fi
  docker run --rm --network "$network" -e "PGPASSWORD=$new_password" "$image" \
    psql -h "$container" -U "$user" -d "$database" -Atqc "select 1" >/dev/null
}

old_keycloak_admin="$(secret old keycloak.admin_password)"
new_keycloak_admin="$(secret new keycloak.admin_password)"
old_harbor_admin="$(secret old harbor.admin_password)"
new_harbor_admin="$(secret new harbor.admin_password)"
old_gitea_admin="$(secret old gitea.admin_password)"
new_gitea_admin="$(secret new gitea.admin_password)"

old_status="$(curl --cacert "$ca_file" -sS -o /dev/null -w '%{http_code}' \
  -X POST "https://keycloak.example.com/realms/master/protocol/openid-connect/token" \
  --data-urlencode grant_type=password \
  --data-urlencode client_id=admin-cli \
  --data-urlencode username=admin \
  --data-urlencode "password=$old_keycloak_admin" || true)"
new_token="$(curl --fail --cacert "$ca_file" -sS \
  -X POST "https://keycloak.example.com/realms/master/protocol/openid-connect/token" \
  --data-urlencode grant_type=password \
  --data-urlencode client_id=admin-cli \
  --data-urlencode username=admin \
  --data-urlencode "password=$new_keycloak_admin" | jq -er .access_token)"
if [[ "$old_status" != "400" && "$old_status" != "401" ]]; then
  echo "ERROR: Keycloak still accepts the previous administrator password (HTTP $old_status)" >&2
  exit 1
fi

expect_http_auth Harbor \
  "https://harbor.example.com/api/v2.0/users/current" \
  admin "$old_harbor_admin" "$new_harbor_admin"
expect_http_auth Gitea \
  "https://git.example.com/api/v1/user" \
  admin "$old_gitea_admin" "$new_gitea_admin"

expect_db_password Keycloak keycloak-db keycloak-db keycloak keycloak \
  "$(secret old keycloak.db_password)" "$(secret new keycloak.db_password)"
expect_db_password Gitea gitea-db gitea-db gitea gitea \
  "$(secret old gitea.db_password)" "$(secret new gitea.db_password)"
expect_db_password Harbor harbor-db harbor-internal postgres registry \
  "$(secret old harbor.db_password)" "$(secret new harbor.db_password)"

for client in harbor openbao gitea; do
  client_uuid="$(curl --fail --cacert "$ca_file" -sS \
    -H "Authorization: Bearer $new_token" \
    "https://keycloak.example.com/admin/realms/homelab/clients?clientId=$client" |
    jq -er '.[0].id')"
  configured_secret="$(curl --fail --cacert "$ca_file" -sS \
    -H "Authorization: Bearer $new_token" \
    "https://keycloak.example.com/admin/realms/homelab/clients/$client_uuid/client-secret" |
    jq -er .value)"
  old_secret="$(secret old "vault_oidc_${client}_client_secret")"
  new_secret="$(secret new "vault_oidc_${client}_client_secret")"
  if [[ "$configured_secret" == "$old_secret" || "$configured_secret" != "$new_secret" ]]; then
    echo "ERROR: Keycloak $client client secret was not rotated" >&2
    exit 1
  fi
done

echo "Technical secret rotation validated; OIDC user passwords were preserved"
