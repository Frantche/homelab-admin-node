#!/usr/bin/env bash
set -euo pipefail

./scripts/set-mode.sh init
./ci/create-sentinel-data.sh
./scripts/backup.sh || true
./scripts/set-mode.sh restore
./scripts/restore.sh || true
./scripts/validate-apis.sh || true
./scripts/validate-dns.sh || true
./scripts/validate-cloudflare-tunnel.sh || true
