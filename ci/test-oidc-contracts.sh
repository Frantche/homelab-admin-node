#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SUCCESS_PLAYBOOK="$REPO_ROOT/ci/playbooks/oidc-contracts-success.yml"
CI_PLAYBOOK="$REPO_ROOT/ci/playbooks/oidc-contracts-ci.yml"
MISSING_SECRET_PLAYBOOK="$REPO_ROOT/ci/playbooks/oidc-contracts-missing-secret.yml"

export ANSIBLE_NOCOLOR=1

echo "[oidc-contracts] non-CI success scenario"
ansible-playbook -i localhost, "$SUCCESS_PLAYBOOK"

echo "[oidc-contracts] CI mock scenario"
ansible-playbook -i localhost, "$CI_PLAYBOOK"

echo "[oidc-contracts] non-CI missing secret failure scenario"
missing_output="$(mktemp)"
if ansible-playbook -i localhost, "$MISSING_SECRET_PLAYBOOK" >"$missing_output" 2>&1; then
  cat "$missing_output"
  echo "[oidc-contracts] expected missing-secret scenario to fail" >&2
  rm -f "$missing_output"
  exit 1
fi

grep -F "oidc_clients.harbor.client_secret is required when ci_mode is false. Define it in the inventory, preferably in secrets.sops.yaml." "$missing_output" >/dev/null
rm -f "$missing_output"

echo "[oidc-contracts] all scenarios passed"
