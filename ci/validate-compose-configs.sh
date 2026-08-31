#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

for tool in ansible-playbook docker; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    printf 'required tool not found: %s\n' "$tool" >&2
    exit 1
  fi
done

validation_root="$(mktemp -d /tmp/admin-node-config-validation.XXXXXX)"
trap 'rm -rf "$validation_root"' EXIT
runtime_root="$validation_root/runtime"
install -Dm0600 /dev/null "$runtime_root/env/traefik.env"

ansible-playbook "$repo_root/ci/playbooks/render-validation-artifacts.yml" \
  -e "validation_output=$validation_root" \
  -e "admin_node_root=$runtime_root" >/dev/null

export \
  GITEA_DB_PASSWORD=validation \
  GITEA_INTERNAL_TOKEN=validation \
  GITEA_JWT_SECRET=validation \
  GITEA_SECRET_KEY=validation \
  HARBOR_ADMIN_PASSWORD=validation \
  HARBOR_DB_PASSWORD=validation \
  HARBOR_REGISTRY_PASSWORD=validation \
  KEYCLOAK_ADMIN=admin \
  KEYCLOAK_ADMIN_PASSWORD=validation \
  KEYCLOAK_DB_PASSWORD=validation

for compose in "$validation_root"/*/compose.yaml; do
  docker compose -f "$compose" config --quiet --no-env-resolution
done
