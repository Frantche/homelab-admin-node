#!/usr/bin/env bash
set -euo pipefail

ACTION="${1:?usage: disaster-recovery-sentinels.sh <create|validate> <state-file>}"
STATE_FILE="${2:?usage: disaster-recovery-sentinels.sh <create|validate> <state-file>}"
CA_FILE="${ADMIN_NODE_CA_FILE:-/srv/admin/certs/ca.pem}"
KEYCLOAK_URL="https://keycloak.example.com"
GITEA_URL="https://git.example.com"
HARBOR_URL="https://harbor.example.com"
HARBOR_REGISTRY="harbor.example.com"
OPENBAO_URL="https://bao.example.com"

umask 077

load_credentials() {
  set -a
  # shellcheck disable=SC1091
  source /srv/admin/env/keycloak.env
  # shellcheck disable=SC1091
  source /srv/admin/env/gitea.env
  # shellcheck disable=SC1091
  source /srv/admin/env/harbor.env
  set +a

  HARBOR_ADMIN_USER="${HARBOR_ADMIN_USER:-admin}"
  OPENBAO_TOKEN="$(
    SOPS_AGE_KEY_FILE=/etc/sops/age/keys.txt \
      sops --decrypt --output-type json \
        /etc/admin-config/homelab-node-admin-config/hosts/group_vars/secrets.sops.yaml |
      jq -er '[
        .openbao.root_token,
        .openbao_config.root_token
      ] | map(select(type == "string" and length > 0)) | first //
        error("OpenBao root token is missing")'
  )"
}

keycloak_token() {
  curl --fail --silent --show-error --cacert "$CA_FILE" \
    -X POST "$KEYCLOAK_URL/realms/master/protocol/openid-connect/token" \
    --data-urlencode grant_type=password \
    --data-urlencode client_id=admin-cli \
    --data-urlencode "username=$KEYCLOAK_ADMIN" \
    --data-urlencode "password=$KEYCLOAK_ADMIN_PASSWORD" |
    jq -er .access_token
}

