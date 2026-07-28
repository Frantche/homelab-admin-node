#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_BASE="${TMPDIR:-/tmp}"
TEST_ROOT="$(mktemp -d "$TEST_BASE/admin-node-repo-permissions-test.XXXXXX")"
TEST_REPO="$TEST_ROOT/repo"
TEST_GROUP="$(id -gn)"

cleanup() {
  if [[ "$TEST_ROOT" != "$TEST_BASE"/admin-node-repo-permissions-test.* ]]; then
    echo "Refusing to remove unexpected test directory: $TEST_ROOT" >&2
    return 1
  fi
  sudo rm -rf "$TEST_ROOT"
}
trap cleanup EXIT

git init --quiet --initial-branch=main "$TEST_REPO"
git -C "$TEST_REPO" config user.email ci@example.test
git -C "$TEST_REPO" config user.name CI

mkdir -p "$TEST_REPO/nested" "$TEST_REPO/secrets"
printf '%s\n' regular >"$TEST_REPO/nested/regular.txt"
printf '#!/usr/bin/env bash\nexit 0\n' >"$TEST_REPO/nested/tool.sh"
printf '%s\n' private >"$TEST_REPO/secrets/runtime.token"
chmod 0700 "$TEST_REPO/nested"
chmod 0600 "$TEST_REPO/nested/regular.txt"
chmod 0700 "$TEST_REPO/nested/tool.sh"
chmod 0600 "$TEST_REPO/secrets/runtime.token"
git -C "$TEST_REPO" add nested
git -C "$TEST_REPO" commit --quiet -m "test repository"

sudo "$REPO_ROOT/scripts/normalize-repo-permissions.sh" \
  "$TEST_REPO" root "$TEST_GROUP"

[[ "$(sudo stat -c '%a:%U:%G' "$TEST_REPO")" == "2775:root:$TEST_GROUP" ]]
[[ "$(sudo stat -c '%a:%U:%G' "$TEST_REPO/nested")" == "2775:root:$TEST_GROUP" ]]
[[ "$(sudo stat -c '%a:%U:%G' "$TEST_REPO/nested/regular.txt")" == "664:root:$TEST_GROUP" ]]
[[ "$(sudo stat -c '%a:%U:%G' "$TEST_REPO/nested/tool.sh")" == "775:root:$TEST_GROUP" ]]
[[ "$(stat -c '%a:%U:%G' "$TEST_REPO/secrets/runtime.token")" == "600:$(id -un):$TEST_GROUP" ]]
sudo git -c "safe.directory=$TEST_REPO" -C "$TEST_REPO" diff --quiet
