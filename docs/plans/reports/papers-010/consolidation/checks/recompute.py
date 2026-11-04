#!/usr/bin/env python3
"""从底层数据复算论文引用的数字。

用法：
  recompute.py --paper <path> --data-root <dir> [--also-scan tables,captions,abstract]

每个检查给出 derived（从数据算出的值）与 status：
  ok                  —— 该数值在被扫描的文本里出现
  cited_not_found     —— 数据算得的值在文本里找不到
  semantics_mismatch  —— 文本的说法与数据列的语义不符
  arithmetic_mismatch —— 文本自述的数字之间算不平
  not_found           —— 这篇论文没有该检查的锚点，跳过
  no_data             —— 缺相应数据文件，跳过
"""

import argparse
import csv
import json
import re
import statistics
import sys
from pathlib import Path

from _common import contains_number, scanned_text

NEUTRAL = {"not_found", "no_data"}


def _rows(path):
    if not path.exists():
        return None
    with path.open(encoding="utf-8") as fh:
        return list(csv.DictReader(fh))


def _int(row, key):
    return int(row[key])


def p2_checks(data_root):
    rows = _rows(data_root / "p2-pairs.csv")
    if rows is None:
        return [{"id": i, "status": "no_data", "derived": None}
                for i in ("p2_controller_sum", "p2_worker_sum", "p2_total_sum",
                          "p2_both_delivered_n", "p2_both_delivered_median",
                          "p2_both_delivered_range", "p2_format_errors_semantics")]
    sub = [r for r in rows if r["both_delivered"].strip().lower() == "true"]
    totals = [_int(r, "total") for r in sub]
    out = [
        {"id": "p2_controller_sum", "derived": sum(_int(r, "controller_turns") for r in rows)},
        {"id": "p2_worker_sum", "derived": sum(_int(r, "worker_decisions") for r in rows)},
        {"id": "p2_total_sum", "derived": sum(_int(r, "total") for r in rows)},
        {"id": "p2_both_delivered_n", "derived": len(sub)},
        {"id": "p2_both_delivered_median",
         "derived": int(statistics.median(totals)) if totals else None},
        {"id": "p2_both_delivered_range",
         "derived": [min(totals), max(totals)] if totals else None},
    ]
    negatives = [r for r in rows if _int(r, "format_errors") < 0]
    out.append({
        "id": "p2_format_errors_semantics",
        "derived": {"rows_with_negative_values": len(negatives), "column_is_delta": bool(negatives)},
        "note": "format_errors 为长链减短链的差值，不能当作格式错误的发生次数",
    })
    return out


def p4_checks(data_root):
    rows = _rows(data_root / "p4-transitions.csv")
    ids = ("p4_transitions_total", "p4_topology_changed", "p4_topology_unchanged",
           "p4_parent_retained", "p4_became_completed_changed",
           "p4_became_completed_unchanged")
    if rows is None:
        return [{"id": i, "status": "no_data", "derived": None} for i in ids]
    changed = [r for r in rows if r["topology_changed"].strip().lower() == "true"]
    unchanged = [r for r in rows if r["topology_changed"].strip().lower() == "false"]
    return [
        {"id": "p4_transitions_total", "derived": len(rows)},
        {"id": "p4_topology_changed", "derived": len(changed)},
        {"id": "p4_topology_unchanged", "derived": len(unchanged)},
        {"id": "p4_parent_retained",
         "derived": sum(1 for r in rows if r["all_parent_nodes_retained"].strip().lower() == "true")},
        {"id": "p4_became_completed_changed",
         "derived": sum(1 for r in changed if _int(r, "became_completed") > 0)},
        {"id": "p4_became_completed_unchanged",
         "derived": sum(1 for r in unchanged if _int(r, "became_completed") > 0)},
    ]


def rt017_checks(data_root):
    path = data_root / "run-status.json"
    if not path.exists():
        for alt in (data_root.parent / "runtime-experiments-017" / "run-status.json",):
            if alt.exists():
                path = alt
                break
    if not path.exists():
        return [{"id": i, "status": "no_data", "derived": None}
                for i in ("rt017_episodes", "rt017_reported_cost")]
    data = json.loads(path.read_text(encoding="utf-8"))
    return [
        {"id": "rt017_episodes", "derived": data.get("complete")},
        {"id": "rt017_reported_cost", "derived": data.get("reported_cost_usd")},
    ]


