#!/usr/bin/env python3
import sys
from pathlib import Path

import yaml


def main() -> None:
    path = Path(sys.argv[1])
    openbao_config_enabled = sys.argv[2].lower() == "true"

    with path.open() as f:
        data = yaml.safe_load(f)

    for section in ("keycloak_config", "harbor_config", "gitea_config"):
        data.setdefault(section, {})["enabled"] = True
    data.setdefault("openbao_config", {})["enabled"] = openbao_config_enabled
    data.setdefault("openbao_config", {}).pop("root_token", None)
    data.setdefault("openbao", {}).pop("root_token", None)

    with path.open("w") as f:
        yaml.safe_dump(data, f, default_flow_style=False, sort_keys=False)


if __name__ == "__main__":
    main()
