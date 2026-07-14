#!/usr/bin/env python3
import re
import sys
from pathlib import Path

import yaml

SECRET_PLACEHOLDER = "CHANGE_ME_IN_SOPS"
GENERATED_SECRET_RE = re.compile(r"^[A-Za-z0-9]{32}$")


def split_image_reference(image: str):
    if "@" in image:
        repository, reference = image.rsplit("@", 1)
        return repository, reference
    if ":" in image.rsplit("/", 1)[-1]:
        repository, reference = image.rsplit(":", 1)
        return repository, reference
    return image, "latest"


def load_yaml(path: Path):
    with path.open() as f:
        data = yaml.safe_load(f)
    if data is None:
        raise SystemExit(f"{path} is empty")
    return data


def validate_no_placeholder(path: Path) -> None:
    if SECRET_PLACEHOLDER in path.read_text():
        raise SystemExit(f"{path} still contains {SECRET_PLACEHOLDER}")


def validate_generated_secrets(example, rendered, path=()) -> None:
    if isinstance(example, str):
        if example == SECRET_PLACEHOLDER:
            if not isinstance(rendered, str) or not GENERATED_SECRET_RE.fullmatch(rendered):
                dotted = ".".join(path)
                raise SystemExit(f"{dotted} must be a generated 32 character ASCII secret")
        return
    if isinstance(example, list):
        for index, item in enumerate(example):
            validate_generated_secrets(item, rendered[index], path + (str(index),))
        return
    if isinstance(example, dict):
        for key, item in example.items():
            validate_generated_secrets(item, rendered[key], path + (str(key),))


def validate_harbor_scan_matches_mirror(all_vars, ci_vars) -> None:
    project = ci_vars.get("harbor_validation_scan_project")
    repository = ci_vars.get("harbor_validation_scan_repository")
    reference = ci_vars.get("harbor_validation_scan_reference")
    if not project and not repository and not reference:
        return
    if not project or not repository or not reference:
        raise SystemExit("harbor_validation_scan_project, repository, and reference must be set together")

    mirrors = all_vars.get("harbor_config", {}).get("registry_mirrors", [])
    for mirror in mirrors:
        mirror_project = mirror.get("project_name", mirror.get("name"))
        image = mirror.get("validation", {}).get("image")
        if mirror_project != project or not image:
            continue
        mirror_repository, mirror_reference = split_image_reference(image)
        if (mirror_repository, mirror_reference) == (repository, reference):
            return
        raise SystemExit(
            "Harbor Trivy scan target must match the registry mirror validation image "
            f"for project {project}: expected {mirror_repository}@{mirror_reference}, "
            f"got {repository}@{reference}"
        )

    raise SystemExit(f"Harbor Trivy scan project {project} has no registry mirror validation image")


def main() -> None:
    if len(sys.argv) < 2:
        raise SystemExit("usage: validate-bootstrap-config-repo.py <yaml>... [--secrets-example <example>]")

    args = sys.argv[1:]
    secrets_example = None
    if "--secrets-example" in args:
        index = args.index("--secrets-example")
        try:
            secrets_example = Path(args[index + 1])
        except IndexError:
            raise SystemExit("--secrets-example requires a path")
        del args[index : index + 2]

    loaded = []
    for path in map(Path, args):
        loaded.append(load_yaml(path))
        validate_no_placeholder(path)

    if secrets_example is not None:
        rendered = loaded[0]
        example = load_yaml(secrets_example)
        validate_generated_secrets(example, rendered)
    if len(loaded) >= 3:
        validate_harbor_scan_matches_mirror(loaded[1], loaded[2])


if __name__ == "__main__":
    main()
