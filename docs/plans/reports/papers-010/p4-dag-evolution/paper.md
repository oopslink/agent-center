# DAG evolution 的实际价值：改图、修复与成果继承

**论文类型：**案例驱动的研究与立场工作稿；作者、单位及通讯信息待补。**状态：**人工智能（AI）辅助分析与写作，尚未人工复核。

## 中文摘要

动态任务图可以表达新工作，但图的增长不等于交付改善。本文复用 Agent Center 的冻结运行记录，以008阶段205条 generation 的结构筛查、六个主案例和两个混合排除案例为基础，并结合004阶段的合同谱系比较，区分图变化、代码修复、信息增量、环境条件与验收范围。本轮重算排除六个显式夹具后，102个Plan中26个有generation，共195条。原研究及本轮分析尚未人工复核，未重跑生产验收。案例显示：Mermaid 修复报告早于新增代际46.486869秒；Access 的一次局部修复在原任务内完成；组织Graph保留旧图节点仍出现代码继承缺口；Spark两个不同提交却具有相同父提交和完整文件树。用户追加范围与外部等待具有独立意义，后继PASS也不能恢复已撤回的旧合同结论。Insight重复G9的五个节点事实可核，根因仍未知，故排除设计负效应归因。本文据此主张保留局部改图能力，并将每次变化绑定到具体前提、候选产物和验收合同。该主张是案例支持的设计方向，不是DAG演化提高成功率或降低成本的因果结论。

**关键词：**DAG演化；Agent harness；产物继承；验收合同；过程重建

## Abstract

Dynamic task graphs can represent new work, but graph growth does not establish delivery improvement. This working paper reuses frozen Agent Center records: a structural screening of 205 generations, six focal cases and two mixed excluded cases from study 008, and contract-lineage comparisons from study 004. It distinguishes graph edits, code repairs, new information, environmental prerequisites, and changes in acceptance scope. A recomputation excluding six explicitly marked fixture plans yields 195 generations across 26 of the remaining 102 plans. The original research and the present analysis have not undergone human review, and production acceptance tests were not rerun. A Mermaid repair report preceded a new generation by 46.486869 seconds, while an Access correction occurred within its original task. Preserved graph nodes did not guarantee code inheritance in the organization-graph case. Conversely, two different Spark commits had the same parent and complete tree. New user requirements and external waiting conditions had distinct meanings, and a successor PASS did not reinstate a revoked earlier verdict. Five duplicate Insight G9 nodes were documented, but their root cause remains unknown and is excluded from negative design attribution. We argue for retaining local graph adaptation while binding each change to concrete premises, candidate artifacts, and acceptance contracts. The evidence supports a design direction, not a causal estimate of success-rate or cost improvements.

**Keywords:** DAG evolution; agent harness; artifact inheritance; acceptance contract; process reconstruction

<!-- BODY_START -->
## 1 引言

动态工作流的价值应由它组织了什么新工作来说明，而不能由计划多了多少代来定义。本文把DAG evolution理解为任务有向无环图（directed acyclic graph，DAG）在运行过程中对后续工作、依赖和处置的调整。在008资料中，新增代际既可能伴随代码修复，也可能记录范围变更、等待或历史处置；这些事件的意义明显不同。[008-M1](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-evolution-008.md) 因而“最后通过了”不足以证明此前每次改图都有效，“改了很多次”也不足以证明机制在反复折腾。

一个尤其容易混淆的问题是成果继承。图可以保留已完成节点，后继代码却仍可能从不含该成果的基线构建；相反，提交标识变化也可能保留相同文件内容。008的本地Git核查同时出现这两类证据。[008-GIT](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/logs/source-checks.json) 如果只观察节点与提交数量，就会把组织历史、产物连续性和实际工作量混为一谈。

本文的研究问题是：哪些变化使改图具有可解释的用途；图、代码与验收需要怎样的独立证据；后继结果应如何影响旧结果的判断。研究以具体时序和反例收紧归因，不估计DAG演化的平均效果。中心立场是保留适应能力，同时支持原任务续修、仅补证据和显式等待；图是承载这些选择的结构，不是修复本身。

这也规定了本文的贡献范围。本文不提出动态图新算法，不把同一案例多次研究当成独立复现，而是呈现一组可审计的判断规则：先定位变化的前提，再核对被继承的产物，最后确认验收适用于哪个合同。相关工作提供比较坐标，后续方法和案例则说明这些规则怎样改变具体结论。

如果把组织动作和产品变化分开，研究便可以提出更具体的反事实问题：没有这次改图，既有候选是否仍能接受同样的复验；有了这次改图，新增节点是否真正取得不同输入。这些问题在现有资料中不能直接求值，却能防止把管理上的推进误当作工程上的完成，并指引未来应补采什么证据。

## 2 相关研究与概念

