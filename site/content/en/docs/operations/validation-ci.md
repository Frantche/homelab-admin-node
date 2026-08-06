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
sudo ./bin/admin-node test harbor-scanner
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
make ci-quality
make ci-continuous
make ci-bootstrap
make ci-full
```

`ci-quality` is the deterministic host-side validation set used by the quality
job. `ci-continuous` adds the OIDC contracts. `ci-bootstrap` runs inside a
prepared Arch VM. The GitHub Actions `bootstrap` job remains the end-to-end
entry point: it downloads a fresh Arch cloud image, injects the candidate SHA
through cloud-init, starts a QEMU VM, verifies `locked` mode, and only then runs
`ci-bootstrap` inside that VM. `ci-full` runs continuous validation followed by both the
`standard` and `offline-images` recovery variants; the recovery journey embeds
the bootstrap path before upgrading the candidate. Set `DR_VARIANTS=standard`
to run only the standard recovery path.

The Grafana import test and the real Trivy image scan remain specialized jobs.
They are exposed through Make targets but are not part of `ci-full`.

CI can use mock Pi-hole and Cloudflare Tunnel services when real external infrastructure is not available.
