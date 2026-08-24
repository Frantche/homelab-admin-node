#!/usr/bin/env python3

import datetime as dt
import importlib.util
import json
import re
import tempfile
import unittest
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("image_security_policy", REPO_ROOT / "scripts/image_security_policy.py")
assert SPEC and SPEC.loader
POLICY = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(POLICY)

IMAGE_REPOSITORY = "ghcr.io/frantche/app"
IMAGE = IMAGE_REPOSITORY + ":1@sha256:" + ("a" * 64)
THIRD_PARTY_REPOSITORY = "example.invalid/app"
THIRD_PARTY_IMAGE = THIRD_PARTY_REPOSITORY + ":1@sha256:" + ("b" * 64)
TODAY = dt.date(2026, 7, 28)


def report(fixed_version: str = "1.2.4", vulnerability: str = "CVE-2099-0001") -> dict:
    return {
        "Results": [{
            "Vulnerabilities": [{
                "VulnerabilityID": vulnerability,
                "PkgName": "sentinel-package",
                "Severity": "CRITICAL",
                "FixedVersion": fixed_version,
            }]
        }]
    }


def policy(expires_at: str | None = None, repository: str = IMAGE_REPOSITORY) -> dict:
    exceptions = []
    if expires_at:
        exceptions.append({
            "id": "temporary-sentinel",
            "repository": repository,
            "vulnerability": "CVE-2099-0001",
            "owner": "security@example.invalid",
            "justification": "Sentinel exception used by the policy contract test.",
            "expires_at": expires_at,
        })
    return {
        "version": 3,
        "description": "Test policy",
        "enforced_repository_prefixes": ["ghcr.io/frantche/"],
        "blocked_severities": ["CRITICAL"],
        "exceptions": exceptions,
    }


def renovate_dependencies(path: str) -> list[dict[str, str]]:
    config = json.loads((REPO_ROOT / "renovate.json").read_text(encoding="utf-8"))
    content = (REPO_ROOT / path).read_text(encoding="utf-8")
    dependencies: list[dict[str, str]] = []
    for manager in config["customManagers"]:
        if not any(regex_matches_path(pattern, path) for pattern in manager["managerFilePatterns"]):
            continue
        for pattern in manager["matchStrings"]:
            python_pattern = pattern.replace("(?<", "(?P<")
            dependencies.extend(match.groupdict() for match in re.finditer(python_pattern, content))
    return dependencies


def regex_matches_path(pattern: str, path: str) -> bool:
    expression = pattern[1:-1] if pattern.startswith("/") and pattern.endswith("/") else pattern
    return re.search(expression, path) is not None


def dependency_for(path: str, repository: str) -> dict[str, str]:
    matches = [dependency for dependency in renovate_dependencies(path) if dependency["depName"] == repository]
    unique_matches = {
        (dependency["currentValue"], dependency["currentDigest"])
        for dependency in matches
    }
    if len(unique_matches) != 1:
        raise AssertionError(
            f"expected Renovate to find one consistent {repository} dependency in {path}, "
            f"found {len(matches)} occurrences with {len(unique_matches)} versions"
        )
    return matches[0]


