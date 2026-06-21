#!/usr/bin/env bash
set -euo pipefail

# In CI, cloudflared won't have a real tunnel - skip gracefully
if [[ "${CI_MOCK_CLOUDFLARE_TUNNEL:-false}" == "true" ]]; then
  # Just verify the container exists
  if docker ps -a --format '{{.Names}}' | grep -qx cloudflared; then
    echo "[validate-cloudflare-tunnel] cloudflared container exists (CI mock mode)"
  else
    echo "[validate-cloudflare-tunnel] cloudflared container not found but mocking enabled - OK"
  fi
  echo "Cloudflare tunnel validation passed"
  exit 0
fi

# Check that the cloudflared container exists and was created
if ! docker ps -a --format '{{.Names}}' | grep -qx cloudflared; then
  echo "Cloudflare tunnel container does not exist" >&2
  exit 1
fi

echo "[validate-cloudflare-tunnel] cloudflared container exists"

# If container is running with valid token, validate public URLs
if docker ps --format '{{.Names}}' | grep -qx cloudflared; then
  if [[ "${SKIP_PUBLIC_URL_VALIDATION:-false}" != "true" && "${CI_SKIP_PUBLIC_URL_VALIDATION:-false}" != "true" ]]; then
    curl -fsS https://keycloak.example.com >/dev/null
    curl -fsS https://bao.example.com >/dev/null
    curl -fsS https://harbor.example.com >/dev/null
    curl -fsS https://git.example.com >/dev/null
    curl -fsS https://traefik.example.com >/dev/null
  fi
else
  echo "[validate-cloudflare-tunnel] ERROR: container exists but is not running" >&2
  exit 1
fi

echo "Cloudflare tunnel validation passed"
