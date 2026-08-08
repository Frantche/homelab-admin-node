---
title: Backup And Restore
weight: 30
---

## Backup

Run a backup:

```bash
sudo /opt/homelab-admin-node/bin/admin-node backup run
```

The backup flow validates service health, prepares local backup data, and applies restic retention to configured repositories.

When `backup.gitea_process.enabled` is true, a separate
`admin-gitea-process-backup.timer` also runs daily at 03:30 by default using
`Frantche/gitea-backup-restore-process`. The schedule is configurable through
`backup.gitea_process.on_calendar`. It runs only when both `gitea-db` and `gitea`
report `healthy`; otherwise that execution is skipped. The helper joins the
isolated `gitea-db` Docker network to resolve PostgreSQL and `admin-edge` for
DNS and remote backup egress. Gitea data stays read-only during backup; helper
history is stored under `/srv/admin/backups/gitea-process`. S3 uploads use
multipart mode by default so large archives can cross a size-limited proxy.

PostgreSQL databases are exported with `pg_dump -Fc`:

- `keycloak.dump` for Keycloak.
- `gitea.dump` for Gitea when `gitea-db` is running.
- `harbor.dump` for Harbor when `harbor-db` is running.

Harbor registry blobs and other file data remain under `/srv/admin/data/harbor`; the default restic path set includes `/srv/admin/data`.

Useful checks:

```bash
make test-restic-config
sudo /opt/homelab-admin-node/bin/admin-node validate apis
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
