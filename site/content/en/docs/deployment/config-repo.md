---
title: Config Repo Setup
weight: 40
---

The config repo is a private Git repository containing deployment-specific values.

Recommended structure:

```text
homelab-admin-node-config/
├── .gitignore
├── .sops.yaml
├── README.md
├── hosts/
│   └── inventory.ini
└── group_vars/
    ├── all.yml
    └── secrets.sops.yaml
```

Create it:

```bash
mkdir homelab-admin-node-config
cd homelab-admin-node-config
git init
git remote add origin git@github.com:<owner>/homelab-admin-node-config.git
mkdir -p hosts group_vars
```

Create `hosts/inventory.ini`:

```ini
[admin]
localhost ansible_connection=local
```

Create `.gitignore`:

```gitignore
group_vars/secrets.yml
group_vars/secrets.yaml
*.age
*.key
```

Create `.sops.yaml` with the public age key, then create encrypted secrets:

```bash
sops group_vars/secrets.sops.yaml
```

Commit only encrypted secrets:

```bash
git add .
git commit -m "initial admin-node config"
git push -u origin main
```

On the node, place or clone the repo at:

```text
/etc/admin-config/homelab-node-admin-config
```

`admin-node converge run` reads the inventory from:

```text
/etc/admin-config/homelab-node-admin-config/hosts/inventory.ini
```

and loads variables from:

```text
/etc/admin-config/homelab-node-admin-config/group_vars/all.yml
/etc/admin-config/homelab-node-admin-config/group_vars/secrets.sops.yaml
```