动态工作流已有不同的优化单位。AFlow以完整工作流为搜索节点，结合蒙特卡洛树搜索、代码修改与执行反馈优化可数值评价的任务流程；其数据集层面的搜索并不等于生产计划中一次改图。[1]<!--ref:R11--><!--anchor:section:4--> DyFlow通过designer与executor分层，根据历史计划、中间输出和反馈生成阶段子图，并用共享记忆缓冲区向后续操作提供输入。[2]<!--ref:R28--><!--anchor:section:3.1, 3.2--> AgentConductor在同一问题内根据执行反馈更新分层DAG，并随任务难度调节图密度，报告其竞赛代码基准中的准确率与token成本。[3]<!--ref:R29--><!--anchor:section:2.1.2, 3.2.2--> 这些工作反对“一切改图无用”的结论，也不允许把其收益直接换算成本系统的generation收益。

过程分析提供另一种视角。Failure as a Process区分失败起点、演进与恢复，同样的可见错误可以来自不同机制。[4]<!--ref:R05--><!--anchor:section:II--> 对象中心过程挖掘指出，同一事件可能联系多个对象，按单一case ID展开会造成分离的视图；后续研究进一步把case表示为相关对象的事件图。[5]<!--ref:R26--><!--anchor:section:Abstract-->[6]<!--ref:R27--><!--anchor:section:IV--> 本文借鉴这种范围意识，但不复现其算法，也不把对象相连当成业务目标等价。

本文区分四类状态。任务图描述组织关系；generation保存某次演化的记录及快照；候选产物由精确提交或文件内容标识；验收合同说明何种行为、范围和环境可以获准通过。累计快照可以包含完成或废弃节点，所以它不是某一时刻的活动图。008中的父子关系完整、端点存在，仅支持结构引用成立，不证明任务能够运行或交付合格。[008-M2](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/derived/delta-summary.json)

因此，本文将“修复”保留给产物或行为的改变，把新增测试事实、用户改需求和图编辑分别记录。这个概念约束使后文能够讨论联合作用，同时避免把共同发生写成单一机制的因果链。

三类工作流方法也提示，比较需要对齐干预单位。搜索完整程序、在同题推理中生成子图与持久开发任务的代际管理，虽然都涉及图，却承担不同的记忆、执行和评价职责。本文只把它们用于界定可比较的问题，不把一个基准中的有效结构搬成生产流程的普遍答案。相关文献在本稿中是事后比较材料，不被描述为早期系统设计的来源。[1]<!--ref:R11--><!--anchor:section:4-->[2]<!--ref:R28--><!--anchor:section:3.1, 3.2-->[3]<!--ref:R29--><!--anchor:section:2.1.2, 3.2.2-->

## 3 材料与方法

008阶段对108个Plan的冻结记录进行结构盘点，其中32个存在generation，且包含六个显式夹具Plan。205条记录由32个根和173个子代组成；170个子代改变节点或边，三个只作处置而无拓扑变化。此处是记录总数，夹具不进入生产效果推断。[008-M1](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-evolution-008.md)、[008-M2](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/derived/delta-summary.json)

原研究筛查全部代际的reason与evidence文本，定向选择九个Plan源包；其中414个Task、116条generation和3420条消息是容器规模，并非全部逐条深读。正式编码含100项不同原生记录，形成六主案例与两混合排除案例。本稿重读所用原记录副本、结构聚合及Git命令日志，未重新执行生产验收。[008-M3](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/reading/coverage.json)

证据按其能力分层：消息证明当时的说法，审计证明记录的状态转换，历史验收报告证明存在某个PASS或REJECT主张，Git结果直接支持指定对象的祖先、文件存在或整树关系。产品代码缺陷可以成为正常修复输入，harness自身故障及未知运行根因则不承担设计负效应推断。[008-X](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-evolution-cases-008.md)

004阶段的六组比较、13个合同片段和27个事件用于检查结果归属，不与008相加成独立样本。其B0为状态展示，B1为完整单Plan本地证据，B2增加明确关联端点；B1不故意删除已出现的后继叙述。[004-M](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-lineage-004.md) 两阶段均为已知案例的AI探索，尚未人工复核，没有保持原图的匹配对照。

具体分析先定位事件及其时间，再分别检查图、产物和合同是否变化。结构差分沿原生边语义解释：from_task_id所指任务依赖to_task_id所指任务；缺省字段按既有比较约定归一。未知派发值仍保留未知，不能转成零执行。此流程使“没有新增节点”“没有新增交付”和“没有运行”成为三个分别需要证据的判断。对跨Plan事件，仅依明确承接、替代或验收关系关联，不凭同项目或相似标题合并。[008-M1](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-evolution-008.md)、[004-M](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-lineage-004.md)

本次从原生Plan和generation冻结导出重新计算基数与父子图差分，表1将夹具分开。显式夹具沿008的名称标记规则识别；排除它们后仍不是无系统bug的纯净效果样本。[制图底表与原生定位](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P4/data.json) · [源文件、字段与哈希](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P4/source-provenance.json)

**表1。Plan与generation记录范围**

