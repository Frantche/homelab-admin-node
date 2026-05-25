#!/usr/bin/env bash
set -euo pipefail

if [[ "${CI_MOCK_APIS:-false}" == "true" ]]; then
  echo "API validation mocked"
  exit 0
fi

bao_health="$(curl -fsS http://127.0.0.1:8200/v1/sys/health)"
python -c 'import json,sys; d=json.loads(sys.argv[1]); assert d.get("initialized") is True; assert d.get("sealed") is False' "$bao_health"

docker exec openbao bao kv put secret/admin-node-sentinel value=ok >/dev/null
docker exec openbao bao kv get -field=value secret/admin-node-sentinel | grep -qx "ok"

curl -fsS http://127.0.0.1:8081/health/ready >/dev/null
curl -fsS http://127.0.0.1:8081/realms/master/.well-known/openid-configuration >/dev/null

curl -fsS http://127.0.0.1:8082/api/v2.0/health >/dev/null

echo "API validation passed"
