# Agent Harness 生产研究：首轮数据审计与探索案例报告

研究日期：2026-11-01（America/Los_Angeles）。状态：阶段性报告 v0.1，非已完成的正式研究。

## 1. 本轮实际完成范围

完成生产 SQLite 一致性快照、BlobStore 独立副本、全量核心元数据审计、30 个探索候选的数据整理和 6 个 Plan 的探索复核。30 个候选涉及 776 个 Task、7,012 条 Message；材料提取不等于全部完成独立结果标注。初版抽样、发现的问题和修订版本均保留。

研究主问题仍是：哪些 harness 复杂性增加协调负担，哪些机制改善可靠交付？本轮已发现测量口径和实际机制方面的证据，尚不能估计复杂结构、DAG evolution 或命名身份的因果效应。

本轮由单一 AI 分析者执行，没有第二名人工标注者。P0 的快照和核心覆盖审计已执行，部署版本映射、历史原始 trace 定位和完整文献比较仍需补足；P1 为探索试点；P2 仅准备候选材料，P3 正式综合报告与 P4 实验尚未完成。

## 2. 数据保全与复现入口

完整本地工作区：[run-001](/Users/oopslink/works/research/agent-harness-study/run-001/README.md)。原始数据包含生产对话与敏感内容，未发布或上传。

| 工件 | 路径 |
|---|---|
| 快照清单 | [raw/manifest.json](/Users/oopslink/works/research/agent-harness-study/run-001/raw/manifest.json) |
| SQL 与参数 | [logs/queries.json](/Users/oopslink/works/research/agent-harness-study/run-001/logs/queries.json) |
| 数据覆盖 | [derived/coverage.json](/Users/oopslink/works/research/agent-harness-study/run-001/derived/coverage.json) |
| 统计结果 | [derived/aggregates.json](/Users/oopslink/works/research/agent-harness-study/run-001/derived/aggregates.json) |
| 样本索引 | [derived/screening-register.md](/Users/oopslink/works/research/agent-harness-study/run-001/derived/screening-register.md) |
| 方法调整 | [logs/method-decisions.md](/Users/oopslink/works/research/agent-harness-study/run-001/logs/method-decisions.md) |
| Git 交叉核验 | [derived/git-corroboration.json](/Users/oopslink/works/research/agent-harness-study/run-001/derived/git-corroboration.json) |

数据库快照大小 45,233,991,680 bytes，SHA256：`fbc9cca0e1147a6bed9feddaf71bc9b17651a76a5983b49de68ad199ac159838`。备份过程为 2026-11-02 06:39:48—06:41:08 UTC（当地 11 月 1 日晚）。使用源只读事务和 SQLite backup，覆盖 WAL 中的一致性视图；未修改生产数据。

首次备份因生产写入反复重启而中断，其日志和无效文件标为 interrupted，不参与分析。BlobStore 为另一时间点的目录副本，不宣称与 DB 原子一致；文件存在性和内容哈希检查独立记录。

复现时使用 Python 3.12，在工作区依次运行 `scripts/analyze.py`、`scripts/cases.py`、`scripts/pilot_notes.py`、`scripts/screen.py`、`scripts/validate.py`。`snapshot.py` 只用于新建快照，拒绝覆盖已有快照；运行其他脚本会重建对应派生产物。快照及脚本应一起保留。

### 验证结果

验证计划在 `logs/method-decisions.md` 中先行登记。V1：全库 `PRAGMA quick_check` 返回 `ok`，快照 SHA256 已保存。V2/V4：8个核心导出表逐表计数与快照相等，Plan关联Task合计核对通过。V3：30个探索候选唯一且存在，6个试点引用全部可解析；40个案例目录包含历次抽样及排除对象，不能当成40个正式样本。V5：文件引用存在性通过，声明大小58处差异和无历史哈希的限制已单列。文档链接和空白检查通过。详细结果见工作区 `logs/validation.json`、`targeted-validation.json` 和 `file-reference-validation.json`。

