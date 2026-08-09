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

The checked-in user-data intentionally contains `RELEASE_REF_REPLACE_ME`,
`ARCH_PACKAGE_SNAPSHOT_REPLACE_ME`, `QUALIFICATION_MANIFEST_URL_REPLACE_ME`,
and `QUALIFICATION_MANIFEST_SHA256_REPLACE_ME` and refuses a production
bootstrap until all are replaced. For production, use a published immutable tag
such as `v1.2.0`; direct commit installation is reserved for the CI renderer.
Use the `qualification.json` asset URL from that public release and its SHA-256
from the release notes. Replace the package snapshot with the qualified Arch Linux
Archive date in `YYYY/MM/DD` form. Also pin the dated Arch image URL and verify
its checksum as recorded in the release qualification manifest; do not use the
moving `latest` image or live package mirrors for a production rebuild.
The selected date must equal `release/arch-package-snapshot` in the selected
commit. After package installation, cloud-init records the date in
`/etc/admin-node/package-snapshot` and records `qualified` in
`/etc/admin-node/package-snapshot-mode`.

The installer also queries the GitHub release API. It refuses drafts,
prereleases, manually authored releases, assets not uploaded by
`github-actions[bot]`, and an API-reported asset digest different from the
operator-provided SHA-256. A pushed tag without a completed promotion is
therefore not installable.

The installer resolves a tag once, checks out the commit detached, and records:

- the requested tag in `/etc/admin-node/release-name`;
- the immutable commit pin in `/etc/admin-node/release-ref`;
- `production` in `/etc/admin-node/release-channel`;
- the checksum-verified manifest in `/etc/admin-node/qualification.json`.

The installed commit and configuration schema are deliberately absent until a
complete convergence succeeds. `admin-node version` therefore returns an error
between release selection and the first successful convergence.

Replacing the placeholder with `main` is supported only as the development
channel. In that mode periodic convergence continues to fast-forward the branch.
The CI renderer has a separate `ci-live` marker and refuses `live` unless
`CI_ALLOW_LIVE_ARCH_MIRROR=true` is explicitly set. That marker is not a
qualified package state and must never be copied to a production node.
