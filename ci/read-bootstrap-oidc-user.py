#!/usr/bin/env python3
import shlex
import subprocess
import sys
from pathlib import Path

import yaml


def main() -> None:
    if len(sys.argv) != 2:
        raise SystemExit("usage: read-bootstrap-oidc-user.py <secrets.sops.yaml>")

    secrets_path = Path(sys.argv[1])
    decrypted = subprocess.check_output(
        ["sops", "--decrypt", "--output-type", "yaml", str(secrets_path)],
        text=True,
    )
    data = yaml.safe_load(decrypted)
    users = data.get("keycloak_config", {}).get("users", [])
    if not users:
        raise SystemExit("keycloak_config.users is required in decrypted secrets")

    user = users[0]
    username = user.get("username", "")
    password = user.get("password", "")
    if not username or not password:
        raise SystemExit("keycloak_config.users[0] requires username and password")

    print(f"OIDC_TEST_USERNAME={shlex.quote(str(username))}")
    print(f"OIDC_TEST_PASSWORD={shlex.quote(str(password))}")


if __name__ == "__main__":
    main()
