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

if [[ -n "${HARBOR_ADMIN_USER:-}" && -n "${HARBOR_ADMIN_PASSWORD:-}" ]]; then
  curl -fsS -u "${HARBOR_ADMIN_USER}:${HARBOR_ADMIN_PASSWORD}" "http://127.0.0.1:8082/api/v2.0/projects?name=admin-ci" >/dev/null
fi

curl -fsS http://127.0.0.1:8080/api/http/routers >/dev/null
curl -fsS -H "Host: keycloak.example.com" http://127.0.0.1 >/dev/null
curl -fsS -H "Host: bao.example.com" http://127.0.0.1 >/dev/null
curl -fsS -H "Host: harbor.example.com" http://127.0.0.1 >/dev/null
dashboard_status="$(curl -s -o /dev/null -w '%{http_code}' -H 'Host: traefik.example.com' http://127.0.0.1)"
if [[ "$dashboard_status" != "401" && "$dashboard_status" != "403" ]]; then
  echo "Traefik dashboard is not protected as expected (status $dashboard_status)" >&2
  exit 1
fi

if [[ -n "${KEYCLOAK_CI_CLIENT_ID:-}" && -n "${KEYCLOAK_CI_CLIENT_SECRET:-}" ]]; then
  curl -fsS -X POST \
    -d "client_id=${KEYCLOAK_CI_CLIENT_ID}" \
    -d "client_secret=${KEYCLOAK_CI_CLIENT_SECRET}" \
    -d "grant_type=client_credentials" \
    http://127.0.0.1:8081/realms/master/protocol/openid-connect/token >/dev/null
fi

echo "API validation passed"
