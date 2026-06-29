---
title: OIDC Validation
weight: 45
---

OIDC validation checks that Keycloak contracts and downstream service integrations stay coherent.

The important relationships are:

- Keycloak owns the realm, users, groups, roles, and clients.
- `oidc_clients` defines shared client IDs and secrets once.
- Harbor, OpenBao, and Gitea consume those client definitions through their own configuration blocks.
- CI can run deterministic mock contracts for missing-secret and success scenarios.

Run OIDC contract checks:

```bash
make test-oidc-contracts
```

The related CI playbooks live under `ci/playbooks/`:

```text
oidc-contracts-success.yml
oidc-contracts-ci.yml
oidc-contracts-missing-secret.yml
oidc-contracts-gitea-missing-secret.yml
```

Use these checks when changing:

- `keycloak_config`
- `oidc_clients`
- `harbor_config.oidc`
- `openbao_config.oidc`
- `gitea_config.oidc`

For browser-level validation, the user journey tests live under `ci/oidc-user-journey/`.
