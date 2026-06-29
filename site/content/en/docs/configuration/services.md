---
title: Services
weight: 30
---

## Traefik

Traefik is the HTTPS entry point for the local services.

Key variables:

```yaml
traefik:
  dashboard_enabled: true
  dashboard_hostname: "traefik.example.com"
  log_level: "INFO"
  access_logs: true
  local_tls_enabled: false
```

## Pi-hole DNS

Pi-hole integration creates or validates local DNS records.

```yaml
pihole:
  enabled: true
  api_version: "auto"
  url: "http://pihole.local/admin"
  api_url: "http://pihole.local"
  dns_records:
    - name: "harbor.example.com"
      ip: "{{ admin_node_lan_ip }}"
```

Store the Pi-hole API token in encrypted secrets.

## Cloudflare Tunnel

Cloudflare Tunnel exposes services through Cloudflare without opening inbound ports directly to the node.

Store tunnel identifiers, tokens, account IDs, API tokens, and credentials JSON in encrypted secrets.

## Keycloak

Keycloak configuration is controlled by `keycloak_config`.

```yaml
keycloak_config:
  enabled: true
  realm:
    name: "homelab"
    display_name: "Homelab"
  realm_roles:
    - "admin"
```

## OIDC clients

Shared OIDC client IDs and secrets are defined once through `oidc_clients`.

```yaml
oidc_clients:
  harbor:
    client_id: "harbor"
    client_secret: "{{ vault_oidc_harbor_client_secret }}"
  openbao:
    client_id: "openbao"
    client_secret: "{{ vault_oidc_openbao_client_secret }}"
  gitea:
    client_id: "gitea"
    client_secret: "{{ vault_oidc_gitea_client_secret }}"
```

## OpenBao

OpenBao can be configured with secret engines and OIDC authentication.

```yaml
openbao_config:
  enabled: true
  oidc:
    enabled: true
    discovery_url: "https://keycloak.example.com/realms/homelab"
    default_role: "default"
```

## Harbor

Harbor supports OIDC and registry mirror proxy-cache projects.

```yaml
harbor_config:
  enabled: true
  oidc:
    enabled: true
    endpoint: "https://keycloak.example.com/realms/homelab"
    user_claim: "preferred_username"
    groups_claim: "groups"
  registry_mirrors: []
```

## Gitea

Gitea supports OIDC and a validation repository/issue used by restore checks.

```yaml
gitea_config:
  enabled: true
  oidc:
    enabled: true
    name: "keycloak"
    discovery_url: "https://keycloak.example.com/realms/homelab/.well-known/openid-configuration"
```
