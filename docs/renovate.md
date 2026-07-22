# Renovate
Renovate n'est pas déployé localement. `renovate.json` configure le serveur Renovate externe.

Le tag de l'image `ghcr.io/frantche/gitea-backup-restore-process` est suivi par
un manager regex Renovate, car cette image est referencee dans un script, un
template Ansible et des runbooks plutot que dans un fichier Compose standard.
