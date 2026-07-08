#!/usr/bin/env python3
import re
import sys
from pathlib import Path

import yaml

SECRET_PLACEHOLDER = "CHANGE_ME_IN_SOPS"
GENERATED_SECRET_RE = re.compile(r"^[A-Za-z0-9]{32}$")


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

    for path in map(Path, args):
        load_yaml(path)
        validate_no_placeholder(path)

    if secrets_example is not None:
        rendered = load_yaml(Path(args[0]))
        example = load_yaml(secrets_example)
        validate_generated_secrets(example, rendered)


if __name__ == "__main__":
    main()