create_sentinels() {
  local token short_token keycloak_access_token keycloak_username keycloak_id
  local gitea_access_token gitea_repo gitea_repo_id gitea_file gitea_file_sha
  local gitea_issue_title
  local harbor_project harbor_tag harbor_digest openbao_mount openbao_path openbao_version
  local docker_config

  [[ ! -e "$STATE_FILE" ]] || {
    echo "sentinel state already exists: $STATE_FILE" >&2
    return 1
  }

  token="$(tr -d '-' </proc/sys/kernel/random/uuid)"
  short_token="${token:0:16}"

  echo "Creating Keycloak sentinel"
  keycloak_access_token="$(keycloak_token)"
  keycloak_username="dr-sentinel-$short_token"
  curl --fail --silent --show-error --cacert "$CA_FILE" \
    -X POST "$KEYCLOAK_URL/admin/realms/homelab/users" \
    -H "Authorization: Bearer $keycloak_access_token" \
    -H "Content-Type: application/json" \
    --data "$(jq -cn \
      --arg username "$keycloak_username" \
      --arg token "$token" \
      '{
        username: $username,
        firstName: $token,
        lastName: "Disaster recovery sentinel",
        enabled: false
      }')"
  keycloak_id="$(
    curl --fail --silent --show-error --cacert "$CA_FILE" \
      -H "Authorization: Bearer $keycloak_access_token" \
      "$KEYCLOAK_URL/admin/realms/homelab/users?username=$keycloak_username&exact=true" |
      jq -er --arg username "$keycloak_username" \
        'if length == 1 and .[0].username == $username then .[0].id else error("Keycloak sentinel lookup mismatch") end'
  )"

  echo "Creating Gitea sentinel"
  gitea_access_token="$(
    docker exec --user git gitea gitea admin user generate-access-token \
      --username "$GITEA_ADMIN_USER" \
      --token-name "dr-sentinel-$short_token" \
      --scopes all \
      --raw \
      --config /data/gitea/conf/app.ini
  )"
  gitea_repo="dr-sentinel-$short_token"
  gitea_file="sentinel.txt"
  gitea_issue_title="Disaster recovery sentinel $short_token"
  gitea_repo_id="$(
    curl --fail --silent --show-error --cacert "$CA_FILE" \
      -H "Authorization: token $gitea_access_token" \
      -X POST "$GITEA_URL/api/v1/user/repos" \
      -H "Content-Type: application/json" \
      --data "$(jq -cn --arg name "$gitea_repo" \
        '{name: $name, private: true, auto_init: true, description: "Immutable disaster recovery sentinel"}')" |
      jq -er .id
  )"
  gitea_file_sha="$(
    curl --fail --silent --show-error --cacert "$CA_FILE" \
      -H "Authorization: token $gitea_access_token" \
      -X POST "$GITEA_URL/api/v1/repos/$GITEA_ADMIN_USER/$gitea_repo/contents/$gitea_file" \
      -H "Content-Type: application/json" \
      --data "$(jq -cn \
        --arg content "$(printf '%s' "$token" | base64 -w0)" \
        '{message: "Add disaster recovery sentinel", content: $content}')" |
      jq -er '.content.sha'
  )"
  curl --fail --silent --show-error --cacert "$CA_FILE" \
    -H "Authorization: token $gitea_access_token" \
    -X POST "$GITEA_URL/api/v1/repos/$GITEA_ADMIN_USER/$gitea_repo/issues" \
    -H "Content-Type: application/json" \
    --data "$(jq -cn --arg title "$gitea_issue_title" --arg token "$token" \
      '{title: $title, body: ("Immutable disaster recovery token: " + $token)}')" \
    >/dev/null

  echo "Creating Harbor sentinel"
  harbor_project="dr-sentinel-$short_token"
  harbor_tag="$short_token"
  curl --fail --silent --show-error --cacert "$CA_FILE" \
    -u "$HARBOR_ADMIN_USER:$HARBOR_ADMIN_PASSWORD" \
    -X POST "$HARBOR_URL/api/v2.0/projects" \
    -H "Content-Type: application/json" \
    --data "$(jq -cn --arg name "$harbor_project" \
      '{project_name: $name, metadata: {public: "false"}}')"

  docker_config="$(mktemp -d /tmp/dr-sentinel-docker.XXXXXX)"
  trap 'rm -rf "$docker_config"' RETURN
  printf '%s' "$HARBOR_ADMIN_PASSWORD" |
    DOCKER_CONFIG="$docker_config" docker login "$HARBOR_REGISTRY" \
      --username "$HARBOR_ADMIN_USER" --password-stdin >/dev/null
  DOCKER_CONFIG="$docker_config" docker pull \
    "$HARBOR_REGISTRY/dockerhub/library/busybox@sha256:1cfa4e2b09e127b9c4ed43578d3f3c18e7d44ea47b9ea98475c0cbe9086525f8" \
    >/dev/null
  docker tag \
    "$HARBOR_REGISTRY/dockerhub/library/busybox@sha256:1cfa4e2b09e127b9c4ed43578d3f3c18e7d44ea47b9ea98475c0cbe9086525f8" \
    "$HARBOR_REGISTRY/$harbor_project/sentinel:$harbor_tag"
  DOCKER_CONFIG="$docker_config" docker push \
    "$HARBOR_REGISTRY/$harbor_project/sentinel:$harbor_tag" >/dev/null
  rm -rf "$docker_config"
  trap - RETURN
  harbor_digest="$(
    curl --fail --silent --show-error --cacert "$CA_FILE" \
      -u "$HARBOR_ADMIN_USER:$HARBOR_ADMIN_PASSWORD" \
      "$HARBOR_URL/api/v2.0/projects/$harbor_project/repositories/sentinel/artifacts/$harbor_tag" |
      jq -er .digest
  )"

  echo "Creating OpenBao sentinel"
  openbao_mount="dr-sentinel-$short_token"
  openbao_path="value"
  curl --fail --silent --show-error --cacert "$CA_FILE" \
    -X POST "$OPENBAO_URL/v1/sys/mounts/$openbao_mount" \
    -H "X-Vault-Token: $OPENBAO_TOKEN" \
    -H "Content-Type: application/json" \
    --data '{"type":"kv","options":{"version":"2"}}'
  openbao_version="$(
    curl --fail --silent --show-error --cacert "$CA_FILE" \
      -X POST "$OPENBAO_URL/v1/$openbao_mount/data/$openbao_path" \
      -H "X-Vault-Token: $OPENBAO_TOKEN" \
      -H "Content-Type: application/json" \
      --data "$(jq -cn --arg token "$token" '{data: {token: $token}}')" |
      jq -er '.data.version'
  )"

  jq -n \
    --arg token "$token" \
    --arg keycloak_username "$keycloak_username" \
    --arg keycloak_id "$keycloak_id" \
    --arg gitea_owner "$GITEA_ADMIN_USER" \
    --arg gitea_access_token "$gitea_access_token" \
    --arg gitea_repo "$gitea_repo" \
    --arg gitea_repo_id "$gitea_repo_id" \
    --arg gitea_file "$gitea_file" \
    --arg gitea_file_sha "$gitea_file_sha" \
    --arg gitea_issue_title "$gitea_issue_title" \
    --arg harbor_project "$harbor_project" \
    --arg harbor_repository "sentinel" \
    --arg harbor_tag "$harbor_tag" \
    --arg harbor_digest "$harbor_digest" \
    --arg openbao_mount "$openbao_mount" \
    --arg openbao_path "$openbao_path" \
    --argjson openbao_version "$openbao_version" \
    '{
      version: 1,
      token: $token,
      keycloak: {username: $keycloak_username, id: $keycloak_id},
      gitea: {
        owner: $gitea_owner,
        access_token: $gitea_access_token,
        repository: $gitea_repo,
        repository_id: $gitea_repo_id,
        file: $gitea_file,
        file_sha: $gitea_file_sha,
        issue_title: $gitea_issue_title
      },
      harbor: {
        project: $harbor_project,
        repository: $harbor_repository,
        tag: $harbor_tag,
        digest: $harbor_digest
      },
      openbao: {
        mount: $openbao_mount,
        path: $openbao_path,
        version: $openbao_version
      }
    }' >"$STATE_FILE"
  chmod 0600 "$STATE_FILE"
  echo "Immutable disaster recovery sentinels created"
}

