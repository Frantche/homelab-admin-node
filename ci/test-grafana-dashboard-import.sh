#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dashboard_dir="${DASHBOARD_DIR:-$repo_root/stacks/observability/grafana/dashboards}"
grafana_url="${GRAFANA_URL:-http://127.0.0.1:3000}"
grafana_user="${GRAFANA_USER:-admin}"
grafana_password="${GRAFANA_PASSWORD:-admin}"
datasource_name="${GRAFANA_DATASOURCE_NAME:-VictoriaMetrics Test}"
datasource_uid="${GRAFANA_DATASOURCE_UID:-victoriametrics-test}"
datasource_url="${GRAFANA_DATASOURCE_URL:-http://victoriametrics:8428}"

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required" >&2
  exit 1
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required" >&2
  exit 1
fi

if [ ! -d "$dashboard_dir" ]; then
  echo "dashboard directory not found: $dashboard_dir" >&2
  exit 1
fi

mapfile -t dashboards < <(find "$dashboard_dir" -maxdepth 1 -type f -name '*.json' | sort)
if [ "${#dashboards[@]}" -eq 0 ]; then
  echo "no dashboard JSON files found in $dashboard_dir" >&2
  exit 1
fi

for attempt in $(seq 1 60); do
  if curl -fsS -u "$grafana_user:$grafana_password" "$grafana_url/api/health" >/dev/null 2>&1; then
    break
  fi
  if [ "$attempt" -eq 60 ]; then
    echo "Grafana did not become healthy at $grafana_url" >&2
    exit 1
  fi
  sleep 2
done

datasource_payload="$(
  jq -n \
    --arg name "$datasource_name" \
    --arg uid "$datasource_uid" \
    --arg url "$datasource_url" \
    '{
      name: $name,
      uid: $uid,
      type: "prometheus",
      access: "proxy",
      url: $url,
      isDefault: true,
      jsonData: {
        httpMethod: "POST"
      }
    }'
)"

datasource_status="$(
  curl -sS -o /tmp/grafana-datasource-response.json -w '%{http_code}' \
    -u "$grafana_user:$grafana_password" \
    -H 'Content-Type: application/json' \
    -X POST \
    -d "$datasource_payload" \
    "$grafana_url/api/datasources"
)"

if [ "$datasource_status" != "200" ] && [ "$datasource_status" != "409" ]; then
  echo "failed to create Grafana datasource, status $datasource_status" >&2
  cat /tmp/grafana-datasource-response.json >&2
  exit 1
fi

for dashboard in "${dashboards[@]}"; do
  jq empty "$dashboard"

  uid="$(jq -r '.uid // empty' "$dashboard")"
  title="$(jq -r '.title // empty' "$dashboard")"
  if [ -z "$uid" ] || [ -z "$title" ]; then
    echo "dashboard must define uid and title: $dashboard" >&2
    exit 1
  fi

  payload="$(
    jq -n \
      --slurpfile dashboard "$dashboard" \
      '{dashboard: $dashboard[0], overwrite: true, folderId: 0}'
  )"

  import_status="$(
    curl -sS -o /tmp/grafana-dashboard-import-response.json -w '%{http_code}' \
      -u "$grafana_user:$grafana_password" \
      -H 'Content-Type: application/json' \
      -X POST \
      -d "$payload" \
      "$grafana_url/api/dashboards/db"
  )"

  if [ "$import_status" != "200" ]; then
    echo "failed to import dashboard $dashboard, status $import_status" >&2
    cat /tmp/grafana-dashboard-import-response.json >&2
    exit 1
  fi

  fetched_title="$(
    curl -fsS \
      -u "$grafana_user:$grafana_password" \
      "$grafana_url/api/dashboards/uid/$uid" |
      jq -r '.dashboard.title // empty'
  )"

  if [ "$fetched_title" != "$title" ]; then
    echo "imported dashboard title mismatch for $uid: got '$fetched_title', want '$title'" >&2
    exit 1
  fi

  echo "Imported Grafana dashboard: $title ($uid)"
done
