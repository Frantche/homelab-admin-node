# Dépôt de configuration séparé (config repo)

## Principe

`homelab-admin-node` est le dépôt **code** : il contient la logique Ansible, les scripts, les templates.  
Un second dépôt **privé**, le *config repo*, contient les valeurs propres à votre déploiement :

- variables non-secrètes (`group_vars/all.yml`) : domaines, IPs, options de services…
- secrets chiffrés SOPS (`group_vars/secrets.sops.yaml`) : mots de passe, tokens, credentials…

Cette séparation permet de partager `homelab-admin-node` publiquement tout en gardant votre configuration et vos secrets dans un dépôt privé versionné.

## Structure du config repo

```
homelab-admin-node-config/          # nom libre
├── .gitignore
├── .sops.yaml                      # clé age publique pour ce déploiement
├── README.md
├── hosts                           # inventaire Ansible minimal
└── group_vars/
    ├── all.yml                     # overrides de configuration (non-secrets)
    └── secrets.sops.yaml           # secrets chiffrés avec SOPS + age
```

### `hosts`

```ini
[admin]
localhost ansible_connection=local
```

### `.sops.yaml`

```yaml
creation_rules:
  - path_regex: group_vars/secrets\.sops\.yaml$
    age: ["age1xxxx...votre-clé-publique-age..."]
```

Les exemples publics maintenus dans ce dépôt se trouvent dans `examples/admin-config/group_vars/`.

### `group_vars/all.yml` — exemple

```yaml
admin_node_lan_ip: "192.168.1.10"
acme_email: "admin@mondomaine.fr"

service_domains:
  harbor: "harbor.mondomaine.fr"
  openbao: "bao.mondomaine.fr"
  keycloak: "keycloak.mondomaine.fr"
  traefik: "traefik.mondomaine.fr"

oidc_clients:
  harbor:
    client_id: "harbor"
    client_secret: "{{ vault_oidc_harbor_client_secret }}"
  openbao:
    client_id: "openbao"
    client_secret: "{{ vault_oidc_openbao_client_secret }}"

traefik:
  dashboard_enabled: true
  dashboard_hostname: "traefik.mondomaine.fr"

pihole:
  enabled: true
  url: "http://pihole.local/admin"
  api_url: "http://pihole.local"
  dns_records:
    - name: "harbor.mondomaine.fr"
      ip: "{{ admin_node_lan_ip }}"
    - name: "bao.mondomaine.fr"
      ip: "{{ admin_node_lan_ip }}"
    - name: "keycloak.mondomaine.fr"
      ip: "{{ admin_node_lan_ip }}"
    - name: "traefik.mondomaine.fr"
      ip: "{{ admin_node_lan_ip }}"
```

### `group_vars/secrets.sops.yaml` — exemple (avant chiffrement)

```yaml
vault_oidc_harbor_client_secret: "CHANGE_ME_IN_SOPS"
vault_oidc_openbao_client_secret: "CHANGE_ME_IN_SOPS"
admin:
  traefik_dashboard_basic_auth: "admin:$$apr1$$hash"
pihole:
  api_token: "votre-token-pihole"
cloudflare:
  tunnel_id: "uuid-du-tunnel"
  tunnel_token: "eyJ..."
  account_id: "votre-account-id"
  dns_api_token: "votre-dns-token"
  credentials_json: |
    {"AccountTag":"...","TunnelID":"...","TunnelSecret":"..."}
keycloak:
  db_password: "mot-de-passe-db"
  admin_user: "admin"
  admin_password: "mot-de-passe-admin"
harbor:
  admin_password: "mot-de-passe-harbor"
  db_password: "mot-de-passe-db-harbor"
  core_secret: "secret-core-harbor"
  jobservice_secret: "secret-jobservice-harbor"
  registry_password: "mot-de-passe-registry-harbor"
backup:
  restic_default_forget_args: "--keep-daily 7 --keep-weekly 4 --keep-monthly 12 --prune"
  restic_require_secure_repositories: true
  restic_repositories:
    - name: local
      repository: "/srv/admin/backups/restic"
      password: "mot-de-passe-restic"
    - name: sftp
      repository: "sftp:backup-admin:/srv/restic/admin-node"
      password: "mot-de-passe-restic-sftp"
    - name: s3
      repository: "s3:https://s3.example.com/admin-node-restic"
      password: "mot-de-passe-restic-s3"
      env:
        AWS_ACCESS_KEY_ID: "access-key"
        AWS_SECRET_ACCESS_KEY: "secret-key"
        AWS_DEFAULT_REGION: "us-east-1"
```

Définissez les `client_id` et `client_secret` OIDC une seule fois via `oidc_clients`. Hors CI, `oidc_clients.harbor.client_id`, `oidc_clients.harbor.client_secret` et, si l'OIDC OpenBao est activé, `oidc_clients.openbao.*` sont obligatoires. En CI (`ci_mode: true`), le dépôt utilise des valeurs mock déterministes distinctes de la production.

