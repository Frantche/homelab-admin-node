#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

legacy_calls='make (quality|test-ci-fast|test-ci-full|go-test|go-coverage)'
if rg -n "$legacy_calls" \
  "$repo_root/README.md" \
  "$repo_root/CONTRIBUTING.md" \
  "$repo_root/AGENTS.md" \
  "$repo_root/ci/README.md" \
  "$repo_root/docs" \
  "$repo_root/site/content/en/docs" \
  "$repo_root/.github"; then
  echo "legacy Make validation entry point found" >&2
  exit 1
fi

if rg -n '(^|[[:space:]])(\./|/opt/homelab-admin-node/)ci/(test-|validate-|scenarios/)' \
  "$repo_root/.github/workflows"; then
  echo "GitHub Actions must invoke validation logic through Make" >&2
  exit 1
fi

make_database="$(make -C "$repo_root" -qp 2>/dev/null || true)"
phony_line="$(grep '^\.PHONY:' <<<"$make_database" || true)"
required_targets=(
  ci-bootstrap
  ci-continuous
  ci-disaster-recovery
  ci-dr-action
  ci-full
  ci-quality
  test-go
  test-grafana-dashboard-import
  test-image-security-policy
  test-image-security-scanner
  validate-compose
  validate-grafana-dashboards
  validate-systemd
)

for target in "${required_targets[@]}"; do
  if ! grep -Eq "(^|[[:space:]])${target}([[:space:]]|$)" <<<"$phony_line"; then
    echo "public Make target is not phony: $target" >&2
    exit 1
  fi
done

continuous_dry_run="$(make -C "$repo_root" -n ci-continuous)"
required_commands=(
  './ci/check-go-coverage.sh'
  "python3 -m unittest discover -s ci -p 'test_*.py'"
  './ci/test-traefik-security-runtime.sh'
  './ci/test-traefik-external-services.sh'
  './ci/test-restic-config.sh'
  './ci/test-offline-images.sh'
  './ci/test-scan-container-images.sh'
  './ci/test-disaster-recovery-actions.sh'
  './ci/validate-compose-configs.sh'
  './ci/validate-systemd-units.sh'
  './ci/validate-grafana-dashboards.sh'
  './ci/test-oidc-contracts.sh'
)

for command in "${required_commands[@]}"; do
  count="$(grep -Fc "$command" <<<"$continuous_dry_run" || true)"
  if [[ "$count" -ne 1 ]]; then
    echo "continuous validation must run exactly once: $command (found $count)" >&2
    exit 1
  fi
done

full_dry_run="$(make -C "$repo_root" -n ci-full)"
if [[ "$(grep -Fc './ci/run-disaster-recovery.sh' <<<"$full_dry_run" || true)" -ne 1 ]]; then
  echo "full validation must run disaster recovery exactly once" >&2
  exit 1
fi

workflow="$repo_root/.github/workflows/bootstrap-user-journey.yml"
while IFS= read -r action; do
  if ! grep -Fq "DR_ACTION=$action" "$workflow"; then
    echo "GitHub Actions does not expose disaster recovery action: $action" >&2
    exit 1
  fi
done < <("$repo_root/ci/run-disaster-recovery.sh" --list-actions)

grep -Fq 'DR_ACTION=cleanup' "$workflow"
