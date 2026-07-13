# Cloudflare Tunnel
Par défaut, cloudflared utilise `cloudflare.tunnel_token`.

Quand au moins un service dans `traefik.external_services` définit `cloudflare: true`, le rôle rend `stacks/cloudflared/config.yml.tpl` en configuration locale et publie keycloak/bao/harbor/gitea/traefik ainsi que ces services externes via cloudflared -> Traefik. Ce mode requiert `cloudflare.tunnel_id` et `cloudflare.credentials_json`.

Le module Cloudflare peut etre active ou desactive via la configuration non secrete :

```yaml
cloudflare:
  enabled: true
```