本轮无生产代码修改、无 commit；未运行与研究文档无关的 Go/Web 全量测试。

## 3. 数据审计结果

| 数据 | 数量 | 可用性 |
|---|---:|---|
| Task | 2,221 | 1,471 completed、556 discarded、186 failed、8 open |
| Plan | 108 | 96 done、8 discarded、3 running、1 paused |
| Plan generation | 205 / 32 个 Plan | 最早 2026-08-13；与更早计划不能直接比较 |
| Task action log | 2,527 | 涉及的时间及 Task 覆盖不完整 |
| Message | 26,961 | 可用于重建交接与结论，但自报不等于独立运行证据 |
| Domain Event | 108,478 | 大量是读状态、授权比较等事件，不能直接视为工作量 |
| Usage event | 15,086 | 能关联到 812 个现有 Task，约占 36.6% |
| AgentActivityEvent | 0 | 不能用此表恢复逐步行为 |
| Agent execution mirror | 0 | 不能用当前投影恢复历史执行 |

四个影响研究设计的重要问题：

1. **终态不是成功标签。** Plan done 既可能表示交付，也可能表示拒绝闭环或行政清理。Spark Host 案例还残留未完成的双平台验收；因此本轮不计算“96/108 成功率”。
2. **完成时间缺失具有时间差异。** 1,471 个 completed Task 中 959 个（65.2%）没有 completed_at；6 月缺 109/599，7 月缺 359/370，8 月缺 392/399，9 月缺 99/103。不能只分析有值样本并当作总体耗时。
3. **模型与成本不可简单比较。** Task.model 全为空，但部分 usage 有模型信息，可按明确关联恢复局部信息。GPT 系列记录的 cost_micros 全为 0；没有定价/采集依据时，这表示成本口径待核实，不表示零成本。
4. **测试与业务记录混存。** 初筛发现 6 个名字明确的 fixture/夹具 Plan、1 个 prod smoke Plan 和3个 Built-in 容器。暂排除这10个，保留98个候选；还需内容审查防止名称筛选漏检。8 个 discarded Plan 中6个是这些 fixture，不能把8个都视为业务失败。

另外，1,043 条 progress incident 全属于 migration_gap 或 progress_classification_unknown。它们反映数据/分类问题，不能作为真实停滞次数。

文件审计：1,329 条有效 file_reference 全部能找到副本，对应副本目录共883个文件。58条引用的声明大小与实际大小不同，其中40条声明为0；保留差异清单，不自动认定文件损坏。blob_metadata 为0行，没有原始内容哈希可对照。已对883个副本保存当前 SHA256 清单，只保证后续研究输入可核验，不能证明历史内容从未变化。

## 4. 六个探索案例

每例本地目录保留 `records.json`、`dossier.md` 和 `annotation.json`。以下是记录支持的发现，不是总体效应估计。

| 案例 | 观察 | 证据强度与限制 |
|---|---|---|
| plan-523d5d71 | 修改测试前置条件和职责后，新增环境交付与独立验收；后续部署仍失败 | generation-b9412e39 / 7d3a1d9f / 14febca0；支持局部恢复，人工纠正与改图共同发生 |
| plan-d7ec8ee4 | 148 个 Task、33 条 generation，G36/G37 记录重复的工具能力边界误解；最终废弃并重建短链 | generation-677d7df4 / d1b3a9e0；诊断来自运行参与者，需原始调用核验；不能用代次数推出因果 |
| plan-70ab192f | 69 个 Task、23 条 generation；真实缺陷导致 NO-GO，后续部署报告仍缺人工授权和真实设备；最后清理任务，Plan 为 done | generation-66cfaf4c / a2577996 与后续消息；体现必要门禁及外部依赖，不能全部归因 LLM |
| plan-2e483a10 | 14 个 Task，0 条 generation；独立验收与 Ship 消息对应可找到的 Git release commit | `dad68255fcc0ac3e82dfd6de7ea29a89e41f4e4f` 存在；支持代码交付证据，历史真机测试仍待原始记录核验 |
| plan-67822059 | Mermaid 并发渲染验收 REJECT 后新增修复与复验，随后集成；过期恢复消息被识别 | generation-384637ec；修复 commit 6855b0bd…、merge b0c40b7c…存在；支持演化后局部闭合，不分离代码修复与改图效应 |
| plan-33c168dc | 交付协议修复误判任务类别，被审查拦截；owner 随后否决方案并要求另开讨论 | 原始 commit 7fd64337…证实扫描标题/描述中的 verify/acceptance 等词；属于 harness 分类缺陷与主动方案撤回 |

