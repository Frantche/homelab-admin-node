#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "Usage: $0 /path/to/age-private-key.txt" >&2
  exit 1
fi

SRC="$1"
DST=/etc/sops/age/keys.txt

if [[ ! -f "$SRC" ]]; then
  echo "Input key file does not exist: $SRC" >&2
  exit 1
fi

install -d -m 0700 -o root -g root /etc/sops/age
install -m 0400 -o root -g root "$SRC" "$DST"

echo "Age key installed at $DST with 0400 root:root"
