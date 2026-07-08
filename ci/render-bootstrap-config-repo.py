#!/usr/bin/env python3
import secrets as secretlib
import shutil
import sys
from pathlib import Path

import yaml


def write_ci_vars(group_vars: Path, admin_repo_url: str) -> None:
    data = {
        "ci_mode": True,
        "admin_ci_disable_auto_converge": True,
        "admin_repo_url": admin_repo_url,
        "admin_node_root": "/srv/admin",
        "admin_mode_file": "/etc/admin-node/mode",
        "admin_git_ref_file": "/etc/admin-node/git-ref",
        "admin_node_lan_ip": "127.0.0.1",
        "acme_email": "ci@example.com",
        "traefik": {
            "dashboard_enabled": True,
            "dashboard_hostname": "traefik.example.com",
            "log_level": "INFO",
            "access_logs": True,
            "local_tls_enabled": True,
        },
        "pihole": {
            "enabled": True,
            "api_version": "auto",
            "url": "http://pihole.local/admin",
            "api_url": "http://pihole.local",
            "dns_records": [
                {"name": "harbor.example.com", "ip": "{{ admin_node_lan_ip }}"},
                {"name": "bao.example.com", "ip": "{{ admin_node_lan_ip }}"},
                {"name": "keycloak.example.com", "ip": "{{ admin_node_lan_ip }}"},
                {"name": "git.example.com", "ip": "{{ admin_node_lan_ip }}"},
                {"name": "traefik.example.com", "ip": "{{ admin_node_lan_ip }}"},
            ],
        },
        "observability": {
            "enabled": True,
            "metrics_endpoint": "http://127.0.0.1:43190/v1/metrics",
            "logs_endpoint": "http://127.0.0.1:43190/v1/logs",
            "compression": "none",
            "otlp_encoding": "json",
            "collection_interval": "5s",
            "service_metrics_interval": "5s",
            "docker_api_version": "1.40",
            "mock_backend_enabled": True,
            "mock_state_dir": "/tmp/admin-node-otel-mock-bootstrap-user-journey",
        },
    }
    with (group_vars / "ci-bootstrap-vars.yml").open("w") as f:
        yaml.safe_dump(data, f, default_flow_style=False, sort_keys=False)


def main() -> None:
    repo_root = Path(sys.argv[1])
    config_repo = Path(sys.argv[2])
    admin_repo_url = sys.argv[3]
    group_vars = config_repo / "hosts" / "group_vars"
    all_example_path = repo_root / "examples/admin-config/group_vars/all.yml.example"

    shutil.copyfile(all_example_path, group_vars / "all.yml")
    write_ci_vars(group_vars, admin_repo_url)

    with (repo_root / "examples/admin-config/group_vars/secrets.sops.yaml.example").open() as f:
        secrets = yaml.safe_load(f)

    oidc_clients = {
        name: {
            "client_id": f"ci-{name}-{secretlib.token_hex(4)}",
            "client_secret": secretlib.token_hex(32),
        }
        for name in ("harbor", "openbao", "gitea")
    }

    secrets.update(
        {
            "vault_oidc_harbor_client_secret": oidc_clients["harbor"]["client_secret"],
            "vault_oidc_openbao_client_secret": oidc_clients["openbao"]["client_secret"],
            "vault_oidc_gitea_client_secret": oidc_clients["gitea"]["client_secret"],
        }
    )
    secrets["admin"] = {"traefik_dashboard_basic_auth": "admin:$$apr1$$ci$$fakehash"}
    secrets["pihole"] = {"api_token": "ci-pihole-api-token"}
    secrets["cloudflare"] = {
        "tunnel_id": "fake-tunnel-id",
        "tunnel_token": "eyJhIjoiZmFrZSIsInQiOiJmYWtlIiwicyI6ImZha2UifQ==",
        "account_id": "fake-account-id",
        "dns_api_token": "fake-dns-token",
        "credentials_json": "{}",
    }
    secrets["keycloak"] = {
        "db_password": "ci-keycloak-db-pass",
        "admin_user": "admin",
        "admin_password": "ci-keycloak-admin-pass",
    }
    secrets["keycloak_config"] = {
        "groups": ["harbor-admins"],
        "users": [
            {
                "username": "ci-sso-user",
                "password": "ci-sso-user-password",
                "email": "ci-sso-user@example.com",
                "first_name": "CI",
                "last_name": "SSO",
                "email_verified": True,
                "temporary_password": False,
                "groups": ["harbor-admins"],
            }
        ],
    }
    secrets["harbor"] = {
        "admin_password": "ci-Harbor-admin-p4ss",
        "db_password": "ci-harbor-db-pass",
        "core_secret": "ci-harbor-core-secret",
        "jobservice_secret": "ci-harbor-job-secret",
        "registry_password": "ci-harbor-registry",
    }
    secrets["gitea"] = {
        "admin_user": "admin",
        "admin_password": "ci-Gitea-admin-p4ss",
        "db_password": "ci-gitea-db-pass",
        "secret_key": "ci-gitea-secret-key-change-me-32chars",
        "internal_token": "ci-gitea-internal-token-change-me-32chars",
        "jwt_secret": "ci-gitea-jwt-secret-change-me-32chars",
    }
    secrets["openbao"] = {"root_token": ""}
    secrets["oidc_clients"] = oidc_clients
    secrets["backup"] = {
        "restic_init_repositories": True,
        "restic_local_password": "ci-restic-pass",
        "restic_default_forget_args": "--keep-last 3 --prune",
        "restic_require_secure_repositories": True,
        "restic_repositories": [
            {
                "name": "local",
                "repository": "/srv/admin/backups/restic",
                "password": "ci-restic-pass",
            }
        ],
    }

    with (group_vars / "secrets.plain.yaml").open("w") as f:
        yaml.safe_dump(secrets, f, default_flow_style=False, sort_keys=False)


if __name__ == "__main__":
    main()
