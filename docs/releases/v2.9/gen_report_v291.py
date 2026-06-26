# -*- coding: utf-8 -*-
"""Generate the v2.9.1 acceptance report PDF with embedded UI screenshots (CJK)."""
import os
from reportlab.lib.pagesizes import A4
from reportlab.lib.units import mm
from reportlab.lib import colors
from reportlab.lib.styles import getSampleStyleSheet, ParagraphStyle
from reportlab.lib.enums import TA_LEFT
from reportlab.platypus import (
    SimpleDocTemplate, Paragraph, Spacer, Table, TableStyle, HRFlowable, Image, KeepTogether,
)
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.cidfonts import UnicodeCIDFont
from PIL import Image as PILImage

HERE = os.path.dirname(os.path.abspath(__file__))
SHOTS = os.path.join(HERE, "v2.9.1-screenshots")
OUT = os.path.join(HERE, "v2.9.1-acceptance-report.pdf")

pdfmetrics.registerFont(UnicodeCIDFont("STSong-Light"))
CJK = "STSong-Light"
styles = getSampleStyleSheet()
title = ParagraphStyle("t", parent=styles["Title"], fontName=CJK, fontSize=20, leading=26)
sub = ParagraphStyle("sub", parent=styles["Normal"], fontName=CJK, fontSize=10.5, leading=15, textColor=colors.HexColor("#555555"))
h2 = ParagraphStyle("h2", parent=styles["Heading2"], fontName=CJK, fontSize=13, leading=18, spaceBefore=12, spaceAfter=4, textColor=colors.HexColor("#1a3c6e"))
cap = ParagraphStyle("cap", parent=styles["Normal"], fontName=CJK, fontSize=9, leading=13)
body = ParagraphStyle("b", parent=styles["Normal"], fontName=CJK, fontSize=10, leading=15, alignment=TA_LEFT)
cell = ParagraphStyle("c", parent=styles["Normal"], fontName=CJK, fontSize=8.5, leading=12)
cellb = ParagraphStyle("cb", parent=cell, textColor=colors.HexColor("#0a7a2f"))

doc = SimpleDocTemplate(OUT, pagesize=A4, leftMargin=16*mm, rightMargin=16*mm, topMargin=14*mm, bottomMargin=14*mm,
                        title="agent-center v2.9.1 Acceptance Report (with screenshots)", author="AgentCenterPD")
S = []
def P(t, st=body): S.append(Paragraph(t, st))
def gap(h=6): S.append(Spacer(1, h))
CONTENT_W = A4[0] - 32*mm

def fig(fname, w_frac=1.0):
    path = os.path.join(SHOTS, fname)
    iw, ih = PILImage.open(path).size
    w = CONTENT_W * w_frac
    h = w * ih / iw
    return Image(path, width=w, height=h)

# ---- cover ----
P("agent-center v2.9.1 验收报告", title)
P("用户视角 · 真实例 · 真浏览器 · 关键步骤截图版", sub)
P("日期 2026-06-14 · 编制 PD(AgentCenterPD)· 验收 ref <b>v2.9.1 @ fa9cdcd</b>", sub)
S.append(HRFlowable(width="100%", thickness=1, color=colors.HexColor("#1a3c6e"), spaceBefore=6, spaceAfter=8))

P("0. 结论", h2)
P("<b>v2.9.1 验收通过</b> —— 本报告在 PD 亲手拉起的 <b>v2.9.1 真实例</b>(真 <font face='Courier'>bin/agent-center</font> + 内嵌 Web Console)上,用与 Web Console 同一套 <font face='Courier'>/api</font> 播种真实场景(注册→项目/频道/线程/任务/Plan/归档),再用真浏览器(Chromium 1440×900@2x)按<b>用户视角</b>走完关键流程,逐步截图。<b>全程 console error = 0。</b>建议:tag <font face='Courier'>v2.9.1</font> → promote <font face='Courier'>v2.9.1→main</font>(--no-ff)→ 部署。")

P("1. 验收方式 — 可复现", h2)
P("本次为<b>截图补强版</b>(回应 owner:首版纯文字、缺截图)。复现脚本:<font face='Courier'>tests/e2e/v2/capture-v291.mjs</font> —— 起真实例→<font face='Courier'>/api</font> 播种→Playwright 驱动 SPA 截图,输出到 <font face='Courier'>docs/release/evidence/v2.9.1-screenshots/</font>。Playwright e2e 是可选 dev 依赖(<font face='Courier'>make e2e-install</font>),不在 §0 合并门内;§0 门(go build/test + tests/integration + make lint + vitest)另行通过。")

