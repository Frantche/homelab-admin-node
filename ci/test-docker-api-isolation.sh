#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if rg -n '/var/run/docker\.sock|docker-socket-proxy|tcp://[^[:space:]]*socket-proxy' \
  "$REPO_ROOT/stacks"; then
  echo "A stack still exposes the Docker API to a container" >&2
  exit 1
fi

if rg -n '^[[:space:]]+docker:$|docker_stats|traefik\.http\.' \
  "$REPO_ROOT/stacks/traefik" \
  "$REPO_ROOT/stacks/observability" \
  "$REPO_ROOT/stacks/openbao/compose.yaml" \
  "$REPO_ROOT/stacks/keycloak/compose.yaml" \
  "$REPO_ROOT/stacks/gitea/compose.yaml" \
  "$REPO_ROOT/stacks/harbor/compose.yaml"; then
  echo "Docker discovery or Docker runtime collection is still configured" >&2
  exit 1
fi

"$REPO_ROOT/ci/test-traefik-external-services.sh"
