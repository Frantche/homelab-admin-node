#!/usr/bin/env bash
set -euo pipefail

if [[ "${CI_MOCK_PIHOLE:-false}" == "true" || "${CI_MOCK_DNS:-false}" == "true" ]]; then
  echo "DNS validation mocked"
  exit 0
fi

ADMIN_IP=${ADMIN_NODE_LAN_IP:-192.168.1.10}
for host in harbor.example.com bao.example.com keycloak.example.com traefik.example.com; do
  resolved="$(getent ahostsv4 "$host" | awk '{print $1; exit}')"
  if [[ "$resolved" != "$ADMIN_IP" ]]; then
    echo "DNS validation failed for $host (got $resolved expected $ADMIN_IP)" >&2
    exit 1
  fi
done

echo "DNS validation passed"
