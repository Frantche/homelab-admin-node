# Traefik
Routes:
- keycloak.example.com -> keycloak:8080
- bao.example.com -> openbao:8200
- harbor.example.com -> harbor
- traefik.example.com -> dashboard (basic auth)
- external services declared in `traefik.external_services` -> configured URL

TLS:
- Let’s Encrypt DNS-01 is used when `cloudflare.dns_api_token` and `acme_email` are configured outside CI and `traefik.local_tls_enabled` is false.
- Otherwise, the Traefik role creates a local root CA and a server certificate signed by that CA.
- Import `/srv/admin/certs/root-ca.pem` in your browser to trust the local fallback certificate. The same CA is also available at `/srv/admin/certs/ca.pem` for services.

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
