#!/usr/bin/env python3
"""从 .bib 按正文首次出现顺序生成参考文献表。

所有字段取自 .bib，不手写——避免在抄写中引入错误的年份、卷期或 DOI。
正文的编号与首次出现顺序不一致时报错退出，不静默改号。

用法：
  build_refs.py --paper <paper.md> --bib <refs.bib> [--write]

默认把生成结果打到 stdout；`--write` 则替换 paper.md 中"## 参考文献"一节的内容。
"""

import argparse
import re
import sys
from pathlib import Path

CITE = re.compile(r"\[(\d+)\]<!--ref:([^>]+)-->")
ENTRY = re.compile(r"@(\w+)\s*\{\s*([^,\s]+)\s*,", re.M)


def parse_bib(text):
    """返回 {key: (type, {field: value})}。按花括号配平切分条目体。"""
    out = {}
    for m in ENTRY.finditer(text):
        etype, key = m.group(1).lower(), m.group(2)
        i = text.index("{", m.start())
        depth, j = 0, i
        while j < len(text):
            if text[j] == "{":
                depth += 1
            elif text[j] == "}":
                depth -= 1
                if depth == 0:
                    break
            j += 1
        body = text[i + 1:j]
        fields = {}
        for fm in re.finditer(r"(\w+)\s*=\s*\{", body):
            name = fm.group(1).lower()
            k = fm.end() - 1
            d, p = 0, k
            while p < len(body):
                if body[p] == "{":
                    d += 1
                elif body[p] == "}":
                    d -= 1
                    if d == 0:
                        break
                p += 1
            # BibTeX 用花括号保护大小写（如 {CSCW}），输出到正文前须去掉
            value = re.sub(r"\s+", " ", body[k + 1:p]).strip()
            fields[name] = value.replace("{", "").replace("}", "")
        out[key] = (etype, fields)
    return out


def format_entry(num, etype, f):
    authors = f.get("author", "").replace(" and ", ", ")
    title = f.get("title", "")
    year = f.get("year", "")
    bits = [f"[{num}] {authors}, “{title},”"]

    venue = f.get("journal") or f.get("booktitle")
    if venue:
        seg = f"*{venue}*"
        if f.get("volume"):
            seg += f", vol. {f['volume']}"
        if f.get("number"):
            seg += f", no. {f['number']}"
        if f.get("pages"):
            seg += f", pp. {f['pages'].replace('--', '–')}"
        bits.append(seg + ",")
    elif f.get("eprint"):
        prefix = f.get("archiveprefix", "arXiv")
        seg = f"{prefix}:{f['eprint']}"
        if f.get("primaryclass"):
            seg += f" [{f['primaryclass']}]"
        bits.append(seg + ",")
    elif f.get("publisher"):
        bits.append(f"{f['publisher']},")

    if year:
        bits.append(f"{year}.")
    if f.get("doi"):
        bits.append(f"DOI: [{f['doi']}](https://doi.org/{f['doi']}).")
    if f.get("eprint") and not venue:
        bits.append("（预印本，未经同行评审）")
    return " ".join(bits)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--paper", required=True)
    ap.add_argument("--bib", required=True)
    ap.add_argument("--write", action="store_true")
    ap.add_argument(
        "--renumber",
        action="store_true",
        help="按首次出现顺序重写正文的引用编号，然后退出；插入新引用后用它代替手工重排",
    )
    args = ap.parse_args()

    paper_path = Path(args.paper)
    text = paper_path.read_text(encoding="utf-8")
    bib = parse_bib(Path(args.bib).read_text(encoding="utf-8"))

    if args.renumber:
        assigned = {}
        for _num, key in CITE.findall(text):
            assigned.setdefault(key, len(assigned) + 1)
        new = CITE.sub(lambda m: f"[{assigned[m.group(2)]}]<!--ref:{m.group(2)}-->", text)
        paper_path.write_text(new, encoding="utf-8")
        print(f"重排 {len(assigned)} 个引用编号")
        return 0

    order, seen = [], {}
    problems = []
    for num, key in CITE.findall(text):
        if key not in seen:
            seen[key] = len(order) + 1
            order.append(key)
        if int(num) != seen[key]:
            problems.append(
                f"编号不一致：{key} 首次出现应为 [{seen[key]}]，正文写的是 [{num}]"
            )
    missing = [k for k in order if k not in bib]
    for k in missing:
        problems.append(f"正文引用的 key 不在 .bib 中：{k}")

    if problems:
        for p in dict.fromkeys(problems):
            print(p, file=sys.stderr)
        return 1

    lines = [format_entry(i, *bib[k]) for i, k in enumerate(order, start=1)]
    block = "\n\n".join(lines)

    if args.write:
        pat = re.compile(r"(## 参考文献\n)(.*?)(?=\n## |\Z)", re.S)
        if not pat.search(text):
            print("找不到「## 参考文献」一节", file=sys.stderr)
            return 1
        paper_path.write_text(pat.sub(lambda m: m.group(1) + "\n" + block + "\n", text),
                              encoding="utf-8")
        print(f"写入 {len(order)} 条参考文献到 {args.paper}")
    else:
        print(block)
    return 0


if __name__ == "__main__":
    sys.exit(main())
