"""盲编码抽样脚本的测试。

关键要求：抽出来的表不能泄漏 AI 已有的判断（断裂类别、用途描述），
只能给原始记录的定位信息；同一 seed 必须可复现。
"""

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SAMPLE = Path(__file__).resolve().parents[2] / "blind-coding" / "sample.py"

LEDGER = """# 台账

| 级别 | 含义 | 正文允许的表述 |
|---|---|---|
| 原生记录 | 冻结库 | 记录显示 |

## §4.1

| 断裂类别 | 原片段编号 | 原生 ID | 证据级别 | 进 A 篇的小节 | 排除理由 |
|---|---|---|---|---|---|
| 责任 | I01 外推 | A02a | 原生记录 | 4.1 | |
| 责任 | I01 用户纠正 | A02b | 原生记录 | 4.1 | |
| 责任 | I02 验收表述 | A03a | 原生记录 | 4.1 | |
| 责任 | I04 等待中心 | A04a | 原生记录 | 4.1 | |
| 责任 | 身份契约 | A01 | 源码静态阅读 | 4.1 | |
| 凭据 | W02 误映射 | B03.1 | 原生记录 | 4.2 | |
| 凭据 | 远端核对 | B03.5 | 参与者报告 | 4.2 | |
"""

APPENDIX = """# P 篇

| 编号 | 用途 | 原生 ID / 类型 | UTC 时间 | 原文路径 |
|---|---|---|---|---|
| A01 | 历史身份契约 | 文档/源码 | 不适用 | [原文](/archive/contract.md) |
| A02a | I01恢复失败与外部介入表述 | `01KY4B5KS3272X3AMXDXHQMEC0` | 2026-07-22T07:20:47.651234Z | [原文](/archive/records/a02a.json) |
| A02b | I01用户纠正 | `01KY4B74V6XGRFG85EV29RS1RT` | 2026-07-22T07:21:37.894896Z | [原文](/archive/records/a02b.json) |
| A03a | I02交付验收表述 | `01KY5BMKX9ZM03BTJTENTQSKDB` | 2026-07-22T16:48:13.737573Z | [原文](/archive/records/a03a.json) |
| A04a | I04等待中心修复dispatch | `01KZABVT594GFNYTWZ7AZRHF30` | 2026-08-06T01:44:03.497609Z | [原文](/archive/records/a04a.json) |
| B03.1 | 旧006 C07-E01原文 | `01KV71B56MYTKFX58W48VARSTA` | 2026-06-16T01:39:41.396427Z | [原文](/archive/records/b031.json) |
| B03.5 | 旧006 C07-E05原文 | `01KV71XZ0F9CXTWEMS6YEAP4DK` | 2026-06-16T01:49:57.64719Z | [原文](/archive/records/b035.json) |
"""


class TestRangeExpansion(unittest.TestCase):
    """台账的原生 ID 列大量使用范围写法，必须展开成逐条编号。"""

    def expand(self, cell):
        sys.path.insert(0, str(SAMPLE.parent))
        try:
            import importlib

            mod = importlib.import_module("sample")
            importlib.reload(mod)
            return mod.expand_codes(cell)
        finally:
            sys.path.pop(0)

    def test_numeric_dotted_range(self):
        self.assertEqual(
            self.expand("A05.1–A05.9"),
            [f"A05.{i}" for i in range(1, 10)],
        )

    def test_alpha_suffix_range(self):
        self.assertEqual(self.expand("B01a–B01f"), [f"B01{c}" for c in "abcdef"])

    def test_zero_padded_hyphen_range(self):
        self.assertEqual(
            self.expand("E01-01—E01-04"),
            ["E01-01", "E01-02", "E01-03", "E01-04"],
        )

    def test_right_side_may_omit_shared_prefix(self):
        self.assertEqual(
            self.expand("N01-E01—E06"),
            [f"N01-E0{i}" for i in range(1, 7)],
        )

    def test_right_side_may_be_bare_number(self):
        self.assertEqual(self.expand("E04-14—18"), [f"E04-{i}" for i in range(14, 19)])

    def test_single_letter_prefix_range(self):
        self.assertEqual(self.expand("M05—M07"), ["M05", "M06", "M07"])

    def test_comma_separated_codes_are_not_ranges(self):
        self.assertEqual(self.expand("A10a、A10b、A10c"), ["A10a", "A10b", "A10c"])

    def test_plain_code_is_returned_as_is(self):
        self.assertEqual(self.expand("B03.1"), ["B03.1"])

    def test_unexpandable_token_survives_as_itself(self):
        self.assertEqual(self.expand("008-GIT"), ["008-GIT"])


