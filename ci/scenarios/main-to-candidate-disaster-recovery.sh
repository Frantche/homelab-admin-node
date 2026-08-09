#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"
source "$REPO_ROOT/ci/lib/arch-vm.sh"

ACTION="${1:?usage: main-to-candidate-disaster-recovery.sh <action>}"
if [[ -f "$REPO_ROOT/.ci/garage/runtime.env" ]]; then
  # shellcheck disable=SC1091
  source "$REPO_ROOT/.ci/garage/runtime.env"
elif [[ "$ACTION" != "cleanup" ]]; then
  echo "Garage runtime is not initialized" >&2
  exit 1
fi
CI_GARAGE_CONTAINER="${CI_GARAGE_CONTAINER:-admin-node-ci-garage}"
MAIN_SHA="${MAIN_SHA:?MAIN_SHA is required}"
CANDIDATE_SHA="${CANDIDATE_SHA:?CANDIDATE_SHA is required}"
MAIN_REPO_URL="${MAIN_REPO_URL:?MAIN_REPO_URL is required}"
CANDIDATE_REPO_URL="${CANDIDATE_REPO_URL:?CANDIDATE_REPO_URL is required}"
SSH_PORT="${SSH_PORT:-2222}"
DR_INCLUDE_IMAGES="${DR_INCLUDE_IMAGES:-false}"
if [[ "$DR_INCLUDE_IMAGES" != "true" && "$DR_INCLUDE_IMAGES" != "false" ]]; then
  echo "DR_INCLUDE_IMAGES must be true or false" >&2
  exit 2
fi
SOURCE_VM_DIR="$REPO_ROOT/.ci/vms/main-source"
FINAL_VM_DIR="$REPO_ROOT/.ci/vms/candidate-final-restore"
STATE_DIR="$REPO_ROOT/.ci/disaster-recovery"
CANDIDATE_RECOVERY_KIT="$STATE_DIR/candidate-recovery-kit.tgz"
CANDIDATE_BACKUP_ID_FILE="$STATE_DIR/candidate-backup-id"
SENTINEL_STATE="$STATE_DIR/data-sentinels.json"
ROTATION_AUDIT="$STATE_DIR/rotation-audit.json"
ARTIFACT_DIR="$REPO_ROOT/.ci/artifacts/disaster-recovery"
GUEST_OFFSITE_ENV="$STATE_DIR/guest-offsite.env"
CONFIG_REPO_DIR="/etc/admin-config/homelab-node-admin-config"
CI_VARS="$CONFIG_REPO_DIR/hosts/group_vars/ci-bootstrap-vars.yml"

install -d -m 0700 "$STATE_DIR"
install -d -m 0755 "$ARTIFACT_DIR"

vm_ssh() { ci_vm_ssh "$SSH_PORT" "$@"; }

write_guest_env() {
  {
    printf 'CI_RESTIC_OFFSITE_ENDPOINT=%q\n' "$CI_RESTIC_OFFSITE_ENDPOINT"
    printf 'CI_RESTIC_OFFSITE_ACCESS_KEY=%q\n' "$CI_RESTIC_OFFSITE_ACCESS_KEY"
    printf 'CI_RESTIC_OFFSITE_SECRET_KEY=%q\n' "$CI_RESTIC_OFFSITE_SECRET_KEY"
    printf 'CI_RESTIC_OFFSITE_PASSWORD=%q\n' "$CI_RESTIC_OFFSITE_PASSWORD"
    printf 'CI_RESTIC_OFFSITE_CACERT=%q\n' "$CI_RESTIC_OFFSITE_CACERT"
    printf 'ADMIN_REPO_URL=%q\n' "$MAIN_REPO_URL"
  } >"$GUEST_OFFSITE_ENV"
  chmod 0600 "$GUEST_OFFSITE_ENV"
}

