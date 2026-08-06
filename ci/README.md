# CI - parcours utilisateur et reprise

La CI valide les commandes qu'un operateur execute reellement, dans des VM Arch
Linux creees depuis l'image cloud officielle.

## Parcours

`scenarios/bootstrap-user-journey.sh` s'execute dans une VM deja creee par
cloud-init. Il genere un config repo depuis les exemples, traverse les modes
`locked`, `init` et `normal`, puis valide services, OIDC, observabilite,
sauvegarde et restauration locale.

`scenarios/main-to-candidate-disaster-recovery.sh` expose une commande
idempotente par etape GitHub Actions. GitHub Actions execute deux variantes :

- `standard`, identique au parcours historique et autorisant les pulls registry ;
- `offline-images`, avec `DR_INCLUDE_IMAGES=true`, qui archive les images et le
  commit Git, bloque les registries sur la VM cible et impose un restore sans pull.

Ces variantes ne s'executent pas sur les pull requests ni sur les push. Elles
sont lancees manuellement avec `workflow_dispatch` en choisissant le scope
`disaster-recovery`, selectionne par defaut, ou automatiquement chaque dimanche
a `03:00 UTC`. Le cron hebdomadaire n'execute pas le job `bootstrap` separe,
mais conserve `ci-quality` et les contrats OIDC requis par le scenario DR. Le
scenario DR execute lui-meme le parcours bootstrap avant la mise a niveau.
Le lancement manuel propose aussi les scopes `bootstrap` et `all`.

Chaque variante :

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
make ci-bootstrap
```

Le parcours complet exige Docker, QEMU, cloud-localds, socat et un acces Internet :

```bash
make ci-full
```

`ci-full` execute les controles continus, les contrats OIDC, puis les variantes
DR `standard` et `offline-images`. Les parcours VM utilisent volontairement
l'image cloud Arch `latest`. Arch etant une rolling release, cloud-init effectue
une mise a niveau complete du systeme avant d'installer les paquets du noeud.

Les SHA et URLs peuvent etre imposes avec `MAIN_SHA`, `CANDIDATE_SHA`,
`MAIN_REPO_URL` et `CANDIDATE_REPO_URL`.

Les fichiers sous `.ci/` sont ephemeres. Aucun kit de reprise ni secret genere
n'est publie comme artefact GitHub Actions.
