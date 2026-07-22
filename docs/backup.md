# Backup

`bin/admin-node backup run` cree une sauvegarde locale sous `/srv/admin/backups/local`, puis exécute les sauvegardes restic depuis le même binaire.

Ansible construit `bin/admin-node` pendant le converge. En execution manuelle depuis le depot, lancer `make build-admin-node` si le binaire est absent.

## PostgreSQL

Les bases PostgreSQL applicatives sont exportees avec `pg_dump -Fc` au format custom PostgreSQL :

- `keycloak.dump` pour Keycloak ;
- `gitea.dump` pour Gitea quand `gitea-db` est present ;
- `harbor.dump` pour Harbor quand `harbor-db` est present.

Le restore utilise `pg_restore` et recree la base cible avant import. Les anciens backups contenant `keycloak.sql` ou `gitea.sql` ne sont pas supportes par ce flux.

Pour Harbor, `harbor.dump` couvre la base `registry`. Les blobs du registry et les autres donnees fichier restent sous `/srv/admin/data/harbor` et sont inclus dans les chemins Restic par defaut via `/srv/admin/data`.

## Restic

La configuration Ansible genere `/srv/admin/env/backup.env` depuis `backup.*`.

Format historique, toujours supporte :

```yaml
backup:
  restic_repository: "/srv/admin/backups/restic"
  restic_password: "CHANGE_ME"
  restic_forget_args: "--keep-last 3 --prune"
```

Format multi-destinations :

```yaml
backup:
  restic_default_forget_args: "--keep-daily 7 --keep-weekly 4 --keep-monthly 12 --prune"
  restic_init_repositories: false
  restic_require_secure_repositories: true
  restic_repositories:
    - name: local
      repository: "/srv/admin/backups/restic"
      password: "CHANGE_ME"
      forget_args: "--keep-last 3 --prune"

    - name: sftp
      repository: "sftp:backup-admin:/srv/restic/admin-node"
      password: "CHANGE_ME"

    - name: s3
      repository: "s3:https://s3.example.com/admin-node-restic"
      password: "CHANGE_ME"
      env:
        AWS_ACCESS_KEY_ID: "CHANGE_ME"
        AWS_SECRET_ACCESS_KEY: "CHANGE_ME"
        AWS_DEFAULT_REGION: "us-east-1"
```

Les connexions non chiffrees sont refusees par defaut. Les repositories `ftp:`, `rest:http://` et `s3:http://` echouent si `restic_require_secure_repositories` vaut `true`.

`rclone:` est aussi refuse par defaut, car le helper ne peut pas verifier si le remote rclone sous-jacent est chiffre. Il faut auditer le remote avant de desactiver explicitement `restic_require_secure_repositories`.

## Retention

Chaque destination peut definir `forget_args`. Sinon, `restic_default_forget_args` est utilise, avec `--keep-last 3 --prune` par defaut.

Exemples :

```yaml
forget_args: "--keep-last 3 --prune"
forget_args: "--keep-daily 7 --keep-weekly 4 --keep-monthly 12 --prune"
forget_args: "--keep-within-daily 7d --keep-within-weekly 1m --keep-within-monthly 1y --prune"
forget_args: "none"
```

`none` lance le backup sans `restic forget`.

## Gitea backup-restore-process