| 范围 | Plan数 | 有generation的Plan | generation总数 | 根记录 | 拓扑改变子代 | 无拓扑改变子代 |
|---|---:|---:|---:|---:|---:|---:|
| 全部冻结记录 | 108 | 32 | 205 | 32 | 170 | 3 |
| 显式夹具 | 6 | 6 | 10 | 6 | 4 | 0 |
| 排除显式夹具 | 102 | 26 | 195 | 26 | 166 | 3 |

图1展示26个有记录Plan的分布：中位数4.5条，范围1–33。分母只限有generation记录的Plan；另外76个没有记录，不能推断它们从未发生live edit。记录次数既不定义失败，也不定义有效恢复。

![图1：非显式夹具Plan的代际记录分布](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p4-dag-evolution/figures/p4-fig1-generation-distribution.png)

**图1。**26个非显式夹具Plan按generation记录数排序，每行保留同一Plan的（a）记录数，（b）根与最后累计节点数，（c）根与最后seq依赖深度。空心圆为根、实心菱形为最后记录；节点轴为以2为底的对数，重合表示数值相同。合计195条记录，G编号见补充映射；不表示无bug样本或成功终局。

## 4 案例与结果

### 4.1 代码改变可以先于改图，也可以留在原任务

Mermaid案例最清楚地约束了时间解释。Tester在回归通过后报告真实浏览器的多图渲染长期pending，随后开发成员报告修复队列与状态发布，再出现新增代际，最后同一候选获得复验PASS报告。表2仅表示记录顺序，不把报告时间冒充实际编码完成时间。

| 表2：2026年8月17日协调世界时（UTC） | 记录事件 | 对解释的约束 |
|---|---|---|
| 08:53:35.958879 | 浏览器缺陷REJECT，E01-01 | 新检查暴露具体产品缺口 |
| 09:00:40.896165 | 修复候选6855b0bd已推送，E01-02 | 修复报告已经存在 |
| 09:01:27.383034 | 新generation补修复与复验节点，E01-04 | 晚于修复报告46.486869秒 |
| 09:13:15.332019 | 同一候选复验报告PASS，E01-03 | 只覆盖该候选与检查范围 |

原始链：[E01-01](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-01.json)、[E01-02](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-02.json)、[E01-04](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-04.json)、[E01-03](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-03.json)。改图可以安排正式复验，却不能被解释为先产生了这次修复。同案旧集成在REJECT后变ready的调度故障另行排除。

Access新Plan中，S1因缺Risk字段和typed-delete默认启用被拒，负责人要求在原分支追加提交；约六分钟后，对06183d8c报告同Task通过。原生Plan仅有一个初始generation及六个任务。[E06-07](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-07.json)、[E06-08](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-08.json)、[E06-10](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-10.json) 这提供了“每次REJECT都必须增代”的直接反例，但不证明原任务续修总是更优。

两段修复的差别不是有没有质量门，而是如何表达后续工作。Mermaid把修复与复验写成新节点，Access保留原Task和原分支；两者都有可定位的拒绝理由与候选修正报告。现有记录没有比较哪一种花费更少，也没有理由把Access的局部成功外推为取消所有代际。它们共同说明，恢复机制应先判断缺口是否改变责任、依赖或验收路径，再选择组织形式，而不把REJECT机械映射为图重建。[E01-04](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-04.json)、[E06-07](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-07.json)

![图2：Mermaid的真实图增量与事件顺序](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p4-dag-evolution/figures/p4-fig2-mermaid-delta.png)

**图2。**Mermaid的真实图增量与事件顺序。generation-384637ec相对父代增加修复、复验两个节点及一条依赖边，累计由3节点2边变为5节点3边。未虚构复验到原集成的依赖边；下方事件按顺序排列、不按时长比例。修复报告比改图早46.486869秒。

图2将“图里安排复验”与“代码已报告修复”分开。累计保留的旧节点不自动代表仍在活动，也不能将文字上的后续放行要求补画成原生依赖。[制图底表与原生定位](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P4/data.json) · [源文件、字段与哈希](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P4/source-provenance.json)

### 4.2 图继承和代码继承需要分别验证

组织Graph案例中的Graph是被开发产品，应与执行任务的DAG区分。G5检查发现同一语义边跨cursor页无法稳定归并；G7后又发现合并边只保留单个effect ID，无法取得两页证据，随后终验报告覆盖跨项目请求与去重。各轮出现不同输入和断言，不能仅凭多次REJECT将它们视为同一劳动的重复。[E02-01](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E02-01.json)、[E02-05](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E02-05.json)、[E02-06](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E02-06.json)

但中间G6从旧main构建，未继承已获局部验收的G5能力。008的Git核查确认G5不是G6祖先，且Graph后端文件在G5存在、在G6缺失；G5后来是G7与最终候选的祖先。[E02-04](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E02-04.json)、[008-GIT](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/logs/source-checks.json) 保留历史节点与文字要求没有独自保证产物连续性。错基线的来源仍未隔离，所以该案例支持增加产物核查，而非证明DAG造成代码丢失。