validate_sentinels() {
  local token keycloak_access_token keycloak_username keycloak_id
  local gitea_access_token gitea_owner gitea_repo gitea_repo_id gitea_file
  local gitea_file_sha gitea_issue_title
  local harbor_project harbor_repository harbor_tag harbor_digest
  local openbao_mount openbao_path openbao_version

  jq -e '.version == 1' "$STATE_FILE" >/dev/null
  token="$(jq -er .token "$STATE_FILE")"

  keycloak_username="$(jq -er .keycloak.username "$STATE_FILE")"
  keycloak_id="$(jq -er .keycloak.id "$STATE_FILE")"
  keycloak_access_token="$(keycloak_token)"
  echo "Validating Keycloak sentinel"
  curl --fail --silent --show-error --cacert "$CA_FILE" \
    -H "Authorization: Bearer $keycloak_access_token" \
    "$KEYCLOAK_URL/admin/realms/homelab/users/$keycloak_id" |
    jq -e \
      --arg username "$keycloak_username" \
      --arg id "$keycloak_id" \
      --arg token "$token" \
      '.username == $username and
       .id == $id and
       .enabled == false and
       .firstName == $token and
       .lastName == "Disaster recovery sentinel"' >/dev/null

  echo "Validating Gitea sentinel"
  gitea_access_token="$(jq -er .gitea.access_token "$STATE_FILE")"
  gitea_owner="$(jq -er .gitea.owner "$STATE_FILE")"
  gitea_repo="$(jq -er .gitea.repository "$STATE_FILE")"
  gitea_repo_id="$(jq -er .gitea.repository_id "$STATE_FILE")"
  gitea_file="$(jq -er .gitea.file "$STATE_FILE")"
  gitea_file_sha="$(jq -er .gitea.file_sha "$STATE_FILE")"
  gitea_issue_title="$(jq -er .gitea.issue_title "$STATE_FILE")"
  curl --fail --silent --show-error --cacert "$CA_FILE" \
    -H "Authorization: token $gitea_access_token" \
    "$GITEA_URL/api/v1/repos/$gitea_owner/$gitea_repo" |
    jq -e --argjson id "$gitea_repo_id" \
      --arg owner "$gitea_owner" --arg name "$gitea_repo" \
      '.id == $id and .name == $name and .owner.login == $owner and .private == true' >/dev/null
  curl --fail --silent --show-error --cacert "$CA_FILE" \
    -H "Authorization: token $gitea_access_token" \
    "$GITEA_URL/api/v1/repos/$gitea_owner/$gitea_repo/contents/$gitea_file" |
    jq -e --arg sha "$gitea_file_sha" --arg token "$token" \
      '.sha == $sha and ((.content | gsub("\\n"; "") | @base64d) == $token)' >/dev/null
  curl --fail --silent --show-error --cacert "$CA_FILE" \
    -H "Authorization: token $gitea_access_token" \
    "$GITEA_URL/api/v1/repos/$gitea_owner/$gitea_repo/issues?state=all&limit=100" |
    jq -e --arg title "$gitea_issue_title" --arg token "$token" \
      'any(.[]; .title == $title and .body == ("Immutable disaster recovery token: " + $token))' >/dev/null

  harbor_project="$(jq -er .harbor.project "$STATE_FILE")"
  harbor_repository="$(jq -er .harbor.repository "$STATE_FILE")"
  harbor_tag="$(jq -er .harbor.tag "$STATE_FILE")"
  harbor_digest="$(jq -er .harbor.digest "$STATE_FILE")"
  echo "Validating Harbor sentinel"
  curl --fail --silent --show-error --cacert "$CA_FILE" \
    -u "$HARBOR_ADMIN_USER:$HARBOR_ADMIN_PASSWORD" \
    "$HARBOR_URL/api/v2.0/projects/$harbor_project" |
    jq -e --arg name "$harbor_project" \
      '.name == $name and .metadata.public == "false"' >/dev/null
  curl --fail --silent --show-error --cacert "$CA_FILE" \
    -u "$HARBOR_ADMIN_USER:$HARBOR_ADMIN_PASSWORD" \
    "$HARBOR_URL/api/v2.0/projects/$harbor_project/repositories/$harbor_repository/artifacts/$harbor_tag" |
    jq -e --arg digest "$harbor_digest" '.digest == $digest' >/dev/null

  openbao_mount="$(jq -er .openbao.mount "$STATE_FILE")"
  openbao_path="$(jq -er .openbao.path "$STATE_FILE")"
  openbao_version="$(jq -er .openbao.version "$STATE_FILE")"
  echo "Validating OpenBao sentinel"
  curl --fail --silent --show-error --cacert "$CA_FILE" \
    -H "X-Vault-Token: $OPENBAO_TOKEN" \
    "$OPENBAO_URL/v1/sys/mounts" |
    jq -e --arg mount "$openbao_mount/" \
      '.data[$mount].type == "kv" and .data[$mount].options.version == "2"' >/dev/null
  curl --fail --silent --show-error --cacert "$CA_FILE" \
    -H "X-Vault-Token: $OPENBAO_TOKEN" \
    "$OPENBAO_URL/v1/$openbao_mount/data/$openbao_path" |
    jq -e --arg token "$token" --argjson version "$openbao_version" \
      '.data.data.token == $token and .data.metadata.version == $version' >/dev/null

  echo "Immutable disaster recovery sentinels validated read-only"
}

load_credentials
case "$ACTION" in
  create) create_sentinels ;;
  validate) validate_sentinels ;;
  *) echo "unknown action: $ACTION" >&2; exit 2 ;;
esac
