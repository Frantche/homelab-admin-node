---
title: Secrets
weight: 25
---

Secrets live in the private config repo and are encrypted with SOPS.

Reference: [SOPS documentation](https://getsops.io/docs/).

Use:

```text
/etc/admin-config/homelab-node-admin-config/group_vars/secrets.sops.yaml
```

Typical secret groups are:

| Group/key | Purpose |
| --- | --- |
| `admin.traefik_dashboard_basic_auth` | Basic-auth hash consumed by the Traefik dashboard middleware. |
| `pihole.api_token` | Pi-hole API token used by DNS record management and validation. |
| `cloudflare.tunnel_id` | Cloudflare Tunnel identifier. |
| `cloudflare.tunnel_token` | Token used by the cloudflared container. |
| `cloudflare.account_id` | Cloudflare account identifier used by tunnel validation. |
| `cloudflare.dns_api_token` | Cloudflare DNS token used when ACME DNS challenge is available. |
| `cloudflare.credentials_json` | Cloudflare tunnel credentials JSON content. |
| `keycloak.db_password` | Keycloak database password. |
| `keycloak.admin_user` | Keycloak administrator username. |
| `keycloak.admin_password` | Keycloak administrator password. |
| `harbor.admin_password` | Harbor admin password used by API configuration tasks. |
| `harbor.db_password` | Harbor database password. |
| `harbor.core_secret` | Harbor core service secret. |
| `harbor.jobservice_secret` | Harbor jobservice secret. |
| `harbor.registry_password` | Harbor internal registry password. |
| `gitea.admin_user` | Gitea administrator username, defaulting to `admin` when omitted. |
| `gitea.admin_password` | Gitea administrator password. |
| `gitea.db_password` | Gitea database password. |
| `gitea.secret_key` | Gitea application secret key. |
| `gitea.internal_token` | Gitea internal token. |
| `gitea.jwt_secret` | Gitea JWT secret. |
| `openbao.root_token` | OpenBao root token used by the OpenBao configuration role. |
| `keycloak_config.users[].password` | Password for managed Keycloak users. |
| `keycloak_config.clients[].secret` | Secret for extra Keycloak clients, when not supplied through `oidc_clients`. |
| `oidc_clients.*.client_secret` | Shared OIDC client secrets for Harbor, OpenBao, and Gitea. |
| `backup.restic_repositories[].password` | Restic repository password. |
| `backup.restic_repositories[].env` | Backend-specific environment variables such as S3 credentials. |
| `observability.*` | Optional credentials or endpoint-specific values if telemetry backends require them. |

Some examples also show `keycloak_config`, `harbor_config`, and `openbao_config` in the secrets file when they contain secret material. Prefer keeping non-secret configuration in `group_vars/all.yml` and only secret values in `group_vars/secrets.sops.yaml`.

Edit secrets with:

```bash
sops group_vars/secrets.sops.yaml
```

Then commit the encrypted file:

```bash
git add group_vars/secrets.sops.yaml
git commit -m "update admin-node secrets"
git push
```

Never commit unencrypted secret files, `age-key.txt`, `/etc/sops/age/keys.txt`, tokens, or provider credentials.
