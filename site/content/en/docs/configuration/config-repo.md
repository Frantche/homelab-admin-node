---
title: Config Repo And Git Workflow
weight: 10
---

Use Git as the source of truth for deployment parameters.

Recommended workflow:

1. Edit `di/group_vars/all.yml` for non-secret DI configuration.
2. Edit `di/group_vars/secrets.sops.yaml` with SOPS for DI secrets.
3. Edit the matching `pr/...` files for PR.
4. Commit and push the private config repo.
5. Pull the private config repo on `admin-01`.
6. Run convergence.

```bash
git -C /etc/admin-config/homelab-node-admin-config pull --ff-only
sudo /opt/homelab-admin-node/bin/admin-node converge run
```

Use normal commits for settings changes:

```bash
git add di/group_vars/all.yml
git commit -m "update admin-node domains"
git push
```

Use SOPS for secrets:

```bash
sops di/group_vars/secrets.sops.yaml
git add di/group_vars/secrets.sops.yaml
git commit -m "update admin-node secrets"
git push
```

Never commit unencrypted secret files or the age private key.

For the current config repository, use:

```text
git@github.com:Frantche/homelab-node-admin-config.git
```

The current VM reads the DI inventory:

```text
/etc/admin-config/homelab-node-admin-config/di/inventory.ini
```
