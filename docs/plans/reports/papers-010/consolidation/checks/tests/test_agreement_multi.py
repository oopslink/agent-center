"""三方及以上编码者的一致率。

两件事必须成立：
  1. 编码表的列位置不能写死——外部编码包的表只有「# | 记录 | 判定 | 备注」四列，
     与台账样式的七列表不同。按表头名字找列，找不到才退回固定下标。
  2. 三方要给出两两 κ **和** Fleiss κ。只报一个总体数字会掩盖"两个编码者很一致、
     第三个与两者都不一致"这种结构，而这正是需要被看见的东西。
"""

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

AGREEMENT = Path(__file__).resolve().parents[2] / "blind-coding" / "agreement.py"

WIDE = (
    "| # | 片段编号 | 原生 ID | UTC | 原文路径 | 判定 | 备注 |\n"
    "|---:|---|---|---|---|---|---|\n"
)
NARROW = "| # | 记录 | 判定 | 备注 |\n|---:|---|---|---|\n"


def wide(rows):
    out = "# 表\n\n" + WIDE
    for n, code, v, note in rows:
        out += f"| {n} | {code} | `01X` | 2026-01-01T00:00:00Z | /p/x.json | {v} | {note} |\n"
    return out


def narrow(rows):
    out = "# 表\n\n编码者：某人\n\n" + NARROW
    for n, v, note in rows:
        out += f"| {n} | [记录 {n:02d}](records/{n:02d}.txt) | {v} | {note} |\n"
    return out


def run(args, cwd):
    proc = subprocess.run([sys.executable, str(AGREEMENT), *args],
                          capture_output=True, text=True, cwd=cwd)
    return proc, json.loads(proc.stdout) if proc.stdout.strip() else None


class TestMultiCoder(unittest.TestCase):
    def test_narrow_sheet_columns_are_found_by_header_name(self):
        with tempfile.TemporaryDirectory() as t:
            d = Path(t)
            (d / "n.md").write_text(narrow([(1, "E", "理由一"), (2, "B", "理由二")]), encoding="utf-8")
            (d / "ai.json").write_text(json.dumps({"codes": {
                "1": {"code": "x", "verdict": "E", "note": ""},
                "2": {"code": "x", "verdict": "D", "note": ""}}}), encoding="utf-8")
            _p, rep = run(["--sheet", "n.md", "--ai", "ai.json",
                           "--categories", "A,B,C,D,E"], str(d))
            self.assertEqual(rep["n_compared"], 2)
            self.assertAlmostEqual(rep["observed_agreement"], 0.5)
            self.assertEqual(rep["human_rows_without_note"], [])
            self.assertEqual(rep["disagreements"][0]["human_note"], "理由二")

    def test_pairwise_and_fleiss_for_three_coders(self):
        # 甲乙完全一致（都 E,E,B），丙全判 D：
        # 甲乙 po=1，甲丙／乙丙 po=0。Fleiss：每题两人同类一人异类。
        with tempfile.TemporaryDirectory() as t:
            d = Path(t)
            (d / "a.md").write_text(wide([(1, "X1", "E", "r"), (2, "X2", "E", "r"),
                                          (3, "X3", "B", "r")]), encoding="utf-8")
            (d / "b.md").write_text(narrow([(1, "E", "r"), (2, "E", "r"), (3, "B", "r")]),
                                    encoding="utf-8")
            (d / "c.md").write_text(narrow([(1, "D", "r"), (2, "D", "r"), (3, "D", "r")]),
                                    encoding="utf-8")
            _p, rep = run(["--coder", "甲=a.md", "--coder", "乙=b.md", "--coder", "丙=c.md",
                           "--categories", "A,B,C,D,E"], str(d))
            self.assertEqual(rep["coders"], ["甲", "乙", "丙"])
            self.assertEqual(rep["n_items"], 3)
            pw = {p["pair"]: p for p in rep["pairwise"]}
            self.assertAlmostEqual(pw["甲×乙"]["observed_agreement"], 1.0)
            self.assertAlmostEqual(pw["甲×丙"]["observed_agreement"], 0.0)
            self.assertAlmostEqual(pw["乙×丙"]["observed_agreement"], 0.0)
            # Fleiss: Pbar = 1/3, Pe = (2/9)^2*... 按类别边缘平方和
            self.assertIn("fleiss_kappa", rep)
            self.assertLess(rep["fleiss_kappa"], 0.5)

    def test_kappa_interval_beyond_one_is_truncated_and_flagged(self):
        # 正态近似在 n 小、κ 高时会给出超过 1 的上界。κ 的定义上界是 1，
        # 直接印 1.081 是错的；截断并标注，而不是悄悄夹住。
        rows = [(i, "E", "r") for i in range(1, 15)] + [(15, "B", "r")]
        other = [(i, "E", "r") for i in range(1, 15)] + [(15, "D", "r")]
        with tempfile.TemporaryDirectory() as t:
            d = Path(t)
            (d / "a.md").write_text(narrow(rows), encoding="utf-8")
            (d / "b.md").write_text(narrow(other), encoding="utf-8")
            _p, rep = run(["--coder", "甲=a.md", "--coder", "乙=b.md",
                           "--categories", "A,B,C,D,E"], str(d))
            pair = rep["pairwise"][0]
            self.assertGreater(pair["kappa_ci95_raw"][1], 1.0)
            self.assertEqual(pair["kappa_ci95"][1], 1.0)
            self.assertTrue(pair["kappa_ci95_truncated"])

    def test_a_coder_missing_a_row_is_reported_not_silently_dropped(self):
        with tempfile.TemporaryDirectory() as t:
            d = Path(t)
            (d / "a.md").write_text(narrow([(1, "E", "r"), (2, "E", "r")]), encoding="utf-8")
            (d / "b.md").write_text(narrow([(1, "E", "r"), (2, "", "")]), encoding="utf-8")
            _p, rep = run(["--coder", "甲=a.md", "--coder", "乙=b.md",
                           "--categories", "A,B,C,D,E"], str(d))
            self.assertEqual(rep["rows_not_coded_by_all"], ["2"])
            self.assertEqual(rep["n_items"], 1)

    def test_three_way_disagreements_list_every_coders_verdict(self):
        with tempfile.TemporaryDirectory() as t:
            d = Path(t)
            (d / "a.md").write_text(narrow([(1, "E", "甲的理由")]), encoding="utf-8")
            (d / "b.md").write_text(narrow([(1, "B", "乙的理由")]), encoding="utf-8")
            (d / "c.md").write_text(narrow([(1, "B", "丙的理由")]), encoding="utf-8")
            _p, rep = run(["--coder", "甲=a.md", "--coder", "乙=b.md", "--coder", "丙=c.md",
                           "--categories", "A,B,C,D,E"], str(d))
            row = rep["item_table"][0]
            self.assertEqual(row["verdicts"], {"甲": "E", "乙": "B", "丙": "B"})
            self.assertFalse(row["unanimous"])
            self.assertEqual(row["notes"]["甲"], "甲的理由")


if __name__ == "__main__":
    unittest.main()
