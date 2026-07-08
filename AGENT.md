# Guide agent pour ce depot

Ce depot construit et opere le noeud d'administration `admin-01` d'un homelab avec Arch Linux, cloud-init, Ansible, Docker Compose, systemd, SOPS/age, OpenBao, Keycloak, Harbor, Gitea, Traefik, Cloudflare Tunnel, Pi-hole DNS, backup et restore.

L'objectif d'un agent de developpement est de faire des changements petits, verifiables et compatibles avec une machine d'administration qui peut etre reconstruite. La priorite est la securite operationnelle: ne jamais rendre les secrets obligatoires en clair, ne jamais casser le mode `locked`, et ne pas contourner les validations qui protegent le cycle de vie.

## Points d'entree importants

- `README.md`: vue d'ensemble, installation, modes et commandes.
- `ansible/site.yml`: orchestration principale et conditions par mode.
- `ansible/group_vars/all.yml`: valeurs par defaut non secretes.
- `examples/admin-config/`: structure attendue du depot de configuration prive.
- `secrets/*.sops.yaml.example` et `ansible/group_vars/secrets.sops.yaml.example`: exemples uniquement, sans secrets reels.
- `cmd/admin-node` et `internal/`: CLI Go d'exploitation, point d'entree runtime pour converge, backup, restore, validation et OpenBao.
- `ci/run-admin-lifecycle.sh` et `ci/scenarios/`: tests d'integration du cycle de vie.
- `stacks/*/compose.yaml`: definition des services Docker Compose.
- `systemd/`: unites et timers deployes sur la VM.

## Invariants a preserver

- Le mode par defaut doit rester `locked` tant que le secret zero age n'est pas installe dans `/etc/sops/age/keys.txt`.
- Les modes supportes sont `locked`, `init`, `normal`, `restore` et `restore_failed`; toute logique nouvelle doit s'integrer explicitement a ce modele.
- Aucun secret reel ne doit etre ajoute au depot. Utiliser des exemples factices dans `*.example`, `ci/ci-extra-vars.json` ou des mocks CI.
- Les secrets de configuration utilisateur appartiennent au config repo prive sous `/etc/admin-config/homelab-node-admin-config`, pas a ce depot.
- Les taches qui manipulent des secrets doivent utiliser `no_log: true` lorsque des valeurs sensibles peuvent apparaitre.
- Le mode CI peut fournir des valeurs factices, mais hors CI les secrets requis doivent echouer explicitement avec un message clair.
- `admin-node converge run` doit rester idempotent, verbeux sur les erreurs et compatible avec un `git pull --ff-only`.
- Les backups ne doivent pas s'executer en mode `locked`.
- La restauration doit rester defensive: valider les services apres restore et basculer vers un etat explicite en cas d'echec.

## Style Ansible

- Utiliser les modules namespaced `ansible.builtin.*` quand ils existent.
- Garder les roles petits et orientes service ou responsabilite: `*_config` pour la configuration applicative, role service pour le deploiement.
- Preferer des variables par defaut non secretes dans `defaults/main.yml` ou `ansible/group_vars/all.yml`.
- Placer les secrets attendus dans les exemples SOPS et documenter leur chemin de variable.
- Toute nouvelle integration doit etre desactivable via une variable `enabled` si elle depend d'un service externe ou d'un secret.
- Ajouter des assertions Ansible pour les preconditions importantes au lieu de laisser echouer une commande opaque.
- Ne pas masquer les erreurs avec `ignore_errors` sauf si le comportement degrade est documente et teste.
- Conserver l'idempotence: declarer `changed_when: false` pour les commandes de lecture/validation.

## Style shell

- Tous les scripts shell doivent commencer par:

```bash
#!/usr/bin/env bash
set -euo pipefail
```