Spark则给出相反方向的反证。原始任务要求25类向导，候选只有13类，而测试只要求至少12类；后续报告补齐25类。[E03-11](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-11.json)、[E03-01](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-01.json)、[E03-02](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-02.json) 其前置ce5bbe69被20e942f6替代，Git核得两者父提交相同且完整tree均为038278d8，因此不同SHA不等于成果丢失。随后另一个可靠性候选确实没有继承已验Web候选，恢复候选才具有正确祖先关系；最终发布矩阵仍报告NO-GO，局部恢复不能扩展成整个产品完成。[008-GIT](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/logs/source-checks.json)、[E03-03](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-03.json)、[E03-05](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-05.json)、[E03-06](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-06.json)

这组正反证据还说明，继承检查需要同时关心结构和语义。祖先关系核对可以发现候选未沿预期代码链构建，完整树核对又能防止把内容相同的重建误判为损失；但两者都不能证明25类向导的交互合同已经满足。Spark中的阈值测试允许13类通过，真正缺失的是与合同一致的断言。因而改图安排补充实现后，仍需在新候选上检查原要求，而不能让新增节点完成状态代替覆盖检查。[E03-01](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-01.json)、[E03-02](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-02.json)、[008-GIT](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/logs/source-checks.json)

![图3：组织Graph的两种谱系](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p4-dag-evolution/figures/p4-fig3-artifact-lineage.png)

**图3。**组织Graph的两种谱系。上层为原生generation父子链；候选与各代的对应来自报告。下层实线是Git直接父提交，虚线仅为Git已核祖先关系，不声称直接父子；G8缺独立代码关系核验，故不补边。

图3让“G6接在G5代际之后”与“G6代码没有继承G5”同时可见。前者不能证明后者，后者也不证明DAG导致选错基线。另一个方向的Spark同树证据仍保留：不同提交标识不必然意味着内容损失。[制图底表与原生定位](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P4/data.json) · [源文件、字段与哈希](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P4/source-provenance.json)

### 4.3 重复事实可核，重复根因仍须悬置

Insight已有canonical G9后又创建五个G9节点，随后全部废弃；一个重复根节点曾进入running。参与者将重复归因于首次读取截断，但没有完整工具响应与并发轨迹来排除系统原因。[E04-03](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-03.json)、[E04-10](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-10.json)、[E04-12](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-12.json)、[E04-14—18](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-evolution-cases-008.md) 因此本文保留重复建链事实，排除其设计负效应归因，也不称为零执行或估算浪费。

后续可观察的有效选择是：视觉证据缺失时保留f4c35357候选及技术验收，只替换视觉复验与下游交付节点，最后出现c558505c的终验报告。[E04-06](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-06.json)、[E04-08](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-08.json) 这体现了局部调整的用途；终验既不能抹去重复历史，也不能证明重复本身促成成功。

这里需要保留两条不相互覆盖的时间线。一条是重复节点的创建、短暂运行与废弃，另一条是有效候选的持续保留、缺失视觉证据的补交和后续终验。将两条线压成“重复后成功”会制造因果暗示；压成“发生重复所以设计无用”又会丢掉局部补证据的可观察用途。本文以原生身份和明确候选分开追踪，使不确定根因不妨碍记录已发生的处置。[E04-12](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-12.json)、[E04-13](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-13.json)、[E04-06](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-06.json)

### 4.4 新范围与后继合同不能被压成统一成功

Spark Host中的Mac、多实例以及默认本地制品服务来自用户明确新增要求，并非同一合同的再次失败。[E05-06](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-06.json)、[E05-08](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-08.json) 部署报告随后区分已部署健康与尚缺sudo授权、真实Linux及public-CA目标；新链要求复用现有部署完成余项。两个剩余任务最终因项目清理被废弃，故全矩阵验收仍未证实。[E05-12](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-12.json)、[E05-14](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-14.json)、[E05-24](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-24.json)、[E05-25](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-25.json)

Access则先撤回旧7700038b的整页PASS，用户再改变信息架构，要求Access只保留两个入口，将RAM Roles列表纳入Team Role属性；新Plan对1b5804fd报告新合同终验通过。[E06-01](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-01.json)、[E06-03](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-03.json)、[E06-04](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-04.json)、[E06-09](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-09.json) 图、合同、代码和责任安排同时变化，不能将新结果只归给改图，更不能用新PASS追认旧PASS。

这两个案例使“完成”至少需要带上产品范围与交付层级。Spark Host已经部署某版本，与真实双平台矩阵尚未完成可以同时成立；Access新页面通过，与旧页面的整体验收失效也可以同时成立。把停止执行记为一种合法处置，不等于把它当作技术成功。图应帮助展示这些差异，而不是借终态折叠掉尚未验证或已放弃的要求。[E05-16](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-16.json)、[E05-17](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-17.json)、[E06-03](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-03.json)

