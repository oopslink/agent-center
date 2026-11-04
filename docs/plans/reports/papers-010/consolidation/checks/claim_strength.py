#!/usr/bin/env python3
"""按小节统计禁用措辞。摘要／结论命中记 blocker（退出码 1），正文命中记 warning。

用法：
  claim_strength.py --paper <path> --banned <file> --sections abstract,conclusion,body
"""

import argparse
import json
import sys
from pathlib import Path

from _common import iter_lines

BLOCKER_SECTIONS = {"abstract", "conclusion"}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--paper", required=True)
    ap.add_argument("--banned", required=True)
    ap.add_argument("--sections", default="abstract,conclusion,body")
    args = ap.parse_args()

    enabled = {s.strip() for s in args.sections.split(",") if s.strip()}
    terms = [
        t.strip()
        for t in Path(args.banned).read_text(encoding="utf-8").splitlines()
        if t.strip() and not t.startswith("#")
    ]
    text = Path(args.paper).read_text(encoding="utf-8")

    hits = []
    for lineno, (line, tag, _is_table, _is_caption) in enumerate(iter_lines(text), start=1):
        if tag not in enabled:
            continue
        for term in terms:
            if term in line:
                hits.append(
                    {
                        "term": term,
                        "section": tag,
                        "level": "blocker" if tag in BLOCKER_SECTIONS else "warning",
                        "line": lineno,
                        "excerpt": line.strip()[:120],
                    }
                )

    blockers = [h for h in hits if h["level"] == "blocker"]
    report = {
        "paper": args.paper,
        "sections_scanned": sorted(enabled),
        "terms": terms,
        "hits": hits,
        "blockers": len(blockers),
        "warnings": len(hits) - len(blockers),
    }
    print(json.dumps(report, ensure_ascii=False, indent=1))
    return 1 if blockers else 0


if __name__ == "__main__":
    sys.exit(main())
