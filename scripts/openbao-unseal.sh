#!/usr/bin/env bash
set -euo pipefail

AGE_KEY=/etc/sops/age/keys.txt
SECRETS_FILE=/opt/homelab-admin-node/secrets/openbao-unseal.sops.yaml
OPENBAO_ADDR=${OPENBAO_ADDR:-http://127.0.0.1:8200}

if [[ ! -f "$AGE_KEY" ]]; then
  echo "Missing age private key at $AGE_KEY. Node remains locked." >&2
  exit 1
fi

if [[ ! -f "$SECRETS_FILE" ]]; then
  echo "Missing OpenBao unseal secrets file: $SECRETS_FILE" >&2
  exit 1
fi

export SOPS_AGE_KEY_FILE="$AGE_KEY"
json="$(sops --decrypt --output-type json "$SECRETS_FILE")"

active_keyset="$(python -c 'import json,sys; d=json.loads(sys.stdin.read()); print(d["openbao"]["active_keyset"])' <<< "$json")"
threshold="$(python -c 'import json,sys; d=json.loads(sys.stdin.read()); ks=d["openbao"]["keysets"][d["openbao"]["active_keyset"]]; print(ks["threshold"])' <<< "$json")"

if [[ -z "$active_keyset" || -z "$threshold" ]]; then
  echo "Unable to read active keyset or threshold" >&2
  exit 1
fi

health="$(curl -fsS "$OPENBAO_ADDR/v1/sys/health")"
initialized="$(python -c 'import json,sys; print(json.loads(sys.stdin.read()).get("initialized", False))' <<< "$health")"
sealed="$(python -c 'import json,sys; print(json.loads(sys.stdin.read()).get("sealed", True))' <<< "$health")"

if [[ "$initialized" != "True" ]]; then
  echo "OpenBao is not initialized" >&2
  exit 1
fi

if [[ "$sealed" == "False" ]]; then
  echo "OpenBao already unsealed"
  exit 0
fi

python - "$json" <<'PY' | while IFS= read -r key; do
import json,sys
obj=json.loads(sys.argv[1])
active=obj["openbao"]["active_keyset"]
ks=obj["openbao"]["keysets"][active]
threshold=int(ks["threshold"])
for k in ks["unseal_keys"][:threshold]:
    print(k)
PY
  docker exec -i openbao bao operator unseal >/dev/null
done

health2="$(curl -fsS "$OPENBAO_ADDR/v1/sys/health")"
sealed2="$(python -c 'import json,sys; print(json.loads(sys.stdin.read()).get("sealed", True))' <<< "$health2")"

if [[ "$sealed2" != "False" ]]; then
  echo "OpenBao unseal failed" >&2
  exit 1
fi

echo "OpenBao unsealed successfully"
