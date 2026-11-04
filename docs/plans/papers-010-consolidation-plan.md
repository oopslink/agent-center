# papers-010 合稿实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 `docs/plans/reports/papers-010/` 的五篇初稿合并为两篇中文定稿——A 篇现场研究、B 篇受控实验——使每句主张的强度与其证据等级一致。

**Architecture:** 先建脚本护栏（数字复算、主张强度禁用词、链接、跨篇重复），让"初稿违规清单"成为可执行的失败基线；A 篇按绑定六元组的六类断裂重组既有冻结证据，不重新抽取；B 篇改造 `runtime-experiments-017` 的运行器以支持凭据可达性因子，试跑定难度后冻结再跑正式批次；最后用 `paper-audit` 过审稿式终检。

**Tech Stack:** Markdown 正文（LaTeX 转换属后续阶段）、Python 3（复算与评分脚本，标准库优先）、`uv`、`pandoc`（导 PDF 供 paper-audit）、既有 CLI 调用方式（Sonnet 5.5 / Haiku 5.5，low effort）、skills：`paper-audit`、`bib-search-citation`、`literature-search-arxiv`。

**Spec:** `docs/plans/papers-010-consolidation-design.md`

## Global Constraints

- 实验正式批次报告价硬上限 **200 美元**；超限停下报告，不自行扩量。
- 试跑上限 **30 次调用**，不进结果。
- 两模型 **Sonnet 5.5 / Haiku 5.5 保持交叉**，low effort；每格 **n≥4**；开发集与留出集齐备。
- 裁剪优先级（仅逼近上限时）：减留出集任务家族数 → 每格降到 4 → 收缩到单模型（保留 Haiku）。**不删因子水平。**
- 禁止后验重试、禁止"追到全对"的追加批次、禁止迭代到出现效应。
- A 篇使用既有冻结快照，**不重新抽取**；范围句写"一个时间窗"，不写"整个部署"。
- 禁用外推性发生率与比率；**完整记录集上的计数允许**。
- A 篇正文、摘要、标题禁用措辞：净效应、优于、收益、发生率、学习曲线。
- 两篇证据不重叠：同一条原始记录不得在两篇中各自作为独立证据。
- 作者仅 oopslink；AI 参与写入"AI 使用声明"；不得将 AI 列为作者。
- 伦理声明不得出现"已获批准"或"经判定豁免"。
- 本阶段只产出中文稿，不产出英文版，不投稿，不声称"可投稿"。
- B 篇每个终点必须脚本可核验。
- 人工子集判定：随机抽 **20–30** 个案例片段，盲编码，报告一致率，表述为"作者对子集作了人工判定"，不写"独立复核"。

## Review Focus

1. **跨文件数字失同步**——同一数字在摘要、正文、表、图题四处出现，改一处漏三处。T1 的复算脚本须同时扫四类位置，不只扫正文。
2. **悬空引用**——删除 P3 重合度分析（原 §4.5／§4.7）与降级命名实验后，残留的"见 §4.5"类指向。T3 的检查须验证每个内部指向都解析到现存小节。
3. **证据重叠复发**——B 篇附录描述旧批次时重新引入 A 篇的部署记录。T12 须按记录 ID 求两篇证据集的交集，断言为空。
4. **禁用措辞回流**——降格后的句子在后续润色中被改回效应比较语气。T1 的禁用词检查须在每个成文任务末尾重跑，而非只跑一次。
5. **评分规则漂移**——B 篇分析脚本的评分映射与冻结的 design-lock 不一致。T11 须把评分映射哈希写入 lock，T12 断言一致。

---

## Task 1: 脚本护栏与初稿违规基线

**Files:**
- Create: `docs/plans/reports/papers-010/consolidation/checks/recompute.py`
- Create: `docs/plans/reports/papers-010/consolidation/checks/claim_strength.py`
- Create: `docs/plans/reports/papers-010/consolidation/checks/crossrefs.py`
- Create: `docs/plans/reports/papers-010/consolidation/checks/banned-terms.txt`
- Create: `docs/plans/reports/papers-010/consolidation/baseline-violations.md`

