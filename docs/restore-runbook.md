# Restore runbook
1. `scripts/set-mode.sh restore`
2. Optionnel: définir `/etc/admin-node/restore-id`
3. Lancer `scripts/restore.sh`
4. Vérifier passage auto vers `normal` sinon `restore_failed`.
5. Lancer `scripts/validate-hardening.sh` pour confirmer SSH, nftables, auditd, fail2ban, journald et permissions sensibles après restauration.
