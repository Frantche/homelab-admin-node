#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
"$SCRIPT_DIR/validate-apis.sh"
"$SCRIPT_DIR/validate-dns.sh"
"$SCRIPT_DIR/validate-cloudflare-tunnel.sh"
