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
init_output="$(docker exec -e BAO_ADDR=http://127.0.0.1:8200 openbao bao operator init -key-shares=5 -key-threshold=3 -format=json)"

# Extract keys and root token
# OpenBao returns keys as unseal_keys_b64/unseal_keys_hex or keys/keys_base64 depending on version
root_token="$(python3 -c '
import json, sys
d = json.loads(sys.argv[1])
print(d["root_token"])
' "$init_output")" || { echo "[init-openbao-ci] ERROR: Failed to parse init output: $init_output" >&2; exit 1; }

unseal_keys="$(python3 -c '
import json, sys
d = json.loads(sys.argv[1])
# Try different key names that OpenBao may use
for key in ("unseal_keys_b64", "unseal_keys_hex", "unseal_keys", "keys_base64", "keys"):
    if key in d:
        print("\n".join(d[key]))
        sys.exit(0)
print("ERROR: no unseal keys found in: " + str(list(d.keys())), file=sys.stderr)
sys.exit(1)
' "$init_output")" || { echo "[init-openbao-ci] ERROR: Failed to extract unseal keys from init output" >&2; exit 1; }

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
mapfile -t unseal_keys_arr <<< "$unseal_keys"
for key in "${unseal_keys_arr[@]:0:3}"; do
  docker exec -e BAO_ADDR=http://127.0.0.1:8200 openbao bao operator unseal "$key" >/dev/null
done

# Verify unsealed
sleep 2
health2="$(curl -s "$OPENBAO_ADDR/v1/sys/health" || true)"
sealed="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1]).get("sealed", True))' "$health2" 2>/dev/null || echo "True")"
if [[ "$sealed" != "False" ]]; then
  echo "[init-openbao-ci] ERROR: OpenBao is still sealed after unseal attempt" >&2
  exit 1
fi

# Enable kv-v2 secrets engine
docker exec -e BAO_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN="$root_token" openbao bao secrets enable -path=secret kv-v2 >/dev/null 2>&1 || true

echo "[init-openbao-ci] OpenBao initialized and unsealed successfully"
echo "[init-openbao-ci] Root token saved to $SECRETS_DIR/openbao-root-token"
