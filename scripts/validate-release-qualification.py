#!/usr/bin/env python3
"""Validate that a release qualification manifest is complete and commit-bound."""

import argparse
import hashlib
import json
import os
import re
import subprocess
import zipfile
from datetime import datetime
from io import BytesIO
from pathlib import Path
from urllib.parse import urlparse
from urllib.request import Request, urlopen

import yaml

SHA256 = re.compile(r"^[0-9a-f]{64}$")
COMMIT = re.compile(r"^[0-9a-f]{40}$")
RELEASE_TAG = re.compile(r"^v[0-9]+\.[0-9]+\.[0-9]+$")
VERSION = re.compile(r"^[0-9]+(?:\.[0-9]+){1,3}(?:[-+][0-9A-Za-z.-]+)?$")
ARCH_SNAPSHOT = re.compile(r"^[0-9]{4}/[0-9]{2}/[0-9]{2}$")
ARCH_IMAGE_DATE = re.compile(r"/images/([0-9]{4}\.[0-9]{2}\.[0-9]{2})/")
IMAGE_LINE = re.compile(r"^\s*image:\s*[\"']?([^\"'\s]+)", re.MULTILINE)
DIGEST_IMAGE = re.compile(r"[a-zA-Z0-9./_-]+:[^\"'\s]+@sha256:[0-9a-f]{64}")
DEFAULT_TRUSTED_EVIDENCE_PREFIX = "https://github.com/Frantche/homelab-admin-node/"
MAX_EVIDENCE_BYTES = 2 * 1024 * 1024
MAX_ACTION_ARTIFACT_BYTES = 100 * 1024 * 1024


def git_revision(ref: str) -> str:
    return subprocess.check_output(["git", "rev-parse", f"{ref}^{{commit}}"], text=True).strip()


def require(mapping: dict, path: str):
    value = mapping
    for part in path.split("."):
        if not isinstance(value, dict) or part not in value:
            raise ValueError(f"missing required field: {path}")
        value = value[part]
    if value in (None, "", []):
        raise ValueError(f"empty required field: {path}")
    return value


def require_https(value: str, path: str) -> None:
    parsed = urlparse(value)
    if parsed.scheme != "https" or not parsed.netloc:
        raise ValueError(f"{path} must be an absolute HTTPS URL")


def require_artifact_checksum(mapping: dict, path: str) -> str:
    checksum = require(mapping, "artifact_sha256")
    if not SHA256.fullmatch(checksum):
        raise ValueError(f"{path}.artifact_sha256 must be a lowercase SHA-256")
    return checksum


def release_version(value: str) -> tuple[int, int, int]:
    return tuple(int(part) for part in value[1:].split("."))


def download_verified(
    url: str,
    expected_sha256: str,
    label: str,
    *,
    capture: bool = False,
    max_bytes: int | None = None,
) -> bytes | None:
    headers = {"User-Agent": "homelab-release-qualification/1"}
    if token := os.environ.get("GITHUB_TOKEN"):
        headers["Authorization"] = f"Bearer {token}"
        headers["X-GitHub-Api-Version"] = "2022-11-28"
    request = Request(url, headers=headers)
    digest = hashlib.sha256()
    collected = bytearray() if capture else None
    size = 0
    with urlopen(request, timeout=120) as response:
        final_url = response.geturl()
        require_https(final_url, f"{label} redirected URL")
        while chunk := response.read(1024 * 1024):
            size += len(chunk)
            if max_bytes is not None and size > max_bytes:
                raise ValueError(f"{label} exceeds the {max_bytes}-byte verification limit")
            digest.update(chunk)
            if collected is not None:
                collected.extend(chunk)
    actual = digest.hexdigest()
    if actual != expected_sha256:
        raise ValueError(f"{label} checksum {actual} does not match manifest {expected_sha256}")
    return bytes(collected) if collected is not None else None


