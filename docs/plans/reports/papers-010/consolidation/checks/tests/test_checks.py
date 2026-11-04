"""检查脚本自身的测试。

这些测试用极小的合成 fixture 验证三个检查器会不会报出它们该报的东西，
不依赖真实初稿与真实数据——对真实材料的扫描是 baseline-violations.md 的工作。
"""

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

CHECKS = Path(__file__).resolve().parent.parent


def run(script, *args):
    proc = subprocess.run(
        [sys.executable, str(CHECKS / script), *args],
        capture_output=True,
        text=True,
    )
    return proc.returncode, proc.stdout, proc.stderr


class TempTree(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name)
        self.addCleanup(self._tmp.cleanup)

    def write(self, rel, text):
        p = self.root / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(text, encoding="utf-8")
        return p


BANNED = "净效应\n优于\n收益\n发生率\n学习曲线\n显著改善\n"


class TestClaimStrength(TempTree):
    def test_banned_term_in_abstract_is_blocker(self):
        paper = self.write(
            "paper.md",
            "# 标题\n\n## 中文摘要\n\n本文表明 A 优于 B。\n\n## 1 引言\n\n无。\n\n## 7 结论\n\n无。\n",
        )
        banned = self.write("banned.txt", BANNED)
        code, out, _ = run(
            "claim_strength.py",
            "--paper", str(paper),
            "--banned", str(banned),
            "--sections", "abstract,conclusion,body",
        )
        report = json.loads(out)
        self.assertEqual(code, 1)
        self.assertTrue(
            any(h["term"] == "优于" and h["section"] == "abstract" and h["level"] == "blocker"
                for h in report["hits"]),
            report,
        )

    def test_banned_term_in_body_is_warning_not_blocker(self):
        paper = self.write(
            "paper.md",
            "# 标题\n\n## 中文摘要\n\n无。\n\n## 4 研究发现\n\n这里讨论发生率的问题。\n\n## 7 结论\n\n无。\n",
        )
        banned = self.write("banned.txt", BANNED)
        code, out, _ = run(
            "claim_strength.py",
            "--paper", str(paper),
            "--banned", str(banned),
            "--sections", "abstract,conclusion,body",
        )
        report = json.loads(out)
        self.assertEqual(code, 0)
        self.assertEqual([h["level"] for h in report["hits"]], ["warning"])

    def test_allow_marker_exempts_a_deliberate_negated_use(self):
        """行内 <!--allow-banned--> 标记使该行的命中降级为 allowed，而非 blocker。"""
        paper = self.write(
            "paper.md",
            "# 标题\n\n## 7 结论\n\n"
            "本文没有估计发生率。<!--allow-banned:发生率-->\n\n"
            "另一处声称 A 优于 B。\n",
        )
        banned = self.write("banned.txt", BANNED)
        code, out, _ = run(
            "claim_strength.py",
            "--paper", str(paper),
            "--banned", str(banned),
            "--sections", "abstract,conclusion,body",
        )
        report = json.loads(out)
        levels = {(h["term"], h["level"]) for h in report["hits"]}
        self.assertIn(("发生率", "allowed"), levels, report)
        self.assertIn(("优于", "blocker"), levels, report)
        self.assertEqual(code, 1)  # 未标记的"优于"仍然是 blocker

    def test_allow_marker_exempts_only_its_own_sentence(self):
        """标记只豁免它所在的那一句；同一行其它句子的命中仍是 blocker。"""
        paper = self.write(
            "paper.md",
            "# 标题\n\n## 7 结论\n\n"
            "本文没有估计发生率。<!--allow-banned:发生率-->"
            "另外本架构的断裂发生率显著低于同类架构。\n",
        )
        banned = self.write("banned.txt", BANNED)
        code, out, _ = run(
            "claim_strength.py", "--paper", str(paper), "--banned", str(banned),
            "--sections", "abstract,conclusion,body",
        )
        report = json.loads(out)
        levels = sorted((h["term"], h["level"]) for h in report["hits"])
        self.assertIn(("发生率", "allowed"), levels, report)
        self.assertIn(("发生率", "blocker"), levels, report)
        self.assertEqual(code, 1)

    def test_allow_marker_only_exempts_the_named_term(self):
        paper = self.write(
            "paper.md",
            "# 标题\n\n## 7 结论\n\n本文不比较收益也不给发生率。<!--allow-banned:发生率-->\n",
        )
        banned = self.write("banned.txt", BANNED)
        code, out, _ = run(
            "claim_strength.py",
            "--paper", str(paper),
            "--banned", str(banned),
            "--sections", "abstract,conclusion,body",
        )
        report = json.loads(out)
        levels = {(h["term"], h["level"]) for h in report["hits"]}
        self.assertIn(("发生率", "allowed"), levels)
        self.assertIn(("收益", "blocker"), levels)
        self.assertEqual(code, 1)

    def test_clean_paper_has_no_hits(self):
        paper = self.write(
            "paper.md",
            "# 标题\n\n## 中文摘要\n\n我们观察到三类断裂。\n\n## 7 结论\n\n记录支持其存在。\n",
        )
        banned = self.write("banned.txt", BANNED)
        code, out, _ = run(
            "claim_strength.py",
            "--paper", str(paper),
            "--banned", str(banned),
            "--sections", "abstract,conclusion,body",
        )
        self.assertEqual(code, 0)
        self.assertEqual(json.loads(out)["hits"], [])


