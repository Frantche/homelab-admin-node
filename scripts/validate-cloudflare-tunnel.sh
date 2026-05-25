#!/usr/bin/env bash
set -euo pipefail

if [[ "${CI_MOCK_CLOUDFLARE_TUNNEL:-false}" == "true" ]]; then
  echo "Cloudflare tunnel validation mocked"
  exit 0
fi

docker ps --format '{{.Names}}' | grep -qx cloudflared

if [[ "${SKIP_PUBLIC_URL_VALIDATION:-false}" != "true" ]]; then
  curl -fsS https://keycloak.example.com >/dev/null
  curl -fsS https://bao.example.com >/dev/null
  curl -fsS https://harbor.example.com >/dev/null
  curl -fsS https://traefik.example.com >/dev/null
fi

echo "Cloudflare tunnel validation passed"
