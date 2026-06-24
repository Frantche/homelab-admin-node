# Traefik
Routes:
- keycloak.example.com -> keycloak:8080
- bao.example.com -> openbao:8200
- harbor.example.com -> harbor
- traefik.example.com -> dashboard (basic auth)

TLS:
- Let’s Encrypt DNS-01 is used when `cloudflare.dns_api_token` and `acme_email` are configured outside CI and `traefik.local_tls_enabled` is false.
- Otherwise, the Traefik role creates a local root CA and a server certificate signed by that CA.
- Import `/srv/admin/certs/root-ca.pem` in your browser to trust the local fallback certificate. The same CA is also available at `/srv/admin/certs/ca.pem` for services.
