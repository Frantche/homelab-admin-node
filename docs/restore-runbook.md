# Restore runbook
1. `bin/admin-node mode set restore`
2. Optionnel: définir `/etc/admin-node/restore-id` avec `latest` ou un ID de backup.
3. Lancer `bin/admin-node restore run --id latest`.
4. Vérifier passage auto vers `normal` sinon `restore_failed`.
5. Lancer `bin/admin-node validate hardening` pour confirmer les contrôles de durcissement principaux après restauration.

Pour choisir interactivement un backup disponible :

```bash
bin/admin-node backup list
bin/admin-node restore select
bin/admin-node restore run --id <backup-id>
```

Si `offline-images.tar` est present dans le backup, le restore charge les images Docker avec `docker load` avant de redemarrer les stacks.

Les bases PostgreSQL sont restaurees depuis les archives custom `keycloak.dump`, `gitea.dump` et `harbor.dump` avec `pg_restore`. Le restore recree la base cible avant import et ne prend pas en charge les anciens dumps SQL plats.

## Restore Gitea via gitea-backup-restore-process

Utiliser ce flux uniquement pour restaurer un backup produit par
`backup.gitea_process`. Le projet externe fournit la commande `gitea-restore`,
qui utilise les memes variables d'environnement que `gitea-backup`.

Runbook prudent :

```bash
bin/admin-node mode set restore
systemctl stop admin-gitea-process-backup.timer

cd /srv/admin/stacks/gitea
docker compose --env-file /srv/admin/env/gitea.env -f compose.yaml stop gitea

install -d -m 0700 /srv/admin/backups/pre-gitea-process-restore
rsync -a --delete /srv/admin/data/gitea/ /srv/admin/backups/pre-gitea-process-restore/gitea-data/

export BACKUP_FILENAME="gitea-backup-YYYY-MM-DD-HH-MM-SS.zip"

docker run --rm \
  --network admin-net \
  --env-file /srv/admin/env/gitea-process-backup.env \
  -e BACKUP_FILENAME="$BACKUP_FILENAME" \
  -v /srv/admin/data/gitea:/data \
  -v /srv/admin/backups/gitea-process/restore-tmp:/srv/admin/backups/gitea-process/restore-tmp \
  ghcr.io/frantche/gitea-backup-restore-process:0.3.6 \
  gitea-restore

docker compose --env-file /srv/admin/env/gitea.env -f compose.yaml up -d
bin/admin-node validate apis
bin/admin-node mode set normal
bin/admin-node converge run
```

Garder `gitea-db` demarre pendant le restore : le helper restaure la base detectee
depuis `/data/gitea/conf/app.ini`. Adapter l'image, le network et
`RESTORE_TMP_FOLDER` si ces valeurs ont ete personnalisees dans
`/srv/admin/env/gitea-process-backup.env`. `BACKUP_FILENAME` doit etre le nom
exact du fichier `.zip` distant a restaurer et n'est requis que pour ce restore
manuel.
