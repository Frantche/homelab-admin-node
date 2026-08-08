#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
scenario="$repo_root/ci/scenarios/main-to-candidate-disaster-recovery.sh"

while IFS= read -r action; do
  if ! grep -Eq "^[[:space:]]{2}${action}\\)" "$scenario"; then
    echo "Orchestrator references unsupported action: $action" >&2
    exit 1
  fi
done < <("$repo_root/ci/run-disaster-recovery.sh" --list-actions)

workflow="$repo_root/.github/workflows/bootstrap-user-journey.yml"
grep -F "contains(github.event.pull_request.labels.*.name, 'release-candidate')" "$workflow" >/dev/null
grep -F 'inputs.candidate_sha || github.event.pull_request.head.sha || github.sha' "$workflow" >/dev/null
grep -F 'needs: [main-to-candidate-disaster-recovery]' "$workflow" >/dev/null

test_root="$(mktemp -d /tmp/admin-dr-promotion-test.XXXXXX)"
trap 'rm -rf "$test_root"' EXIT
candidate_sha="0123456789abcdef0123456789abcdef01234567"

write_evidence() {
  local variant="$1" tested_sha="$2"
  jq -n --arg sha "$tested_sha" --arg variant "$variant" \
    '{schema_version: 1, tested_commit: $sha, variant: $variant,
      backup_manifest: {cli_revision: $sha, complete: true},
      restore_validation: {status: "passed", sentinels: "passed", rotated_secrets: "passed", service_validations: "passed"}}' \
    >"$test_root/evidence-$variant.json"
}

write_evidence standard "$candidate_sha"
if "$repo_root/ci/validate-dr-promotion.sh" "$candidate_sha" "$test_root" >/dev/null 2>&1; then
  echo "promotion unexpectedly passed without offline-images evidence" >&2
  exit 1
fi
write_evidence offline-images "ffffffffffffffffffffffffffffffffffffffff"
if "$repo_root/ci/validate-dr-promotion.sh" "$candidate_sha" "$test_root" >/dev/null 2>&1; then
  echo "promotion unexpectedly accepted stale evidence" >&2
  exit 1
fi
write_evidence offline-images "$candidate_sha"
"$repo_root/ci/validate-dr-promotion.sh" "$candidate_sha" "$test_root" >/dev/null
