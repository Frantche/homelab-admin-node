#!/usr/bin/env bash
set -euo pipefail

# Initialize OpenBao in CI and store unseal keys for the test lifecycle.
# This creates a SOPS-compatible secrets file with the init output.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SECRETS_DIR="$REPO_ROOT/secrets"

mkdir -p "$SECRETS_DIR"

echo "[init-openbao-ci] Waiting for OpenBao to be ready..."
for i in $(seq 1 30); do
  # bao status exits non-zero when sealed/starting, capture output regardless
  bao_out=""
  if ! bao_out="$(docker exec -e BAO_ADDR=http://127.0.0.1:8200 openbao bao status 2>&1)"; then
    :
  fi
  if echo "$bao_out" | grep -q "Initialized"; then
    break
  fi
  if [[ $i -eq 30 ]]; then
    echo "[init-openbao-ci] ERROR: OpenBao did not become reachable" >&2
    docker logs openbao 2>&1 | tail -20
    exit 1
  fi
  sleep 2
done

# Check if already initialized (exit code 0 means unsealed = initialized)
initialized="False"
bao_status=""
if ! bao_status="$(docker exec -e BAO_ADDR=http://127.0.0.1:8200 openbao bao status -format=json 2>/dev/null)"; then
  bao_status=""
fi
if [[ -n "$bao_status" ]]; then
  initialized="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1]).get("initialized", False))' "$bao_status" 2>/dev/null || echo "False")"
fi

if [[ "$initialized" == "True" ]]; then
  echo "[init-openbao-ci] OpenBao already initialized"
  exit 0
fi

echo "[init-openbao-ci] Initializing OpenBao..."
init_output="$(docker exec -e BAO_ADDR=http://127.0.0.1:8200 openbao bao operator init -key-shares=5 -key-threshold=3 -format=json)"

# Extract keys and root token
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

# Create the SOPS-compatible secrets file using the CI-only age key.
plain_secrets="$(mktemp)"
trap 'rm -f "$plain_secrets"' EXIT
cat > "$plain_secrets" <<EOF
openbao:
  active_keyset: "ci-keyset"
  keysets:
    "ci-keyset":
      threshold: 3
      unseal_keys:
$(echo "$unseal_keys" | while IFS= read -r key; do echo "        - \"$key\""; done)
  root_token: "$root_token"
EOF

age_public_key="$(sudo age-keygen -y /etc/sops/age/keys.txt)"
sops --config /dev/null --encrypt --age "$age_public_key" --input-type yaml --output-type yaml "$plain_secrets" > "$SECRETS_DIR/openbao-unseal.sops.yaml"

# Export root token for other scripts
export OPENBAO_TOKEN="$root_token"

# Unseal OpenBao
echo "[init-openbao-ci] Unsealing OpenBao..."
mapfile -t unseal_keys_arr <<< "$unseal_keys"
for key in "${unseal_keys_arr[@]:0:3}"; do
  # bao operator unseal returns exit 2 while vault is still sealed (progress); 0 or 2 are acceptable
  unseal_rc=0
  docker exec -e BAO_ADDR=http://127.0.0.1:8200 openbao bao operator unseal "$key" >/dev/null 2>&1 || unseal_rc=$?
  if [[ $unseal_rc -ne 0 && $unseal_rc -ne 2 ]]; then
    echo "[init-openbao-ci] ERROR: unseal command failed with exit code $unseal_rc" >&2
    exit 1
  fi
done

# Verify unsealed
sleep 2
bao_status2=""
if ! bao_status2="$(docker exec -e BAO_ADDR=http://127.0.0.1:8200 openbao bao status -format=json 2>/dev/null)"; then
  bao_status2=""
fi
sealed="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1]).get("sealed", True))' "$bao_status2" 2>/dev/null || echo "True")"
if [[ "$sealed" != "False" ]]; then
  echo "[init-openbao-ci] ERROR: OpenBao is still sealed after unseal attempt" >&2
  exit 1
fi

# Enable kv-v2 secrets engine (may already be enabled, which is acceptable)
enable_rc=0
docker exec -e BAO_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN="$root_token" openbao bao secrets enable -path=secret kv-v2 >/dev/null 2>&1 || enable_rc=$?
if [[ $enable_rc -ne 0 ]]; then
  # Check if it's already enabled (list and verify)
  if ! docker exec -e BAO_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN="$root_token" openbao bao secrets list -format=json 2>/dev/null | grep -q '"secret/"'; then
    echo "[init-openbao-ci] ERROR: Failed to enable kv-v2 secrets engine" >&2
    exit 1
  fi
fi

echo "[init-openbao-ci] OpenBao initialized and unsealed successfully"
echo "[init-openbao-ci] Root token saved to $SECRETS_DIR/openbao-root-token"
