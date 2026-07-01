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
- Backup, restore, validation, and disaster recovery are part of the normal workflow.

## Quick Start

1. Create a Proxmox VM from an Arch Linux cloud image.
2. Attach `cloud-init/admin-01.user-data.yaml`.
3. Boot the VM and wait for cloud-init to clone this repository into `/opt/homelab-admin-node`.
4. Create or clone the private config repository under `/etc/admin-config/homelab-node-admin-config`.
   The current layout uses `di/` and `pr/`; the VM selects `di` with:

   ```text
   INVENTORY_PATH=/etc/admin-config/homelab-node-admin-config/di/inventory.ini
   ```

5. Install the age private key with:

   ```bash
   sudo /opt/homelab-admin-node/bin/admin-node secret install-age-key ./age-key.txt
   ```

6. Switch to init mode, converge, then initialize OpenBao if needed:

   ```bash
   sudo /opt/homelab-admin-node/bin/admin-node mode set init
   sudo /opt/homelab-admin-node/bin/admin-node converge run
   sudo /opt/homelab-admin-node/bin/admin-node openbao init-if-needed
   ```

7. Commit the generated or updated encrypted secrets to the private config repo.
8. Switch to normal mode and validate:

   ```bash
   sudo /opt/homelab-admin-node/bin/admin-node mode set normal
   sudo /opt/homelab-admin-node/bin/admin-node converge run
   sudo /opt/homelab-admin-node/bin/admin-node validate all
   ```

The full Proxmox, config repo, secrets, deployment, and operations guides are in the documentation site.

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
make test-ci-fast
```

Some targets require local tools such as Ansible, ShellCheck, SOPS, Docker, QEMU, or Hugo.
