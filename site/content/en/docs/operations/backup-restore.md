---
title: Backup And Restore
weight: 30
---

## Backup

Run a backup:

```bash
sudo /opt/homelab-admin-node/bin/admin-node backup run
```

The backup flow validates service health, prepares local backup data, and
applies restic retention to configured repositories. A successful command means
that every artifact required by the active stateful stacks was produced. It
fails instead of silently omitting an active Gitea or Harbor database, Harbor
registry data, or the OpenBao snapshot.

The backup manifest contains an `artifacts` inventory. Required entries must be
`produced`; optional offline-image and repository-bundle entries are explicitly
`disabled` during a standard backup. `admin-node backup verify` checks this
inventory in addition to file hashes.

Failed runs retain a non-restorable artifact inventory under
`/srv/admin/backups/local/.failed/<backup-id>/manifest.json`. This record shows
which required artifacts were produced or failed without publishing a partial
recovery point. Required remote delivery is tagged uniquely, queried back from
each Restic repository, and recorded locally only after the snapshot is visible.

When `backup.gitea_process.enabled` is true, a separate
`admin-gitea-process-backup.timer` also runs daily at 03:30 by default using
`Frantche/gitea-backup-restore-process`. The schedule is configurable through
`backup.gitea_process.on_calendar`. It runs only when both `gitea-db` and `gitea`
report `healthy`; otherwise the service fails, emits a systemd failure event,
and does not refresh its success marker. The helper joins the
isolated `gitea-db` Docker network to resolve PostgreSQL and `admin-edge` for
DNS and remote backup egress. Gitea data stays read-only during backup; helper
history is stored under `/srv/admin/backups/gitea-process`. S3 uploads use
multipart mode by default so large archives can cross a size-limited proxy.

PostgreSQL databases are exported with `pg_dump -Fc`:

- `keycloak.dump` for Keycloak.
- `gitea.dump` for an active Gitea stack; a missing `gitea-db` is a backup
  failure.
- `harbor.dump` for an active Harbor stack; a missing `harbor-db` or registry
  data is a backup failure.

An active OpenBao stack must also produce `openbao.snap`. Install the dedicated
snapshot token through convergence before relying on scheduled backups.

Remote delivery is required by default. Set
`backup.require_remote_repository: false` explicitly only for a deployment that
intentionally accepts local-only backups. When remote delivery is required, a
missing Restic binary, missing non-local repository, incomplete credentials, or
repository failure makes the backup command fail while preserving any complete
local recovery point already created.

### Consistency contract

The standard backup briefly stops the Gitea application container while its
PostgreSQL dump and filesystem capture are created. The database remains
running. This prevents repository, attachment, LFS, issue, and metadata writes
from crossing the recovery boundary. The previous running state is restored
after the capture and on every failure path. Capture time is limited by
`backup.gitea_quiesce_timeout` (10 minutes by default), followed by a bounded
two-minute health wait after restart. Exceeding either limit fails the backup
instead of publishing an uncertain recovery point.

Each successful Gitea boundary is recorded in `manifest.json` with its method,
start, and completion timestamps. A Btrfs source records a quiesced logical
dump plus Btrfs snapshot; the portable fallback records a quiesced logical dump
plus bounded filesystem copy. Neither pair is described as globally atomic.
This contract is emitted as manifest version 3. Restore and listing remain
compatible with historical version 2 recovery points, while every version 3
manifest with an active Gitea stack is rejected unless it contains exactly one
recognized Gitea consistency boundary.

The other stateful stacks use these contracts:

- Keycloak has database-only durable state and uses its PostgreSQL logical dump.
- OpenBao uses its native Raft snapshot rather than pairing a database with files.
- Harbor enters application read-only mode around its PostgreSQL dump and file
  capture when the default `backup.require_harbor_read_only` policy is enabled.

Harbor registry blobs and other file data remain under `/srv/admin/data/harbor`; the default restic path set includes `/srv/admin/data`.

