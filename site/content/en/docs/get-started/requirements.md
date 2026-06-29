---
title: Requirements
weight: 30
---

The expected production target is a Proxmox VM running Arch Linux from a cloud image.

You need:

- Proxmox VE with shell access to the node.
- A Linux bridge for the VM, usually `vmbr0`.
- An SSH key for the `admin` VM user.
- A private Git repository for deployment configuration.
- `age` and `sops` for encrypted secrets.
- DNS names for the services, either local-only or public.
- Optional Cloudflare account and tunnel if public ingress is required.
- Optional Pi-hole if local DNS records should be managed automatically.

The VM should have at least:

- 2 vCPU for testing, 4 vCPU recommended.
- 4 GiB RAM minimum.
- 20 GiB disk minimum, more if Harbor or local backups are used.

Development and CI use mocks for Pi-hole and Cloudflare Tunnel when external infrastructure is unavailable.
