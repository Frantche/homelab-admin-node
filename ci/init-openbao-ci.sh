#!/usr/bin/env bash
set -euo pipefail

# Initialize and unseal OpenBao for CI using the Go CLI implementation.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SECRETS_DIR="$REPO_ROOT/secrets"
ROOT_TOKEN_FILE="$SECRETS_DIR/openbao-root-token"

"$REPO_ROOT/scripts/build-admin-node.sh" >/dev/null
mkdir -p "$SECRETS_DIR"

echo "[init-openbao-ci] Initializing OpenBao..."
"$REPO_ROOT/bin/admin-node" openbao init-if-needed \
  --keyset-name ci-keyset \
  --root-token-out "$ROOT_TOKEN_FILE"

echo "[init-openbao-ci] Unsealing OpenBao..."
"$REPO_ROOT/bin/admin-node" openbao unseal

echo "[init-openbao-ci] Ensuring CI KV engine..."
"$REPO_ROOT/bin/admin-node" openbao enable-kv \
  --token-file "$ROOT_TOKEN_FILE" \
  --path secret

echo "[init-openbao-ci] OpenBao initialized and unsealed successfully"
echo "[init-openbao-ci] Root token saved to $ROOT_TOKEN_FILE"