### `.gitignore`

```gitignore
# Ne jamais committer de secrets non chiffrés
group_vars/secrets.yml
group_vars/secrets.yaml
*.age
*.key
```

## Mise en place — pas à pas

### 1. Créer le dépôt Git privé

```bash
mkdir homelab-admin-node-config
cd homelab-admin-node-config
git init
git remote add origin git@github.com:<username>/homelab-admin-node-config.git
```

### 2. Générer la clé age (secret zéro)

```bash
# Générer la paire de clés
age-keygen -o age-key.txt
# Affiche : Public key: age1xxxx...

# Installer la clé privée sur l'admin-node (secret zéro)
sudo install -D -m 0400 -o root -g root age-key.txt /etc/sops/age/keys.txt

# Supprimer la copie locale non protégée
rm age-key.txt
```

Conservez une copie sécurisée de la clé privée (gestionnaire de mots de passe, coffre HSM…).
La procédure complète est documentée dans `docs/secret-zero.md`.

### 3. Configurer SOPS

Créez `.sops.yaml` avec la clé **publique** :

```yaml
creation_rules:
  - path_regex: group_vars/secrets\.sops\.yaml$
    age: ["age1xxxx...votre-clé-publique..."]
```

### 4. Créer et chiffrer les secrets

```bash
# Créer le fichier en clair, puis chiffrer
sops group_vars/secrets.sops.yaml
# ou depuis un fichier existant :
sops --encrypt group_vars/secrets.yaml > group_vars/secrets.sops.yaml
```

### 5. Structurer la configuration

Remplissez `group_vars/all.yml` avec vos variables non-secrètes et `hosts` avec l'inventaire minimal.

### 6. Committer et pousser

```bash
git add .
git commit -m "initial config"
git push -u origin main
```

## Utilisation avec admin-node converge

Le premier `git clone` du dépôt `homelab-admin-node` est réalisé par cloud-init dans `/opt/homelab-admin-node`.  
`bin/admin-node converge run` exécute ensuite `git pull --ff-only` sur ce dépôt avant chaque convergence. Apres la synchronisation Ansible, le role `base` appelle `scripts/build-admin-node.sh`: le binaire Go est reconstruit uniquement si les sources Go ont change.

### 1. Mettre à jour le config repo via git CLI (optionnel)

```bash
git -C /etc/admin-config/homelab-node-admin-config pull --ff-only
```

### 2. Déposer l'inventaire Ansible utilisateur

Par défaut, `admin-node converge run` lit l'inventaire depuis `/etc/admin-config/homelab-node-admin-config/hosts/inventory.ini`.
Un exemple minimal est disponible dans ce dépôt: `ansible/inventory.ini`.

```bash
sudo install -D -m 0644 /opt/homelab-admin-node/ansible/inventory.ini /etc/admin-config/homelab-node-admin-config/hosts/inventory.ini
```

### 3. Lancer la convergence

```bash
sudo ./bin/admin-node converge run
```

`admin-node converge run` :

1. Met à jour `/opt/homelab-admin-node` via `git pull --ff-only`
2. Vérifie la présence du playbook local `/opt/homelab-admin-node/ansible/site.yml`
3. Vérifie la présence de l'inventaire utilisateur `/etc/admin-config/homelab-node-admin-config/hosts/inventory.ini`
4. Exécute `ansible-playbook -i /etc/admin-config/homelab-node-admin-config/hosts/inventory.ini /opt/homelab-admin-node/ansible/site.yml`
5. Pendant le role `base`, maintient `bin/admin-node` a jour via un build local conditionnel; `bin/admin-node` et `bin/admin-node.source.sha256` ne sont pas versionnes.

## Modifier les secrets

```bash
# Éditer directement (SOPS ouvre l'éditeur configuré)
sops group_vars/secrets.sops.yaml

# Puis committer le fichier chiffré modifié
git add group_vars/secrets.sops.yaml
git commit -m "update secrets"
git push
```

## Exemple CI — mock config repo

Le répertoire `ci/mock-config-repo/` de ce dépôt constitue un exemple minimal de config repo (sans chiffrement SOPS, pour les tests CI). La CI l'installe avec `bin/admin-node ci install-mock-config-repo`; `ci/setup-ci-config-repo.sh` reste un wrapper de compatibilite.

## Sécurité

- Le config repo doit être **privé**.
- Le fichier `secrets.sops.yaml` doit toujours être chiffré avant d'être commité (le `.gitignore` protège contre un commit accidentel en clair).
- La clé age privée (secret zéro) ne doit jamais être commitée.
- Consultez `docs/secret-zero.md` pour la gestion du secret zéro.