class ImageSecurityPolicyTests(unittest.TestCase):
    def test_repository_inventory_is_complete_and_digest_pinned(self) -> None:
        images = POLICY.inventory()
        self.assertGreater(len(images), 10)
        self.assertTrue(all("@sha256:" in image for image in images))

    def test_inventory_is_the_canonical_utility_image_source(self) -> None:
        image = "ghcr.io/example/tool:12.34.56@sha256:" + ("a" * 64)
        self.assertEqual(
            POLICY.inventory_image_for_repository([image], "ghcr.io/example/tool"),
            image,
        )

    def test_inventory_requires_one_utility_image_per_repository(self) -> None:
        with self.assertRaisesRegex(ValueError, "exactly one alpine reference"):
            POLICY.inventory_image_for_repository([], "alpine")

    def test_utility_source_must_match_the_inventory_exactly(self) -> None:
        current = POLICY.DEFAULT_INVENTORY.read_text(encoding="utf-8")
        current_image = POLICY.inventory_image_for_repository(POLICY.inventory(), "alpine")
        current_digest = current_image.rsplit("@sha256:", 1)[1]
        replacement_digest = "a" * 64
        if current_digest == replacement_digest:
            replacement_digest = "b" * 64
        mismatched_image = (
            current_image.rsplit("@sha256:", 1)[0] + "@sha256:" + replacement_digest
        )
        mismatched = current.replace(current_image, mismatched_image)
        self.assertNotEqual(current, mismatched)
        with tempfile.TemporaryDirectory() as directory:
            inventory = Path(directory) / "container-images.txt"
            inventory.write_text(mismatched, encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "inventory utility image not found"):
                POLICY.inventory(inventory)

    def test_renovate_manages_utility_sources_and_inventory_together(self) -> None:
        inventory_path = "security/container-images.txt"
        sources = {
            "alpine": "ansible/roles/docker/defaults/main.yml",
            "goharbor/prepare": "ansible/roles/harbor/tasks/main.yml",
            "ghcr.io/frantche/gitea-backup-restore-process": "scripts/gitea-process-backup.sh",
        }
        for repository, source_path in sources.items():
            with self.subTest(repository=repository):
                source = dependency_for(source_path, repository)
                inventory = dependency_for(inventory_path, repository)
                self.assertEqual(source["currentValue"], inventory["currentValue"])
                self.assertEqual(source["currentDigest"], inventory["currentDigest"])

    def test_renovate_manages_every_gitea_backup_image_default(self) -> None:
        repository = "ghcr.io/frantche/gitea-backup-restore-process"
        inventory = dependency_for("security/container-images.txt", repository)
        for source_path in (
            "cmd/admin-node/main.go",
            "scripts/gitea-process-backup.sh",
            "ansible/roles/backup/templates/gitea-process-backup-env.j2",
        ):
            with self.subTest(source_path=source_path):
                source = dependency_for(source_path, repository)
                self.assertEqual(source["currentValue"], inventory["currentValue"])
                self.assertEqual(source["currentDigest"], inventory["currentDigest"])

    def test_fixable_critical_vulnerability_is_blocked(self) -> None:
        self.assertEqual(
            POLICY.violations(report(), IMAGE, policy(), TODAY),
            ["CVE-2099-0001 (sentinel-package) fixable in 1.2.4"],
        )

    def test_frantche_repository_is_strictly_enforced(self) -> None:
        current_policy = policy()
        POLICY.validate_policy(current_policy, TODAY)
        self.assertTrue(POLICY.repository_is_enforced(IMAGE_REPOSITORY, current_policy))

    def test_third_party_repository_is_not_strictly_enforced(self) -> None:
        current_policy = policy()
        POLICY.validate_policy(current_policy, TODAY)
        self.assertFalse(POLICY.repository_is_enforced(THIRD_PARTY_REPOSITORY, current_policy))
        self.assertEqual(
            POLICY.violations(report(), THIRD_PARTY_IMAGE, current_policy, TODAY),
            ["CVE-2099-0001 (sentinel-package) fixable in 1.2.4"],
        )

    def test_unfixed_critical_vulnerability_is_not_blocked(self) -> None:
        self.assertEqual(POLICY.violations(report(""), IMAGE, policy(), TODAY), [])

    def test_active_repository_exception_is_accepted(self) -> None:
        self.assertEqual(POLICY.violations(report(), IMAGE, policy("2026-07-29"), TODAY), [])

    def test_repository_exception_follows_tag_and_digest_updates(self) -> None:
        updated = IMAGE_REPOSITORY + ":2@sha256:" + ("b" * 64)
        self.assertEqual(POLICY.violations(report(), updated, policy("2026-07-29"), TODAY), [])

    def test_repository_exception_does_not_cover_another_repository(self) -> None:
        other = "example.invalid/other:1@sha256:" + ("b" * 64)
        self.assertEqual(
            POLICY.violations(report(), other, policy("2026-07-29"), TODAY),
            ["CVE-2099-0001 (sentinel-package) fixable in 1.2.4"],
        )

    def test_repository_exception_does_not_cover_another_vulnerability(self) -> None:
        self.assertEqual(
            POLICY.violations(
                report(vulnerability="CVE-2099-0002"),
                IMAGE,
                policy("2026-07-29"),
                TODAY,
            ),
            ["CVE-2099-0002 (sentinel-package) fixable in 1.2.4"],
        )

    def test_repository_exception_rejects_tags_and_digests(self) -> None:
        with self.assertRaisesRegex(ValueError, "without a tag or digest"):
            POLICY.validate_policy(policy("2026-07-29", IMAGE_REPOSITORY + ":1"), TODAY)

    def test_duplicate_repository_vulnerability_scope_is_rejected(self) -> None:
        duplicate = policy("2026-07-29")
        second = dict(duplicate["exceptions"][0])
        second["id"] = "duplicate-sentinel"
        duplicate["exceptions"].append(second)
        with self.assertRaisesRegex(ValueError, "duplicate exception scope"):
            POLICY.validate_policy(duplicate, TODAY)

    def test_expired_exception_is_rejected(self) -> None:
        with self.assertRaisesRegex(ValueError, "expired"):
            POLICY.violations(report(), IMAGE, policy("2026-07-27"), TODAY)

    def test_cli_never_accepts_malformed_json(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            malformed = Path(directory) / "report.json"
            malformed.write_text("{", encoding="utf-8")
            with self.assertRaises(json.JSONDecodeError):
                POLICY.load_json(malformed)


if __name__ == "__main__":
    unittest.main()
