---
title: Cloud-init
weight: 20
---

The repository provides cloud-init inputs under `cloud-init/`:

| File | Purpose |
| --- | --- |
| `admin-01.user-data.yaml` | User, packages, repository clone, scripts, and systemd bootstrap. |
| `admin-01.network-data.example.yaml` | Example network-data file for static networking. |

cloud-init is responsible for the first irreversible bootstrap step:

1. Create the `admin` user.
2. Install base packages such as Git, Ansible, Docker, SOPS, age, restic, jq, and curl.
3. Clone this repository into `/opt/homelab-admin-node`.
4. Install systemd units for convergence and stack operation.
5. Leave the node in `locked` mode until secrets and config are provided.

The user-data file must not contain production secrets. The secret zero is installed later with `admin-node secret install-age-key`.
