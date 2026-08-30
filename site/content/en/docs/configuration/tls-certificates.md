---
title: TLS Certificates
weight: 28
---

Traefik terminates HTTPS for the service domains. It supports two certificate
modes:

- a certificate signed by a local CA generated on `admin-01`;
- publicly trusted Let's Encrypt certificates obtained with the Cloudflare
  DNS-01 challenge.

The local CA is appropriate for a private LAN when you control the client trust
stores. Let's Encrypt avoids installing a CA on every client, but requires a
real domain managed in Cloudflare and a DNS API token.

## How the mode is selected

The mode is derived during convergence rather than selected by a separate
command.

| Result | Conditions |
| --- | --- |
| Let's Encrypt | `ci_mode` is `false`, `traefik.local_tls_enabled` is `false`, `cloudflare.enabled` is `true`, and both `cloudflare.dns_api_token` and `acme_email` are non-empty. |
| Local CA | `ci_mode` is `true`, local TLS is explicitly enabled, Cloudflare is disabled, or an ACME input is missing. |

Consequently, setting `traefik.local_tls_enabled: false` alone does not
guarantee Let's Encrypt. A missing DNS token or email deliberately falls back
to the local CA.

Keep the non-secret settings in the selected environment's
`group_vars/all.yml` and the Cloudflare token in
`group_vars/secrets.sops.yaml`.

## Enable the local CA certificate

Set this in the private config repo, for example in `pr/group_vars/all.yml`:

```yaml
traefik:
  local_tls_enabled: true
```

Run convergence:

```bash
sudo env INVENTORY_PATH=/etc/admin-config/homelab-node-admin-config/pr/inventory.ini \
  /opt/homelab-admin-node/bin/admin-node converge run
```

Convergence generates and manages:

| File | Purpose |
| --- | --- |
| `/srv/admin/certs/ca.pem` | Local CA certificate to trust on clients. |
| `/srv/admin/certs/ca-key.pem` | Local CA private key; keep only on the node and in protected backups. |
| `/srv/admin/certs/cert.pem` | Traefik server certificate. |
| `/srv/admin/certs/key.pem` | Traefik server private key. |

The server certificate covers the configured `service_domains`, external
service hostnames, `admin_node_fqdn` when set, `localhost`, and the node LAN IP.
The role installs the CA in the trust store of `admin-01` and in Docker's trust
configuration for the Harbor hostname.

Other computers, phones, and browsers do not automatically trust this CA.
Transfer `/srv/admin/certs/ca.pem` through a trusted channel and import it as a
trusted root CA on each client that needs the services. Never distribute
`ca-key.pem`, `key.pem`, or any other private key.

If a configured hostname changes or the server certificate is close to expiry,
the next convergence regenerates the local CA and server certificate set. A new
CA must then be imported on clients. Plan hostname changes carefully because
replacing the CA changes the trust anchor.

## Enable Let's Encrypt

Use real service names below a domain managed in Cloudflare. Set the following
non-secret values in the selected environment's `group_vars/all.yml`:

```yaml
acme_email: "admin@example.net"

cloudflare:
  enabled: true

traefik:
  local_tls_enabled: false
```

Store a Cloudflare DNS API token in the matching encrypted secrets file:

```yaml
cloudflare:
  dns_api_token: "REPLACE_IN_SOPS"
```

The token must be scoped to the required DNS zones and allowed to create and
remove DNS challenge records. Use the narrowest scope that covers all
`service_domains`. Edit and commit this value only through SOPS:

```bash
cd /etc/admin-config/homelab-node-admin-config
sudo env SOPS_AGE_KEY_FILE=/etc/sops/age/keys.txt \
  sops pr/group_vars/secrets.sops.yaml
sudo git add pr/group_vars/secrets.sops.yaml
sudo git commit -m "configure Traefik ACME DNS challenge"
sudo git push
```

Run convergence after the config repo is updated. Traefik requests one or more
certificates through the Cloudflare DNS-01 challenge and stores its ACME state
under `/srv/admin/data/traefik/letsencrypt`. Do not edit `acme.json` manually.
Cloudflare Tunnel exposure and certificate issuance are separate: DNS-01 does
not require opening port 443 to the Internet, but the domain and authoritative
DNS zone must be valid publicly.

## Verify the active certificate

First confirm that the rendered Traefik configuration contains the expected
resolver. The first command prints matches only in Let's Encrypt mode:

```bash
sudo grep -n "certificatesResolvers\|certResolver" \
  /srv/admin/stacks/traefik/traefik.yml \
  /srv/admin/stacks/traefik/dynamic/config.yml
sudo docker compose -f /srv/admin/stacks/traefik/compose.yaml logs --tail=100 traefik
```

Inspect the certificate served for a hostname:

```bash
openssl s_client -connect bao.example.net:443 -servername bao.example.net \
  </dev/null 2>/dev/null \
  | openssl x509 -noout -subject -issuer -dates
```

For local TLS, verify the chain explicitly with the generated CA:

```bash
openssl s_client -connect bao.example.net:443 -servername bao.example.net \
  -CAfile /srv/admin/certs/ca.pem </dev/null 2>/dev/null \
  | grep "Verify return code"
```

Finish with the repository validation:

```bash
sudo /opt/homelab-admin-node/bin/admin-node validate all
```

When switching modes, change the private config repo and converge again.
Previously generated local certificates may remain on disk after switching to
Let's Encrypt; use the rendered Traefik configuration and the certificate
issuer, not file presence, to determine the active mode.

## OpenBao's internal certificate

OpenBao also uses a separate internal CA and server certificate between
Traefik, OpenBao, and the metrics collector. The `openbao_pki` role manages the
`/srv/admin/certs/openbao-internal-*` files. This internal transport certificate
is always separate from the certificate that Traefik presents to browsers and
is not selected by `traefik.local_tls_enabled`.

Clients should connect to the configured OpenBao service domain through
Traefik. They trust either the admin-node local CA or Let's Encrypt according to
the Traefik mode; they do not need the OpenBao internal CA.
