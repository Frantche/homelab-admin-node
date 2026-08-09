#!/usr/bin/env python3
import os
import re
from pathlib import Path

import yaml

PLACEHOLDER_KEY = "ssh-ed25519 AAAA_PLACEHOLDER_REPLACE_ME admin@example"


def main() -> None:
    vm_dir = Path(os.environ.get("CI_VM_DIR", ".ci/vm"))
    public_key_path = Path(
        os.environ.get("CI_SSH_PUBLIC_KEY", ".ci/ssh/id_ed25519.pub")
    )
    pubkey = public_key_path.read_text().strip()
    repo_url = os.environ["REPO_URL"]
    repo_ref = os.environ["REPO_REF"]
    arch_snapshot = os.environ.get("ARCH_PACKAGE_SNAPSHOT", "live")
    if re.fullmatch(r"[0-9a-f]{40}", repo_ref) is None:
        raise SystemExit("REPO_REF must be a lowercase full commit SHA")
    if arch_snapshot != "live" and re.fullmatch(r"[0-9]{4}/[0-9]{2}/[0-9]{2}", arch_snapshot) is None:
        raise SystemExit("ARCH_PACKAGE_SNAPSHOT must be live or YYYY/MM/DD")

    with Path("cloud-init/admin-01.user-data.yaml").open() as f:
        data = yaml.safe_load(f)

    bootcmd = data.get("bootcmd", [])
    replaced_snapshot = False
    for index, command in enumerate(bootcmd):
        if isinstance(command, str) and "ARCH_PACKAGE_SNAPSHOT_REPLACE_ME" in command:
            bootcmd[index] = command.replace("ARCH_PACKAGE_SNAPSHOT_REPLACE_ME", arch_snapshot)
            replaced_snapshot = True
    if not replaced_snapshot:
        raise SystemExit("Arch package snapshot placeholder not found")

    for user in data.get("users", []):
        keys = user.get("ssh_authorized_keys", [])
        user["ssh_authorized_keys"] = [
            pubkey if key == PLACEHOLDER_KEY else key for key in keys
        ]

    replaced_install = False
    new_runcmd = []
    for cmd in data.get("runcmd", []):
        if (
            isinstance(cmd, list)
            and len(cmd) == 3
            and cmd[0] == "/usr/local/bin/admin-node-install-release"
        ):
            new_runcmd.append([cmd[0], repo_url, repo_ref])
            replaced_install = True
        else:
            new_runcmd.append(cmd)

    if not replaced_install:
        raise SystemExit("release installer command not found in cloud-init template")

    data["runcmd"] = new_runcmd

    vm_dir.mkdir(parents=True, exist_ok=True)
    with (vm_dir / "user-data").open("w") as f:
        f.write("#cloud-config\n")
        yaml.dump(data, f, default_flow_style=False, allow_unicode=True)


if __name__ == "__main__":
    main()
