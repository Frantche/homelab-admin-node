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
2. Fully upgrade the Arch Linux rolling-release system and reboot into the updated kernel.
3. Install base packages such as Git, Ansible, Docker, SOPS, age, restic, jq, and curl.
4. Clone this repository into `/opt/homelab-admin-node`.
5. Install systemd units for convergence and stack operation.
6. Leave the node in `locked` mode until secrets and config are provided.

The user-data file must not contain production secrets. The secret zero is installed later with `admin-node secret install-age-key`.

## Select a release before booting

The checked-in user-data intentionally contains `RELEASE_REF_REPLACE_ME` and
`ARCH_PACKAGE_SNAPSHOT_REPLACE_ME` and refuses to bootstrap until both are
replaced. For production, replace the release ref with a
published immutable tag such as `v1.2.0`, or with the exact 40-character commit
from that release. Replace the package snapshot with the qualified Arch Linux
Archive date in `YYYY/MM/DD` form. Also pin the dated Arch image URL and verify
its checksum as recorded in the release qualification manifest; do not use the
moving `latest` image or live package mirrors for a production rebuild.

The installer resolves a tag once, checks out the commit detached, and records:

- the requested tag in `/etc/admin-node/release-name`;
- the immutable commit pin in `/etc/admin-node/release-ref`.

The installed commit and configuration schema are deliberately absent until a
complete convergence succeeds. `admin-node version` therefore returns an error
between release selection and the first successful convergence.

Replacing the placeholder with `main` is supported only as the development
channel. In that mode periodic convergence continues to fast-forward the branch.
