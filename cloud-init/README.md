# cloud-init

Le cloud-init prépare la base OS, active sshd/docker et configure entièrement `admin-converge` (script + unités systemd + timer).

Aucun secret sensible ne doit apparaître ici.

Le modèle refuse volontairement de démarrer tant que
`RELEASE_REF_REPLACE_ME`, `ARCH_PACKAGE_SNAPSHOT_REPLACE_ME`,
`QUALIFICATION_MANIFEST_URL_REPLACE_ME` et
`QUALIFICATION_MANIFEST_SHA256_REPLACE_ME` ne sont pas remplacés par le tag,
le snapshot Arch et le manifeste de qualification publiés. La valeur
`main` et le snapshot `live` sont réservés au canal de développement et à la CI.
Le renderer CI exige en plus `CI_ALLOW_LIVE_ARCH_MIRROR=true`; le modèle de
production conserve toujours le mode `qualified`. Le bootstrap enregistre le
snapshot réellement utilisé dans `/etc/admin-node/package-snapshot` après
l'installation des paquets. Un SHA direct est refusé hors du renderer CI ; une
installation production exige un tag annoté et un manifeste publié qui lie ce
tag au commit résolu.
