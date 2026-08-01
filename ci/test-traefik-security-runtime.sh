#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TRAEFIK_IMAGE="$(awk '/^[[:space:]]*image: traefik:/{print $2; exit}' "$REPO_ROOT/stacks/traefik/compose.yaml.j2")"
TEST_DIR="$(mktemp -d)"
CONTAINER_NAME="traefik-security-test-$$"

cleanup() {
  docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
  rm -rf "$TEST_DIR"
}
trap cleanup EXIT

mkdir -p "$TEST_DIR/dynamic"
install -m 0644 /dev/stdin "$TEST_DIR/traefik.yml" <<'YAML'
entryPoints:
  test:
    address: ":8080"
api:
  dashboard: true
providers:
  file:
    directory: /etc/traefik/dynamic
YAML

install -m 0644 /dev/stdin "$TEST_DIR/dynamic/security.yml" <<'YAML'
http:
  routers:
    authenticated:
      rule: "Host(`security.test`)"
      entryPoints: [test]
      service: api@internal
      middlewares: [security-headers, test-auth]
    limited:
      rule: "Host(`limited.test`)"
      entryPoints: [test]
      service: api@internal
      middlewares: [security-headers, test-rate]
  middlewares:
    security-headers:
      headers:
        contentTypeNosniff: true
        forceSTSHeader: true
        referrerPolicy: "strict-origin-when-cross-origin"
        stsSeconds: 31536000
    test-auth:
      basicAuth:
        users:
          - "admin:$apr1$H6uskkkW$IgXLP6ewTrSuBkTrqE8wj/"
    test-rate:
      rateLimit:
        average: 1
        burst: 1
        period: "1m"
YAML

docker run -d --name "$CONTAINER_NAME" \
  -p 127.0.0.1::8080 \
  -v "$TEST_DIR/traefik.yml:/etc/traefik/traefik.yml:ro" \
  -v "$TEST_DIR/dynamic:/etc/traefik/dynamic:ro" \
  "$TRAEFIK_IMAGE" \
  --configFile=/etc/traefik/traefik.yml >/dev/null

PORT="$(docker inspect --format '{{(index (index .NetworkSettings.Ports "8080/tcp") 0).HostPort}}' "$CONTAINER_NAME")"
for _ in $(seq 1 30); do
  if [[ "$(curl --silent --output /dev/null --write-out '%{http_code}' \
    "http://127.0.0.1:$PORT/api/rawdata" -H 'Host: limited.test')" == "200" ]]; then
    break
  fi
  sleep 0.2
done

HEADERS="$TEST_DIR/refusal-headers"
STATUS="$(curl --silent --output /dev/null --dump-header "$HEADERS" --write-out '%{http_code}' \
  "http://127.0.0.1:$PORT/api/rawdata" -H 'Host: security.test')"
[[ "$STATUS" == "401" ]]
grep -Eqi '^X-Content-Type-Options: nosniff[[:space:]]*$' "$HEADERS"
grep -Eqi '^Strict-Transport-Security: max-age=31536000[[:space:]]*$' "$HEADERS"
grep -Eqi '^Referrer-Policy: strict-origin-when-cross-origin[[:space:]]*$' "$HEADERS"

STATUS="$(curl --silent --user admin:test --output /dev/null --write-out '%{http_code}' \
  "http://127.0.0.1:$PORT/api/rawdata" -H 'Host: security.test')"
[[ "$STATUS" == "200" ]]

# The readiness request consumed the single request burst.
STATUS="$(curl --silent --output /dev/null --write-out '%{http_code}' \
  "http://127.0.0.1:$PORT/api/rawdata" -H 'Host: limited.test')"
[[ "$STATUS" == "429" ]]
