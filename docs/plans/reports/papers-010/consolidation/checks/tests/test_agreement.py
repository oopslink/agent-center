"""一致率计算的测试。

关键不变量：
  1. 只在两侧都给出判定的条目上计算，未填条目进 missing 而不是被当成某一类；
  2. Cohen's κ 用两侧各自的边缘分布算期望一致，而不是用 1/4；
  3. 一致率为 1 且只用到单一类别时 κ 无定义（分母为 0），必须报 None 而不是崩溃或报 1.0；
  4. 分歧条目逐条列出，带双方判定与片段编号——分歧本身是结果。
"""

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

AGREEMENT = Path(__file__).resolve().parents[2] / "blind-coding" / "agreement.py"

SHEET_HEAD = (
    "# 盲编码表\n\n"
    "| # | 片段编号 | 原生 ID | UTC | 原文路径 | 判定 | 备注 |\n"
    "|---:|---|---|---|---|---|---|\n"
)


def sheet(rows):
    """rows: [(编号, 片段编号, 判定, 备注)]"""
    out = SHEET_HEAD
    for n, code, verdict, note in rows:
        out += f"| {n} | {code} | `01X` | 2026-01-01T00:00:00Z | /p/{code}.json | {verdict} | {note} |\n"
    return out


def ai(codes):
    """codes: {编号: (片段编号, 判定)}"""
    return json.dumps(
        {
            "coder": "AI",
            "codes": {k: {"code": c, "verdict": v, "note": ""} for k, (c, v) in codes.items()},
        },
        ensure_ascii=False,
    )


def run(sheet_text, ai_text, extra=()):
    with tempfile.TemporaryDirectory() as d:
        sp = Path(d) / "sheet.md"
        ap = Path(d) / "ai.json"
        sp.write_text(sheet_text, encoding="utf-8")
        ap.write_text(ai_text, encoding="utf-8")
        proc = subprocess.run(
            [sys.executable, str(AGREEMENT), "--sheet", str(sp), "--ai", str(ap), *extra],
            capture_output=True,
            text=True,
        )
    return proc, json.loads(proc.stdout) if proc.stdout.strip() else None


class TestAgreement(unittest.TestCase):
    def test_counts_only_rows_both_sides_coded(self):
        s = sheet([(1, "A01", "A", ""), (2, "A02", "", ""), (3, "A03", "B", "")])
        a = ai({"1": ("A01", "A"), "2": ("A02", "B"), "3": ("A03", "B")})
        _proc, rep = run(s, a)
        self.assertEqual(rep["n_compared"], 2)
        self.assertEqual(rep["missing_human"], ["2"])
        self.assertEqual(rep["observed_agreement"], 1.0)

    def test_disagreements_are_listed_with_both_verdicts(self):
        s = sheet([(1, "A01", "A", "理由一"), (2, "A02", "D", "理由二")])
        a = ai({"1": ("A01", "A"), "2": ("A02", "B")})
        _proc, rep = run(s, a)
        self.assertEqual(rep["observed_agreement"], 0.5)
        self.assertEqual(len(rep["disagreements"]), 1)
        d = rep["disagreements"][0]
        self.assertEqual((d["row"], d["code"], d["human"], d["ai"]), ("2", "A02", "D", "B"))
        self.assertEqual(d["human_note"], "理由二")

    def test_kappa_uses_marginals_not_uniform_chance(self):
        # 人工 4A/1B，AI 5A：观测一致 4/5=0.8。
        # 期望一致 = (5/5)*(4/5) + 0*(1/5) = 0.8 → κ = 0。
        rows = [(i, f"A0{i}", "A", "") for i in range(1, 5)] + [(5, "A05", "B", "")]
        s = sheet(rows)
        a = ai({str(i): (f"A0{i}", "A") for i in range(1, 6)})
        _proc, rep = run(s, a)
        self.assertAlmostEqual(rep["observed_agreement"], 0.8)
        self.assertAlmostEqual(rep["expected_agreement"], 0.8)
        self.assertAlmostEqual(rep["cohens_kappa"], 0.0)

    def test_kappa_is_none_when_undefined(self):
        # 两侧都只用了一个类别：期望一致 = 1，κ 的分母为 0。
        s = sheet([(1, "A01", "D", ""), (2, "A02", "D", "")])
        a = ai({"1": ("A01", "D"), "2": ("A02", "D")})
        _proc, rep = run(s, a)
        self.assertEqual(rep["observed_agreement"], 1.0)
        self.assertIsNone(rep["cohens_kappa"])
        self.assertIn("undefined", rep["kappa_note"])

    def test_code_mismatch_between_sheet_and_ai_is_an_error(self):
        # 行号对上但片段编号对不上，说明两侧读的不是同一条记录。
        s = sheet([(1, "A01", "A", "")])
        a = ai({"1": ("B99", "A")})
        proc, rep = run(s, a)
        self.assertEqual(proc.returncode, 1)
        self.assertEqual(rep["code_mismatches"], [{"row": "1", "sheet": "A01", "ai": "B99"}])

    def test_invalid_verdict_is_rejected(self):
        s = sheet([(1, "A01", "E", "")])
        a = ai({"1": ("A01", "A")})
        proc, rep = run(s, a)
        self.assertEqual(proc.returncode, 1)
        self.assertEqual(rep["invalid_verdicts"], [{"row": "1", "side": "human", "verdict": "E"}])

    def test_five_category_codebook_is_supported(self):
        # v2 新增 E 自行承接：不传 --categories 时 E 必须被当成非法值挡下，
        # 传了就要进混淆矩阵并参与 κ 的边缘分布。
        s = sheet([(1, "A01", "E", ""), (2, "A02", "E", ""), (3, "A03", "A", "")])
        a = ai({"1": ("A01", "E"), "2": ("A02", "D"), "3": ("A03", "A")})
        proc, rep = run(s, a)
        self.assertEqual(proc.returncode, 1)
        self.assertTrue(any(v["verdict"] == "E" for v in rep["invalid_verdicts"]))

        proc, rep = run(s, a, extra=("--categories", "A,B,C,D,E"))
        self.assertEqual(proc.returncode, 0)
        self.assertEqual(rep["invalid_verdicts"], [])
        self.assertEqual(sorted(rep["confusion"]), ["A", "B", "C", "D", "E"])
        self.assertEqual(rep["confusion"]["E"]["D"], 1)
        self.assertAlmostEqual(rep["observed_agreement"], 2 / 3)

    def test_empty_note_is_reported_as_protocol_violation(self):
        # 备注必填是 codebook 的硬要求：备注为空的条目要点出来，
        # 否则"没按判据填"会被一致率数字掩盖。
        s = sheet([(1, "A01", "A", ""), (2, "A02", "B", "有理由")])
        a = ai({"1": ("A01", "A"), "2": ("A02", "B")})
        _proc, rep = run(s, a)
        self.assertEqual(rep["human_rows_without_note"], ["1"])

    def test_confusion_matrix_covers_all_four_categories(self):
        s = sheet([(1, "A01", "A", ""), (2, "A02", "C", "")])
        a = ai({"1": ("A01", "B"), "2": ("A02", "C")})
        _proc, rep = run(s, a)
        self.assertEqual(sorted(rep["confusion"]), ["A", "B", "C", "D"])
        self.assertEqual(rep["confusion"]["A"]["B"], 1)
        self.assertEqual(rep["confusion"]["C"]["C"], 1)
        self.assertEqual(rep["confusion"]["A"]["A"], 0)


if __name__ == "__main__":
    unittest.main()
