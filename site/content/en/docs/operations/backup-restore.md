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

The restore is destructive for the current Gitea data and database, so keep it
manual:

1. Enter restore mode and stop the Gitea process backup timer.

   ```bash
   sudo /opt/homelab-admin-node/bin/admin-node mode set restore
   sudo systemctl stop admin-gitea-process-backup.timer
   ```

2. Stop the Gitea application container, but keep `gitea-db` running.

   ```bash
   cd /srv/admin/stacks/gitea
   sudo docker compose --env-file /srv/admin/env/gitea.env -f compose.yaml stop gitea
   ```

3. Keep a local safety copy of the current Gitea data.

   ```bash
   sudo install -d -m 0700 /srv/admin/backups/pre-gitea-process-restore
   sudo rsync -a --delete /srv/admin/data/gitea/ /srv/admin/backups/pre-gitea-process-restore/gitea-data/
   ```

4. Set the remote backup filename to restore, then run `gitea-restore` with the
   Ansible-rendered backend environment.

   ```bash
   export BACKUP_FILENAME="gitea-backup-YYYY-MM-DD-HH-MM-SS.zip"

   sudo docker run --rm \
     --network admin-net \
     --env-file /srv/admin/env/gitea-process-backup.env \
     -e BACKUP_FILENAME="$BACKUP_FILENAME" \
     -v /srv/admin/data/gitea:/data \
     -v /srv/admin/backups/gitea-process/restore-tmp:/srv/admin/backups/gitea-process/restore-tmp \
     ghcr.io/frantche/gitea-backup-restore-process:0.3.21 \
     gitea-restore
   ```

   `BACKUP_FILENAME` must match the exact remote `.zip` filename in the S3
   bucket or FTP directory. It is only needed for manual restore, not for the
   daily backup job.

5. Restart and validate Gitea.

   ```bash
   cd /srv/admin/stacks/gitea
   sudo docker compose --env-file /srv/admin/env/gitea.env -f compose.yaml up -d
   sudo /opt/homelab-admin-node/bin/admin-node validate apis
   ```

6. Return to normal mode after validation.

   ```bash
   sudo /opt/homelab-admin-node/bin/admin-node mode set normal
   sudo /opt/homelab-admin-node/bin/admin-node converge run
   ```

If `backup.gitea_process.image`, `network`, or `restore_tmp_folder` were
customized, reuse the corresponding values from
`/srv/admin/env/gitea-process-backup.env`.