def download_json(url: str, label: str) -> dict:
    request = Request(
        url,
        headers={"Accept": "application/vnd.github+json", "User-Agent": "homelab-release-qualification/1"},
    )
    if token := os.environ.get("GITHUB_TOKEN"):
        request.add_header("Authorization", f"Bearer {token}")
        request.add_header("X-GitHub-Api-Version", "2022-11-28")
    with urlopen(request, timeout=120) as response:
        require_https(response.geturl(), f"{label} redirected URL")
        content = response.read(MAX_EVIDENCE_BYTES + 1)
    if len(content) > MAX_EVIDENCE_BYTES:
        raise ValueError(f"{label} exceeds the {MAX_EVIDENCE_BYTES}-byte verification limit")
    value = json.loads(content)
    if not isinstance(value, dict):
        raise ValueError(f"{label} must contain one JSON object")
    return value


def download_bytes(url: str, label: str, max_bytes: int) -> bytes:
    headers = {"Accept": "application/vnd.github+json", "User-Agent": "homelab-release-qualification/1"}
    if token := os.environ.get("GITHUB_TOKEN"):
        headers["Authorization"] = f"Bearer {token}"
        headers["X-GitHub-Api-Version"] = "2022-11-28"
    request = Request(url, headers=headers)
    with urlopen(request, timeout=120) as response:
        require_https(response.geturl(), f"{label} redirected URL")
        content = response.read(max_bytes + 1)
    if len(content) > max_bytes:
        raise ValueError(f"{label} exceeds the {max_bytes}-byte verification limit")
    return content


def workflow_api_prefix(trusted_prefix: str) -> str:
    parsed = urlparse(trusted_prefix)
    if parsed.hostname == "github.com":
        repository = parsed.path.strip("/")
        if repository.count("/") != 1:
            raise ValueError("trusted GitHub prefix must identify exactly one repository")
        return f"https://api.github.com/repos/{repository}/actions/runs/"
    return trusted_prefix + "actions/runs/"


def artifact_api_prefix(trusted_prefix: str) -> str:
    parsed = urlparse(trusted_prefix)
    if parsed.hostname == "github.com":
        repository = parsed.path.strip("/")
        return f"https://api.github.com/repos/{repository}/actions/artifacts/"
    return trusted_prefix + "actions/artifacts/"


def require_provenance(entry: dict) -> None:
    for field in (
        "workflow_run_id",
        "workflow_run_api",
        "workflow_path",
        "workflow_source_sha",
        "workflow_artifact_id",
        "workflow_artifact_api",
        "workflow_artifact_name",
    ):
        require(entry, field)
    if not COMMIT.fullmatch(entry["workflow_source_sha"]):
        raise ValueError("evidence workflow_source_sha must be a full Git commit")
    for field in ("workflow_run_id", "workflow_artifact_id"):
        if not isinstance(entry[field], int) or entry[field] <= 0:
            raise ValueError(f"evidence {field} must be a positive integer")
    require_https(entry["workflow_run_api"], "evidence.workflow_run_api")
    require_https(entry["workflow_artifact_api"], "evidence.workflow_artifact_api")


def verify_workflow_run(entry: dict, trusted_prefix: str) -> None:
    run_id = require(entry, "workflow_run_id")
    if not isinstance(run_id, int) or run_id <= 0:
        raise ValueError("evidence workflow_run_id must be a positive integer")
    workflow_path = require(entry, "workflow_path")
    if workflow_path != ".github/workflows/bootstrap-user-journey.yml":
        raise ValueError("evidence must come from the trusted qualification workflow")
    run_api = require(entry, "workflow_run_api")
    expected_url = workflow_api_prefix(trusted_prefix) + str(run_id)
    if run_api != expected_url:
        raise ValueError(f"evidence workflow run API must be {expected_url}")
    run = download_json(run_api, "GitHub Actions workflow run")
    if (
        run.get("id") != run_id
        or run.get("head_sha") != entry["workflow_source_sha"]
        or run.get("conclusion") != "success"
        or run.get("path") != workflow_path
        or run.get("event") != "workflow_dispatch"
    ):
        raise ValueError("evidence workflow run identity, commit, event, path, or conclusion is invalid")
    source_check = subprocess.run(
        ["git", "merge-base", "--is-ancestor", entry["workflow_source_sha"], "origin/main"],
        check=False,
        capture_output=True,
    )
    if source_check.returncode != 0:
        raise ValueError("evidence workflow source is not from trusted main history")


