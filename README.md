# homelab-admin-node

`homelab-admin-node` builds and operates a reproducible homelab administration VM.

It targets an `admin-01` node, usually deployed on Proxmox from an Arch Linux cloud image with cloud-init. The node is then converged with Ansible and Docker Compose.

## What It Runs

- Traefik for HTTPS ingress.
- Keycloak for identity and OIDC.
- OpenBao for secret management.
- Harbor for registry and proxy-cache mirrors.
- Gitea for Git hosting and validation workflows.
- Cloudflare Tunnel for public ingress when enabled.
- Pi-hole DNS integration for local records.
- Restic backup and restore.
- Host hardening, service validation, and lifecycle CI scenarios.

## Why Use It

The project keeps the admin node reproducible and recoverable:

- Public code, roles, templates, and stacks live in this repository.
- Private deployment values live in a separate private config repository.
- Secrets are encrypted with SOPS and age.
- The node starts in `locked` mode until the secret zero and config repo are present.
- `admin-node converge run` applies the desired state consistently.
- A persistent systemd timer applies a full Arch Linux upgrade every week.
- Backup, restore, validation, and disaster recovery are part of the normal workflow.

## Quick Start

1. Create a Proxmox VM from an Arch Linux cloud image.
2. Attach `cloud-init/admin-01.user-data.yaml`.
3. Boot the VM and wait for cloud-init to clone this repository into `/opt/homelab-admin-node`.
4. Create or clone the private config repository under `/etc/admin-config/homelab-node-admin-config`.
   The current layout uses `di/` and `pr/`. Select `di` for integration or `pr` for production. A production VM uses:

   ```text
   INVENTORY_PATH=/etc/admin-config/homelab-node-admin-config/pr/inventory.ini
   ```

5. Build the CLI, which is generated locally and is not stored in Git:

   ```bash
   cd /opt/homelab-admin-node
   sudo make build-admin-node
   ```

6. Install the age private key with:

   ```bash
   sudo /opt/homelab-admin-node/bin/admin-node secret install-age-key ./age-key.txt
   ```

7. Switch to init mode and converge. This convergence automatically runs the
   idempotent OpenBao initialization. The final command below is optional and
   can be used to verify or retry that operation explicitly:

   ```bash
   sudo /opt/homelab-admin-node/bin/admin-node mode set init
   sudo env INVENTORY_PATH=/etc/admin-config/homelab-node-admin-config/pr/inventory.ini \
     /opt/homelab-admin-node/bin/admin-node converge run
   sudo /opt/homelab-admin-node/bin/admin-node openbao init-if-needed
   ```

8. Retrieve `openbao.root_token` from the generated
   `/opt/homelab-admin-node/secrets/openbao-unseal.sops.yaml`, copy it to the
   same key in the selected environment's SOPS file, and commit only that
   encrypted config-repo file. See the
   [secret-zero guide](site/content/en/docs/deployment/secret-zero.md#secret-lifecycle)
   for the exact commands.
9. Switch to normal mode and validate:

   ```bash
   sudo /opt/homelab-admin-node/bin/admin-node mode set normal
   sudo env INVENTORY_PATH=/etc/admin-config/homelab-node-admin-config/pr/inventory.ini \
     /opt/homelab-admin-node/bin/admin-node converge run
   sudo /opt/homelab-admin-node/bin/admin-node validate all
   ```

The full Proxmox, config repo, secrets, deployment, and operations guides are in the documentation site.
Certificate mode selection and the local CA/Let's Encrypt procedures are in
the [TLS certificate guide](site/content/en/docs/configuration/tls-certificates.md).

## Documentation

The Hugo/Docsy documentation source lives in `site/`.
The published documentation is intended for GitHub Pages:

https://frantche.github.io/homelab-admin-node/

```bash
make docs-build
make docs-serve
```

GitHub Pages is built from `main` by `.github/workflows/pages.yml`.

## Development

Useful checks:

```bash
make build-admin-node
make lint
make validate
make ci-quality
make ci-bootstrap
```

Some targets require local tools such as Ansible, ShellCheck, SOPS, Docker, QEMU, or Hugo.
The Go modules require Go 1.27. Existing admin nodes may keep an older Arch
`go` package temporarily: convergence uses `GOTOOLCHAIN=auto` and the managed
root caches under `/var/cache/admin-node` to fetch and verify Go 1.27 before
rebuilding the CLI.
Contribution conventions and the validation matrix are documented in
[`CONTRIBUTING.md`](CONTRIBUTING.md).
