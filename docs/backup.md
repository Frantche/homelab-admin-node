# Backup

`bin/admin-node backup run` cree une sauvegarde locale sous `/srv/admin/backups/local`, puis exécute les sauvegardes restic depuis le même binaire.

Ansible construit `bin/admin-node` pendant le converge. En execution manuelle depuis le depot, lancer `make build-admin-node` si le binaire est absent.

## Restic

La configuration Ansible genere `/srv/admin/env/backup.env` depuis `backup.*`.

Format historique, toujours supporte :

```yaml
backup:
  restic_repository: "/srv/admin/backups/restic"
  restic_password: "CHANGE_ME"
  restic_forget_args: "--keep-last 3 --prune"
```

Format multi-destinations :

```yaml
backup:
  restic_default_forget_args: "--keep-daily 7 --keep-weekly 4 --keep-monthly 12 --prune"
  restic_init_repositories: false
  restic_require_secure_repositories: true
  restic_repositories:
    - name: local
      repository: "/srv/admin/backups/restic"
      password: "CHANGE_ME"
      forget_args: "--keep-last 3 --prune"

    - name: sftp
      repository: "sftp:backup-admin:/srv/restic/admin-node"
      password: "CHANGE_ME"

    - name: s3
      repository: "s3:https://s3.example.com/admin-node-restic"
      password: "CHANGE_ME"
      env:
        AWS_ACCESS_KEY_ID: "CHANGE_ME"
        AWS_SECRET_ACCESS_KEY: "CHANGE_ME"
        AWS_DEFAULT_REGION: "us-east-1"
```

Les connexions non chiffrees sont refusees par defaut. Les repositories `ftp:`, `rest:http://` et `s3:http://` echouent si `restic_require_secure_repositories` vaut `true`.

`rclone:` est aussi refuse par defaut, car le helper ne peut pas verifier si le remote rclone sous-jacent est chiffre. Il faut auditer le remote avant de desactiver explicitement `restic_require_secure_repositories`.

## Retention

Chaque destination peut definir `forget_args`. Sinon, `restic_default_forget_args` est utilise, avec `--keep-last 3 --prune` par defaut.

Exemples :

```yaml
forget_args: "--keep-last 3 --prune"
forget_args: "--keep-daily 7 --keep-weekly 4 --keep-monthly 12 --prune"
forget_args: "--keep-within-daily 7d --keep-within-weekly 1m --keep-within-monthly 1y --prune"
forget_args: "none"
```

`none` lance le backup sans `restic forget`.

## CI

`make test-restic-config` valide :

- un repository restic local ;
- un repository SFTP local via `sshd` loopback, cle SSH dediee et mot de passe desactive ;
- le refus d'un repository `ftp://` non chiffre.

`make test-offline-images` valide le mecanisme offline avec une image Docker legere reelle (`busybox:latest` par defaut) :

- pull de l'image ;
- backup `--include-images` ;
- creation d'un `offline-images.tar` non vide ;
- suppression locale de l'image ;
- restore avec `docker load` ;
- verification que l'image est de nouveau disponible sans pull.

Pour produire une sauvegarde restaurable hors ligne :

```bash
bin/admin-node backup run --include-images
```

Le backup contient alors `offline-images.tar`. Pendant le restore, `bin/admin-node restore run` charge ce tar avant de redemarrer les stacks, ce qui permet de restaurer les images deja exportees sans pull reseau.
