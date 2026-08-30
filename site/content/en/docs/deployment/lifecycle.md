---
title: First Convergence
weight: 50
---

After cloud-init, secret zero, and the config repo are ready, use the lifecycle modes to bring the node up safely.

Before switching modes, confirm that the config repo is present and that the convergence service uses the intended environment:

```bash
sudo git -C /etc/admin-config/homelab-node-admin-config status --short --branch
systemctl cat admin-converge.service
```

For an integration VM, the inventory should be the following path; use the corresponding `pr/inventory.ini` path for production:

```text
/etc/admin-config/homelab-node-admin-config/di/inventory.ini
```

The inventory configured in the systemd drop-in is available only to the service. The commands below therefore start `admin-converge.service`; invoking the CLI directly with `sudo` would require passing `INVENTORY_PATH` explicitly.

## Locked mode

The node should start in `locked` mode:

```bash
cat /etc/admin-node/mode
```

This mode keeps the system safe before production secrets are present.

## Init mode

Switch to `init` mode:

```bash
sudo /opt/homelab-admin-node/bin/admin-node mode set init
sudo systemctl start admin-converge.service
```

Init mode deploys the first service state needed to bootstrap the node.

The preceding convergence automatically initializes OpenBao when needed. The
following idempotent command is optional and is useful to verify or retry the
operation explicitly:

```bash
sudo /opt/homelab-admin-node/bin/admin-node openbao init-if-needed
```

When OpenBao generates or updates encrypted material, update the selected environment in the encrypted config repo before moving to normal mode. This production example uses `pr`:

Retrieve the generated `openbao.root_token` and store it under the same key in
`pr/group_vars/secrets.sops.yaml`. The exact source path, extraction command,
and field precedence are documented in
[Secret Zero]({{< relref "/docs/deployment/secret-zero#secret-lifecycle" >}}).

```bash
cd /etc/admin-config/homelab-node-admin-config
sudo env SOPS_AGE_KEY_FILE=/etc/sops/age/keys.txt sops pr/group_vars/secrets.sops.yaml
sudo git add pr/group_vars/secrets.sops.yaml
sudo git commit -m "update OpenBao bootstrap token"
sudo git push
```

The committed file must stay encrypted. Do not commit raw OpenBao tokens, unseal material, or decrypted temporary files.

## Normal mode

Switch to steady-state operation:

```bash
sudo /opt/homelab-admin-node/bin/admin-node mode set normal
sudo systemctl start admin-converge.service
```

Normal mode deploys and validates the operational service set, then runs backup tasks according to configuration.

If the code repo is already aligned and root cannot fetch it over SSH, use `--skip-git-pull` for the local convergence run:

```bash
sudo env INVENTORY_PATH=/etc/admin-config/homelab-node-admin-config/pr/inventory.ini \
  /opt/homelab-admin-node/bin/admin-node converge run --skip-git-pull
```

## Validate

```bash
sudo /opt/homelab-admin-node/bin/admin-node validate all
```

If a specific subsystem fails, run the narrower validator:

```bash
sudo /opt/homelab-admin-node/bin/admin-node validate apis
sudo /opt/homelab-admin-node/bin/admin-node validate dns
sudo /opt/homelab-admin-node/bin/admin-node validate tunnel
sudo /opt/homelab-admin-node/bin/admin-node validate hardening
sudo /opt/homelab-admin-node/bin/admin-node validate observability
```

The API validators use service domain environment variables when they are set. Keep the systemd drop-in and manual shell environment aligned for Harbor, OpenBao, Keycloak, Gitea, Traefik, and the node LAN IP.
