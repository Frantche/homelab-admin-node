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

## Release-candidate promotion

Adding the `release-candidate` label to a pull request runs both DR variants for
that pull request's exact 40-character head SHA. A scheduled weekly run remains
an environment-health check for the then-current `main`; it is never evidence
for a later commit. Ordinary pull requests and pushes intentionally skip the
expensive DR matrix.

Each successful variant retains a small 90-day JSON evidence artifact containing
the tested commit, variant, backup manifest, and restore validation summary.
Large diagnostic logs retain their shorter lifecycle.

To promote, manually dispatch `bootstrap-user-journey`, choose
the default branch as the workflow ref, select `disaster-recovery`, provide the
full candidate SHA, and enable `promote`. Promotion is refused from any other
workflow ref.
The promotion job runs only after both `standard` and `offline-images` succeed,
then revalidates that both current-run evidence files and embedded manifests
match the candidate. Only then can it create the immutable
`homelab-production-<12-character-SHA>` tag. Missing, stale, skipped, or
mismatched evidence fails closed and creates no marker.

The write-enabled promotion job always executes tooling from the default-branch
revision that defined the manually dispatched workflow. Candidate-controlled
scripts are never executed with `contents: write`; the candidate is identified
only as an immutable Git object after its evidence has been validated.

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
