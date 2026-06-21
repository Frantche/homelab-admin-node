#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPORT_DIR="${HARDENING_REPORT_DIR:-$REPO_ROOT/ci/lynis}"
mkdir -p "$REPORT_DIR"

if ! command -v lynis >/dev/null 2>&1; then
  echo "[hardening-audit] installing lynis"
  pacman -Sy --noconfirm --needed lynis
fi

"$REPO_ROOT/scripts/validate-hardening.sh"

echo "[hardening-audit] running Lynis"
lynis audit system \
  --quick \
  --no-colors \
  --auditor "homelab-admin-node-ci" \
  --report-file "$REPORT_DIR/lynis-report.dat" \
  --log-file "$REPORT_DIR/lynis.log" \
  | tee "$REPORT_DIR/lynis-output.txt"

echo "[hardening-audit] Lynis report written to $REPORT_DIR"
