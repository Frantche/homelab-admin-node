#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG_REPO_DIR="${CONFIG_REPO_DIR:-/etc/admin-config/homelab-node-admin-config}"

echo "=== Running OIDC user journey via Playwright ==="
if command -v pacman >/dev/null 2>&1; then
  pacman -Sy --noconfirm --needed nodejs npm chromium nss
fi
if [[ ! -f /srv/admin/certs/ca.pem ]]; then
  echo "ERROR: Expected local CA at /srv/admin/certs/ca.pem" >&2
  exit 1
fi

pushd "$REPO_ROOT/ci/oidc-user-journey" >/dev/null
if [[ -f package-lock.json ]]; then
  npm ci --no-audit --no-fund
else
  npm install --no-audit --no-fund
fi
eval "$(
  SOPS_AGE_KEY_FILE=/etc/sops/age/keys.txt \
    python3 "$REPO_ROOT/ci/read-bootstrap-oidc-user.py" \
    "$CONFIG_REPO_DIR/hosts/group_vars/secrets.sops.yaml"
)"
CI=true \
  NODE_EXTRA_CA_CERTS=/srv/admin/certs/ca.pem \
  SSL_CERT_FILE=/srv/admin/certs/ca.pem \
  PLAYWRIGHT_CHROMIUM_EXECUTABLE="${PLAYWRIGHT_CHROMIUM_EXECUTABLE:-/usr/bin/chromium}" \
  OIDC_TEST_USERNAME="$OIDC_TEST_USERNAME" \
  OIDC_TEST_PASSWORD="$OIDC_TEST_PASSWORD" \
  npm test
popd >/dev/null