Useful checks:

```bash
make test-restic-config
sudo /opt/homelab-admin-node/bin/admin-node validate apis
sudo /opt/homelab-admin-node/bin/admin-node backup status
```

`backup status` reports the last standard, remote, Gitea-process,
offline-image, and repository-integrity successes. It exits non-zero when a
required class is missing or older than its configured threshold, so it can be
used directly by monitoring. The command reads only secret-free marker files.

Run an integrity check immediately when needed:

```bash
sudo /opt/homelab-admin-node/bin/admin-node backup restic-check
```

The scheduled `admin-backup-integrity.timer` runs the same check weekly. Inspect
failures with:

```bash
journalctl -t admin-node-backup --since today
systemctl status admin-backup-status.service admin-backup-integrity.service
```

## Restore

Restore mode is explicit to avoid accidental destructive operations.

Typical recovery flow:

1. Rebuild or boot `admin-01`.
2. Install the age private key.
3. Restore or clone the private config repo.
4. Set restore mode:

   ```bash
   sudo /opt/homelab-admin-node/bin/admin-node mode set restore
   ```

5. Run restore:

   ```bash
   sudo /opt/homelab-admin-node/bin/admin-node restore run
   ```

6. Validate services.
7. Switch back to normal mode:

   ```bash
   sudo /opt/homelab-admin-node/bin/admin-node mode set normal
   sudo /opt/homelab-admin-node/bin/admin-node converge run
   ```

If restore fails, the node can remain in `restore_failed` while logs and restored files are inspected.

Database restore uses `pg_restore` against the custom-format dumps and recreates the target database before import. Legacy flat SQL dumps are not supported by this restore flow.

## Gitea Backup-Restore-Process Restore

Use this flow only for backups produced by `backup.gitea_process`. The external
project provides `gitea-restore`, which uses the same backend environment
variables as `gitea-backup`.

The restore replaces the current Gitea data and database. Use the guarded CLI
workflow rather than invoking Docker directly:

```bash
sudo /opt/homelab-admin-node/bin/admin-node gitea restore-process \
  --backup-filename "gitea-backup-YYYY-MM-DD-HH-MM-SS.zip"
```

`--backup-filename` must match the exact remote `.zip` filename in the S3 bucket
or FTP directory. It is only needed for restore, not for the daily backup job.

The command:

- acquires the global administration-operation lock;
- enters `restore` mode and stops convergence, standard-backup, and Gitea-backup
  timers;
- stops Gitea writes and saves the current database plus filesystem under
  `/srv/admin/backups/pre-gitea-process-restore`;
- restores the selected remote archive and its PostgreSQL dump;
- validates the database, users, repository storage, container health, and
  internal Gitea API;
- optionally converges the node, returns to `normal`, and resumes timers only
  after success.

If the command fails, it leaves the node in `restore_failed` and keeps the
timers stopped. Inspect the error and the safety copy before retrying. The
rollback material is:

```text
/srv/admin/backups/pre-gitea-process-restore/gitea-data/
/srv/admin/backups/pre-gitea-process-restore/gitea.dump
```

The command refuses to overwrite either safety artifact. Before another restore,
archive the whole directory and select a new empty location with
`--pre-restore-dir`; never delete the only known-good rollback copy.

Do not return the node to `normal` until Gitea has been repaired or the saved
filesystem and custom-format PostgreSQL dump have been restored and validated.
After a successful manual recovery, run the full validation before re-enabling
normal operation:

```bash
sudo /opt/homelab-admin-node/bin/admin-node validate all
sudo /opt/homelab-admin-node/bin/admin-node mode set normal
sudo /opt/homelab-admin-node/bin/admin-node converge run --skip-git-pull
```

If `backup.gitea_process.image`, `network`, or `restore_tmp_folder` were
customized, reuse the corresponding values from
`/srv/admin/env/gitea-process-backup.env`.