### 4.5 改图幅度、节点保留与累计增长

定量分析以排除显式夹具后的169个父子转移为单位，涉及20个实际具有子代的Plan；其余6个有generation的Plan只有根记录。对父、子节点集Vₚ、V꜀定义保留率`R=|Vₚ∩V꜀|/|Vₚ|`，新增占比`A=|V꜀−Vₚ|/|V꜀|`；边按端点、kind、when及max_rounds归一后的元组比较。属性改变可能表现为边元组的一删一增，不能一律解释为依赖端点改变。表3列出互斥的结构变化类型。

**表3。169个父子转移的结构变化**

| 变化类型 | 转移数 | 占169个转移 |
|---|---:|---:|
| 新增节点（可增边），不删除既有节点或边 | 60 | 35.5% |
| 新增节点并删除／改写边，保留父节点 | 83 | 49.1% |
| 存在父节点删除（可同时新增） | 20 | 11.8% |
| 节点不变，仅边改变 | 3 | 1.8% |
| 节点、边均不变 | 3 | 1.8% |

149/169次（88.2%）保留全部父节点；按转移计算的平均保留率为98.0%。每次新增节点中位数为3个，IQR为2–4个；但新增节点只占子代累计节点的中位9.5%（IQR 5.8%–16.0%）。因此“图中多数节点是保留的历史”与“仍有真实依赖调整”可以同时成立，不能将高节点保留率当成无意义重复。

转移数量分布不均，记录最多的三个Plan贡献40.8%转移。所有转移等权时，平均新增占比为12.7%；先在每Plan内平均、再对20个Plan等权时为18.9%。图4（d）保留这两种权重，防止长演化链被当成很多独立任务。这里的分母变化是统计权重敏感性，不是改图收益的估计。

对26个有generation的Plan，逐一比较唯一根记录与按时间最后一条记录，累计节点数中位数从8变为17.5；逐Plan“最后／根”比值的中位数为2.04倍（IQR 1.17–3.31，范围1.00–7.79）。这些是配对快照的结构增长，不代表同等难度下多花的工作、模型实际读取量或认知负担；最后记录也不等于成功终局。

![图4：局部改图与累计增长的定量比较](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p4-dag-evolution/figures/p4-fig4-evolution-quantification.png)

**图4。**（a）169次转移的父节点保留率与边Jaccard经验累积分布；（b）边元组增删联合分布，点面积按15+10n平方点显示同坐标转移数；（c）26个Plan根／最后累计节点数，两轴以2为底，虚线是数量不变；（d）新增节点占子代比例，分别按169次转移等权和20个Plan等权。结构统计排除显式夹具，但不是无bug效果样本。

这些结果把DAG evolution的可测部分限定为局部增量、历史保留和累计结构变化。代码修复、产物继承、人工补充及验收合同的贡献仍须由案例分别识别；无法从169次转移推算“多少次有效恢复”或“多少次重复折腾”。[全部父子差分](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/data/P4-deltas.json) · [26个Plan配对表](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/data/P4-plans.json) · [方案与权重定义](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/protocol.md)

### 4.6 从拓扑差分到任务内容差分

节点保留和依赖保留并不相同。169个父子转移的边Jaccard中位数为0.680，IQR 0.500–0.833；每次新增边元组中位数3（IQR 2–5），删除边元组中位数1（IQR 0–3）。边元组包含端点、kind、when、max_rounds，属性变化也可表现为一删一增。图4a–b将高节点保留与真实边变动并置，不能从149次保留全部父节点推断149次无效重复。

仅对父子共有Task进一步比较五个字段，得到表4。分母是169次转移；一个转移可同时改变多种字段，行数不能相加。字段变化次数是Task×转移，重复Task也不代表新的独立任务。

**表4。共有Task字段变化的两种计数单位**

| 字段 | 至少改一项的转移数／169 | Task×转移变化次数 |
|---|---:|---:|
| status | 160 | 592 |
| assignee_ref | 16 | 22 |
| description | 1 | 16 |
| title | 0 | 0 |
| delivery_contract | 0 | 0 |

3次拓扑完全不变的转移都改变了共有Task状态，分别涉及2、2、1个Task；因此不能把它们命名为“空转”或“完全重复快照”。另一方面，状态变动可能只是其它动作的快照结果，不能倒过来称为改图导致状态推进。delivery_contract字段不变也不证明验收合同未变，因为验收范围可能存在于任务描述、消息及其它实体中。

![图5：拓扑之外的任务字段演化](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p4-dag-evolution/figures/p4-fig5-task-attribute-evolution.png)

**图5。**（a）20个有子代Plan内，至少改变一项共有Task字段的转移比例，括号为该Plan转移分母；（b）169次转移的字段变化计数；（c）三次拓扑不变记录的状态、负责人改动数。只检查列出的五个Task字段和归一拓扑，未声称完整序列化快照全字段等价；新建或删除Task不计入共有Task字段变化。

