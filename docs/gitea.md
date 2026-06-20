# Gitea

Gitea est déployé comme stack Docker Compose dédiée derrière Traefik.

## Configuration

Le service public est défini par `service_domains.gitea` et vaut `git.example.com` dans les exemples CI.

Les secrets runtime doivent être fournis dans le config repo, idéalement dans `group_vars/secrets.sops.yaml` :

```yaml
gitea:
  admin_user: "admin"
  admin_password: "CHANGE_ME"
  db_password: "CHANGE_ME"
  secret_key: "CHANGE_ME"
  internal_token: "CHANGE_ME"
  jwt_secret: "CHANGE_ME"
```

## OIDC Keycloak

Le client Keycloak est géré par `ansible/roles/keycloak_config` quand `gitea_config.oidc.enabled=true`.

```yaml
oidc_clients:
  gitea:
    client_id: "gitea"
    client_secret: "CHANGE_ME"

gitea_config:
  enabled: true
  oidc:
    enabled: true
    name: "keycloak"
    discovery_url: "https://keycloak.example.com/realms/homelab/.well-known/openid-configuration"
```

La redirect URI attendue est :

```text
https://git.example.com/user/oauth2/keycloak/callback
```

## Validation

`scripts/validate-gitea-data.sh` valide l'API Gitea, crée si besoin le dépôt `admin-node-validation`, puis crée si besoin l'issue `Backup restore sentinel`.

Cette validation est appelée par `scripts/validate-apis.sh`, donc elle s'exécute avant backup et après restore.

## Backup Et Restore

`scripts/backup.sh` sauvegarde :

- la base PostgreSQL Gitea dans `gitea.sql`;
- les données applicatives et repositories depuis `/srv/admin/data/gitea`.

`scripts/restore.sh` restaure les données applicatives, réimporte `gitea.sql`, redémarre la stack et relance la validation API.
