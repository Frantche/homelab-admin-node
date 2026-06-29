---
title: Overview
weight: 10
---

`homelab-admin-node` is the code repository for a dedicated homelab administration VM named `admin-01`.

The node is intentionally independent from Talos or Kubernetes. It hosts and operates the control-plane services a homelab often needs before the rest of the platform is healthy:

- Traefik for HTTPS ingress and the local dashboard.
- Keycloak for identity and OIDC.
- OpenBao for secret management.
- Harbor for container registry and proxy-cache mirrors.
- Gitea for Git hosting and validation workflows.
- Cloudflare Tunnel for public ingress when enabled.
- Pi-hole DNS integration for local records.
- Restic-based backups and restore.
- Hardening, API validation, and CI lifecycle scenarios.

The repository is designed around a clean split:

- This public repository contains code, Ansible roles, templates, Compose stacks, tests, and documentation.
- A separate private config repository contains your deployment values and SOPS-encrypted secrets.

That split lets you rebuild an admin node from source while keeping private infrastructure settings out of the public codebase.
