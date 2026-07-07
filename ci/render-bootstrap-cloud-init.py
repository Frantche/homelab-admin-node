#!/usr/bin/env python3
import os
from pathlib import Path

import yaml


PLACEHOLDER_KEY = "ssh-ed25519 AAAA_PLACEHOLDER_REPLACE_ME admin@example"


def main() -> None:
    pubkey = Path(".ci/ssh/id_ed25519.pub").read_text().strip()
    repo_url = os.environ["REPO_URL"]
    repo_branch = os.environ["REPO_BRANCH"]

    with Path("cloud-init/admin-01.user-data.yaml").open() as f:
        data = yaml.safe_load(f)

    for user in data.get("users", []):
        keys = user.get("ssh_authorized_keys", [])
        user["ssh_authorized_keys"] = [
            pubkey if key == PLACEHOLDER_KEY else key for key in keys
        ]

    new_runcmd = []
    for cmd in data.get("runcmd", []):
        if isinstance(cmd, list) and len(cmd) >= 3 and cmd[0] == "bash" and "REPO_URL" in cmd[-1]:
            script = cmd[-1]
            script = script.replace(
                "https://github.com/Frantche/homelab-admin-node.git",
                repo_url,
            )
            script = script.replace(
                'git clone "$REPO_URL"',
                f'git clone --branch {repo_branch} "$REPO_URL"',
            )
            new_runcmd.append(cmd[:-1] + [script])
        else:
            new_runcmd.append(cmd)

    data["runcmd"] = new_runcmd

    with Path(".ci/vm/user-data").open("w") as f:
        f.write("#cloud-config\n")
        yaml.dump(data, f, default_flow_style=False, allow_unicode=True)


if __name__ == "__main__":
    main()
