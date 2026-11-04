# 初稿违规基线（Task 1 产出）

本文件记录三个检查脚本在**合稿开始前**对五篇初稿的扫描结果，以及设计稿 §8 修订清单逐条的归属任务。
基线不是待办列表的全部——它是可执行的失败起点：后续任务的验收标准是让相应检查转绿。

- 脚本：`consolidation/checks/{recompute,claim_strength,crossrefs}.py`
- 自测：`python3 -m unittest discover -s consolidation/checks/tests -t consolidation/checks/tests`（17 项）
- 扫描日期：2026-11-04
- 扫描对象：`p1-logical-agent-identity`、`p2-workflow-responsibility`、`p3-role-shared-context`、`p4-dag-evolution`、`p5-memory-applicability` 的 `paper.md`

## 1 数字复算（recompute.py）

命令：`recompute.py --paper <paper> --data-root mechanisms-020`

| 检查 | 从数据算得 | 初稿状态 | 归属 |
|---|---|---|---|
| `p2_controller_sum` | 52 | ok（P2 正文一致） | — |
| `p2_worker_sum` | 39 | ok | — |
| `p2_total_sum` | 91 | ok | — |
| `p2_both_delivered_n` | 11 | ok | — |
| `p2_both_delivered_median` | 7 | ok | — |
| `p2_both_delivered_range` | [4, 8] | ok | — |
| `p2_format_errors_semantics` | 7 行为负值，列是**差值** | **semantics_mismatch**：P2 §4.8 称"共 7 次格式错误"，把差值列读成了发生次数 | T6 |
| `p4_transitions_total` | 169 | ok（P4 正文一致） | — |
| `p4_topology_changed` / `unchanged` | 166 / 3 | ok | — |
| `p4_parent_retained` | 149 | ok | — |
| `p4_became_completed_changed` | 101 | ok | — |
| `p4_became_completed_unchanged` | 1 | ok | — |
| `rt017_episodes` | 54 | ok | — |
| `rt017_reported_cost` | **6.2051478** | **cited_not_found**：`runtime-experiments-017/report.md` 记 6.31371 美元／696 次 CLI 尝试，而同目录 `run-status.json` 记 6.2051478 美元／694 次 `model_attempts` | T12 |
| `p1_census_closure` | 三个分区等式全部成立（927+311+983=2221；1930+597=2527；927+311=1238） | ok（P1 正文自洽） | — |

**新发现（不在设计稿 §8 清单里）：** `rt017_reported_cost` 的两处记载互不一致。设计稿 §9 与实施计划的成本锚引用的 6.31 美元来自 `report.md`，与 `run-status.json` 不符。两者差 0.11 美元，不影响 200 美元上限的结论，但 B 篇附录描述前批次时必须选定一个来源并说明另一个的差异。

## 2 主张强度（claim_strength.py）

命令：`claim_strength.py --paper <paper> --banned consolidation/checks/banned-terms.txt --sections abstract,conclusion,body`

禁用词表：净效应、优于、收益、发生率、学习曲线、显著改善。摘要／结论命中为 blocker，正文命中为 warning。

| 论文 | blocker | warning | blocker 位置 |
|---|---:|---:|---|
| P1 | 4 | 12 | 摘要"优于"、摘要"收益"、结论"发生率"、结论"优于" |
| P2 | 0 | 6 | — |
| P3 | 1 | 7 | 结论"收益" |
| P4 | 1 | 7 | 结论"收益" |
| P5 | 2 | 4 | 摘要"收益"、结论"优于" |

合计 8 个 blocker、36 个 warning。归属：A 篇取材部分 → T5／T6／T7（成文时即须为 0 blocker）；B 篇 → T12。

## 3 交叉引用与证据不重叠（crossrefs.py）

