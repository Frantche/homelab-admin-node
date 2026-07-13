#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

export ANSIBLE_NOCOLOR=1
export ANSIBLE_ROLES_PATH="$REPO_ROOT/ansible/roles"

ansible-playbook -i localhost, "$REPO_ROOT/ci/playbooks/traefik-external-services.yml"