最后一个案例的关键证据超过对话自报：保存的 `raw/contract-candidate.patch` 显示 `InferredDeliveryContract(title, description)` 拼接并扫描自由文本；CreateTask 和 runnable 检查使用此推断。审查消息 `01M0VVF3NEHW7F8T8SETJC1PBQ` 指出普通代码任务被误判的具体条件，owner 消息 `01M0W338J0NHP88M6NW6R75D2P` 明确要求废弃。这里可支持“错误分类规则有实际代码依据”，尚未重跑历史二进制验证运行后果。

首次试点抽中的 `plan-c75e8aae` 实际是 disposable fixture：消息明确其 blocked source 为 intentional，父测试已 PASS。该案作为排除证据保留，并由 plan-33c168dc 替换。选择过程没有删除或隐藏这次误抽。

### 扩展初筛发现

对探索候选的收口消息继续检查后，另外保存了5条候选机制记录（`derived/additional-closure-signals.json`），尚未完成其完整轨迹标注：

- `plan-bdccf57b` 的 `01KW4W85Q47GXEY5GKXZWK54M3` 指出模板将 Ship 指派给 integration-dev，后续改派 PD；职责混淆可能来自模板本身。
- `plan-236dadc8` 的 `01M1M25A7M2RFKGTZRGDXJ644R` 报告实际 main 已交付，自动 eager-push 却因分支命名判 non_delivery；应查证交付检测假阴性。
- `plan-becb5960` 的 `01M13AQ9HTJNMY5GYYG2KKGJF4` 明确说 done 表示“本轮 REJECT 已收口”。这是反对直接用 done 计算成功率的直接文本证据。
- `plan-d1680545` 的 `01M1WZMK71XWDDDK56XQPSM1N7` 说明原任务误派外部 Agent，同一 Issue 下重建任务已完成。仅看旧 Task 会漏掉业务结果。
- `plan-929d9aaf` 的 `01M1CTGRRZ7YMF8S8AJC7KFSXF` 是旧 Insight Plan 废弃后的新链最终验收报告，并列出持久附件，可继续交叉核验。

因此正式分析还需增加 **Issue/交付目标层的替代谱系**：旧 Task、新 Task、废弃 Plan、重建 Plan 可能属于同一次业务交付，不能当作彼此独立的样本。

已经读取重建 Insight Plan 的 `ac://files/01M1CTCG81R7B93DE68HV5C0ZK`：3989 bytes，当前 SHA256 `76d31f68f1c5a0feaded81887203976f76807d2b9aaaceb840afc2df7c4c2ea4`，文件包含 A–F 矩阵、候选/集成 SHA、执行命令与截图列表，最终记录为 PASS。这证明验收文件确实存在并包含上述内容；未据此声称所有测试已由本研究独立重跑。

## 5. 对五项原始判断的当前回答

