#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
mock_port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
python3 "$repo_root/ci/mock-harbor-pull-secret-api.py" --port "$mock_port" &
mock_pid=$!
trap 'kill "$mock_pid" 2>/dev/null || true' EXIT

for _ in $(seq 1 50); do
  if curl --silent --fail "http://127.0.0.1:$mock_port/__state" >/dev/null; then
    break
  fi
  sleep 0.1
done

ansible-playbook "$repo_root/ci/playbooks/harbor-pull-secret-contracts.yml" \
  -e "mock_port=$mock_port"
