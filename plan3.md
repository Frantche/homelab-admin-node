# Plan3 - Build idempotent du binaire admin-node

Regle de suivi: cocher un item uniquement quand le code est implemente, teste, et corrige si necessaire. En cas d'echec, corriger avant de continuer. Ce fichier devient la source de verite pour reprendre avec "continue le plan3".

## 0. Etat initial

- [x] Verifier etre sur la branche `feature/admin-node-go-cli`.
- [x] Verifier `git status --short --branch` et identifier les changements non lies.
- [x] Verifier que `plan.md` et `plan2.md` existent.
- [x] Confirmer que le role Ansible `base` compile actuellement `bin/admin-node` a chaque converge.
- [x] Confirmer que `admin-converge.sh` met a jour le repo via `git pull --ff-only` avant Ansible.
- [x] Confirmer que le module Go n'a pas de dependance externe non verrouillee.

## Decisions d'implementation

- [x] Garder le build local sur le noeud admin comme strategie v1.
- [x] Ne pas introduire d'artefacts CI signes dans cette etape.
- [x] Rendre le build local conditionnel via fingerprint des sources Go.
- [x] Garder `bin/` hors Git.
- [x] Remplacer le binaire de maniere atomique uniquement apres build reussi.
- [x] Faire appeler la meme logique par Ansible et par `make build-admin-node`.

## 1. Script de build idempotent

- [x] Ajouter `scripts/build-admin-node.sh`.
- [x] Calculer un hash stable a partir de `cmd/**/*.go`, `internal/**/*.go`, `go.mod` et `go.sum` si present.
- [x] Stocker le hash courant dans `bin/admin-node.source.sha256`.
- [x] Ne pas recompiler si `bin/admin-node` existe et si le hash stocke correspond au hash courant.
- [x] Compiler vers un fichier temporaire `bin/admin-node.tmp`.
- [x] Verifier que le binaire temporaire demarre avec une commande d'aide.
- [x] Remplacer atomiquement `bin/admin-node` par le binaire temporaire.
- [x] Mettre a jour `bin/admin-node.source.sha256` uniquement apres remplacement reussi.
- [x] S'assurer que le script echoue clairement si `go` est absent.

## 2. Integration Makefile et Ansible

- [x] Modifier `make build-admin-node` pour appeler `scripts/build-admin-node.sh`.
- [x] Modifier le role Ansible `base` pour appeler `scripts/build-admin-node.sh` au lieu de `go build` directement.
- [x] Rendre la tache Ansible non changed quand le binaire est deja a jour.
- [x] Garder le build apres la synchronisation du repo vers `/opt/homelab-admin-node`.
- [x] Verifier que les wrappers stricts continuent de trouver `bin/admin-node`.

## 3. Robustesse et securite

- [x] Verifier qu'un build echoue ne remplace pas le binaire existant.
- [x] Verifier que le hash n'est pas mis a jour si le build echoue.
- [x] Verifier que le script ne telecharge pas de dependances dans l'etat actuel du projet.
- [x] Documenter que toute future dependance Go devra etre verrouillee par `go.sum`.
- [x] Documenter que les artefacts CI signes restent une evolution possible, non implementee ici.

## 4. Documentation

- [x] Mettre a jour `docs/admin-node.md` avec le build local conditionnel.
- [x] Mettre a jour `docs/testing.md` avec la nouvelle commande de build partagee.
- [x] Mettre a jour `docs/config-repo.md` ou `README.md` pour expliquer le flux `git pull` puis build idempotent.
- [x] Mentionner que `bin/admin-node` et son hash de build restent non versionnes.

## 5. Validation finale

- [x] Lancer `make build-admin-node` une premiere fois et verifier que le binaire est cree.
- [x] Relancer `make build-admin-node` sans changement Go et verifier qu'il ne recompile pas.
- [x] Modifier temporairement une source Go ou simuler un hash obsolete, puis verifier qu'un rebuild est declenche.
- [x] Verifier que `go test ./...` passe.
- [x] Verifier `ansible-playbook -i ansible/inventory.ini ansible/site.yml --syntax-check`.
- [x] Lancer `sudo make test-ci-fast`.
- [x] Lancer `sudo make test-ci-full` si l'environnement le permet.
- [x] Verifier `git status --short --branch`.
- [x] Mettre a jour cette section avec les resultats exacts.
- [x] Cocher uniquement les validations reellement passees.

Resultats verifies:

- `bash -n scripts/build-admin-node.sh`: OK.
- `GO_BIN=/tmp/admin-node-missing-go scripts/build-admin-node.sh`: OK, erreur claire et code 127.
- `make build-admin-node`: OK, premier passage `changed=true`.
- `make build-admin-node`: OK, second passage `changed=false`.
- Hash obsolete simule dans `bin/admin-node.source.sha256`: OK, rebuild declenche avec `changed=true`.
- Faux compilateur Go en echec: OK, `bin/admin-node` inchange et hash non mis a jour.
- `go list -m all`: OK, seul le module courant est liste.
- `CI_MOCK_PIHOLE=true ./scripts/validate-dns.sh`: OK, wrapper strict trouve `bin/admin-node`.
- `go test ./...`: OK.
- `ansible-playbook -i ansible/inventory.ini ansible/site.yml --syntax-check`: OK.
- `sudo make test-ci-fast`: OK, scenario `fresh-branch` passe.
- `sudo make test-ci-full`: OK, scenarios `fresh-branch`, `upgrade-main-to-branch` et `restore-main-backup-with-branch` passent.
- `git status --short --branch`: OK, branche `feature/admin-node-go-cli`; changements attendus non commites.

## Risques a suivre

- [x] Le build local garde Go installe sur le noeud admin; accepte pour v1 car le flux reste simple et autonome.
- [x] Le test idempotent doit prouver que les convergences periodiques ne recompilent pas inutilement.
- [x] L'absence actuelle de dependances externes rend le build local acceptable; ce point devra etre reevalue si `go.sum` apparait.
- [ ] Les artefacts CI signes pourraient devenir preferables si le projet a besoin de releases reproductibles multi-plateformes.
