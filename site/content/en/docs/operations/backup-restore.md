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
report `healthy`; otherwise that execution is skipped.

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

4. Run `gitea-restore` with the Ansible-rendered backend environment.

   ```bash
   sudo docker run --rm \
     --network admin-net \
     --env-file /srv/admin/env/gitea-process-backup.env \
     -v /srv/admin/data/gitea:/data \
     -v /srv/admin/backups/gitea-process/restore-tmp:/srv/admin/backups/gitea-process/restore-tmp \
     harbor.frantchenco.page/library/gitea-backup:latest \
     gitea-restore
   ```

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
