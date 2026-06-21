#!/usr/bin/env bash
set -euo pipefail

AGE_KEY=${AGE_KEY:-/etc/sops/age/keys.txt}
SECRETS_DIR=${SECRETS_DIR:-/opt/homelab-admin-node/secrets}
SECRETS_FILE=${SECRETS_FILE:-$SECRETS_DIR/openbao-unseal.sops.yaml}
KEYSET_NAME=${KEYSET_NAME:-$(date +%Y-%m-initial)}
OPENBAO_CONTAINER=${OPENBAO_CONTAINER:-openbao}

if [[ ! -f "$AGE_KEY" ]]; then
  echo "Missing age private key at $AGE_KEY" >&2
  exit 1
fi

mkdir -p "$SECRETS_DIR"

if [[ -f "$SECRETS_FILE" ]]; then
  echo "OpenBao unseal secrets already exist: $SECRETS_FILE"
  exit 0
fi

echo "[openbao-init] waiting for OpenBao container API"
status_out=""
for i in $(seq 1 60); do
  status_out="$(docker exec -e BAO_ADDR=http://127.0.0.1:8200 "$OPENBAO_CONTAINER" bao status -format=json 2>/dev/null)" || true
  if [[ -n "$status_out" ]]; then
    break
  fi
  if [[ $i -eq 60 ]]; then
    echo "OpenBao did not become reachable" >&2
    docker logs "$OPENBAO_CONTAINER" 2>&1 | tail -40 >&2 || true
    exit 1
  fi
  sleep 2
done

initialized="$(python3 -c 'import json,sys; print(json.loads(sys.stdin.read()).get("initialized", False))' <<< "$status_out")"
if [[ "$initialized" == "True" ]]; then
  echo "OpenBao is already initialized but $SECRETS_FILE is missing" >&2
  exit 1
fi

echo "[openbao-init] initializing OpenBao"
init_output="$(docker exec -e BAO_ADDR=http://127.0.0.1:8200 "$OPENBAO_CONTAINER" bao operator init -key-shares=5 -key-threshold=3 -format=json)"

tmp_plain="$(mktemp)"
trap 'rm -f "$tmp_plain"' EXIT

python3 - "$KEYSET_NAME" "$init_output" > "$tmp_plain" <<'PY'
import json
import sys

keyset = sys.argv[1]
data = json.loads(sys.argv[2])
keys = None
for field in ("unseal_keys_b64", "unseal_keys_hex", "unseal_keys", "keys_base64", "keys"):
    if field in data:
        keys = data[field]
        break
if not keys:
    raise SystemExit(f"no unseal keys found in init output fields: {sorted(data)}")
root_token = data.get("root_token", "")
print("openbao:")
print(f'  active_keyset: "{keyset}"')
print("  keysets:")
print(f'    "{keyset}":')
print("      threshold: 3")
print("      unseal_keys:")
for key in keys:
    print(f'        - "{key}"')
print(f'  root_token: "{root_token}"')
print("openbao_config:")
print(f'  root_token: "{root_token}"')
PY

age_public_key="$(age-keygen -y "$AGE_KEY")"
sops --config /dev/null --encrypt --age "$age_public_key" --filename-override "$SECRETS_FILE" "$tmp_plain" > "$SECRETS_FILE"
chmod 0600 "$SECRETS_FILE"

echo "[openbao-init] wrote encrypted unseal secrets to $SECRETS_FILE"
