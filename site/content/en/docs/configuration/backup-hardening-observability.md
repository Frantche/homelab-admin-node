---
title: Backup, Hardening, Observability
weight: 40
---

## Backup

Backups use restic repositories configured in encrypted secrets.

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

The role manages SSH hardening, sudoers, nftables, journald persistence, auditd, fail2ban, sysctl settings, sensitive file permissions, and optional AppArmor profiles.

## Observability

The observability role deploys an OpenTelemetry Collector. Backends remain external.

```yaml
observability:
  enabled: true
  metrics_endpoint: "http://victoriametrics.example.net:8428/opentelemetry/v1/metrics"
  logs_endpoint: "http://victorialogs.example.net:9428/insert/opentelemetry/v1/logs"
  collection_interval: "30s"
```
