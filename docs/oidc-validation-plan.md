# OIDC validation plan

This plan tracks the remaining work for OpenBao OIDC and Harbor OIDC validation.

## Goals

- Configure OpenBao OIDC successfully during a normal convergence.
- Configure Harbor OIDC successfully during the same normal convergence.
- Commit only after the normal convergence validates both services.
- Prefer re-running convergence against the existing node-admin runtime state instead of rebuilding every stack.

## Execution plan

1. Preserve this plan in the repository under `docs/`.
2. Inspect the current node-admin runtime state:
   - `/etc/admin-node/mode`
   - `/etc/admin-config/hosts`
   - `/opt/homelab-admin-node/secrets/openbao-root-token`
   - current Docker stack health
3. Ensure the node is in `normal` mode.
4. Run one normal convergence with the current OpenBao root token:
   - inventory: `/etc/admin-config/homelab-node-admin-config/hosts/inventory.ini`
   - playbook: `/opt/homelab-admin-node/ansible/site.yml`
   - extra var: `openbao.root_token`
5. If OpenBao OIDC fails, capture the OpenBao API error response locally and fix the role payload.
6. Re-run normal convergence without resetting the environment.
7. If Harbor OIDC fails, capture the Harbor API error response locally and fix the role payload.
8. Re-run normal convergence until both OpenBao OIDC and Harbor OIDC pass.
9. Run fast validations:
   - `ansible-playbook --syntax-check`
   - `ci/test-oidc-contracts.sh`
10. Commit the role changes only after normal convergence passes.

## Current validation result

The normal convergence has passed with the CI OIDC inventory:

- OpenBao OIDC backend configuration passed.
- OpenBao OIDC role configuration passed.
- Harbor OIDC SSO configuration passed.
- Runtime API validation passed.

## Full validation

After the normal convergence works, run the lifecycle scenarios when practical:

- `sudo /opt/homelab-admin-node/ci/run-admin-lifecycle.sh fresh-branch`
- `sudo /opt/homelab-admin-node/ci/run-admin-lifecycle.sh upgrade-main-to-branch`
- `sudo /opt/homelab-admin-node/ci/run-admin-lifecycle.sh restore-main-backup-with-branch`

Do not use full lifecycle resets for every small OIDC iteration unless the runtime state is inconsistent.
