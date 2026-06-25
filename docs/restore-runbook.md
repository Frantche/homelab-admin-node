# Restore runbook
1. `scripts/set-mode.sh restore`
2. Optionnel: définir `/etc/admin-node/restore-id` avec `latest` ou un ID de backup.
3. Lancer `scripts/restore.sh` ou directement `bin/admin-node restore run --id latest`.
4. Vérifier passage auto vers `normal` sinon `restore_failed`.
5. Lancer `scripts/validate-hardening.sh` pour confirmer SSH, nftables, auditd, fail2ban, journald et permissions sensibles après restauration.

Pour choisir interactivement un backup disponible :

```bash
bin/admin-node backup list
bin/admin-node restore select
bin/admin-node restore run --id <backup-id>
```

Si `offline-images.tar` est present dans le backup, le restore charge les images Docker avec `docker load` avant de redemarrer les stacks.
