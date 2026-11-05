"""抽样框过滤与跨轮排重的测试。

第一轮的 25 条里有 10 条（用户发言、裸动作码）不在四类判据的适用范围内，
判据明文把它们归 D，于是分歧的一半出在抽样框而不是判据边界上。
抽样框必须是**机械可复算**的：agent 自述且正文达到长度下限，
不能由我逐条挑"看起来像处置选择"的记录。
"""

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SAMPLE = Path(__file__).resolve().parents[2] / "blind-coding" / "sample.py"

LEDGER = """# 台账

## §4.1

| 断裂类别 | 原片段编号 | 原生 ID | 证据级别 | 进 A 篇的小节 | 排除理由 |
|---|---|---|---|---|---|
| 责任 | agent 长 | A01 | 原生记录 | 4.1 | |
| 责任 | agent 短 | A02 | 原生记录 | 4.1 | |
| 责任 | 用户发言 | A03 | 原生记录 | 4.1 | |
| 责任 | 裸动作 | A04 | 原生记录 | 4.1 | |
| 责任 | 第二条 agent 长 | A05 | 原生记录 | 4.1 | |
"""

APPENDIX = """# P 篇

| 编号 | 用途 | 原生 ID / 类型 | UTC 时间 | 原文路径 |
|---|---|---|---|---|
| A01 | x | `01KY4B5KS3272X3AMXDXHQMEC0` | 2026-07-22T07:20:47.651234Z | [原文](RECORDS/a01.json) |
| A02 | x | `01KY4B5KS3272X3AMXDXHQMEC1` | 2026-07-22T07:20:48.651234Z | [原文](RECORDS/a02.json) |
| A03 | x | `01KY4B5KS3272X3AMXDXHQMEC2` | 2026-07-22T07:20:49.651234Z | [原文](RECORDS/a03.json) |
| A04 | x | `01KY4B5KS3272X3AMXDXHQMEC3` | 2026-07-22T07:20:50.651234Z | [原文](RECORDS/a04.json) |
| A05 | x | `01KY4B5KS3272X3AMXDXHQMEC4` | 2026-07-22T07:20:51.651234Z | [原文](RECORDS/a05.json) |
"""

LONG = "我承接这项义务并已显式调用 fork_executor，" * 6


def build(d):
    recs = d / "RECORDS"
    recs.mkdir()
    def rec(name, nid, **record):
        (recs / name).write_text(
            json.dumps({"id": name, "record": dict(id=nid, **record)}, ensure_ascii=False),
            encoding="utf-8",
        )
    rec("a01.json", "01KY4B5KS3272X3AMXDXHQMEC0",
        sender_identity_id="agent:agent-1", content=LONG)
    rec("a02.json", "01KY4B5KS3272X3AMXDXHQMEC1",
        sender_identity_id="agent:agent-1", content="收到")
    rec("a03.json", "01KY4B5KS3272X3AMXDXHQMEC2",
        sender_identity_id="user:user-1", content=LONG)
    rec("a04.json", "01KY4B5KS3272X3AMXDXHQMEC3", action="unblocked")
    rec("a05.json", "01KY4B5KS3272X3AMXDXHQMEC4",
        sender_identity_id="agent:agent-2", content=LONG)
    (d / "ledger.md").write_text(LEDGER, encoding="utf-8")
    (d / "appendix.md").write_text(APPENDIX, encoding="utf-8")


def run(d, extra=()):
    proc = subprocess.run(
        [sys.executable, str(SAMPLE),
         "--ledger", str(d / "ledger.md"),
         "--appendix", str(d / "appendix.md"),
         "--sheet", str(d / "sheet.md"),
         "--manifest", str(d / "m.json"),
         "--record-root", str(d), *extra],
        capture_output=True, text=True,
    )
    man = json.loads((d / "m.json").read_text(encoding="utf-8")) if (d / "m.json").exists() else None
    sheet = (d / "sheet.md").read_text(encoding="utf-8") if (d / "sheet.md").exists() else ""
    return proc, man, sheet


class TestFrame(unittest.TestCase):
    def test_frame_keeps_only_agent_authored_records_above_length_floor(self):
        with tempfile.TemporaryDirectory() as t:
            d = Path(t); build(d)
            _p, man, _s = run(d, ("--frame", "agent-disposition", "--n", "99"))
            self.assertEqual(sorted(r["code"] for r in man["sample"]), ["A01", "A05"])
            self.assertEqual(man["frame"], "agent-disposition")

    def test_frame_records_why_each_pool_entry_was_dropped(self):
        with tempfile.TemporaryDirectory() as t:
            d = Path(t); build(d)
            _p, man, _s = run(d, ("--frame", "agent-disposition", "--n", "99"))
            self.assertEqual(man["frame_excluded"]["A02"], "content_below_floor")
            self.assertEqual(man["frame_excluded"]["A03"], "not_agent_authored")
            self.assertEqual(man["frame_excluded"]["A04"], "no_content")

    def test_no_frame_keeps_every_resolvable_record(self):
        with tempfile.TemporaryDirectory() as t:
            d = Path(t); build(d)
            _p, man, _s = run(d, ("--n", "99"))
            self.assertEqual(len(man["sample"]), 5)
            self.assertEqual(man["frame"], "none")

    def test_exclude_drops_codes_already_coded_in_an_earlier_round(self):
        with tempfile.TemporaryDirectory() as t:
            d = Path(t); build(d)
            (d / "used.json").write_text(
                json.dumps({"sample": [{"code": "A01"}]}), encoding="utf-8")
            _p, man, _s = run(d, ("--frame", "agent-disposition", "--n", "99",
                                  "--exclude", str(d / "used.json")))
            self.assertEqual([r["code"] for r in man["sample"]], ["A05"])
            self.assertEqual(man["excluded_prior"], ["A01"])

    def test_census_when_n_covers_the_whole_frame_is_declared_in_the_sheet(self):
        with tempfile.TemporaryDirectory() as t:
            d = Path(t); build(d)
            _p, man, sheet = run(d, ("--frame", "agent-disposition", "--n", "99"))
            self.assertTrue(man["census"])
            self.assertIn("全部", sheet)
            # 普查时不能印出一个 seed 值，否则读者会以为这批是抽的
            self.assertNotIn("seed=", sheet)

    def test_sheet_still_leaks_no_classification(self):
        with tempfile.TemporaryDirectory() as t:
            d = Path(t); build(d)
            _p, _m, sheet = run(d, ("--frame", "agent-disposition", "--n", "99"))
            for leaked in ("责任", "凭据", "外推", "承责"):
                self.assertNotIn(leaked, sheet)


if __name__ == "__main__":
    unittest.main()
