#!/usr/bin/env python3
"""比对人工盲编码表与 AI 侧编码，报原始一致率、Cohen's κ 与逐条分歧。

用法：
  agreement.py --sheet sheet.md --ai ai-coding.json [--out agreement.md]

退出码 1 的情形：两侧片段编号对不上（读的不是同一条记录）、
或出现四类之外的判定值。一致率低本身不是错误，不改变退出码。
"""

import argparse
import hashlib
import json
import re
import sys
from pathlib import Path

CATEGORIES = ("A", "B", "C", "D")
ROW = re.compile(r"^\|\s*(\d+)\s*\|")


def read_sheet(path):
    """返回 {行号: {"code":..., "verdict":..., "note":...}}；未填判定的 verdict 为 ""。"""
    out = {}
    for line in Path(path).read_text(encoding="utf-8").splitlines():
        if not ROW.match(line):
            continue
        cells = [c.strip() for c in line.strip().strip("|").split("|")]
        if len(cells) < 6:
            continue
        out[cells[0]] = {
            "code": cells[1].strip("`"),
            "verdict": cells[5].strip().upper(),
            "note": cells[6] if len(cells) > 6 else "",
        }
    return out


def read_ai(path):
    data = json.loads(Path(path).read_text(encoding="utf-8"))
    return {
        k: {"code": v["code"], "verdict": v["verdict"].strip().upper(), "note": v.get("note", "")}
        for k, v in data["codes"].items()
    }, data


def kappa(pairs):
    """Cohen's κ：期望一致取两侧各自的边缘分布，不是 1/4。

    两侧都只用一个类别时 pe == 1，κ 的分母为 0 —— 此时 κ 无定义，返回 None。
    把它当成 1.0 会把「样本里只出现一类」说成完美一致。
    """
    n = len(pairs)
    if n == 0:
        return None, 0.0, 0.0, "no comparable rows"
    po = sum(1 for h, a in pairs if h == a) / n
    pe = 0.0
    for c in CATEGORIES:
        ph = sum(1 for h, _a in pairs if h == c) / n
        pa = sum(1 for _h, a in pairs if a == c) / n
        pe += ph * pa
    if abs(1.0 - pe) < 1e-12:
        return None, po, pe, "undefined: expected agreement is 1 (both sides used a single category)"
    return (po - pe) / (1.0 - pe), po, pe, ""


def render(report):
    lines = [
        "# 盲编码一致率",
        "",
        f"- 比对条目：{report['n_compared']} / 抽样 {report['n_rows']}",
        f"- 原始一致率：{report['observed_agreement']:.3f}"
        if report["observed_agreement"] is not None
        else "- 原始一致率：无可比对条目",
        f"- 期望一致（边缘分布）：{report['expected_agreement']:.3f}",
        f"- Cohen's κ：{report['cohens_kappa']:.3f}"
        if report["cohens_kappa"] is not None
        else f"- Cohen's κ：无定义（{report['kappa_note']}）",
        f"- 人工未填：{report['missing_human'] or '无'}",
        f"- AI 未填：{report['missing_ai'] or '无'}",
        "",
        "## 混淆矩阵（行＝人工，列＝AI）",
        "",
        "| 人工＼AI | " + " | ".join(CATEGORIES) + " |",
        "|---|" + "---|" * len(CATEGORIES),
    ]
    for h in CATEGORIES:
        lines.append(f"| {h} | " + " | ".join(str(report["confusion"][h][a]) for a in CATEGORIES) + " |")
    lines += ["", "## 分歧条目", ""]
    if not report["disagreements"]:
        lines.append("无。")
    else:
        lines += ["| # | 片段编号 | 人工 | AI | 人工备注 | AI 备注 |", "|---:|---|---|---|---|---|"]
        for d in report["disagreements"]:
            lines.append(
                f"| {d['row']} | {d['code']} | {d['human']} | {d['ai']} | "
                f"{d['human_note'] or '—'} | {d['ai_note'] or '—'} |"
            )
    lines += [
        "",
        "## 来源",
        "",
        f"- 人工表：`{report['sheet']}`（sha256 `{report['sheet_sha256']}`）",
        f"- AI 侧：`{report['ai']}`（sha256 `{report['ai_sha256']}`）",
        f"- AI 侧盲法：{report['ai_blinding']}",
        "",
    ]
    return "\n".join(lines)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--sheet", required=True)
    ap.add_argument("--ai", required=True)
    ap.add_argument("--out")
    args = ap.parse_args()

    human = read_sheet(args.sheet)
    ai, ai_raw = read_ai(args.ai)

    code_mismatches = [
        {"row": r, "sheet": human[r]["code"], "ai": ai[r]["code"]}
        for r in sorted(set(human) & set(ai), key=int)
        if human[r]["code"] != ai[r]["code"]
    ]
    invalid = []
    for side, src in (("human", human), ("ai", ai)):
        for r in sorted(src, key=int):
            v = src[r]["verdict"]
            if v and v not in CATEGORIES:
                invalid.append({"row": r, "side": side, "verdict": v})

    rows = sorted(set(human) & set(ai), key=int)
    pairs, disagreements = [], []
    confusion = {h: {a: 0 for a in CATEGORIES} for h in CATEGORIES}
    for r in rows:
        h, a = human[r]["verdict"], ai[r]["verdict"]
        if h not in CATEGORIES or a not in CATEGORIES:
            continue
        pairs.append((h, a))
        confusion[h][a] += 1
        if h != a:
            disagreements.append(
                {
                    "row": r,
                    "code": human[r]["code"],
                    "human": h,
                    "ai": a,
                    "human_note": human[r]["note"],
                    "ai_note": ai[r]["note"],
                }
            )

    k, po, pe, note = kappa(pairs)
    report = {
        "sheet": args.sheet,
        "ai": args.ai,
        "sheet_sha256": hashlib.sha256(Path(args.sheet).read_bytes()).hexdigest(),
        "ai_sha256": hashlib.sha256(Path(args.ai).read_bytes()).hexdigest(),
        "ai_blinding": ai_raw.get("blinding", "未声明"),
        "n_rows": len(human),
        "n_compared": len(pairs),
        "observed_agreement": po if pairs else None,
        "expected_agreement": pe,
        "cohens_kappa": k,
        "kappa_note": note,
        "missing_human": [r for r in sorted(human, key=int) if human[r]["verdict"] not in CATEGORIES],
        "missing_ai": [r for r in sorted(ai, key=int) if ai[r]["verdict"] not in CATEGORIES],
        "rows_only_in_sheet": sorted(set(human) - set(ai), key=int),
        "rows_only_in_ai": sorted(set(ai) - set(human), key=int),
        "confusion": confusion,
        "human_distribution": {c: sum(1 for h, _a in pairs if h == c) for c in CATEGORIES},
        "ai_distribution": {c: sum(1 for _h, a in pairs if a == c) for c in CATEGORIES},
        "disagreements": disagreements,
        "code_mismatches": code_mismatches,
        "invalid_verdicts": invalid,
    }
    print(json.dumps(report, ensure_ascii=False, indent=1))
    if args.out:
        Path(args.out).write_text(render(report), encoding="utf-8")
    return 1 if code_mismatches or invalid else 0


if __name__ == "__main__":
    sys.exit(main())
