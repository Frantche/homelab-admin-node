#!/usr/bin/env python3

import datetime as dt
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("image_security_policy", REPO_ROOT / "scripts/image_security_policy.py")
assert SPEC and SPEC.loader
POLICY = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(POLICY)

IMAGE = "example.invalid/app:1@sha256:" + ("a" * 64)
TODAY = dt.date(2026, 7, 28)


def report(fixed_version: str = "1.2.4") -> dict:
    return {
        "Results": [{
            "Vulnerabilities": [{
                "VulnerabilityID": "CVE-2099-0001",
                "PkgName": "sentinel-package",
                "Severity": "CRITICAL",
                "FixedVersion": fixed_version,
            }]
        }]
    }


def policy(expires_at: str | None = None) -> dict:
    exceptions = []
    if expires_at:
        exceptions.append({
            "id": "temporary-sentinel",
            "image": IMAGE,
            "vulnerability": "CVE-2099-0001",
            "owner": "security@example.invalid",
            "justification": "Sentinel exception used by the policy contract test.",
            "expires_at": expires_at,
        })
    return {"version": 1, "blocked_severities": ["CRITICAL"], "exceptions": exceptions}


class ImageSecurityPolicyTests(unittest.TestCase):
    def test_repository_inventory_is_complete_and_digest_pinned(self) -> None:
        images = POLICY.inventory()
        self.assertGreater(len(images), 10)
        self.assertTrue(all("@sha256:" in image for image in images))

    def test_fixable_critical_vulnerability_is_blocked(self) -> None:
        self.assertEqual(
            POLICY.violations(report(), IMAGE, policy(), TODAY),
            ["CVE-2099-0001 (sentinel-package) fixable in 1.2.4"],
        )

    def test_unfixed_critical_vulnerability_is_not_blocked(self) -> None:
        self.assertEqual(POLICY.violations(report(""), IMAGE, policy(), TODAY), [])

    def test_active_exact_exception_is_accepted(self) -> None:
        self.assertEqual(POLICY.violations(report(), IMAGE, policy("2026-07-29"), TODAY), [])

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
