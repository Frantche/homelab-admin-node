#!/usr/bin/env python3
"""Validate that a release qualification manifest is complete and commit-bound."""

import argparse
import json
import re
import subprocess
from pathlib import Path

import yaml

SHA256 = re.compile(r"^[0-9a-f]{64}$")
COMMIT = re.compile(r"^[0-9a-f]{40}$")
RELEASE_TAG = re.compile(r"^v[0-9]+\.[0-9]+\.[0-9]+$")
IMAGE_LINE = re.compile(r"^\s*image:\s*[\"']?([^\"'\s]+)", re.MULTILINE)
DIGEST_IMAGE = re.compile(r"[a-zA-Z0-9./_-]+:[^\"'\s]+@sha256:[0-9a-f]{64}")


def git_revision(ref: str) -> str:
    return subprocess.check_output(
        ["git", "rev-parse", f"{ref}^{{commit}}"], text=True
    ).strip()


def require(mapping: dict, path: str):
    value = mapping
    for part in path.split("."):
        if not isinstance(value, dict) or part not in value:
            raise ValueError(f"missing required field: {path}")
        value = value[part]
    if value in (None, "", []):
        raise ValueError(f"empty required field: {path}")
    return value


def validate(path: Path, expected_commit: str) -> None:
    document = json.loads(path.read_text())
    commit = require(document, "release.commit")
    tag = require(document, "release.tag")
    if not COMMIT.fullmatch(commit):
        raise ValueError("release.commit must be a lowercase full Git SHA")
    if commit != expected_commit:
        raise ValueError(f"manifest commit {commit} does not match {expected_commit}")
    if not RELEASE_TAG.fullmatch(tag):
        raise ValueError("release.tag must use immutable vMAJOR.MINOR.PATCH syntax")
    tag_type = subprocess.check_output(
        ["git", "cat-file", "-t", f"refs/tags/{tag}"], text=True
    ).strip()
    if tag_type != "tag":
        raise ValueError("release tag must be annotated")
    if git_revision(tag) != commit:
        raise ValueError(f"tag {tag} does not resolve to manifest commit {commit}")

    schema = require(document, "release.config_schema")
    repository_schema = Path("release/config-schema-version").read_text().strip()
    if schema != repository_schema:
        raise ValueError(
            f"manifest config schema {schema} does not match repository {repository_schema}"
        )
    require(document, "release.qualified_at")
    require(document, "platform.arch_image_url")
    image_checksum = require(document, "platform.arch_image_sha256")
    if not SHA256.fullmatch(image_checksum):
        raise ValueError("platform.arch_image_sha256 must be a lowercase SHA-256")
    require(document, "platform.package_strategy")
    require(document, "platform.package_state_artifact")
    require(document, "dependencies.ansible_core")
    require(document, "dependencies.python")
    collections = require(document, "dependencies.collections")
    recorded_collections = {item.get("name"): item.get("version") for item in collections}
    requirements = yaml.safe_load(Path("ansible/requirements.yml").read_text())
    expected_collections = {
        item["name"]: item["version"] for item in requirements["collections"]
    }
    if recorded_collections != expected_collections:
        raise ValueError("Ansible collection versions do not match requirements.yml")

    images = require(document, "container_images")
    if not all(re.search(r"@sha256:[0-9a-f]{64}$", image) for image in images):
        raise ValueError("every container image must be pinned by sha256 digest")
    repository_images = set()
    for compose in Path("stacks").glob("*/compose.yaml*"):
        repository_images.update(IMAGE_LINE.findall(compose.read_text()))
    repository_images.update(
        DIGEST_IMAGE.findall(
            Path("ansible/roles/docker/defaults/main.yml").read_text()
        )
    )
    missing_images = sorted(repository_images - set(images))
    if missing_images:
        raise ValueError(
            "manifest does not record repository container images: "
            + ", ".join(missing_images)
        )

    bootstrap = require(document, "evidence.bootstrap")
    if len(bootstrap) != 2:
        raise ValueError("exactly two fresh-node bootstrap evidence entries are required")
    inventories = set()
    for item in bootstrap:
        if item.get("commit") != commit or item.get("result") != "passed":
            raise ValueError("bootstrap evidence must pass for the exact release commit")
        require(item, "artifact")
        inventory = require(item, "component_inventory_sha256")
        if not SHA256.fullmatch(inventory):
            raise ValueError("component inventory checksum must be SHA-256")
        inventories.add(inventory)
    if len(inventories) != 1:
        raise ValueError("fresh-node component inventories do not match")

    dr = require(document, "evidence.disaster_recovery")
    if dr.get("commit") != commit or dr.get("result") != "passed":
        raise ValueError("disaster-recovery evidence must pass for the exact commit")
    require(dr, "artifact")
    for name in ("upgrade", "rollback"):
        evidence = require(document, f"evidence.{name}")
        if evidence.get("result") != "passed":
            raise ValueError(f"{name} evidence must pass")
        require(evidence, "artifact")
    require(document, "evidence.upgrade.from_release")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("manifest", type=Path)
    parser.add_argument("--commit", default="HEAD")
    args = parser.parse_args()
    try:
        validate(args.manifest, git_revision(args.commit))
    except (ValueError, OSError, json.JSONDecodeError, subprocess.CalledProcessError) as exc:
        raise SystemExit(f"release qualification invalid: {exc}") from exc
    print(f"release qualification valid: {args.manifest}")


if __name__ == "__main__":
    main()
