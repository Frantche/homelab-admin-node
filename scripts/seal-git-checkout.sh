#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -ne 1 ]]; then
  echo "Usage: $0 REPOSITORY" >&2
  exit 2
fi

repository="$(realpath "$1")"
if [[ ! -d "$repository/.git" ]]; then
  echo "Git repository not found: $repository" >&2
  exit 1
fi

chown root:root "$repository"
chmod 0755 "$repository"

while IFS= read -r -d '' entry; do
  metadata="${entry%%$'\t'*}"
  relative_path="${entry#*$'\t'}"
  index_mode="${metadata%% *}"
  path="$repository/$relative_path"

  parent="$(dirname "$path")"
  while [[ "$parent" != "$repository" ]]; do
    chown root:root "$parent"
    chmod 0755 "$parent"
    parent="$(dirname "$parent")"
  done

  case "$index_mode" in
    100644)
      chown root:root "$path"
      chmod 0644 "$path"
      ;;
    100755)
      chown root:root "$path"
      chmod 0755 "$path"
      ;;
    120000)
      chown -h root:root "$path"
      ;;
    160000)
      ;;
    *)
      echo "Unsupported Git index mode $index_mode for $relative_path" >&2
      exit 1
      ;;
  esac
done < <(git -C "$repository" ls-files --stage -z)

chown -R root:root "$repository/.git"
find "$repository/.git" -type d -exec chmod 0700 {} +
find "$repository/.git" -type f -exec chmod 0600 {} +