| 判断 | 当前证据 | 下一步 |
|---|---|---|
| Agent 多层结构造成认知问题 | 存在职责、权限边界和重复恢复的候选片段；尚不能归因 supervisor/executor 层级 | 补实际可见输入、工具调用和执行身份，排除错误规则与不可用工具 |
| 工作流复杂导致卡住 | 有分类规则误伤、状态/通知冲突、重复前置条件等具体机制 | 区分必要拒绝、外部等待、主动取消、无产出循环，逐案核验 |
| DAG evolution 有用 | 有恢复局部交付的正例，也有33代后重建的反例 | 以“解决了哪项前置条件”为单位分析，检查人工介入和新增代码贡献 |
| 名字不利共享，角色有用 | 本轮没有可分离名字、角色、session 和记忆效应的比较 | 只做机制假设；后续实验分别控制名字、角色契约、context 范围 |
| 名字有情绪价值、无自动开发收益 | 对话中的协作语气不足以测量开发者情绪或证明自动开发无收益 | 需要开发者复核/访谈；自动交付效果需独立实验 |

首轮可提出的设计假设是：任务分类与交付合同应有可验证的显式依据；恢复应针对具体阻塞条件并记录新增证据；业务交付、执行状态和行政清理应分别观测。这些是研究推论，不构成已经批准的产品架构修改。

## 6. 文献和方法记录

已保存三个起始论文的摘要页面、MAST 与 harness 组件研究的全文 HTML、arXiv 查询返回和检索清单。Semantic Scholar 请求返回 HTTP 429，失败已记录。尚未完成多库系统筛选及全部全文阅读，因此不宣称完整综述或新颖性成立。

- [Measuring Agents in Production](https://arxiv.org/abs/2512.04123)：生产实践调查背景。
- [An Empirical Study of Harness Design for Coding Agents](https://arxiv.org/abs/2609.20804)：组件控制比较，提醒本研究后续实验需要固定模型和预算。
- [Understanding Agent-Reactive Bugs at the Model-Harness Boundary](https://arxiv.org/abs/2607.15684)：提示将模型反应与 harness 规则共同考察。
- [Why Do Multi-Agent LLM Systems Fail?](https://arxiv.org/abs/2503.13657)：系统设计、agent 间协调及验证/终止分类可用于后续编码对照。

本地 sources/ 的材料保留原始获取时间和 URL。本文对文献仅作范围定位，没有采用其数字作为本系统结论。

arXiv 范围查询另取得20条记录（保存为 `sources/arxiv-records.json`）。摘要级检查发现三个需优先全文比较的工作：

- [How Agent Harness Evolution Shapes Coding Agent Quality](https://arxiv.org/abs/2607.03691)：固定模型比较 harness 不同发布版本；本研究需要区分产品版本演进与运行时 DAG evolution 两种“evolution”。
- [Recursive Agent Harnesses](https://arxiv.org/abs/2606.13643)：研究递归 harness 在长上下文任务中的收益；这是“多层结构总是有害”的重要替代解释，应检查任务类型和边界契约。
- [Complex Agents, Shallow Tests](https://arxiv.org/abs/2610.04921)：关注 harness 实现与测试充分性；本轮发现的分类规则错误应与这一方向比较，避免全归入模型认知问题。

以上只完成摘要级定位，尚未评价其全文方法质量或复现实验。

## 7. 后续工作与完成门槛

1. 补齐 P0：历史执行 trace/部署版本映射；明确无法恢复的区间；完成相关工作范围矩阵。
   本轮尝试只读打开 worker.db 返回 unable to open database file，未取得 worker 执行侧记录；错误已留痕，不能据此断言历史 trace 已被删除。
2. 校准 P1：人工复核六例中的关键判断，尤其真实交付、主动取消与必要门禁；修订标注手册后冻结。
3. 正式 P2：当前30例是基于可观察代理分层的候选，尚不能称10/10/10结果分层样本。实际非done业务Plan只有3个，需按内容重新分类，记录配额修订依据后逐例核验。
4. P3：完成全样本证据评估后输出机制频率、敏感性分析和正式报告；缺失时间及无效成本不参与点估计。
5. P4：只有明确可测试机制后才运行隔离受控实验；不在生产系统偷偷改变策略。

正式研究出口尚未达到。本报告保留首轮可复现事实和候选机制，避免在原始 trace、结果核验和人工复核不足时给出确定性因果结论。
