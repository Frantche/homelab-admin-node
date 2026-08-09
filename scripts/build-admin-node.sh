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

if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
  admin_node_go_cache_default="/var/cache/admin-node/go-build"
  admin_node_go_mod_cache_default="/var/cache/admin-node/go-mod"
  admin_node_go_path_default="/var/cache/admin-node/go-path"
  export GOCACHE="${GOCACHE:-${ADMIN_NODE_GO_CACHE:-$admin_node_go_cache_default}}"
  export GOMODCACHE="${GOMODCACHE:-${ADMIN_NODE_GO_MOD_CACHE:-$admin_node_go_mod_cache_default}}"
  export GOPATH="${GOPATH:-${ADMIN_NODE_GO_PATH:-$admin_node_go_path_default}}"
  install -d -m 0700 "$GOCACHE" "$GOMODCACHE" "$GOPATH"
elif [[ -z "${XDG_CACHE_HOME:-}" && -z "${HOME:-}" ]]; then
  admin_node_go_cache_default="${TMPDIR:-/tmp}/admin-node-go-build-${UID:-$(id -u)}"
  admin_node_go_mod_cache_default="${TMPDIR:-/tmp}/admin-node-go-mod-${UID:-$(id -u)}"
  admin_node_go_path_default="${TMPDIR:-/tmp}/admin-node-go-path-${UID:-$(id -u)}"
  export GOCACHE="${GOCACHE:-${ADMIN_NODE_GO_CACHE:-$admin_node_go_cache_default}}"
  export GOMODCACHE="${GOMODCACHE:-${ADMIN_NODE_GO_MOD_CACHE:-$admin_node_go_mod_cache_default}}"
  export GOPATH="${GOPATH:-${ADMIN_NODE_GO_PATH:-$admin_node_go_path_default}}"
  install -d -m 0700 "$GOCACHE" "$GOMODCACHE" "$GOPATH"
fi

cd "$REPO_DIR"
mkdir -p "$BIN_DIR"

required_go_version="$(awk '$1 == "go" { print $2; exit }' go.mod)"
actual_go_version="$("$GO_BIN" env GOVERSION)"
if [[ -z "$required_go_version" || "$actual_go_version" != "go$required_go_version" ]]; then
  echo "Go toolchain mismatch: go.mod requires $required_go_version, selected toolchain is $actual_go_version" >&2
  exit 2
fi

TMP_BIN="$(mktemp "$BIN_DIR/admin-node.tmp.XXXXXX")"
TMP_HASH="$(mktemp "$BIN_DIR/admin-node.source.sha256.tmp.XXXXXX")"

cleanup() {
  rm -f "$TMP_BIN" "$TMP_HASH"
}
trap cleanup EXIT

source_files_hash="$(
  {
    printf '%s\0' go.mod
    printf '%s\0' scripts/build-admin-node.sh
    if [[ -f go.sum ]]; then
      printf '%s\0' go.sum
    fi
    find cmd internal -type f -name '*.go' -print0
  } | sort -z | xargs -0 sha256sum | sha256sum | awk '{print $1}'
)"
toolchain_fingerprint="$(
  {
    "$GO_BIN" version
    "$GO_BIN" env GOOS GOARCH CGO_ENABLED GOFLAGS GOEXPERIMENT
  } | sha256sum | awk '{print $1}'
)"
source_hash="$(printf '%s\0%s\0' "$source_files_hash" "$toolchain_fingerprint" | sha256sum | awk '{print $1}')"

if [[ -x "$BIN_PATH" && -f "$HASH_PATH" ]] && [[ "$(cat "$HASH_PATH")" == "$source_hash" ]]; then
  echo "admin-node build: up to date"
  echo "changed=false"
  exit 0
fi

echo "admin-node build: compiling"
"$GO_BIN" build -buildvcs=false -mod=readonly -o "$TMP_BIN" ./cmd/admin-node
"$TMP_BIN" --help >/dev/null
printf '%s\n' "$source_hash" > "$TMP_HASH"
chmod 0750 "$TMP_BIN"
chmod 0640 "$TMP_HASH"
mv "$TMP_BIN" "$BIN_PATH"
mv "$TMP_HASH" "$HASH_PATH"
echo "admin-node build: updated $BIN_PATH"
echo "changed=true"
