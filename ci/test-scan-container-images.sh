#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_DIR="$(mktemp -d)"

cleanup() {
  rm -rf "$TEST_DIR"
}
trap cleanup EXIT

mkdir -p "$TEST_DIR/bin" "$TEST_DIR/artifacts"
install -m 0755 /dev/stdin "$TEST_DIR/bin/trivy" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

output=""
format=""
while (($# > 0)); do
  case "$1" in
    --format)
      format="$2"
      shift 2
      ;;
    --output)
      output="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done

printf '%s\n' "$format" >> "$TRIVY_CALL_LOG"
if [[ "$format" == "json" ]]; then
  printf '%s\n' '{"Results":[{"Vulnerabilities":[{"VulnerabilityID":"CVE-SCAN-SENTINEL","PkgName":"sentinel","Severity":"CRITICAL","FixedVersion":"2.0.0"}]}]}' > "$output"
else
  printf '%s\n' '{"bomFormat":"CycloneDX"}' > "$output"
fi
SH

export TRIVY_CALL_LOG="$TEST_DIR/trivy-calls"
if scan_output="$(PATH="$TEST_DIR/bin:$PATH" "$REPO_ROOT/ci/scan-container-images.sh" "$TEST_DIR/artifacts" 2>&1)"; then
  echo "scan unexpectedly accepted the sentinel vulnerabilities" >&2
  exit 1
fi

if ! grep -Fq "::warning title=Third-party image vulnerabilities::" <<<"$scan_output"; then
  echo "scan did not emit a GitHub Actions warning for third-party images" >&2
  exit 1
fi
if ! grep -Fq "ghcr.io/frantche/gitea-backup-restore-process" <<<"$scan_output"; then
  echo "scan did not strictly reject the Frantche-owned image" >&2
  exit 1
fi
if ! grep -Fq "CVE-SCAN-SENTINEL (sentinel) fixable in 2.0.0" <<<"$scan_output"; then
  echo "scan warning did not list the vulnerability finding" >&2
  exit 1
fi

sentinel_report="$TEST_DIR/sentinel-report.json"
printf '%s\n' '{"Results":[{"Vulnerabilities":[{"VulnerabilityID":"CVE-SCAN-SENTINEL","PkgName":"sentinel","Severity":"CRITICAL","FixedVersion":"2.0.0"}]}]}' > "$sentinel_report"
if ! "$REPO_ROOT/scripts/image_security_policy.py" evaluate \
  --image "example.invalid/app:1@sha256:$(printf 'b%.0s' {1..64})" \
  --report "$sentinel_report" >/dev/null 2>&1; then
  echo "third-party vulnerability finding was unexpectedly blocking" >&2
  exit 1
fi
if "$REPO_ROOT/scripts/image_security_policy.py" evaluate \
  --image "ghcr.io/frantche/app:1@sha256:$(printf 'c%.0s' {1..64})" \
  --report "$sentinel_report" >/dev/null 2>&1; then
  echo "Frantche-owned vulnerability finding was unexpectedly accepted" >&2
  exit 1
fi

image_count="$("$REPO_ROOT/scripts/image_security_policy.py" inventory | wc -l)"
call_count="$(wc -l < "$TRIVY_CALL_LOG")"
if [[ "$call_count" -ne "$((image_count * 2))" ]]; then
  echo "scan stopped early: got $call_count Trivy calls, expected $((image_count * 2))" >&2
  exit 1
fi