def verify_workflow_artifact(entry: dict, trusted_prefix: str, required_digests: set[str]) -> None:
    artifact_id = entry["workflow_artifact_id"]
    artifact_api = entry["workflow_artifact_api"]
    expected_api = artifact_api_prefix(trusted_prefix) + str(artifact_id)
    if artifact_api != expected_api:
        raise ValueError(f"evidence workflow artifact API must be {expected_api}")
    metadata = download_json(artifact_api, "GitHub Actions artifact metadata")
    archive_url = expected_api + "/zip"
    workflow_run = metadata.get("workflow_run")
    if (
        metadata.get("id") != artifact_id
        or metadata.get("name") != entry["workflow_artifact_name"]
        or metadata.get("expired") is not False
        or metadata.get("archive_download_url") != archive_url
        or not isinstance(workflow_run, dict)
        or workflow_run.get("id") != entry["workflow_run_id"]
        or workflow_run.get("head_sha") != entry["workflow_source_sha"]
    ):
        raise ValueError("evidence GitHub Actions artifact identity or workflow binding is invalid")
    archive = download_bytes(archive_url, "GitHub Actions artifact archive", MAX_ACTION_ARTIFACT_BYTES)
    found: set[str] = set()
    try:
        with zipfile.ZipFile(BytesIO(archive)) as bundle:
            members = [item for item in bundle.infolist() if not item.is_dir()]
            if len(members) > 100 or sum(item.file_size for item in members) > 10 * 1024 * 1024:
                raise ValueError("GitHub Actions evidence archive is unexpectedly large")
            for member in members:
                if member.file_size <= MAX_EVIDENCE_BYTES:
                    found.add(hashlib.sha256(bundle.read(member)).hexdigest())
    except zipfile.BadZipFile as exc:
        raise ValueError("GitHub Actions evidence artifact is not a valid ZIP archive") from exc
    missing = required_digests - found
    if missing:
        raise ValueError("staged evidence bytes are not present in the authenticated workflow artifact")


def verify_evidence_artifact(
    entry: dict,
    kind: str,
    trusted_prefix: str,
    claim_names: tuple[str, ...],
) -> None:
    artifact = require(entry, "artifact")
    if not artifact.startswith(trusted_prefix):
        raise ValueError(f"{kind} evidence artifact must be hosted below {trusted_prefix}")
    checksum = require(entry, "artifact_sha256")
    if not SHA256.fullmatch(checksum):
        raise ValueError(f"{kind} evidence artifact_sha256 must be a lowercase SHA-256")
    content = download_verified(
        artifact,
        checksum,
        f"{kind} evidence artifact",
        capture=True,
        max_bytes=MAX_EVIDENCE_BYTES,
    )
    try:
        claims = json.loads(content)
    except (TypeError, json.JSONDecodeError) as exc:
        raise ValueError(f"{kind} evidence artifact must contain one JSON object") from exc
    if not isinstance(claims, dict) or claims.get("kind") != kind:
        raise ValueError(f"{kind} evidence artifact has the wrong evidence kind")
    for name in claim_names:
        if claims.get(name) != entry.get(name):
            raise ValueError(f"{kind} evidence artifact claim {name} does not match manifest")
    verify_workflow_run(entry, trusted_prefix)
    required_digests = {checksum}
    if "component_inventory_sha256" in entry:
        inventory_artifact = require(entry, "component_inventory_artifact")
        if not inventory_artifact.startswith(trusted_prefix):
            raise ValueError(f"{kind} component inventory must be hosted below {trusted_prefix}")
        inventory_checksum = require(entry, "component_inventory_sha256")
        download_verified(inventory_artifact, inventory_checksum, f"{kind} component inventory")
        required_digests.add(inventory_checksum)
    verify_workflow_artifact(entry, trusted_prefix, required_digests)


