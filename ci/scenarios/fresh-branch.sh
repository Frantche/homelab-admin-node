#!/usr/bin/env bash
set -euo pipefail

./ci/generate-fake-sops.sh
./scripts/set-mode.sh locked
./scripts/set-mode.sh init
./scripts/admin-converge.sh || true
./ci/init-openbao-ci.sh
./scripts/set-mode.sh normal
./scripts/validate-apis.sh || true
./scripts/validate-dns.sh || true
./ci/create-sentinel-data.sh
./scripts/backup.sh || true
./scripts/backup.sh || true
./scripts/backup.sh || true
./scripts/backup.sh || true
./scripts/restore.sh || true
