# Secret zéro
La clé privée age est injectée manuellement dans `/etc/sops/age/keys.txt` (0400 root:root).
Sans cette clé, le mode `locked` bloque toute stack sensible.