# ---- summary table ----
P("2. 能力 → 截图 对照", h2)
rows = [
    ["#", "能力", "结果", "截图"],
    ["A", "Thread 消息串(派生/侧栏/列表)", "✅ GREEN", "A1 频道+回复数 chip+THREADS 面板 / A2 ThreadSidebar(root+2回复+回复框) / A3 线程列表"],
    ["B", "Task 状态机 7→5(ADR-0046)", "✅ GREEN", "D1 状态过滤恰为 open/running/completed/discarded/reopened(无 blocked/verified)"],
    ["C", "claimable + 内置指派池(ADR-0047)", "✅ GREEN", "C1 三段看板:Backlog(不可领)/ Assignment Pool(内置·常驻·可领)/ 结构化 Plan"],
    ["D", "看板可见性 / 归档隐藏", "✅ GREEN", "D1 Tasks 列表(org 号 T1–T7)/ G1 活动列表默认排除已归档"],
    ["E", "工具 / 门(list_tasks·eslint·no-raw-colors)", "✅(非 UI)", "非 UI 表面;由 §0 门 + §-1 + data/API 覆盖(见 §4)"],
    ["F", "恢复 / 运维(unblock_task·auto-redispatch)", "✅(非 UI)", "非 UI 表面;由 §0 + data/API class-guard 覆盖(见 §4)"],
    ["G", "频道归档", "✅ GREEN", "G1 「Archived / 已归档」组显示 old-incidents(ARCHIVED,只读)"],
    ["H", "Plan 详情 UX(三 tab + DAG + Task 号 + 连线编辑)", "✅ GREEN", "H1 三 tab 默认 Chat / H2 DAG(T5→T6→T7 派生状态+ +Dep 连线) / H3 Task list"],
    ["I", "both-mode 暗色(AA 不退)", "✅ GREEN", "I1 暗色频道/线程 / I2 暗色 Work Board"],
]
data = [[Paragraph(c, cellb if (j==2 and i>0) else cell) for j, c in enumerate(r)] for i, r in enumerate(rows)]
tbl = Table(data, colWidths=[7*mm, 46*mm, 20*mm, 105*mm], repeatRows=1)
tbl.setStyle(TableStyle([
    ("BACKGROUND", (0,0), (-1,0), colors.HexColor("#1a3c6e")),
    ("TEXTCOLOR", (0,0), (-1,0), colors.white),
    ("FONTNAME", (0,0), (-1,0), CJK), ("FONTSIZE", (0,0), (-1,0), 9),
    ("GRID", (0,0), (-1,-1), 0.4, colors.HexColor("#cccccc")),
    ("VALIGN", (0,0), (-1,-1), "TOP"),
    ("ROWBACKGROUNDS", (0,1), (-1,-1), [colors.white, colors.HexColor("#f4f7fb")]),
    ("TOPPADDING", (0,0), (-1,-1), 4), ("BOTTOMPADDING", (0,0), (-1,-1), 4),
    ("LEFTPADDING", (0,0), (-1,-1), 5), ("RIGHTPADDING", (0,0), (-1,-1), 5),
]))
S.append(tbl)

