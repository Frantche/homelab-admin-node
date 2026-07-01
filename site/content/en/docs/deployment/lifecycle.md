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

For the current VM, the inventory should be:

```text
/etc/admin-config/homelab-node-admin-config/di/inventory.ini
```

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
sudo /opt/homelab-admin-node/bin/admin-node converge run
```

Init mode deploys the first service state needed to bootstrap the node.

Initialize OpenBao if needed:

```bash
sudo /opt/homelab-admin-node/bin/admin-node openbao init-if-needed
```

When OpenBao generates or updates encrypted material, update the encrypted config repo before moving to normal mode:

```bash
cd /etc/admin-config/homelab-node-admin-config
sudo env SOPS_AGE_KEY_FILE=/etc/sops/age/keys.txt sops di/group_vars/secrets.sops.yaml
sudo git add di/group_vars/secrets.sops.yaml
sudo git commit -m "update OpenBao bootstrap token"
sudo git push
```

The committed file must stay encrypted. Do not commit raw OpenBao tokens, unseal material, or decrypted temporary files.

## Normal mode

Switch to steady-state operation:

```bash
sudo /opt/homelab-admin-node/bin/admin-node mode set normal
sudo /opt/homelab-admin-node/bin/admin-node converge run
```

Normal mode deploys and validates the operational service set, then runs backup tasks according to configuration.

If the code repo is already aligned and root cannot fetch it over SSH, use `--skip-git-pull` for the local convergence run:

```bash
sudo /opt/homelab-admin-node/bin/admin-node converge run --skip-git-pull
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