class TestCrossPositionConsistency(TempTree):
    """同一个量在摘要/正文/表/图题四处出现时，必须逐处一致。

    原实现把四类位置拼成一个字符串做成员判定，于是"改一处漏三处"恰好是
    它结构上无法检出的——这正是计划 Review Focus 第 1 条要求抓的失败模式。
    """

    def make(self, body_n, abstract_n=None, table_n=None, caption_n=None):
        parts = ["# 标题", ""]
        if abstract_n is not None:
            parts += ["## 摘要", "", f"共 {abstract_n} 次父子转移。", ""]
        parts += ["## 4 结果", "", f"记录中共 {body_n} 次父子转移。", ""]
        if table_n is not None:
            parts += ["| 项 | 值 |", "|---|---:|", f"| 父子转移 | {table_n} 次父子转移 |", ""]
        if caption_n is not None:
            parts += [f"**表 1。**共 {caption_n} 次父子转移的分布。", ""]
        return self.write("paper.md", "\n".join(parts))

    def data(self):
        self.write(
            "data/p4-transitions.csv",
            "id,parent,plan,topology_changed,all_parent_nodes_retained,shared_status_changes,"
            "became_completed,left_completed\n"
            + "".join(f"g{i},g0,p1,True,True,1,1,0\n" for i in range(3)),
        )
        return self.root / "data"

    def run_it(self, paper, data, *extra):
        return run("recompute.py", "--paper", str(paper), "--data-root", str(data),
                   "--checks", "p4", "--also-scan", "tables,captions,abstract", *extra)

    def test_all_positions_agreeing_passes(self):
        data = self.data()
        paper = self.make(3, abstract_n=3, table_n=3, caption_n=3)
        code, out, _ = self.run_it(paper, data)
        ids = {c["id"]: c for c in json.loads(out)["checks"]}
        self.assertEqual(ids["p4_transitions_total"]["status"], "ok", out)
        self.assertEqual(code, 0, out)

    def test_abstract_disagreeing_with_body_is_detected(self):
        """只改摘要、正文不动 —— 原实现在这里返回 ok。"""
        data = self.data()
        paper = self.make(3, abstract_n=9)
        code, out, _ = self.run_it(paper, data)
        ids = {c["id"]: c for c in json.loads(out)["checks"]}
        chk = ids["p4_transitions_total"]
        self.assertEqual(chk["status"], "position_conflict", out)
        self.assertIn("abstract", json.dumps(chk, ensure_ascii=False))
        self.assertEqual(code, 1)

    def test_table_disagreeing_with_body_is_detected(self):
        data = self.data()
        paper = self.make(3, table_n=9)
        code, out, _ = self.run_it(paper, data)
        ids = {c["id"]: c for c in json.loads(out)["checks"]}
        self.assertEqual(ids["p4_transitions_total"]["status"], "position_conflict", out)
        self.assertEqual(code, 1)

    def test_caption_disagreeing_with_body_is_detected(self):
        data = self.data()
        paper = self.make(3, caption_n=9)
        code, out, _ = self.run_it(paper, data)
        ids = {c["id"]: c for c in json.loads(out)["checks"]}
        self.assertEqual(ids["p4_transitions_total"]["status"], "position_conflict", out)
        self.assertEqual(code, 1)

    def test_positions_where_the_anchor_is_absent_are_not_required(self):
        """没提这个量的位置不算冲突。"""
        data = self.data()
        paper = self.make(3)
        code, out, _ = self.run_it(paper, data)
        ids = {c["id"]: c for c in json.loads(out)["checks"]}
        self.assertEqual(ids["p4_transitions_total"]["status"], "ok", out)
        self.assertEqual(code, 0, out)

    def test_unanchored_checks_are_reported_not_silently_neutral(self):
        """锚点在任何位置都找不到时，必须在报告里列出，而不是静默中立。"""
        data = self.data()
        paper = self.write("paper.md", "# 标题\n\n## 4 结果\n\n表里有个 3。\n")
        code, out, _ = self.run_it(paper, data)
        rep = json.loads(out)
        self.assertIn("p4_transitions_total", rep["unanchored"])
        self.assertEqual(code, 0, out)  # 不计为失败，但必须可见

    def test_report_lists_the_value_found_in_each_position(self):
        data = self.data()
        paper = self.make(3, abstract_n=9, table_n=3)
        _code, out, _ = self.run_it(paper, data)
        chk = {c["id"]: c for c in json.loads(out)["checks"]}["p4_transitions_total"]
        self.assertEqual(chk["positions"]["abstract"], [9])
        self.assertEqual(chk["positions"]["body"], [3])
        self.assertEqual(chk["positions"]["tables"], [3])


