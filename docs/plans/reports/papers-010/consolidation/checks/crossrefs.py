#!/usr/bin/env python3
"""交叉引用、本地链接、台账覆盖与两篇证据不重叠的校验。

四种调用形态：
  crossrefs.py --paper <path>                     内部章节指向与本地链接
  crossrefs.py --bib <path> [--bib <path>] [--paper <path>]
  crossrefs.py --paper <path> --ledger <path>     台账与骨架双向覆盖
  crossrefs.py --disjoint <paperA> <paperB>       按原生记录 ID 求交集并断言为空
"""

import argparse
import json
import re
import sys
from pathlib import Path

from _common import sections

SECTION_REF = re.compile(r"§\s*(\d+(?:\.\d+)*)")
LINK = re.compile(r"\]\(([^)\s]+)\)")
BIB_ENTRY = re.compile(r"^@(\w+)\s*\{\s*([^,\s]+)\s*,", re.MULTILINE)
RECORD_PATTERNS = (
    re.compile(r"\b01[0-9A-Z]{24}\b"),
    re.compile(r"\b(?:generation|task|plan)-[0-9a-f]{8}\b"),
)


def record_ids(text):
    found = set()
    for pat in RECORD_PATTERNS:
        found.update(pat.findall(text))
    return found


def check_paper(path, report):
    text = Path(path).read_text(encoding="utf-8")
    present = set(sections(text))
    referenced = set(SECTION_REF.findall(text))
    report["dangling_sections"] = sorted(referenced - present)

    missing = []
    base = Path(path).resolve().parent
    for raw in LINK.findall(text):
        if raw.startswith(("http://", "https://", "mailto:", "#")):
            continue
        target = raw.split("#")[0]
        if not target:
            continue
        resolved = Path(target) if target.startswith("/") else base / target
        if not resolved.exists():
            missing.append(target)
    report["missing_links"] = sorted(set(missing))
    return text


def check_ledger(paper_text, ledger_path, report):
    leaf = {num for num, (depth, _t) in sections(paper_text).items() if depth >= 3}
    ledger_text = Path(ledger_path).read_text(encoding="utf-8")

    # 台账含多张表，各表列数可能不同：遇到带"小节"的表头就重置目标列下标，
    # 只认表头之后、列数够长的数据行。
    rows = [l for l in ledger_text.splitlines() if l.strip().startswith("|")]
    target_idx = None
    covered = set()
    for row in rows:
        cells = [c.strip() for c in row.strip().strip("|").split("|")]
        if any("小节" in c for c in cells):
            target_idx = next(i for i, c in enumerate(cells) if "小节" in c)
            continue
        if all(set(c) <= {"-", ":"} for c in cells if c):
            continue
        if target_idx is None or len(cells) <= target_idx:
            continue
        for num in re.findall(r"\d+(?:\.\d+)*", cells[target_idx]):
            covered.add(num)

    report["sections_without_ledger_source"] = sorted(leaf - covered)
    report["ledger_sections_missing_in_paper"] = sorted(covered - leaf)


def check_bibs(bib_paths, paper_path, report):
    unparsable = []
    uncited = []
    paper_text = Path(paper_path).read_text(encoding="utf-8") if paper_path else None
    for bib in bib_paths:
        raw = Path(bib).read_text(encoding="utf-8")
        keys = [k for _t, k in BIB_ENTRY.findall(raw)]
        if raw.strip() and not keys:
            unparsable.append(bib)
        if paper_text is not None:
            for key in keys:
                if key not in paper_text:
                    uncited.append(key)
    report["unparsable_bibs"] = sorted(set(unparsable))
    report["uncited_bib_keys"] = sorted(set(uncited))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--paper")
    ap.add_argument("--ledger")
    ap.add_argument("--bib", action="append", default=[])
    ap.add_argument("--disjoint", nargs=2, metavar=("PAPER_A", "PAPER_B"))
    args = ap.parse_args()

    report = {}
    paper_text = None

    if args.disjoint:
        a, b = args.disjoint
        ids_a = record_ids(Path(a).read_text(encoding="utf-8"))
        ids_b = record_ids(Path(b).read_text(encoding="utf-8"))
        report["papers"] = [a, b]
        report["record_ids"] = {a: len(ids_a), b: len(ids_b)}
        report["shared_record_ids"] = sorted(ids_a & ids_b)
    else:
        if args.paper:
            report["paper"] = args.paper
            paper_text = check_paper(args.paper, report)
        if args.ledger:
            if paper_text is None:
                ap.error("--ledger 需要同时给 --paper")
            check_ledger(paper_text, args.ledger, report)
        if args.bib:
            check_bibs(args.bib, args.paper, report)
        if not args.paper and not args.bib:
            ap.error("需要 --paper、--bib 或 --disjoint 之一")

    print(json.dumps(report, ensure_ascii=False, indent=1))
    problem_keys = (
        "dangling_sections",
        "missing_links",
        "sections_without_ledger_source",
        "ledger_sections_missing_in_paper",
        "unparsable_bibs",
        "uncited_bib_keys",
        "shared_record_ids",
    )
    return 1 if any(report.get(k) for k in problem_keys) else 0


if __name__ == "__main__":
    sys.exit(main())
