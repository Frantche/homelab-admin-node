#!/usr/bin/env python3
"""Exercise the cloud-init release selector against a local Git remote."""

import hashlib
import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[1]
CLOUD_INIT = ROOT / "cloud-init/admin-01.user-data.yaml"


class ReleaseInstallerTest(unittest.TestCase):
    def setUp(self) -> None:
        self.tempdir = tempfile.TemporaryDirectory()
        self.addCleanup(self.tempdir.cleanup)
        self.root = Path(self.tempdir.name)
        source = self.root / "source"
        self.git("init", "--initial-branch=main", str(source))
        self.git("-C", str(source), "config", "user.email", "ci@example.test")
        self.git("-C", str(source), "config", "user.name", "CI")
        (source / "release").mkdir()
        (source / "release/config-schema-version").write_text("1\n", encoding="utf-8")
        (source / "scripts").mkdir()
        (source / "scripts/verify-installed-release.py").write_text(
            (ROOT / "scripts/verify-installed-release.py").read_text(encoding="utf-8"),
            encoding="utf-8",
        )
        self.git("-C", str(source), "add", "release/config-schema-version", "scripts/verify-installed-release.py")
        self.git("-C", str(source), "commit", "-m", "initial")
        self.commit = self.output("-C", str(source), "rev-parse", "HEAD")
        self.git("-C", str(source), "tag", "-a", "v1.2.3", "-m", "qualified")
        self.git("-C", str(source), "tag", "v1.2.4")
        self.remote = self.root / "origin.git"
        self.git("clone", "--bare", str(source), str(self.remote))

        data = yaml.safe_load(CLOUD_INIT.read_text(encoding="utf-8"))
        content = next(
            item["content"]
            for item in data["write_files"]
            if item["path"] == "/usr/local/bin/admin-node-install-release"
        )
        self.state_dir = self.root / "state"
        self.state_dir.mkdir()
        content = content.replace("/etc/admin-node", str(self.state_dir))
        self.installer = self.root / "admin-node-install-release"
        self.installer.write_text(content, encoding="utf-8")
        self.installer.chmod(0o755)

        self.qualification = self.root / "qualification.json"
        self.qualification.write_text(
            json.dumps({"release": {"tag": "v1.2.3", "commit": self.commit}}),
            encoding="utf-8",
        )
        self.qualification_sha256 = hashlib.sha256(self.qualification.read_bytes()).hexdigest()
        self.release_metadata = self.root / "release-metadata.json"
        self.release_metadata.write_text(
            json.dumps(
                {
                    "tag_name": "v1.2.3",
                    "draft": False,
                    "prerelease": False,
                    "author": {"login": "github-actions[bot]"},
                    "assets": [
                        {
                            "name": "qualification.json",
                            "state": "uploaded",
                            "digest": "sha256:" + self.qualification_sha256,
                            "uploader": {"login": "github-actions[bot]"},
                        }
                    ],
                }
            ),
            encoding="utf-8",
        )
        bin_dir = self.root / "bin"
        bin_dir.mkdir()
        fake_curl = bin_dir / "curl"
        fake_curl.write_text(
            "#!/usr/bin/env bash\nset -euo pipefail\n"
            "output=\nurl=\nwhile (( $# )); do\n"
            "  if [[ \"$1\" == --output ]]; then output=\"$2\"; shift 2\n"
            "  elif [[ \"$1\" == https://* ]]; then url=\"$1\"; shift\n"
            "  else shift; fi\n"
            "done\n"
            "if [[ \"$url\" == https://api.github.com/* ]]; then\n"
            "  cp \"$RELEASE_METADATA_TEST_SOURCE\" \"$output\"\n"
            "else\n"
            "  cp \"$QUALIFICATION_TEST_SOURCE\" \"$output\"\n"
            "fi\n",
            encoding="utf-8",
        )
        fake_curl.chmod(0o755)
        self.install_env = os.environ.copy()
        self.install_env["PATH"] = str(bin_dir) + os.pathsep + self.install_env["PATH"]
        self.install_env["QUALIFICATION_TEST_SOURCE"] = str(self.qualification)
        self.install_env["RELEASE_METADATA_TEST_SOURCE"] = str(self.release_metadata)
        self.install_env["GIT_CONFIG_COUNT"] = "1"
        self.install_env["GIT_CONFIG_KEY_0"] = "url.file://" + str(self.remote) + ".insteadOf"
        self.install_env["GIT_CONFIG_VALUE_0"] = "https://github.com/example/repo.git"

    def run_installer(
        self, release_ref: str, checkout: str, channel: str = "production"
    ) -> subprocess.CompletedProcess:
        env = self.install_env.copy()
        env["REPO_DIR"] = str(self.root / checkout)
        extra = []
        if release_ref.startswith("v"):
            extra = [
                "https://github.com/example/repo/releases/download/v1.2.3/qualification.json",
                self.qualification_sha256,
                channel,
            ]
        elif channel == "ci":
            extra = ["", "", channel]
        return subprocess.run(
            [str(self.installer), "https://github.com/example/repo.git", release_ref, *extra],
            check=False,
            capture_output=True,
            text=True,
            env=env,
        )

    def test_accepts_annotated_release_tag_and_persists_commit_pin(self) -> None:
        result = self.run_installer("v1.2.3", "tag-checkout")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            (self.state_dir / "release-name").read_text(encoding="utf-8").strip(),
            "v1.2.3",
        )
        self.assertEqual(
            (self.state_dir / "release-ref").read_text(encoding="utf-8").strip(),
            self.commit,
        )
        self.assertEqual(
            self.output("-C", str(self.root / "tag-checkout"), "branch", "--show-current"),
            "",
        )

    def test_rejects_lightweight_tag_and_arbitrary_branch(self) -> None:
        lightweight = self.run_installer("v1.2.4", "lightweight-checkout")
        self.assertNotEqual(lightweight.returncode, 0)
        self.assertIn("must be annotated", lightweight.stderr)

        branch = self.run_installer("feature", "branch-checkout")
        self.assertNotEqual(branch.returncode, 0)
        self.assertIn("release ref must be", branch.stderr)

    def test_rejects_direct_commit_outside_explicit_ci_channel(self) -> None:
        production = self.run_installer(self.commit, "sha-production")
        self.assertNotEqual(production.returncode, 0)
        self.assertIn("reserved for the explicit CI channel", production.stderr)

        ci = self.run_installer(self.commit, "sha-ci", channel="ci")
        self.assertEqual(ci.returncode, 0, ci.stderr)

    def test_rejects_qualification_checksum_mismatch(self) -> None:
        original = self.qualification_sha256
        self.qualification_sha256 = "0" * 64
        self.addCleanup(setattr, self, "qualification_sha256", original)
        result = self.run_installer("v1.2.3", "bad-qualification")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("did NOT match", result.stderr)

    def test_rejects_release_not_published_by_promotion_bot(self) -> None:
        metadata = json.loads(self.release_metadata.read_text(encoding="utf-8"))
        metadata["author"]["login"] = "manual-user"
        self.release_metadata.write_text(json.dumps(metadata), encoding="utf-8")
        result = self.run_installer("v1.2.3", "manual-release")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("trusted promotion bot", result.stderr)

    def test_main_rerun_does_not_discard_local_commits(self) -> None:
        first = self.run_installer("main", "main-checkout")
        self.assertEqual(first.returncode, 0, first.stderr)
        checkout = self.root / "main-checkout"
        self.git("-C", str(checkout), "config", "user.email", "ci@example.test")
        self.git("-C", str(checkout), "config", "user.name", "CI")
        (checkout / "LOCAL.md").write_text("keep me\n", encoding="utf-8")
        self.git("-C", str(checkout), "add", "LOCAL.md")
        self.git("-C", str(checkout), "commit", "-m", "local development")
        local_commit = self.output("-C", str(checkout), "rev-parse", "HEAD")

        second = self.run_installer("main", "main-checkout")
        self.assertEqual(second.returncode, 0, second.stderr)
        self.assertEqual(self.output("-C", str(checkout), "rev-parse", "HEAD"), local_commit)
        self.assertFalse((self.state_dir / "git-ref").exists())
        self.assertFalse((self.state_dir / "config-schema-version").exists())

    @staticmethod
    def git(*args: str) -> None:
        subprocess.run(["git", *args], check=True, capture_output=True, text=True)

    @staticmethod
    def output(*args: str) -> str:
        return subprocess.check_output(["git", *args], text=True).strip()


if __name__ == "__main__":
    unittest.main()
