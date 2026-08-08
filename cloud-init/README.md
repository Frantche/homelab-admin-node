# cloud-init

Le cloud-init prépare la base OS, active sshd/docker et configure entièrement `admin-converge` (script + unités systemd + timer).

Aucun secret sensible ne doit apparaître ici.

Le modèle refuse volontairement de démarrer tant que
`RELEASE_REF_REPLACE_ME` n'a pas été remplacé par un tag immuable ou un commit
complet. La valeur `main` est réservée au canal de développement.
