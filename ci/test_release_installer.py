#!/usr/bin/env python3
"""Exercise the cloud-init release selector against a local Git remote."""

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
        self.git("-C", str(source), "add", "release/config-schema-version")
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

    def run_installer(self, release_ref: str, checkout: str) -> subprocess.CompletedProcess:
        env = os.environ.copy()
        env["REPO_DIR"] = str(self.root / checkout)
        return subprocess.run(
            [str(self.installer), str(self.remote), release_ref],
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

    def test_ci_renderer_injects_exact_package_snapshot(self) -> None:
        public_key = self.root / "id_ed25519.pub"
        public_key.write_text("ssh-ed25519 AAAATEST ci@example.test\n", encoding="utf-8")
        rendered = self.root / "rendered"
        env = os.environ.copy()
        env.update(
            {
                "CI_VM_DIR": str(rendered),
                "CI_SSH_PUBLIC_KEY": str(public_key),
                "REPO_URL": str(self.remote),
                "REPO_REF": self.commit,
                "ARCH_PACKAGE_SNAPSHOT": "2026/08/08",
            }
        )
        result = subprocess.run(
            ["python3", str(ROOT / "ci/render-bootstrap-cloud-init.py")],
            cwd=ROOT,
            check=False,
            capture_output=True,
            text=True,
            env=env,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        data = yaml.safe_load((rendered / "user-data").read_text(encoding="utf-8"))
        bootcmd = "\n".join(data["bootcmd"])
        self.assertIn("snapshot='2026/08/08'", bootcmd)
        self.assertNotIn("ARCH_PACKAGE_SNAPSHOT_REPLACE_ME", bootcmd)
        rendered_files = {item["path"]: item for item in data["write_files"]}
        self.assertEqual(
            rendered_files["/etc/admin-node/package-snapshot-mode"]["content"].strip(),
            "qualified",
        )
        self.assertIn(
            "snapshot='2026/08/08'",
            rendered_files["/usr/local/bin/admin-node-record-package-snapshot"]["content"],
        )

    def test_ci_renderer_refuses_implicit_or_unauthorized_live_mirror(self) -> None:
        public_key = self.root / "id_ed25519.pub"
        public_key.write_text("ssh-ed25519 AAAATEST ci@example.test\n", encoding="utf-8")
        base_env = os.environ.copy()
        base_env.update(
            {
                "CI_VM_DIR": str(self.root / "rendered-live"),
                "CI_SSH_PUBLIC_KEY": str(public_key),
                "REPO_URL": str(self.remote),
                "REPO_REF": self.commit,
            }
        )
        missing = subprocess.run(
            ["python3", str(ROOT / "ci/render-bootstrap-cloud-init.py")],
            cwd=ROOT,
            check=False,
            capture_output=True,
            text=True,
            env=base_env,
        )
        self.assertNotEqual(missing.returncode, 0)
        self.assertIn("explicit YYYY/MM/DD", missing.stderr)

        base_env["ARCH_PACKAGE_SNAPSHOT"] = "live"
        unauthorized = subprocess.run(
            ["python3", str(ROOT / "ci/render-bootstrap-cloud-init.py")],
            cwd=ROOT,
            check=False,
            capture_output=True,
            text=True,
            env=base_env,
        )
        self.assertNotEqual(unauthorized.returncode, 0)
        self.assertIn("CI_ALLOW_LIVE_ARCH_MIRROR=true", unauthorized.stderr)

        base_env["CI_ALLOW_LIVE_ARCH_MIRROR"] = "true"
        allowed = subprocess.run(
            ["python3", str(ROOT / "ci/render-bootstrap-cloud-init.py")],
            cwd=ROOT,
            check=False,
            capture_output=True,
            text=True,
            env=base_env,
        )
        self.assertEqual(allowed.returncode, 0, allowed.stderr)
        data = yaml.safe_load((self.root / "rendered-live/user-data").read_text(encoding="utf-8"))
        rendered_files = {item["path"]: item for item in data["write_files"]}
        self.assertEqual(
            rendered_files["/etc/admin-node/package-snapshot-mode"]["content"].strip(),
            "ci-live",
        )

    @staticmethod
    def git(*args: str) -> None:
        subprocess.run(["git", *args], check=True, capture_output=True, text=True)

    @staticmethod
    def output(*args: str) -> str:
        return subprocess.check_output(["git", *args], text=True).strip()


if __name__ == "__main__":
    unittest.main()
