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
import math
import re
import sys
from pathlib import Path

DEFAULT_CATEGORIES = ("A", "B", "C", "D")
ROW = re.compile(r"^\|\s*(\d+)\s*\|")


def _columns(lines):
    """从表头按名字定位列下标。外部编码包的表只有四列，写死下标会读错列。"""
    for line in lines:
        if not line.strip().startswith("|") or ROW.match(line):
            continue
        cells = [c.strip() for c in line.strip().strip("|").split("|")]
        if not any("判定" in c for c in cells):
            continue
        idx = {
            "verdict": next(i for i, c in enumerate(cells) if "判定" in c),
            "note": next((i for i, c in enumerate(cells) if "备注" in c), None),
            "code": next((i for i, c in enumerate(cells) if "片段编号" in c), None),
        }
        return idx
    return None


def read_sheet(path):
    """返回 {行号: {"code":..., "verdict":..., "note":...}}；未填判定的 verdict 为 ""。

    code 为 None 表示该表没有片段编号列（外部编码包只给记录序号），
    此时跨表的片段编号一致性无从检查，由调用方跳过。
    """
    lines = Path(path).read_text(encoding="utf-8").splitlines()
    idx = _columns(lines) or {"verdict": 5, "note": 6, "code": 1}
    out = {}
    for line in lines:
        if not ROW.match(line):
            continue
        cells = [c.strip() for c in line.strip().strip("|").split("|")]
        if len(cells) <= idx["verdict"]:
            continue
        note_i, code_i = idx["note"], idx["code"]
        out[cells[0]] = {
            "code": cells[code_i].strip("`") if code_i is not None and len(cells) > code_i else None,
            "verdict": cells[idx["verdict"]].strip().upper(),
            "note": cells[note_i].strip() if note_i is not None and len(cells) > note_i else "",
        }
    return out


def read_coder(path):
    """编码表或 ai-coding.json 都可以当一份编码读入。"""
    return read_ai(path)[0] if str(path).endswith(".json") else read_sheet(path)


def _clamped(k, se):
    """κ 的定义域是 [-1, 1]。正态近似在 n 小时会越界，截断并由调用方标注，
    而不是印出 1.081 这种不存在的值。"""
    if se is None:
        return None
    return [max(-1.0, k - 1.96 * se), min(1.0, k + 1.96 * se)]


def fleiss_kappa(assignments, categories):
    """多编码者的 Fleiss κ。assignments: [{coder: verdict}]，每项都被全部编码者编过。

    两两 κ 会掩盖"两人高度一致、第三人与两者都不一致"的结构，所以两个都报。
    """
    n_items = len(assignments)
    if n_items == 0:
        return None, None, None, "no items"
    n_raters = len(next(iter(assignments)))
    if n_raters < 2:
        return None, None, None, "needs at least two coders"
    p_bar = 0.0
    totals = {c: 0 for c in categories}
    for item in assignments:
        counts = {c: 0 for c in categories}
        for v in item.values():
            counts[v] += 1
        for c in categories:
            totals[c] += counts[c]
        p_bar += (sum(k * k for k in counts.values()) - n_raters) / (n_raters * (n_raters - 1))
    p_bar /= n_items
    pe = sum((totals[c] / (n_items * n_raters)) ** 2 for c in categories)
    if abs(1.0 - pe) < 1e-12:
        return None, p_bar, pe, "undefined: expected agreement is 1"
    return (p_bar - pe) / (1.0 - pe), p_bar, pe, ""


def read_ai(path):
    data = json.loads(Path(path).read_text(encoding="utf-8"))
    return {
        k: {"code": v["code"], "verdict": v["verdict"].strip().upper(), "note": v.get("note", "")}
        for k, v in data["codes"].items()
    }, data


def kappa(pairs, categories):
    """Cohen's κ：期望一致取两侧各自的边缘分布，不是 1/4。

    两侧都只用一个类别时 pe == 1，κ 的分母为 0 —— 此时 κ 无定义，返回 None。
    把它当成 1.0 会把「样本里只出现一类」说成完美一致。
    """
    n = len(pairs)
    if n == 0:
        return None, 0.0, 0.0, "no comparable rows"
    po = sum(1 for h, a in pairs if h == a) / n
    pe = 0.0
    for c in categories:
        ph = sum(1 for h, _a in pairs if h == c) / n
        pa = sum(1 for _h, a in pairs if a == c) / n
        pe += ph * pa
    if abs(1.0 - pe) < 1e-12:
        return None, po, pe, "undefined: expected agreement is 1 (both sides used a single category)"
    return (po - pe) / (1.0 - pe), po, pe, ""


