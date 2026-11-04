#!/usr/bin/env python3
"""生成只含派生物的脱敏复现包。

公开的是派生表、核验脚本与实验数据；**原始协作对话不在其中**。因此案例可按
标识追溯到本地档案，但不可还原到具体个人。

脱敏规则（逐条可核）：
- 本机 home 绝对路径 → `<ARCHIVE>`，顺带去掉路径里的用户名
- 独立出现的用户名 → `<AUTHOR>`
- 内部主机名（*.local）→ `<HOST>`
- 二进制文件不复制，只在 MANIFEST 里登记为跳过

用法：
  sanitize.py --source <目录> --out <目录>     生成脱敏副本与 MANIFEST.json
  sanitize.py --source <目录> --out <目录> --check   只检查 out 是否仍有泄漏
"""

import argparse
import hashlib
import json
import re
import sys
from pathlib import Path

USERNAME = "oopslink"
RULES = [
    (re.compile(r"/Users/[A-Za-z0-9_.-]+"), "<ARCHIVE>"),
    (re.compile(r"/home/[A-Za-z0-9_.-]+"), "<ARCHIVE>"),
    (re.compile(r"\b[A-Za-z0-9_-]+\.local\b"), "<HOST>"),
    (re.compile(re.escape(USERNAME)), "<AUTHOR>"),
]
LEAKS = [
    (re.compile(r"/Users/"), "本机 home 路径"),
    (re.compile(r"/home/"), "本机 home 路径"),
    (re.compile(re.escape(USERNAME)), "用户名"),
    (re.compile(r"\b[A-Za-z0-9_-]+\.local\b"), "内部主机名"),
]
TEXT_SUFFIXES = {".json", ".csv", ".md", ".txt", ".py", ".jsonl", ".diff", ".bib", ".svg"}


def is_text(path):
    if path.suffix.lower() not in TEXT_SUFFIXES:
        return False
    try:
        path.read_text(encoding="utf-8")
        return True
    except (UnicodeDecodeError, ValueError):
        return False


def scrub(text):
    for pattern, replacement in RULES:
        text = pattern.sub(replacement, text)
    return text


def check(out):
    problems = []
    for p in sorted(Path(out).rglob("*")):
        if not p.is_file() or p.name == "MANIFEST.json":
            continue
        try:
            text = p.read_text(encoding="utf-8")
        except (UnicodeDecodeError, ValueError):
            continue
        for pattern, label in LEAKS:
            m = pattern.search(text)
            if m:
                problems.append(f"{p.relative_to(out)}: 残留{label} {m.group(0)!r}")
    return problems


def build(source, out):
    source, out = Path(source), Path(out)
    out.mkdir(parents=True, exist_ok=True)
    files, skipped = [], []
    for p in sorted(source.rglob("*")):
        if not p.is_file() or p.name == "MANIFEST.json":
            continue
        rel = p.relative_to(source).as_posix()
        if not is_text(p):
            skipped.append(rel)
            continue
        target = out / rel
        target.parent.mkdir(parents=True, exist_ok=True)
        text = scrub(p.read_text(encoding="utf-8"))
        target.write_text(text, encoding="utf-8")
        files.append({"path": rel,
                      "sha256": hashlib.sha256(text.encode("utf-8")).hexdigest(),
                      "bytes": len(text.encode("utf-8"))})
    manifest = {"source_note": "派生物的脱敏副本；原始协作对话不包含在内。",
                "rules": [r[1] for r in RULES],
                "files": files, "skipped_binary": skipped}
    (out / "MANIFEST.json").write_text(
        json.dumps(manifest, ensure_ascii=False, indent=1) + "\n", encoding="utf-8")
    return manifest


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--source", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--check", action="store_true")
    args = ap.parse_args()

    if args.check:
        problems = check(args.out)
        for p in problems:
            print(p, file=sys.stderr)
        print(f"检查 {args.out}：{'发现 %d 处泄漏' % len(problems) if problems else '无泄漏'}")
        return 1 if problems else 0

    manifest = build(args.source, args.out)
    print(f"复制 {len(manifest['files'])} 个文本文件，跳过 {len(manifest['skipped_binary'])} 个二进制文件")
    problems = check(args.out)
    for p in problems:
        print(p, file=sys.stderr)
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
