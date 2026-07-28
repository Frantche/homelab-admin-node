#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARTIFACT_DIR="${1:-$REPO_ROOT/.ci/artifacts/image-security}"

command -v trivy >/dev/null || {
  echo "trivy is required to scan container images" >&2
  exit 2
}

mkdir -p "$ARTIFACT_DIR"
"$REPO_ROOT/scripts/image_security_policy.py" validate

while IFS= read -r image; do
  identifier="$(printf '%s' "$image" | sha256sum | cut -c1-16)"
  echo "Scanning $image"
  trivy image --quiet --scanners vuln --format json \
    --output "$ARTIFACT_DIR/$identifier.trivy.json" "$image"
  trivy image --quiet --scanners vuln --format cyclonedx \
    --output "$ARTIFACT_DIR/$identifier.cdx.json" "$image"
  "$REPO_ROOT/scripts/image_security_policy.py" evaluate \
    --image "$image" --report "$ARTIFACT_DIR/$identifier.trivy.json"
done < <("$REPO_ROOT/scripts/image_security_policy.py" inventory)
