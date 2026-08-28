---
title: Backup, Hardening, Observability
weight: 40
---

## Backup

Backups use restic repositories configured in encrypted secrets.

Reference: [restic documentation](https://restic.readthedocs.io/en/stable/).

```yaml
backup:
  require_remote_repository: true
  restic_default_forget_args: "--keep-daily 7 --keep-weekly 4 --keep-monthly 12"
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
| `backup.restic_forget_args` | `--keep-last 3` | Legacy single repository retention arguments. `--prune` is deferred to the weekly integrity job when still present in an older configuration. |
| `backup.restic_default_forget_args` | `--keep-last 3` | Default retention arguments for repositories. |
| `backup.restic_init_repositories` | `false` | Initializes repositories before backup when enabled. |
| `backup.restic_require_secure_repositories` | `true` | Rejects insecure repository declarations when enabled. |
| `backup.require_remote_repository` | `true` | Must remain `true`; standard and offline-image backups require at least one non-local repository. Missing tooling, configuration, credentials, or delivery is fatal. |
| `backup.restic_backup_paths` | tool default | Optional explicit backup path list passed to the backup environment. |
| `backup.operation_lock_timeout` | `30m` | Maximum time a scheduled backup waits for another admin-node operation, such as convergence, to release the global lock. |
| `backup.standard_max_age` | `36h` | Maximum age accepted for the last successful standard backup. |
| `backup.gitea_process.max_age` | `36h` | Maximum age accepted for the separate Gitea process backup when enabled. |
| `backup.remote_max_age` | `36h` | Maximum age accepted for a successful non-local Restic delivery when remote backup is required. |
| `backup.offline_max_age` | `0` | Maximum age for offline-image backups. `0` keeps this class optional until an offline schedule is enabled. |
| `backup.integrity_max_age` | `192h` | Maximum age accepted for the last successful Restic integrity check. |
| `backup.gitea_quiesce_timeout` | `10m` | Maximum Gitea capture time while the standard backup creates its database/filesystem consistency boundary; restart has a separate two-minute health timeout. |

Successful backup classes write secret-free, mode `0600` markers below
`/srv/admin/backups/status`. `admin-node backup status` checks the markers
against the configured thresholds and exits non-zero when a required class is
missing or stale. It never opens a repository or reads backup credentials.

Two monitoring timers are enabled in `normal` mode:

- `admin-backup-status.timer` validates freshness every hour, starting 30
  minutes after the timer is activated;
- `admin-backup-integrity.timer` reads one deterministic quarter of each Restic
  repository weekly, then prunes every repository only after all integrity
  checks succeed. It advances only after checks and pruning succeed, providing a
  complete cryptographic data read over four successful checks. Run
  `admin-node backup restic-check` explicitly after initial installation to
  create the first integrity status marker without coupling the weekly timer to
  systemd reloads.

Failures trigger `admin-backup-failure@.service`, which writes a stable
`admin-node-backup` journal event suitable for forwarding to homelab alerting.
After upgrading an existing node, run one standard backup, the optional Gitea
process backup when enabled, and `admin-node backup restic-check` to initialize
the markers immediately.

The standard backup stops only the Gitea application container, keeps
`gitea-db` available for `pg_dump`, captures the database and files while writes
are quiesced, and restores the application's previous running state. The
manifest records the method and boundary timestamps. If stopping, capturing,
or restarting fails—or if the timeout expires—the backup is not published.

Backup completion is fail-closed for active stateful stacks. An active Keycloak,
Gitea, Harbor, or OpenBao stack must produce its required database/snapshot and
filesystem artifacts. In particular, OpenBao requires the dedicated snapshot
token and Harbor requires registry data. The completed manifest records each
required artifact as `produced`; offline images and the repository bundle are
recorded as `disabled` for a standard backup.

A local Restic repository is not sufficient for an application backup. At
least one SFTP, S3, REST, or other supported non-local repository must be
configured. Local directories remain useful as CI fixtures or staging, but are
not accepted as production recovery authority.

### Scheduled offline recovery points

Set `backup.offline.enabled: true` only after completing the
[offline recovery runbook]({{< relref "/docs/operations/offline-recovery" >}}).
The separate timer runs `backup run --include-images`, enforces its own free-space
floor, keeps its own number of offline points, and verifies the resulting bundle.

| Variable | Default | Purpose |
| --- | --- | --- |
| `backup.offline.enabled` | `false` | Opts in to `admin-offline-backup.timer`; locked mode always stops it. |
| `backup.offline.on_calendar` | `Sun *-*-* 04:00:00` | Offline backup schedule. |
| `backup.offline.randomized_delay_sec` | `30m` | Spreads image-heavy work. |
| `backup.offline.retention` | `2` | Offline points retained independently from standard backups. |
| `backup.offline.max_age` | `192h` | Maximum accepted age reported by `backup offline-status`. |
| `backup.offline.minimum_free_bytes` | `32212254720` | Required free capacity before exporting images (30 GiB). |
| `backup.offline.recovery_kit_max_age` | `2160h` | Maximum age of the external recovery-kit attestation (90 days). |

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
    region: "us-east-1"
    aws_access_key_id: "CHANGE_ME"
    aws_secret_access_key: "CHANGE_ME"
    max_retention: 7
```

When enabled, Ansible starts `admin-gitea-process-backup.timer`, which runs daily
at 03:30 by default. The systemd calendar can be customized with
`backup.gitea_process.on_calendar`. The service waits up to 30 minutes for a
concurrent admin operation such as convergence to release the global lock. The
wait duration or timeout is recorded in the journal, and the whole service is
bounded to 45 minutes. The service checks that both `gitea-db` and `gitea` are
healthy before running `ghcr.io/frantche/gitea-backup-restore-process:0.3.35`; if either
container is not healthy, the service fails, emits a systemd failure event,
and does not refresh its success marker.

| Variable | Default/example | Purpose |
| --- | --- | --- |
| `backup.gitea_process.enabled` | `false` | Enables the additional Gitea backup timer. |
| `backup.gitea_process.on_calendar` | `*-*-* 03:30:00` | systemd `OnCalendar` schedule for the timer. |
| `backup.gitea_process.method` | `s3` or `ftp` | Storage backend passed as `BACKUP_METHODE`. |
| `backup.gitea_process.image` | `ghcr.io/frantche/gitea-backup-restore-process:0.3.35` | Backup container image. |
| `backup.gitea_process.network` | `gitea-db` | Docker network used by the helper to reach PostgreSQL. Override only when the Gitea database uses a different isolated network. |
| `backup.gitea_process.egress_network` | `admin-edge` | Second Docker network used for DNS and access to the remote S3 or FTP backend. If it matches `network`, the helper attaches it only once. |
| `backup.gitea_process.backup_file_log` | `/srv/admin/backups/gitea-process/history/backupFileLog.txt` | Writable helper history outside the read-only Gitea data mount. Its parent is mounted as scratch storage. |
| `backup.gitea_process.s3_multipart_enabled` | `true` | Splits S3 uploads into parts, avoiding reverse-proxy request-size limits for large archives. |
| `backup.gitea_process.max_retention` | `5` | Maximum number of backups retained by the helper. |
| `backup.gitea_process.endpoint_url` | required for S3 | S3-compatible endpoint URL. |
| `backup.gitea_process.bucket` | required for S3 | S3 bucket name. |
| `backup.gitea_process.region` | required for S3 | S3 region passed as `REGION`; use your provider's expected value, such as `us-east-1`. |
| `backup.gitea_process.aws_access_key_id` | required for S3 | S3 access key. Store encrypted. |
| `backup.gitea_process.aws_secret_access_key` | required for S3 | S3 secret key. Store encrypted. |
| `backup.gitea_process.ftp_host` | required for FTP | FTP host and port. |
| `backup.gitea_process.ftp_user` | required for FTP | FTP username. Store encrypted when sensitive. |
| `backup.gitea_process.ftp_password` | required for FTP | FTP password. Store encrypted. |
| `backup.gitea_process.env` | `{}` | Extra environment variables passed to the helper. |

## Hardening

Hardening is enabled by default.
Backup and stack systemd services also use a restrictive umask, prevent new
privileges, isolate temporary files, and deny direct access to home directories
and kernel-control interfaces. Rendered units are checked with
`systemd-analyze verify` by `make ci-quality`.

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
| `admin-node-overview.json` | Global health, host saturation, and application request rate. |
| `admin-node-host-docker.json` | Hostmetrics; legacy Docker panels are retained but no longer receive data. |
| `admin-node-traefik.json` | Traefik request rate, status codes, latency, and errors. |
| `admin-node-harbor.json` | Harbor core/exporter inventory, API traffic, latency, and tasks. |
| `admin-node-openbao.json` | OpenBao scrape health, seal status, request latency, leases, and Raft storage. |
| `admin-node-gitea.json` | Gitea scrape health, process/runtime metrics, HTTP traffic, and optional Gitea counters. |

Some panels may show `No data` when a service version does not expose the
corresponding metric. Legacy Docker panels intentionally show `No data`: the
`docker_stats` receiver was removed so no container has direct or proxied access
to the Docker API. The dashboards otherwise target `hostmetrics`, Gitea, Harbor
core/exporter, OpenBao, and Traefik.
