#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BIN_DIR="$REPO_DIR/bin"
BIN_PATH="$BIN_DIR/admin-node"
HASH_PATH="$BIN_DIR/admin-node.source.sha256"
GO_BIN="${GO_BIN:-go}"

if ! command -v "$GO_BIN" >/dev/null 2>&1; then
  echo "go binary not found: $GO_BIN. Install Go before building admin-node." >&2
  exit 127
fi

cd "$REPO_DIR"
mkdir -p "$BIN_DIR"
TMP_BIN="$(mktemp "$BIN_DIR/admin-node.tmp.XXXXXX")"
TMP_HASH="$(mktemp "$BIN_DIR/admin-node.source.sha256.tmp.XXXXXX")"

cleanup() {
  rm -f "$TMP_BIN" "$TMP_HASH"
}
trap cleanup EXIT

source_hash="$(
  {
    printf '%s\0' go.mod
    if [[ -f go.sum ]]; then
      printf '%s\0' go.sum
    fi
    find cmd internal -type f -name '*.go' -print0
  } | sort -z | xargs -0 sha256sum | sha256sum | awk '{print $1}'
)"

if [[ -x "$BIN_PATH" && -f "$HASH_PATH" ]] && [[ "$(cat "$HASH_PATH")" == "$source_hash" ]]; then
  echo "admin-node build: up to date"
  echo "changed=false"
  exit 0
fi

echo "admin-node build: compiling"
"$GO_BIN" build -mod=readonly -o "$TMP_BIN" ./cmd/admin-node
"$TMP_BIN" --help >/dev/null
printf '%s\n' "$source_hash" > "$TMP_HASH"
mv "$TMP_BIN" "$BIN_PATH"
mv "$TMP_HASH" "$HASH_PATH"
echo "admin-node build: updated $BIN_PATH"
echo "changed=true"
