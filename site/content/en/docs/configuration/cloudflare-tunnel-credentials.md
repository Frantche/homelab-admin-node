---
title: Cloudflare Tunnel Credentials
weight: 27
---

The admin node supports two Cloudflare Tunnel modes:

- Token mode uses `cloudflare.tunnel_token`. Configure the published application hostnames in the Cloudflare dashboard or API.
- Local ingress mode uses `cloudflare.tunnel_id` and `cloudflare.credentials_json`. The admin-node role renders the ingress rules locally.

Local ingress mode is selected automatically when at least one item in `traefik.external_services` has `cloudflare: true`. The tunnel credentials JSON must come from a locally-managed Cloudflare Tunnel. Do not construct this file manually.

Reference: [Create a locally-managed tunnel](https://developers.cloudflare.com/tunnel/advanced/local-management/create-local-tunnel/) and [Tunnel permissions](https://developers.cloudflare.com/tunnel/advanced/local-management/tunnel-permissions/).

## Required Cloudflare permissions

The identity used to create the tunnel needs access to the Cloudflare account and zone that will own it. For an API token, use the least-privilege scopes below:

| Scope | Permission | Required for |
| --- | --- | --- |
| Account | Cloudflare Tunnel: Edit | Creating and managing the tunnel. |
| Zone | DNS: Edit | Creating the CNAME records for published hostnames. |
| Zone | Zone: Read | Looking up zone details in API-based automation. |

Restrict Account and Zone resources to the account and DNS zone used by the admin node.

Interactive `cloudflared tunnel login` uses the permissions of the dashboard user. That user must be able to manage tunnels in the selected account and DNS in the selected zone.

## Create the credentials JSON

Run these commands on a trusted operator workstation. The workstation must have a browser available for the interactive login:

```bash
umask 077
cloudflared tunnel login
cloudflared tunnel create homelab-admin
```

On Linux, the commands normally create two files under `~/.cloudflared/`:

| File | Scope | Keep on `admin-01`? |
| --- | --- | --- |
| `cert.pem` | Account-wide tunnel management, including create, route, list, and delete operations. | No. Keep it only on a trusted operator workstation. |
| `<TUNNEL-UUID>.json` | Permission to run one specific tunnel. | Store its content in SOPS; do not copy the unencrypted file into the repository. |

The create command prints the tunnel UUID and the credentials file path. Verify the JSON structure without printing the secret:

```bash
jq -e '
  has("AccountTag") and
  has("TunnelID") and
  has("TunnelSecret")
' ~/.cloudflared/<TUNNEL-UUID>.json
```

The credentials file has this structure:

```json
{
  "AccountTag": "...",
  "TunnelID": "00000000-0000-0000-0000-000000000000",
  "TunnelSecret": "..."
}
```

Treat `TunnelSecret` as a password. Do not print the complete file in logs, shell traces, tickets, or CI output.

## Store the credentials with SOPS

Edit the secrets file for the active environment in the private config repository. For example:

```bash
cd /etc/admin-config/homelab-node-admin-config
sops di/group_vars/secrets.sops.yaml
```

Add the tunnel UUID and the complete JSON document:

```yaml
cloudflare:
  tunnel_id: "00000000-0000-0000-0000-000000000000"
  credentials_json: |
    {"AccountTag":"...","TunnelID":"00000000-0000-0000-0000-000000000000","TunnelSecret":"..."}
```

The `TunnelID` inside `credentials_json` must match `cloudflare.tunnel_id`. Token mode and local ingress mode use different credentials; `cloudflare.tunnel_token` is not used after local ingress mode is selected.

Commit only the SOPS-encrypted file:

```bash
git add di/group_vars/secrets.sops.yaml
git commit -m "configure Cloudflare tunnel credentials"
git push
```

Never commit `cert.pem`, the unencrypted `<TUNNEL-UUID>.json`, or a decrypted SOPS file.

## Select the published hostnames

Enable local ingress for an external service in the non-secret environment configuration:

```yaml
cloudflare:
  enabled: true

traefik:
  external_services:
    - name: "nas"
      hostname: "nas.example.com"
      url: "https://192.168.1.50:8443"
      cloudflare: true
      pihole_dns: false
      tls:
        ca_pem: |
          -----BEGIN CERTIFICATE-----
          ...
          -----END CERTIFICATE-----
```

When any external service sets `cloudflare: true`, the generated local ingress configuration includes the built-in service domains for Keycloak, OpenBao, Harbor, and Gitea, plus each opted-in external service. The Traefik dashboard hostname is deliberately excluded and remains reachable only through internal ingress. All Cloudflare ingress entries forward to Traefik as a reverse proxy, which then selects the application backend from the request hostname.

## Manage the public DNS routes

The admin-node role can manage one exact, proxied CNAME for every published hostname. Enable DNS management and declare the Cloudflare zone in the non-secret environment configuration:

```yaml
cloudflare:
  enabled: true
  dns:
    enabled: true
    zone: "example.com"
```

The role builds the DNS list from the Keycloak, OpenBao, Harbor, and Gitea `service_domains` and every `traefik.external_services` item that has `cloudflare: true`. Each hostname points to the same `<TUNNEL-UUID>.cfargotunnel.com` target. No wildcard record is created. Convergence also removes the Traefik dashboard CNAME if, and only if, it points to this tunnel.

The operation is idempotent: convergence creates a missing CNAME and updates an existing CNAME when its tunnel target or proxy setting differs. A conflicting record of another type, such as an existing A record with the same name, causes an explicit Cloudflare API failure rather than deleting the record.

DNS management requires `cloudflare.dns_api_token` in the SOPS-encrypted environment secrets. Grant the token `Zone: Read` and `DNS: Edit` only for `cloudflare.dns.zone`. The role does not need the account-wide `cert.pem` and does not run `cloudflared tunnel route dns`.

## Converge and verify

Pull the encrypted configuration and run convergence:

```bash
git -C /etc/admin-config/homelab-node-admin-config pull --ff-only
sudo /opt/homelab-admin-node/bin/admin-node converge run
sudo /opt/homelab-admin-node/bin/admin-node validate tunnel
```

Inspect the container without displaying its environment or credential contents:

```bash
sudo docker ps --filter name=cloudflared
sudo docker logs --tail 100 cloudflared
```

The rendered credentials file must remain inaccessible to unrelated host users while being readable by the non-root cloudflared container user. If the log reports `permission denied` for `/etc/cloudflared/config.yml` or `/etc/cloudflared/credentials.json`, verify the bind-mounted files' owner, group, and mode. Use a runtime-specific group with mode `0640`; do not make the credentials world-readable.
