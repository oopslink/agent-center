#!/usr/bin/env python3
"""从 A 篇证据台账抽取盲编码样本，生成不含 AI 判断的编码表。

只抽"可按四类判据判定"的片段：证据级别为原生记录或参与者报告，
且在某篇初稿的证据附录里能解析到 ULID 与原文路径。

输出的 sheet.md 只给定位信息（片段编号、原生 ID、UTC、原文路径）与空白判定列；
台账的"断裂类别"和附录的"用途"描述都不进表，否则就不是盲编码。

用法：
  sample.py --ledger <台账> --appendix <附录|可重复> --sheet <输出> --manifest <输出>
            [--n 25] [--seed 20261104]
"""

import argparse
import json
import random
import re
from pathlib import Path

ELIGIBLE_LEVELS = ("原生记录", "参与者报告")
ULID = re.compile(r"\b01[0-9A-Z]{24}\b")
LINK = re.compile(r"\]\(([^)\s]+)\)")

CATEGORIES = ("责任外置", "合法升级或等待", "技术故障", "无法判定")


# 编号本身含 ASCII 连字符（E01-01、N01-E01），所以范围分隔符优先认全角破折号，
# 只在 token 里没有全角破折号时才退回用连字符切。
WIDE_DASH = "[–—~]"
ASCII_DASH = "-"
TRAILING_RUN = re.compile(r"^(.*?)(\d+|[a-z]+)$")


def _split_run(token):
    m = TRAILING_RUN.match(token)
    return (m.group(1), m.group(2)) if m else (None, None)


def _series(start, end):
    """在 start 与 end 之间产出序列，保持 start 的位宽。"""
    if start.isdigit() and end.isdigit():
        width = len(start)
        lo, hi = int(start), int(end)
        if hi < lo:
            return None
        return [str(i).zfill(width) for i in range(lo, hi + 1)]
    if start.isalpha() and end.isalpha() and len(start) == len(end) == 1:
        if ord(end) < ord(start):
            return None
        return [chr(c) for c in range(ord(start), ord(end) + 1)]
    return None


def expand_codes(cell):
    """把台账原生 ID 单元格展开成逐条编号。

    支持 "A05.1–A05.9"、"B01a–B01f"、"E01-01—E01-04"、"N01-E01—E06"、
    "E04-14—18"、"M05—M07"；展不开的 token 原样保留。
    """
    out = []
    for token in re.split(r"[、，,\s]+", cell.strip()):
        token = token.strip().strip("`")
        if not token:
            continue
        parts = re.split(WIDE_DASH, token)
        if len(parts) != 2:
            parts = token.split(ASCII_DASH)
        expanded = None
        if len(parts) == 2 and all(parts):
            left, right = parts
            prefix, start = _split_run(left)
            if prefix is not None:
                end = None
                if right.startswith(prefix):
                    end = right[len(prefix):]
                else:
                    _rp, rrun = _split_run(right)
                    if rrun is not None and len(rrun) <= len(right):
                        end = rrun
                if end:
                    series = _series(start, end)
                    if series:
                        expanded = [prefix + s for s in series]
        for code in expanded or [token]:
            if code not in out:
                out.append(code)
    return out


def _table_rows(text):
    """逐行产出表格单元格列表，并带上该行所属表的表头。"""
    header = None
    for line in text.splitlines():
        if not line.strip().startswith("|"):
            continue
        cells = [c.strip() for c in line.strip().strip("|").split("|")]
        if all(set(c) <= {"-", ":"} for c in cells if c):
            continue
        if any("原片段编号" in c or "编号" == c or "用途" in c for c in cells) and header != cells:
            header = cells
            continue
        if header is None:
            continue
        yield header, cells


def eligible_codes(ledger_path):
    text = Path(ledger_path).read_text(encoding="utf-8")
    codes = []
    for header, cells in _table_rows(text):
        idx = {name: i for i, name in enumerate(header)}
        ci = next((i for n, i in idx.items() if "原生 ID" in n), None)
        li = next((i for n, i in idx.items() if "证据级别" in n), None)
        if ci is None or li is None or len(cells) <= max(ci, li):
            continue
        level = cells[li]
        if not any(lv in level for lv in ELIGIBLE_LEVELS):
            continue
        for code in expand_codes(cells[ci]):
            if code not in codes:
                codes.append(code)
    return codes


def appendix_index(paths):
    index = {}
    for path in paths:
        text = Path(path).read_text(encoding="utf-8")
        for header, cells in _table_rows(text):
            idx = {name: i for i, name in enumerate(header)}
            ei = next((i for n, i in idx.items() if n.strip() == "编号"), None)
            if ei is None or len(cells) <= ei:
                continue
            code = cells[ei].strip("`")
            row_text = " | ".join(cells)
            ulid = ULID.search(row_text)
            links = [l for l in LINK.findall(row_text) if not l.startswith("http")]
            utc = re.search(r"\d{4}-\d{2}-\d{2}T[\d:.]+Z", row_text)
            if not ulid or not links:
                continue
            index.setdefault(code, {
                "code": code,
                "native_id": ulid.group(0),
                "utc": utc.group(0) if utc else "",
                "source": links[0],
            })
    return index


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--ledger", required=True)
    ap.add_argument("--appendix", action="append", required=True)
    ap.add_argument("--sheet", required=True)
    ap.add_argument("--manifest", required=True)
    ap.add_argument("--n", type=int, default=25)
    ap.add_argument("--seed", type=int, default=20261104)
    args = ap.parse_args()

    index = appendix_index(args.appendix)
    pool = [index[c] for c in eligible_codes(args.ledger) if c in index]
    rng = random.Random(args.seed)
    picked = rng.sample(pool, min(args.n, len(pool)))

    manifest = {
        "ledger": args.ledger,
        "appendix": args.appendix,
        "seed": args.seed,
        "requested_n": args.n,
        "pool_size": len(pool),
        "categories": list(CATEGORIES),
        "sample": picked,
    }
    Path(args.manifest).write_text(
        json.dumps(manifest, ensure_ascii=False, indent=1), encoding="utf-8"
    )

    lines = [
        "# 盲编码表",
        "",
        f"抽样：seed={args.seed}，请求 {args.n} 条，可判定池 {len(pool)} 条，实际 {len(picked)} 条。",
        "",
        "填写方式：打开「原文路径」读原始记录（必要时读相邻记录以获得上下文），",
        "在「判定」列填 codebook.md 定义的四类之一，理由写在「备注」列。",
        "判据定义见 [codebook.md](codebook.md)。本表不含任何既有编码结果。",
        "",
        "| # | 片段编号 | 原生 ID | UTC | 原文路径 | 判定 | 备注 |",
        "|---:|---|---|---|---|---|---|",
    ]
    for i, row in enumerate(picked, start=1):
        lines.append(
            f"| {i} | {row['code']} | `{row['native_id']}` | {row['utc']} | "
            f"{row['source']} |  |  |"
        )
    lines.append("")
    Path(args.sheet).write_text("\n".join(lines), encoding="utf-8")
    print(f"pool={len(pool)} sampled={len(picked)} sheet={args.sheet}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
