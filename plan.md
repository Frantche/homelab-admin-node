# Plan - CLI Go admin-node

Regle de suivi: cocher un item uniquement quand l'implementation est terminee, testee, et corrigee si necessaire. Si une etape echoue, ne pas avancer avant correction. Ce fichier est la source de verite pour reprendre le travail avec "continue le plan".

## 0. Preparation

- [x] Verifier l'etat Git et identifier les changements non lies a preserver.
- [x] Confirmer que les scripts existants continuent de servir d'interface stable pour systemd, Ansible et CI.
- [x] Documenter dans ce fichier toute decision nouvelle prise pendant l'implementation.

## Decisions d'implementation

- [x] Conserver les scripts Bash comme interface publique stable et les faire deleguer a `bin/admin-node` seulement quand le binaire existe.
- [x] Garder `bin/` hors Git; construire le binaire avec `make build-admin-node`.
- [x] Installer Go via cloud-init/Ansible pour permettre le build local du binaire sur le noeud admin.
- [x] Utiliser des tests unitaires avec faux `docker` pour valider backup/restore sans toucher Docker reel.

## 1. Fondations Go CLI

- [x] Ajouter `go.mod` et la structure minimale du binaire `admin-node`.
- [x] Ajouter une commande racine avec aide CLI.
- [x] Ajouter les sous-commandes vides: `validate`, `backup`, `restore`.
- [x] Ajouter une couche d'execution de commandes externes mockable.
- [x] Ajouter une couche de config pour chemins par defaut, variables d'environnement et mode CI.
- [x] Ajouter les premiers tests unitaires du parsing de config.
- [x] Verifier avec `go test ./...`.

## 2. Validation API en Go

- [x] Implementer le modele `CheckResult` avec statuts `ok`, `warn`, `fail`, `skipped`.
- [x] Implementer `admin-node validate apis`.
- [x] Implementer `admin-node validate all`.
- [x] Ajouter la sortie texte lisible.
- [x] Ajouter `--output json`.
- [x] Migrer la validation Keycloak.
- [x] Migrer la validation Harbor.
- [x] Migrer la validation Traefik.
- [x] Migrer la validation Gitea API.
- [x] Migrer la verification repo/issue sentinelle Gitea.
- [x] Encapsuler la reparation admin Gitea via `docker exec`.
- [x] Migrer ou encapsuler la validation OpenBao.
- [x] Garder `scripts/validate-apis.sh` comme wrapper compatible.
- [x] Garder `scripts/validate-gitea-data.sh` comme wrapper compatible ou le remplacer par un wrapper Go.
- [x] Verifier avec `go test ./...`.
- [ ] Verifier avec `admin-node validate all --output json` en mode CI/mock.
- [ ] Verifier que les scenarios existants continuent d'appeler la validation correctement.

## 3. DNS et Tunnel

- [x] Implementer `admin-node validate dns`.
- [x] Reprendre le comportement `CI_MOCK_PIHOLE=true`.
- [x] Implementer `admin-node validate tunnel`.
- [x] Reprendre le comportement `CI_MOCK_CLOUDFLARE_TUNNEL=true`.
- [x] Reprendre le comportement `SKIP_PUBLIC_URL_VALIDATION=true`.
- [x] Garder `scripts/validate-dns.sh` et `scripts/validate-cloudflare-tunnel.sh` comme wrappers.
- [x] Verifier avec tests unitaires Go.
- [ ] Verifier avec les scenarios CI existants.

## 4. Manifestes et Listing Backups

- [x] Definir le format `manifest.json`.
- [x] Ajouter generation de manifeste sans changer encore le restore.
- [x] Ajouter lecture des backups existants sans manifeste.
- [x] Implementer `admin-node backup list`.
- [x] Afficher ID, date, taille, statut manifeste, presence dumps, presence images offline.
- [x] Implementer `latest` comme selection du backup local le plus recent.
- [x] Ajouter tests unitaires de tri et parsing.
- [x] Verifier que les backups existants restent lisibles.

## 5. Backup en Go

