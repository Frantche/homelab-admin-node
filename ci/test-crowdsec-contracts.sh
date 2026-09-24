#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ansible-playbook "$repo_root/ci/playbooks/crowdsec-contracts.yml"