CENSUS_ANCHORS = {
    "total_tasks": re.compile(r"([\d,]+)\s*个\s*Task\s*中"),
    "with_actions": re.compile(r"([\d,]+)\s*个关联到动作日志"),
    "total_actions": re.compile(r"全部\s*([\d,]+)\s*条\s*Task\s*动作"),
    "migrated": re.compile(r"([\d,]+)\s*条由迁移过程重建"),
    "nonmig_actions": re.compile(r"其余\s*([\d,]+)\s*条非迁移动作"),
    "nonmig_tasks": re.compile(r"非迁移动作覆盖\s*([\d,]+)\s*个\s*Task"),
    "only_migrated": re.compile(r"([\d,]+)\s*个\s*Task\s*只有迁移记录"),
    "no_actions": re.compile(r"([\d,]+)\s*个没有动作行"),
}


def census_check(full_text):
    vals = {}
    for name, pat in CENSUS_ANCHORS.items():
        m = pat.search(full_text)
        if m:
            vals[name] = int(m.group(1).replace(",", ""))
    if len(vals) < len(CENSUS_ANCHORS):
        return {"id": "p1_census_closure", "status": "not_found",
                "derived": {"found": sorted(vals)}}
    eqs = {
        "tasks_partition": vals["nonmig_tasks"] + vals["only_migrated"] + vals["no_actions"]
        == vals["total_tasks"],
        "actions_partition": vals["nonmig_actions"] + vals["migrated"] == vals["total_actions"],
        "with_actions_sum": vals["nonmig_tasks"] + vals["only_migrated"] == vals["with_actions"],
    }
    ok = all(eqs.values())
    return {
        "id": "p1_census_closure",
        "status": "ok" if ok else "arithmetic_mismatch",
        "derived": {"values": vals, "equations": eqs},
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--paper", required=True)
    ap.add_argument("--data-root", required=True)
    ap.add_argument("--also-scan", default="")
    ap.add_argument(
        "--checks",
        default="",
        help="只跑 id 以这些前缀开头的检查，逗号分隔（如 p2,p4）；默认全跑",
    )
    args = ap.parse_args()

    enabled = {"body"} | {s.strip() for s in args.also_scan.split(",") if s.strip()}
    full_text = Path(args.paper).read_text(encoding="utf-8")
    haystack = scanned_text(full_text, enabled)
    data_root = Path(args.data_root)

    prefixes = tuple(p.strip() for p in args.checks.split(",") if p.strip())
    checks = p2_checks(data_root) + p4_checks(data_root) + rt017_checks(data_root)
    if prefixes:
        checks = [c for c in checks if c["id"].startswith(prefixes)]

    for chk in checks:
        if chk.get("status"):
            continue
        if chk["id"] == "p2_format_errors_semantics":
            claims_count = re.search(r"([\d,]+)\s*次格式错误", haystack)
            chk["status"] = (
                "semantics_mismatch"
                if claims_count and chk["derived"]["column_is_delta"]
                else "ok"
            )
            if claims_count:
                chk["cited"] = claims_count.group(0)
            continue
        derived = chk["derived"]
        if derived is None:
            chk["status"] = "no_data"
            continue
        values = derived if isinstance(derived, list) else [derived]
        chk["status"] = (
            "ok" if all(contains_number(haystack, v) for v in values) else "cited_not_found"
        )

    if not prefixes or "p1".startswith(prefixes) or any(p == "p1" for p in prefixes):
        checks.append(census_check(full_text))

    failing = [c for c in checks if c["status"] not in ({"ok"} | NEUTRAL)]
    report = {
        "paper": args.paper,
        "data_root": str(data_root),
        "scanned": sorted(enabled),
        "checks": checks,
        "failing": [c["id"] for c in failing],
    }
    print(json.dumps(report, ensure_ascii=False, indent=1))
    return 1 if failing else 0


if __name__ == "__main__":
    sys.exit(main())
