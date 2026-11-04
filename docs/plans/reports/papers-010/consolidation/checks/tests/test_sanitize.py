"""脱敏脚本的测试：公开物里不得留下用户名、本机绝对路径或内部主机名。"""
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SAN = Path(__file__).resolve().parents[2] / "repro" / "sanitize.py"


class TestSanitize(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name)
        self.addCleanup(self._tmp.cleanup)
        self.src = self.root / "src"
        self.out = self.root / "out"
        self.src.mkdir()

    def run_san(self, *extra):
        proc = subprocess.run(
            [sys.executable, str(SAN), "--source", str(self.src), "--out", str(self.out), *extra],
            capture_output=True, text=True)
        return proc.returncode, proc.stdout, proc.stderr

    def test_home_paths_are_replaced(self):
        (self.src / "a.json").write_text(json.dumps(
            {"path": "/Users/oopslink/works/research/x.json"}), encoding="utf-8")
        code, _out, err = self.run_san()
        self.assertEqual(code, 0, err)
        text = (self.out / "a.json").read_text(encoding="utf-8")
        self.assertNotIn("/Users/", text)
        self.assertNotIn("oopslink", text)
        self.assertIn("<ARCHIVE>", text)

    def test_check_rejects_a_leaked_username(self):
        (self.src / "a.txt").write_text("done by oopslink", encoding="utf-8")
        self.out.mkdir()
        (self.out / "a.txt").write_text("done by oopslink", encoding="utf-8")
        code, _out, err = self.run_san("--check")
        self.assertEqual(code, 1)
        self.assertIn("oopslink", err)

    def test_check_passes_on_sanitized_output(self):
        (self.src / "a.json").write_text(json.dumps(
            {"p": "/Users/oopslink/x", "host": "mac-internal.local"}), encoding="utf-8")
        self.assertEqual(self.run_san()[0], 0)
        code, _out, err = self.run_san("--check")
        self.assertEqual(code, 0, err)

    def test_internal_hostnames_are_replaced(self):
        (self.src / "a.txt").write_text("host mac-internal.local ok", encoding="utf-8")
        self.run_san()
        self.assertNotIn(".local", (self.out / "a.txt").read_text(encoding="utf-8"))

    def test_manifest_records_every_copied_file_with_a_hash(self):
        (self.src / "a.txt").write_text("plain", encoding="utf-8")
        (self.src / "sub").mkdir()
        (self.src / "sub" / "b.csv").write_text("x,y\n1,2\n", encoding="utf-8")
        self.run_san()
        man = json.loads((self.out / "MANIFEST.json").read_text(encoding="utf-8"))
        names = {e["path"] for e in man["files"]}
        self.assertEqual(names, {"a.txt", "sub/b.csv"})
        for e in man["files"]:
            self.assertEqual(len(e["sha256"]), 64)

    def test_binary_files_are_skipped_and_reported(self):
        (self.src / "x.png").write_bytes(b"\x89PNG\x00\x01\x02")
        self.run_san()
        man = json.loads((self.out / "MANIFEST.json").read_text(encoding="utf-8"))
        self.assertIn("x.png", man["skipped_binary"])
        self.assertFalse((self.out / "x.png").exists())


if __name__ == "__main__":
    unittest.main(verbosity=2)
