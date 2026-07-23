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

CI is organized around operator journeys:

| Journey | Purpose |
| --- | --- |
| `bootstrap-user-journey` | Bootstrap the candidate SHA from a fresh Arch image and validate the browser OIDC path. |
| `main-to-candidate-disaster-recovery` | Deploy main, upgrade to the candidate, back up to Garage, destroy the source disk, restore on a fresh candidate VM, and rotate technical secrets. |

The recovery journey preserves every password under `keycloak_config.users`.
Only client, administrator, and database credentials are rotated.

Run locally:

```bash
make test-ci-fast
make test-ci-full
```

CI can use mock Pi-hole and Cloudflare Tunnel services when real external infrastructure is not available.
