# admin-node CLI

`admin-node` est le binaire Go d'exploitation du noeud admin. Les scripts Bash migrés (`backup.sh`, `restore.sh`, `validate-*.sh`) sont maintenant des wrappers stricts vers ce binaire. Si `bin/admin-node` est absent, ils echouent avec un message demandant `make build-admin-node`.

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
bin/admin-node validate all --output json
```

Les statuts possibles sont `ok`, `warn`, `fail` et `skipped`. Le code de sortie vaut `1` si au moins un check est en `fail`.

## Backup

```bash
bin/admin-node backup list
bin/admin-node backup run
bin/admin-node backup run --include-images
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
