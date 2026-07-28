#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_BASE="${TMPDIR:-/tmp}"
TEST_ROOT="$(mktemp -d "$TEST_BASE/admin-node-go-cache-test.XXXXXX")"
TEST_REPO="$TEST_ROOT/repo"
TEST_GO_CACHE="$TEST_ROOT/cache/go-build"
TEST_GO_MOD_CACHE="$TEST_ROOT/cache/go-mod"
TEST_GO_PATH="$TEST_ROOT/cache/go-path"

cleanup() {
  if [[ "$TEST_ROOT" != "$TEST_BASE"/admin-node-go-cache-test.* ]]; then
    echo "Refusing to remove unexpected test directory: $TEST_ROOT" >&2
    return 1
  fi
  sudo rm -rf "$TEST_ROOT"
}
trap cleanup EXIT

mkdir -p "$TEST_REPO"
cp -a \
  "$REPO_ROOT/go.mod" \
  "$REPO_ROOT/cmd" \
  "$REPO_ROOT/internal" \
  "$REPO_ROOT/scripts" \
  "$TEST_REPO/"

first_output="$(
  sudo env -i \
    PATH="$PATH" \
    ADMIN_NODE_GO_CACHE="$TEST_GO_CACHE" \
    ADMIN_NODE_GO_MOD_CACHE="$TEST_GO_MOD_CACHE" \
    ADMIN_NODE_GO_PATH="$TEST_GO_PATH" \
    "$TEST_REPO/scripts/build-admin-node.sh"
)"
grep -Fq "changed=true" <<<"$first_output"

second_output="$(
  sudo env -i \
    PATH="$PATH" \
    ADMIN_NODE_GO_CACHE="$TEST_GO_CACHE" \
    ADMIN_NODE_GO_MOD_CACHE="$TEST_GO_MOD_CACHE" \
    ADMIN_NODE_GO_PATH="$TEST_GO_PATH" \
    "$TEST_REPO/scripts/build-admin-node.sh"
)"
grep -Fq "changed=false" <<<"$second_output"

for cache_dir in "$TEST_GO_CACHE" "$TEST_GO_MOD_CACHE" "$TEST_GO_PATH"; do
  [[ "$(sudo stat -c '%a:%U:%G' "$cache_dir")" == "700:root:root" ]]
done
