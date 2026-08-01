#!/usr/bin/env python3
"""Inventory container images and enforce the repository vulnerability policy."""

from __future__ import annotations

import argparse
import datetime as dt
import json
import re
import sys
from pathlib import Path
from typing import Any

REPO_ROOT = Path(__file__).resolve().parents[1]
DEFAULT_POLICY = REPO_ROOT / "security/image-vulnerability-policy.json"
DEFAULT_INVENTORY = REPO_ROOT / "security/container-images.txt"
IMAGE_WITH_DIGEST = re.compile(r"^[^\s]+:[^\s]+@sha256:[0-9a-f]{64}$")


def load_json(path: Path) -> Any:
    with path.open(encoding="utf-8") as handle:
        return json.load(handle)


def inventory(path: Path = DEFAULT_INVENTORY) -> list[str]:
    images = [
        line.strip()
        for line in path.read_text(encoding="utf-8").splitlines()
        if line.strip() and not line.lstrip().startswith("#")
    ]
    errors = []
    if len(images) != len(set(images)):
        errors.append("the image inventory must contain no duplicates")
    errors.extend(f"image is not pinned by tag and sha256 digest: {image}" for image in images if not IMAGE_WITH_DIGEST.match(image))

    declared = set()
    compose_files = list((REPO_ROOT / "stacks").glob("*/compose.yaml"))
    compose_files.extend((REPO_ROOT / "stacks").glob("*/compose.yaml.j2"))
    for compose in sorted(compose_files):
        for line in compose.read_text(encoding="utf-8").splitlines():
            if not re.match(r"^\s*image:", line):
                continue
            default = re.search(r"default\(['\"]([^'\"]+)['\"]\)", line)
            value = default.group(1) if default else line.split(":", 1)[1].strip().strip("\"'")
            declared.add(value)
    missing = sorted(declared.difference(images))
    errors.extend(f"Compose image missing from security/container-images.txt: {image}" for image in missing)

    utility_sources = {
        REPO_ROOT / "ansible/roles/docker/tasks/main.yml": "alpine:3.20@sha256:",
        REPO_ROOT / "ansible/roles/harbor/tasks/main.yml": "goharbor/prepare:v2.15.2@sha256:",
        REPO_ROOT / "scripts/gitea-process-backup.sh": "ghcr.io/frantche/gitea-backup-restore-process:0.3.6@sha256:",
    }
    for source, prefix in utility_sources.items():
        content = source.read_text(encoding="utf-8")
        matches = re.findall(re.escape(prefix) + r"[0-9a-f]{64}", content)
        if not matches:
            errors.append(f"pinned production utility image not found in {source.relative_to(REPO_ROOT)}: {prefix}")
        elif matches[0] not in images:
            errors.append(f"utility image missing from security/container-images.txt: {matches[0]}")

    if errors:
        raise ValueError("\n".join(errors))
    return images


def parse_date(value: str, field: str) -> dt.date:
    try:
        return dt.date.fromisoformat(value)
    except (TypeError, ValueError) as exc:
        raise ValueError(f"{field} must be an ISO date (YYYY-MM-DD)") from exc


def validate_policy(policy: dict[str, Any], today: dt.date) -> list[dict[str, Any]]:
    if policy.get("version") != 1:
        raise ValueError("policy version must be 1")
    if policy.get("blocked_severities") != ["CRITICAL"]:
        raise ValueError("blocked_severities must contain CRITICAL")
    exceptions = policy.get("exceptions")
    if not isinstance(exceptions, list):
        raise ValueError("exceptions must be a list")

    ids: set[str] = set()
    required = {"id", "image", "vulnerability", "owner", "justification", "expires_at"}
    for exception in exceptions:
        missing = required.difference(exception)
        if missing:
            raise ValueError(f"exception is missing fields: {', '.join(sorted(missing))}")
        if not all(isinstance(exception[field], str) and exception[field].strip() for field in required):
            raise ValueError("all exception fields must be non-empty strings")
        if exception["id"] in ids:
            raise ValueError(f"duplicate exception id: {exception['id']}")
        ids.add(exception["id"])
        if parse_date(exception["expires_at"], f"exception {exception['id']} expires_at") < today:
            raise ValueError(f"exception {exception['id']} expired on {exception['expires_at']}")
    return exceptions


def violations(report: dict[str, Any], image: str, policy: dict[str, Any], today: dt.date) -> list[str]:
    exceptions = validate_policy(policy, today)
    blocked = set(policy["blocked_severities"])
    found: list[str] = []
    for result in report.get("Results") or []:
        for vulnerability in result.get("Vulnerabilities") or []:
            severity = str(vulnerability.get("Severity", "")).upper()
            fixed_version = str(vulnerability.get("FixedVersion") or "").strip()
            vulnerability_id = str(vulnerability.get("VulnerabilityID") or "UNKNOWN")
            if severity not in blocked or not fixed_version:
                continue
            excepted = any(
                exception["image"] == image and exception["vulnerability"] == vulnerability_id
                for exception in exceptions
            )
            if not excepted:
                package = vulnerability.get("PkgName") or "unknown-package"
                found.append(f"{vulnerability_id} ({package}) fixable in {fixed_version}")
    return found


def main() -> int:
    parser = argparse.ArgumentParser()
    subparsers = parser.add_subparsers(dest="command", required=True)
    inventory_parser = subparsers.add_parser("inventory")
    inventory_parser.add_argument("--file", type=Path, default=DEFAULT_INVENTORY)
    validate_parser = subparsers.add_parser("validate")
    validate_parser.add_argument("--policy", type=Path, default=DEFAULT_POLICY)
    evaluate_parser = subparsers.add_parser("evaluate")
    evaluate_parser.add_argument("--policy", type=Path, default=DEFAULT_POLICY)
    evaluate_parser.add_argument("--report", type=Path, required=True)
    evaluate_parser.add_argument("--image", required=True)
    for subparser in (validate_parser, evaluate_parser):
        subparser.add_argument("--today", type=dt.date.fromisoformat, default=dt.date.today())
    args = parser.parse_args()

    try:
        if args.command == "inventory":
            print("\n".join(inventory(args.file)))
        elif args.command == "validate":
            validate_policy(load_json(args.policy), args.today)
        else:
            found = violations(load_json(args.report), args.image, load_json(args.policy), args.today)
            if found:
                print(f"{args.image}: blocked fixable vulnerabilities:", file=sys.stderr)
                for item in found:
                    print(f"- {item}", file=sys.stderr)
                return 1
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        print(f"image security policy error: {exc}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
