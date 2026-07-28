#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_ROOT="$(mktemp -d)"

cleanup() {
  sudo chown -R "$(id -u):$(id -g)" "$TEST_ROOT" 2>/dev/null || true
  rm -rf "$TEST_ROOT"
}
trap cleanup EXIT

chmod 0755 "$TEST_ROOT"
git clone --quiet "$REPO_ROOT" "$TEST_ROOT/runtime"
sudo "$REPO_ROOT/scripts/seal-git-checkout.sh" "$TEST_ROOT/runtime"

[[ "$(stat -c '%U:%G:%a' "$TEST_ROOT/runtime")" == "root:root:755" ]]
[[ "$(stat -c '%U:%G:%a' "$TEST_ROOT/runtime/.git")" == "root:root:700" ]]
if sudo -u nobody test -w "$TEST_ROOT/runtime/internal/converge/converge.go"; then
  echo "An unprivileged operator can modify trusted runtime code" >&2
  exit 1
fi
if sudo -u nobody test -w "$TEST_ROOT/runtime/.git/config"; then
  echo "An unprivileged operator can modify trusted Git metadata" >&2
  exit 1
fi

grep -qF 'ADMIN_CONVERGE_REQUIRE_APPROVAL=true' "$REPO_ROOT/systemd/admin-converge.service"
grep -qF 'ExecStart=/var/lib/admin-node/runtime/bin/admin-node converge run' "$REPO_ROOT/systemd/admin-converge.service"