Le backup Restic/local reste le flux principal. Pour ajouter un second backup dedie a Gitea via
[`Frantche/gitea-backup-restore-process`](https://github.com/Frantche/gitea-backup-restore-process),
activer `backup.gitea_process.enabled`.

Ansible deploie alors :

- `/srv/admin/env/gitea-process-backup.env` avec les secrets backend ;
- `admin-gitea-process-backup.service` ;
- `admin-gitea-process-backup.timer`, programme par defaut chaque jour a `03:30`.

Le calendrier systemd est parametrable depuis l'inventaire avec
`backup.gitea_process.on_calendar`. Exemple : `*-*-* 02:15:00`.

Le service verifie `gitea-db` et `gitea` avant de lancer le conteneur. Si l'un des deux
conteneurs n'est pas `healthy`, le backup est ignore proprement pour cette execution.

Exemple S3 :

```yaml
backup:
  gitea_process:
    enabled: true
    on_calendar: "*-*-* 03:30:00"
    method: s3
    endpoint_url: "https://s3.example.com"
    bucket: "gitea-backups"
    aws_access_key_id: "CHANGE_ME"
    aws_secret_access_key: "CHANGE_ME"
    max_retention: 7
```

Exemple FTP :

```yaml
backup:
  gitea_process:
    enabled: true
    method: ftp
    ftp_host: "ftp.example.com:21"
    ftp_user: "backup-user"
    ftp_password: "CHANGE_ME"
    ftp_dir: "/gitea"
```

Secrets et identifiants doivent rester dans `group_vars/secrets.sops.yaml`.

### Restore Gitea avec backup-restore-process

Le projet `gitea-backup-restore-process` fournit aussi la commande `gitea-restore`.
Elle utilise les memes variables backend que le backup (`BACKUP_METHODE`, S3 ou FTP,
`APP_INI_PATH`, `RESTORE_TMP_FOLDER`) et restaure les fichiers Gitea ainsi que la
base detectee depuis `/data/gitea/conf/app.ini`.

Ce restore doit rester une operation manuelle et controlee :

1. Passer le noeud en mode restore pour eviter les convergences applicatives normales.

   ```bash
   sudo /opt/homelab-admin-node/bin/admin-node mode set restore
   ```

2. Arreter le timer de backup Gitea et le conteneur applicatif Gitea.

   ```bash
   sudo systemctl stop admin-gitea-process-backup.timer
   cd /srv/admin/stacks/gitea
   sudo docker compose --env-file /srv/admin/env/gitea.env -f compose.yaml stop gitea
   ```

   Garder `gitea-db` demarre : le restore PostgreSQL du helper a besoin de joindre
   la base indiquee par `app.ini`.

3. Faire une copie locale de securite de l'etat courant avant ecrasement.

   ```bash
   sudo install -d -m 0700 /srv/admin/backups/pre-gitea-process-restore
   sudo rsync -a --delete /srv/admin/data/gitea/ /srv/admin/backups/pre-gitea-process-restore/gitea-data/
   ```

4. Lancer le conteneur de restore avec le fichier d'environnement genere par Ansible.

   ```bash
   sudo docker run --rm \
     --network admin-net \
     --env-file /srv/admin/env/gitea-process-backup.env \
     -v /srv/admin/data/gitea:/data \
     -v /srv/admin/backups/gitea-process/restore-tmp:/srv/admin/backups/gitea-process/restore-tmp \
     ghcr.io/frantche/gitea-backup-restore-process:0.3.6 \
     gitea-restore
   ```

   Si `backup.gitea_process.image`, `network` ou `restore_tmp_folder` ont ete
   personnalises, reprendre les memes valeurs que dans
   `/srv/admin/env/gitea-process-backup.env`.

5. Redemarrer Gitea puis valider.

   ```bash
   cd /srv/admin/stacks/gitea
   sudo docker compose --env-file /srv/admin/env/gitea.env -f compose.yaml up -d
   sudo /opt/homelab-admin-node/bin/admin-node validate apis
   ```

6. Une fois le service verifie, revenir en mode normal et relancer la convergence.

   ```bash
   sudo /opt/homelab-admin-node/bin/admin-node mode set normal
   sudo /opt/homelab-admin-node/bin/admin-node converge run
   ```

Le restore integre du binaire `admin-node` reste le chemin recommande pour restaurer
un backup homelab complet. Le restore `gitea-restore` est reserve aux restaurations
Gitea issues du second flux `backup.gitea_process`.

## CI

`make test-restic-config` valide :

- un repository restic local ;
- un repository SFTP local via `sshd` loopback, cle SSH dediee et mot de passe desactive ;
- le refus d'un repository `ftp://` non chiffre.

`make test-offline-images` valide le mecanisme offline avec une image Docker legere reelle (`busybox:latest` par defaut) :

- pull de l'image ;
- backup `--include-images` ;
- creation d'un `offline-images.tar` non vide ;
- suppression locale de l'image ;
- restore avec `docker load` ;
- verification que l'image est de nouveau disponible sans pull.

Pour produire une sauvegarde restaurable hors ligne :

```bash
bin/admin-node backup run --include-images
```

Le backup contient alors `offline-images.tar`. Pendant le restore, `bin/admin-node restore run` charge ce tar avant de redemarrer les stacks, ce qui permet de restaurer les images deja exportees sans pull reseau.
