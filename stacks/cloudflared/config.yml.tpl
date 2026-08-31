tunnel: "{{ cloudflare.tunnel_id }}"
credentials-file: /etc/cloudflared/credentials.json
ingress:
  - hostname: "{{ service_domains.keycloak }}"
    service: "https://traefik:443"
    originRequest:
      originServerName: "{{ service_domains.keycloak }}"
{% if not (traefik_letsencrypt_enabled | default(false) | bool) %}
      caPool: /etc/cloudflared/admin-node-ca.pem
{% endif %}
  - hostname: "{{ service_domains.openbao }}"
    service: "https://traefik:443"
    originRequest:
      originServerName: "{{ service_domains.openbao }}"
{% if not (traefik_letsencrypt_enabled | default(false) | bool) %}
      caPool: /etc/cloudflared/admin-node-ca.pem
{% endif %}
  - hostname: "{{ service_domains.harbor }}"
    service: "https://traefik:443"
    originRequest:
      originServerName: "{{ service_domains.harbor }}"
{% if not (traefik_letsencrypt_enabled | default(false) | bool) %}
      caPool: /etc/cloudflared/admin-node-ca.pem
{% endif %}
  - hostname: "{{ service_domains.gitea | default('git.example.com') }}"
    service: "https://traefik:443"
    originRequest:
      originServerName: "{{ service_domains.gitea | default('git.example.com') }}"
{% if not (traefik_letsencrypt_enabled | default(false) | bool) %}
      caPool: /etc/cloudflared/admin-node-ca.pem
{% endif %}
{% for external_service in traefik_external_services | default([]) if external_service.cloudflare | default(false) | bool %}
  - hostname: "{{ external_service.hostname }}"
    service: "https://traefik:443"
    originRequest:
      originServerName: "{{ external_service.hostname }}"
{% if not (traefik_letsencrypt_enabled | default(false) | bool) %}
      caPool: /etc/cloudflared/admin-node-ca.pem
{% endif %}
{% endfor %}
  - service: http_status:404
