import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

from verify_release_source import verify


class ReleaseSourceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="widget-release-source-")
        self.directory = Path(self.temp.name).resolve()
        assert self.directory.parent == Path(tempfile.gettempdir()).resolve()
        self.previous = Path.cwd()
        os.chdir(self.directory)
        self.git("init", "-b", "main")
        self.git("config", "user.name", "Release Test")
        self.git("config", "user.email", "test@example.invalid")
        self.metadata = {
            "RT_VERSION": {"#1": {"0000": {
                "fixed": {"file_version": "1.2.3.0", "product_version": "1.2.3.0"},
                "info": {"0411": {"FileVersion": "1.2.3.0", "ProductVersion": "1.2.3.0"}},
            }}},
            "RT_MANIFEST": {"#1": {"0409": {"identity": {"version": "1.2.3.0"}}}},
        }
        Path("version.json").write_text(json.dumps(self.metadata), encoding="utf-8")
        self.git("add", "version.json")
        self.git("commit", "-m", "initial")
        self.initial = self.git("rev-parse", "HEAD")
        self.git("update-ref", "refs/remotes/origin/main", self.initial)
        self.git("tag", "v1.2.3")

    def tearDown(self):
        os.chdir(self.previous)
        # TemporaryDirectory is confined to the resolved directory created in setUp.
        assert self.directory.parent == Path(tempfile.gettempdir()).resolve()
        self.temp.cleanup()

    def git(self, *args):
        return subprocess.check_output(["git", *args], text=True, stderr=subprocess.DEVNULL).strip()

    def test_main_tag_is_allowed(self):
        verify("refs/tags/v1.2.3", self.initial, "version.json")

    def test_main_ancestor_tag_is_allowed(self):
        self.git("commit", "--allow-empty", "-m", "later")
        self.git("update-ref", "refs/remotes/origin/main", self.git("rev-parse", "HEAD"))
        verify("refs/tags/v1.2.3", self.initial, "version.json")

    def test_unmerged_branch_is_rejected(self):
        self.git("checkout", "-b", "unreviewed")
        self.git("commit", "--allow-empty", "-m", "unreviewed")
        candidate = self.git("rev-parse", "HEAD")
        self.git("tag", "-f", "v1.2.3")
        with self.assertRaisesRegex(ValueError, "origin/main"):
            verify("refs/tags/v1.2.3", candidate, "version.json")

    def test_branch_event_is_rejected(self):
        with self.assertRaisesRegex(ValueError, "tag"):
            verify("refs/heads/main", self.initial, "version.json")

    def test_different_tag_commit_is_rejected(self):
        self.git("commit", "--allow-empty", "-m", "different")
        with self.assertRaisesRegex(ValueError, "does not point"):
            verify("refs/tags/v1.2.3", self.git("rev-parse", "HEAD"), "version.json")

    def test_version_mismatch_is_rejected(self):
        self.metadata["RT_VERSION"]["#1"]["0000"]["fixed"]["file_version"] = "9.9.9.0"
        Path("version.json").write_text(json.dumps(self.metadata), encoding="utf-8")
        with self.assertRaisesRegex(ValueError, "versions differ"):
            verify("refs/tags/v1.2.3", self.initial, "version.json")


if __name__ == "__main__":
    unittest.main()
