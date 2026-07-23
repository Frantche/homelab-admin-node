# Tests

Les controles rapides couvrent Go, Ansible, les scripts shell et les contrats
OIDC :

```bash
go test -race ./...
make lint
make test-oidc-contracts
```

La CI d'integration contient deux parcours bloquants.

`bootstrap-candidate` installe le SHA candidat depuis une image Arch vierge et
valide le bootstrap, les exemples, les API, l'OIDC navigateur, l'observabilite,
le reboot et le durcissement.

`main-to-candidate-disaster-recovery` deploie d'abord le SHA `main`, cree un
backup Restic dans Garage, passe au SHA candidat avec la configuration `main`,
detruit la VM, puis restaure ce backup sur une seconde VM vierge. Il tourne
ensuite les secrets clients OIDC, les mots de passe administrateurs et les mots
de passe PostgreSQL.

Les mots de passe declares dans `keycloak_config.users` ne sont jamais tournes
par ce parcours. Le test echoue s'ils changent.

Execution locale complete :

```bash
make test-ci-full
```

Ce test necessite Docker, QEMU, `cloud-localds`, `socat`, `curl`, `jq` et un
acces Internet. Sans KVM, les deux VM sont executees en emulation et le parcours
peut durer plusieurs heures.
