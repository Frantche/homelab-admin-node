---
title: Config Repo Setup
weight: 40
---

The config repo is a private Git repository containing deployment-specific values.

Recommended structure:

```text
homelab-node-admin-config/
├── .gitignore
├── .sops.yaml
├── README.md
├── di/
│   ├── inventory.ini
│   └── group_vars/
│       ├── all.yml
│       └── secrets.sops.yaml
└── pr/
    ├── inventory.ini
    └── group_vars/
        ├── all.yml
        └── secrets.sops.yaml
```

Create it:

```bash
mkdir homelab-node-admin-config
cd homelab-node-admin-config
git init
git remote add origin git@github.com:<owner>/homelab-node-admin-config.git
mkdir -p di/group_vars pr/group_vars
```

For this deployment the remote is:

```text
git@github.com:Frantche/homelab-node-admin-config.git
```

Create `di/inventory.ini` and `pr/inventory.ini`:

```ini
[admin]
localhost ansible_connection=local
```

Create `.gitignore`:

```gitignore
di/group_vars/secrets.yml
di/group_vars/secrets.yaml
pr/group_vars/secrets.yml
pr/group_vars/secrets.yaml
*.age
*.key
```

Create `.sops.yaml` with the public age key, then create encrypted secrets:

```yaml
creation_rules:
  - path_regex: di/group_vars/secrets\.sops\.yaml$
    age: ["age1..."]
  - path_regex: pr/group_vars/secrets\.sops\.yaml$
    age: ["age1..."]
```

For the first iteration, `di` and `pr` can use the same admin/NAS age recipient. Separate environment-specific age keys can be added later.

```bash
sops di/group_vars/secrets.sops.yaml
sops pr/group_vars/secrets.sops.yaml
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

The active environment is selected by `INVENTORY_PATH`. For the current VM, use `di`:

```text
INVENTORY_PATH=/etc/admin-config/homelab-node-admin-config/di/inventory.ini
```

The `di` inventory loads variables from:

```text
/etc/admin-config/homelab-node-admin-config/di/group_vars/all.yml
/etc/admin-config/homelab-node-admin-config/di/group_vars/secrets.sops.yaml
```

Configure the systemd convergence service to keep the same inventory on boot and timer runs:

```bash
sudo systemctl edit admin-converge.service
```

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

Reload systemd after changing the drop-in:

```bash
sudo systemctl daemon-reload
```