**Interfaces:**
- Consumes: 五篇 `p*/paper.md`；`mechanisms-020/*.csv`、`git-replay.json`、`reanalysis.json`；`runtime-experiments-017/run-status.json`
- Produces 三个脚本的完整命令行接口（后续任务只用这里声明的参数）：
  - `recompute.py --paper <path> --data-root <dir> [--also-scan tables,captions,abstract]` —— 退出码 0=全部数字可复算，1=有不符；stdout 为 JSON 报告。`--also-scan` 把表格、图题与摘要一并纳入扫描，默认只扫正文。
  - `claim_strength.py --paper <path> --banned <file> --sections abstract,conclusion,body` —— 摘要／结论命中记 blocker（退出码 1），正文命中记 warning（退出码 0 并输出告警）。
  - `crossrefs.py` 四种调用形态：`--paper <path>` 校验内部章节指向与本地链接；`--bib <path>`（可重复）校验 .bib 条目可解析且被正文引用；`--paper <path> --ledger <path>` 校验台账与骨架双向覆盖；`--disjoint <paperA> <paperB>` 按原生记录 ID 求两篇证据集交集并断言为空。

- [ ] **Step 1: 写出会失败的复算检查**

`recompute.py` 必须断言这些已知关系（取自初稿，全部可从底层数据复算）：

```python
# p2-pairs.csv：12 对，控制端增量合计 52、工作端 39、总计 91
# both_delivered 子集 n=11，总增量中位数 7，范围 4–8
# p4-transitions.csv：169 次转移；166 拓扑改变中 101 次有共有任务转完成；3 次不变中 1 次
# P1 普查闭合：927+311+983 == 2221；1930+597 == 2527
# rt017 run-status.json：54 episodes 的报告价，与正文引用一致
```

- [ ] **Step 2: 跑复算，确认初稿有不符**

Run: `python3 consolidation/checks/recompute.py --paper p2-workflow-responsibility/paper.md --data-root mechanisms-020`
Expected: 退出码 1，报告中至少包含 `format_errors` 列语义不符一项（CSV 中该列为差值，7 行为 −1，正文称"共 7 次格式错误"）

- [ ] **Step 3: 写禁用措辞检查与禁用词表**

`banned-terms.txt` 收录：净效应、优于、收益、发生率、学习曲线、显著改善。`claim_strength.py` 按小节分别统计，摘要与结论区命中记为 blocker，正文命中记为 warning。

- [ ] **Step 4: 跑禁用措辞与交叉引用检查，记录基线**

Run: `python3 consolidation/checks/claim_strength.py --paper p1-logical-agent-identity/paper.md --banned consolidation/checks/banned-terms.txt --sections abstract,conclusion,body`
Expected: 非零命中，写入 `baseline-violations.md`

- [ ] **Step 5: 把 §8 修订清单的 15 行逐条落为可检查项**

每行对应 `baseline-violations.md` 中一个条目，标注归属任务编号。P5 的两处坏字（"适正确采用象"、"不能用用于"）与 P4 摘要／§4.6 指代冲突须在此登记。

- [ ] **Step 6: Commit**

```bash
git add docs/plans/reports/papers-010/consolidation/
git commit -m "chore(papers-010): add consolidation guard scripts and violation baseline"
```

---

## Task 2: 文献扩充与统一 .bib

**Files:**
- Create: `docs/plans/reports/papers-010/consolidation/references-A.bib`
- Create: `docs/plans/reports/papers-010/consolidation/references-B.bib`
- Create: `docs/plans/reports/papers-010/consolidation/literature-coverage.md`
- Read: 五篇 `p*/references.bib`

**Interfaces:**
- Consumes: T1 的护栏脚本（用于最终核验引用存在性）
- Produces: 两个合并去重的 .bib；`literature-coverage.md` 给出主题×文献矩阵与缺口说明

- [ ] **Step 1: 合并现有五篇 .bib 并去重，记录初始规模**

初稿引用数为 P1 8、P2 10、P3 8、P4 9、P5 7，去重后合计应显著小于 42。

- [ ] **Step 2: 按主题检索补充文献**

