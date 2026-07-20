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
