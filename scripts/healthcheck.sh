#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
"$REPO_ROOT/scripts/build-admin-node.sh" >/dev/null
"$REPO_ROOT/bin/admin-node" validate apis
if [[ "${PIHOLE_ENABLED:-true}" == "true" ]]; then
  "$REPO_ROOT/bin/admin-node" validate dns
else
  echo "[healthcheck] skipping DNS validation because PIHOLE_ENABLED=false"
fi
if [[ "${CLOUDFLARE_ENABLED:-true}" == "true" ]]; then
  "$REPO_ROOT/bin/admin-node" validate tunnel
else
  echo "[healthcheck] skipping Cloudflare tunnel validation because CLOUDFLARE_ENABLED=false"
fi