def render(report):
    categories = tuple(report["categories"])
    lines = [
        "# 盲编码一致率",
        "",
        f"- 比对条目：{report['n_compared']} / 抽样 {report['n_rows']}",
        f"- 原始一致率：{report['observed_agreement']:.3f}"
        if report["observed_agreement"] is not None
        else "- 原始一致率：无可比对条目",
        f"- 期望一致（边缘分布）：{report['expected_agreement']:.3f}",
        f"- Cohen's κ：{report['cohens_kappa']:.3f}"
        f"（SE {report['kappa_se']:.3f}，95% 区间 "
        f"{report['kappa_ci95'][0]:.3f}–{report['kappa_ci95'][1]:.3f}）"
        if report["cohens_kappa"] is not None
        else f"- Cohen's κ：无定义（{report['kappa_note']}）",
        f"- 人工未填：{report['missing_human'] or '无'}",
        f"- 人工备注为空（codebook 要求逐条写理由）：{report['human_rows_without_note'] or '无'}",
        f"- AI 未填：{report['missing_ai'] or '无'}",
        "",
        "## 混淆矩阵（行＝人工，列＝AI）",
        "",
        "| 人工＼AI | " + " | ".join(categories) + " |",
        "|---|" + "---|" * len(categories),
    ]
    for h in categories:
        lines.append(f"| {h} | " + " | ".join(str(report["confusion"][h][a]) for a in categories) + " |")
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


def multi(args, categories):
    coders, order = {}, []
    for spec in args.coder:
        label, _, path = spec.partition("=")
        order.append(label)
        coders[label] = read_coder(path)

    rows = sorted(set().union(*(set(c) for c in coders.values())), key=int)
    complete, incomplete = [], []
    for r in rows:
        vs = {lab: coders[lab].get(r, {}).get("verdict", "") for lab in order}
        (complete if all(v in categories for v in vs.values()) else incomplete).append(r)

    assignments = [{lab: coders[lab][r]["verdict"] for lab in order} for r in complete]
    pairwise = []
    for i, a in enumerate(order):
        for b in order[i + 1:]:
            pairs = [(x[a], x[b]) for x in assignments]
            k, po, pe, note = kappa(pairs, categories)
            se = (math.sqrt(po * (1 - po) / (len(pairs) * (1 - pe) ** 2))
                  if k is not None and pairs else None)
            pairwise.append({
                "pair": f"{a}×{b}",
                "n": len(pairs),
                "observed_agreement": po,
                "expected_agreement": pe,
                "cohens_kappa": k,
                "kappa_se": se,
                "kappa_ci95": _clamped(k, se),
                "kappa_ci95_raw": [k - 1.96 * se, k + 1.96 * se] if se is not None else None,
                "kappa_ci95_truncated": (se is not None
                                         and (k + 1.96 * se > 1.0 or k - 1.96 * se < -1.0)),
                "kappa_note": note,
                "disagreements": [r for r, x in zip(complete, assignments) if x[a] != x[b]],
            })
    fk, pbar, fpe, fnote = fleiss_kappa(assignments, categories)

    item_table = []
    for r, x in zip(complete, assignments):
        item_table.append({
            "row": r,
            "verdicts": x,
            "unanimous": len(set(x.values())) == 1,
            "notes": {lab: coders[lab][r]["note"] for lab in order},
        })

    report = {
        "coders": order,
        "sources": {lab: spec.partition("=")[2] for lab, spec in zip(order, args.coder)},
        "categories": list(categories),
        "n_items": len(complete),
        "rows_not_coded_by_all": incomplete,
        "pairwise": pairwise,
        "fleiss_kappa": fk,
        "fleiss_pbar": pbar,
        "fleiss_pe": fpe,
        "fleiss_note": fnote,
        "unanimous_items": sum(1 for x in item_table if x["unanimous"]),
        "distributions": {lab: {c: sum(1 for x in assignments if x[lab] == c) for c in categories}
                          for lab in order},
        "item_table": item_table,
        "rows_without_note": {lab: [x["row"] for x in item_table if not x["notes"][lab]]
                              for lab in order},
    }
    print(json.dumps(report, ensure_ascii=False, indent=1))
    if args.out:
        Path(args.out).write_text(render_multi(report), encoding="utf-8")
    return 0