用 `literature-search-arxiv` skill（遵守其 3 秒速率限制与许可通知要求）检索四个 A 篇必须覆盖的主题：多智能体 LLM 失效分类与协调、计划修复与复用、组织／团队记忆与知识治理、agent harness 与模型—接口边界；两个 B 篇必须覆盖的主题：多智能体工作流拓扑与成本、证据／凭据在分布式协作中的认可。每个主题记录检索式与命中。

- [ ] **Step 3: 核验每条新增文献的出版记录与撤稿状态**

沿用 `mechanisms-020/citation-check.json` 的做法（Crossref + OpenAlex，检查 `is_retracted`）。无法核验的不入库。

- [ ] **Step 4: 用 bib-search-citation 核对声明支持关系**

对 A 篇与 B 篇各自的核心主张，确认 .bib 中存在可支持该主张定位的条目；不存在的主张改写或标为本研究新增。

- [ ] **Step 5: 核验并提交**

Run: `python3 consolidation/checks/crossrefs.py --bib consolidation/references-A.bib --bib consolidation/references-B.bib`
Expected: 退出码 0；`literature-coverage.md` 中每个主题至少有一条已核验文献

```bash
git add docs/plans/reports/papers-010/consolidation/
git commit -m "docs(papers-010): merge and extend bibliographies for A and B papers"
```

---

## Task 3: A 篇证据台账与骨架

**Files:**
- Create: `docs/plans/reports/papers-010/paper-a-field-study/evidence-ledger.md`
- Create: `docs/plans/reports/papers-010/paper-a-field-study/paper.md`（仅骨架）
- Create: `docs/plans/reports/papers-010/paper-a-field-study/exclusions.md`

**Interfaces:**
- Consumes: T1 的 `crossrefs.py`；design §7 的内容归属表
- Produces: `evidence-ledger.md` 的表格列为 `断裂类别 | 原片段编号 | 原生 ID | 证据级别 | 进 A 篇的小节 | 排除理由`，后续成文任务按它取材

- [ ] **Step 1: 建台账，逐条登记六类断裂的取材**

六类：责任、凭据、信息、成果与合同、适用范围，加一类"剩余义务"（P2 的局部完成与交付义务并存）。每条标注证据级别：原生记录／参与者报告／源码静态阅读／AI 分析。

- [ ] **Step 2: 登记删除与降级决定**

`exclusions.md` 记录并给出理由：P3 原 §4.5／§4.7 角色读取重合度分析（四口径 0.074／0.063／0.142／0.033，删层后跨 0，结论自我抵消）；P3 命名实验降入 B 篇附录；P2 §4.6 与 P4 §4.5 重复的结构统计合并；边 Jaccard 与父节点保留率移入附录。

- [ ] **Step 3: 写骨架，每节只有标题与一句"本节要回答什么"**

骨架须包含：标题、摘要位（留空）、§1 引言、§2 框架与相关工作、§3 方法（含时间窗范围句与人工判定说明位）、§4 六类断裂、§5 机制与竞争解释、§6 证据边界、§7 结论、声明、证据附录。

- [ ] **Step 4: 校验台账与骨架自洽**

Run: `python3 consolidation/checks/crossrefs.py --paper paper-a-field-study/paper.md --ledger paper-a-field-study/evidence-ledger.md`
Expected: 退出码 0——台账中每个"进 A 篇的小节"都解析到骨架中存在的小节，且骨架每节至少有一条台账来源

- [ ] **Step 5: Commit**

```bash
git add docs/plans/reports/papers-010/paper-a-field-study/
git commit -m "docs(papers-010): add A-paper evidence ledger, exclusions and skeleton"
```

---

## Task 4: 盲编码包（交付负责人）

**Files:**
- Create: `docs/plans/reports/papers-010/consolidation/blind-coding/sample.json`
- Create: `docs/plans/reports/papers-010/consolidation/blind-coding/codebook.md`
- Create: `docs/plans/reports/papers-010/consolidation/blind-coding/sheet.md`
- Create: `docs/plans/reports/papers-010/consolidation/blind-coding/sample.py`

