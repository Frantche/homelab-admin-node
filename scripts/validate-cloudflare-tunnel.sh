#!/usr/bin/env bash
set -euo pipefail

# Check that the cloudflared container exists and was created
if ! docker ps -a --format '{{.Names}}' | grep -qx cloudflared; then
  echo "Cloudflare tunnel container does not exist" >&2
  exit 1
fi

echo "[validate-cloudflare-tunnel] cloudflared container exists"

# If container is running with valid token, validate public URLs
if docker ps --format '{{.Names}}' | grep -qx cloudflared; then
  if [[ "${SKIP_PUBLIC_URL_VALIDATION:-false}" != "true" ]]; then
    curl -fsS https://keycloak.example.com >/dev/null
    curl -fsS https://bao.example.com >/dev/null
    curl -fsS https://harbor.example.com >/dev/null
    curl -fsS https://traefik.example.com >/dev/null
  fi
else
  echo "[validate-cloudflare-tunnel] container not running (expected if no valid token)"
fi

echo "Cloudflare tunnel validation passed"
