# cloud-init

Le cloud-init est volontairement incomplet: il prépare uniquement la base OS, active sshd/docker, installe unités `admin-converge`, et lance la première convergence en mode `locked`.

Aucun secret sensible ne doit apparaître ici.