图1同时保留全部26个Plan的记录数、根／最后节点数和seq深度，避免只展示三条长链。新增分析提供了“改了哪些结构和内容”的全量描述，但有效恢复、代码继承与验收范围仍要逐案判断，不能从计数生成改图收益率。[逐Task字段差分](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/006-dense-evidence/data/P4-attributes.json) · [Plan索引与根／最后配对](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/006-dense-evidence/supplement.md)

## 5 讨论与设计含义

### 5.1 评价变化的内容，而不是变化的数量

上述案例表明，一次generation可以同时承载不同变化。表5将其拆开，目的不是数值贡献分解，而是要求每项效果主张对应恰当的证据。

| 表5：变化因素 | 案例中可识别的内容 | 评价边界 |
|---|---|---|
| 图编辑 | Mermaid复验、Insight只补视觉证据 | 解释后续工作安排，不等于代码修复 |
| 代码改变 | 渲染队列、向导覆盖、Risk与typed-delete | 检查精确候选与行为，不由节点数代理 |
| 新检查信息 | cursor、多effect、错基线 | 可构成修复理由，不预设角色净收益 |
| 用户新要求 | Mac多实例、本地制品、Access新入口 | 保留合同变更，不能视为固定任务复试 |
| 外部条件 | sudo、真实Linux、公开证书目标 | 显式等待，不靠改图创造资源 |

表5依据[E01—E06](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-evolution-cases-008.md)。它支持一个实施建议：创建新图前记录哪项前提发生变化、预期新增什么产物、哪些成果仍有效。若没有这些内容，系统可以要求补足理由；但不能仅因没有代码变化就拒绝演化，因为证据补交与范围处置也可能是合法工作。

同样，零拓扑变化不应自动判为无效。008记录的三个相关子代涉及历史处置、延续已有恢复链和等待正式环境；后者明确避免创建可运行的占位重试。[008-M1](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-evolution-008.md) 此类操作改变的是计划的解释和执行条件。评价应区分管理状态、产品进展和可交付证据，而不是强迫每次处置都生成新的工作。

### 5.2 让图、产物和验收互相约束

产物继承既不能只看文字承诺，也不能只看祖先关系。组织Graph反例说明节点保留不足；Spark同树反例说明某个旧SHA不在祖先链中也不足以证明内容消失。本文建议采用表6的互补检查，把“同树”“有祖先”和“行为已验”分别记录。

| 表6：证据对象 | 能够核实的内容 | 仍不能推出 |
|---|---|---|
| 图及代际 | 节点、边、父代、保留或废弃决策 | 当前运行正确、代码已继承 |
| 精确候选与Git关系 | 指定祖先、完整树、文件存在 | 产品行为与完整合同已通过 |
| 验收及其撤回 | 对某候选、某合同的历史判断 | 后继所有SHA或更大范围都合格 |
| 环境回执 | 指定部署与前提是否得到确认 | 未执行平台或授权路径已通过 |

依据为[008-GIT](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/logs/source-checks.json)、[E05-12](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-12.json)、[E06-03](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-03.json)。设计上可要求每次后继声明列出被保留能力及其证据；内容等价时说明依据，发生修改时明确哪些旧验收可以复用、哪些必须重跑。这是待验证的机制建议，本文没有新增门禁实现。

### 5.3 结果关联的价值是纠正范围

004比较说明，跨Plan关联不应以“翻转多少失败”为目标。B1已经包含完整本地会话，B2只增加明确关系和端点证据。表7保留其代表性差异，避免把最弱的状态页面伪装成唯一基线。[004-C](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/derived/comparisons.json)

| 表7：案例 | B1已能判断 | B2新增内容 | 不变的边界 |
|---|---|---|---|
| Insight | 旧Plan废弃且已承接新Plan | 新开发合同的后继终验报告 | 生产发布仍未知 |
| Access | 旧PASS撤回、新合同已出现 | 新候选终验及合同关联 | 旧PASS仍撤销 |
| Operation | 本地已有替代交付叙述 | 替代Task字段与归属核对 | 没有结果翻转 |
| Mermaid | 同Plan修复、PASS与集成 | 指定祖先关系补证 | 漂移后的分支tip未获旧验收 |

由此可提出最小的演化记录单位：一次有明确前提的处置，关联一组仍有效的产物、一个待满足的合同片段和下一项行动。原任务续修、仅补证据、局部增图、跨Plan承接和等待都应是可选路径。若任务范围没有变、输入仍充分，Access片段说明无需仅为REJECT而强制新代；若新责任或依赖需要独立审计，局部增图才有具体理由。未来应在等价任务与权限条件下比较这些选择，分别记录交付、额外协调和恢复成本，才能评价动态图的净价值。

一种可供验证的界面也应分别显示当前可执行任务和累计审计历史。前者回答接下来可以做什么，后者回答为什么走到这里；二者可以关联，却不宜共用一个大小指标。对每个保留成果，展示候选、适用合同、已知检查和剩余条件，能够让用户判断此次演化是否只是重新命名工作。这个建议来自本研究对累计快照和成果关系的区分，尚未通过产品实验验证。

