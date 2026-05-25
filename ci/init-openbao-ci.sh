#!/usr/bin/env bash
set -euo pipefail

# Initialize OpenBao in CI and store unseal keys for the test lifecycle.
# This creates a SOPS-compatible secrets file with the init output.

OPENBAO_ADDR=${OPENBAO_ADDR:-http://127.0.0.1:8200}
SECRETS_DIR=/opt/homelab-admin-node/secrets

mkdir -p "$SECRETS_DIR"

echo "[init-openbao-ci] Waiting for OpenBao to be ready..."
for _ in $(seq 1 30); do
  http_code="$(curl -s -o /dev/null -w '%{http_code}' "$OPENBAO_ADDR/v1/sys/health" 2>/dev/null || echo "000")"
  if [[ "$http_code" != "000" ]]; then
    break
  fi
  sleep 2
done

# Check if already initialized
health="$(curl -s "$OPENBAO_ADDR/v1/sys/health" 2>/dev/null || true)"
initialized="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1]).get("initialized", False))' "$health" 2>/dev/null || echo "False")"

if [[ "$initialized" == "True" ]]; then
  echo "[init-openbao-ci] OpenBao already initialized"
  exit 0
fi

echo "[init-openbao-ci] Initializing OpenBao..."
init_output="$(docker exec openbao bao operator init -key-shares=5 -key-threshold=3 -format=json)"

# Extract keys and root token
root_token="$(python3 -c 'import json,sys; d=json.loads(sys.argv[1]); print(d["root_token"])' "$init_output")"
unseal_keys="$(python3 -c 'import json,sys; d=json.loads(sys.argv[1]); print("\n".join(d["unseal_keys"]))' "$init_output")"

# Save root token for later use
echo "$root_token" > "$SECRETS_DIR/openbao-root-token"

# Create the SOPS-compatible secrets file (unencrypted for CI)
cat > "$SECRETS_DIR/openbao-unseal.sops.yaml" <<EOF
openbao:
  active_keyset: "ci-keyset"
  keysets:
    "ci-keyset":
      threshold: 3
      unseal_keys:
$(echo "$unseal_keys" | while IFS= read -r key; do echo "        - \"$key\""; done)
  root_token: "$root_token"
EOF

# Export root token for other scripts
export OPENBAO_TOKEN="$root_token"

# Unseal OpenBao
echo "[init-openbao-ci] Unsealing OpenBao..."
echo "$unseal_keys" | head -3 | while IFS= read -r key; do
  docker exec openbao bao operator unseal "$key" >/dev/null
done

# Verify unsealed
sleep 2
health2="$(curl -fsS "$OPENBAO_ADDR/v1/sys/health")"
sealed="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1]).get("sealed", True))' "$health2")"
if [[ "$sealed" != "False" ]]; then
  echo "[init-openbao-ci] ERROR: OpenBao is still sealed after unseal attempt" >&2
  exit 1
fi

# Enable kv-v2 secrets engine
docker exec -e VAULT_TOKEN="$root_token" openbao bao secrets enable -path=secret kv-v2 >/dev/null 2>&1 || true

echo "[init-openbao-ci] OpenBao initialized and unsealed successfully"
echo "[init-openbao-ci] Root token saved to $SECRETS_DIR/openbao-root-token"
