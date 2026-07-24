# CI - parcours utilisateur et reprise

La CI valide les commandes qu'un operateur execute reellement, dans des VM Arch
Linux creees depuis l'image cloud officielle.

## Parcours

`scenarios/bootstrap-user-journey.sh` s'execute dans une VM deja creee par
cloud-init. Il genere un config repo depuis les exemples, traverse les modes
`locked`, `init` et `normal`, puis valide services, OIDC, observabilite,
sauvegarde et restauration locale.

`scenarios/main-to-candidate-disaster-recovery.sh` expose une commande
idempotente par etape GitHub Actions. Le job :

1. cree une VM source avec le SHA exact de `main` ;
2. sauvegarde les donnees dans un Garage S3 externe a la VM ;
3. redemarre le noeud et valide son durcissement ;
4. detruit le disque source ;
5. restaure le backup `main` sur une nouvelle VM avec l'outillage candidat ;
6. converge et valide le deploiement restaure avec le candidat ;
7. tourne les secrets techniques et les mots de passe de bases de donnees ;
8. confirme que les mots de passe des utilisateurs OIDC n'ont pas change.

Garage et son endpoint TLS sont prepares par `setup-garage.sh`. Les fonctions
QEMU reutilisables vivent dans `lib/arch-vm.sh`.

## Execution locale

Le parcours rapide suppose qu'il est lance comme root dans une VM deja preparee :

```bash
make test-ci-fast
```

Le parcours complet exige Docker, QEMU, cloud-localds, socat et un acces Internet :

```bash
make test-ci-full
```

Les SHA et URLs peuvent etre imposes avec `MAIN_SHA`, `CANDIDATE_SHA`,
`MAIN_REPO_URL` et `CANDIDATE_REPO_URL`.

Les fichiers sous `.ci/` sont ephemeres. Aucun kit de reprise ni secret genere
n'est publie comme artefact GitHub Actions.