| 检查 | 结果 | 归属 |
|---|---|---|
| 五篇的本地链接 | 全部存在（0 个失效） | — |
| 内部章节指向 | P2／P3／P4／P5 无悬空；**P1 报 §11 悬空** | 已知假阳性 |
| P1 §11 | 证据表 A14 的"§1–8、§11–12"指向被引设计文献的章节，不是本稿小节 | 不修；A 篇改用"该文献 §11–12"的写法规避 |
| **P1 ∩ P2 共用原生记录 ID** | **5 条**：`01KZDJ41E4XMR6RSRDW2342DCE`、`01KZDKN7K7XRNZ0DPE7G9V0GQA`、`01KZDKS2M645VTZJ9GR8SW0MDJ`（I05 案例）、`01M0QM59RA181QMKVGFFYGHDR9`（Access）、`01M1EH0A4N0RMSPQ2C1ZJHCHX1`（Spark） | T12 的 `--disjoint` 必须为空 |

P1 共引 33 条原生记录 ID，P2 共 41 条。这组交集是设计稿所述 salami 问题的直接证据。

## 4 设计稿 §8 修订清单的逐条归属

| # | 位置 | 问题 | 可否脚本检查 | 归属 |
|---:|---|---|---|---|
| 1 | P5 §2.2 第 49 行 | "适正确采用象"（应为"适用对象"） | 否（人工） | T6 |
| 2 | P5 §4.2 第 175 行 | "不能用用于" | 否（人工） | T6 |
| 3 | P4 摘要 vs §4.6 | "3 次拓扑不变中也有 1 次出现这一状态变化"与"三次全部存在状态变化"指代冲突 | 部分（`p4_became_completed_unchanged`=1 可锚定正确说法） | T6 |
| 4 | P1 表 11 vs §4.9 | 表给"合格候选／验收"（7／6），正文给"分派／验收"（8／8、6／8），表中的 7 不在正文出现 | 否（人工） | T6 |
| 5 | P1 §3.7、表 11 | 等待历史对照是事后设计，与主实验并列同一张表 | 否（人工） | T6 |
| 6 | P1 图 2、P2 图 2、P4 图 2 | 微秒级墙钟间隔（29.639493 秒等） | 是（正则扫 `\d+\.\d{5,}秒`） | T6 |
| 7 | `mechanisms-020/p2-pairs.csv` | `format_errors` 实为差值，列名误导 | 是（`p2_format_errors_semantics`） | T6 |
| 8 | P2 §4.8–4.9 | 唯一负例同时用于角色数与视图两个因素的比较 | 否（人工） | T12 |
| 9 | P2 §4.9 | 停摆由研究者设定的 6 次启动预算造成，被叙述为流程性质 | 否（人工） | T12 |
| 10 | P3 摘要 | 2/8 由材料选择器决定，被写成 256 次响应的实证结果 | 否（人工） | T6 |
| 11 | P3 §3.5 vs §3.6 | §3.5 称"没有无显示名对照"，§3.6 已补该条件 | 否（人工） | T6 |
| 12 | P3 §4.6 | 单人"上瘾"自述出现在摘要与结论 | 是（禁用词表可加"上瘾"用于摘要／结论区） | T7 |
| 13 | 五篇 §3.5 | 同一段方法学免责声明逐字出现在五篇 | 是（跨篇重复段落检测） | T5 |
| 14 | 五篇作者行 | AI 列为共同作者 | 是（grep 作者行） | T7 |
| 15 | 五篇参考文献 | 7–10 篇，去重后合计不足 | 是（`--bib` 计数） | T2 |

第 6、13 两项的脚本检查尚未实现——它们不在 T1 的接口声明内。T6 与 T5 执行时各自用一次性 grep 核验即可，不扩充 `checks/` 的接口。

## 5 本基线不覆盖的内容

- 图件是否与新编号对应（T6 复用既有图件时人工核对）。
- 论证质量、文献定位是否充分（T14 的 `paper-audit` 负责）。
- 人工子集判定的一致率（T4／T8）。
