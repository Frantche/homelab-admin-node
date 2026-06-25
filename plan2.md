# Plan2 - Finalisation CLI Go admin-node

Regle de suivi: cocher un item uniquement quand le code est implemente, teste, et corrige si necessaire. En cas d'echec CI, corriger avant de continuer. Ce fichier devient la source de verite pour reprendre avec "continue le plan".

## 0. Etat initial

- [x] Verifier etre sur la branche `feature/admin-node-go-cli`.
- [x] Verifier `git status` et identifier les changements non lies.
- [x] Verifier que `plan.md` existe et conserver son historique de progression.
- [x] Verifier que `make build-admin-node` fonctionne.
- [x] Verifier que `go test ./...` fonctionne.
- [x] Verifier que `ansible-playbook -i ansible/inventory.ini ansible/site.yml --syntax-check` fonctionne.

## 1. Validation CLI Go en mode mock

- [x] Construire `bin/admin-node`.
- [x] Verifier `bin/admin-node validate dns --output json` avec `CI_MOCK_PIHOLE=true`.
- [x] Verifier `bin/admin-node validate tunnel --output json` avec `CI_MOCK_CLOUDFLARE_TUNNEL=true`.
- [x] Ajouter ou ajuster un mode CI/mock permettant de lancer `admin-node validate all --output json` sans services externes non initialises.
- [x] Verifier `admin-node validate all --output json` en mode CI/mock.
- [x] Corriger toute erreur de statut, JSON ou exit code avant de continuer.

## 2. Test offline images avec image legere reelle

- [x] Ajouter un test CI cible pour `--include-images` avec une image legere reelle, par defaut `busybox:latest`.
- [x] Puller l'image de test si absente.
- [x] Creer un compose minimal de test referencant cette image.
- [x] Executer `admin-node backup run --include-images` dans un environnement isole ou mocke.
- [x] Verifier que `offline-images.tar` est cree et non vide.
- [x] Supprimer l'image locale de test.
- [x] Executer le chemin restore offline.
- [x] Verifier que `docker load -i offline-images.tar` restaure l'image.
- [x] Verifier qu'aucun pull reseau n'est necessaire apres le `docker load`.

## 3. Validation restore SQL et OpenBao en integration

- [x] Lancer `make test-ci-fast`.
- [x] Si `make test-ci-fast` echoue, lire le premier echec pertinent et corriger avant toute suite.
- [x] Verifier explicitement que le restore Keycloak reimporte `keycloak.sql`.
- [x] Verifier explicitement que le restore Gitea reimporte `gitea.sql`.
- [x] Verifier explicitement que le restore Gitea conserve repo/issue sentinelle.
- [x] Verifier explicitement que le restore OpenBao restaure `openbao.snap`.
- [x] Verifier que le mode repasse a `normal` apres restore reussi.
- [x] Verifier que le mode passe a `restore_failed` en cas d'echec controle.

## 4. CI full lifecycle

- [x] Lancer `make test-ci-full`.
- [x] Valider `fresh-branch`.
- [x] Valider `upgrade-main-to-branch`.
- [x] Valider `restore-main-backup-with-branch`.
- [x] Corriger tout ecart introduit par les wrappers Go.
- [x] Relancer le scenario echoue jusqu'a succes.
- [x] Relancer `make test-ci-full` complet apres corrections.

## 5. Suppression des fallbacks Bash dupliques

- [x] Transformer `scripts/validate-apis.sh` en wrapper strict vers `bin/admin-node validate apis`.
- [x] Transformer `scripts/validate-gitea-data.sh` en wrapper strict vers `bin/admin-node validate gitea`.
- [x] Transformer `scripts/validate-dns.sh` en wrapper strict vers `bin/admin-node validate dns`.
- [x] Transformer `scripts/validate-cloudflare-tunnel.sh` en wrapper strict vers `bin/admin-node validate tunnel`.
- [x] Transformer `scripts/backup.sh` en wrapper strict vers `bin/admin-node backup run`.
- [x] Transformer `scripts/restore.sh` en wrapper strict vers `bin/admin-node restore run`.
- [x] Garder uniquement les petits scripts non migres ou encore plus lisibles en Bash.
- [x] Verifier que les erreurs sont claires si `bin/admin-node` est absent.
- [x] Mettre a jour docs et commentaires qui mentionnent encore l'ancien fallback Bash.

## 6. Packaging et installation du binaire

- [x] Confirmer que `make build-admin-node` cree `bin/admin-node`.
- [x] Verifier que `bin/` reste ignore par Git.
- [x] Confirmer que cloud-init installe Go.
- [x] Confirmer qu'Ansible installe Go.
- [x] Decider si Ansible doit construire `bin/admin-node` automatiquement pendant le converge.
- [x] Si oui, ajouter l'etape Ansible de build.
- [ ] Si non, documenter explicitement que le binaire doit etre construit avant deploy/sync. Non applicable: choix "oui".

## 7. Documentation finale

- [x] Mettre a jour `docs/admin-node.md` avec le nouveau comportement sans fallback Bash.
- [x] Mettre a jour `docs/backup.md` pour le backup offline avec image legere testee.
- [x] Mettre a jour `docs/restore-runbook.md` pour `admin-node restore run/select`.
- [x] Mettre a jour `docs/testing.md` avec les nouvelles commandes CI.
- [x] Mettre a jour `README.md` si le chemin utilisateur change.

## 8. Validation finale avant merge

- [x] Lancer `make build-admin-node`.
- [x] Lancer `go test ./...`.
- [x] Lancer `ansible-playbook -i ansible/inventory.ini ansible/site.yml --syntax-check`.
- [x] Lancer `make test-restic-config`.
- [x] Lancer le test offline image legere.
- [x] Lancer `make test-ci-fast`.
- [x] Lancer `make test-ci-full`.
- [x] Verifier `git status --short --branch`.
- [x] Mettre a jour cette section avec les resultats exacts.
- [x] Cocher uniquement les validations reellement passees.

Resultats verifies:

- `make build-admin-node`: OK, cree `bin/admin-node`.
- `go test ./...`: OK.
- `ansible-playbook -i ansible/inventory.ini ansible/site.yml --syntax-check`: OK.
- `make test-restic-config`: OK, configuration locale valide; verification runtime SFTP ignoree car `/run/sshd` indisponible dans cet environnement.
- `make test-offline-images`: OK, `offline-images.tar` cree, charge via `docker load`, et image utilisable sans pull reseau.
- `sudo make test-ci-fast`: OK, scenario `fresh-branch` passe avec backup/restore et retour en mode `normal`.
- `sudo make test-ci-full`: OK, scenarios `fresh-branch`, `upgrade-main-to-branch` et `restore-main-backup-with-branch` passes.
- `git status --short --branch`: OK, branche `feature/admin-node-go-cli`; changements attendus non commites pour cette implementation.

## Risques a suivre

- [x] CI full potentiellement longue ou destructive sur `/srv/admin`; lancee volontairement dans l'environnement de validation courant.
- [x] Suppression du fallback Bash acceptable uniquement apres corrections des scenarios CI.
- [x] Go sur le noeud admin augmente la surface d'installation; choix retenu et documente, Ansible construit le binaire pendant le converge.
- [ ] Offline image avec `busybox` valide le mecanisme Docker, pas le poids ni la duree d'export des images Harbor completes.
