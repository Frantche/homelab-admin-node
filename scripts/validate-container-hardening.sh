#!/usr/bin/env bash
set -euo pipefail

services=(
  cloudflared gitea-db gitea keycloak-db keycloak
  harbor-log harbor-db harbor-redis harbor-registry harbor-registryctl
  harbor-core harbor-portal harbor-jobservice harbor-trivy harbor-exporter
  harbor-nginx otel-mock-backend otel-collector openbao traefik
)
optional_services=(cloudflared otel-mock-backend otel-collector)
user_exceptions=(
  gitea-db gitea keycloak-db openbao traefik otel-mock-backend
  harbor-log harbor-db harbor-redis harbor-registry harbor-registryctl
  harbor-core harbor-portal harbor-jobservice harbor-trivy harbor-exporter
  harbor-nginx
)
write_exceptions=(
  gitea harbor-log harbor-db harbor-redis harbor-registry harbor-registryctl
  harbor-core harbor-portal harbor-jobservice harbor-trivy harbor-exporter
  harbor-nginx
)

contains() {
  local needle="$1"
  shift
  local item
  for item in "$@"; do
    [[ "$item" == "$needle" ]] && return 0
  done
  return 1
}

for service in "${services[@]}"; do
  if ! inspect="$(docker inspect "$service" 2>/dev/null)"; then
    if contains "$service" "${optional_services[@]}"; then
      continue
    fi
    echo "Required container is missing: $service" >&2
    exit 1
  fi

  jq -e '.[0].HostConfig.CapDrop | index("ALL") != null' <<<"$inspect" >/dev/null
  jq -e '.[0].HostConfig.SecurityOpt | any(. == "no-new-privileges:true")' <<<"$inspect" >/dev/null
  jq -e '.[0].HostConfig.PidsLimit > 0 and .[0].HostConfig.Memory > 0 and .[0].HostConfig.NanoCpus > 0' <<<"$inspect" >/dev/null
  jq -e '.[0].Config.Healthcheck != null' <<<"$inspect" >/dev/null

  if ! contains "$service" "${user_exceptions[@]}"; then
    jq -e '.[0].Config.User != ""' <<<"$inspect" >/dev/null
  fi
  if ! contains "$service" "${write_exceptions[@]}"; then
    jq -e '.[0].HostConfig.ReadonlyRootfs == true' <<<"$inspect" >/dev/null
  fi
done
