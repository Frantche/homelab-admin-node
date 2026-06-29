---
title: Validation And CI
weight: 40
---

Validation commands:

```bash
make validate
make validate-apis
make validate-dns
make validate-cloudflare-tunnel
make validate-hardening
make validate-observability
```

Equivalent CLI checks:

```bash
sudo ./bin/admin-node validate all
sudo ./bin/admin-node validate apis
sudo ./bin/admin-node validate harbor
sudo ./bin/admin-node validate openbao
sudo ./bin/admin-node validate gitea
sudo ./bin/admin-node validate dns
sudo ./bin/admin-node validate tunnel
sudo ./bin/admin-node validate hardening
sudo ./bin/admin-node validate observability
```

CI lifecycle scenarios live under `ci/scenarios/`:

| Scenario | Purpose |
| --- | --- |
| `fresh-branch` | Fresh deployment from the current branch. |
| `upgrade-main-to-branch` | Upgrade an existing main deployment to the branch. |
| `restore-main-backup-with-branch` | Restore a main backup using branch code. |
| `bootstrap-user-journey` | End-to-end user journey around bootstrap and validation. |

Run locally:

```bash
make test-ci-fast
make test-ci-full
```

CI can use mock Pi-hole and Cloudflare Tunnel services when real external infrastructure is not available.
