#!/usr/bin/env bash
set -euo pipefail

./scripts/set-mode.sh init
./scripts/admin-converge.sh || true
./scripts/set-mode.sh normal
./ci/create-sentinel-data.sh
./scripts/backup.sh || true
echo "${GITHUB_HEAD_REF:-main}" > /etc/admin-node/git-ref
./scripts/admin-converge.sh || true
./scripts/validate-apis.sh || true
./scripts/validate-dns.sh || true
./scripts/validate-cloudflare-tunnel.sh || true
./scripts/backup.sh || true
