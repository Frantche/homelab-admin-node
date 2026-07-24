#!/usr/bin/env bash
set -euo pipefail

CI_ARCH_IMAGE="${CI_ARCH_IMAGE:-$PWD/.ci/cache/arch.qcow2}"
CI_SSH_KEY="${CI_SSH_KEY:-$PWD/.ci/ssh/id_ed25519}"

ci_vm_download_image() {
  install -d -m 0755 "$(dirname "$CI_ARCH_IMAGE")"
  if [[ ! -s "$CI_ARCH_IMAGE" ]]; then
    curl --fail --location --retry 3 \
      --output "$CI_ARCH_IMAGE" \
      https://geo.mirror.pkgbuild.com/images/latest/Arch-Linux-x86_64-cloudimg.qcow2
  fi
}

ci_vm_generate_ssh_key() {
  install -d -m 0700 "$(dirname "$CI_SSH_KEY")"
  if [[ ! -f "$CI_SSH_KEY" ]]; then
    ssh-keygen -t ed25519 -N "" -f "$CI_SSH_KEY"
  fi
  chmod 0600 "$CI_SSH_KEY"
}

ci_vm_create() {
  local vm_dir="$1"
  local vm_name="$2"
  local repo_url="$3"
  local repo_ref="$4"

  if [[ ! "$repo_ref" =~ ^[0-9a-fA-F]{40}$ ]]; then
    echo "ERROR: VM repository ref must be a full commit SHA" >&2
    return 1
  fi
  case "$vm_dir" in
    "$PWD"/.ci/vms/*) ;;
    *)
      echo "ERROR: VM directory must be below $PWD/.ci/vms" >&2
      return 1
      ;;
  esac

  ci_vm_download_image
  ci_vm_generate_ssh_key
  install -d -m 0755 "$vm_dir"
  cp --reflink=auto "$CI_ARCH_IMAGE" "$vm_dir/disk.qcow2"
  qemu-img resize "$vm_dir/disk.qcow2" 20G

  CI_VM_DIR="$vm_dir" \
    CI_SSH_PUBLIC_KEY="$CI_SSH_KEY.pub" \
    REPO_URL="$repo_url" \
    REPO_REF="$repo_ref" \
    python3 "$PWD/ci/render-bootstrap-cloud-init.py"

  cat >"$vm_dir/meta-data" <<EOF
instance-id: ${vm_name}
local-hostname: ${vm_name}
EOF
  cloud-localds "$vm_dir/seed.img" "$vm_dir/user-data" "$vm_dir/meta-data"
}

ci_vm_start() {
  local vm_dir="$1"
  local ssh_port="$2"
  local kvm_flag=()
  local cpu_type="qemu64"

  if [[ -e /dev/kvm ]]; then
    kvm_flag=(-enable-kvm)
    cpu_type="host"
  fi

  qemu-system-x86_64 \
    "${kvm_flag[@]}" \
    -m 4096 \
    -smp 2 \
    -cpu "$cpu_type" \
    -nographic \
    -drive "file=$vm_dir/disk.qcow2,format=qcow2,if=virtio" \
    -drive "file=$vm_dir/seed.img,format=raw,if=virtio" \
    -netdev "user,id=net0,hostfwd=tcp::${ssh_port}-:22" \
    -device virtio-net-pci,netdev=net0 \
    -serial mon:stdio \
    >"$vm_dir/qemu.log" 2>&1 &
  echo "$!" >"$vm_dir/qemu.pid"
}

ci_vm_ssh() {
  local ssh_port="$1"
  shift
  ssh \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    -o ConnectTimeout=5 \
    -i "$CI_SSH_KEY" \
    -p "$ssh_port" \
    admin@127.0.0.1 "$@"
}

ci_vm_scp_to() {
  local ssh_port="$1"
  local source="$2"
  local destination="$3"
  scp \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    -i "$CI_SSH_KEY" \
    -P "$ssh_port" \
    "$source" "admin@127.0.0.1:$destination"
}

ci_vm_scp_from() {
  local ssh_port="$1"
  local source="$2"
  local destination="$3"
  scp \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    -i "$CI_SSH_KEY" \
    -P "$ssh_port" \
    "admin@127.0.0.1:$source" "$destination"
}

ci_vm_wait() {
  local ssh_port="$1"
  local vm_dir="$2"

  for attempt in $(seq 1 180); do
    if ci_vm_ssh "$ssh_port" "echo ready" >/dev/null 2>&1; then
      break
    fi
    if [[ "$attempt" -eq 180 ]]; then
      tail -100 "$vm_dir/qemu.log" >&2 || true
      echo "ERROR: VM SSH did not become ready" >&2
      return 1
    fi
    sleep 5
  done

  for attempt in $(seq 1 360); do
    if ci_vm_ssh "$ssh_port" "test -f /var/lib/cloud/instance/boot-finished" >/dev/null 2>&1; then
      return 0
    fi
    if [[ "$attempt" -eq 360 ]]; then
      ci_vm_ssh "$ssh_port" "sudo tail -100 /var/log/cloud-init-output.log" >&2 || true
      echo "ERROR: cloud-init did not complete" >&2
      return 1
    fi
    sleep 5
  done
}

ci_vm_stop() {
  local vm_dir="$1"
  if [[ -f "$vm_dir/qemu.pid" ]]; then
    kill "$(cat "$vm_dir/qemu.pid")" 2>/dev/null || true
    wait "$(cat "$vm_dir/qemu.pid")" 2>/dev/null || true
    rm -f "$vm_dir/qemu.pid"
  fi
}

ci_vm_destroy() {
  local vm_dir="$1"
  case "$vm_dir" in
    "$PWD"/.ci/vms/*)
      ci_vm_stop "$vm_dir"
      rm -f \
        "$vm_dir/disk.qcow2" \
        "$vm_dir/seed.img" \
        "$vm_dir/user-data" \
        "$vm_dir/meta-data"
      ;;
    *)
      echo "ERROR: refusing to destroy unexpected VM directory $vm_dir" >&2
      return 1
      ;;
  esac
}

ci_vm_collect_logs() {
  local ssh_port="$1"
  local vm_dir="$2"
  local output_dir="$3"
  install -d -m 0755 "$output_dir"
  cp "$vm_dir/qemu.log" "$output_dir/qemu.log" 2>/dev/null || true
  if ci_vm_ssh "$ssh_port" "echo ready" >/dev/null 2>&1; then
    ci_vm_ssh "$ssh_port" "sudo cat /var/log/cloud-init-output.log" >"$output_dir/cloud-init-output.log" 2>/dev/null || true
    ci_vm_ssh "$ssh_port" "sudo journalctl --no-pager" >"$output_dir/journal.log" 2>/dev/null || true
    ci_vm_ssh "$ssh_port" "sudo docker ps -a" >"$output_dir/docker-ps.txt" 2>/dev/null || true
    ci_vm_ssh "$ssh_port" \
      "for container in \$(sudo docker ps -a --format '{{.Names}}'); do echo \"--- \$container ---\"; sudo docker logs \"\$container\" 2>&1; done" \
      >"$output_dir/docker-logs.txt" 2>/dev/null || true
  fi
}
