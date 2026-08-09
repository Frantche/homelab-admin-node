# cloud-init

Le cloud-init prépare la base OS, active sshd/docker et configure entièrement `admin-converge` (script + unités systemd + timer).

Aucun secret sensible ne doit apparaître ici.

Le modèle refuse volontairement de démarrer tant que
`RELEASE_REF_REPLACE_ME`, `QUALIFICATION_MANIFEST_URL_REPLACE_ME` et
`QUALIFICATION_MANIFEST_SHA256_REPLACE_ME` ne sont pas remplacés par le tag et
le manifeste de qualification publiés. La valeur `main` est réservée au canal
de développement et à la CI. Un SHA direct est refusé hors du renderer CI ;
une installation production exige un tag annoté et un manifeste publié qui lie
ce tag au commit résolu.
