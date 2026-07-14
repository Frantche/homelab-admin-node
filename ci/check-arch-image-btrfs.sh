#!/usr/bin/env bash
set -euo pipefail

image="${1:-}"
if [[ -z "$image" ]]; then
  echo "usage: $0 <arch-cloud-image.qcow2>" >&2
  exit 2
fi

if [[ ! -f "$image" ]]; then
  echo "[arch-image-btrfs] image not found: $image" >&2
  exit 1
fi

sudo modprobe nbd max_part=8

nbd_device=""
for candidate in /dev/nbd{0..15}; do
  [[ -b "$candidate" ]] || continue
  candidate_base="$(basename "$candidate")"
  if [[ ! -e "/sys/block/${candidate_base}/pid" ]]; then
    nbd_device="$candidate"
    break
  fi
done

if [[ -z "$nbd_device" ]]; then
  echo "[arch-image-btrfs] no free /dev/nbd device found" >&2
  exit 1
fi

cleanup() {
  sudo qemu-nbd --disconnect "$nbd_device" >/dev/null 2>&1 || true
}
trap cleanup EXIT

sudo qemu-nbd --read-only --connect="$nbd_device" "$image"
sleep 1

echo "[arch-image-btrfs] inspecting $image via $nbd_device"
lsblk -f "$nbd_device"

if ! lsblk -nr -o FSTYPE "$nbd_device" | grep -qx "btrfs"; then
  echo "[arch-image-btrfs] expected the Arch cloud image root filesystem to be btrfs" >&2
  exit 1
fi

echo "[arch-image-btrfs] btrfs filesystem found"
