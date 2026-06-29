---
title: admin-node CLI
weight: 10
---

Build the CLI:

```bash
make build-admin-node
```

Root usage:

```text
admin-node <command> [options]
```

Commands:

| Command | Purpose |
| --- | --- |
| `validate` | Validate services and host state. |
| `backup` | Run and inspect backups. |
| `restore` | Restore backups. |
| `mode` | Set lifecycle mode. |
| `converge` | Run Ansible convergence. |
| `secret` | Install local secret material. |
| `openbao` | Initialize and unseal OpenBao. |
| `ci` | Run CI helper operations. |

Common examples:

```bash
sudo ./bin/admin-node mode set normal
sudo ./bin/admin-node converge run
sudo ./bin/admin-node validate all
sudo ./bin/admin-node backup run
sudo ./bin/admin-node restore run
```

Validation supports text and JSON:

```bash
sudo ./bin/admin-node validate apis --output json
```
