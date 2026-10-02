#!/usr/bin/env python3
import argparse
import os
import re
from pathlib import Path

DEFAULT_DOMAINS = {
    "keycloak": "keycloak.example.com",
    "openbao": "bao.example.com",
    "harbor": "harbor.example.com",
    "gitea": "git.example.com",
    "traefik": "traefik.example.com",
    "crowdsec": "crowdsec.example.com",
    "crowdsec_web_ui": "crowdsec-ui.example.com",
}
CONFIG_PATH = Path(
    os.environ.get(
        "ADMIN_NODE_CONFIG_ALL_YML",
        "/etc/admin-config/homelab-node-admin-config/hosts/group_vars/all.yml",
    )
)


def load_domains() -> dict[str, str]:
    domains = dict(DEFAULT_DOMAINS)
    if not CONFIG_PATH.is_file():
        return domains

    lines = CONFIG_PATH.read_text().splitlines()
    in_section = False
    for raw_line in lines:
        line = raw_line.rstrip()
        if not in_section:
            if re.match(r"^service_domains:\s*$", line):
                in_section = True
            continue

        if line and not line.startswith(" "):
            break

        match = re.match(r'^\s{2}([A-Za-z0-9_-]+):\s*"?([^"#]+?)"?\s*$', line)
        if match:
            domains[match.group(1)] = match.group(2).strip()

    in_section = False
    for raw_line in lines:
        line = raw_line.rstrip()
        if not in_section:
            if re.match(r"^crowdsec_web_ui:\s*$", line):
                in_section = True
            continue

        if line and not line.startswith(" "):
            break

        match = re.match(r'^  hostname:\s*"?([^"#]+?)"?\s*$', line)
        if match:
            domains["crowdsec_web_ui"] = match.group(1).strip()
            break

    return domains


def main() -> int:
    parser = argparse.ArgumentParser()
    subparsers = parser.add_subparsers(dest="command", required=True)

    subparsers.add_parser("list")

    get_parser = subparsers.add_parser("get")
    get_parser.add_argument("name")

    args = parser.parse_args()
    domains = load_domains()

    if args.command == "list":
        for key in (
            "keycloak",
            "openbao",
            "harbor",
            "gitea",
            "traefik",
            "crowdsec",
            "crowdsec_web_ui",
        ):
            value = domains.get(key)
            if value:
                print(value)
        return 0

    value = domains.get(args.name)
    if not value:
        raise SystemExit(f"unknown service domain: {args.name}")
    print(value)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
