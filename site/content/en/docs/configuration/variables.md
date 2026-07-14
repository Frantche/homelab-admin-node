---
title: Core Variables
weight: 20
---

The most important non-secret variables are:

| Variable | Default/example | Purpose |
| --- | --- | --- |
| `admin_repo_url` | `ssh://git@example.com/homelab/homelab-admin-node.git` | Git URL used by the node to clone or update this repository. |
| `admin_node_root` | `/srv/admin` | Runtime root for stack data, copied Compose files, env files, backups, and generated certificates. |
| `admin_mode_file` | `/etc/admin-node/mode` | File that records the lifecycle mode used by the admin-node workflow. |
| `admin_git_ref_file` | `/etc/admin-node/git-ref` | File that records the deployed Git reference. |
| `admin_node_lan_ip` | `192.168.1.10` | LAN IP used by DNS records and local certificate SANs. |
| `admin_node_fqdn` | empty string | Optional node FQDN for inventory-specific references. |
| `acme_email` | `admin@example.com` | Email used when Traefik can request ACME certificates. |
| `ci_mode` | `false` | Enables CI defaults and mock behavior for services that need external credentials. |

## Storage isolation

`storage_isolation` can isolate stack data paths so one service cannot consume all available disk space. It is disabled by default in the role defaults, while the public admin-config example enables the Btrfs path so CI exercises quotas on every bootstrap run.

```yaml
storage_isolation:
  enabled: true
  backend: auto # auto, btrfs, lvm
  migrate_existing: false
  lvm:
    volume_group: ""
    filesystem: ext4
  entries:
    - name: harbor
      path: "{{ admin_node_root }}/data/harbor"
      size: "40G"
    - name: backups
      path: "{{ admin_node_root }}/backups"
      size: "30G"
```

| Variable | Default/example | Purpose |
| --- | --- | --- |
| `storage_isolation.enabled` | `false` | Enables per-path storage isolation. |
| `storage_isolation.backend` | `auto` | Chooses `btrfs` on Btrfs filesystems, otherwise uses `lvm` only when a volume group is configured. |
| `storage_isolation.migrate_existing` | `false` | Allows existing non-empty directories to be copied into isolated storage. Prefer enabling this per entry. |
| `storage_isolation.lvm.volume_group` | empty string | Existing guest OS LVM volume group used by the `lvm` backend. |
| `storage_isolation.lvm.filesystem` | `ext4` | Filesystem created on new logical volumes. |
| `storage_isolation.entries[].name` | `harbor` | Stable identifier used for subvolume or logical volume naming. |
| `storage_isolation.entries[].path` | `/srv/admin/data/harbor` | Directory to isolate. |
| `storage_isolation.entries[].size` | `40G` | Btrfs qgroup limit or LVM logical volume size. |
| `storage_isolation.entries[].migrate_existing` | inherits global value | Opt-in migration for this entry. |
| `storage_isolation.entries[].stop_units` | `[]` | Systemd units stopped before migrating existing data and started afterwards. |

Btrfs is the preferred backend for the Arch cloud image used by this project. The role verifies `btrfs-progs`, enables Btrfs quotas, creates one subvolume per entry, and applies a qgroup limit.

The LVM backend expects an existing volume group inside the VM. Proxmox storage named `local-lvm` is host-side storage for VM disks; it does not create LVM volume groups inside Arch.

Existing non-empty directories are never migrated unless `migrate_existing=true`. During migration, the role copies data with `rsync -aHAX --numeric-ids`, moves the old directory aside as `.pre-storage-isolation-<timestamp>`, and keeps it for manual rollback.

Example:

```yaml
admin_node_lan_ip: "192.168.1.10"
acme_email: "admin@example.com"

service_domains:
  harbor: "harbor.example.com"
  openbao: "bao.example.com"
  keycloak: "keycloak.example.com"
  gitea: "git.example.com"
  traefik: "traefik.example.com"
```

## Service domains

`service_domains` maps each stack component to the hostname used by Traefik, DNS records, OIDC redirect URLs, and API validation.

| Key | Default/example | Purpose |
| --- | --- | --- |
| `service_domains.harbor` | `harbor.example.com` | Harbor registry and API hostname. |
| `service_domains.openbao` | `bao.example.com` | OpenBao UI and API hostname. |
| `service_domains.keycloak` | `keycloak.example.com` | Keycloak realm and OIDC issuer hostname. |
| `service_domains.gitea` | `git.example.com` | Gitea UI and API hostname. |
| `service_domains.traefik` | `traefik.example.com` | Traefik dashboard hostname and default certificate common name. |

## Traefik external services

`traefik.external_services` publishes services outside the admin-node Docker stacks through Traefik.

```yaml
traefik:
  external_services:
    - name: "nas"
      hostname: "nas.example.com"
      url: "https://192.168.1.50:8443"
      pihole_dns: true
      cloudflare: false
      tls:
        verify: false
```

Use `tls.ca_pem` instead of `tls.verify: false` when the backend has a private CA that should be trusted by Traefik.

## CI switches

`ci_mode` enables CI-safe defaults. The nested `ci` map controls individual mocks used by validation and convergence tests.

| Variable | Default | Purpose |
| --- | --- | --- |
| `ci.mock_pihole` | `true` | Allows Pi-hole validation to run against CI mocks. |
| `ci.mock_cloudflare_tunnel` | `true` | Allows Cloudflare Tunnel validation to run without a real tunnel. |
| `ci.skip_public_url_validation` | `true` | Skips public URL validation in CI-oriented runs. |

Secrets should be placed in the environment secrets file, such as `di/group_vars/secrets.sops.yaml` or `pr/group_vars/secrets.sops.yaml`, not in `di/group_vars/all.yml` or `pr/group_vars/all.yml`.
