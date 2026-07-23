#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"
source "$REPO_ROOT/ci/lib/arch-vm.sh"
# shellcheck disable=SC1091
source "$REPO_ROOT/.ci/garage/runtime.env"

MAIN_SHA="${MAIN_SHA:?MAIN_SHA is required}"
CANDIDATE_SHA="${CANDIDATE_SHA:?CANDIDATE_SHA is required}"
MAIN_REPO_URL="${MAIN_REPO_URL:?MAIN_REPO_URL is required}"
CANDIDATE_REPO_URL="${CANDIDATE_REPO_URL:?CANDIDATE_REPO_URL is required}"
SSH_PORT="${SSH_PORT:-2222}"
SOURCE_VM_DIR="$REPO_ROOT/.ci/vms/main-source"
TARGET_VM_DIR="$REPO_ROOT/.ci/vms/candidate-restore"
RECOVERY_DIR="$REPO_ROOT/.ci/recovery-kit"
ARTIFACT_DIR="$REPO_ROOT/.ci/artifacts/disaster-recovery"
GUEST_OFFSITE_ENV="$REPO_ROOT/.ci/garage/guest-offsite.env"
OFFSITE_CONFIG_HELPER="$REPO_ROOT/ci/configure-bootstrap-offsite.py"
CONFIG_REPO_DIR="/etc/admin-config/homelab-node-admin-config"
CI_VARS="$CONFIG_REPO_DIR/hosts/group_vars/ci-bootstrap-vars.yml"
active_vm_dir=""

install -d -m 0700 "$RECOVERY_DIR"
install -d -m 0755 "$ARTIFACT_DIR"

cleanup() {
  local status=$?
  if [[ -n "$active_vm_dir" ]]; then
    ci_vm_collect_logs "$SSH_PORT" "$active_vm_dir" "$ARTIFACT_DIR/active-vm" || true
    ci_vm_destroy "$active_vm_dir" || true
  fi
  if [[ -f "$REPO_ROOT/.ci/garage/socat.pid" ]]; then
    kill "$(cat "$REPO_ROOT/.ci/garage/socat.pid")" 2>/dev/null || true
  fi
  docker logs "$CI_GARAGE_CONTAINER" >"$ARTIFACT_DIR/garage.log" 2>&1 || true
  cp "$REPO_ROOT/.ci/garage/socat.log" "$ARTIFACT_DIR/socat.log" 2>/dev/null || true
  docker rm -f "$CI_GARAGE_CONTAINER" >/dev/null 2>&1 || true
  rm -f "$RECOVERY_DIR/recovery-kit.tgz" "$GUEST_OFFSITE_ENV"
  sudo rm -rf "$REPO_ROOT/.ci/garage"
  exit "$status"
}
trap cleanup EXIT

{
  printf 'CI_RESTIC_OFFSITE_ENDPOINT=%q\n' "$CI_RESTIC_OFFSITE_ENDPOINT"
  printf 'CI_RESTIC_OFFSITE_ACCESS_KEY=%q\n' "$CI_RESTIC_OFFSITE_ACCESS_KEY"
  printf 'CI_RESTIC_OFFSITE_SECRET_KEY=%q\n' "$CI_RESTIC_OFFSITE_SECRET_KEY"
  printf 'CI_RESTIC_OFFSITE_PASSWORD=%q\n' "$CI_RESTIC_OFFSITE_PASSWORD"
  printf 'CI_RESTIC_OFFSITE_CACERT=%q\n' "$CI_RESTIC_OFFSITE_CACERT"
  printf 'ADMIN_REPO_URL=%q\n' "$MAIN_REPO_URL"
} >"$GUEST_OFFSITE_ENV"
chmod 0600 "$GUEST_OFFSITE_ENV"

vm_ssh() {
  ci_vm_ssh "$SSH_PORT" "$@"
}

install_offsite_access() {
  ci_vm_scp_to "$SSH_PORT" "$CI_GARAGE_CA_FILE" /tmp/ci-garage-ca.crt
  ci_vm_scp_to "$SSH_PORT" "$GUEST_OFFSITE_ENV" /tmp/ci-offsite.env
  vm_ssh "sudo install -m 0644 /tmp/ci-garage-ca.crt /etc/ssl/certs/ci-garage-ca.crt && \
    sudo install -D -m 0644 /tmp/ci-garage-ca.crt /etc/ca-certificates/trust-source/anchors/ci-garage-ca.crt && \
    sudo update-ca-trust"
  vm_ssh "if ! grep -qF garage.test /etc/hosts; then \
      echo '10.0.2.2 garage.test' | sudo tee -a /etc/hosts >/dev/null; \
    fi; \
    sudo install -m 0600 /tmp/ci-offsite.env /etc/admin-node/ci-offsite.env"
}

