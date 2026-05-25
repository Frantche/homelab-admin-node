#!/usr/bin/env bash
set -euo pipefail
mkdir -p /etc/sops/age /opt/homelab-admin-node/secrets
cat > /etc/sops/age/keys.txt <<'KEY'
AGE-SECRET-KEY-1EXAMPLEPLACEHOLDER00000000000000000000000000000000000
KEY
chmod 0400 /etc/sops/age/keys.txt
cp secrets/openbao-unseal.sops.yaml.example /opt/homelab-admin-node/secrets/openbao-unseal.sops.yaml