**Interfaces:**
- Consumes: T3 的 `evidence-ledger.md`
- Produces: `sheet.md` 为负责人填写用；`sample.py --n 25 --seed <int>` 从台账抽样并生成不含任何结论的编码表

- [ ] **Step 1: 写判据定义**

`codebook.md` 的四个互斥类别：责任外置（可行动时明确以角色边界把仍属自己的目标交给外部主体）／合法升级或等待（无权限或前提未满足）／技术故障（线程、工作区、配置、接口缺陷）／无法判定。每类给正反各一个示例，示例不取自样本。

- [ ] **Step 2: 写抽样脚本并固定随机种子**

- [ ] **Step 3: 生成编码表，断言表中不含结论泄漏**

Run: `python3 consolidation/blind-coding/sample.py --n 25 --seed 20261104 && python3 consolidation/checks/claim_strength.py --paper consolidation/blind-coding/sheet.md --banned consolidation/blind-coding/leakage-terms.txt`
Expected: 退出码 0——表中不出现 AI 既有编码、不出现"责任外置"等类别词以外的判断性措辞

- [ ] **Step 4: Commit 并交付**

```bash
git add docs/plans/reports/papers-010/consolidation/blind-coding/
git commit -m "docs(papers-010): add blind coding package for author subset judgement"
```

提交后通知负责人填表；**T8 依赖其返回，但 T5–T7 不阻塞**。

---

## Task 5: A 篇 §1–§3（引言、框架、方法）

**Files:**
- Modify: `docs/plans/reports/papers-010/paper-a-field-study/paper.md`

**Interfaces:**
- Consumes: T2 的 `references-A.bib`；T3 的台账；T4 的 codebook（方法节引用其判据）
- Produces: 定稿的 §1–§3，后续结果节引用其中定义的六元组记号

- [ ] **Step 1: 写框架节，把绑定六元组定为全文记号**

沿用 P2 §3.1 的 `〈目标，动作，主体，对象，合同，授权，前提〉`，并把六类断裂定义为该结构在边界处的具体失配。相关工作用 T2 的文献，须明确本研究的位置是"单部署的描述与机制"，不声称填补算法空白。

- [ ] **Step 2: 写方法节，一次性写清五篇原先重复的方法学声明**

内容须含：单部署、目的性选例、事后框架、冻结时间窗、证据级别定义、排除规则（技术故障不承担设计负效应）、人工子集判定与一致率的报告方式、AI 参与范围。原先逐字出现在五篇的那段免责声明在此只出现一次。

- [ ] **Step 3: 跑护栏**

Run: `python3 consolidation/checks/claim_strength.py --paper paper-a-field-study/paper.md --banned consolidation/checks/banned-terms.txt --sections abstract,conclusion,body && python3 consolidation/checks/crossrefs.py --paper paper-a-field-study/paper.md`
Expected: 两者退出码均为 0

- [ ] **Step 4: Commit**

```bash
git commit -am "docs(papers-010): write A-paper introduction, framework and methods"
```

---

## Task 6: A 篇 §4（六类断裂的结果）

**Files:**
- Modify: `docs/plans/reports/papers-010/paper-a-field-study/paper.md`
- Create: `docs/plans/reports/papers-010/paper-a-field-study/figures/`（复用既有图件，按新编号）

**Interfaces:**
- Consumes: T3 台账；T1 的 `recompute.py`
- Produces: 六个结果小节，每节的每个数字都能被 `recompute.py` 复算

- [ ] **Step 1: 按台账逐类写结果，数字一律从底层数据取**

六类各一节。涉及既有图件时按新编号复用，不重绘。统计压缩按 `exclusions.md` 执行。

- [ ] **Step 2: 修掉基线清单中归属本任务的缺陷**

至少包括：P4 摘要与原 §4.6 的指代冲突（按 `p4-transitions.csv` 的 3/3/1 改写）；P1 原表 11 与正文的指标不一致（统一为同一指标）；微秒级墙钟间隔改为秒或毫秒；P5 两处坏字。

- [ ] **Step 3: 复算全部数字**

Run: `python3 consolidation/checks/recompute.py --paper paper-a-field-study/paper.md --data-root mechanisms-020 --also-scan tables,captions,abstract`
Expected: 退出码 0

