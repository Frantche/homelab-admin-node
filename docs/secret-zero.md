# Secret zéro

Le "secret zéro" est la clé privée `age` utilisée par `sops` pour déchiffrer les fichiers secrets du nœud d'administration.
Elle est stockée manuellement dans `/etc/sops/age/keys.txt` avec les permissions `0400 root:root`.
Sans cette clé, le mode `locked` bloque toute stack sensible.

## Générer la paire de clés

Sur une machine de confiance ayant `age-keygen` installé :

```bash
age-keygen -o age-key.txt
```

La commande écrit la clé privée dans `age-key.txt` et affiche la clé publique au format `age1...`.
Conservez la clé privée hors du dépôt Git.

## Installer la clé privée sur l'admin-node

Copiez ensuite la clé privée sur le nœud d'administration :

```bash
sudo ./bin/admin-node secret install-age-key /path/to/age-key.txt
```

La commande crée `/etc/sops/age/keys.txt` avec les bons droits.
Le même résultat peut être obtenu manuellement avec :

```bash
sudo install -d -m 0700 -o root -g root /etc/sops/age
sudo install -m 0400 -o root -g root age-key.txt /etc/sops/age/keys.txt
```

## Configurer SOPS

Récupérez la clé publique affichée par `age-keygen` et placez-la dans `.sops.yaml` :

```yaml
creation_rules:
  - path_regex: group_vars/secrets\.sops\.yaml$
    age: ["age1xxxx...votre-clé-publique-age..."]
```

## Vérifier le fonctionnement

Vous pouvez tester le déchiffrement avec :

```bash
SOPS_AGE_KEY_FILE=/etc/sops/age/keys.txt sops --decrypt /chemin/vers/un/fichier.sops.yaml
```

Si la clé est absente, `sops` ne pourra pas déchiffrer les secrets et le système reste volontairement bloqué en `locked`.
