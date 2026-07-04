---
title: Services
weight: 30
---

## Traefik

Traefik is the HTTPS entry point for the local services.

Reference: [Traefik documentation](https://doc.traefik.io/traefik/).

```yaml
traefik:
  dashboard_enabled: true
  dashboard_hostname: "traefik.example.com"
  log_level: "INFO"
  access_logs: true
  local_tls_enabled: false
```

| Variable | Default/example | Purpose |
| --- | --- | --- |
| `traefik.dashboard_enabled` | `true` | Enables the Traefik dashboard route. |
| `traefik.dashboard_hostname` | `traefik.example.com` | Hostname used for the dashboard route. Usually matches `service_domains.traefik`. |
| `traefik.log_level` | `INFO` | Traefik log level. |
| `traefik.access_logs` | `true` | Enables access logs. |
| `traefik.local_tls_enabled` | `false` | Forces local CA certificates instead of ACME. Local TLS is also used in CI or when Cloudflare DNS/ACME inputs are missing. |

## Pi-hole DNS

Pi-hole integration creates or validates local DNS records.

Reference: [Pi-hole documentation](https://docs.pi-hole.net/).

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

| Variable | Default/example | Purpose |
| --- | --- | --- |
| `pihole.enabled` | `true` | Enables Pi-hole DNS record management and validation. |
| `pihole.api_version` | `auto` | API mode used by the role. Supported values are `auto`, `v5`, and `v6`. |
| `pihole.url` | `http://pihole.local/admin` | Pi-hole admin UI URL. |
| `pihole.api_url` | `http://pihole.local` | Base URL used for Pi-hole API calls. |
| `pihole.dns_records[].name` | `harbor.example.com` | DNS name to create or validate. |
| `pihole.dns_records[].ip` | `{{ admin_node_lan_ip }}` | Target IP for the DNS record. |
| `pihole.api_token` | secret | API token stored in the active environment secrets file, such as `di/group_vars/secrets.sops.yaml`. |

## Cloudflare Tunnel

Cloudflare Tunnel exposes services through Cloudflare without opening inbound ports directly to the node.

Reference: [Cloudflare Tunnel documentation](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/).

Store tunnel identifiers, tokens, account IDs, API tokens, and credentials JSON in encrypted secrets.

| Variable | Secret | Purpose |
| --- | --- | --- |
| `cloudflare.tunnel_id` | yes | Tunnel ID used by validation and tunnel configuration. |
| `cloudflare.tunnel_token` | yes | Token injected into the cloudflared container. |
| `cloudflare.account_id` | yes | Cloudflare account ID used for tunnel checks. |
| `cloudflare.dns_api_token` | yes | DNS API token used by Traefik ACME DNS challenge when available. |
| `cloudflare.credentials_json` | yes | Credentials JSON for tunnel operations that need it. |

## Keycloak

Keycloak configuration is controlled by `keycloak_config`.

Reference: [Keycloak documentation](https://www.keycloak.org/documentation).

```yaml
keycloak_config:
  enabled: true
  realm:
    name: "homelab"
    display_name: "Homelab"
  realm_roles:
    - "admin"
```

| Variable | Default/example | Purpose |
| --- | --- | --- |
| `keycloak_config.enabled` | `false` | Enables Keycloak realm, client, group, and user configuration. |
| `keycloak_config.realm.name` | `homelab` | Realm name. |
| `keycloak_config.realm.display_name` | `Homelab` | Realm display name. |
| `keycloak_config.realm.ssl_required` | `external` | Realm SSL policy. |
| `keycloak_config.realm_roles[]` | `[]` | Realm roles to create. |
| `keycloak_config.groups[]` | `[]` | Group names to create. |
| `keycloak_config.clients[]` | `[]` | Extra or overriding OIDC client definitions. Matching Harbor, OpenBao, or Gitea clients are merged with managed defaults. |
| `keycloak_config.clients[].client_id` | required | Keycloak client ID. |
| `keycloak_config.clients[].name` | `client_id` | Display name for the client. |
| `keycloak_config.clients[].enabled` | `true` | Enables the client. |
| `keycloak_config.clients[].public` | `false` | Creates a public client when true. |
| `keycloak_config.clients[].client_authenticator_type` | module default | Client authenticator type, such as `client-secret`. |
| `keycloak_config.clients[].standard_flow_enabled` | `true` | Enables authorization code flow. |
| `keycloak_config.clients[].direct_access_grants_enabled` | `false` | Enables direct access grants. |
| `keycloak_config.clients[].service_accounts_enabled` | `false` | Enables service accounts. |
| `keycloak_config.clients[].redirect_uris` | `[]` | Valid redirect URIs. |
| `keycloak_config.clients[].web_origins` | `[]` | Allowed web origins. |
| `keycloak_config.clients[].root_url` | unset | Optional root URL. |
| `keycloak_config.clients[].base_url` | unset | Optional base URL. |
| `keycloak_config.clients[].protocol_mappers` | managed clients include groups mapper | Protocol mappers passed to Keycloak. |
| `keycloak_config.clients[].secret` | from `oidc_clients` for managed clients | Client secret. Store encrypted. |
| `keycloak_config.users[]` | `[]` | Users to create and optionally assign realm roles and groups. Passwords belong in encrypted secrets. |
| `keycloak_config.users[].username` | required | Username for a managed user. |
| `keycloak_config.users[].enabled` | `true` | Enables the user. |
| `keycloak_config.users[].email` | unset | User email. |
| `keycloak_config.users[].first_name` | unset | User first name. |
| `keycloak_config.users[].last_name` | unset | User last name. |
| `keycloak_config.users[].email_verified` | `false` | Marks the email as verified. |
| `keycloak_config.users[].password` | unset | Password to set or rotate. Store encrypted. |
| `keycloak_config.users[].temporary_password` | `false` | Marks the password as temporary. |
| `keycloak_config.users[].realm_roles` | `[]` | Realm roles to assign to the user. |
| `keycloak_config.users[].groups` | `[]` | Existing group names to assign to the user. |
| `keycloak_config_url` | `https://{{ service_domains.keycloak }}` | Role default for the Keycloak API base URL. |

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

| Variable | Default in CI | Required outside CI | Purpose |
| --- | --- | --- | --- |
| `oidc_clients.harbor.client_id` | `harbor` | when Harbor OIDC is enabled | Client ID managed in Keycloak and configured in Harbor. |
| `oidc_clients.harbor.client_secret` | CI placeholder | when Harbor OIDC is enabled | Harbor OIDC client secret. Store encrypted. |
| `oidc_clients.openbao.client_id` | `openbao` | when OpenBao OIDC is enabled | Client ID managed in Keycloak and configured in OpenBao. |
| `oidc_clients.openbao.client_secret` | CI placeholder | when OpenBao OIDC is enabled | OpenBao OIDC client secret. Store encrypted. |
| `oidc_clients.gitea.client_id` | `gitea` | when Gitea OIDC is enabled | Client ID managed in Keycloak and configured in Gitea. |
| `oidc_clients.gitea.client_secret` | CI placeholder | when Gitea OIDC is enabled | Gitea OIDC client secret. Store encrypted. |

## OpenBao

OpenBao can be configured with secret engines and OIDC authentication.

Reference: [OpenBao documentation](https://openbao.org/docs/).

```yaml
openbao_config:
  enabled: true
  secret_engines:
    - path: "secret"
      type: "kv-v2"
  oidc:
    enabled: true
    discovery_url: "https://keycloak.example.com/realms/homelab"
    default_role: "default"
    roles:
      - name: "admin"
        group: "admin"
        secret_engines:
          - "secret"
```

| Variable | Default/example | Purpose |
| --- | --- | --- |
| `openbao_config.enabled` | `false` | Enables OpenBao post-start configuration. |
| `openbao_config.root_token` | empty string | Root token fallback. `openbao.root_token` from secrets takes precedence. |
| `openbao_config.secret_engines[]` | `[]` | Secret engines to enable. Each item requires `path` and `type`. |
| `openbao_config.oidc.enabled` | `false` | Enables OIDC auth method configuration. |
| `openbao_config.oidc.discovery_url` | Keycloak realm URL | OIDC discovery issuer URL. |
| `openbao_config.oidc.discovery_ca_pem` | unset | Inline CA bundle for OIDC discovery. |
| `openbao_config.oidc.discovery_ca_file` | `/srv/admin/certs/ca.pem` | CA file read when inline CA PEM is not set. |
| `openbao_config.oidc.default_role` | `default` | Default OIDC role. |
| `openbao_config.oidc.role.name` | `default` | OIDC role name to create. |
| `openbao_config.oidc.role.user_claim` | `sub` | Claim used as the user identity. |
| `openbao_config.oidc.role.groups_claim` | `groups` | Claim used for groups. |
| `openbao_config.oidc.role.allowed_redirect_uris` | `[]` | Allowed OIDC callback URLs. |
| `openbao_config.oidc.role.bound_audiences` | `[]` | Accepted token audiences. |
| `openbao_config.oidc.role.policies` | ignored | Legacy field. The default OIDC role is always limited to `["default"]`. |
| `openbao_config.oidc.roles[]` | `[]` | Explicit OIDC roles that grant access to KV-v2 secret engines. Each item requires `name`, `group`, and `secret_engines`. |
| `openbao_config.oidc.roles[].name` | example: `admin` | OpenBao OIDC role name and suffix for the generated `oidc-<name>` ACL policy. |
| `openbao_config.oidc.roles[].group` | example: `admin` | Required OIDC `groups` claim value for the role. |
| `openbao_config.oidc.roles[].secret_engines[]` | example: `secret` | KV-v2 secret engine paths to administer. Paths are normalized with a trailing slash and must exist in `openbao_config.secret_engines`. |
| `openbao_config_url` | `https://{{ service_domains.openbao }}` | Role default for the OpenBao API base URL. |
| `openbao_config_validate_certs` | `false` | TLS certificate validation setting for OpenBao configuration API calls. |

## Harbor

Harbor supports OIDC and registry mirror proxy-cache projects.

Reference: [Harbor documentation](https://goharbor.io/docs/).

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

| Variable | Default/example | Purpose |
| --- | --- | --- |
| `harbor_config.enabled` | `false` | Enables Harbor API configuration. |
| `harbor_config.validate_certs` | `{{ not ci_mode }}` | TLS validation for Harbor API calls. |
| `harbor_config.validate_registry_mirrors` | `false` | Default validation toggle for mirror pull checks. |
| `harbor_config.oidc.enabled` | `false` | Enables Harbor OIDC configuration. |
| `harbor_config.oidc.endpoint` | Keycloak realm URL | OIDC issuer endpoint. |
| `harbor_config.oidc.verify_cert` | `{{ not ci_mode }}` | Tells Harbor whether to verify the OIDC provider certificate. |
| `harbor_config.oidc.auto_onboard` | `true` | Automatically creates Harbor users after OIDC login. |
| `harbor_config.oidc.scope` | `openid,profile,email,offline_access` | OIDC scopes. Must include `offline_access`. |
| `harbor_config.oidc.user_claim` | `preferred_username` | Claim Harbor uses as the username. |
| `harbor_config.oidc.groups_claim` | `groups` | Claim Harbor uses for groups. |
| `harbor_config.oidc.admin_group` | empty string | Group granted Harbor admin access. |
| `harbor_config.registry_mirrors[]` | `[]` | Proxy-cache registry declarations. |
| `harbor_config.registry_mirrors[].name` | required | Harbor registry endpoint name. |
| `harbor_config.registry_mirrors[].project_name` | mirror `name` | Harbor project name for the proxy cache. |
| `harbor_config.registry_mirrors[].type` | `docker-hub` | Registry type. Supported values: `docker-hub`, `docker-registry`, `github-ghcr`, `harbor`, `quay`. |
| `harbor_config.registry_mirrors[].url` | required | Upstream registry URL. |
| `harbor_config.registry_mirrors[].public` | example value | Whether the proxy-cache project is public. |
| `harbor_config.registry_mirrors[].username` | unset | Upstream registry username for private mirrors. Store encrypted when sensitive. |
| `harbor_config.registry_mirrors[].password` | unset | Upstream registry password/token for private mirrors. Store encrypted. |
| `harbor_config.registry_mirrors[].validation.enabled` | `harbor_config.validate_registry_mirrors` | Enables a test pull through the mirror. |
| `harbor_config.registry_mirrors[].validation.image` | required when validation is enabled | Image path used for mirror validation. |
| `harbor_config.registry_mirrors[].validation.username` | unset | Harbor username used by validation pull when needed. |
| `harbor_config.registry_mirrors[].validation.password` | unset | Harbor password/token used by validation pull when needed. |

## Gitea

Gitea supports OIDC and a validation repository/issue used by restore checks.

Reference: [Gitea documentation](https://docs.gitea.com/).

```yaml
gitea_config:
  enabled: true
  oidc:
    enabled: true
    name: "keycloak"
    discovery_url: "https://keycloak.example.com/realms/homelab/.well-known/openid-configuration"
```

| Variable | Default/example | Purpose |
| --- | --- | --- |
| `gitea_config.enabled` | `false` | Enables Gitea API and admin CLI configuration. |
| `gitea_config.validate_certs` | `{{ not ci_mode }}` | TLS validation for Gitea API calls. |
| `gitea_config.oidc.enabled` | `false` | Enables Gitea OpenID Connect auth source configuration. |
| `gitea_config.oidc.name` | `keycloak` | Name of the Gitea auth source. |
| `gitea_config.oidc.discovery_url` | Keycloak well-known URL | OpenID Connect discovery URL. |
| `gitea_config.oidc.scopes` | `openid email profile` | OAuth scopes passed to Gitea. |
| `gitea_config.validation.repo` | `admin-node-validation` | Repository used by restore validation. |
| `gitea_config.validation.issue_title` | `Backup restore sentinel` | Issue title used by restore validation. |
| `gitea_config_url` | `https://{{ service_domains.gitea }}` | Role default for the Gitea API base URL. |
