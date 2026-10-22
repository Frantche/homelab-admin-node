#!/usr/bin/env python3
import importlib.util
import tempfile
import unittest
from copy import deepcopy
from pathlib import Path

import yaml

REPO_ROOT = Path(__file__).resolve().parents[1]
CI_DOCKER_CIDR = "172.16.0.0/12"


def load_module(name: str, path: Path):
    spec = importlib.util.spec_from_file_location(name, path)
    if not spec or not spec.loader:
        raise RuntimeError(f"could not load {path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


RENDERER = load_module("render_bootstrap_config_repo", REPO_ROOT / "ci/render-bootstrap-config-repo.py")
VALIDATOR = load_module("validate_bootstrap_config_repo", REPO_ROOT / "ci/validate-bootstrap-config-repo.py")


class BootstrapConfigRepoTests(unittest.TestCase):
    def test_ci_web_ui_allowlist_includes_docker_bridge_without_losing_example(self):
        with tempfile.TemporaryDirectory() as temporary_directory:
            group_vars = Path(temporary_directory)
            example_path = REPO_ROOT / "examples/admin-config/group_vars/all.yml.example"
            example = yaml.safe_load(example_path.read_text(encoding="utf-8"))
            (group_vars / "all.yml").write_text(
                example_path.read_text(encoding="utf-8"), encoding="utf-8"
            )

            RENDERER.write_ci_vars(group_vars, "https://example.invalid/admin-node.git")
            ci_vars = yaml.safe_load(
                (group_vars / "ci-bootstrap-vars.yml").read_text(encoding="utf-8")
            )

        expected = deepcopy(example["crowdsec_web_ui"])
        expected["allowed_cidrs"].append(CI_DOCKER_CIDR)
        self.assertEqual(ci_vars["crowdsec_web_ui"], expected)
        VALIDATOR.validate_crowdsec_web_ui_ci_config(example, ci_vars)

    def test_ci_web_ui_allowlist_validation_requires_docker_bridge(self):
        example_path = REPO_ROOT / "examples/admin-config/group_vars/all.yml.example"
        example = yaml.safe_load(example_path.read_text(encoding="utf-8"))
        configured = deepcopy(example["crowdsec_web_ui"])

        with self.assertRaisesRegex(SystemExit, "CI Docker bridge range"):
            VALIDATOR.validate_crowdsec_web_ui_ci_config(
                example, {"crowdsec_web_ui": configured}
            )


if __name__ == "__main__":
    unittest.main()
