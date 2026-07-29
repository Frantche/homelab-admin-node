# Durcissement des conteneurs

Tous les services Compose suppriment les capacités Linux par défaut, activent
`no-new-privileges`, déclarent un healthcheck et limitent les processus, la
mémoire et le CPU. Le root filesystem est en lecture seule sauf lorsque
l'entrypoint fournisseur initialise plusieurs chemins avant de perdre ses
privilèges. Les volumes sans suffixe `:ro` sont les seules écritures
persistantes attendues.

| Service | Utilisateur runtime | Capacités ajoutées | Écritures et exception |
| --- | --- | --- | --- |
| `cloudflared` | `65532:65532` | aucune | rootfs RO, `/tmp` tmpfs |
| `gitea-db` | entrypoint root puis postgres | `CHOWN`, `DAC_OVERRIDE`, `SETGID`, `SETUID` | rootfs RO, données PostgreSQL et tmpfs |
| `gitea` | entrypoint root puis `1000:1000` | `CHOWN`, `DAC_OVERRIDE`, `SETGID`, `SETUID` | rootfs RW requis par l'entrypoint Gitea, `/data` persistant |
| `keycloak-db` | entrypoint root puis postgres | `CHOWN`, `DAC_OVERRIDE`, `SETGID`, `SETUID` | rootfs RO, données PostgreSQL et tmpfs |
| `keycloak` | `1000:0` | aucune | rootfs RW requis par la première augmentation Quarkus de l'image fournisseur, tmpfs temporaires |
| `harbor-log` | entrypoint Harbor | `CHOWN`, `DAC_OVERRIDE`, `SETGID`, `SETUID` | rootfs RW fournisseur, logs persistants |
| `harbor-db` | entrypoint root puis postgres | `CHOWN`, `DAC_OVERRIDE`, `SETGID`, `SETUID` | rootfs RW fournisseur, données PostgreSQL |
| `harbor-redis` | entrypoint Harbor | `CHOWN`, `SETGID`, `SETUID` | rootfs RW fournisseur, données Redis |
| `harbor-registry` | entrypoint Harbor | `CHOWN`, `SETGID`, `SETUID` | rootfs RW fournisseur, stockage registry |
| `harbor-registryctl` | entrypoint Harbor | `CHOWN`, `SETGID`, `SETUID` | rootfs RW fournisseur, stockage registry |
| `harbor-core` | entrypoint Harbor puis UID 10000 | `SETGID`, `SETUID` | rootfs RW fournisseur, `/data` |
| `harbor-portal` | entrypoint Harbor | `CHOWN`, `SETGID`, `SETUID`, `NET_BIND_SERVICE` | rootfs RW fournisseur |
| `harbor-jobservice` | entrypoint Harbor | `CHOWN`, `SETGID`, `SETUID` | rootfs RW fournisseur, journaux de jobs |
| `harbor-trivy` | entrypoint Harbor | aucune | rootfs RW fournisseur, caches et rapports |
| `harbor-exporter` | entrypoint Harbor | aucune | rootfs RW fournisseur |
| `harbor-nginx` | entrypoint Harbor | `CHOWN`, `SETGID`, `SETUID`, `NET_BIND_SERVICE` | rootfs RW fournisseur |
| `otel-mock-backend` | root, CI uniquement | aucune | rootfs RO, état mock monté et `/tmp` tmpfs |
| `otel-collector` | `10001:10001` | aucune | rootfs RO et `/tmp` tmpfs |
| `openbao` | `100:1000` (`openbao`) | aucune | rootfs RO, `/tmp` en tmpfs et scratch snapshot dédié en `0700`, stockage Raft/fichier et certificats internes en lecture seule |
| `traefik` | entrypoint fournisseur | `NET_BIND_SERVICE` | rootfs RO, ACME persistant et `/tmp` tmpfs |

Les exceptions sont contrôlées à deux niveaux :

- `ci/test-container-hardening.py` vérifie toutes les définitions Compose ;
- `scripts/validate-container-hardening.sh` inspecte la configuration Docker
  effective pendant le scénario bootstrap.

Les snapshots OpenBao transitent par
`/srv/admin/backups/openbao-scratch`, monté uniquement dans le conteneur sous
`/openbao/snapshot`. Le fichier est créé avec un umask restrictif, copié dans
l'artefact de sauvegarde en `0600`, puis supprimé après chaque sauvegarde ou
restauration. Ce scratch n'est pas une donnée applicative et n'est pas inclus
séparément dans Restic.
