#!/usr/bin/env bash
set -euo pipefail

OPENBAO_ADDR=${OPENBAO_ADDR:-http://127.0.0.1:8200}

if ! curl -fsS "$OPENBAO_ADDR/v1/sys/health" >/dev/null 2>&1; then
  echo "OpenBao API not reachable at $OPENBAO_ADDR" >&2
  exit 1
fi

echo "Initializing OpenBao (output intentionally shown so operator can securely capture keys)"
docker exec -e BAO_ADDR=http://127.0.0.1:8200 openbao bao operator init -key-shares=5 -key-threshold=3