class TestSample(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name)
        self.addCleanup(self._tmp.cleanup)
        self.ledger = self.root / "ledger.md"
        self.ledger.write_text(LEDGER, encoding="utf-8")
        self.appendix = self.root / "appendix.md"
        self.appendix.write_text(APPENDIX, encoding="utf-8")
        self.sheet = self.root / "sheet.md"
        self.manifest = self.root / "sample.json"

    def run_sample(self, *extra):
        proc = subprocess.run(
            [
                sys.executable, str(SAMPLE),
                "--ledger", str(self.ledger),
                "--appendix", str(self.appendix),
                "--sheet", str(self.sheet),
                "--manifest", str(self.manifest),
                *extra,
            ],
            capture_output=True, text=True,
        )
        return proc.returncode, proc.stdout, proc.stderr

    def codes(self):
        return [r["code"] for r in json.loads(self.manifest.read_text(encoding="utf-8"))["sample"]]

    def test_same_seed_gives_same_sample(self):
        code, _, err = self.run_sample("--n", "3", "--seed", "7")
        self.assertEqual(code, 0, err)
        first = self.codes()
        self.run_sample("--n", "3", "--seed", "7")
        self.assertEqual(first, self.codes())

    def test_different_seed_can_change_order(self):
        self.run_sample("--n", "4", "--seed", "1")
        a = self.codes()
        self.run_sample("--n", "4", "--seed", "999")
        self.assertNotEqual(a, self.codes())

    def test_n_caps_the_sample_size(self):
        self.run_sample("--n", "2", "--seed", "7")
        self.assertEqual(len(self.codes()), 2)

    def test_only_message_level_rows_are_eligible(self):
        self.run_sample("--n", "99", "--seed", "7")
        got = set(self.codes())
        self.assertNotIn("A01", got)  # 源码静态阅读，且无 ULID
        self.assertEqual(got, {"A02a", "A02b", "A03a", "A04a", "B03.1", "B03.5"})

    def test_sheet_rows_omit_ai_classification(self):
        """台账的「断裂类别」不得出现在数据行里（指令段的类别选项不算泄漏）。"""
        self.run_sample("--n", "99", "--seed", "7")
        body = [
            l for l in self.sheet.read_text(encoding="utf-8").splitlines()
            if l.startswith("|") and len(l.split("|")) == 9 and "片段编号" not in l
        ]
        self.assertTrue(body)
        for line in body:
            for leaked in ("责任", "凭据", "成果", "适用范围", "剩余义务"):
                self.assertNotIn(leaked, line, f"数据行泄漏了 {leaked}: {line}")

    def test_sheet_omits_purpose_descriptions_anywhere(self):
        """附录的「用途」描述带着 AI 的解读，整表任何位置都不得出现。"""
        self.run_sample("--n", "99", "--seed", "7")
        sheet = self.sheet.read_text(encoding="utf-8")
        for leaked in ("恢复失败与外部介入", "等待中心修复", "用户纠正", "交付验收表述"):
            self.assertNotIn(leaked, sheet, f"泄漏了 {leaked}")

    def test_sheet_carries_locator_and_blank_verdict(self):
        self.run_sample("--n", "1", "--seed", "7")
        sheet = self.sheet.read_text(encoding="utf-8")
        self.assertIn("01K", sheet)
        self.assertIn("/archive/records/", sheet)
        self.assertIn("| 判定 |", sheet.replace("｜", "|"))


if __name__ == "__main__":
    unittest.main(verbosity=2)
