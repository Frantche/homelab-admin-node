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
  - hostname: "{{ service_domains.gitea | default('git.example.com') }}"
    service: "https://traefik:443"
    originRequest:
      noTLSVerify: true
  - hostname: "{{ service_domains.traefik }}"
    service: "https://traefik:443"
    originRequest:
      noTLSVerify: true
{% for external_service in traefik_external_services | default([]) if external_service.cloudflare | default(false) | bool %}
  - hostname: "{{ external_service.hostname }}"
    service: "https://traefik:443"
    originRequest:
      noTLSVerify: true
{% endfor %}
  - service: http_status:404
