# cloud-init

Le cloud-init prépare la base OS, active sshd/docker et configure entièrement `admin-converge` (script + unités systemd + timer).

Aucun secret sensible ne doit apparaître ici.

Le modèle refuse volontairement de démarrer tant que
`RELEASE_REF_REPLACE_ME` et `ARCH_PACKAGE_SNAPSHOT_REPLACE_ME` ne sont pas
remplacés par le tag ou commit immuable et le snapshot Arch qualifiés. La valeur
`main` et le snapshot `live` sont réservés au canal de développement et à la CI.