def render_multi(report):
    order = report["coders"]
    cats = report["categories"]
    lines = [
        "# 多编码者一致率",
        "",
        f"- 编码者：{'、'.join(order)}",
        f"- 全部编码者都编过的条目：{report['n_items']}"
        f"（未被全部编码者编过：{report['rows_not_coded_by_all'] or '无'}）",
        f"- 三者完全一致的条目：{report['unanimous_items']} / {report['n_items']}",
        f"- Fleiss κ：{report['fleiss_kappa']:.3f}"
        if report["fleiss_kappa"] is not None
        else f"- Fleiss κ：无定义（{report['fleiss_note']}）",
        "",
        "## 两两比对",
        "",
        "| 配对 | 条目 | 一致率 | Cohen's κ | 95% 区间 |",
        "|---|---:|---:|---:|---|",
    ]
    for p in report["pairwise"]:
        ci = (f"{p['kappa_ci95'][0]:.3f}–{p['kappa_ci95'][1]:.3f}"
              + ("（正态近似上界越界，已截断至 1）" if p["kappa_ci95_truncated"] else "")
              if p["kappa_ci95"] else "—")
        k = f"{p['cohens_kappa']:.3f}" if p["cohens_kappa"] is not None else "无定义"
        lines.append(f"| {p['pair']} | {p['n']} | {p['observed_agreement']:.3f} | {k} | {ci} |")
    lines += ["", "## 各编码者的类别分布", "",
              "| 编码者 | " + " | ".join(cats) + " |", "|---|" + "---|" * len(cats)]
    for lab in order:
        lines.append(f"| {lab} | " + " | ".join(str(report["distributions"][lab][c])
                                                for c in cats) + " |")
    lines += ["", "## 逐条判定", "",
              "| # | " + " | ".join(order) + " | 一致 |",
              "|---:|" + "---|" * (len(order) + 1)]
    for row in report["item_table"]:
        mark = "是" if row["unanimous"] else "**否**"
        lines.append(f"| {row['row']} | "
                     + " | ".join(row["verdicts"][lab] for lab in order)
                     + f" | {mark} |")
    lines.append("")
    return "\n".join(lines)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--sheet")
    ap.add_argument("--ai")
    ap.add_argument("--coder", action="append", default=[], metavar="标签=路径",
                    help="多编码者模式；给两份以上，报两两 κ 与 Fleiss κ")
    ap.add_argument("--out")
    ap.add_argument("--categories", default=",".join(DEFAULT_CATEGORIES),
                    help="判据的类别集合；v2 的五类传 A,B,C,D,E")
    args = ap.parse_args()

    categories = tuple(c.strip().upper() for c in args.categories.split(",") if c.strip())

    if args.coder:
        return multi(args, categories)

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
            if v and v not in categories:
                invalid.append({"row": r, "side": side, "verdict": v})

    rows = sorted(set(human) & set(ai), key=int)
    pairs, disagreements = [], []
    confusion = {h: {a: 0 for a in categories} for h in categories}
    for r in rows:
        h, a = human[r]["verdict"], ai[r]["verdict"]
        if h not in categories or a not in categories:
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

    k, po, pe, note = kappa(pairs, categories)
    # n 小的时候点估计会被当成结论读。SE = sqrt(po(1-po) / (n (1-pe)^2))，
    # 正态近似；n=15 时区间很宽，这正是必须印出来的信息。
    if k is None:
        se = ci = None
    else:
        se = math.sqrt(po * (1 - po) / (len(pairs) * (1 - pe) ** 2))
        ci = [k - 1.96 * se, k + 1.96 * se]
    report = {
        "sheet": args.sheet,
        "ai": args.ai,
        "categories": list(categories),
        "sheet_sha256": hashlib.sha256(Path(args.sheet).read_bytes()).hexdigest(),
        "ai_sha256": hashlib.sha256(Path(args.ai).read_bytes()).hexdigest(),
        "ai_blinding": ai_raw.get("blinding", "未声明"),
        "n_rows": len(human),
        "n_compared": len(pairs),
        "observed_agreement": po if pairs else None,
        "expected_agreement": pe,
        "cohens_kappa": k,
        "kappa_se": se,
        "kappa_ci95": ci,
        "kappa_note": note,
        "missing_human": [r for r in sorted(human, key=int) if human[r]["verdict"] not in categories],
        "missing_ai": [r for r in sorted(ai, key=int) if ai[r]["verdict"] not in categories],
        # codebook 要求逐条写理由：备注为空就是没按判据填，必须单列而不是让一致率把它盖住
        "human_rows_without_note": [r for r in sorted(human, key=int)
                                    if human[r]["verdict"] in categories and not human[r]["note"].strip()],
        "rows_only_in_sheet": sorted(set(human) - set(ai), key=int),
        "rows_only_in_ai": sorted(set(ai) - set(human), key=int),
        "confusion": confusion,
        "human_distribution": {c: sum(1 for h, _a in pairs if h == c) for c in categories},
        "ai_distribution": {c: sum(1 for _h, a in pairs if a == c) for c in categories},
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
