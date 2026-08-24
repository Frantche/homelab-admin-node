# Renovate
Renovate n'est pas déployé localement. `renovate.json` configure le serveur Renovate externe.

Les versions patch de Go restent dans le groupe avec fusion automatique
`weekly non-major dependency updates`. Les versions mineures de Go utilisent
le groupe distinct `weekly Go minor updates`, sans fusion automatique, afin de
permettre les adaptations et validations manuelles nécessaires.

Le tag de l'image `ghcr.io/frantche/gitea-backup-restore-process` est suivi par
un manager regex Renovate, car cette image est referencee dans un script, un
template Ansible et des runbooks plutot que dans un fichier Compose standard.