class TestNumberCandidates(unittest.TestCase):
    """浮点数的候选写法不能被 6 位有效数字截断。"""

    def setUp(self):
        sys.path.insert(0, str(CHECKS))
        import importlib

        self.mod = importlib.import_module("_common")
        importlib.reload(self.mod)
        self.addCleanup(lambda: sys.path.remove(str(CHECKS)))

    def test_full_precision_value_is_a_candidate(self):
        # :g 默认 6 位有效数字，会把 6.2051478 变成 6.20515
        self.assertTrue(self.mod.contains_number("报告价 6.2051478 美元", 6.205147799999996))

    def test_rounded_forms_remain_candidates(self):
        for text in ("6.21", "6.205", "6.2051", "6.20515"):
            self.assertTrue(self.mod.contains_number(f"价 {text} 元", 6.205147799999996), text)

    def test_unrelated_number_is_not_matched(self):
        self.assertFalse(self.mod.contains_number("价 7.1 元", 6.205147799999996))

    def test_integer_still_matches_with_thousands_separator(self):
        self.assertTrue(self.mod.contains_number("共 26,961 条", 26961))


class TestCrossrefs(TempTree):
    def test_dangling_section_reference_is_reported(self):
        paper = self.write(
            "paper.md",
            "# 标题\n\n## 1 引言\n\n见 §4.5 的分析。\n\n## 4 研究发现\n\n### 4.1 一\n\n无。\n",
        )
        code, out, _ = run("crossrefs.py", "--paper", str(paper))
        report = json.loads(out)
        self.assertEqual(code, 1)
        self.assertIn("4.5", report["dangling_sections"])

    def test_existing_section_reference_passes(self):
        paper = self.write(
            "paper.md",
            "# 标题\n\n## 1 引言\n\n见 §4.1 的分析。\n\n## 4 研究发现\n\n### 4.1 一\n\n无。\n",
        )
        code, out, _ = run("crossrefs.py", "--paper", str(paper))
        self.assertEqual(code, 0, out)
        self.assertEqual(json.loads(out)["dangling_sections"], [])

    def test_missing_local_link_is_reported(self):
        paper = self.write("paper.md", "# 标题\n\n见[附录](appendix.md)。\n")
        code, out, _ = run("crossrefs.py", "--paper", str(paper))
        report = json.loads(out)
        self.assertEqual(code, 1)
        self.assertIn("appendix.md", report["missing_links"])

    def test_disjoint_detects_shared_record_id(self):
        a = self.write("a.md", "# A\n\n记录 `01KY4B5KS3272X3AMXDXHQMEC0` 与 `generation-709baba8`。\n")
        b = self.write("b.md", "# B\n\n记录 `01KY4B5KS3272X3AMXDXHQMEC0` 另见。\n")
        code, out, _ = run("crossrefs.py", "--disjoint", str(a), str(b))
        report = json.loads(out)
        self.assertEqual(code, 1)
        self.assertIn("01KY4B5KS3272X3AMXDXHQMEC0", report["shared_record_ids"])

    def test_disjoint_detects_shared_ledger_codes(self):
        """A 篇附录用的是 A01–A21 这类台账编号，不是 ULID；漏掉它们检查就空转。"""
        a = self.write("a.md", "# A\n\n| A21 | 多时点人工纠偏 | 原生消息 |\n")
        b = self.write("b.md", "# B\n\n附录复用案例 A21 的部署记录。\n")
        code, out, _ = run("crossrefs.py", "--disjoint", str(a), str(b))
        report = json.loads(out)
        self.assertIn("A21", report["shared_record_ids"], report)
        self.assertEqual(code, 1)

    def test_disjoint_detects_shared_evidence_codes_of_other_families(self):
        for code_text in ("B03.1", "N01-E01", "E04-10", "M02"):
            a = self.write("a.md", f"# A\n\n| {code_text} | 用途 |\n")
            b = self.write("b.md", f"# B\n\n见 {code_text}。\n")
            rc, out, _ = run("crossrefs.py", "--disjoint", str(a), str(b))
            self.assertIn(code_text, json.loads(out)["shared_record_ids"], code_text)
            self.assertEqual(rc, 1, code_text)

    def test_disjoint_passes_when_no_shared_ids(self):
        a = self.write("a.md", "# A\n\n记录 `01KY4B5KS3272X3AMXDXHQMEC0`。\n")
        b = self.write("b.md", "# B\n\n记录 `01KZDJ41E4XMR6RSRDW2342DCE`。\n")
        code, out, _ = run("crossrefs.py", "--disjoint", str(a), str(b))
        self.assertEqual(code, 0, out)
        self.assertEqual(json.loads(out)["shared_record_ids"], [])

    def test_ledger_coverage_reads_tables_after_an_unrelated_first_table(self):
        paper = self.write(
            "paper.md",
            "# A\n\n## 4 研究发现\n\n### 4.1 责任\n\n无。\n\n### 4.2 凭据\n\n无。\n",
        )
        ledger = self.write(
            "ledger.md",
            "| 级别 | 含义 | 正文允许的表述 |\n|---|---|---|\n| 原生记录 | 冻结库 | 记录显示 |\n\n"
            "## §4.1\n\n"
            "| 断裂类别 | 原片段编号 | 原生 ID | 证据级别 | 进 A 篇的小节 | 排除理由 |\n"
            "|---|---|---|---|---|---|\n"
            "| 责任 | I01 | x | 原生记录 | 4.1 | |\n\n"
            "## §4.2\n\n"
            "| 断裂类别 | 原片段编号 | 原生 ID | 证据级别 | 进 A 篇的小节 | 排除理由 |\n"
            "|---|---|---|---|---|---|\n"
            "| 凭据 | W02 | y | 原生记录 | 4.2 | |\n",
        )
        code, out, _ = run("crossrefs.py", "--paper", str(paper), "--ledger", str(ledger))
        report = json.loads(out)
        self.assertEqual(report["sections_without_ledger_source"], [], report)
        self.assertEqual(report["ledger_sections_missing_in_paper"], [], report)
        self.assertEqual(code, 0, out)

    def test_ledger_coverage_reports_section_without_source(self):
        paper = self.write(
            "paper.md",
            "# A\n\n## 4 研究发现\n\n### 4.1 责任\n\n无。\n\n### 4.2 凭据\n\n无。\n",
        )
        ledger = self.write(
            "ledger.md",
            "| 断裂类别 | 原片段编号 | 原生 ID | 证据级别 | 进 A 篇的小节 | 排除理由 |\n"
            "|---|---|---|---|---|---|\n"
            "| 责任 | I01 | x | 原生记录 | 4.1 | |\n",
        )
        code, out, _ = run("crossrefs.py", "--paper", str(paper), "--ledger", str(ledger))
        report = json.loads(out)
        self.assertEqual(code, 1)
        self.assertIn("4.2", report["sections_without_ledger_source"])