对于自动化策略，关键也不是一律少改图，而是避免预先规定唯一恢复形式。若新的反例改变验收对象，可以新增独立复验；若只缺一份可审计报告，可以保留候选并补证据；若缺少现实授权，应记录等待而不反复创建可运行任务。这些选择都需要相同的输入、输出和责任说明，其成本及可靠性应在未来分别测量。

## 6 局限

本研究没有静态图与动态图的随机或匹配对照，不能估计成功率提升、token节省、耗时或认知负荷。六个案例由已知问题和可读记录目的性选择，结构盘点包含夹具，容器总量不是执行分母；累计generation、Task和消息也不能解释为活跃图大小。[008-M1](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-evolution-008.md)

验收主要来自历史参与者报告。008的Git核查比单纯自述多了一层对象证据，但只支持祖先、文件与树关系，没有独立确认全部产品行为；本稿读取这些保存结果，也没有再次运行验收。三个实现源码blob读取失败已留日志，因此不能推断相应实现不存在或恢复所有历史部署版本。[008-GIT](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/logs/source-checks.json)

重复G9、错误基线及部分运行恢复仍有未知根因，本文只保留可核事实及归因边界。004与008案例相互重合，B2也不是结果真值，不能报告重建准确率。001—009由单AI开展，本轮另有分工AI写作与审阅，均尚未人工复核。精确模型输入、版本与完整工具响应缺失，使未来复现需要先恢复输入条件；AI评阅不能弥补这些资料缺口，也不等同人工核查。

结果判定还有合同恢复不完全的限制。当前Plan描述可能经过编辑，不能倒推最初需求；历史报告中的“独立”也只是执行者的描述，未必具有研究方法上的独立性。尤其Access的新Plan集中由同一PD（原记录协调角色标签）承担工作，不能据其后继PASS比较单角色与多角色优劣。004明确保留了这些边界，本稿沿用而不将其改写为已验证的组织效应。[004-M](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-lineage-004.md)

## 7 结论

DAG演化的实际用途在于表达新的工作、依赖和处置，使剩余缺口进入可追踪的执行路径。本文的案例同时说明其边界：修复可以发生在改图之前，也可以在原任务内完成；保留节点不保证保留代码，换SHA也不保证改变内容；后继合同通过不能撤销旧候选的失败或恢复已经撤回的PASS。[E01-02](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-02.json)、[E06-08](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-08.json)、[008-GIT](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/logs/source-checks.json)

因此，本文建议保留局部适应机制，并将每次变化绑定具体前提、候选产物和验收范围，同时允许继续已有链、只补证据与等待真实条件。更可靠的评价单位是有证据的变化及其结果，而非代数和终态。这个方向尚待对照检验：现有材料支持解释用途、识别反例和限定结果归属，不支持动态图必然提高交付或应被全面取消的结论。
从工程角度看，值得保留的是根据新证据调整后续路径的能力，值得约束的是把组织变动误认作成果。今后的实证比较应同时固定或记录合同、候选、环境与权限，并保留失败、撤回和放弃事件。只有在这些条件下，才能进一步回答哪种演化方式更可靠、更经济，而不再依靠图看起来变大或最终状态变绿来判断。
<!-- BODY_END -->

## 参考文献

