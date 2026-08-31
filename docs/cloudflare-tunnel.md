# Cloudflare Tunnel
Cloudflared utilise uniquement le mode de configuration locale. Le role rend et
monte `config.yml` et `credentials.json`, puis execute
`cloudflared tunnel --config /etc/cloudflared/config.yml run <UUID>`.

La configuration publie keycloak/bao/harbor/gitea ainsi que les services
externes ayant `cloudflare: true` via cloudflared -> Traefik. Le dashboard
Traefik reste interne et n'est jamais ajoute aux ingress cloudflared. Ce mode
requiert toujours `cloudflare.tunnel_id` et `cloudflare.credentials_json`.

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
