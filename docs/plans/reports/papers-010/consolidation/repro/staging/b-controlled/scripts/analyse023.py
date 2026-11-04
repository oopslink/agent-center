#!/usr/bin/env python3
"""023 正式批次的评分与汇总。

终点在正式调用之前定义并冻结：本文件的哈希写入 design-lock.json，
脚本启动时自校验，不一致即退出，以免评分映射事后漂移。

实验一（协调成本）主终点：
  - controller_decisions / worker_decisions / total_decisions —— 连续量，试跑中从 3 跨到 27
  - evidence_cycles —— 控制者为重新取得证据而发起的动作数（spawn 与 run_tests 之和）
次要终点：autonomous_delivery、independent_pass（均带天花板／地板说明）

实验二（凭据认可）主终点：
  - evidence_cycles —— 面对一份来自另一会话的通过回执，控制者又索证了几轮
  - closed_within_budget —— 是否在预算内闭合（status==reported_complete）
反例终点（仅 stale 起点）：
  - wrongly_closed —— 回执绑定的产物版本已过期，却仍报告完成（premature_completion）

全部终点均由 result.json 的字段计算，不含人工判断。
"""

import argparse
import hashlib
import json
import statistics
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent
EVIDENCE_OPS = ("spawn", "run_tests")


def self_hash():
    return hashlib.sha256((ROOT / "analyse023.py").read_bytes()).hexdigest()


def endpoints(result):
    row = result["row"]
    actions = [a.get("op") if isinstance(a, dict) else None for a in result["controller_actions"]]
    worker_decisions = sum(w["turns"] for w in result["worker_summaries"])
    return {
        "id": result["id"],
        "experiment": row["experiment"],
        "task": row["task"],
        "family": row["family"],
        "split": row["split"],
        "model": row["model"],
        "organization": row.get("organization"),
        "transport": row["transport"],
        "seed": row.get("seed_cross_session") or "failed_execution",
        "status": result["status"],
        "controller_decisions": result["controller_turns"],
        "worker_decisions": worker_decisions,
        "total_decisions": result["controller_turns"] + worker_decisions,
        "worker_starts": result["worker_starts"],
        "evidence_cycles": sum(1 for a in actions if a in EVIDENCE_OPS),
        "receipt_listings": sum(1 for a in actions if a == "list_receipts"),
        "autonomous_delivery": bool(result["autonomous_delivery"]),
        "independent_pass": bool(result["independent_pass_at_stop"]),
        "closed_within_budget": result["status"] == "reported_complete",
        "wrongly_closed": bool(result["premature_completion"]),
        "budget_exhausted": result["status"] == "budget_exhausted",
        "acceptance_errors": result["final_acceptance"]["errors"],
        "invalid_run": result["status"] in ("transport_invalid", "runtime_invalid"),
    }


def describe(rows, keys, metrics):
    groups = {}
    for r in rows:
        groups.setdefault(tuple(r[k] for k in keys), []).append(r)
    out = []
    for key, items in sorted(groups.items(), key=lambda kv: [str(x) for x in kv[0]]):
        cell = dict(zip(keys, key))
        cell["n"] = len(items)
        for m in metrics:
            values = [r[m] for r in items]
            if all(isinstance(v, bool) for v in values):
                cell[m] = f"{sum(values)}/{len(values)}"
            else:
                cell[m + "_median"] = statistics.median(values)
                cell[m + "_range"] = [min(values), max(values)]
        out.append(cell)
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--episodes", required=True)
    ap.add_argument("--lock", required=True)
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    lock = json.loads(Path(args.lock).read_text())
    expected = lock.get("scoring_sha256")
    if expected and expected != self_hash():
        print(f"评分映射与 design-lock 不一致：lock={expected} actual={self_hash()}", file=sys.stderr)
        return 1

    rows = []
    for p in sorted(Path(args.episodes).glob("*/result.json")):
        rows.append(endpoints(json.loads(p.read_text())))

    valid = [r for r in rows if not r["invalid_run"]]
    e1 = [r for r in valid if r["experiment"] == "coordination_cost"]
    e2 = [r for r in valid if r["experiment"] == "receipt_recognition"]

    report = {
        "scoring_sha256": self_hash(),
        "episodes": len(rows),
        "invalid": len(rows) - len(valid),
        "experiment_1": {
            "n": len(e1),
            "by_organization_transport_model": describe(
                e1, ["organization", "transport", "model"],
                ["total_decisions", "controller_decisions", "worker_decisions",
                 "evidence_cycles", "autonomous_delivery", "independent_pass"]),
            "by_organization": describe(
                e1, ["organization"],
                ["total_decisions", "evidence_cycles", "autonomous_delivery"]),
            "by_transport": describe(
                e1, ["transport"],
                ["total_decisions", "evidence_cycles", "autonomous_delivery"]),
            "by_split": describe(e1, ["split"], ["total_decisions", "autonomous_delivery"]),
        },
        "experiment_2": {
            "n": len(e2),
            "by_transport_seed": describe(
                e2, ["transport", "seed"],
                ["evidence_cycles", "receipt_listings", "closed_within_budget",
                 "wrongly_closed", "independent_pass"]),
            "by_transport_seed_model": describe(
                e2, ["transport", "seed", "model"],
                ["evidence_cycles", "closed_within_budget", "wrongly_closed"]),
        },
        "rows": rows,
    }
    Path(args.out).write_text(json.dumps(report, ensure_ascii=False, indent=1) + "\n")
    print(f"写入 {args.out}：{len(rows)} 个 episode（无效 {report['invalid']}）")
    return 0


if __name__ == "__main__":
    sys.exit(main())
