#!/usr/bin/env bash
set -euo pipefail

if [[ ! -e /dev/kvm ]]; then
  echo "KVM not available, QEMU will run without hardware acceleration"
  exit 0
fi

echo 'KERNEL=="kvm", GROUP="kvm", MODE="0666", OPTIONS+="static_node=kvm"' |
  sudo tee /etc/udev/rules.d/99-kvm4all.rules >/dev/null
sudo udevadm control --reload-rules
if ! sudo udevadm trigger --name-match=kvm; then
  echo "Warning: udev trigger for /dev/kvm failed; continuing" >&2
fi
ls -l /dev/kvm
