#!/usr/bin/env python3
import secrets
import shutil
import string
import sys
from pathlib import Path

import yaml

SECRET_PLACEHOLDER = "CHANGE_ME_IN_SOPS"
SECRET_ALPHABET = string.ascii_letters + string.digits
SECRET_LENGTH = 32


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


def random_ascii_secret() -> str:
    return "".join(secrets.choice(SECRET_ALPHABET) for _ in range(SECRET_LENGTH))


def replace_secret_placeholders(value):
    if isinstance(value, str):
        if value == SECRET_PLACEHOLDER:
            return random_ascii_secret()
        return value
    if isinstance(value, list):
        return [replace_secret_placeholders(item) for item in value]
    if isinstance(value, dict):
        return {key: replace_secret_placeholders(item) for key, item in value.items()}
    return value


def main() -> None:
    repo_root = Path(sys.argv[1])
    config_repo = Path(sys.argv[2])
    admin_repo_url = sys.argv[3]
    group_vars = config_repo / "hosts" / "group_vars"
    all_example_path = repo_root / "examples/admin-config/group_vars/all.yml.example"

    shutil.copyfile(all_example_path, group_vars / "all.yml")
    write_ci_vars(group_vars, admin_repo_url)

    with (repo_root / "examples/admin-config/group_vars/secrets.sops.yaml.example").open() as f:
        secrets_data = yaml.safe_load(f)

    rendered_secrets = replace_secret_placeholders(secrets_data)

    with (group_vars / "secrets.plain.yaml").open("w") as f:
        yaml.safe_dump(rendered_secrets, f, default_flow_style=False, sort_keys=False)


if __name__ == "__main__":
    main()
