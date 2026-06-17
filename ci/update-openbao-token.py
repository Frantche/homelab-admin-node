#!/usr/bin/env python3
import os
from pathlib import Path

CONFIG_PATH = Path(os.environ.get("OPENBAO_CONFIG_PATH", "/etc/admin-config/homelab-node-admin-config/hosts/group_vars/all.yml"))
SECTIONS = ("openbao:", "openbao_config:")


def replace_root_token(text: str, section: str, token: str) -> str:
    lines = text.splitlines()
    in_section = False

    for idx, line in enumerate(lines):
        if line == section:
            in_section = True
            continue

        if in_section and line and not line.startswith(" "):
            break

        if in_section and line.startswith("  root_token:"):
            lines[idx] = f'  root_token: "{token}"'
            return "\n".join(lines) + "\n"

    raise SystemExit(f"missing root_token under {section}")


def main() -> None:
    token = os.environ.get("OPENBAO_TOKEN", "")
    if not token:
        raise SystemExit("OPENBAO_TOKEN is required")

    text = CONFIG_PATH.read_text()
    for section in SECTIONS:
        text = replace_root_token(text, section, token)
    CONFIG_PATH.write_text(text)


if __name__ == "__main__":
    main()
