#!/usr/bin/env python3
import json
import os
import subprocess
import tempfile
from pathlib import Path

CONFIG_PATH = Path(os.environ.get("OPENBAO_CONFIG_PATH", "/etc/admin-config/homelab-node-admin-config/hosts/group_vars/all.yml"))
AGE_KEY_PATH = Path(os.environ.get("SOPS_AGE_KEY_FILE", "/etc/sops/age/keys.txt"))
SECTIONS = ("openbao", "openbao_config")


def upsert_root_token(text: str, section: str, token: str) -> str:
    lines = text.splitlines()
    section_header = f"{section}:"

    for idx, line in enumerate(lines):
        if line != section_header:
            continue

        insert_at = idx + 1
        for child_idx in range(idx + 1, len(lines)):
            child_line = lines[child_idx]
            if child_line and not child_line.startswith(" "):
                break
            insert_at = child_idx + 1
            if child_line.startswith("  root_token:"):
                lines[child_idx] = f'  root_token: "{token}"'
                return "\n".join(lines) + "\n"

        lines.insert(insert_at, f'  root_token: "{token}"')
        return "\n".join(lines) + "\n"

    if lines and lines[-1] != "":
        lines.append("")
    lines.extend([section_header, f'  root_token: "{token}"'])
    return "\n".join(lines) + "\n"


def update_plain_config(path: Path, token: str) -> None:
    text = path.read_text()
    for section in SECTIONS:
        text = upsert_root_token(text, section, token)
    path.write_text(text)


def update_sops_config(path: Path, token: str) -> None:
    if not path.exists():
        return
    if not AGE_KEY_PATH.exists():
        raise SystemExit(f"SOPS age key is required to update {path}: {AGE_KEY_PATH}")

    env = os.environ.copy()
    env["SOPS_AGE_KEY_FILE"] = str(AGE_KEY_PATH)
    decrypted = subprocess.check_output(
        ["sops", "--decrypt", "--output-type", "json", str(path)],
        env=env,
        text=True,
    )
    data = json.loads(decrypted or "{}")
    for section in SECTIONS:
        value = data.setdefault(section, {})
        if not isinstance(value, dict):
            raise SystemExit(f"{section} must be a mapping in {path}")
        value["root_token"] = token

    age_public_key = subprocess.check_output(
        ["age-keygen", "-y", str(AGE_KEY_PATH)],
        text=True,
    ).strip()

    with tempfile.NamedTemporaryFile("w", delete=False) as plain_file:
        json.dump(data, plain_file)
        plain_file.write("\n")
        plain_path = Path(plain_file.name)

    try:
        encrypted = subprocess.check_output(
            [
                "sops",
                "--config",
                "/dev/null",
                "--encrypt",
                "--age",
                age_public_key,
                "--input-type",
                "json",
                "--output-type",
                "yaml",
                str(plain_path),
            ],
            text=True,
        )
        path.write_text(encrypted)
    finally:
        plain_path.unlink(missing_ok=True)


def main() -> None:
    token = os.environ.get("OPENBAO_TOKEN", "")
    if not token:
        raise SystemExit("OPENBAO_TOKEN is required")

    update_plain_config(CONFIG_PATH, token)
    update_sops_config(CONFIG_PATH.with_name("secrets.sops.yaml"), token)


if __name__ == "__main__":
    main()
