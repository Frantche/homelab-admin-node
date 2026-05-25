# Traefik
Routes:
- keycloak.example.com -> keycloak:8080
- bao.example.com -> openbao:8200
- harbor.example.com -> harbor
- traefik.example.com -> dashboard (basic auth)
