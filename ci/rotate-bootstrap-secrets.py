#!/usr/bin/env python3
import argparse
import hashlib
import json
import secrets
import string
from pathlib import Path

import yaml


ALPHABET = string.ascii_letters + string.digits
ROTATED_PATHS = (
    ("vault_oidc_harbor_client_secret",),
    ("vault_oidc_openbao_client_secret",),
    ("vault_oidc_gitea_client_secret",),
    ("keycloak", "db_password"),
    ("keycloak", "admin_password"),
    ("harbor", "db_password"),
    ("harbor", "admin_password"),
    ("gitea", "db_password"),
    ("gitea", "admin_password"),
)


def random_secret() -> str:
    return "".join(secrets.choice(ALPHABET) for _ in range(40))


def value_at(data, path):
    value = data
    for key in path:
        value = value[key]
    return value


def set_value(data, path, value):
    target = data
    for key in path[:-1]:
        target = target[key]
    target[path[-1]] = value


def user_passwords(data):
    return {
        user["username"]: user.get("password")
        for user in data.get("keycloak_config", {}).get("users", [])
    }


def user_password_fingerprints(data):
    return {
        username: hashlib.sha256((password or "").encode()).hexdigest()
        for username, password in user_passwords(data).items()
    }


def prepare(data):
    before_users = user_passwords(data)
    old_values = {".".join(path): value_at(data, path) for path in ROTATED_PATHS}
    old_harbor_admin = data["harbor"]["admin_password"]

    for path in ROTATED_PATHS:
        set_value(data, path, random_secret())

    data["harbor"]["previous_admin_password"] = old_harbor_admin
    data["harbor"]["rotate_admin_password"] = True
    data["gitea"]["rotate_admin_password"] = True
    data["gitea"]["restart_after_oidc_secret_rotation"] = True

    if user_passwords(data) != before_users:
        raise SystemExit("OIDC user passwords changed during technical secret rotation")

    return {
        "old": old_values,
        "new": {".".join(path): value_at(data, path) for path in ROTATED_PATHS},
        "oidc_user_password_sha256": user_password_fingerprints(data),
    }


def finalize(data, audit):
    if user_password_fingerprints(data) != audit["oidc_user_password_sha256"]:
        raise SystemExit("OIDC user passwords changed during technical secret rotation")
    data.get("harbor", {}).pop("previous_admin_password", None)
    data.get("harbor", {}).pop("rotate_admin_password", None)
    data.get("gitea", {}).pop("rotate_admin_password", None)
    data.get("gitea", {}).pop("restart_after_oidc_secret_rotation", None)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", choices=("prepare", "finalize"))
    parser.add_argument("secrets_file", type=Path)
    parser.add_argument("--audit-file", type=Path)
    args = parser.parse_args()

    data = yaml.safe_load(args.secrets_file.read_text())
    audit = None
    if args.mode == "prepare":
        audit = prepare(data)
        if args.audit_file is None:
            raise SystemExit("--audit-file is required in prepare mode")
    else:
        if args.audit_file is None or not args.audit_file.is_file():
            raise SystemExit("--audit-file is required in finalize mode")
        audit = json.loads(args.audit_file.read_text())
        finalize(data, audit)

    args.secrets_file.write_text(
        yaml.safe_dump(data, default_flow_style=False, sort_keys=False)
    )
    args.secrets_file.chmod(0o600)
    if args.mode == "prepare":
        args.audit_file.write_text(json.dumps(audit))
        args.audit_file.chmod(0o600)


if __name__ == "__main__":
    main()