run_converge() {
  local extra_args="${1:-}"
  vm_ssh "sudo CI_MOCK_PIHOLE=true \
    CI_MOCK_CLOUDFLARE_TUNNEL=true \
    CI_SKIP_PUBLIC_URL_VALIDATION=true \
    SKIP_PUBLIC_URL_VALIDATION=true \
    ADMIN_CONVERGE_SKIP_GIT_PULL=true \
    ANSIBLE_EXTRA_ARGS='-e @${CI_VARS} ${extra_args}' \
    /opt/homelab-admin-node/bin/admin-node converge run"
}

run_validations() {
  vm_ssh "sudo CI_MOCK_PIHOLE=true \
    CI_MOCK_CLOUDFLARE_TUNNEL=true \
    CI_SKIP_PUBLIC_URL_VALIDATION=true \
    SKIP_PUBLIC_URL_VALIDATION=true \
    /opt/homelab-admin-node/bin/admin-node validate all"
  vm_ssh "sudo /opt/homelab-admin-node/ci/run-oidc-user-journey.sh"
}

run_backup() {
  vm_ssh "sudo CI_MOCK_PIHOLE=true \
    CI_MOCK_CLOUDFLARE_TUNNEL=true \
    CI_SKIP_PUBLIC_URL_VALIDATION=true \
    SKIP_PUBLIC_URL_VALIDATION=true \
    /opt/homelab-admin-node/bin/admin-node backup run"
}

echo "=== Deploying main baseline on the source VM ==="
ci_vm_create "$SOURCE_VM_DIR" admin-main-source "$MAIN_REPO_URL" "$MAIN_SHA"
ci_vm_start "$SOURCE_VM_DIR" "$SSH_PORT"
active_vm_dir="$SOURCE_VM_DIR"
ci_vm_wait "$SSH_PORT" "$SOURCE_VM_DIR"
install_offsite_access

vm_ssh "sudo bash -c 'set -a; source /etc/admin-node/ci-offsite.env; set +a; \
  /opt/homelab-admin-node/ci/setup-bootstrap-config-repo.sh'"
ci_vm_scp_to "$SSH_PORT" "$OFFSITE_CONFIG_HELPER" /tmp/configure-bootstrap-offsite.py
vm_ssh "sudo bash -c 'set -a; source /etc/admin-node/ci-offsite.env; set +a; \
  python3 /tmp/configure-bootstrap-offsite.py /etc/admin-config/homelab-node-admin-config'"
vm_ssh "sudo CI_MOCK_PIHOLE=true \
  CI_MOCK_CLOUDFLARE_TUNNEL=true \
  CI_SKIP_PUBLIC_URL_VALIDATION=true \
  SKIP_PUBLIC_URL_VALIDATION=true \
  CI_SKIP_LOCAL_RESTORE=true \
  /opt/homelab-admin-node/ci/scenarios/bootstrap-user-journey.sh"

echo "=== Creating a main backup in Garage ==="
run_backup
backup_id="$(vm_ssh "sudo find /srv/admin/backups/local -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | sort | tail -n1")"
if [[ ! "$backup_id" =~ ^[0-9]{8}-[0-9]{6}$ ]]; then
  echo "ERROR: invalid backup ID returned by source VM: $backup_id" >&2
  exit 1
fi
manifest_revision="$(vm_ssh "sudo jq -r .cli_revision /srv/admin/backups/local/$backup_id/manifest.json")"
if [[ "$manifest_revision" != "$MAIN_SHA" ]]; then
  echo "ERROR: backup revision $manifest_revision does not match main $MAIN_SHA" >&2
  exit 1
fi
printf '%s\n' "$backup_id" >"$ARTIFACT_DIR/backup-id.txt"
vm_ssh "sudo cat /srv/admin/backups/local/$backup_id/manifest.json" >"$ARTIFACT_DIR/main-backup-manifest.json"