- [ ] **Step 4: 跑禁用措辞与交叉引用**

Expected: 两者退出码均为 0

- [ ] **Step 5: Commit**

```bash
git add -A docs/plans/reports/papers-010/paper-a-field-study/
git commit -m "docs(papers-010): write A-paper results across six binding failures"
```

---

## Task 7: A 篇 §5–§7、声明与证据附录

**Files:**
- Modify: `docs/plans/reports/papers-010/paper-a-field-study/paper.md`

**Interfaces:**
- Consumes: T5 的记号；T6 的结果
- Produces: 完整 A 篇（摘要除外的全部正文）；声明段落供 B 篇复用同一措辞

- [ ] **Step 1: 写机制与竞争解释节，每个机制列支持、反证与仍缺判别**

沿用初稿已有的竞争解释表结构，但删去所有效应比较语气。

- [ ] **Step 2: 写证据边界节**

须显式包含：不估计发生率、不估计持续时间、不估计相对架构风险；人工判定仅覆盖子集；跨篇同源材料不构成独立重复。

- [ ] **Step 3: 写声明四段**

作者仅 oopslink；AI 使用声明按 arXiv 政策披露用途；伦理按自我声明模板（非人类被试、既有工程记录、未采集第三方个人数据、发表前脱敏），不写获批或豁免；数据可用性指向 T13 的脱敏复现包。

- [ ] **Step 4: 写摘要**

摘要只允许出现 §4 中已复算的数字与 §7 的结论，不得出现比较级。

- [ ] **Step 5: 全量护栏**

Run: 三个检查脚本依次运行
Expected: 全部退出码 0

- [ ] **Step 6: Commit**

```bash
git commit -am "docs(papers-010): complete A-paper discussion, limits, statements and abstract"
```

---

## Task 8: 并入人工子集判定与一致率

**Files:**
- Create: `docs/plans/reports/papers-010/consolidation/blind-coding/agreement.py`
- Create: `docs/plans/reports/papers-010/consolidation/blind-coding/agreement.json`
- Modify: `docs/plans/reports/papers-010/paper-a-field-study/paper.md`

**Interfaces:**
- Consumes: 负责人填好的 `sheet.md`；T3 台账中的 AI 编码
- Produces: `agreement.py --sheet <path> --ledger <path>` 输出逐条比对与一致率

- [ ] **Step 1: 写一致率脚本，逐条输出分歧**

报告总一致率与按类别的分歧明细。不做 kappa，除非样本量与边缘分布支持。

- [ ] **Step 2: 跑脚本**

Run: `python3 consolidation/blind-coding/agreement.py --sheet consolidation/blind-coding/sheet.md --ledger paper-a-field-study/evidence-ledger.md`
Expected: 退出码 0，产出 `agreement.json`

- [ ] **Step 3: 把一致率与分歧写入方法节与证据边界节**