install_offsite_access() {
  write_guest_env
  ci_vm_scp_to "$SSH_PORT" "$CI_GARAGE_CA_FILE" /tmp/ci-garage-ca.crt
  ci_vm_scp_to "$SSH_PORT" "$GUEST_OFFSITE_ENV" /tmp/ci-offsite.env
  vm_ssh "sudo install -m 0644 /tmp/ci-garage-ca.crt /etc/ssl/certs/ci-garage-ca.crt; \
    sudo install -D -m 0644 /tmp/ci-garage-ca.crt /etc/ca-certificates/trust-source/anchors/ci-garage-ca.crt; \
    sudo update-ca-trust; \
    grep -qF garage.test /etc/hosts || echo '10.0.2.2 garage.test' | sudo tee -a /etc/hosts >/dev/null; \
    for domain in keycloak.example.com bao.example.com harbor.example.com git.example.com traefik.example.com; do \
      grep -qF \"\$domain\" /etc/hosts || echo \"127.0.0.1 \$domain\" | sudo tee -a /etc/hosts >/dev/null; \
    done; \
    sudo install -m 0600 /tmp/ci-offsite.env /etc/admin-node/ci-offsite.env"
}

block_public_docker_registries() {
  vm_ssh "for domain in registry-1.docker.io auth.docker.io production.cloudflare.docker.com ghcr.io docker.gitea.com; do \
    grep -qF \"127.0.0.1 \$domain\" /etc/hosts || \
      echo \"127.0.0.1 \$domain\" | sudo tee -a /etc/hosts >/dev/null; \
    done"
}

run_converge() {
  local extra="${1:-}"
  vm_ssh "sudo CI_MOCK_PIHOLE=true CI_MOCK_CLOUDFLARE_TUNNEL=true \
    CI_SKIP_PUBLIC_URL_VALIDATION=true SKIP_PUBLIC_URL_VALIDATION=true \
    ADMIN_CONVERGE_SKIP_GIT_PULL=true \
    ANSIBLE_EXTRA_ARGS='-e @${CI_VARS} ${extra}' \
    /opt/homelab-admin-node/bin/admin-node converge run"
}

run_validations() {
  vm_ssh "sudo CI_MOCK_PIHOLE=true CI_MOCK_CLOUDFLARE_TUNNEL=true \
    CI_SKIP_PUBLIC_URL_VALIDATION=true SKIP_PUBLIC_URL_VALIDATION=true \
    /opt/homelab-admin-node/bin/admin-node validate all"
  vm_ssh "sudo /opt/homelab-admin-node/ci/run-oidc-user-journey.sh"
}

run_backup() {
  local image_args=""
  if [[ "$DR_INCLUDE_IMAGES" == "true" ]]; then
    image_args=" --include-images"
  fi
  vm_ssh "sudo CI_MOCK_PIHOLE=true CI_MOCK_CLOUDFLARE_TUNNEL=true OBSERVABILITY_ENABLED=true \
    CI_SKIP_PUBLIC_URL_VALIDATION=true SKIP_PUBLIC_URL_VALIDATION=true \
    /opt/homelab-admin-node/bin/admin-node backup run$image_args"
}

create_candidate_recovery_kit() {
  vm_ssh "sudo tar -C / -czf /tmp/admin-node-recovery-kit.tgz etc/sops/age/keys.txt \
    etc/admin-config/homelab-node-admin-config opt/homelab-admin-node/secrets/openbao-unseal.sops.yaml; \
    sudo chown admin:admin /tmp/admin-node-recovery-kit.tgz; sudo chmod 0600 /tmp/admin-node-recovery-kit.tgz"
  rm -f "$CANDIDATE_RECOVERY_KIT"
  ci_vm_scp_from "$SSH_PORT" /tmp/admin-node-recovery-kit.tgz "$CANDIDATE_RECOVERY_KIT"
  chmod 0600 "$CANDIDATE_RECOVERY_KIT"
}

install_sentinel_script() {
  ci_vm_scp_to "$SSH_PORT" "$REPO_ROOT/ci/disaster-recovery-sentinels.sh" \
    /tmp/disaster-recovery-sentinels.sh
  vm_ssh "sudo chmod 0700 /tmp/disaster-recovery-sentinels.sh"
}

