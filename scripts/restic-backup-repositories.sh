#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${RESTIC_BACKUP_ENV_FILE:-/srv/admin/env/backup.env}"
if [[ -f "$ENV_FILE" ]]; then
  set -a
  # shellcheck source=/dev/null
  source "$ENV_FILE"
  set +a
fi

RESTIC_BACKUP_PATHS="${RESTIC_BACKUP_PATHS:-/srv/admin/stacks /srv/admin/env /srv/admin/data}"
RESTIC_DEFAULT_FORGET_ARGS="${RESTIC_DEFAULT_FORGET_ARGS:---keep-last 3 --prune}"
RESTIC_INIT_REPOSITORIES="${RESTIC_INIT_REPOSITORIES:-false}"
RESTIC_REQUIRE_SECURE_REPOSITORIES="${RESTIC_REQUIRE_SECURE_REPOSITORIES:-true}"

sanitize_repo_id() {
  local id="$1"
  id="${id^^}"
  id="${id//[^A-Z0-9_]/_}"
  printf '%s' "$id"
}

repo_var() {
  local name="$1"
  local safe_id="$2"
  local var="${name}_${safe_id}"
  printf '%s' "${!var:-}"
}

validate_secure_repository() {
  local repo="$1"
  if [[ "$RESTIC_REQUIRE_SECURE_REPOSITORIES" != "true" ]]; then
    return 0
  fi

  if [[ "$repo" != *:* ]]; then
    return 0
  fi

  case "$repo" in
    /*|.*)
      return 0
      ;;
    sftp:*|rest:https://*|s3:s3.*|s3:https://*|swift:*|b2:*|azure:*|gs:*)
      return 0
      ;;
    rest:http://*|s3:http://*|ftp:*)
      echo "[restic] refusing insecure repository URL: $repo" >&2
      return 1
      ;;
    rclone:*)
      echo "[restic] refusing rclone repository while RESTIC_REQUIRE_SECURE_REPOSITORIES=true: $repo" >&2
      echo "[restic] rclone can wrap insecure remotes; set RESTIC_REQUIRE_SECURE_REPOSITORIES=false only after auditing the remote." >&2
      return 1
      ;;
    *)
      echo "[restic] unsupported or insecure repository URL: $repo" >&2
      return 1
      ;;
  esac
}

export_backend_env() {
  local safe_id="$1"
  local backend_vars=(
    AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN AWS_DEFAULT_REGION
    RESTIC_REST_USERNAME RESTIC_REST_PASSWORD
    B2_ACCOUNT_ID B2_ACCOUNT_KEY
    GOOGLE_PROJECT_ID GOOGLE_APPLICATION_CREDENTIALS
    AZURE_ACCOUNT_NAME AZURE_ACCOUNT_KEY AZURE_ACCOUNT_SAS AZURE_CLIENT_ID AZURE_CLIENT_SECRET AZURE_TENANT_ID
    OS_AUTH_URL OS_REGION_NAME OS_USERNAME OS_PASSWORD OS_TENANT_ID OS_TENANT_NAME OS_USER_ID OS_USER_DOMAIN_NAME OS_USER_DOMAIN_ID OS_PROJECT_NAME OS_PROJECT_DOMAIN_NAME
    ST_AUTH ST_USER ST_KEY
  )

  local backend_var value
  for backend_var in "${backend_vars[@]}"; do
    unset "$backend_var"
    value="$(repo_var "$backend_var" "$safe_id")"
    if [[ -n "$value" ]]; then
      export "$backend_var=$value"
    fi
  done
}

run_restic_repo() {
  local id="$1"
  local safe_id repo password forget_args option_args
  safe_id="$(sanitize_repo_id "$id")"
  repo="$(repo_var RESTIC_REPOSITORY "$safe_id")"
  password="$(repo_var RESTIC_PASSWORD "$safe_id")"
  forget_args="$(repo_var RESTIC_FORGET_ARGS "$safe_id")"
  option_args="$(repo_var RESTIC_OPTIONS "$safe_id")"

  if [[ -z "$repo" ]]; then
    echo "[restic] RESTIC_REPOSITORY_${safe_id} is required" >&2
    return 1
  fi
  if [[ -z "$password" ]]; then
    echo "[restic] RESTIC_PASSWORD_${safe_id} is required" >&2
    return 1
  fi

  validate_secure_repository "$repo"

  export RESTIC_REPOSITORY="$repo"
  export RESTIC_PASSWORD="$password"
  export_backend_env "$safe_id"

  local restic_options=()
  if [[ -n "$option_args" ]]; then
    # shellcheck disable=SC2206
    restic_options=($option_args)
  fi

  if [[ "$RESTIC_INIT_REPOSITORIES" == "true" ]] && ! restic "${restic_options[@]}" cat config >/dev/null 2>&1; then
    echo "[restic] initializing repository '$id'"
    restic "${restic_options[@]}" init
  fi

  echo "[restic] backing up to repository '$id'"
  # shellcheck disable=SC2206
  local backup_paths=($RESTIC_BACKUP_PATHS)
  restic "${restic_options[@]}" backup "${backup_paths[@]}"

  if [[ -z "$forget_args" ]]; then
    forget_args="$RESTIC_DEFAULT_FORGET_ARGS"
  fi
  if [[ "$forget_args" != "none" ]]; then
    # shellcheck disable=SC2206
    local forget_words=($forget_args)
    restic "${restic_options[@]}" forget "${forget_words[@]}"
  fi
}

if ! command -v restic >/dev/null 2>&1; then
  echo "[restic] restic is not installed, skipping remote backups"
  exit 0
fi

if [[ -n "${RESTIC_REPOSITORIES:-}" ]]; then
  # shellcheck disable=SC2206
  repo_ids=($RESTIC_REPOSITORIES)
  for repo_id in "${repo_ids[@]}"; do
    run_restic_repo "$repo_id"
  done
elif [[ -n "${RESTIC_REPOSITORY:-}" ]]; then
  validate_secure_repository "$RESTIC_REPOSITORY"
  : "${RESTIC_PASSWORD:?RESTIC_PASSWORD is required when RESTIC_REPOSITORY is set}"
  echo "[restic] backing up to legacy RESTIC_REPOSITORY"
  # shellcheck disable=SC2206
  backup_paths=($RESTIC_BACKUP_PATHS)
  restic backup "${backup_paths[@]}"
  # shellcheck disable=SC2206
  forget_words=($RESTIC_DEFAULT_FORGET_ARGS)
  restic forget "${forget_words[@]}"
else
  echo "[restic] no repositories configured, skipping remote backup"
fi
