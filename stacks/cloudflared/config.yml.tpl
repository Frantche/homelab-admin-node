tunnel: "{{ cloudflare.tunnel_id }}"
credentials-file: /etc/cloudflared/credentials.json
ingress:
  - hostname: "{{ service_domains.keycloak }}"
    service: "http://traefik:80"
  - hostname: "{{ service_domains.openbao }}"
    service: "http://traefik:80"
  - hostname: "{{ service_domains.harbor }}"
    service: "http://traefik:80"
  - hostname: "{{ service_domains.traefik }}"
    service: "http://traefik:80"
  - service: http_status:404
