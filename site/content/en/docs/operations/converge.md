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

The default inventory path is:

```text
/etc/admin-config/homelab-node-admin-config/hosts/inventory.ini
```
