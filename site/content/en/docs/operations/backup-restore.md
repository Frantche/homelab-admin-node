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