class TestRecompute(TempTree):
    def make_data_root(self):
        self.write(
            "data/p2-pairs.csv",
            "task,model,binding,both_delivered,controller_turns,worker_decisions,worker_starts,format_errors,total\n"
            "t1,m,joined,True,3,2,1,0,5\n"
            "t2,m,joined,True,4,3,1,-1,7\n",
        )
        self.write(
            "data/p4-transitions.csv",
            "id,parent,plan,topology_changed,all_parent_nodes_retained,shared_status_changes,became_completed,left_completed\n"
            "g1,g0,p1,True,True,2,1,0\n"
            "g2,g1,p1,False,True,1,0,0\n",
        )
        self.write("data/run-status.json", json.dumps({"complete": 2, "reported_cost_usd": 1.5}))
        return self.root / "data"

    def test_cited_number_absent_from_paper_is_reported(self):
        """无锚点的检查走成员判定：数字在正文里找不到即报 cited_not_found。"""
        data = self.make_data_root()
        paper = self.write("paper.md", "# 标题\n\n控制端共增加 99 次决策。\n")
        code, out, _ = run("recompute.py", "--paper", str(paper), "--data-root", str(data))
        report = json.loads(out)
        ids = {c["id"]: c for c in report["checks"]}
        self.assertEqual(ids["p2_both_delivered_range"]["derived"], [5, 7])
        self.assertEqual(ids["p2_both_delivered_range"]["status"], "cited_not_found")
        self.assertEqual(code, 1)

    def test_all_derived_numbers_present_passes(self):
        data = self.make_data_root()
        paper = self.write(
            "paper.md",
            "# 标题\n\n控制端 7 次、工作端 5 次、合计 12 次；2 对均交付，"
            "其总增量中位数 6、范围 5 至 7。\n"
            "转移 2 次，其中 1 次拓扑改变、1 次未改变；保留全部父节点 2 次；"
            "拓扑改变中 1 次有共有任务转完成，未改变中 0 次。\n"
            "报告价 1.5 美元，2 个 episode。\n",
        )
        code, out, _ = run("recompute.py", "--paper", str(paper), "--data-root", str(data))
        report = json.loads(out)
        bad = [c for c in report["checks"] if c["status"] != "ok"]
        self.assertEqual(code, 0, json.dumps(bad, ensure_ascii=False))

    def test_format_errors_delta_column_semantics_is_flagged(self):
        data = self.make_data_root()
        paper = self.write("paper.md", "# 标题\n\n共 1 次格式错误。\n")
        code, out, _ = run("recompute.py", "--paper", str(paper), "--data-root", str(data))
        report = json.loads(out)
        ids = {c["id"]: c for c in report["checks"]}
        self.assertEqual(code, 1)
        self.assertEqual(ids["p2_format_errors_semantics"]["status"], "semantics_mismatch")

    def test_p1_census_closure_detects_broken_sum(self):
        data = self.make_data_root()
        paper = self.write(
            "paper.md",
            "# 标题\n\n2221个 Task 中，1238个关联到动作日志。\n"
            "全部2527条 Task 动作中，597条由迁移过程重建。"
            "其余1930条非迁移动作覆盖900个 Task；"
            "另有311个 Task 只有迁移记录，983个没有动作行。\n",
        )
        code, out, _ = run("recompute.py", "--paper", str(paper), "--data-root", str(data))
        ids = {c["id"]: c for c in json.loads(out)["checks"]}
        self.assertEqual(code, 1)
        self.assertEqual(ids["p1_census_closure"]["status"], "arithmetic_mismatch")

    def test_p1_census_closure_passes_on_consistent_numbers(self):
        data = self.make_data_root()
        paper = self.write(
            "paper.md",
            "# 标题\n\n2221个 Task 中，1238个关联到动作日志。\n"
            "全部2527条 Task 动作中，597条由迁移过程重建。"
            "其余1930条非迁移动作覆盖927个 Task；"
            "另有311个 Task 只有迁移记录，983个没有动作行。\n",
        )
        code, out, _ = run("recompute.py", "--paper", str(paper), "--data-root", str(data))
        ids = {c["id"]: c for c in json.loads(out)["checks"]}
        self.assertEqual(ids["p1_census_closure"]["status"], "ok")

    def test_p1_census_closure_accepts_alternative_phrasings(self):
        """锚点正则不应迫使正文只能用一种写法。"""
        data = self.make_data_root()
        paper = self.write(
            "paper.md",
            "# 标题\n\n2,527 条 Task 动作中，597 条由迁移过程重建；"
            "其余 1,930 条非迁移动作覆盖 927 个 Task，占全部 2,221 个 Task 的 41.7%。"
            "另有 311 个 Task 只有迁移记录、983 个没有动作行，"
            "因此存在任何动作行的覆盖率为 1,238 个 Task、55.7%。\n",
        )
        code, out, _ = run("recompute.py", "--paper", str(paper), "--data-root", str(data),
                           "--checks", "p1")
        ids = {c["id"]: c for c in json.loads(out)["checks"]}
        self.assertEqual(ids["p1_census_closure"]["status"], "ok", out)
        self.assertEqual(code, 0, out)

    def test_p1_census_closure_absent_anchors_are_skipped(self):
        data = self.make_data_root()
        paper = self.write("paper.md", "# 标题\n\n没有普查数字。\n")
        _, out, _ = run("recompute.py", "--paper", str(paper), "--data-root", str(data))
        ids = {c["id"]: c for c in json.loads(out)["checks"]}
        self.assertEqual(ids["p1_census_closure"]["status"], "not_found")

    def test_checks_filter_limits_scope_to_named_prefixes(self):
        data = self.make_data_root()
        paper = self.write(
            "paper.md",
            "# 标题\n\n转移 2 次，其中 1 次拓扑改变、1 次未改变；保留全部父节点 2 次；"
            "拓扑改变中 1 次有共有任务转完成，未改变中 0 次。\n",
        )
        code_all, out_all, _ = run("recompute.py", "--paper", str(paper), "--data-root", str(data))
        self.assertEqual(code_all, 1)
        self.assertTrue(
            any(i.startswith("p2_") for i in json.loads(out_all)["failing"]),
            out_all,
        )
        code, out, _ = run(
            "recompute.py", "--paper", str(paper), "--data-root", str(data), "--checks", "p4",
        )
        report = json.loads(out)
        self.assertEqual(code, 0, out)
        self.assertEqual(report["failing"], [])
        self.assertTrue(all(c["id"].startswith("p4_") for c in report["checks"]), report)

    def test_also_scan_defaults_to_body_only(self):
        data = self.make_data_root()
        paper = self.write(
            "paper.md",
            "# 标题\n\n## 中文摘要\n\n范围 5 至 7。\n\n## 1 引言\n\n无数字。\n",
        )
        _code, out, _ = run("recompute.py", "--paper", str(paper), "--data-root", str(data))
        ids = {c["id"]: c for c in json.loads(out)["checks"]}
        self.assertEqual(ids["p2_both_delivered_range"]["status"], "cited_not_found")
        _code2, out2, _ = run(
            "recompute.py", "--paper", str(paper), "--data-root", str(data),
            "--also-scan", "abstract",
        )
        ids2 = {c["id"]: c for c in json.loads(out2)["checks"]}
        # 个位数 derived 归为 ok_weak（成员判定无鉴别力），但不再是"找不到"
        self.assertIn(ids2["p2_both_delivered_range"]["status"], ("ok", "ok_weak"))


if __name__ == "__main__":
    unittest.main(verbosity=2)
