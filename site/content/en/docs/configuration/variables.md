---
title: Core Variables
weight: 20
---

The most important non-secret variables are:

| Variable | Purpose |
| --- | --- |
| `admin_repo_url` | Git URL used by the node to clone or update this repository. |
| `admin_node_root` | Runtime root for admin-node data, default `/srv/admin`. |
| `admin_mode_file` | Lifecycle mode file, default `/etc/admin-node/mode`. |
| `admin_git_ref_file` | File tracking the deployed Git reference. |
| `admin_node_lan_ip` | LAN IP used by DNS records and service defaults. |
| `admin_node_fqdn` | Optional FQDN for the node. |
| `acme_email` | Email used for ACME/TLS configuration. |
| `service_domains` | Hostnames for Harbor, OpenBao, Keycloak, Gitea, and Traefik. |
| `ci_mode` | Enables CI-specific behavior and mocks. |

Example:

```yaml
admin_node_lan_ip: "192.168.1.10"
acme_email: "admin@example.com"

service_domains:
  harbor: "harbor.example.com"
  openbao: "bao.example.com"
  keycloak: "keycloak.example.com"
  gitea: "git.example.com"
  traefik: "traefik.example.com"
```

Secrets should be placed in `group_vars/secrets.sops.yaml`, not in `group_vars/all.yml`.
