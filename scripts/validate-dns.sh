#!/usr/bin/env bash
set -euo pipefail

ADMIN_IP=${ADMIN_NODE_LAN_IP:-192.168.1.10}
for host in harbor.example.com bao.example.com keycloak.example.com traefik.example.com; do
  resolved="$(getent ahostsv4 "$host" 2>/dev/null | awk '{print $1; exit}')"
  if [[ -z "$resolved" ]]; then
    echo "DNS validation: cannot resolve $host (no DNS record found, skipping)" >&2
    continue
  fi
  if [[ "$resolved" != "$ADMIN_IP" ]]; then
    echo "DNS validation failed for $host (got $resolved expected $ADMIN_IP)" >&2
    exit 1
  fi
done

echo "DNS validation passed"
