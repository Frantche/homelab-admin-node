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

Run:

```bash
sudo /opt/homelab-admin-node/bin/admin-node converge run
```

Skip the repository pull when testing local changes:

```bash
sudo /opt/homelab-admin-node/bin/admin-node converge run --skip-git-pull
```

Pass extra Ansible arguments:

```bash
sudo /opt/homelab-admin-node/bin/admin-node converge run --extra-vars "--check"
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
Environment=HARBOR_DOMAIN=harbor.example.com
Environment=OPENBAO_DOMAIN=bao.example.com
Environment=KEYCLOAK_DOMAIN=keycloak.example.com
Environment=GITEA_DOMAIN=git.example.com
Environment=TRAEFIK_DOMAIN=traefik.example.com
Environment=ADMIN_NODE_LAN_IP=192.0.2.10
```

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
sudo /opt/homelab-admin-node/bin/admin-node converge run --skip-git-pull
```
