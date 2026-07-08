#!/usr/bin/env python3
import sys
from pathlib import Path

import yaml


def main() -> None:
    for path in map(Path, sys.argv[1:]):
        with path.open() as f:
            data = yaml.safe_load(f)
        if data is None:
            raise SystemExit(f"{path} is empty")
        text = path.read_text()
        if "CHANGE_ME_IN_SOPS" in text:
            raise SystemExit(f"{path} still contains CHANGE_ME_IN_SOPS")


if __name__ == "__main__":
    main()