- [x] Implementer `admin-node backup run`.
- [x] Refuser l'execution en mode `locked`.
- [x] Appeler la validation avant backup.
- [x] Creer `/srv/admin/backups/local/<timestamp>`.
- [x] Dumper Keycloak Postgres.
- [x] Dumper Gitea Postgres si `gitea-db` existe.
- [x] Creer snapshot OpenBao si token disponible.
- [x] Copier `stacks`, `env`, et `gitea-data`.
- [x] Appeler restic via la configuration existante.
- [x] Appliquer la retention locale.
- [x] Ecrire `manifest.json`.
- [x] Transformer `scripts/backup.sh` en wrapper vers `admin-node backup run`.
- [x] Verifier avec `go test ./...`.
- [x] Verifier avec `make test-restic-config`.
- [ ] Verifier avec scenario CI `fresh-branch`.

## 6. Restore en Go

- [x] Implementer `admin-node restore run --id <backup-id|latest>`.
- [x] Implementer `admin-node restore select`.
- [x] Lire `/etc/admin-node/restore-id` pour compatibilite si aucun `--id` n'est fourni.
- [x] Arreter les stacks dans l'ordre actuel.
- [x] Restaurer `gitea-data`.
- [ ] Reimporter `keycloak.sql`.
- [ ] Reimporter `gitea.sql`.
- [ ] Restaurer `openbao.snap`.
- [x] Redemarrer les stacks.
- [x] Lancer la validation post-restore avec creation sentinelle desactivee.
- [x] Ecrire le mode `normal` en cas de succes.
- [x] Ecrire le mode `restore_failed` en cas d'echec.
- [x] Transformer `scripts/restore.sh` en wrapper vers `admin-node restore run`.
- [x] Verifier avec `go test ./...`.
- [ ] Verifier avec scenario CI `restore-main-backup-with-branch`.

## 7. Offline Docker Images

- [x] Ajouter option `admin-node backup run --include-images`.
- [x] Detecter les images depuis les compose files rendus.
- [x] Preferer `docker compose config --images` quand possible.
- [x] Exporter les images via `docker save -o <backup>/offline-images.tar`.
- [x] Ajouter les images exactes dans `manifest.json`.
- [x] Pendant restore, charger `offline-images.tar` via `docker load` si present.
- [ ] Ajouter un test CI avec image legere.
- [ ] Verifier qu'un restore offline peut demarrer sans pull reseau pour les images incluses.

## 8. Wrappers et Compatibilite

- [x] Verifier que systemd continue d'appeler les scripts existants.
- [x] Verifier que les roles Ansible n'ont pas besoin de changer d'interface.
- [ ] Verifier que les scenarios CI existants restent compatibles.
- [ ] Supprimer uniquement la logique Bash devenue dupliquee quand le chemin Go est stable.
- [x] Garder les petits scripts Bash simples quand ils restent plus lisibles qu'une migration Go.

## 9. Documentation

- [x] Documenter l'installation/build du binaire Go.
- [x] Documenter les commandes `admin-node validate`.
- [x] Documenter les commandes `admin-node backup`.
- [x] Documenter les commandes `admin-node restore`.
- [x] Documenter le format `manifest.json`.
- [x] Documenter le mode offline avec images Docker.
- [x] Mettre a jour les docs existantes sans dupliquer inutilement.

## 10. Validation Finale

- [x] Lancer `go test ./...`.
- [x] Lancer `ansible-playbook -i ansible/inventory.ini ansible/site.yml --syntax-check`.
- [x] Lancer `make test-restic-config`.
- [ ] Lancer `make test-ci-fast`.
- [ ] Lancer `make test-ci-full` si l'environnement le permet.
- [x] Verifier `git status`.
- [x] Resumer les changements et les risques restants.

## Risques restants

- [x] Les scenarios CI complets `fresh-branch`, `restore-main-backup-with-branch` et `test-ci-full` restent a lancer avant merge.
- [x] Les chemins restore SQL Keycloak/Gitea et snapshot OpenBao sont implementes mais pas encore valides par un scenario d'integration complet.
- [x] Le test offline images couvre `docker save`/`docker load` avec faux Docker; il reste a valider avec une image reelle legere en CI.