def validate(
    path: Path,
    expected_commit: str,
    expected_tag: str | None = None,
    *,
    verify_remote: bool = False,
    trusted_evidence_prefix: str = DEFAULT_TRUSTED_EVIDENCE_PREFIX,
) -> None:
    document = json.loads(path.read_text())
    commit = require(document, "release.commit")
    tag = require(document, "release.tag")
    if expected_tag is not None and tag != expected_tag:
        raise ValueError(f"manifest tag {tag} does not match requested release tag {expected_tag}")
    if not COMMIT.fullmatch(commit):
        raise ValueError("release.commit must be a lowercase full Git SHA")
    if commit != expected_commit:
        raise ValueError(f"manifest commit {commit} does not match {expected_commit}")
    if not RELEASE_TAG.fullmatch(tag):
        raise ValueError("release.tag must use immutable vMAJOR.MINOR.PATCH syntax")
    tag_type = subprocess.check_output(["git", "cat-file", "-t", f"refs/tags/{tag}"], text=True).strip()
    if tag_type != "tag":
        raise ValueError("release tag must be annotated")
    if git_revision(tag) != commit:
        raise ValueError(f"tag {tag} does not resolve to manifest commit {commit}")

    schema = require(document, "release.config_schema")
    repository_schema = Path("release/config-schema-version").read_text().strip()
    if schema != repository_schema:
        raise ValueError(f"manifest config schema {schema} does not match repository {repository_schema}")
    qualified_at = require(document, "release.qualified_at")
    try:
        qualified_timestamp = datetime.fromisoformat(qualified_at.replace("Z", "+00:00"))
    except (AttributeError, ValueError) as exc:
        raise ValueError("release.qualified_at must be an ISO-8601 timestamp") from exc
    if qualified_timestamp.tzinfo is None:
        raise ValueError("release.qualified_at must include an explicit timezone")
    arch_image_url = require(document, "platform.arch_image_url")
    require_https(arch_image_url, "platform.arch_image_url")
    if urlparse(arch_image_url).hostname not in {
        "geo.mirror.pkgbuild.com",
        "archive.archlinux.org",
    }:
        raise ValueError("platform.arch_image_url must use an official Arch Linux host")
    image_date_match = ARCH_IMAGE_DATE.search(urlparse(arch_image_url).path)
    if image_date_match is None:
        raise ValueError("platform.arch_image_url must reference a dated /images/YYYY.MM.DD/ image")
    image_checksum = require(document, "platform.arch_image_sha256")
    if not SHA256.fullmatch(image_checksum):
        raise ValueError("platform.arch_image_sha256 must be a lowercase SHA-256")
    package_snapshot = require(document, "platform.package_repository_snapshot")
    if not ARCH_SNAPSHOT.fullmatch(package_snapshot):
        raise ValueError("platform.package_repository_snapshot must use YYYY/MM/DD syntax")
    try:
        image_date = datetime.strptime(image_date_match.group(1), "%Y.%m.%d").date()
        snapshot_date = datetime.strptime(package_snapshot, "%Y/%m/%d").date()
    except ValueError as exc:
        raise ValueError("Arch image and package snapshot dates must be valid calendar dates") from exc
    if snapshot_date < image_date:
        raise ValueError("Arch package snapshot cannot predate the qualified cloud image")
    repository_snapshot = Path("release/arch-package-snapshot").read_text().strip()
    if package_snapshot != repository_snapshot:
        raise ValueError(
            f"manifest package snapshot {package_snapshot} does not match repository {repository_snapshot}"
        )
    require(document, "platform.package_strategy")
    package_artifact = require(document, "platform.package_state_artifact")
    require_https(package_artifact, "platform.package_state_artifact")
    package_checksum = require(document, "platform.package_state_sha256")
    if not SHA256.fullmatch(package_checksum):
        raise ValueError("platform.package_state_sha256 must be a lowercase SHA-256")
    for dependency in ("ansible_core", "python", "go"):
        version = require(document, f"dependencies.{dependency}")
        if not VERSION.fullmatch(version):
            raise ValueError(f"dependencies.{dependency} must be a version number")
    go_mod_version = next(
        line.split(maxsplit=1)[1] for line in Path("go.mod").read_text().splitlines() if line.startswith("go ")
    )
    if document["dependencies"]["go"] != go_mod_version:
        raise ValueError("dependencies.go does not match the exact go.mod toolchain version")
    collections = require(document, "dependencies.collections")
    recorded_collections = {item.get("name"): item.get("version") for item in collections}
    requirements = yaml.safe_load(Path("ansible/requirements.yml").read_text())
    expected_collections = {item["name"]: item["version"] for item in requirements["collections"]}
    if recorded_collections != expected_collections:
        raise ValueError("Ansible collection versions do not match requirements.yml")

    images = require(document, "container_images")
    if len(images) != len(set(images)):
        raise ValueError("container_images must not contain duplicates")
    if not all(re.search(r"@sha256:[0-9a-f]{64}$", image) for image in images):
        raise ValueError("every container image must be pinned by sha256 digest")
    repository_images = set()
    for compose in Path("stacks").glob("*/compose.yaml*"):
        repository_images.update(IMAGE_LINE.findall(compose.read_text()))
    repository_images.update(DIGEST_IMAGE.findall(Path("ansible/roles/docker/defaults/main.yml").read_text()))
    recorded_images = set(images)
    missing_images = sorted(repository_images - recorded_images)
    unexpected_images = sorted(recorded_images - repository_images)
    if missing_images or unexpected_images:
        raise ValueError(
            "manifest container images differ from repository"
            f"; missing: {', '.join(missing_images) or 'none'}"
            f"; unexpected: {', '.join(unexpected_images) or 'none'}"
        )

    bootstrap = require(document, "evidence.bootstrap")
    if len(bootstrap) != 2:
        raise ValueError("exactly two fresh-node bootstrap evidence entries are required")
    inventories = set()
    bootstrap_nodes = set()
    bootstrap_artifacts = set()
    for item in bootstrap:
        node = require(item, "node")
        if node in bootstrap_nodes:
            raise ValueError("fresh-node bootstrap evidence must use two distinct nodes")
        bootstrap_nodes.add(node)
        if item.get("commit") != commit or item.get("result") != "passed":
            raise ValueError("bootstrap evidence must pass for the exact release commit")
        artifact = require(item, "artifact")
        require_https(artifact, "evidence.bootstrap.artifact")
        require_artifact_checksum(item, "evidence.bootstrap")
        if artifact in bootstrap_artifacts:
            raise ValueError("fresh-node bootstrap evidence must use two distinct artifacts")
        bootstrap_artifacts.add(artifact)
        inventory = require(item, "component_inventory_sha256")
        if not SHA256.fullmatch(inventory):
            raise ValueError("component inventory checksum must be SHA-256")
        inventories.add(inventory)
        require_https(require(item, "component_inventory_artifact"), "evidence.bootstrap.component_inventory_artifact")
        require_provenance(item)
    if len(inventories) != 1:
        raise ValueError("fresh-node component inventories do not match")

    dr_entries = require(document, "evidence.disaster_recovery")
    if not isinstance(dr_entries, list) or len(dr_entries) != 2:
        raise ValueError("exactly two disaster-recovery evidence entries are required")
    dr_variants = set()
    for dr in dr_entries:
        if not isinstance(dr, dict):
            raise ValueError("disaster-recovery evidence entries must be objects")
        variant = require(dr, "variant")
        if variant not in {"standard", "offline-images"} or variant in dr_variants:
            raise ValueError("disaster-recovery evidence must contain standard and offline-images exactly once")
        dr_variants.add(variant)
        if dr.get("commit") != commit or dr.get("result") != "passed":
            raise ValueError("disaster-recovery evidence must pass for the exact commit")
        artifact = require(dr, "artifact")
        require_https(artifact, "evidence.disaster_recovery.artifact")
        require_artifact_checksum(dr, "evidence.disaster_recovery")
        require_provenance(dr)
    if dr_variants != {"standard", "offline-images"}:
        raise ValueError("disaster-recovery evidence must contain standard and offline-images")
    for name in ("upgrade", "rollback"):
        evidence = require(document, f"evidence.{name}")
        if evidence.get("commit") != commit or evidence.get("result") != "passed":
            raise ValueError(f"{name} evidence must pass for the exact release commit")
        artifact = require(evidence, "artifact")
        require_https(artifact, f"evidence.{name}.artifact")
        require_artifact_checksum(evidence, f"evidence.{name}")
        require_provenance(evidence)
    from_release = require(document, "evidence.upgrade.from_release")
    if not RELEASE_TAG.fullmatch(from_release):
        raise ValueError("evidence.upgrade.from_release must use vMAJOR.MINOR.PATCH syntax")
    if release_version(from_release) >= release_version(tag):
        raise ValueError("evidence.upgrade.from_release must be older than the qualified release")
    upgrade_inventory = require(document, "evidence.upgrade.component_inventory_sha256")
    if not SHA256.fullmatch(upgrade_inventory):
        raise ValueError("upgrade component inventory checksum must be SHA-256")
    if upgrade_inventory not in inventories:
        raise ValueError("upgraded-node component inventory does not match fresh nodes")
    require_https(
        require(document["evidence"]["upgrade"], "component_inventory_artifact"),
        "evidence.upgrade.component_inventory_artifact",
    )

    if verify_remote:
        require_https(trusted_evidence_prefix, "trusted evidence prefix")
        if not trusted_evidence_prefix.endswith("/"):
            raise ValueError("trusted evidence prefix must end with /")
        if not package_artifact.startswith(trusted_evidence_prefix):
            raise ValueError("qualified package inventory must be hosted below the trusted evidence prefix")
        download_verified(
            arch_image_url,
            image_checksum,
            "qualified Arch image",
        )
        download_verified(
            package_artifact,
            package_checksum,
            "qualified package inventory",
        )
        for item in bootstrap:
            verify_evidence_artifact(
                item,
                "bootstrap",
                trusted_evidence_prefix,
                (
                    "commit",
                    "result",
                    "node",
                    "component_inventory_sha256",
                    "workflow_run_id",
                    "workflow_path",
                    "workflow_source_sha",
                    "workflow_artifact_name",
                ),
            )
        for item in dr_entries:
            verify_evidence_artifact(
                item,
                "disaster_recovery",
                trusted_evidence_prefix,
                (
                    "commit",
                    "result",
                    "variant",
                    "workflow_run_id",
                    "workflow_path",
                    "workflow_source_sha",
                    "workflow_artifact_name",
                ),
            )
        verify_evidence_artifact(
            document["evidence"]["upgrade"],
            "upgrade",
            trusted_evidence_prefix,
            (
                "commit",
                "result",
                "from_release",
                "component_inventory_sha256",
                "workflow_run_id",
                "workflow_path",
                "workflow_source_sha",
                "workflow_artifact_name",
            ),
        )
        verify_evidence_artifact(
            document["evidence"]["rollback"],
            "rollback",
            trusted_evidence_prefix,
            (
                "commit",
                "result",
                "workflow_run_id",
                "workflow_path",
                "workflow_source_sha",
                "workflow_artifact_name",
            ),
        )


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("manifest", type=Path)
    parser.add_argument("--commit", default="HEAD")
    parser.add_argument("--tag")
    parser.add_argument(
        "--structure-only",
        action="store_true",
        help="validate structure only; never use this mode to promote a production release",
    )
    parser.add_argument(
        "--trusted-evidence-prefix",
        default=DEFAULT_TRUSTED_EVIDENCE_PREFIX,
    )
    args = parser.parse_args()
    try:
        validate(
            args.manifest,
            git_revision(args.commit),
            args.tag,
            verify_remote=not args.structure_only,
            trusted_evidence_prefix=args.trusted_evidence_prefix,
        )
    except (ValueError, OSError, json.JSONDecodeError, subprocess.CalledProcessError) as exc:
        raise SystemExit(f"release qualification invalid: {exc}") from exc
    print(f"release qualification valid: {args.manifest}")


if __name__ == "__main__":
    main()
