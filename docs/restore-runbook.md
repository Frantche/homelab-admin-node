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

Runbook via `admin-node` :

```bash
sudo /opt/homelab-admin-node/bin/admin-node gitea restore-process \
  --backup-filename gitea-backup-YYYY-MM-DD-HH-MM-SS.zip \
  --inventory /etc/admin-config/homelab-node-admin-config/di/inventory.ini
```

Garder `gitea-db` demarre pendant le restore : le helper restaure la base detectee
depuis `/data/gitea/conf/app.ini`. Adapter l'image, le network et
`RESTORE_TMP_FOLDER` si ces valeurs ont ete personnalisees dans
`/srv/admin/env/gitea-process-backup.env`. `BACKUP_FILENAME` doit etre le nom
exact du fichier `.zip` distant a restaurer et n'est requis que pour ce restore
dedie. Cette commande repasse en mode `normal` avant de lancer la convergence,
ce qui evite d'executer le restore homelab complet.
