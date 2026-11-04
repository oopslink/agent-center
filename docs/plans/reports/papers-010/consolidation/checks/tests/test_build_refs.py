"""参考文献生成器的测试：编号必须跟随正文首次出现顺序，预印本必须被标出。"""

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

BUILD = Path(__file__).resolve().parents[2] / "build_refs.py"

BIB = """@article{alpha2020one,
  author = {A. One and B. Two},
  title = {First {CSCW} Title},
  journal = {Journal of Things},
  volume = {1},
  pages = {1--10},
  year = {2020},
  doi = {10.1000/alpha}
}

@misc{beta2026two,
  author = {C. Three},
  title = {Second Title},
  year = {2026},
  eprint = {2601.00001},
  archivePrefix = {arXiv},
  primaryClass = {cs.SE},
  doi = {10.48550/arXiv.2601.00001}
}

@article{delta2024quoted,
  title = "Quoted Field Title",
  author = "E. Five and F. Six",
  journal = "Journal of Quotes",
  volume = "12",
  pages = "157--173",
  year = "2024",
  doi = "10.1000/delta"
}

@inproceedings{gamma2019three,
  author = {D. Four},
  title = {Third Title},
  booktitle = {Proc. of Somewhere},
  year = {2019},
  doi = {10.1000/gamma}
}
"""


class TestBuildRefs(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name)
        self.addCleanup(self._tmp.cleanup)
        self.bib = self.root / "refs.bib"
        self.bib.write_text(BIB, encoding="utf-8")
        self.paper = self.root / "paper.md"

    def build(self, *extra):
        proc = subprocess.run(
            [sys.executable, str(BUILD), "--paper", str(self.paper), "--bib", str(self.bib), *extra],
            capture_output=True, text=True,
        )
        return proc.returncode, proc.stdout, proc.stderr

    def test_numbering_follows_first_appearance(self):
        self.paper.write_text(
            "# P\n\n见 [1]<!--ref:gamma2019three--> 与 [2]<!--ref:alpha2020one-->，"
            "又见 [1]<!--ref:gamma2019three-->。\n\n## 参考文献\n\n占位\n",
            encoding="utf-8",
        )
        code, out, err = self.build()
        self.assertEqual(code, 0, err)
        self.assertLess(out.index("Third Title"), out.index("First CSCW Title"))
        self.assertIn("[1] D. Four", out)
        self.assertIn("[2] A. One", out)

    def test_preprint_is_marked(self):
        self.paper.write_text("# P\n\n[1]<!--ref:beta2026two-->\n", encoding="utf-8")
        code, out, err = self.build()
        self.assertEqual(code, 0, err)
        self.assertIn("arXiv:2601.00001", out)
        self.assertIn("预印本", out)

    def test_published_entry_is_not_marked_preprint(self):
        self.paper.write_text("# P\n\n[1]<!--ref:alpha2020one-->\n", encoding="utf-8")
        _code, out, _err = self.build()
        self.assertNotIn("预印本", out)
        self.assertIn("Journal of Things", out)

    def test_bibtex_protective_braces_are_stripped(self):
        self.paper.write_text("# P\n\n[1]<!--ref:alpha2020one-->\n", encoding="utf-8")
        _code, out, _err = self.build()
        self.assertIn("First CSCW Title", out)
        self.assertNotIn("{", out)
        self.assertNotIn("}", out)

    def test_renumber_rewrites_numbers_to_first_appearance_order(self):
        self.paper.write_text(
            "# P\n\n先 [9]<!--ref:gamma2019three--> 再 [3]<!--ref:alpha2020one-->，"
            "又见 [9]<!--ref:gamma2019three-->。\n",
            encoding="utf-8",
        )
        code, _out, err = self.build("--renumber")
        self.assertEqual(code, 0, err)
        text = self.paper.read_text(encoding="utf-8")
        self.assertIn("[1]<!--ref:gamma2019three-->", text)
        self.assertIn("[2]<!--ref:alpha2020one-->", text)
        self.assertNotIn("[9]", text)
        self.assertNotIn("[3]", text)

    def test_renumber_then_build_is_consistent(self):
        self.paper.write_text(
            "# P\n\n[7]<!--ref:beta2026two--> 和 [2]<!--ref:gamma2019three-->\n"
            "\n## 参考文献\n\n占位\n",
            encoding="utf-8",
        )
        self.assertEqual(self.build("--renumber")[0], 0)
        code, out, err = self.build()
        self.assertEqual(code, 0, err)
        self.assertLess(out.index("Second Title"), out.index("Third Title"))

    def test_quote_delimited_fields_are_parsed(self):
        """有些 bib 条目用 field = "..." 而非 field = {...}；漏掉会整条渲染成空。"""
        self.paper.write_text("# P\n\n[1]<!--ref:delta2024quoted-->\n", encoding="utf-8")
        code, out, err = self.build()
        self.assertEqual(code, 0, err)
        self.assertIn("Quoted Field Title", out)
        self.assertIn("E. Five", out)
        self.assertIn("Journal of Quotes", out)
        self.assertIn("157–173", out)
        self.assertIn("10.1000/delta", out)
        self.assertNotRegex(out, r"^\[1\] , ")

    def test_citation_missing_from_bib_is_an_error(self):
        self.paper.write_text("# P\n\n[1]<!--ref:nosuchkey-->\n", encoding="utf-8")
        code, _out, err = self.build()
        self.assertEqual(code, 1)
        self.assertIn("nosuchkey", err)

    def test_numbering_mismatch_in_paper_is_reported(self):
        """正文写的编号与首次出现顺序不一致时必须报错，而不是静默改号。"""
        self.paper.write_text(
            "# P\n\n[2]<!--ref:gamma2019three--> 然后 [1]<!--ref:alpha2020one-->\n",
            encoding="utf-8",
        )
        code, _out, err = self.build()
        self.assertEqual(code, 1)
        self.assertIn("gamma2019three", err)


if __name__ == "__main__":
    unittest.main(verbosity=2)
