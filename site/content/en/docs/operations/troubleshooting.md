---
title: Troubleshooting
weight: 50
---

## cloud-init

```bash
cloud-init status --wait
sudo tail -n 200 /var/log/cloud-init-output.log
```

## systemd

```bash
systemctl status admin-converge.timer
systemctl status admin-converge.service
journalctl -u admin-converge.service -n 200 --no-pager
```

## Convergence

```bash
sudo /opt/homelab-admin-node/bin/admin-node converge run --skip-git-pull
```

Check:

- The mode file at `/etc/admin-node/mode`.
- The config repo path under `/etc/admin-config/homelab-node-admin-config`.
- The selected inventory path, usually `INVENTORY_PATH=/etc/admin-config/homelab-node-admin-config/di/inventory.ini`.
- The age key at `/etc/sops/age/keys.txt`.
- SOPS decryption of `di/group_vars/secrets.sops.yaml`.

If root cannot fetch the code or config repository over SSH, install a deploy key for root, configure root SSH to use the intended key, or run the pull with an explicit `GIT_SSH_COMMAND`. If the code repository is already at the intended commit, use `--skip-git-pull` for convergence.

## Secrets and persistent data

Database passwords and application secrets are stored in persistent volumes under `/srv/admin`. If a secret is changed after data already exists, the service may keep the old database credential inside its data directory. Reset application data only when an intentional rebuild is acceptable.

OpenBao root tokens are generated during initialization. After `openbao init-if-needed`, update the encrypted environment secrets and commit only the SOPS file. Do not commit decrypted unseal material or raw root tokens.

## TLS names

When adding or renaming service domains, verify that the local certificate contains every hostname used by validation. If local TLS certificates were generated before the domain was added, regenerate them and restart Traefik.

## External integrations

`ci_mode: true` enables mock behavior for integrations such as Pi-hole and Cloudflare Tunnel. It is useful for bootstrap when external providers are not ready, but production validation requires real provider credentials, correct Pi-hole API settings, and a valid Cloudflare tunnel token.

## Services

```bash
docker ps
docker compose -f /opt/homelab-admin-node/stacks/traefik/compose.yaml ps
sudo /opt/homelab-admin-node/bin/admin-node validate all
```

Use narrower validation commands to isolate DNS, tunnel, hardening, observability, or API failures.
