#!/usr/bin/env python3
"""Negative contracts for the release qualification validator."""

import importlib.util
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location(
    "release_qualification", ROOT / "scripts/validate-release-qualification.py"
)
VALIDATOR = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(VALIDATOR)

COMMIT = "a" * 40
CHECKSUM = "b" * 64


class ReleaseQualificationTest(unittest.TestCase):
    def setUp(self) -> None:
        self.tempdir = tempfile.TemporaryDirectory()
        self.addCleanup(self.tempdir.cleanup)
        self.root = Path(self.tempdir.name)
        (self.root / "release").mkdir()
        (self.root / "release/config-schema-version").write_text("1\n", encoding="utf-8")
        (self.root / "ansible/roles/docker/defaults").mkdir(parents=True)
        (self.root / "ansible/roles/docker/defaults/main.yml").write_text("", encoding="utf-8")
        (self.root / "ansible/requirements.yml").write_text(
            "collections:\n  - name: ansible.posix\n    version: '2.2.0'\n",
            encoding="utf-8",
        )
        stack = self.root / "stacks/example"
        stack.mkdir(parents=True)
        self.image = "example/service:1@sha256:" + "c" * 64
        (stack / "compose.yaml").write_text(
            f"services:\n  app:\n    image: {self.image}\n", encoding="utf-8"
        )
        self.manifest = self.valid_manifest()
        self.path = self.root / "qualification.json"
        self.previous_cwd = Path.cwd()
        os.chdir(self.root)
        self.addCleanup(os.chdir, self.previous_cwd)

    def valid_manifest(self) -> dict:
        def evidence(url: str) -> dict:
            return {
                "commit": COMMIT,
                "result": "passed",
                "artifact": url,
            }

        return {
            "release": {
                "tag": "v1.2.3",
                "commit": COMMIT,
                "config_schema": "1",
                "qualified_at": "2026-08-09T12:00:00Z",
            },
            "platform": {
                "arch_image_url": "https://example.test/images/2026.08.01/arch.qcow2",
                "arch_image_sha256": CHECKSUM,
                "package_repository_snapshot": "2026/08/09",
                "package_strategy": "Arch Linux Archive snapshot",
                "package_state_artifact": "https://example.test/pacman-Q.txt",
                "package_state_sha256": CHECKSUM,
            },
            "dependencies": {
                "ansible_core": "2.20.0",
                "python": "3.14.0",
                "collections": [{"name": "ansible.posix", "version": "2.2.0"}],
            },
            "container_images": [self.image],
            "evidence": {
                "bootstrap": [
                    {
                        **evidence("https://example.test/bootstrap-a"),
                        "node": "node-a",
                        "component_inventory_sha256": CHECKSUM,
                    },
                    {
                        **evidence("https://example.test/bootstrap-b"),
                        "node": "node-b",
                        "component_inventory_sha256": CHECKSUM,
                    },
                ],
                "disaster_recovery": [
                    {**evidence("https://example.test/dr-standard"), "variant": "standard"},
                    {**evidence("https://example.test/dr-offline"), "variant": "offline-images"},
                ],
                "upgrade": {
                    **evidence("https://example.test/upgrade"),
                    "from_release": "v1.2.2",
                },
                "rollback": evidence("https://example.test/rollback"),
            },
        }

    def validate(self) -> None:
        self.path.write_text(json.dumps(self.manifest), encoding="utf-8")

        def git_output(args, text=True):
            del text
            if args[1:3] == ["cat-file", "-t"]:
                return "tag\n"
            if args[1] == "rev-parse":
                return COMMIT + "\n"
            raise AssertionError(args)

        with mock.patch.object(VALIDATOR.subprocess, "check_output", side_effect=git_output):
            VALIDATOR.validate(self.path, COMMIT)

    def test_accepts_commit_bound_release(self) -> None:
        self.validate()

    def test_rejects_upgrade_or_rollback_evidence_for_another_commit(self) -> None:
        for name in ("upgrade", "rollback"):
            with self.subTest(name=name):
                self.manifest["evidence"][name]["commit"] = "d" * 40
                with self.assertRaisesRegex(ValueError, "exact release commit"):
                    self.validate()
                self.manifest["evidence"][name]["commit"] = COMMIT

    def test_rejects_moving_package_repository(self) -> None:
        self.manifest["platform"]["package_repository_snapshot"] = "live"
        with self.assertRaisesRegex(ValueError, "YYYY/MM/DD"):
            self.validate()

    def test_rejects_moving_cloud_image(self) -> None:
        self.manifest["platform"]["arch_image_url"] = "https://example.test/images/latest/arch.qcow2"
        with self.assertRaisesRegex(ValueError, "dated"):
            self.validate()

    def test_rejects_upgrade_from_same_or_newer_release(self) -> None:
        self.manifest["evidence"]["upgrade"]["from_release"] = "v1.2.3"
        with self.assertRaisesRegex(ValueError, "older"):
            self.validate()


if __name__ == "__main__":
    unittest.main()
