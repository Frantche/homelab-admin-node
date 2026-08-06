# Tests

Les controles rapides couvrent Go, Ansible, les scripts shell et les contrats
OIDC :

```bash
make ci-continuous
```

La CI d'integration contient deux parcours bloquants.

`bootstrap` installe le SHA candidat depuis une image Arch vierge et
valide le bootstrap, les exemples, les API, l'OIDC navigateur, l'observabilite,
le reboot et le durcissement.

`main-to-candidate-disaster-recovery` deploie d'abord le SHA `main`, cree un
backup Restic dans Garage, passe au SHA candidat avec la configuration `main`,
detruit la VM, puis restaure ce backup sur une seconde VM vierge. La matrice
execute le parcours historique `standard` et le parcours `offline-images`.
Ce dernier archive les images Docker et le commit Git, puis bloque les
registries sur la VM de restauration. Les deux variantes tournent ensuite les
secrets clients OIDC, les mots de passe administrateurs et les mots de passe
PostgreSQL.

Les variantes DR sont exclues des `pull_request` et des `push`. Elles sont
declenchables manuellement depuis GitHub Actions avec le scope
`disaster-recovery`, selectionne par defaut, et sont planifiees chaque dimanche
a `03:00 UTC`. Le cron ne lance pas le job `bootstrap` separe, mais le parcours
DR execute le bootstrap avant la mise a niveau. Les scopes manuels `bootstrap`
et `all` restent disponibles.

Les mots de passe declares dans `keycloak_config.users` ne sont jamais tournes
par ce parcours. Le test echoue s'ils changent.

Execution locale complete :

```bash
make ci-full
```

La cible execute les variantes `standard` et `offline-images`. Pour n'en lancer
qu'une :

```bash
DR_VARIANTS=standard make ci-full
```

Ce test necessite Docker, QEMU, `cloud-localds`, `socat`, `curl`, `jq` et un
acces Internet. Sans KVM, les deux VM sont executees en emulation et le parcours
peut durer plusieurs heures.