validate_sentinels() {
  install_sentinel_script
  ci_vm_scp_to "$SSH_PORT" "$SENTINEL_STATE" /tmp/admin-node-data-sentinels.json
  vm_ssh "sudo /tmp/disaster-recovery-sentinels.sh validate /tmp/admin-node-data-sentinels.json"
}

restore_candidate_backup() {
  local backup_id restored_revision

  backup_id="$(<"$CANDIDATE_BACKUP_ID_FILE")"
  ci_vm_scp_to "$SSH_PORT" "$CANDIDATE_RECOVERY_KIT" /tmp/admin-node-recovery-kit.tgz
  vm_ssh "sudo tar -C / -xzf /tmp/admin-node-recovery-kit.tgz; sudo chmod 0400 /etc/sops/age/keys.txt; \
    sudo chmod 0600 /opt/homelab-admin-node/secrets/openbao-unseal.sops.yaml; \
    sudo /opt/homelab-admin-node/bin/admin-node mode set restore"
  run_converge "-e restore_repository=offsite -e restore_id=$backup_id"
  restored_revision="$(vm_ssh "sudo jq -r .cli_revision /srv/admin/backups/local/$backup_id/manifest.json")"
  [[ "$restored_revision" == "$CANDIDATE_SHA" ]] || {
    echo "restored revision $restored_revision does not match $CANDIDATE_SHA" >&2
    return 1
  }
  [[ "$(vm_ssh "sudo git -C /opt/homelab-admin-node rev-parse HEAD")" == "$CANDIDATE_SHA" ]]
  if [[ "$DR_INCLUDE_IMAGES" == "true" ]]; then
    vm_ssh "sudo jq -r '.offline_image_archives[].archive_tag' /srv/admin/backups/local/$backup_id/manifest.json | \
      while IFS= read -r image; do sudo docker image inspect \"\$image\" >/dev/null; done"
  fi
}