分歧条目须逐条说明处置：改判、保留并标注争议、或移出正文。

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "docs(papers-010): fold author subset judgement and agreement into A-paper"
```

---

## Task 9: B 篇仪器改造与桥接重跑

**Files:**
- Create: `docs/plans/reports/papers-010/experiments-023/runner/receipts.py`
- Modify: `runtime-experiments-017` 运行器的传输与合同层（按其实际布局，复制到 `experiments-023/runner/` 后改，不改原批次）
- Create: `docs/plans/reports/papers-010/experiments-023/runner/tests/`
- Create: `docs/plans/reports/papers-010/experiments-023/bridge-rerun.json`

**Interfaces:**
- Consumes: `runtime-experiments-017/` 的运行器、`design-lock.json`、`protocol.md`
- Produces: 三个凭据可达性水平的实现——`transport=report_only`（现状）／`transport=shared_receipts`（可查询回执对象）／`transport=issuer_bound`（明确规定签发者）；每个回执对象含 `receipt_id`、`artifact_version`、`contract_id`、`result`、`issuer`

- [ ] **Step 1: 写仪器测试，断言三个水平的可见性差异**

```python
# report_only: reviewer 的回执对象不出现在 controller 的工具结果中
# shared_receipts: controller 能按 receipt_id 查到 reviewer 签发的回执，且字段完整
# issuer_bound: 合同声明仅接受指定签发者的回执；非指定签发者的有效回执被判不合格
# 无效回执（artifact_version 与当前产物不符）在三个水平下均须被判不合格
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd experiments-023/runner && uv run pytest tests -v`
Expected: FAIL——`receipts.py` 尚未实现

- [ ] **Step 3: 实现回执对象与三个传输水平**

- [ ] **Step 4: 跑测试确认通过**

Expected: PASS，含无效回执反例

- [ ] **Step 5: 桥接重跑一个 rt017 旧条件**

选 rt017 中已完成的一个两角色条件，在 `report_only` 水平下重跑，断言产物终验结果与决策数落在原记录的可比范围内；差异写入 `bridge-rerun.json`。这是旧批次仍可作为"前批次诊断"引用的前提。

- [ ] **Step 6: Commit**

```bash
git add -A docs/plans/reports/papers-010/experiments-023/
git commit -m "feat(papers-010): add receipt-visibility transport levels and bridge rerun"
```

---

## Task 10: 任务家族构造、试跑与 kill point

**Files:**
- Create: `docs/plans/reports/papers-010/experiments-023/tasks/dev/`、`tasks/holdout/`
- Create: `docs/plans/reports/papers-010/experiments-023/pilot/results.json`
- Create: `docs/plans/reports/papers-010/experiments-023/pilot/decision.md`

**Interfaces:**
- Consumes: T9 的运行器
- Produces: 开发集与留出集的任务家族；试跑通过率分布；kill point 判定结论

- [ ] **Step 1: 构造有真实失败空间的任务家族**

每个任务须满足至少一条：规格内含可发现的歧义、需跨多文件、或审查者必须抓到一个真 bug 才能通过。开发集与留出集按家族划分，不按重复划分。提示只给目标、合同、权限、工具语义，**不预告"失败由你修复"一类待验证原则**。

- [ ] **Step 2: 先做无模型的自洽性检查**

断言每个任务的合法动作集合非空、存在至少一条通过路径、终点可脚本判定。

- [ ] **Step 3: 试跑，上限 30 次调用**

Run: 运行器的 pilot 模式，记录实际报告价
Expected: 通过率既不在 1.0 也不在 0.0；记录分布

- [ ] **Step 4: 写 kill point 判定**

`decision.md` 给出结论之一：继续正式批次／调整任务难度后再试跑一次（总调用仍受 30 次上限约束）／B 篇降级为方法与仪器报告。判定依据与实际通过率一并记录。

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat(papers-010): add experiment task families, pilot results and kill-point decision"
```

---

## Task 11: 冻结设计并跑正式批次

**Files:**
- Create: `docs/plans/reports/papers-010/experiments-023/design-lock.json`
- Create: `docs/plans/reports/papers-010/experiments-023/results/`
- Create: `docs/plans/reports/papers-010/experiments-023/run-status.json`

**Interfaces:**
- Consumes: T10 的任务家族与判定
- Produces: 两个实验的全部原始响应、工具事件、产物版本与报告价

- [ ] **Step 1: 冻结 design-lock，把评分映射哈希写进去**

lock 须含：因子与水平、每格 n、任务家族划分、主终点定义、评分映射的文件哈希、排除规则、预算上限。**在任何正式调用之前冻结。**

- [ ] **Step 2: 跑实验一（协调成本）**

因子：角色数／交接数 × 凭据可达性。主终点：独立验收通过率、控制端与工作端决策数。

- [ ] **Step 3: 跑实验二（凭据认可）**

由构造诱发——控制者持有在自身工具历史中可证明看不到的跨会话产物。主终点：重复索证轮数、预算内闭合。worker 启动预算放宽到不构成约束，不作为交叉因子。无效回执反例单独计分。

- [ ] **Step 4: 核对预算与完整性**

