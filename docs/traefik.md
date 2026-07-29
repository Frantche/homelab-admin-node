# Traefik
Routes:
- keycloak.example.com -> keycloak:8080
- bao.example.com -> openbao:8200 en HTTPS vérifié par la CA interne dédiée
- harbor.example.com -> harbor
- traefik.example.com -> dashboard (basic auth)
- external services declared in `traefik.external_services` -> configured URL

TLS:
- Let’s Encrypt DNS-01 is used when `cloudflare.dns_api_token` and `acme_email` are configured outside CI and `traefik.local_tls_enabled` is false.
- Otherwise, the Traefik role creates a local root CA and a server certificate signed by that CA.
- Import `/srv/admin/certs/root-ca.pem` in your browser to trust the local fallback certificate. The same CA is also available at `/srv/admin/certs/ca.pem` for services.

HTTP protections:

- All public routers add HSTS, MIME sniffing protection, a restrictive referrer policy, a permissions policy, and `SAMEORIGIN` framing.
- OpenBao and Keycloak use a shared request-rate and concurrency limit. The dashboard combines these protections with basic authentication.
- Harbor and Gitea use concurrency protection only. Request-rate and body buffering limits are deliberately omitted so registry pushes and Git LFS transfers remain supported.
- External services receive the security headers without an application-specific traffic limit.

The defaults can be adjusted under `traefik.security`. The same policy applies over LAN and public ingress: administrative access remains protected by each application's strong authentication (OIDC where configured), and the Traefik dashboard additionally requires basic authentication. The runtime contract test verifies both anonymous refusal and authenticated access.

Forwarded client addresses are accepted only from explicitly trusted reverse proxies:

```yaml
traefik:
  forwarded_headers_trusted_ips:
    - "192.0.2.0/24"
```

Leave this list empty for direct client access. When using a CDN or another proxy, configure only its current egress CIDRs; do not trust forwarded headers from every source.

External services:

```yaml
traefik:
  external_services:
    - name: "nas"
      hostname: "nas.example.com"
      url: "http://192.168.1.50:8080"
      pihole_dns: true
      cloudflare: false
```

For an HTTPS backend with a self-signed certificate, either disable backend certificate verification:

```yaml
traefik:
  external_services:
    - name: "legacy-app"
      hostname: "legacy.example.com"
      url: "https://192.168.1.51:8443"
      tls:
        verify: false
```

Or provide the backend CA certificate:

```yaml
traefik:
  external_services:
    - name: "internal-app"
      hostname: "internal.example.com"
      url: "https://internal.lan:9443"
      tls:
        ca_pem: |
          -----BEGIN CERTIFICATE-----
          ...
          -----END CERTIFICATE-----
```

`pihole_dns: true` adds a local DNS record to `admin_node_lan_ip`. `cloudflare: true` renders the hostname in the local Cloudflare Tunnel ingress config and requires `cloudflare.tunnel_id` plus `cloudflare.credentials_json`.
