#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if rg -n 'BAO_ADDR=http://|http://openbao:8200|tls_disable[[:space:]]*=[[:space:]]*1|disable_mlock|IPC_LOCK|memlock' \
  "$REPO_ROOT/cmd" \
  "$REPO_ROOT/internal" \
  "$REPO_ROOT/ansible" \
  "$REPO_ROOT/stacks"; then
  echo "OpenBao still has an unencrypted path or obsolete mlock configuration" >&2
  exit 1
fi

grep -qF 'tls_cert_file = "/openbao/tls/tls.crt"' "$REPO_ROOT/stacks/openbao/openbao.hcl"
grep -qF -- '- traefik-openbao' "$REPO_ROOT/stacks/openbao/compose.yaml"
grep -qF -- '- openbao-metrics' "$REPO_ROOT/stacks/openbao/compose.yaml"
grep -qF -- '- "{{ service_domains.keycloak }}"' "$REPO_ROOT/stacks/traefik/compose.yaml"

if grep -qF -- '- admin-edge' "$REPO_ROOT/stacks/openbao/compose.yaml"; then
  echo "OpenBao still shares the general admin-edge network" >&2
  exit 1
fi

"$REPO_ROOT/ci/test-traefik-external-services.sh"
