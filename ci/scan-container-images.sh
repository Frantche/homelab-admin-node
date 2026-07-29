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

scan_failed=0
while IFS= read -r image; do
  identifier="$(printf '%s' "$image" | sha256sum | cut -c1-16)"
  echo "Scanning $image"
  if ! trivy image --quiet --scanners vuln --format json \
    --output "$ARTIFACT_DIR/$identifier.trivy.json" "$image"; then
    echo "$image: Trivy vulnerability scan failed" >&2
    scan_failed=1
    continue
  fi
  if ! trivy image --quiet --scanners vuln --format cyclonedx \
    --output "$ARTIFACT_DIR/$identifier.cdx.json" "$image"; then
    echo "$image: CycloneDX SBOM generation failed" >&2
    scan_failed=1
  fi
  if ! "$REPO_ROOT/scripts/image_security_policy.py" evaluate \
    --image "$image" --report "$ARTIFACT_DIR/$identifier.trivy.json"; then
    scan_failed=1
  fi
done < <("$REPO_ROOT/scripts/image_security_policy.py" inventory)

if ((scan_failed != 0)); then
  echo "One or more image scans or policy evaluations failed; see the complete results above." >&2
  exit 1
fi
