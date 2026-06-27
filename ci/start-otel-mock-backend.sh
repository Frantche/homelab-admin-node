#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

state_dir="${CI_OTEL_MOCK_STATE_DIR:-/tmp/admin-node-otel-mock}"
container_name="${CI_OTEL_MOCK_CONTAINER_NAME:-otel-mock-backend}"
port="${CI_OTEL_MOCK_PORT:-43190}"

mkdir -p "$state_dir"
docker network create admin-net >/dev/null 2>&1 || true
docker rm -f "$container_name" >/dev/null 2>&1 || true

docker run -d \
  --name "$container_name" \
  --network admin-net \
  -v "$REPO_ROOT/ci/otel-mock-backend.py:/otel-mock-backend.py:ro" \
  -v "$state_dir:/state" \
  python:3.13-alpine \
  python /otel-mock-backend.py --host 0.0.0.0 --port "$port" --state-dir /state >/dev/null

for _ in $(seq 1 30); do
  if docker run --rm --network admin-net curlimages/curl:8.17.0 \
      -fsS "http://${container_name}:${port}/healthz" >/dev/null 2>&1; then
    echo "OTLP mock backend ready at http://${container_name}:${port}"
    exit 0
  fi
  sleep 1
done

echo "OTLP mock backend did not become ready" >&2
docker logs "$container_name" >&2 || true
exit 1
