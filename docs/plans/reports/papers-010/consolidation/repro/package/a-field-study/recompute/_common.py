"""三个检查脚本共用的 Markdown 切分与数字查找。"""

import re

HEADING = re.compile(r"^(#{1,6})\s+(.*)$")
TABLE_ROW = re.compile(r"^\s*\|")
CAPTION = re.compile(r"^\s*\*\*(图|表)")
ABSTRACT_WORDS = ("摘要", "abstract")
CONCLUSION_WORDS = ("结论", "conclusion")


def _section_tag(heading_text):
    if heading_text is None:
        return "body"
    low = heading_text.lower()
    if any(w in low for w in ABSTRACT_WORDS):
        return "abstract"
    if any(w in low for w in CONCLUSION_WORDS):
        return "conclusion"
    return "body"


def iter_lines(text):
    """产出 (行文本, 小节标签, 是否表格行, 是否图表题)。

    小节标签取自最近一个 h2 或更浅的标题；更深的标题不改变标签，
    以免 "### 4.1" 把它所在的结论节切走。
    """
    heading = None
    for line in text.splitlines():
        m = HEADING.match(line)
        if m:
            if len(m.group(1)) <= 2:
                heading = m.group(2)
            continue
        yield line, _section_tag(heading), bool(TABLE_ROW.match(line)), bool(CAPTION.match(line))


def scanned_text(text, enabled):
    """按启用的扫描类别拼出待扫描文本。enabled 是标签集合。"""
    out = []
    for line, tag, is_table, is_caption in iter_lines(text):
        if tag not in enabled:
            continue
        if is_table and "tables" not in enabled:
            continue
        if is_caption and "captions" not in enabled:
            continue
        out.append(line)
    return "\n".join(out)


def sections(text):
    """返回 {小节编号: 标题}，编号取自 "## 4 标题" / "### 4.1 标题"。"""
    found = {}
    for line in text.splitlines():
        m = HEADING.match(line)
        if not m:
            continue
        num = re.match(r"^(\d+(?:\.\d+)*)\s+(.*)$", m.group(2).strip())
        if num:
            found[num.group(1)] = (len(m.group(1)), num.group(2))
    return found


def number_candidates(value):
    """一个数值在正文里可能的写法。"""
    if isinstance(value, bool):
        return [str(value)]
    if isinstance(value, int):
        return [str(value), f"{value:,}"]
    out = []
    for nd in range(2, 8):
        s = f"{round(float(value), nd):g}"
        if s not in out:
            out.append(s)
    return out


def contains_number(haystack, value):
    for cand in number_candidates(value):
        esc = re.escape(cand)
        if re.search(r"(?<![0-9.,])" + esc + r"(?![0-9.])", haystack):
            return True
    return False
