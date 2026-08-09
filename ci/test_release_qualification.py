#!/usr/bin/env python3
"""Negative contracts for the release qualification validator."""

import hashlib
import importlib.util
import io
import json
import os
import tempfile
import unittest
import zipfile
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
        (self.root / "release/arch-package-snapshot").write_text("2026/08/09\n", encoding="utf-8")
        (self.root / "go.mod").write_text("module example.test/release\n\ngo 1.26.5\n")
        (self.root / "ansible/roles/docker/defaults").mkdir(parents=True)
        (self.root / "ansible/roles/docker/defaults/main.yml").write_text("", encoding="utf-8")
        (self.root / "ansible/requirements.yml").write_text(
            "collections:\n  - name: ansible.posix\n    version: '2.2.0'\n",
            encoding="utf-8",
        )
        stack = self.root / "stacks/example"
        stack.mkdir(parents=True)
        self.image = "example/service:1@sha256:" + "c" * 64
        (stack / "compose.yaml").write_text(f"services:\n  app:\n    image: {self.image}\n", encoding="utf-8")
        self.manifest = self.valid_manifest()
        self.path = self.root / "qualification.json"
        self.previous_cwd = Path.cwd()
        os.chdir(self.root)
        self.addCleanup(os.chdir, self.previous_cwd)

    def valid_manifest(self) -> dict:
        run_id = 100

        def evidence(url: str) -> dict:
            nonlocal run_id
            run_id += 1
            return {
                "commit": COMMIT,
                "result": "passed",
                "artifact": url,
                "artifact_sha256": CHECKSUM,
                "workflow_run_id": run_id,
                "workflow_run_api": f"https://example.test/actions/runs/{run_id}",
                "workflow_path": ".github/workflows/bootstrap-user-journey.yml",
                "workflow_source_sha": "d" * 40,
                "workflow_artifact_id": run_id + 1000,
                "workflow_artifact_api": f"https://example.test/actions/artifacts/{run_id + 1000}",
                "workflow_artifact_name": f"qualification-evidence-{run_id}",
            }

        return {
            "release": {
                "tag": "v1.2.3",
                "commit": COMMIT,
                "config_schema": "1",
                "qualified_at": "2026-08-09T12:00:00Z",
            },
            "platform": {
                "arch_image_url": "https://geo.mirror.pkgbuild.com/images/2026.08.01/arch.qcow2",
                "arch_image_sha256": CHECKSUM,
                "package_repository_snapshot": "2026/08/09",
                "package_strategy": "Arch Linux Archive snapshot",
                "package_state_artifact": "https://example.test/pacman-Q.txt",
                "package_state_sha256": CHECKSUM,
            },
            "dependencies": {
                "ansible_core": "2.20.0",
                "python": "3.14.0",
                "go": "1.26.5",
                "collections": [{"name": "ansible.posix", "version": "2.2.0"}],
            },
            "container_images": [self.image],
            "evidence": {
                "bootstrap": [
                    {
                        **evidence("https://example.test/bootstrap-a"),
                        "node": "node-a",
                        "component_inventory_sha256": CHECKSUM,
                        "component_inventory_artifact": "https://example.test/inventory-a.txt",
                    },
                    {
                        **evidence("https://example.test/bootstrap-b"),
                        "node": "node-b",
                        "component_inventory_sha256": CHECKSUM,
                        "component_inventory_artifact": "https://example.test/inventory-b.txt",
                    },
                ],
                "disaster_recovery": [
                    {**evidence("https://example.test/dr-standard"), "variant": "standard"},
                    {**evidence("https://example.test/dr-offline"), "variant": "offline-images"},
                ],
                "upgrade": {
                    **evidence("https://example.test/upgrade"),
                    "from_release": "v1.2.2",
                    "component_inventory_sha256": CHECKSUM,
                    "component_inventory_artifact": "https://example.test/inventory-upgrade.txt",
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

    def test_rejects_snapshot_or_go_toolchain_not_bound_to_repository(self) -> None:
        self.manifest["platform"]["package_repository_snapshot"] = "2026/08/10"
        with self.assertRaisesRegex(ValueError, "does not match repository"):
            self.validate()
        self.manifest["platform"]["package_repository_snapshot"] = "2026/08/09"
        self.manifest["dependencies"]["go"] = "1.27.0"
        with self.assertRaisesRegex(ValueError, "go.mod"):
            self.validate()

    def test_rejects_evidence_without_content_checksum(self) -> None:
        del self.manifest["evidence"]["rollback"]["artifact_sha256"]
        with self.assertRaisesRegex(ValueError, "artifact_sha256"):
            self.validate()

    def test_rejects_self_attestation_without_workflow_provenance(self) -> None:
        del self.manifest["evidence"]["rollback"]["workflow_run_id"]
        with self.assertRaisesRegex(ValueError, "workflow_run_id"):
            self.validate()

    def test_rejects_moving_cloud_image(self) -> None:
        self.manifest["platform"]["arch_image_url"] = "https://geo.mirror.pkgbuild.com/images/latest/arch.qcow2"
        with self.assertRaisesRegex(ValueError, "dated"):
            self.validate()

    def test_rejects_upgrade_from_same_or_newer_release(self) -> None:
        self.manifest["evidence"]["upgrade"]["from_release"] = "v1.2.3"
        with self.assertRaisesRegex(ValueError, "older"):
            self.validate()

    def test_rejects_upgrade_inventory_different_from_fresh_nodes(self) -> None:
        self.manifest["evidence"]["upgrade"]["component_inventory_sha256"] = "d" * 64
        with self.assertRaisesRegex(ValueError, "does not match fresh nodes"):
            self.validate()

    def test_remote_verification_checks_bytes_and_evidence_claims(self) -> None:
        image = b"qualified image"
        packages = b"ansible 2.20.0\npython 3.14.0\n"
        component_inventory = b"qualified components\n"
        self.manifest["platform"]["arch_image_sha256"] = hashlib.sha256(image).hexdigest()
        self.manifest["platform"]["package_state_sha256"] = hashlib.sha256(packages).hexdigest()
        payloads = {
            self.manifest["platform"]["arch_image_url"]: image,
            self.manifest["platform"]["package_state_artifact"]: packages,
        }
        evidence_entries = [
            *(("bootstrap", item) for item in self.manifest["evidence"]["bootstrap"]),
            *(("disaster_recovery", item) for item in self.manifest["evidence"]["disaster_recovery"]),
            ("upgrade", self.manifest["evidence"]["upgrade"]),
            ("rollback", self.manifest["evidence"]["rollback"]),
        ]
        for _kind, entry in evidence_entries:
            payloads[entry["workflow_run_api"]] = json.dumps(
                {
                    "id": entry["workflow_run_id"],
                    "head_sha": entry["workflow_source_sha"],
                    "conclusion": "success",
                    "path": entry["workflow_path"],
                    "event": "workflow_dispatch",
                }
            ).encode()
            if "component_inventory_sha256" in entry:
                entry["component_inventory_sha256"] = hashlib.sha256(component_inventory).hexdigest()
                payloads[entry["component_inventory_artifact"]] = component_inventory

        for kind, entry in evidence_entries:
            claims = {"kind": kind}
            for name in (
                "commit",
                "result",
                "node",
                "component_inventory_sha256",
                "variant",
                "from_release",
                "workflow_run_id",
                "workflow_path",
                "workflow_source_sha",
                "workflow_artifact_id",
                "workflow_artifact_name",
            ):
                if name in entry:
                    claims[name] = entry[name]
            payload = json.dumps(claims, sort_keys=True).encode()
            entry["artifact_sha256"] = hashlib.sha256(payload).hexdigest()
            payloads[entry["artifact"]] = payload
            archive = io.BytesIO()
            with zipfile.ZipFile(archive, "w") as bundle:
                bundle.writestr("evidence.json", payload)
                if "component_inventory_sha256" in entry:
                    bundle.writestr("component-inventory.txt", component_inventory)
            archive_url = entry["workflow_artifact_api"] + "/zip"
            payloads[entry["workflow_artifact_api"]] = json.dumps(
                {
                    "id": entry["workflow_artifact_id"],
                    "name": entry["workflow_artifact_name"],
                    "expired": False,
                    "archive_download_url": archive_url,
                    "workflow_run": {
                        "id": entry["workflow_run_id"],
                        "head_sha": entry["workflow_source_sha"],
                    },
                }
            ).encode()
            payloads[archive_url] = archive.getvalue()

        class Response:
            def __init__(self, url: str, content: bytes):
                self.url = url
                self.stream = io.BytesIO(content)

            def __enter__(self):
                return self

            def __exit__(self, *_args):
                return None

            def geturl(self):
                return self.url

            def read(self, size: int):
                return self.stream.read(size)

        def open_request(request, timeout):
            self.assertEqual(timeout, 120)
            return Response(request.full_url, payloads[request.full_url])

        self.path.write_text(json.dumps(self.manifest), encoding="utf-8")
        with (
            mock.patch.object(
                VALIDATOR.subprocess,
                "check_output",
                side_effect=lambda args, text=True: "tag\n" if args[1:3] == ["cat-file", "-t"] else COMMIT + "\n",
            ),
            mock.patch.object(VALIDATOR.subprocess, "run", return_value=mock.Mock(returncode=0)),
            mock.patch.object(VALIDATOR, "urlopen", side_effect=open_request),
        ):
            VALIDATOR.validate(
                self.path,
                COMMIT,
                verify_remote=True,
                trusted_evidence_prefix="https://example.test/",
            )

        with (
            mock.patch.object(
                VALIDATOR.subprocess,
                "check_output",
                side_effect=lambda args, text=True: "tag\n" if args[1:3] == ["cat-file", "-t"] else COMMIT + "\n",
            ),
            mock.patch.object(VALIDATOR.subprocess, "run", return_value=mock.Mock(returncode=1)),
            mock.patch.object(VALIDATOR, "urlopen", side_effect=open_request),
            self.assertRaisesRegex(ValueError, "trusted main history"),
        ):
            VALIDATOR.validate(
                self.path,
                COMMIT,
                verify_remote=True,
                trusted_evidence_prefix="https://example.test/",
            )

        self.manifest["evidence"]["rollback"]["artifact_sha256"] = "0" * 64
        self.path.write_text(json.dumps(self.manifest), encoding="utf-8")
        with (
            mock.patch.object(
                VALIDATOR.subprocess,
                "check_output",
                side_effect=lambda args, text=True: "tag\n" if args[1:3] == ["cat-file", "-t"] else COMMIT + "\n",
            ),
            mock.patch.object(VALIDATOR.subprocess, "run", return_value=mock.Mock(returncode=0)),
            mock.patch.object(VALIDATOR, "urlopen", side_effect=open_request),
            self.assertRaisesRegex(ValueError, "checksum"),
        ):
            VALIDATOR.validate(
                self.path,
                COMMIT,
                verify_remote=True,
                trusted_evidence_prefix="https://example.test/",
            )


if __name__ == "__main__":
    unittest.main()
