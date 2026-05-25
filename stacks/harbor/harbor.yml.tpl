hostname: "{{ service_domains.harbor }}"
http:
  port: 8080
harbor_admin_password: "{{ harbor.admin_password }}"
data_volume: /srv/admin/data/harbor
