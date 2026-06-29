---
title: Secrets
weight: 25
---

Secrets live in the private config repo and are encrypted with SOPS.

Use:

```text
/etc/admin-config/homelab-node-admin-config/group_vars/secrets.sops.yaml
```

Typical secret groups are:

| Group | Examples |
| --- | --- |
| `admin` | Traefik dashboard basic auth. |
| `pihole` | Pi-hole API token. |
| `cloudflare` | Tunnel ID, tunnel token, account ID, DNS API token, credentials JSON. |
| `keycloak` | Database password, admin user, admin password. |
| `harbor` | Admin password, DB password, core secret, jobservice secret, registry password. |
| `openbao` | Unseal material and root token when required by the workflow. |
| `backup` | Restic repositories, passwords, and backend environment variables. |
| `observability` | Backend credentials if your telemetry endpoints require them. |

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
