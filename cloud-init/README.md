# cloud-init

Le cloud-init est volontairement incomplet: il prépare uniquement la base OS, active docker, clone le dépôt, copie les exemples vers `/etc/admin-config`, installe `admin-converge`, puis lance une convergence initiale qui reste en `locked` tant que les secrets ne sont pas déposés.

Aucun secret sensible ne doit apparaître ici.
