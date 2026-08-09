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
grep -F 'github.ref_name == github.event.repository.default_branch' "$workflow" >/dev/null

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

mkdir -p "$test_root/duplicate"
cp "$test_root/evidence-standard.json" "$test_root/duplicate/evidence-standard.json"
if "$repo_root/ci/validate-dr-promotion.sh" "$candidate_sha" "$test_root" >/dev/null 2>&1; then
  echo "promotion unexpectedly accepted ambiguous duplicate evidence" >&2
  exit 1
fi

grep -F "ref: \"\${{ github.sha }}\"" "$workflow" >/dev/null
if sed -n '/promote-tested-candidate:/,$p' "$workflow" | grep -F "ref: \"\${{ inputs.candidate_sha || github.sha }}\"" >/dev/null; then
  echo "promotion job must not execute candidate-controlled tooling with write permission" >&2
  exit 1
fi
promotion_job="$(sed -n '/promote-tested-candidate:/,$p' "$workflow")"
# shellcheck disable=SC2016 # Match the literal runtime variable in workflow source.
grep -F 'pip install --user -r "$GITHUB_WORKSPACE/ci/requirements.txt"' <<<"$promotion_job" >/dev/null
if grep -F 'pip install' <<<"$promotion_job" | grep -F 'candidate_dir' >/dev/null; then
  echo "promotion job must not install candidate-controlled Python dependencies" >&2
  exit 1
fi
draft_line="$(grep -nF -- '--draft' <<<"$promotion_job" | head -n1 | cut -d: -f1)"
# shellcheck disable=SC2016 # Match the literal workflow shell expression.
push_line="$(grep -nF 'git push origin "refs/tags/$RELEASE_TAG"' <<<"$promotion_job" | head -n1 | cut -d: -f1)"
# shellcheck disable=SC2016 # Match the literal workflow shell expression.
publish_line="$(grep -nF 'gh release edit "$RELEASE_TAG" --draft=false' <<<"$promotion_job" | head -n1 | cut -d: -f1)"
if [[ -z "$draft_line" || -z "$push_line" || -z "$publish_line" || "$draft_line" -ge "$push_line" || "$push_line" -ge "$publish_line" ]]; then
  echo "promotion must stage a draft release before pushing the tag and publish it only afterwards" >&2
  exit 1
fi
