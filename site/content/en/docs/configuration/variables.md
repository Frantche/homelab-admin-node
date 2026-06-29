---
title: Core Variables
weight: 20
---

The most important non-secret variables are:

| Variable | Default/example | Purpose |
| --- | --- | --- |
| `admin_repo_url` | `ssh://git@example.com/homelab/homelab-admin-node.git` | Git URL used by the node to clone or update this repository. |
| `admin_node_root` | `/srv/admin` | Runtime root for stack data, copied Compose files, env files, backups, and generated certificates. |
| `admin_mode_file` | `/etc/admin-node/mode` | File that records the lifecycle mode used by the admin-node workflow. |
| `admin_git_ref_file` | `/etc/admin-node/git-ref` | File that records the deployed Git reference. |
| `admin_node_lan_ip` | `192.168.1.10` | LAN IP used by DNS records and local certificate SANs. |
| `admin_node_fqdn` | empty string | Optional node FQDN for inventory-specific references. |
| `acme_email` | `admin@example.com` | Email used when Traefik can request ACME certificates. |
| `ci_mode` | `false` | Enables CI defaults and mock behavior for services that need external credentials. |

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

## Service domains

`service_domains` maps each stack component to the hostname used by Traefik, DNS records, OIDC redirect URLs, and API validation.

| Key | Default/example | Purpose |
| --- | --- | --- |
| `service_domains.harbor` | `harbor.example.com` | Harbor registry and API hostname. |
| `service_domains.openbao` | `bao.example.com` | OpenBao UI and API hostname. |
| `service_domains.keycloak` | `keycloak.example.com` | Keycloak realm and OIDC issuer hostname. |
| `service_domains.gitea` | `git.example.com` | Gitea UI and API hostname. |
| `service_domains.traefik` | `traefik.example.com` | Traefik dashboard hostname and default certificate common name. |

## CI switches

`ci_mode` enables CI-safe defaults. The nested `ci` map controls individual mocks used by validation and convergence tests.

| Variable | Default | Purpose |
| --- | --- | --- |
| `ci.mock_pihole` | `true` | Allows Pi-hole validation to run against CI mocks. |
| `ci.mock_cloudflare_tunnel` | `true` | Allows Cloudflare Tunnel validation to run without a real tunnel. |
| `ci.skip_public_url_validation` | `true` | Skips public URL validation in CI-oriented runs. |

Secrets should be placed in `group_vars/secrets.sops.yaml`, not in `group_vars/all.yml`.
