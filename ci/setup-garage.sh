#!/usr/bin/env bash
set -euo pipefail

GARAGE_ROOT="${GARAGE_ROOT:-$PWD/.ci/garage}"
GARAGE_IMAGE="${GARAGE_IMAGE:-dxflrs/garage@sha256:dac0c92add4f1a0b41035e94b41036a270ffbe88a37c7ac9c3f19e6dc5bdccf2}"
GARAGE_CONTAINER="${GARAGE_CONTAINER:-admin-node-ci-garage}"
GARAGE_TLS_PORT="${GARAGE_TLS_PORT:-4443}"

for command in curl docker openssl socat; do
  if ! command -v "$command" >/dev/null; then
    echo "ERROR: required command not found: $command" >&2
    exit 1
  fi
done

cleanup_on_error() {
  local status=$?
  trap - EXIT
  if [[ "$status" -ne 0 ]]; then
    if [[ -f "$GARAGE_ROOT/socat.pid" ]]; then
      kill "$(cat "$GARAGE_ROOT/socat.pid")" 2>/dev/null || true
    fi
    docker rm -f "$GARAGE_CONTAINER" >/dev/null 2>&1 || true
  fi
  exit "$status"
}
trap cleanup_on_error EXIT

install -d -m 0700 "$GARAGE_ROOT/meta" "$GARAGE_ROOT/data" "$GARAGE_ROOT/tls"

access_key="GK$(openssl rand -hex 16)"
secret_key="$(openssl rand -hex 32)"
restic_password="$(openssl rand -hex 32)"
rpc_secret="$(openssl rand -hex 32)"
admin_token="$(openssl rand -base64 32)"
metrics_token="$(openssl rand -base64 32)"

openssl req -x509 -newkey rsa:3072 -nodes -days 1 \
  -subj "/CN=admin-node-ci-garage-ca" \
  -keyout "$GARAGE_ROOT/tls/ca.key" \
  -out "$GARAGE_ROOT/tls/ca.crt" >/dev/null 2>&1
openssl req -newkey rsa:3072 -nodes \
  -subj "/CN=garage.test" \
  -keyout "$GARAGE_ROOT/tls/server.key" \
  -out "$GARAGE_ROOT/tls/server.csr" >/dev/null 2>&1
cat >"$GARAGE_ROOT/tls/server.ext" <<EOF
subjectAltName=DNS:garage.test
extendedKeyUsage=serverAuth
EOF
openssl x509 -req -days 1 \
  -in "$GARAGE_ROOT/tls/server.csr" \
  -CA "$GARAGE_ROOT/tls/ca.crt" \
  -CAkey "$GARAGE_ROOT/tls/ca.key" \
  -CAcreateserial \
  -extfile "$GARAGE_ROOT/tls/server.ext" \
  -out "$GARAGE_ROOT/tls/server.crt" >/dev/null 2>&1

cat >"$GARAGE_ROOT/garage.toml" <<EOF
metadata_dir = "/var/lib/garage/meta"
data_dir = "/var/lib/garage/data"
db_engine = "sqlite"
replication_factor = 1
rpc_bind_addr = "127.0.0.1:3901"
rpc_public_addr = "127.0.0.1:3901"
rpc_secret = "${rpc_secret}"

[s3_api]
s3_region = "garage"
api_bind_addr = "127.0.0.1:3900"
root_domain = ".s3.garage.test"

[admin]
api_bind_addr = "127.0.0.1:3903"
admin_token = "${admin_token}"
metrics_token = "${metrics_token}"
EOF

docker rm -f "$GARAGE_CONTAINER" >/dev/null 2>&1 || true
docker run -d \
  --name "$GARAGE_CONTAINER" \
  --network host \
  -v "$GARAGE_ROOT/garage.toml:/etc/garage.toml:ro" \
  -v "$GARAGE_ROOT/meta:/var/lib/garage/meta" \
  -v "$GARAGE_ROOT/data:/var/lib/garage/data" \
  -e "GARAGE_DEFAULT_ACCESS_KEY=$access_key" \
  -e "GARAGE_DEFAULT_SECRET_KEY=$secret_key" \
  -e "GARAGE_DEFAULT_BUCKET=admin-node-restic" \
  "$GARAGE_IMAGE" \
  /garage server --single-node --default-bucket >/dev/null

socat \
  "OPENSSL-LISTEN:${GARAGE_TLS_PORT},reuseaddr,fork,cert=${GARAGE_ROOT}/tls/server.crt,key=${GARAGE_ROOT}/tls/server.key,verify=0" \
  TCP:127.0.0.1:3900 \
  >"$GARAGE_ROOT/socat.log" 2>&1 &
echo "$!" >"$GARAGE_ROOT/socat.pid"

for attempt in $(seq 1 60); do
  status="$(curl --cacert "$GARAGE_ROOT/tls/ca.crt" --resolve "garage.test:${GARAGE_TLS_PORT}:127.0.0.1" -sS -o /dev/null -w '%{http_code}' "https://garage.test:${GARAGE_TLS_PORT}/" || true)"
  if [[ "$status" != "000" ]]; then
    break
  fi
  if [[ "$attempt" -eq 60 ]]; then
    docker logs "$GARAGE_CONTAINER" >&2 || true
    echo "ERROR: Garage TLS endpoint did not become ready" >&2
    exit 1
  fi
  sleep 2
done

{
  printf 'CI_RESTIC_OFFSITE_ENDPOINT=%q\n' "s3:https://garage.test:${GARAGE_TLS_PORT}/admin-node-restic"
  printf 'CI_RESTIC_OFFSITE_ACCESS_KEY=%q\n' "$access_key"
  printf 'CI_RESTIC_OFFSITE_SECRET_KEY=%q\n' "$secret_key"
  printf 'CI_RESTIC_OFFSITE_PASSWORD=%q\n' "$restic_password"
  printf 'CI_RESTIC_OFFSITE_CACERT=%q\n' "/etc/ssl/certs/ci-garage-ca.crt"
  printf 'CI_GARAGE_CA_FILE=%q\n' "$GARAGE_ROOT/tls/ca.crt"
  printf 'CI_GARAGE_CONTAINER=%q\n' "$GARAGE_CONTAINER"
} >"$GARAGE_ROOT/runtime.env"
chmod 0600 "$GARAGE_ROOT/runtime.env"
trap - EXIT
