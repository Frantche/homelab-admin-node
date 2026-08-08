#!/usr/bin/env bash
set -euo pipefail

candidate_sha="${1:?usage: validate-dr-promotion.sh <candidate-sha> <evidence-dir>}"
evidence_dir="${2:?usage: validate-dr-promotion.sh <candidate-sha> <evidence-dir>}"

if [[ ! "$candidate_sha" =~ ^[0-9a-f]{40}$ ]]; then
  echo "candidate SHA must be a full immutable 40-character commit" >&2
  exit 2
fi
if [[ ! -d "$evidence_dir" ]]; then
  echo "DR evidence directory is missing: $evidence_dir" >&2
  exit 1
fi

for variant in standard offline-images; do
  evidence="$(find "$evidence_dir" -type f -name "evidence-$variant.json" -print -quit)"
  if [[ -z "$evidence" ]]; then
    echo "current-run DR evidence is missing for $variant" >&2
    exit 1
  fi
  jq -e \
    --arg sha "$candidate_sha" \
    --arg variant "$variant" \
    '.schema_version == 1 and
     .tested_commit == $sha and
     .variant == $variant and
     .backup_manifest.cli_revision == $sha and
     .backup_manifest.complete == true and
     .restore_validation.status == "passed" and
     .restore_validation.sentinels == "passed" and
     .restore_validation.rotated_secrets == "passed" and
     .restore_validation.service_validations == "passed"' \
    "$evidence" >/dev/null || {
      echo "DR evidence for $variant is stale, incomplete, or belongs to another commit" >&2
      exit 1
    }
done

printf 'DR promotion evidence is complete for %s (standard, offline-images)\n' "$candidate_sha"