case "$ACTION" in
  create-source)
    ci_vm_create "$SOURCE_VM_DIR" admin-main-source "$MAIN_REPO_URL" "$MAIN_SHA"
    ci_vm_start "$SOURCE_VM_DIR" "$SSH_PORT"
    ci_vm_wait "$SSH_PORT" "$SOURCE_VM_DIR"
    install_offsite_access
    vm_ssh "sudo bash -c 'set -a; source /etc/admin-node/ci-offsite.env; set +a; /opt/homelab-admin-node/ci/setup-bootstrap-config-repo.sh'"
    ci_vm_scp_to "$SSH_PORT" "$REPO_ROOT/ci/configure-bootstrap-offsite.py" /tmp/configure-bootstrap-offsite.py
    vm_ssh "sudo bash -c 'set -a; source /etc/admin-node/ci-offsite.env; set +a; python3 /tmp/configure-bootstrap-offsite.py $CONFIG_REPO_DIR'"
    ;;
  deploy-main)
    vm_ssh "sudo CI_MOCK_PIHOLE=true CI_MOCK_CLOUDFLARE_TUNNEL=true \
      CI_SKIP_PUBLIC_URL_VALIDATION=true SKIP_PUBLIC_URL_VALIDATION=true \
      CI_SKIP_LOCAL_RESTORE=true make -C /opt/homelab-admin-node ci-bootstrap"
    ;;
  upgrade-candidate)
    vm_ssh "sudo git -C /opt/homelab-admin-node fetch --no-tags '$CANDIDATE_REPO_URL' '$CANDIDATE_SHA'; \
      sudo git -C /opt/homelab-admin-node checkout --detach '$CANDIDATE_SHA'; \
      sudo /opt/homelab-admin-node/scripts/build-admin-node.sh; \
      printf '%s\n' '$CANDIDATE_SHA' | sudo tee /etc/admin-node/release-name >/dev/null; \
      printf '%s\n' '$CANDIDATE_SHA' | sudo tee /etc/admin-node/release-ref >/dev/null"
    run_converge
    ;;
  reboot-candidate-hardening)
    vm_ssh "sudo reboot" || true
    sleep 10
    ci_vm_wait "$SSH_PORT" "$SOURCE_VM_DIR"
    vm_ssh "sudo /opt/homelab-admin-node/bin/admin-node validate hardening"
    vm_ssh "sudo /opt/homelab-admin-node/bin/admin-node openbao unseal"
    ;;
  create-sentinels)
    install_sentinel_script
    vm_ssh "sudo /tmp/disaster-recovery-sentinels.sh create /tmp/admin-node-data-sentinels.json && \
      sudo chown admin:admin /tmp/admin-node-data-sentinels.json"
    ci_vm_scp_from "$SSH_PORT" /tmp/admin-node-data-sentinels.json "$SENTINEL_STATE"
    chmod 0600 "$SENTINEL_STATE"
    ;;
  validate-upgraded-candidate)
    validate_sentinels
    run_validations
    ;;
  rotate-secrets)
    vm_ssh "sudo /opt/homelab-admin-node/ci/rotate-bootstrap-config.sh prepare"
    run_converge
    ;;
  validate-secret-rotation)
    vm_ssh "sudo /opt/homelab-admin-node/ci/validate-secret-rotation.sh"
    validate_sentinels
    run_validations
    ;;
  finalize-secret-rotation)
    vm_ssh "sudo /opt/homelab-admin-node/ci/rotate-bootstrap-config.sh finalize"
    run_converge
    vm_ssh "sudo chown admin:admin /tmp/admin-node-secret-rotation-audit.json; \
      sudo chmod 0600 /tmp/admin-node-secret-rotation-audit.json"
    ci_vm_scp_from "$SSH_PORT" /tmp/admin-node-secret-rotation-audit.json "$ROTATION_AUDIT"
    chmod 0600 "$ROTATION_AUDIT"
    vm_ssh "sudo rm -f /tmp/admin-node-secret-rotation-audit.json"
    ;;
  backup-candidate)
    run_backup
    post_id="$(vm_ssh "sudo find /srv/admin/backups/local -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | sort | tail -n1")"
    [[ "$post_id" =~ ^[0-9]{8}-[0-9]{6}$ ]] || { echo "invalid backup ID: $post_id" >&2; exit 1; }
    [[ "$(vm_ssh "sudo jq -r .cli_revision /srv/admin/backups/local/$post_id/manifest.json")" == "$CANDIDATE_SHA" ]]
    vm_ssh "sudo jq -e '.consistency == \"service-specific-consistency-boundaries\" and
        ([.consistency_boundaries[] | select(
          .service == \"gitea\" and
          (.method | startswith(\"application-quiesced-postgresql-dump-and-\")) and
          (.started_at | length > 0) and
          (.completed_at | length > 0)
        )] | length == 1)' /srv/admin/backups/local/$post_id/manifest.json >/dev/null"
    if [[ "$DR_INCLUDE_IMAGES" == "true" ]]; then
      vm_ssh "sudo jq -e '.offline_images == true and .stack_definitions == true and .repository_bundle == true and \
          (.active_stacks | index(\"cloudflared\") | not) and (.active_stacks | index(\"observability\") != null) and \
          ([.images[] | select(startswith(\"cloudflare/cloudflared:\"))] | length == 0) and \
          (.images | length > 0) and ((.offline_image_archives | length) == (.images | length))' \
        /srv/admin/backups/local/$post_id/manifest.json >/dev/null; \
        sudo test -s /srv/admin/backups/local/$post_id/offline-images.tar; \
        sudo test -s /srv/admin/backups/local/$post_id/repository.bundle; \
        sudo test -f /srv/admin/backups/local/$post_id/stack-definitions/gitea/compose.yaml; \
        sudo test -f /srv/admin/backups/local/$post_id/stack-definitions/observability/compose.yaml; \
        sudo test ! -e /srv/admin/backups/local/$post_id/stack-definitions/cloudflared"
    else
      vm_ssh "sudo jq -e '.offline_images == false and .stack_definitions == true and \
          (.active_stacks | length > 0) and (.active_stacks | index(\"cloudflared\") | not) and \
          (.active_stacks | index(\"observability\") != null) and ((.repository_bundle // false) == false)' \
        /srv/admin/backups/local/$post_id/manifest.json >/dev/null; \
        sudo test ! -e /srv/admin/backups/local/$post_id/offline-images.tar; \
        sudo test ! -e /srv/admin/backups/local/$post_id/repository.bundle; \
        sudo test -f /srv/admin/backups/local/$post_id/stack-definitions/gitea/compose.yaml; \
        sudo test -f /srv/admin/backups/local/$post_id/stack-definitions/observability/compose.yaml; \
        sudo test ! -e /srv/admin/backups/local/$post_id/stack-definitions/cloudflared"
    fi
    printf '%s\n' "$post_id" >"$CANDIDATE_BACKUP_ID_FILE"
    vm_ssh "sudo cat /srv/admin/backups/local/$post_id/manifest.json" >"$ARTIFACT_DIR/post-rotation-manifest.json"
    create_candidate_recovery_kit
    ;;
  destroy-source)
    ci_vm_collect_logs "$SSH_PORT" "$SOURCE_VM_DIR" "$ARTIFACT_DIR/source-vm"
    ci_vm_destroy "$SOURCE_VM_DIR"
    [[ ! -e "$SOURCE_VM_DIR/disk.qcow2" ]]
    ;;
  create-final-target)
    ci_vm_create "$FINAL_VM_DIR" admin-candidate-final-restore "$CANDIDATE_REPO_URL" "$CANDIDATE_SHA"
    ci_vm_start "$FINAL_VM_DIR" "$SSH_PORT"
    ci_vm_wait "$SSH_PORT" "$FINAL_VM_DIR"
    vm_ssh "sudo /opt/homelab-admin-node/scripts/build-admin-node.sh"
    install_offsite_access
    if [[ "$DR_INCLUDE_IMAGES" == "true" ]]; then
      block_public_docker_registries
    fi
    ;;
  restore-candidate)
    restore_candidate_backup
    ;;
  validate-final-candidate)
    validate_sentinels
    ci_vm_scp_to "$SSH_PORT" "$ROTATION_AUDIT" /tmp/admin-node-secret-rotation-audit.json
    vm_ssh "sudo /opt/homelab-admin-node/ci/validate-secret-rotation.sh \
      /tmp/admin-node-secret-rotation-audit.json"
    run_validations
    vm_ssh "sudo rm -f /tmp/admin-node-secret-rotation-audit.json \
      /tmp/admin-node-data-sentinels.json"
    ;;
  destroy-final-target)
    ci_vm_collect_logs "$SSH_PORT" "$FINAL_VM_DIR" "$ARTIFACT_DIR/final-vm"
    ci_vm_destroy "$FINAL_VM_DIR"
    ;;
  cleanup)
    ci_vm_collect_logs "$SSH_PORT" "$SOURCE_VM_DIR" "$ARTIFACT_DIR/source-vm-failure" || true
    ci_vm_collect_logs "$SSH_PORT" "$FINAL_VM_DIR" "$ARTIFACT_DIR/final-vm-failure" || true
    ci_vm_destroy "$SOURCE_VM_DIR" || true
    ci_vm_destroy "$FINAL_VM_DIR" || true
    docker logs "$CI_GARAGE_CONTAINER" >"$ARTIFACT_DIR/garage.log" 2>&1 || true
    cp "$REPO_ROOT/.ci/garage/socat.log" "$ARTIFACT_DIR/socat.log" 2>/dev/null || true
    [[ ! -f "$REPO_ROOT/.ci/garage/socat.pid" ]] || kill "$(<"$REPO_ROOT/.ci/garage/socat.pid")" 2>/dev/null || true
    docker rm -f "$CI_GARAGE_CONTAINER" >/dev/null 2>&1 || true
    rm -f "$CANDIDATE_RECOVERY_KIT" "$GUEST_OFFSITE_ENV" \
      "$CANDIDATE_BACKUP_ID_FILE" "$SENTINEL_STATE" "$ROTATION_AUDIT"
    sudo rm -rf "$REPO_ROOT/.ci/garage"
    ;;
  *) echo "unknown action: $ACTION" >&2; exit 2 ;;
esac
