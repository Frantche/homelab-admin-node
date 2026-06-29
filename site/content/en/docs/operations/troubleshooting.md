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
- The inventory file under `hosts/inventory.ini`.
- The age key at `/etc/sops/age/keys.txt`.
- SOPS decryption of `group_vars/secrets.sops.yaml`.

## Services

```bash
docker ps
docker compose -f /opt/homelab-admin-node/stacks/traefik/compose.yaml ps
sudo /opt/homelab-admin-node/bin/admin-node validate all
```

Use narrower validation commands to isolate DNS, tunnel, hardening, observability, or API failures.
