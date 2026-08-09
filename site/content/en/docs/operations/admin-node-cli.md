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

The CLI automatically loads deployed non-secret runtime settings from
`/srv/admin/env/backup.env`. Precedence is explicit process environment, then
the managed file, then built-in defaults. The parser uses a fixed allowlist and
does not execute the file or retain unrelated secret entries. You do not need
to `source` the file before manual commands.

Operational commands such as `validate all`, `backup run`, and `restore run`
refuse to continue when service domains still resolve to built-in
`example.com` values. Run convergence first or provide explicit process
environment overrides. A missing managed file does not block lifecycle commands
such as `mode` and `converge`, which are needed during bootstrap.

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
| `version` | Verify and show the selected release, immutable pin, installed revision, config schema, package snapshot, checkout state and tag binding. |

Common examples:

```bash
sudo ./bin/admin-node mode set normal
sudo ./bin/admin-node converge run
sudo ./bin/admin-node validate all
sudo ./bin/admin-node backup run
sudo ./bin/admin-node restore run
sudo ./bin/admin-node version --json
```

Validation supports text and JSON:

```bash
sudo ./bin/admin-node validate apis --output json
sudo ./bin/admin-node test harbor-scanner
```
