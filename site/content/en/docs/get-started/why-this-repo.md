---
title: Why This Repository
weight: 50
---

This repository is useful when you want the admin node itself to be reproducible, reviewable, and recoverable.

The main advantages are:

- **Git-backed infrastructure state**: code, playbooks, templates, service definitions, and documentation evolve through normal Git review.
- **Private deployment state**: local IPs, domains, credentials, and secrets are isolated in a private config repo.
- **Encrypted secrets**: SOPS and age keep secrets encrypted at rest while still allowing them to be versioned.
- **Safe bootstrap**: the node starts in `locked` mode until the secret zero and config repository are present.
- **Predictable convergence**: `admin-node converge run` pulls code and applies Ansible state consistently.
- **Recoverability**: backups, restore mode, validation, and disaster recovery are documented and tested as first-class workflows.
- **Homelab independence**: the admin VM can operate outside Kubernetes and remains available when cluster services are broken.
