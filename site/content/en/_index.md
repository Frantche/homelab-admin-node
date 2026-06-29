---
title: homelab-admin-node
linkTitle: Home
---

{{< blocks/cover title="homelab-admin-node" image_anchor="top" height="min" color="primary" >}}
A reproducible administration node for homelabs, built around Proxmox, cloud-init, Arch Linux, Ansible, Docker Compose, encrypted secrets, and a private Git-backed configuration repository.

{{< /blocks/cover >}}

{{% blocks/lead color="white" %}}
`homelab-admin-node` rebuilds and operates an independent `admin-01` virtual machine for core homelab services: Traefik, Keycloak, OpenBao, Harbor, Gitea, Cloudflare Tunnel, Pi-hole DNS integration, backups, restore, validation, and host hardening.
{{% /blocks/lead %}}

{{< blocks/section color="light" >}}
{{% blocks/feature icon="fa-solid fa-rotate" title="Reproducible operations" %}}
The node is converged from Git, cloud-init, Ansible roles, and Docker Compose stacks instead of hand-maintained server state.
{{% /blocks/feature %}}

{{% blocks/feature icon="fa-solid fa-lock" title="Private configuration" %}}
Public code stays in this repository. Deployment-specific values and SOPS-encrypted secrets live in a separate private config repository.
{{% /blocks/feature %}}

{{% blocks/feature icon="fa-solid fa-life-ring" title="Recovery oriented" %}}
Lifecycle modes, backups, restore procedures, and validation checks are part of the project instead of afterthoughts.
{{% /blocks/feature %}}
{{< /blocks/section >}}
