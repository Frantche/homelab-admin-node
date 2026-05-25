#!/usr/bin/env bash
set -euo pipefail

scenario="${1:-fresh-branch}"
case "$scenario" in
  fresh-branch|upgrade-main-to-branch|restore-main-backup-with-branch) ;;
  *) echo "Unknown scenario: $scenario" >&2; exit 1 ;;
esac

CI_MOCK_PIHOLE=${CI_MOCK_PIHOLE:-true}
CI_MOCK_CLOUDFLARE_TUNNEL=${CI_MOCK_CLOUDFLARE_TUNNEL:-true}
CI_MOCK_APIS=${CI_MOCK_APIS:-true}
SKIP_PUBLIC_URL_VALIDATION=${SKIP_PUBLIC_URL_VALIDATION:-true}
export CI_MOCK_PIHOLE CI_MOCK_CLOUDFLARE_TUNNEL CI_MOCK_APIS SKIP_PUBLIC_URL_VALIDATION

"./ci/scenarios/${scenario}.sh"
