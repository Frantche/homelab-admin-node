# Cloudflare Tunnel
`stacks/cloudflared/config.yml.tpl` publie keycloak/bao/harbor/traefik via cloudflared -> Traefik.

Le module Cloudflare peut etre active ou desactive via la configuration non secrete :

```yaml
cloudflare:
  enabled: true
```