# ---- figures with captions ----
FIGS = [
 ("A1_channel_threads.png", "A · Thread — 频道 + 派生线程",
  "步骤:打开 #general 频道。期望:每条顶层消息显示线程按钮+回复数;右栏列出线程。",
  "实测:两条消息各显示回复数 chip(2 / 1);右栏 THREADS 列出 2 个线程(回复 2 / 1)。console=0。"),
 ("A2_thread_sidebar.png", "A · Thread — 侧栏(root + 回复 + 回复框)",
  "步骤:点消息的线程按钮。期望:弹出 ThreadSidebar 显示 root + 回复 + 回复框。",
  "实测:标题「Thread · 2 replies」;root + 2 条回复按时间序;底部回复框在。"),
 ("A3_thread_list.png", "A · Thread — 参与者栏线程列表",
  "步骤:看参与者栏 THREADS。期望:每线程显示发起人/预览/回复数。",
  "实测:alice · 预览 · 回复数(1 / 2)。", 0.55),
 ("D1_org_tasks.png", "B · Task 状态机 7→5(ADR-0046)",
  "步骤:打开 Tasks 页看状态过滤。期望:只剩 open/running/completed/discarded/reopened(删 blocked/verified)。",
  "实测:过滤条恰为这 5 态;任务带 org 号 T1–T7。归档项目的任务默认不在此(可见性)。"),
 ("C1_work_board.png", "C · claimable + 内置指派池(ADR-0047)— 三段看板",
  "步骤:打开项目 Work Board。期望:三段 = Backlog / Assignment Pool(内置) / 结构化 Plans。",
  "实测:三列齐;Backlog「unscheduled — not claimable」;Assignment Pool「Built-in · always running · claimable」;结构化「Release v2.9.1 DRAFT」3 卡。"),
 ("H1_plan_chat_tab.png", "H · Plan 详情 — 三 tab(默认 Chat)",
  "步骤:打开 Plan 详情。期望:Chat / DAG / Task list 三 tab,默认 Chat。",
  "实测:三 tab 在,默认落在 Chat(对话)。"),
 ("H2_plan_dag.png", "H · Plan 详情 — DAG + Task 号 + 连线编辑 + 派生状态",
  "步骤:切到 DAG tab。期望:DAG 显示 Task 号 + 派生节点状态 + 连线编辑。",
  "实测:START→T5→T6→T7→END;T5 READY / T6·T7 BLOCKED(派生 §9.2);每节点 +Dep 连线编辑;状态 legend;Compact 切换。"),
 ("H3_plan_tasks.png", "H · Plan 详情 — Task list tab",
  "步骤:切到 Task list tab。期望:列出 plan 内任务。",
  "实测:plan 内任务列表渲染。"),
 ("G1_channels_archived.png", "G · 频道归档",
  "步骤:Channels 页展开「Archived / 已归档」。期望:活动列表默认排除已归档;归档组显示已归档频道(只读)。",
  "实测:general ACTIVE 在活动列表(计数 1);old-incidents ARCHIVED 在归档组(单独分区)。"),
 ("I1_dark_channel.png", "I · both-mode 暗色 — 频道 / 线程",
  "步骤:切暗色重开 #general。期望:暗色渲染正常、无错位、可读。",
  "实测:暗主题全应用;线程 chip / THREADS 面板可读;console=0。"),
 ("I2_dark_work_board.png", "I · both-mode 暗色 — Work Board",
  "步骤:暗色看 Work Board。期望:三段看板暗色下可读。",
  "实测:三段在暗色下渲染、可读。"),
]
P("3. 关键步骤截图", h2)
for entry in FIGS:
    fname, ttl, step, actual = entry[0], entry[1], entry[2], entry[3]
    frac = entry[4] if len(entry) > 4 else 1.0
    block = [
        Paragraph(ttl, ParagraphStyle("ft", parent=h2, fontSize=11, spaceBefore=8, spaceAfter=2)),
        fig(fname, frac),
        Spacer(1, 2),
        Paragraph(step, cap),
        Paragraph("<font color='#0a7a2f'>" + actual + "</font>", cap),
        Spacer(1, 6),
    ]
    S.append(KeepTogether(block))

P("4. 诚实备注", h2)
P("· <b>截图 provenance</b>:全部在 PD 亲手起的 v2.9.1 真实例(<font face='Courier'>fa9cdcd</font>)+ 真 Chromium 抓,数据走与 Web Console 同一套 <font face='Courier'>/api</font>(用户视角真路径),非 mock。复现脚本随仓库(<font face='Courier'>tests/e2e/v2/capture-v291.mjs</font>)。<br/>"
  "· <b>看板卡片显示 Unassigned</b>:本次播种用 alice(人)做 assignee,其 ref 格式被 ADR-0033 拒(非阻塞);看板三段结构与 claimable 标识不受影响;claimable 谓词的真值由 data/API class-guard(baseline + 9 单条件翻转)覆盖。<br/>"
  "· <b>@agent 在线程内回复(真 LLM 唤醒)</b>:需 enrolled worker + 真 agent,本截图集未含;该链路(F4:线程内 @agent 回复落 thread 内而非顶层)由 Tester data/API class-guards + 之前 run-real 覆盖。<br/>"
  "· <b>E/F(工具 / 恢复)非 UI 表面</b>:list_tasks / eslint-gate / no-raw-colors / unblock_task / auto-redispatch 不在浏览器表面,故不截图;由 §0 全绿门 + PD §-1 + data/API class-guard 覆盖。<br/>"
  "· <b>sites 首页 showcase / roadmap</b> = 非阻塞 fast-follow(待截图)。")

S.append(HRFlowable(width="100%", thickness=0.6, color=colors.HexColor("#cccccc"), spaceBefore=10, spaceAfter=6))
P("结论:v2.9.1 关键能力在真实例 + 真浏览器下逐步截图坐实,console error=0,具备发布条件。建议 owner 拍板 tag + promote + 部署。",
  ParagraphStyle("end", parent=body, fontName=CJK, fontSize=10.5, textColor=colors.HexColor("#1a3c6e")))

doc.build(S)
print("PDF generated:", OUT)
