# admin-node CLI

`admin-node` est le binaire Go d'exploitation du noeud admin. Les entrypoints runtime systemd, Ansible et CI appellent directement ce binaire pour la convergence, les modes, la validation, le backup et le restore.

## Build

```bash
make build-admin-node
```

Le binaire est genere dans `bin/admin-node`. Le repertoire `bin/` n'est pas versionne, y compris le fichier de fingerprint `bin/admin-node.source.sha256`.

Le build passe par `scripts/build-admin-node.sh`. Le script calcule un fingerprint des sources Go (`cmd`, `internal`, `go.mod`, et `go.sum` si present) et ne recompile que si ce fingerprint change. Ansible appelle le meme script pendant le converge, juste apres la synchronisation du depot dans `/opt/homelab-admin-node`.

Le remplacement du binaire est atomique: le nouveau binaire est compile dans un fichier temporaire, verifie avec `--help`, puis deplace vers `bin/admin-node`. Toute future dependance Go doit etre verrouillee par `go.sum`; les artefacts CI signes restent une evolution possible mais ne sont pas utilises dans cette version.

## Validation

```bash
bin/admin-node validate apis
bin/admin-node validate dns
bin/admin-node validate tunnel
bin/admin-node validate hardening
bin/admin-node validate all --output json
```

Les statuts possibles sont `ok`, `warn`, `fail` et `skipped`. Le code de sortie vaut `1` si au moins un check est en `fail`.

## Backup

```bash
bin/admin-node backup list
bin/admin-node backup run
bin/admin-node backup run --include-images
bin/admin-node backup restic
```

`backup run` reprend le comportement historique: validation pre-backup, dumps Keycloak/Gitea, snapshot OpenBao si un token est disponible, copie des fichiers applicatifs, restic et retention locale. Un `manifest.json` est ecrit dans chaque nouveau backup.

Avec `--include-images`, les images Docker detectees depuis les compose files rendus sont exportees dans `offline-images.tar`.

## Restore

```bash
bin/admin-node restore run --id latest
bin/admin-node restore run --id 20260625-120000
bin/admin-node restore select
```

Le restore charge automatiquement `offline-images.tar` s'il est present, restaure les donnees disponibles, redemarre les stacks et lance la validation post-restore avec creation de sentinelle Gitea desactivee.

## Mode, Converge Et Secrets

```bash
bin/admin-node mode set init
bin/admin-node mode set normal
bin/admin-node converge run
bin/admin-node converge run --skip-git-pull --extra-vars "-e admin_ci_disable_auto_converge=true"
bin/admin-node secret install-age-key /path/to/age-key.txt
```

`converge run` prend le lock `/run/admin-converge.lock`, exécute `git pull --ff-only` sauf option contraire, puis lance `ansible-playbook`.

## OpenBao

```bash
bin/admin-node openbao init-if-needed
bin/admin-node openbao unseal
```

Ces commandes remplacent les anciens scripts d'initialisation et d'unseal OpenBao. Elles utilisent `docker exec bao`, `sops` et la clé age locale.
