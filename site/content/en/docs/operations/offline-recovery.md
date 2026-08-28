---
title: Offline Recovery
weight: 35
---

## Purpose

The daily backup does not export container images. The opt-in offline schedule
creates an immutable repository bundle, rendered stack definitions, and an image
archive so recovery can proceed while public registries are unavailable.

The backup is not a recovery-key escrow. Never put the age identity, Restic
passwords, private config-repository credentials, and OpenBao recovery material
inside the same backup or in this public repository.

## External recovery-kit checklist

Keep the following in at least two controlled off-node locations. Prefer
different custodians or access controls for the backup repository and the key
material:

- an off-site copy of `/etc/sops/age/keys.txt`;
- tested access to the private configuration repository, including its remote credentials;
- the Restic repository location, password, and backend credentials;
- the encrypted OpenBao unseal/recovery material and the separate means to decrypt it;
- a record of the last restore exercise and the custodians who verified separation.

The `backup.offline.recovery_kit` inventory contains only boolean attestations
and `last_verified_at`; it must never contain a credential. Enabling the timer
fails convergence until every attestation is true. Revisit it after credential
rotation and at least every 90 days.

## Operate and verify

After configuring capacity, schedule, retention, and the checklist, converge and inspect:

```bash
sudo admin-node converge run
systemctl list-timers admin-offline-backup.timer
sudo systemctl start admin-offline-backup.service
sudo admin-node backup offline-status
```

The status command reports the newest offline point, age, freshness, manifest
structure and size verification, and recovery-kit state. It exits non-zero with
actionable prerequisite names when anything is missing or stale.
The Restic prerequisite must be a complete, syntactically valid non-local
repository declaration; a local path or `file:` repository does not satisfy
off-node recovery readiness. Offline scheduling also requires
`backup.require_remote_repository: true`, and a point is not healthy until its
manifest records a successful remote delivery.
The scheduled service also requires the Ansible-managed
`/srv/admin/env/backup.env`; a missing policy file is a hard failure rather than
an invitation to run with permissive defaults.

Capacity must include the live data snapshot plus exported images and temporary
headroom. The default 30 GiB floor is only a starting point; measure the largest
offline point, retain at least two, and keep additional filesystem headroom.

The CI offline restore removes the source image and starts the restored Compose
project with `--pull never`. This proves the archived image is sufficient and no
external registry fallback is possible.
