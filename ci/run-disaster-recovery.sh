#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
scenario="$repo_root/ci/scenarios/main-to-candidate-disaster-recovery.sh"

actions=(
  create-source
  deploy-main
  create-sentinels
  upgrade-candidate
  reboot-candidate-hardening
  validate-upgraded-candidate
  rotate-secrets
  validate-secret-rotation
  finalize-secret-rotation
  backup-candidate
  destroy-source
  create-final-target
  restore-candidate
  validate-final-candidate
  destroy-final-target
)

if [[ "${1:-}" == "--list-actions" ]]; then
  printf '%s\n' "${actions[@]}"
  exit 0
fi

modes=("$@")
if ((${#modes[@]} == 0)); then
  modes=(standard offline-images)
fi

cleanup_required=false
cleanup() {
  if [[ "$cleanup_required" == true ]]; then
    "$scenario" cleanup
    cleanup_required=false
  fi
}
trap cleanup EXIT

for mode in "${modes[@]}"; do
  case "$mode" in
    standard) export DR_INCLUDE_IMAGES=false ;;
    offline-images) export DR_INCLUDE_IMAGES=true ;;
    *)
      echo "Unknown disaster-recovery mode: $mode" >&2
      exit 2
      ;;
  esac

  cleanup_required=true
  "$repo_root/ci/setup-garage.sh"
  for action in "${actions[@]}"; do
    "$scenario" "$action"
  done
  cleanup
done
