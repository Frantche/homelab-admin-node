# Chaîne GitOps de confiance

La convergence automatique n'exécute jamais le checkout opérateur
`/opt/homelab-admin-node`. Ce répertoire reste utilisable pour les revues et
les opérations manuelles. Le timer exécute uniquement
`/var/lib/admin-node/runtime`, un checkout `root:root` dont les métadonnées Git
sont en `0700`.

## Conditions d'exécution

Le service systemd active `ADMIN_CONVERGE_REQUIRE_APPROVAL=true`. Avant tout
checkout, le CLI :

1. refuse les modifications locales suivies ;
2. récupère les objets distants sans effectuer de merge ou de pull ;
3. lit le SHA complet approuvé dans
   `/etc/admin-node/approved-code-revision` ou
   `/etc/admin-node/approved-inventory-revision` ;
4. vérifie que le commit appartient à `origin/main` ;
5. vérifie sa signature avec le trousseau dédié
   `/var/lib/admin-node/gnupg` ;
6. détache le checkout exactement sur ce SHA.

Le rôle `base` installe les clés publiques GitHub `web-flow`, utilisées pour les
commits de merge signés par GitHub. Des clés publiques supplémentaires,
notamment celle du dépôt de configuration, doivent être importées explicitement
dans ce trousseau root.

## Approbation

Après la fusion d'une PR et la réussite des checks :

```bash
sudo /var/lib/admin-node/runtime/bin/admin-node converge approve \
  --repository /var/lib/admin-node/runtime \
  --approval-file /etc/admin-node/approved-code-revision \
  <sha-complet>

sudo /var/lib/admin-node/runtime/bin/admin-node converge approve \
  --repository /etc/admin-config/homelab-node-admin-config \
  --approval-file /etc/admin-node/approved-inventory-revision \
  <sha-complet>
```

La commande récupère le commit, vérifie son appartenance à l'upstream et sa
signature, puis remplace atomiquement le fichier d'approbation. Un commit non
signé, signé par une clé absente du trousseau ou hors de `origin/main` est
refusé.

## Rollback

Après une convergence réussie, les révisions code et inventaire sont enregistrées
dans `/var/lib/admin-node/*last-known-good`. Si Ansible échoue après une mise à
jour, le CLI restaure les deux révisions précédentes et rejoue leur playbook. Le
service reste en échec afin d'alerter l'opérateur, mais le checkout et l'état
appliqué reviennent à la dernière release fonctionnelle.

En cas de rollback manuel, réapprouver les deux SHA connus comme bons puis
relancer :

```bash
sudo systemctl start admin-converge.service
```
