# Cloudflare Tunnel
Par défaut, cloudflared utilise `cloudflare.tunnel_token`.

Quand au moins un service dans `traefik.external_services` définit `cloudflare: true`, le rôle rend `stacks/cloudflared/config.yml.tpl` en configuration locale et publie keycloak/bao/harbor/gitea ainsi que ces services externes via cloudflared -> Traefik. Le dashboard Traefik reste interne et n'est jamais ajoute aux ingress cloudflared. Ce mode requiert `cloudflare.tunnel_id` et `cloudflare.credentials_json`.

Le module Cloudflare peut etre active ou desactive via la configuration non secrete :

```yaml
cloudflare:
  enabled: true
```

La gestion idempotente des CNAME publics peut etre activee sans wildcard :

```yaml
cloudflare:
  enabled: true
  dns:
    enabled: true
    zone: "example.com"
```

Le role fait pointer chaque domaine applicatif integre et chaque service externe
ayant `cloudflare: true` vers `<tunnel_id>.cfargotunnel.com`. Le domaine du
dashboard Traefik est exclu et son ancien CNAME vers ce tunnel est supprime. Le token
`cloudflare.dns_api_token`, chiffre avec SOPS, doit disposer de `Zone: Read` et
`DNS: Edit` sur cette zone.
