"""复现包 verify.py 必须把两轮一致率也算进断言，否则论文里的 κ 无人复算。"""
import json, subprocess, sys, unittest
from pathlib import Path

PKG = Path(__file__).resolve().parents[2] / "repro" / "package" / "a-field-study"


class TestVerifyCoversAgreement(unittest.TestCase):
    def test_package_ships_both_rounds(self):
        for r in (1, 2):
            for f in ("sheet.md", "ai-coding.json", "sample.json"):
                self.assertTrue((PKG / "blind-coding" / f"round-{r}" / f).exists(),
                                f"round-{r}/{f} 不在包内")
        self.assertTrue((PKG / "blind-coding" / "codebook.md").exists())
        self.assertTrue((PKG / "blind-coding" / "revisions.md").exists())
        self.assertTrue((PKG / "blind-coding" / "round-2" / "coder-c-codex.md").exists(),
                        "第三名编码者的表不在包内")
        self.assertTrue((PKG / "blind-coding" / "round-4-human" / "sheet.md").exists(),
                        "外部人类编码者的表不在包内")

    def test_verify_asserts_the_kappa_values_in_the_paper(self):
        proc = subprocess.run([sys.executable, str(PKG / "verify.py")],
                              capture_output=True, text=True, cwd=str(PKG))
        self.assertEqual(proc.returncode, 0, proc.stdout[-2000:] + proc.stderr[-2000:])
        for key in ("r1_kappa", "r2_kappa", "r1_agreement", "r2_agreement",
                    "r2_n", "r1_n_compared",
                    "r2_kappa_codex", "r2_kappa_ai_pair",
                    "r2_kappa_human_pair", "r2_kappa_human_opus", "r2_kappa_human_codex",
                    "r2_fleiss4", "r2_unanimous4"):
            self.assertIn(key, proc.stdout, f"verify.py 没有断言 {key}")

    def test_mutating_a_human_verdict_makes_verify_fail(self):
        # 变异检查：改掉人工表里的一个判定，κ 必变，verify 必须退出 1。
        sheet = PKG / "blind-coding" / "round-2" / "sheet.md"
        original = sheet.read_text(encoding="utf-8")
        mutated = original.replace("| E | agent 自行处理了", "| A | agent 自行处理了", 1)
        self.assertNotEqual(mutated, original, "变异没命中，测试本身失效")
        try:
            sheet.write_text(mutated, encoding="utf-8")
            proc = subprocess.run([sys.executable, str(PKG / "verify.py")],
                                  capture_output=True, text=True, cwd=str(PKG))
            self.assertEqual(proc.returncode, 1)
        finally:
            sheet.write_text(original, encoding="utf-8")


if __name__ == "__main__":
    unittest.main()
