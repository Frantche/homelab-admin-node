---
title: Convergence
weight: 20
---

`admin-node converge run` is the main operation loop.

It:

1. Updates `/opt/homelab-admin-node` with `git pull --ff-only` unless disabled.
2. Reads the inventory from the private config repo.
3. Runs `ansible-playbook` against `ansible/site.yml`.
4. Builds `bin/admin-node` when Go sources changed.
5. Applies roles according to `/etc/admin-node/mode`.

The project requires Go 1.27. Convergence explicitly enables Go's managed
toolchain selection, so an existing node whose Arch `go` package is still on
Go 1.26 downloads and verifies Go 1.27 automatically before rebuilding the
CLI. The downloaded toolchain is retained in the root caches under
`/var/cache/admin-node`; convergence does not perform a partial Arch package
upgrade.

The first convergence after a Go version change therefore requires access to
the configured `GOPROXY` and `GOSUMDB`. If the download or verification fails,
the convergence fails and the existing CLI remains in place because the build
installs its replacement atomically.

For a manual run, pass the selected inventory explicitly. This production example uses `pr`:

```bash
sudo env INVENTORY_PATH=/etc/admin-config/homelab-node-admin-config/pr/inventory.ini \
  /opt/homelab-admin-node/bin/admin-node converge run
```

Skip the repository pull when testing local changes:

```bash
sudo env INVENTORY_PATH=/etc/admin-config/homelab-node-admin-config/pr/inventory.ini \
  /opt/homelab-admin-node/bin/admin-node converge run --skip-git-pull
```

Pass extra Ansible arguments:

```bash
sudo env INVENTORY_PATH=/etc/admin-config/homelab-node-admin-config/pr/inventory.ini \
  /opt/homelab-admin-node/bin/admin-node converge run --extra-vars "--check"
```

The built-in legacy inventory path is:

```text
/etc/admin-config/homelab-node-admin-config/hosts/inventory.ini
```

For real deployments with the split `di` and `pr` config repo, set `INVENTORY_PATH` explicitly instead of relying on the legacy path. The current VM uses:

```text
/etc/admin-config/homelab-node-admin-config/di/inventory.ini
```

For boot and timer runs, keep that setting in a systemd drop-in:

```ini
[Service]
Environment=INVENTORY_PATH=/etc/admin-config/homelab-node-admin-config/di/inventory.ini
```

That drop-in is applied only when systemd launches the service. Start a convergence with the configured service environment using:

```bash
sudo systemctl start admin-converge.service
```

A direct `sudo ... admin-node converge run` invocation does not inherit the service environment and must pass `INVENTORY_PATH`, as shown above.

The explicit CI/production execution mode, service domains, the admin-node LAN
address, runtime paths, feature flags, and backup policy are loaded from the managed
`/srv/admin/env/backup.env`; they do not need to be duplicated in this drop-in.

The convergence timer runs 5 minutes after boot, then 30 minutes after the previous activation by default. Override those intervals in inventory with:

```yaml
admin_converge_timer_on_boot_sec: 5m
admin_converge_timer_on_unit_active_sec: 30m
```

The `/opt/homelab-admin-node` checkout is kept writable by the `homelab` group so manual Git operations do not leave root-only metadata. The group members are the existing local users listed in `hardening.ssh.allow_users`:

```yaml
admin_node_repo_owner: root
admin_node_repo_group: homelab
hardening:
  ssh:
    allow_users:
      - admin
```

When the code repository is already on the intended commit, or root cannot fetch the code repository, use:

```bash
sudo env INVENTORY_PATH=/etc/admin-config/homelab-node-admin-config/pr/inventory.ini \
  /opt/homelab-admin-node/bin/admin-node converge run --skip-git-pull
```
