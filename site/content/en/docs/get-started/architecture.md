---
title: Architecture
weight: 20
---

The runtime flow is:

```text
LAN clients -> Pi-hole -> admin-01 -> Traefik -> Keycloak/OpenBao/Harbor/Gitea
Internet    -> Cloudflare -> cloudflared -> Traefik -> same services
```

The automation flow is:

```text
Proxmox VM
  -> cloud-init
  -> /opt/homelab-admin-node
  -> admin-converge systemd timer/service
  -> bin/admin-node converge run
  -> ansible/site.yml
  -> Docker Compose stacks and host configuration
```

The main moving parts are:

- **cloud-init** installs the base packages, clones this repository, and installs systemd units.
- **systemd** runs convergence on boot and on a timer.
- **`bin/admin-node`** wraps common operations: mode changes, convergence, validation, backup, restore, secret installation, OpenBao initialization, and CI helpers.
- **Ansible** applies host state and service configuration according to the current lifecycle mode.
- **Docker Compose** runs the service stacks under `stacks/`.
- **The private config repo** supplies inventory, non-secret variables, and encrypted secrets.

Lifecycle modes keep bootstrap safe:

| Mode | Purpose |
| --- | --- |
| `locked` | Default safe mode. Secrets are absent and service stacks are not fully deployed. |
| `init` | Initial deployment and bootstrap work, including service setup and OpenBao initialization path. |
| `normal` | Steady-state operation, validation, and backup. |
| `restore` | Restore files and services from backup. |
| `restore_failed` | Explicit failed restore state for investigation and retry. |
