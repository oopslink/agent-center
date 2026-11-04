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


def iter_lines_numbered(text):
    """与 iter_lines 相同，但带上**原文件中的真实行号**。

    iter_lines 跳过标题行后再 enumerate，会让报出的行号随标题数累积偏移，
    使审计工具的锚点失效。
    """
    heading = None
    for lineno, line in enumerate(text.splitlines(), start=1):
        m = HEADING.match(line)
        if m:
            if len(m.group(1)) <= 2:
                heading = m.group(2)
            continue
        yield lineno, (line, _section_tag(heading), bool(TABLE_ROW.match(line)),
                       bool(CAPTION.match(line)))


def text_by_position(text, enabled):
    """按位置类别分别返回文本：abstract / conclusion / body / tables / captions。

    与 `scanned_text` 的区别是**不拼接**：跨位置一致性必须逐类比对，
    拼成一个字符串后"某处出现过"就会被当成通过。
    """
    out = {k: [] for k in ("abstract", "conclusion", "body", "tables", "captions")}
    for line, tag, is_table, is_caption in iter_lines(text):
        if is_table:
            if "tables" in enabled:
                out["tables"].append(line)
            continue
        if is_caption:
            if "captions" in enabled:
                out["captions"].append(line)
            continue
        if tag in enabled:
            out[tag].append(line)
    return {k: "\n".join(v) for k, v in out.items() if v}


def numbers_near(haystack, anchor):
    """取出锚点附近的数字。锚点是含一个捕获组的正则，组内为数字。"""
    found = []
    for m in re.finditer(anchor, haystack):
        raw = m.group(1).replace(",", "")
        try:
            found.append(int(raw))
        except ValueError:
            try:
                found.append(float(raw))
            except ValueError:
                continue
    return found


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
    # `:g` 默认只保留 6 位有效数字（6.2051478 会变成 6.20515），
    # 因此除了各档四舍五入的写法，还要给出去掉尾零的全精度写法。
    out = []
    for nd in range(2, 12):
        s = f"{round(float(value), nd):.{nd}f}".rstrip("0").rstrip(".")
        if s and s not in out:
            out.append(s)
    return out


def contains_number(haystack, value):
    for cand in number_candidates(value):
        esc = re.escape(cand)
        if re.search(r"(?<![0-9.,])" + esc + r"(?![0-9.])", haystack):
            return True
    return False
