---
title: Backup, Hardening, Observability
weight: 40
---

## Backup

Backups use restic repositories configured in encrypted secrets.

Reference: [restic documentation](https://restic.readthedocs.io/en/stable/).

```yaml
backup:
  restic_default_forget_args: "--keep-daily 7 --keep-weekly 4 --keep-monthly 12 --prune"
  restic_require_secure_repositories: true
  restic_repositories:
    - name: local
      repository: "/srv/admin/backups/restic"
      password: "CHANGE_ME"
```

Repositories can be local, SFTP, S3, or any restic-supported backend.

| Variable | Default/example | Purpose |
| --- | --- | --- |
| `backup.restic_repositories[]` | derived from legacy single-repository settings when unset | List of restic repositories to use. |
| `backup.restic_repositories[].name` | required | Logical repository name. |
| `backup.restic_repositories[].repository` | required | Restic repository URL/path. |
| `backup.restic_repositories[].password` | required | Restic password. Store encrypted. |
| `backup.restic_repositories[].forget_args` | `backup.restic_default_forget_args` | Per-repository retention arguments. |
| `backup.restic_repositories[].options` | unset | Extra restic CLI options applied to backup, init, and forget commands for that repository. |
| `backup.restic_repositories[].env` | `{}` | Backend-specific environment variables such as S3 credentials. Store sensitive values encrypted. |
| `backup.restic_repository` | unset | Legacy single repository URL used when `restic_repositories` is absent. |
| `backup.restic_password` | unset | Legacy single repository password. Store encrypted. |
| `backup.restic_forget_args` | `--keep-last 3 --prune` | Legacy single repository retention arguments. |
| `backup.restic_default_forget_args` | `--keep-last 3 --prune` | Default retention arguments for repositories. |
| `backup.restic_init_repositories` | `false` | Initializes repositories before backup when enabled. |
| `backup.restic_require_secure_repositories` | `true` | Rejects insecure repository declarations when enabled. |
| `backup.restic_backup_paths` | tool default | Optional explicit backup path list passed to the backup environment. |

### Gitea Backup-Restore-Process

The standard local/restic backup remains enabled. A second Gitea-specific job can
be enabled with `backup.gitea_process.enabled`.

```yaml
backup:
  gitea_process:
    enabled: true
    on_calendar: "*-*-* 03:30:00"
    method: s3
    endpoint_url: "https://s3.example.com"
    bucket: "gitea-backups"
    aws_access_key_id: "CHANGE_ME"
    aws_secret_access_key: "CHANGE_ME"
    max_retention: 7
```

When enabled, Ansible starts `admin-gitea-process-backup.timer`, which runs daily
at 03:30 by default. The systemd calendar can be customized with
`backup.gitea_process.on_calendar`. The service checks that both `gitea-db` and `gitea` are healthy before
running `harbor.frantchenco.page/library/gitea-backup:latest`; if either
container is not healthy, that execution is skipped.

| Variable | Default/example | Purpose |
| --- | --- | --- |
| `backup.gitea_process.enabled` | `false` | Enables the additional Gitea backup timer. |
| `backup.gitea_process.on_calendar` | `*-*-* 03:30:00` | systemd `OnCalendar` schedule for the timer. |
| `backup.gitea_process.method` | `s3` or `ftp` | Storage backend passed as `BACKUP_METHODE`. |
| `backup.gitea_process.image` | `harbor.frantchenco.page/library/gitea-backup:latest` | Backup container image. |
| `backup.gitea_process.max_retention` | `5` | Maximum number of backups retained by the helper. |
| `backup.gitea_process.endpoint_url` | required for S3 | S3-compatible endpoint URL. |
| `backup.gitea_process.bucket` | required for S3 | S3 bucket name. |
| `backup.gitea_process.aws_access_key_id` | required for S3 | S3 access key. Store encrypted. |
| `backup.gitea_process.aws_secret_access_key` | required for S3 | S3 secret key. Store encrypted. |
| `backup.gitea_process.ftp_host` | required for FTP | FTP host and port. |
| `backup.gitea_process.ftp_user` | required for FTP | FTP username. Store encrypted when sensitive. |
| `backup.gitea_process.ftp_password` | required for FTP | FTP password. Store encrypted. |
| `backup.gitea_process.env` | `{}` | Extra environment variables passed to the helper. |

## Hardening

Hardening is enabled by default.

```yaml
hardening:
  enabled: true
  ssh:
    allow_users:
      - admin
  firewall:
    ssh_allowed_cidrs:
      - "0.0.0.0/0"
    https_allowed_cidrs:
      - "0.0.0.0/0"
  fail2ban:
    enabled: true
  auditd:
    enabled: true
  apparmor:
    enabled: true
    enforce: true
```

The role manages SSH hardening, sudoers, nftables, journald persistence, auditd, fail2ban, sysctl settings, sensitive file permissions, and optional AppArmor profiles. Existing local users listed in `hardening.ssh.allow_users` are also added to the `homelab` group so they can operate the shared Git checkout under `/opt/homelab-admin-node`.

Reference: [Docker Compose documentation](https://docs.docker.com/compose/) for container runtime declarations affected by hardening profiles.

| Variable | Default/example | Purpose |
| --- | --- | --- |
| `hardening.enabled` | `true` | Enables the hardening role. |
| `hardening.ssh.allow_users[]` | `["admin"]` | Users allowed by the managed SSH drop-in and, when they exist locally, members of the `homelab` operator group for the admin checkout. |
| `hardening.sudo.nopasswd` | `true` | Controls passwordless sudo for the wheel group. |
| `hardening.firewall.ssh_allowed_cidrs[]` | `["0.0.0.0/0", "::/0"]` | CIDRs allowed to reach SSH in nftables. |
| `hardening.firewall.https_allowed_cidrs[]` | `["0.0.0.0/0", "::/0"]` | CIDRs allowed to reach HTTPS in nftables. |
| `hardening.fail2ban.enabled` | `true` | Installs and enables fail2ban SSH protection. |
| `hardening.auditd.enabled` | `true` | Installs auditd and deploys admin-node audit rules. |
| `hardening.apparmor.enabled` | `true` | Installs and configures AppArmor support. |
| `hardening.apparmor.enforce` | `true` | Loads enabled profiles in enforce mode when runtime support is active. |
| `hardening.apparmor.auto_reboot` | `true` | Allows the role to request a reboot when AppArmor needs kernel activation. |
| `hardening.apparmor.profiles.traefik` | `true` | Enables the Traefik AppArmor profile. |
| `hardening.apparmor.profiles.cloudflared` | `true` | Enables the cloudflared AppArmor profile. |
| `hardening.apparmor.profiles.openbao` | `true` | Enables the OpenBao AppArmor profile. |
| `hardening.lynis.enabled` | `true` | Enables Lynis-related hardening checks/configuration. |

## Observability

The observability role deploys an OpenTelemetry Collector. Backends remain external.

Reference: [OpenTelemetry Collector documentation](https://opentelemetry.io/docs/collector/).

```yaml
observability:
  enabled: true
  metrics_endpoint: "http://victoriametrics.example.net:8428/opentelemetry/v1/metrics"
  logs_endpoint: "http://victorialogs.example.net:9428/insert/opentelemetry/v1/logs"
  collection_interval: "30s"
```

| Variable | Default/example | Purpose |
| --- | --- | --- |
| `observability.enabled` | `false` | Deploys the OpenTelemetry Collector stack when enabled. |
| `observability.metrics_endpoint` | example VictoriaMetrics OTLP URL | Required when enabled. OTLP HTTP metrics destination. |
| `observability.logs_endpoint` | example VictoriaLogs OTLP URL | Required when enabled. OTLP HTTP logs destination. |
| `observability.collection_interval` | `30s` | Collector scrape/collection interval. |
| `observability.docker_api_version` | `1.40` | Docker API version used by the collector configuration. |
| `observability.expose_host_ports` | `false` | Exposes collector ports on the host when enabled. |

### Grafana dashboards

Importable Grafana dashboards are provided under
`stacks/observability/grafana/dashboards/`.

The JSON files are standalone dashboard exports. They do not deploy Grafana and
do not pin a datasource UID. During import, select a Prometheus-compatible
Grafana datasource pointing at VictoriaMetrics.

Available dashboards:

| Dashboard | Purpose |
| --- | --- |
| `admin-node-overview.json` | Global health, host saturation, application request rate, and top containers. |
| `admin-node-host-docker.json` | Hostmetrics and Docker runtime metrics. |
| `admin-node-traefik.json` | Traefik request rate, status codes, latency, and errors. |
| `admin-node-harbor.json` | Harbor core/exporter inventory, API traffic, latency, and tasks. |
| `admin-node-openbao.json` | OpenBao scrape health, seal status, request latency, leases, and Raft storage. |
| `admin-node-gitea.json` | Gitea scrape health, process/runtime metrics, HTTP traffic, and optional Gitea counters. |

Some panels may show `No data` when a service version does not expose the
corresponding metric. The dashboards target the metrics already collected by the
OpenTelemetry Collector: `hostmetrics`, `docker_stats`, Gitea, Harbor
core/exporter, OpenBao, and Traefik.
