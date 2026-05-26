tunnel: "{{ cloudflare.tunnel_id }}"
credentials-file: /etc/cloudflared/credentials.json
ingress:
  - hostname: "{{ service_domains.keycloak }}"
    service: "https://traefik:443"
    originRequest:
      noTLSVerify: true
  - hostname: "{{ service_domains.openbao }}"
    service: "https://traefik:443"
    originRequest:
      noTLSVerify: true
  - hostname: "{{ service_domains.harbor }}"
    service: "https://traefik:443"
    originRequest:
      noTLSVerify: true
  - hostname: "{{ service_domains.traefik }}"
    service: "https://traefik:443"
    originRequest:
      noTLSVerify: true
  - service: http_status:404