- Utiliser des chemins explicites et des variables avec valeurs par defaut quand un script peut etre lance en CI ou sur la VM.
- Ecrire les erreurs operationnelles sur stderr quand elles representent un refus ou un echec.
- Eviter les suppressions larges; si `rm -rf` est necessaire, borner le chemin et construire la liste cible explicitement.
- Ne pas supposer qu'un conteneur existe: verifier les conteneurs optionnels avant `docker exec`.
- Garder les scripts utilisables hors GitHub Actions si possible; les specialisations CI doivent etre controlees par variables d'environnement.
- Garder la logique Python explicite: ne pas integrer de heredocs Python dans les scripts Bash. Placer la logique Python dans des fichiers `.py` dedies et les appeler depuis les scripts d'orchestration Bash.

## Docker Compose et stacks

- Chaque stack vit sous `stacks/<service>/` et doit rester deployable par Ansible.
- Ne pas introduire de ports publics directs sans passer par Traefik, sauf besoin technique documente.
- Les hostnames publics et locaux doivent provenir de `service_domains` ou des variables de configuration, pas de constantes dispersees.
- Les fichiers de config rendus ou copies par Ansible doivent avoir des permissions explicites quand ils contiennent des tokens, mots de passe ou cles.

## DNS, tunnel et OIDC

- Pi-hole et Cloudflare Tunnel ont des mocks CI; conserver ces chemins pour que les tests restent executables sans infra externe.
- Les clients OIDC partages doivent passer par `oidc_clients.*` afin d'eviter des secrets divergents entre Keycloak, Harbor, OpenBao et Gitea.
- Les changements OIDC doivent couvrir au minimum les contrats existants: scopes, claim `groups`, redirect URIs, secret partage et comportement hors CI.

## Tests et validations

Executer le niveau de test adapte au risque:

```bash
make lint
make ansible-syntax
make shellcheck
make validate
make test-oidc-contracts
make test-ci-fast
make test-ci-full
```

- Pour un changement shell: lancer au moins `make shellcheck`.
- Pour un changement Ansible: lancer au moins `make ansible-syntax`.
- Pour DNS, Cloudflare Tunnel ou validations API: lancer `make validate` ou la cible precise.
- Pour OIDC/Keycloak/Harbor/OpenBao/Gitea: lancer `make test-oidc-contracts` et envisager `make test-ci-fast`.
- Pour backup, restore, cloud-init, systemd ou orchestration globale: envisager `make test-ci-full`.

Si une commande ne peut pas etre lancee localement faute de dependance ou de contexte VM, le signaler clairement dans le compte rendu avec la raison.

## Documentation attendue

- Mettre a jour `README.md` pour les changements de parcours utilisateur ou de commande.
- Mettre a jour `docs/*.md` pour les changements operationnels, secrets, restore, DNS, tunnel, OIDC ou CI.
- Mettre a jour les exemples dans `examples/admin-config/` et `secrets/*.example` quand une variable utilisateur change.
- Garder la documentation en francais, concise et executable.

## A eviter

- Ajouter un secret, token, mot de passe ou cle privee reel dans le depot.
- Casser la compatibilite avec une VM Arch Linux fraiche.
- Introduire une dependance lourde sans l'ajouter au cloud-init, a la doc et au flux CI concerne.
- Changer les chemins systeme critiques sans migration documentee: `/opt/homelab-admin-node`, `/srv/admin`, `/etc/admin-node`, `/etc/admin-config`, `/etc/sops/age/keys.txt`.
- Modifier backup/restore, OpenBao unseal/init ou `admin-node converge run` sans validation explicite.
- Supprimer les mocks CI pour Pi-hole ou Cloudflare Tunnel.
- Faire des refactors larges non lies au changement demande.

## Processus recommande pour un agent

1. Lire les fichiers proches du changement avant de modifier.
2. Identifier le mode impacte: `locked`, `init`, `normal`, `restore` ou CI.
3. Preserver les exemples et les chemins du config repo prive.
4. Faire une modification limitee et idempotente.
5. Ajouter ou ajuster la validation la plus proche.
6. Lancer les commandes de test pertinentes.
7. Resumer les fichiers modifies, les validations lancees et les limites restantes.
