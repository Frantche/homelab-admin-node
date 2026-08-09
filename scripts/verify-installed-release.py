#!/usr/bin/env python3
"""Bind a downloaded qualification manifest to a selected immutable Git tag."""

import argparse
import json
import re
import subprocess
from pathlib import Path

COMMIT = re.compile(r"^[0-9a-f]{40}$")
RELEASE_TAG = re.compile(r"^v[0-9]+\.[0-9]+\.[0-9]+$")


def verify(path: Path, metadata_path: Path, tag: str, commit: str, manifest_sha256: str) -> None:
    if RELEASE_TAG.fullmatch(tag) is None or COMMIT.fullmatch(commit) is None:
        raise ValueError("release tag or commit has invalid syntax")
    document = json.loads(path.read_text(encoding="utf-8"))
    release = document.get("release")
    if not isinstance(release, dict):
        raise ValueError("qualification manifest has no release object")
    if release.get("tag") != tag or release.get("commit") != commit:
        raise ValueError("qualification manifest does not match selected tag and commit")
    metadata = json.loads(metadata_path.read_text(encoding="utf-8"))
    author = metadata.get("author")
    assets = metadata.get("assets")
    if (
        metadata.get("tag_name") != tag
        or metadata.get("draft") is not False
        or metadata.get("prerelease") is not False
        or not isinstance(author, dict)
        or author.get("login") != "github-actions[bot]"
        or not isinstance(assets, list)
    ):
        raise ValueError("GitHub release is absent, unpublished, or was not created by the trusted promotion bot")
    matching_assets = [
        asset for asset in assets if isinstance(asset, dict) and asset.get("name") == "qualification.json"
    ]
    if len(matching_assets) != 1:
        raise ValueError("GitHub release must contain exactly one qualification.json asset")
    asset = matching_assets[0]
    uploader = asset.get("uploader")
    if (
        asset.get("state") != "uploaded"
        or asset.get("digest") != f"sha256:{manifest_sha256}"
        or not isinstance(uploader, dict)
        or uploader.get("login") != "github-actions[bot]"
    ):
        raise ValueError("qualification asset digest or trusted uploader is invalid")
    tag_ref = f"refs/tags/{tag}"
    tag_type = subprocess.check_output(["git", "cat-file", "-t", tag_ref], text=True).strip()
    resolved = subprocess.check_output(["git", "rev-parse", f"{tag_ref}^{{commit}}"], text=True).strip()
    if tag_type != "tag" or resolved != commit:
        raise ValueError("selected tag is not annotated or does not resolve to qualified commit")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("manifest", type=Path)
    parser.add_argument("--release-metadata", type=Path, required=True)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--manifest-sha256", required=True)
    args = parser.parse_args()
    try:
        verify(
            args.manifest,
            args.release_metadata,
            args.tag,
            args.commit,
            args.manifest_sha256,
        )
    except (ValueError, OSError, json.JSONDecodeError, subprocess.CalledProcessError) as exc:
        raise SystemExit(f"installed release qualification invalid: {exc}") from exc


if __name__ == "__main__":
    main()
