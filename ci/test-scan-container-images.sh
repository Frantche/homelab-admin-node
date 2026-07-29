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
if PATH="$TEST_DIR/bin:$PATH" "$REPO_ROOT/ci/scan-container-images.sh" "$TEST_DIR/artifacts"; then
  echo "scan unexpectedly accepted the sentinel vulnerabilities" >&2
  exit 1
fi

image_count="$("$REPO_ROOT/scripts/image_security_policy.py" inventory | wc -l)"
call_count="$(wc -l < "$TRIVY_CALL_LOG")"
if [[ "$call_count" -ne "$((image_count * 2))" ]]; then
  echo "scan stopped early: got $call_count Trivy calls, expected $((image_count * 2))" >&2
  exit 1
fi
