# homelab-admin-node

Repository complet pour construire et opérer un nœud d'administration homelab (Arch Linux + cloud-init + Ansible Pull + Docker Compose).

## 1. Objectif du projet
Ce dépôt reconstruit une VM `admin-01` indépendante de Talos/Kubernetes pour gérer Traefik, Keycloak, OpenBao, Harbor, Cloudflare Tunnel, backups et restauration.

## 2. Architecture
LAN -> Pi-hole -> admin-01 -> Traefik -> Keycloak/OpenBao/Harbor/dashboard.
Internet -> Cloudflare -> cloudflared -> Traefik -> mêmes services.

## 3. Pourquoi cloud-init est volontairement incomplet
Le cloud-init prépare uniquement la base système et lance une convergence. Les secrets restent absents pour garantir le mode `locked` par défaut.

## 4. Principe du secret zéro
Le secret zéro est la clé privée age installée manuellement dans `/etc/sops/age/keys.txt` (0400 root:root).

## 5. Première installation
1. Provisionner la VM avec `cloud-init/admin-01.user-data.yaml`.
2. Vérifier `/etc/admin-node/mode` = `locked`.
3. Injecter la clé age via `scripts/unlock.sh`.
4. Passer en mode `init` via `scripts/set-mode.sh init`.
5. Lancer `scripts/admin-converge.sh`.

## 6. Injection manuelle de la clé age
Utiliser `sudo ./scripts/unlock.sh /path/to/age-key.txt`.

## 7. Mode init
Déploie les stacks, initialise OpenBao, configure DNS/Tunnel (ou mocks), valide APIs, exécute un premier backup.

## 8. Initialisation OpenBao
Lancer `scripts/openbao-init.sh` puis enregistrer les clés d'unseal dans `secrets/openbao-unseal.sops.yaml`.

## 9. Ajout des unseal keys dans SOPS
Utiliser le format multi-keyset documenté dans `secrets/openbao-unseal.sops.yaml.example`.

## 10. Passage en mode normal
`sudo ./scripts/set-mode.sh normal && sudo ./scripts/admin-converge.sh`.

## 11. Configuration Traefik
Voir `docs/traefik.md` et `stacks/traefik`.

## 12. Configuration Cloudflare Tunnel
Voir `docs/cloudflare-tunnel.md` et `stacks/cloudflared/config.yml.tpl`.

## 13. Configuration Pi-hole DNS local
Voir `docs/pihole-dns.md` et rôle `ansible/roles/pihole_dns`.

## 13bis. Configuration SSO et rôles applicatifs
- Keycloak (realm/roles/users/clients): rôle `ansible/roles/keycloak_config` via `keycloak_config.*`.
- OpenBao (secret engines + auth OIDC): rôle `ansible/roles/openbao_config` via `openbao_config.*`.
- Harbor (OIDC + registry mirrors proxy-cache): rôle `ansible/roles/harbor_config` via `harbor_config.*`.

## 14. Backup
`scripts/backup.sh` vérifie santé APIs/DNS/tunnel puis applique rétention locale + `restic forget --keep-last 3 --prune`.

## 15. Restore
`scripts/restore.sh` restaure fichiers + services, valide, bascule `mode` vers `normal` ou `restore_failed`.

## 16. Validation API
`scripts/validate-apis.sh`, `scripts/validate-dns.sh`, `scripts/validate-cloudflare-tunnel.sh`.

## 17. Fonctionnement Renovate
Renovate externe uniquement. Fichier local: `renovate.json`.

## 18. Scénarios CI
`fresh-branch`, `upgrade-main-to-branch`, `restore-main-backup-with-branch` (voir `ci/scenarios`).

## 19. Procédure disaster recovery
Voir `docs/restore-runbook.md`.

## 20. Limitations connues
Mocks CI par défaut pour Pi-hole et Cloudflare Tunnel si infra absente.

## 21. Checklist finale
- cloud-init minimal sans secret
- modes `locked/init/normal/restore/restore_failed`
- stacks Docker Compose et validations API/DNS/Tunnel
- backup + restore + rétention 3 snapshots

## 22. Dépôt de configuration séparé (config repo)
Gérez votre configuration et vos secrets dans un dépôt Git **privé** séparé.
Voir `docs/config-repo.md` pour la structure, la mise en place et l'utilisation avec `admin-converge.sh`.

```bash
export ADMIN_REPO_URL="ssh://git@github.com/Frantche/homelab-admin-node.git"
export CONFIG_REPO_URL="ssh://git@github.com/VOUS/homelab-admin-node-config.git"
sudo -E ./scripts/admin-converge.sh
```

## Commandes
```bash
make lint
make ansible-syntax
make shellcheck
make validate
make validate-apis
make validate-dns
make validate-cloudflare-tunnel
make test-ci-fast
```
