# CI — scripts de test du cycle de vie

Ce dossier contient les scripts exécutés **à l'intérieur de la VM Arch Linux** lors des tests d'intégration GitHub Actions (`.github/workflows/admin-node-lifecycle.yml`).

## Fichiers

| Fichier | Rôle |
|---|---|
| `run-admin-lifecycle.sh` | Point d'entrée principal. Reçoit le nom d'un scénario en argument et le délègue au script correspondant dans `scenarios/`. Appelé par le workflow CI et par `make test-ci-fast` / `make test-ci-full`. |
| `setup-ci-env.sh` | Prérequis CI : génère un certificat TLS auto-signé, ajoute les entrées `/etc/hosts` pour les domaines de service, installe les collections Ansible requises. Exécuté en premier dans chaque scénario. |
| `init-openbao-ci.sh` | Initialise et descelle OpenBao dans le conteneur Docker. Stocke le root token dans `/opt/homelab-admin-node/secrets/openbao-root-token` et crée le fichier de secrets SOPS (non chiffré) pour les tests. |
| `create-sentinel-data.sh` | Crée un fichier sentinelle dans `/srv/admin/data/sentinel/value.txt` pour vérifier que la sauvegarde/restauration conserve bien les données. |
| `assertions.sh` | Fonctions d'assertion shell (`assert_file_exists`, `assert_contains`) sourcées par tous les scénarios pour valider les étapes intermédiaires. |
| `test-oidc-contracts.sh` | Vérifie localement les contrats Ansible OIDC/Harbor/Keycloak (mocks CI, échec explicite hors CI, secret partagé, scope `offline_access`, mapper `groups`). |
| `ci-extra-vars.json` | Variables Ansible supplémentaires pour le mode CI : mots de passe factices, token Cloudflare fictif, paramètres Keycloak/Harbor/OpenBao/Backup. Passé via `--extra-vars` à chaque exécution du playbook. |

## Scénarios (`scenarios/`)

| Scénario | Description |
|---|---|
| `fresh-branch.sh` | Déploiement complet depuis zéro : init → Ansible (mode init) → initialisation OpenBao → mode normal → Ansible (mode normal) → données sentinelles → sauvegardes + rétention → restauration. |
| `upgrade-main-to-branch.sh` | Simule une mise à niveau de branche : déploiement initial → sauvegarde → écriture d'un nouveau `git-ref` → re-déploiement Ansible → sauvegarde post-upgrade. |
| `restore-main-backup-with-branch.sh` | Vérifie la restauration à partir d'une sauvegarde existante : déploiement initial → sauvegarde → restauration → re-déploiement Ansible post-restauration. |

## Utilisation locale

Les scénarios s'exécutent dans la VM. Pour un test rapide depuis la machine hôte :

```bash
make test-ci-fast    # scénario fresh-branch uniquement
make test-ci-full    # les 3 scénarios
```

## Flux dans le workflow GitHub Actions

```
GitHub Actions (ubuntu-24.04)
  └─ Lance une VM Arch Linux via QEMU
       └─ cloud-init installe ansible, docker, sops, restic …
            └─ ssh → /opt/homelab-admin-node/ci/run-admin-lifecycle.sh <scenario>
                 ├─ setup-ci-env.sh
                 ├─ ansible-playbook …
                 ├─ init-openbao-ci.sh
                 ├─ create-sentinel-data.sh
                 └─ scripts/backup.sh / restore.sh …
```
