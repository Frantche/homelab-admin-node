#!/usr/bin/env python3
"""Verify every Compose service has the common hardening baseline."""

import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DOC = (ROOT / "docs/container-hardening.md").read_text(encoding="utf-8")

USER_EXCEPTIONS = {
    "gitea-db",
    "gitea",
    "harbor-log",
    "harbor-db",
    "harbor-redis",
    "harbor-registry",
    "harbor-registryctl",
    "harbor-core",
    "harbor-portal",
    "harbor-jobservice",
    "harbor-trivy",
    "harbor-exporter",
    "harbor-nginx",
    "keycloak-db",
    "otel-mock-backend",
    "openbao",
    "traefik",
}

WRITE_EXCEPTIONS = {
    "gitea",
    "keycloak",
    "harbor-log",
    "harbor-db",
    "harbor-redis",
    "harbor-registry",
    "harbor-registryctl",
    "harbor-core",
    "harbor-portal",
    "harbor-jobservice",
    "harbor-trivy",
    "harbor-exporter",
    "harbor-nginx",
}


def services(path: Path) -> dict[str, str]:
    result: dict[str, list[str]] = {}
    current: str | None = None
    in_services = False
    for line in path.read_text(encoding="utf-8").splitlines():
        if line == "services:":
            in_services = True
            continue
        if in_services and re.match(r"^[A-Za-z]", line):
            break
        match = re.match(r"^  ([a-zA-Z0-9_-]+):$", line) if in_services else None
        if match:
            current = match.group(1)
            result[current] = []
        elif current:
            result[current].append(line)
    return {name: "\n".join(lines) for name, lines in result.items()}


def validation_errors() -> list[str]:
    errors: list[str] = []
    seen: set[str] = set()
    compose_files = list((ROOT / "stacks").glob("*/compose.yaml"))
    compose_files.extend((ROOT / "stacks").glob("*/compose.yaml.j2"))
    for compose in sorted(compose_files):
        for name, block in services(compose).items():
            seen.add(name)
            merged = "<<: *harbor-hardening" in block
            checks = {
                "cap_drop ALL": "cap_drop:" in block and re.search(r"^\s+- ALL$", block, re.MULTILINE),
                "no-new-privileges": "no-new-privileges:true" in block or merged,
                "pids_limit": "pids_limit:" in block or merged,
                "mem_limit": "mem_limit:" in block or merged,
                "cpus": "cpus:" in block or merged,
                "healthcheck": "healthcheck:" in block,
                "explicit user or exception": "user:" in block or name in USER_EXCEPTIONS,
                "read-only rootfs or exception": "read_only: true" in block or name in WRITE_EXCEPTIONS,
                "documented matrix row": f"| `{name}` |" in DOC,
            }
            for label, passed in checks.items():
                if not passed:
                    errors.append(f"{compose.relative_to(ROOT)}:{name}: missing {label}")

    unknown_user_exceptions = USER_EXCEPTIONS - seen
    unknown_write_exceptions = WRITE_EXCEPTIONS - seen
    if unknown_user_exceptions:
        errors.append(f"unknown user exceptions: {sorted(unknown_user_exceptions)}")
    if unknown_write_exceptions:
        errors.append(f"unknown write exceptions: {sorted(unknown_write_exceptions)}")

    openbao_compose = (ROOT / "stacks/openbao/compose.yaml.j2").read_text(encoding="utf-8")
    if '"{{ admin_node_root }}/backups/openbao-scratch:/openbao/snapshot"' not in openbao_compose:
        errors.append("stacks/openbao/compose.yaml.j2: missing dedicated snapshot scratch bind mount")

    openbao_tasks = (ROOT / "ansible/roles/openbao/tasks/main.yml").read_text(encoding="utf-8")
    for expected in (
        'path: "{{ admin_node_root }}/backups/openbao-scratch"',
        'owner: "100"',
        'group: "1000"',
        'mode: "0700"',
    ):
        if expected not in openbao_tasks:
            errors.append(f"ansible/roles/openbao/tasks/main.yml: snapshot scratch missing {expected}")
    return errors


class ContainerHardeningTests(unittest.TestCase):
    def test_all_services_have_the_documented_hardening_baseline(self) -> None:
        self.assertEqual(validation_errors(), [])


if __name__ == "__main__":
    unittest.main()
