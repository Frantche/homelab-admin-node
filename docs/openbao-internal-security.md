# Sécurité interne OpenBao

OpenBao n'est pas connecté au réseau généraliste `admin-edge`. Il rejoint
uniquement deux bridges internes :

- `traefik-openbao`, limité à Traefik et OpenBao pour l'UI et l'API ;
- `openbao-metrics`, limité à OpenBao et au collecteur OpenTelemetry.

Sur `traefik-openbao`, Traefik porte aussi l'alias DNS du domaine Keycloak.
OpenBao peut ainsi lire la découverte OIDC en HTTPS via Traefik, sans rejoindre
le réseau généraliste `admin-edge`.

Toutes les connexions utilisent TLS. Le rôle `openbao_pki` maintient une CA
interne dédiée et un certificat serveur avec les SAN `openbao`, `localhost` et
`127.0.0.1`. Traefik et OpenTelemetry montent uniquement la CA publique et
vérifient le nom `openbao`. Les commandes `init`, `unseal`, `backup`, `restore`
et `validate` exécutées dans le conteneur utilisent aussi
`BAO_CACERT=/openbao/tls/ca.pem`.

La clé de CA reste `0600 root:root`. La clé serveur est `0640 root:1000`, ce qui
permet uniquement au processus OpenBao de la lire. Le certificat est renouvelé
à moins de trente jours de son expiration.

OpenBao 2.6 a supprimé le support de `mlock` et refuse de démarrer si
`disable_mlock` est encore configuré. La capacité `IPC_LOCK` et la limite
`memlock` ont donc été retirées au lieu de donner une fausse garantie. Le nœud
doit désactiver le swap ou utiliser un swap chiffré, conformément au
durcissement post-installation OpenBao.

Les métriques sans token ne sont accessibles que sur le bridge interne
`openbao-metrics`; elles ne sont pas routées séparément par Traefik.
