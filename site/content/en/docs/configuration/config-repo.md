---
title: Config Repo And Git Workflow
weight: 10
---

Use Git as the source of truth for deployment parameters.

Recommended workflow:

1. Edit `group_vars/all.yml` for non-secret configuration.
2. Edit `group_vars/secrets.sops.yaml` with SOPS for secrets.
3. Commit and push the private config repo.
4. Pull the private config repo on `admin-01`.
5. Run convergence.

```bash
git -C /etc/admin-config/homelab-node-admin-config pull --ff-only
sudo /opt/homelab-admin-node/bin/admin-node converge run
```

Use normal commits for settings changes:

```bash
git add group_vars/all.yml
git commit -m "update admin-node domains"
git push
```

Use SOPS for secrets:

```bash
sops group_vars/secrets.sops.yaml
git add group_vars/secrets.sops.yaml
git commit -m "update admin-node secrets"
git push
```

Never commit unencrypted secret files or the age private key.
