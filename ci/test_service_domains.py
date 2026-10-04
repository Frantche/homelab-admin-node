#!/usr/bin/env python3
"""Verify CI host mappings include configured service hostnames."""

import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SERVICE_DOMAINS = ROOT / "ci/service-domains.py"


class ServiceDomainsTests(unittest.TestCase):
    def test_list_includes_configured_crowdsec_web_ui_hostname(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            config = Path(temp_dir) / "all.yml"
            config.write_text(
                "service_domains:\n"
                "  keycloak: login.internal.example\n"
                "crowdsec_web_ui:\n"
                "  enabled: true\n"
                '  hostname: "crowdsec-ui.internal.example"\n',
                encoding="utf-8",
            )
            env = os.environ | {"ADMIN_NODE_CONFIG_ALL_YML": str(config)}

            result = subprocess.run(
                [sys.executable, str(SERVICE_DOMAINS), "list"],
                check=True,
                capture_output=True,
                text=True,
                env=env,
            )

        domains = result.stdout.splitlines()
        self.assertIn("login.internal.example", domains)
        self.assertIn("crowdsec-ui.internal.example", domains)
        self.assertNotIn("crowdsec-ui.example.com", domains)


if __name__ == "__main__":
    unittest.main()
