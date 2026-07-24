#!/usr/bin/env python3
import os
import subprocess
import sys
import tempfile
from pathlib import Path

import yaml


def required_env(name: str) -> str:
    value = os.environ.get(name, "")
    if not value:
        raise SystemExit(f"{name} is required")
    return value


def main() -> None:
    config_repo = Path(sys.argv[1])
    encrypted_file = config_repo / "hosts/group_vars/secrets.sops.yaml"
    ci_vars_file = config_repo / "hosts/group_vars/ci-bootstrap-vars.yml"
    sops_env = os.environ.copy()
    sops_env.setdefault("SOPS_AGE_KEY_FILE", "/etc/sops/age/keys.txt")

    plain = subprocess.run(
        ["sops", "--decrypt", "--output-type", "yaml", str(encrypted_file)],
        check=True,
        env=sops_env,
        capture_output=True,
    ).stdout
    data = yaml.safe_load(plain)
    backup = data["backup"]
    backup["require_remote_repository"] = True
    backup["restic_repositories"] = [
        repo
        for repo in backup.get("restic_repositories", [])
        if repo["name"] != "offsite"
    ]
    backup["restic_repositories"].append(
        {
            "name": "offsite",
            "repository": required_env("CI_RESTIC_OFFSITE_ENDPOINT"),
            "password": required_env("CI_RESTIC_OFFSITE_PASSWORD"),
            "forget_args": "none",
            "options": f'--cacert {required_env("CI_RESTIC_OFFSITE_CACERT")}',
            "env": {
                "AWS_ACCESS_KEY_ID": required_env(
                    "CI_RESTIC_OFFSITE_ACCESS_KEY"
                ),
                "AWS_SECRET_ACCESS_KEY": required_env(
                    "CI_RESTIC_OFFSITE_SECRET_KEY"
                ),
                "AWS_DEFAULT_REGION": "garage",
            },
        }
    )

    with tempfile.NamedTemporaryFile(
        mode="w",
        dir=encrypted_file.parent,
        prefix=".secrets.sops.",
        suffix=".yaml",
        delete=False,
    ) as output:
        output_path = Path(output.name)
        os.chmod(output_path, 0o600)
        subprocess.run(
            [
                "sops",
                "--config",
                str(config_repo / ".sops.yaml"),
                "--encrypt",
                "--input-type",
                "yaml",
                "--output-type",
                "yaml",
                "--filename-override",
                "hosts/group_vars/secrets.sops.yaml",
                "/dev/stdin",
            ],
            check=True,
            env=sops_env,
            input=yaml.safe_dump(data, sort_keys=False),
            text=True,
            stdout=output,
        )

    os.replace(output_path, encrypted_file)
    os.chmod(encrypted_file, 0o600)

    ci_vars = yaml.safe_load(ci_vars_file.read_text())
    ci_vars.pop("backup", None)
    ci_vars_file.write_text(yaml.safe_dump(ci_vars, sort_keys=False))

    subprocess.run(
        [
            "git",
            "-C",
            str(config_repo),
            "add",
            str(encrypted_file.relative_to(config_repo)),
            str(ci_vars_file.relative_to(config_repo)),
        ],
        check=True,
    )
    subprocess.run(
        [
            "git",
            "-C",
            str(config_repo),
            "-c",
            "user.name=CI Admin",
            "-c",
            "user.email=ci@example.com",
            "commit",
            "-m",
            "Configure CI offsite backup",
        ],
        check=True,
    )


if __name__ == "__main__":
    main()
