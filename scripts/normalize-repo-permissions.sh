#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -ne 3 ]]; then
  echo "Usage: $0 REPOSITORY OWNER GROUP" >&2
  exit 2
fi

repository="$(realpath "$1")"
owner="$2"
group="$3"

if [[ ! -d "$repository/.git" ]]; then
  echo "Git repository not found: $repository" >&2
  exit 1
fi
if ! getent passwd "$owner" >/dev/null; then
  echo "Unknown repository owner: $owner" >&2
  exit 1
fi
if ! getent group "$group" >/dev/null; then
  echo "Unknown repository group: $group" >&2
  exit 1
fi

normalize_directory() {
  local directory="$1"
  chown "$owner:$group" "$directory"
  chmod 2775 "$directory"
}

normalize_directory "$repository"

while IFS= read -r -d '' entry; do
  metadata="${entry%%$'\t'*}"
  relative_path="${entry#*$'\t'}"
  index_mode="${metadata%% *}"
  path="$repository/$relative_path"

  parent="$(dirname "$path")"
  while [[ "$parent" != "$repository" ]]; do
    if [[ "$parent" != "$repository/"* ]]; then
      echo "Tracked path escapes repository: $relative_path" >&2
      exit 1
    fi
    normalize_directory "$parent"
    parent="$(dirname "$parent")"
  done

  case "$index_mode" in
    100644)
      chown "$owner:$group" "$path"
      chmod 0664 "$path"
      ;;
    100755)
      chown "$owner:$group" "$path"
      chmod 0775 "$path"
      ;;
    120000)
      chown -h "$owner:$group" "$path"
      ;;
    160000)
      ;;
    *)
      echo "Unsupported Git index mode $index_mode for $relative_path" >&2
      exit 1
      ;;
  esac
done < <(git -C "$repository" ls-files --stage -z)