[1] J. Zhang et al., “AFlow: Automating Agentic Workflow Generation,” arXiv:2410.10762v4, 2024. DOI: [10.48550/arXiv.2410.10762](https://doi.org/10.48550/arXiv.2410.10762). [核定版本](https://arxiv.org/abs/2410.10762v4).

[2] Y. Wang et al., “DyFlow: Dynamic Workflow Framework for Agentic Reasoning,” arXiv:2509.26062v1, 2025. DOI: [10.48550/arXiv.2509.26062](https://doi.org/10.48550/arXiv.2509.26062). [核定版本](https://arxiv.org/abs/2509.26062v1).

[3] S. Wang et al., “AgentConductor: Topology Evolution for Multi-Agent Competition-Level Code Generation,” arXiv:2602.17100v1, 2026. DOI: [10.48550/arXiv.2602.17100](https://doi.org/10.48550/arXiv.2602.17100). [核定版本](https://arxiv.org/abs/2602.17100v1).

[4] X. Zhao et al., “Failure as a Process: An Anatomy of CLI Coding Agent Trajectories,” arXiv:2607.09510v1, 2026. DOI: [10.48550/arXiv.2607.09510](https://doi.org/10.48550/arXiv.2607.09510). [核定版本](https://arxiv.org/abs/2607.09510v1).

[5] W. M. P. van der Aalst, “Object-Centric Process Mining: Dealing with Divergence and Convergence in Event Data,” *Software Engineering and Formal Methods*, 2019. DOI: [10.1007/978-3-030-30446-1_1](https://doi.org/10.1007/978-3-030-30446-1_1). [出版记录](https://link.springer.com/chapter/10.1007/978-3-030-30446-1_1).

[6] J. N. Adams, D. Schuster, S. Schmitz, G. Schuh, and W. M. P. van der Aalst, “Defining Cases and Variants for Object-Centric Event Data,” arXiv:2208.03235v1, 2022. DOI: [10.48550/arXiv.2208.03235](https://doi.org/10.48550/arXiv.2208.03235). [核定版本](https://arxiv.org/abs/2208.03235v1).

## 研究声明

**数据可用性。**冻结提取、证据副本、差分和Git命令日志保存在私有run-001目录。附录为本机绝对路径；公开共享、许可及脱敏方案待作者确认，不能由这些链接推断数据已经公开。

**伦理与隐私。**使用既有开发消息、稳定身份、任务及产物记录，没有新增用户实验。伦理审查状态、使用授权与外部发布范围待补；最终发表应核查敏感配置、内部路径与对话的披露权限。

**作者贡献。**人类作者、单位、通讯信息及CRediT角色待补。AI不列为作者，最终署名者承担事实及论证责任。

**利益冲突。** 待作者声明。

**资助。** 本研究未获得资助。

**AI使用。**001—009原研究为单AI探索；010本轮有分工AI补查、引用核验、写作和审阅。原研究及本轮分析均尚未人工复核；AI审阅不构成独立人工复核或跨模型复现。精确模型版本与参数未完整记录。

**同源数据及跨篇重合。**本文与P1—P3共享Agent Center和run-001材料，Mermaid、Access及部分角色检查记录有重合。本篇以008演化与004结果谱系为主，不把重读或跨篇引用当新样本。外部文献用于比较，不声称后出研究影响更早的系统设计。

## 证据附录

008证据JSON保留原记录、record_key、source与pointer。以下编号不属于外部参考文献。004比较是既有研究的分析产物，保持其“未人工复核”状态；Git日志核对也不等于独立重跑产品验收。

| 证据 | 原始文件绝对路径 | 论点与证据级别 |
|---|---|---|
| E01-01—04 | [拒绝](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-01.json)、[修复报告](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-02.json)、[复验](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-03.json)、[代际原记录](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-04.json) | 修复报告早于改图；时间戳和报告 |
| E02-01、04、05、06 | [cursor缺口](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E02-01.json)、[错基线](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E02-04.json)、[多effect](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E02-05.json)、[终验报告](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E02-06.json) | 各轮有新信息；PASS是历史主张 |
| E03-11、01、02 | [初始合同](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-11.json)、[13对25](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-01.json)、[补齐报告](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-02.json) | 原合同缺口，不是新增25类要求 |
| 008-GIT | [source-checks.json](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/logs/source-checks.json) | 祖先、父提交、文件、同树及失败读取的命令与退出码 |
| E03-03、05、06 | [下游错候选](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-03.json)、[恢复候选](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-05.json)、[NO-GO](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-06.json) | 局部恢复不等于发布完成 |
| E04-09、10、12、13 | [原G9](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-09.json)、[重复G9](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-10.json)、[running审计](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-12.json)、[discard审计](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-13.json) | 重复发生但不是零执行 |
| E04-14—18 | [Task1](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-14.json)、[Task2](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-15.json)、[Task3](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-16.json)、[Task4](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-17.json)、[Task5](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-18.json) | 原生状态，五个重复节点废弃 |
| E04-03、06、08 | [根因自述](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-03.json)、[仅补证据](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-06.json)、[终验报告](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-08.json) | 根因未知与局部复用分别判断 |
| E05-06、08 | [Mac要求](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-06.json)、[本地分发要求](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-08.json) | 用户新范围 |
| E05-12、14 | [部署与缺口](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-12.json)、[复用部署链](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-14.json) | 外部前提并非代码缺陷 |
| E05-15—17、24—25 | [暂停](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-15.json)、[放弃授权任务](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-16.json)、[放弃验收任务](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-17.json)、[任务状态1](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-24.json)、[任务状态2](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-25.json) | 最终余项未执行通过 |
| E06-01、03、04、09 | [旧PASS](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-01.json)、[撤回](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-03.json)、[新IA](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-04.json)、[新终验](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-09.json) | 新合同不追认旧合同 |
| E06-07、08、10 | [原Task续修](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-07.json)、[同Task通过](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-08.json)、[唯一根快照](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-10.json) | 不增代修复的局部反例 |
| 008-M1、M2、M3、X | [总报告](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-evolution-008.md)、[结构聚合](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/derived/delta-summary.json)、[阅读覆盖](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/reading/coverage.json)、[排除台账](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-evolution-cases-008.md) | 方法、单位及排除边界 |
| 004-M、C | [方法与层级](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-lineage-004.md)、[B1/B2比较](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/derived/comparisons.json) | 结果补全、范围修正或不变，不是成功率提升 |
