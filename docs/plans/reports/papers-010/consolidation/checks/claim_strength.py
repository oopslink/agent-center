#!/usr/bin/env python3
"""按小节统计禁用措辞。摘要／结论命中记 blocker（退出码 1），正文命中记 warning。

用法：
  claim_strength.py --paper <path> --banned <file> --sections abstract,conclusion,body
"""

import argparse
import json
import re
import sys
from pathlib import Path

from _common import iter_lines_numbered

BLOCKER_SECTIONS = {"abstract", "conclusion"}
# 行内 <!--allow-banned:词1,词2--> 把该行指定词的命中降级为 allowed。
# 用于故意的否定式表述（例如声明本文不给发生率），使例外显式且可审。
ALLOW = re.compile(r"<!--\s*allow-banned:([^>]*?)-->")
# 按句切分：Markdown 的一个段落通常是一整行，若按行豁免，一个标记会放过整段。
SENTENCE = re.compile(r"[^。！？]*(?:[。！？]+(?:<!--[^>]*-->)?|$)")


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
    for lineno, (line, tag, _is_table, _is_caption) in iter_lines_numbered(text):
        if tag not in enabled:
            continue
        for sentence in SENTENCE.findall(line):
            if not sentence.strip():
                continue
            allowed = set()
            for m in ALLOW.finditer(sentence):
                allowed.update(t.strip() for t in m.group(1).split(",") if t.strip())
            bare = ALLOW.sub("", sentence)
            for term in terms:
                if term not in bare:
                    continue
                if term in allowed:
                    level = "allowed"
                elif tag in BLOCKER_SECTIONS:
                    level = "blocker"
                else:
                    level = "warning"
                hits.append(
                    {
                        "term": term,
                        "section": tag,
                        "level": level,
                        "line": lineno,
                        "excerpt": sentence.strip()[:120],
                    }
                )

    blockers = [h for h in hits if h["level"] == "blocker"]
    report = {
        "paper": args.paper,
        "sections_scanned": sorted(enabled),
        "terms": terms,
        "hits": hits,
        "blockers": len(blockers),
        "warnings": sum(1 for h in hits if h["level"] == "warning"),
        "allowed": sum(1 for h in hits if h["level"] == "allowed"),
    }
    print(json.dumps(report, ensure_ascii=False, indent=1))
    return 1 if blockers else 0


if __name__ == "__main__":
    sys.exit(main())