Run: 汇总 `run-status.json` 的报告价与 episode 数
Expected: 报告价 ≤200 美元；每格实际 n≥4；无后验重试记录

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat(papers-010): freeze design lock and run B-paper main batches"
```

---

## Task 12: B 篇分析与成文

**Files:**
- Create: `docs/plans/reports/papers-010/experiments-023/analyse.py`
- Create: `docs/plans/reports/papers-010/paper-b-controlled/paper.md`
- Create: `docs/plans/reports/papers-010/paper-b-controlled/appendix-prior-batches.md`

**Interfaces:**
- Consumes: T11 的结果与 lock；T2 的 `references-B.bib`；T7 的声明措辞
- Produces: 完整 B 篇

- [ ] **Step 1: 写分析脚本，断言评分映射与 lock 一致**

脚本启动时校验 `design-lock.json` 中记录的评分映射哈希，不一致则退出码非 0。

- [ ] **Step 2: 产出结果表与图，全部条件与不显著结果一并呈现**

- [ ] **Step 3: 写正文**

质量终点上的零结果作为有界零结果报告并给描述性区间，**不做等效性声明**。旧批次进 `appendix-prior-batches.md`，定位为"前批次及其天花板诊断"，并引用 T9 的桥接重跑说明仪器已变更。

- [ ] **Step 4: 断言两篇证据不重叠**

Run: `python3 consolidation/checks/crossrefs.py --disjoint paper-a-field-study/paper.md paper-b-controlled/paper.md`
Expected: 退出码 0——按原生记录 ID 求交集为空

- [ ] **Step 5: 全量护栏**

Expected: 三个检查脚本退出码均为 0

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "docs(papers-010): analyse B-paper batches and write controlled-experiment paper"
```

---

## Task 13: 脱敏复现包

**Files:**
- Create: `docs/plans/reports/papers-010/consolidation/repro/README.md`
- Create: `docs/plans/reports/papers-010/consolidation/repro/sanitize.py`

**Interfaces:**
- Consumes: A 篇与 B 篇的数据依赖
- Produces: 只含派生物的复现包

- [ ] **Step 1: 写脱敏脚本**

公开：动作日志派生表、读取回执审计脱敏版、169 次父子转移表、Git 对象哈希与核验脚本、B 篇实验的全部输入／原始响应／评分脚本。不公开原始协作对话。

- [ ] **Step 2: 断言脱敏结果不含可识别信息与本机绝对路径**

Run: `python3 consolidation/repro/sanitize.py --check`
Expected: 退出码 0——无用户名、无 `/Users/` 路径、无内部主机名

- [ ] **Step 3: 断言 B 篇完全可复算**

Run: 在脱敏包内重跑 `analyse.py`
Expected: 输出与 T12 的结果表一致

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "chore(papers-010): add sanitized reproduction package"
```

---

## Task 14: paper-audit 终检与修复

**Files:**
- Create: `docs/plans/reports/papers-010/consolidation/audit/`
- Modify: 两篇 `paper.md`

**Interfaces:**
- Consumes: 两篇定稿
- Produces: 审稿报告与修复记录

- [ ] **Step 1: 导出 PDF**

Run: `pandoc paper-a-field-study/paper.md -o consolidation/audit/paper-a.pdf`（B 篇同）

- [ ] **Step 2: 跑 paper-audit 的 deep-review 模式**

Run: 以 `--mode deep-review --lang zh` 对两篇分别运行
Expected: 产出 `review_report.md` 与 `revision_suggestions.md`

- [ ] **Step 3: 按 blocker／major／minor 分级处置**

blocker 必修；major 逐条给出修或不修的理由；minor 可记录不修。**不得为消除审稿意见而放大主张。**

- [ ] **Step 4: 修复后重跑全量护栏与 re-audit**

Expected: 三个检查脚本退出码 0；re-audit 显示无机械回归

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "docs(papers-010): address paper-audit findings on both papers"
```

---

## 交付物与本阶段终点

- `paper-a-field-study/paper.md`、`paper-b-controlled/paper.md`：两篇中文定稿
- `consolidation/`：护栏脚本、台账、盲编码包、一致率、脱敏复现包、审稿记录
- `experiments-023/`：改造后的运行器、冻结 lock、全部原始结果

**不包含**：英文版、arXiv endorsement、投稿。本阶段结束时不投稿，也不声称"可投稿"。
