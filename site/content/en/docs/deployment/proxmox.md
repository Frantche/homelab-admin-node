---
title: Proxmox From A To Z
weight: 10
---

This tutorial imports an Arch Linux cloud image into Proxmox, creates a reusable cloud-init template, and deploys the `admin-01` VM.

## Variables

Adjust these values for your environment:

```bash
PROXMOX_HOST=192.168.1.100
VMID=9000
NEW_VMID=101
STORAGE=local-lvm
BRIDGE=vmbr0
ADMIN_IP=192.168.1.10
GATEWAY=192.168.1.1
NAMESERVERS="192.168.1.2 1.1.1.1"
```

## Download the cloud image

Run on the Proxmox node:

```bash
wget -O /tmp/arch-cloud.qcow2 \
  https://geo.mirror.pkgbuild.com/images/latest/Arch-Linux-x86_64-cloudimg.qcow2
```

The current Arch cloud image commonly uses a Btrfs root filesystem, but the storage-isolation role still verifies the target filesystem during convergence. The `STORAGE=local-lvm` value below is Proxmox host storage for the VM disk; it does not create LVM inside the guest OS.

## Create the template VM

```bash
qm create "$VMID" --name "arch-cloud-template" \
  --memory 2048 \
  --cores 2 \
  --net0 "virtio,bridge=$BRIDGE" \
  --ostype l26 \
  --agent enabled=1

qm importdisk "$VMID" /tmp/arch-cloud.qcow2 "$STORAGE"
qm set "$VMID" --scsihw virtio-scsi-pci --scsi0 "${STORAGE}:vm-${VMID}-disk-0"
qm set "$VMID" --boot order=scsi0
qm set "$VMID" --ide2 "${STORAGE}:cloudinit"
qm set "$VMID" --serial0 socket --vga serial0
qm set "$VMID" --ciuser admin
qm set "$VMID" --sshkeys ~/.ssh/id_ed25519.pub
qm set "$VMID" --ipconfig0 ip=dhcp
qm template "$VMID"
```

## Clone the admin VM

```bash
qm clone "$VMID" "$NEW_VMID" --name "admin-01" --full
qm set "$NEW_VMID" --memory 4096 --cores 4
qm resize "$NEW_VMID" scsi0 +18G
qm set "$NEW_VMID" --ipconfig0 "ip=${ADMIN_IP}/24,gw=${GATEWAY}"
qm set "$NEW_VMID" --nameserver "$NAMESERVERS"
```

## Attach repository cloud-init

Copy the repository user-data file to a Proxmox snippets storage:

```bash
scp cloud-init/admin-01.user-data.yaml \
  "root@${PROXMOX_HOST}:/var/lib/vz/snippets/admin-01.user-data.yaml"
```

Enable `snippets` content on the `local` storage if needed, then attach it:

```bash
qm set "$NEW_VMID" --cicustom "user=local:snippets/admin-01.user-data.yaml"
qm start "$NEW_VMID"
```

## Verify first boot

```bash
ssh "admin@${ADMIN_IP}"
cloud-init status --wait
systemctl status admin-converge.timer
ls -la /opt/homelab-admin-node
cat /etc/admin-node/mode
```

The expected initial mode is `locked`.

## Troubleshooting

| Problem | Check |
| --- | --- |
| VM does not boot | Confirm `qm set --boot order=scsi0` and the imported disk name. |
| No network | Confirm bridge, IP configuration, gateway, and nameservers. |
| cloud-init does not run | Confirm the cloud-init drive exists and the snippet storage supports `snippets`. |
| SSH refused | Wait for cloud-init, then verify the public key and VM network. |
| Repository missing | Inspect `/var/log/cloud-init-output.log` and network access from the VM. |
