# Observabilite

La stack `observability` deploie un OpenTelemetry Collector. Les backends restent externes: VictoriaMetrics pour les metriques et VictoriaLogs pour les logs.

## Dashboards Grafana

Les dashboards Grafana importables sont dans:

```bash
stacks/observability/grafana/dashboards/
```

Dashboards disponibles:

- `admin-node-overview.json`: vue globale du noeud admin, des scrapes, de l'hote et des conteneurs.
- `admin-node-host-docker.json`: metriques hostmetrics et docker_stats.
- `admin-node-traefik.json`: trafic, codes HTTP, latence et erreurs Traefik.
- `admin-node-harbor.json`: Harbor core/exporter, inventaire et activite API.
- `admin-node-openbao.json`: etat OpenBao, requetes, latence et stockage Raft.
- `admin-node-gitea.json`: sante Gitea, runtime Go/process et metriques HTTP.

Import manuel:

1. Creer dans Grafana une datasource Prometheus ou VictoriaMetrics qui pointe vers l'endpoint Prometheus-compatible de VictoriaMetrics.
2. Ouvrir `Dashboards > New > Import`.
3. Coller ou charger un des fichiers JSON.
4. Selectionner la datasource dans la variable `datasource`.

Les dashboards utilisent les labels ajoutes par le Collector, notamment `service_namespace="homelab-admin-node"`, `deployment_environment` et `host_name`. Le filtre `Environment` vaut `homelab` par defaut et peut etre change apres import.

Certains panneaux peuvent afficher `No data` si la version du service n'expose pas la metrique correspondante. Les panneaux essentiels reposent sur les sources deja collectees par le repo: `hostmetrics`, `docker_stats`, Gitea, Harbor core/exporter, OpenBao et Traefik.