echo "=== Upgrading the deployed main node to the candidate SHA ==="
vm_ssh "sudo git -C /opt/homelab-admin-node fetch ${CANDIDATE_REPO_URL@Q} ${CANDIDATE_SHA@Q} && \
  sudo git -C /opt/homelab-admin-node checkout --detach FETCH_HEAD && \
  sudo /opt/homelab-admin-node/scripts/build-admin-node.sh"
candidate_revision="$(vm_ssh "sudo git -C /opt/homelab-admin-node rev-parse HEAD")"
if [[ "$candidate_revision" != "$CANDIDATE_SHA" ]]; then
  echo "ERROR: source VM did not switch to candidate SHA" >&2
  exit 1
fi
run_converge
run_validations

echo "=== Exporting only the encrypted recovery kit ==="
vm_ssh "sudo tar -C / -czf /tmp/admin-node-recovery-kit.tgz \
    etc/sops/age/keys.txt \
    etc/admin-config/homelab-node-admin-config \
    opt/homelab-admin-node/secrets/openbao-unseal.sops.yaml && \
  sudo chown admin:admin /tmp/admin-node-recovery-kit.tgz && \
  sudo chmod 0600 /tmp/admin-node-recovery-kit.tgz"
ci_vm_scp_from "$SSH_PORT" /tmp/admin-node-recovery-kit.tgz "$RECOVERY_DIR/recovery-kit.tgz"

ci_vm_collect_logs "$SSH_PORT" "$SOURCE_VM_DIR" "$ARTIFACT_DIR/source-vm"
ci_vm_destroy "$SOURCE_VM_DIR"
active_vm_dir=""
if [[ -e "$SOURCE_VM_DIR/disk.qcow2" ]]; then
  echo "ERROR: source VM disk still exists before recovery VM creation" >&2
  exit 1
fi

echo "=== Restoring the main backup on a fresh candidate VM ==="
ci_vm_create "$TARGET_VM_DIR" admin-candidate-restore "$CANDIDATE_REPO_URL" "$CANDIDATE_SHA"
ci_vm_start "$TARGET_VM_DIR" "$SSH_PORT"
active_vm_dir="$TARGET_VM_DIR"
ci_vm_wait "$SSH_PORT" "$TARGET_VM_DIR"
vm_ssh "sudo /opt/homelab-admin-node/scripts/build-admin-node.sh"
install_offsite_access
ci_vm_scp_to "$SSH_PORT" "$RECOVERY_DIR/recovery-kit.tgz" /tmp/admin-node-recovery-kit.tgz
vm_ssh "sudo tar -C / -xzf /tmp/admin-node-recovery-kit.tgz && \
  sudo chmod 0400 /etc/sops/age/keys.txt && \
  sudo chmod 0600 /opt/homelab-admin-node/secrets/openbao-unseal.sops.yaml"
vm_ssh "sudo /opt/homelab-admin-node/bin/admin-node mode set restore"
run_converge "-e restore_repository=offsite -e restore_id=$backup_id"

restored_revision="$(vm_ssh "sudo jq -r .cli_revision /srv/admin/backups/local/$backup_id/manifest.json")"
if [[ "$restored_revision" != "$MAIN_SHA" ]]; then
  echo "ERROR: fresh VM restored a backup not produced by main" >&2
  exit 1
fi
vm_ssh "sudo test -s /srv/admin/data/sentinel/value.txt"
run_converge
run_validations

echo "=== Rotating technical identities and database credentials ==="
vm_ssh "sudo /opt/homelab-admin-node/ci/rotate-bootstrap-config.sh prepare"
run_converge
vm_ssh "sudo /opt/homelab-admin-node/ci/validate-secret-rotation.sh"
run_validations
vm_ssh "sudo /opt/homelab-admin-node/ci/rotate-bootstrap-config.sh finalize"
run_converge
vm_ssh "sudo rm -f /tmp/admin-node-secret-rotation-audit.json"

echo "=== Creating the post-rotation offsite backup ==="
run_backup
post_rotation_id="$(vm_ssh "sudo find /srv/admin/backups/local -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | sort | tail -n1")"
vm_ssh "sudo cat /srv/admin/backups/local/$post_rotation_id/manifest.json" >"$ARTIFACT_DIR/post-rotation-manifest.json"
run_validations

ci_vm_collect_logs "$SSH_PORT" "$TARGET_VM_DIR" "$ARTIFACT_DIR/target-vm"
ci_vm_destroy "$TARGET_VM_DIR"
active_vm_dir=""
echo "=== main-to-candidate-disaster-recovery PASSED ==="
