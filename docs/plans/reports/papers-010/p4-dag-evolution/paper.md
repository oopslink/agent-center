# 智能体任务图演化中的产物继承与验收语义

*Artifact Inheritance and Acceptance Semantics in Evolving Agent Task Graphs*

**作者：** oopslink

**作者主页：** [https://github.com/oopslink](https://github.com/oopslink)

**论文类型：**案例研究与受控行为实验工作稿。**状态：**人工智能（AI）辅助分析与写作，尚未人工复核。

## 中文摘要

任务图演化能够组织新增义务，但不能直接代表产物继承或验收改善。本文结合Agent Center的195个非显式夹具generation快照、真实案例及256次受控短题响应，区分任务图、候选产物、合同和环境。历史证据同时包含图保留而能力缺失、不同提交同完整树、原任务续修与后继合同替代，说明单看代际不足以判断交付。实验交叉累计／活动图及产物、合同关系联表／分表，两模型原严格得分均96/128，但候选与合同字段均128/128；全部负分来自两题的动作分类歧义，不能用于DAG负面归因。排除两题全部条件后，两模型均96/96，各因素配对差均0；另冻结64次材料修复复测，单独保存。本文提出以变化前提、仍有效成果和待满足合同约束恢复形式。当前证据未证明动态图净收益、长期成本或内部认知原因，全部分析尚未人工复核。

**关键词：**DAG演化；Agent harness；产物继承；验收合同；过程重建

## Abstract

Task-graph evolution can organize new obligations but does not directly establish artifact inheritance or improved acceptance. This study combines 195 Agent Center generation snapshots after excluding explicit fixture plans, historical cases, and 256 controlled short-task responses. It separates the task graph, candidate artifact, contract, and environment. Historical evidence includes preserved graph history with missing capabilities, different commits with identical complete trees, in-task repair, and replacement contracts. The experiment crossed cumulative versus active graph views with joined versus distributed artifact and contract relations. Both models scored 96/128 under the frozen strict rubric, although candidate and contract fields were correct in all 128 responses. Every mismatch came from overlapping action categories in two fixtures and is excluded from negative DAG attribution. Across the other six scenarios, both models scored 96/96 with zero paired differences for all factors. A separately frozen 64-call material-repair study is preserved independently. The analysis motivates recovery choices grounded in changed premises, retained artifacts, and outstanding contracts, without establishing the net delivery value of dynamic graphs, long-term cost, or internal cognitive causes. Human verification has not been completed.

**Keywords:** DAG evolution; agent harness; artifact inheritance; acceptance contract; process reconstruction

<!-- BODY_START -->
## 1 引言

动态任务图能够调整后续工作、依赖和处置，但图增长本身不表示交付改善。Agent Center的演化记录同时包含代码修复、新检查、范围扩展、等待和历史处置。这些变化需要不同证据：后继通过不能说明此前每次改图都有效，多次改图也不能直接说明机制无用。[008-M1](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-evolution-008.md)

成果继承尤其容易被图结构遮蔽。图中保留已完成节点，后继代码仍可能从缺少相应成果的基线构建；反过来，两个不同提交标识也可能对应相同文件树。既有Git核查同时记录了这两类情况，说明组织历史与产物连续性需要分别判断。[008-GIT](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/logs/source-checks.json)

本文研究哪些变化为改图提供具体用途，如何连接图、产物与验收，以及关联重建怎样改变结果归属。核心立场是保留局部适应能力，同时允许原任务续修、补证据与显式等待。评价应先确认变化的前提和剩余工作，再核对继承成果与适用合同；本研究不提出新调度算法，也不估计DAG演化的平均效果。

## 2 相关研究与概念

动态工作流已有不同的优化单位。AFlow以完整工作流为搜索节点，结合蒙特卡洛树搜索、代码修改与执行反馈优化可数值评价的任务流程；其数据集层面的搜索并不等于生产计划中一次改图。[1]<!--ref:R11--><!--anchor:section:4--> DyFlow通过designer与executor分层，根据历史计划、中间输出和反馈生成阶段子图，并用共享记忆缓冲区向后续操作提供输入。[2]<!--ref:R28--><!--anchor:section:3.1, 3.2--> AgentConductor在同一问题内根据执行反馈更新分层DAG，并随任务难度调节图密度，报告其竞赛代码基准中的准确率与token成本。[3]<!--ref:R29--><!--anchor:section:2.1.2, 3.2.2--> 这些工作反对“一切改图无用”的结论，也不允许把其收益直接换算成本系统的generation收益。

过程分析提供另一种视角。Failure as a Process区分失败起点、演进与恢复，同样的可见错误可以来自不同机制。[4]<!--ref:R05--><!--anchor:section:II--> 对象中心过程挖掘指出，同一事件可能联系多个对象，按单一case ID展开会造成分离的视图；后续研究进一步把case表示为相关对象的事件图。[5]<!--ref:R26--><!--anchor:section:Abstract-->[6]<!--ref:R27--><!--anchor:section:IV--> 本文借鉴这种范围意识，但不复现其算法，也不把对象相连当成业务目标等价。

本文区分四类状态。任务图描述组织关系；generation保存某次演化的记录及快照；候选产物由精确提交或文件内容标识；验收合同说明何种行为、范围和环境可以获准通过。累计快照可以包含完成或废弃节点，所以它不是某一时刻的活动图。008中的父子关系完整、端点存在，仅支持结构引用成立，不证明任务能够运行或交付合格。[008-M2](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/derived/delta-summary.json)

因此，本文将“修复”保留给产物或行为的改变，把新增测试事实、用户改需求和图编辑分别记录。这个概念约束使后文能够讨论联合作用，同时避免把共同发生写成单一机制的因果链。

三类工作流方法也提示，比较需要对齐干预单位。搜索完整程序、在同题推理中生成子图与持久开发任务的代际管理，虽然都涉及图，却承担不同的记忆、执行和评价职责。本文只把它们用于界定可比较的问题，不把一个基准中的有效结构搬成生产流程的普遍答案。相关文献在本稿中是事后比较材料，不被描述为早期系统设计的来源。[1]<!--ref:R11--><!--anchor:section:4-->[2]<!--ref:R28--><!--anchor:section:3.1, 3.2-->[3]<!--ref:R29--><!--anchor:section:2.1.2, 3.2.2-->

图历史的呈现也可能影响当前证据的选取。Lost in the Middle的问答与检索实验说明，信息位置的效果随任务与模型而异，简单任务还可能出现天花板。本轮因而将历史视图作为有界表示因素，不把短题表现外推为动态图长期收益或部署模型的注意力机制。[7]<!--ref:R32--><!--anchor:section:2.3, 3.2-->

## 3 材料与方法

### 3.1 冻结部署资料与案例重建

本研究复用DAG演化专项的冻结记录、逐案证据与Git核查结果。结构盘点覆盖108个Plan、205条generation；排除六个显式夹具后，102个Plan中有26个保存了generation，共195条（表1）。其中20个Plan有子代，形成169个父子转移。显式夹具排除与设计失效归因是不同筛选，剩余结构记录仍含运行恢复与未知根因。[008-M1](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-evolution-008.md)、[008-M2](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/derived/delta-summary.json)

案例采用目的性选择，形成六个主案例与两个混合排除案例。另复用交付谱系研究的六组比较、13个合同片段和27个事件，检查单Plan判断与增加明确关联后的差异；重合记录不作为独立样本累加。材料范围、原生标识、查询与Git明细见[补充附录](evidence-details.md)。[008-M3](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/reading/coverage.json)、[004-M](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-lineage-004.md)

分析分别核对图结构、候选产物、输入条件和验收合同。消息用于恢复当时的主张，原生快照用于确认记录变化，Git日志用于确认指定对象的祖先、文件与整树关系。产品代码缺陷可以作为正常修复输入，harness故障及未知运行根因不承担设计负效应。全部研究尚未人工复核，未重跑历史生产验收。[008-X](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-evolution-cases-008.md)

结构统计沿父子快照比较节点、归一边元组和共有Task字段，并同时给出按转移与按Plan汇总的结果。累计快照保留历史节点，不能解释为当时的活动图；跨Plan结果只依据明确的承接、替代或验收关系关联。公式、字段归一及比较层级见补充附录。

**表1。Plan与generation记录范围**

| 范围 | Plan数 | 有generation的Plan | generation总数 | 根记录 | 拓扑改变子代 | 无拓扑改变子代 |
|---|---:|---:|---:|---:|---:|---:|
| 全部冻结记录 | 108 | 32 | 205 | 32 | 170 | 3 |
| 显式夹具 | 6 | 6 | 10 | 6 | 4 | 0 |
| 排除显式夹具 | 102 | 26 | 195 | 26 | 166 | 3 |

26个有记录Plan的generation数中位数为4.5，范围1–33（图1）。其余76个Plan没有这类记录，不能据此判断从未发生其它图编辑。这里的数量描述保存的演化历史，不是失败或恢复次数。

![图1：非显式夹具Plan的代际记录分布](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p4-dag-evolution/figures/p4-fig1-generation-distribution.png)

**图1。**26个非显式夹具Plan按generation记录数排序，每行保留同一Plan的（a）记录数，（b）根与最后累计节点数，（c）根与最后seq依赖深度。空心圆为根、实心菱形为最后记录；节点轴为以2为底的对数，重合表示数值相同。合计195条记录，G编号见补充映射；不表示无bug样本或成功终局。

### 3.2 受控图视图与证据关联实验

另以八种合成状态检验修复形式和证据范围选择：已有代码缺陷、新独立检查、已有回执待挂接、外部资源等待、授权新范围、能力丢失后的产物恢复、同树不同提交和替代合同通过。三个二水平因素为累计／活动图视图、产物关系联表／分表、合同关系联表／分表。累计视图展开18个已完成且无活动依赖的历史节点，活动视图保留其计数与无活动依赖事实；两组当前义务相同。联表与分表保留相同候选—提交—完整树—功能及合同—范围关系，只改变关系计算方式。

每模型8情境×8条件×2呈现顺序共128次，两模型合计256次，固定Sonnet/Haiku 5.5、同一隔离CLI与low effort。全部输入、评分和运行脚本在正式输出前哈希冻结。模型推荐下一处置，按action、candidate、contract分别核对；没有实际修改图、代码或生产验收。格式问题、字段错误和一次规则反馈后修改分别保存。这是表示与关联恢复对照，不是静态图对动态图的交付消融。

另构造四对相同任务图分别对应不同产物能力、合同范围、判定有效性和环境的状态，以检查仅凭图判断通过是否充分。这些是分析性反例，不能计作真实回归或模型错误。原始输出、所有反向证据和图源见[实验附录](controlled-experiments.md)，全部分析尚未人工复核。

输出读取后另做题目有效性审计；预检的表示等价与程序测试不能保证规范答案唯一。发现缺陷的情境按全部条件排除负面归因，原分数不改写。另固定本篇两题的全部条件、顺序和模型，共64次材料修复复测，属于事后诊断；不是新增预定独立样本，也不与主分母合并。具体问题及结果见研究发现。

## 4 研究发现

### 4.1 图编辑组织后续工作，代码修复可以独立发生

Mermaid案例中，真实浏览器检查暴露多图渲染缺口，开发成员随后报告修复候选已推送，之后才出现增加修复与复验节点的generation，最后对同一候选报告复验通过。表2显示修复报告早于改图，故改图在这里能够表达正式后续路径，不能解释为先产生了代码修复。

| 表2：2026年8月17日协调世界时（UTC） | 记录事件 | 对解释的约束 |
|---|---|---|
| 08:53:35.958879 | 浏览器缺陷REJECT，E01-01 | 新检查暴露具体产品缺口 |
| 09:00:40.896165 | 修复候选6855b0bd已推送，E01-02 | 修复报告已经存在 |
| 09:01:27.383034 | 新generation补修复与复验节点，E01-04 | 晚于修复报告46.486869秒 |
| 09:13:15.332019 | 同一候选复验报告PASS，E01-03 | 只覆盖该候选与检查范围 |

图2呈现了新增两个节点和一条依赖边的局部调整。报告时间用于确定记录顺序，不等于实际编码完成时间；同案中旧集成在拒绝后变为ready的调度故障，继续排除设计负效应归因。[E01-01](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-01.json)、[E01-02](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-02.json)、[E01-04](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-04.json)、[E01-03](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-03.json)

Access提供了另一种恢复路径。局部候选因缺Risk字段与删除确认问题被拒后，负责人要求在原分支追加修正，后续报告同一Task通过；原生Plan只保存一个初始generation和六个任务。这说明一次拒绝可以在原任务中继续处理，不必机械地触发新代。两个案例共同把选择条件指向责任、依赖和验收路径是否发生变化，而非是否出现REJECT标签。[E06-07](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-07.json)、[E06-08](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-08.json)、[E06-10](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-10.json)

![图2：Mermaid的真实图增量与事件顺序](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p4-dag-evolution/figures/p4-fig2-mermaid-delta.png)

**图2。**Mermaid的真实图增量与事件顺序。generation-384637ec相对父代增加修复、复验两个节点及一条依赖边，累计由3节点2边变为5节点3边。未虚构复验到原集成的依赖边；下方事件按顺序排列、不按时长比例。修复报告比改图早46.486869秒。

### 4.2 图中的历史保留不保证代码中的成果继承

组织Graph案例中，后继代际保留了此前工作记录，但一个中间候选从旧main构建，未继承已获局部验收的能力。Git核查确认，前一候选不是该中间候选的祖先，相应后端文件也从存在变为缺失；后续恢复候选重新包含预期祖先关系。图3将代际父子链与代码谱系并列，显示这两种连续性可以脱节。错基线的来源尚未隔离，该案例支持增加产物核查，不能证明DAG造成代码丢失。[E02-04](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E02-04.json)、[008-GIT](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/logs/source-checks.json)

Spark给出相反方向的反证：两个不同提交拥有相同父提交和完整文件树，因此提交标识变化不必然意味着内容丢失。同案又确有下游候选未继承已验Web成果的记录，后续恢复才得到正确祖先关系。祖先与整树检查需要配合使用，既发现真实缺口，也避免把内容等价的替代误判为丢失。精确提交映射见补充附录。[008-GIT](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/logs/source-checks.json)、[E03-03](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-03.json)、[E03-05](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-05.json)

产物关系仍不能替代行为验收。Graph的检查范围从跨页归并扩展到多effect取证和跨项目作用域；Spark则需要将“至少十二类”的测试对齐二十五类合同。新的检查与断言提供了后续修复输入，其价值不能由保留节点数或祖先关系推算。Spark最终发布矩阵仍为NO-GO，局部恢复不等于产品完成。[E02-01](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E02-01.json)、[E02-05](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E02-05.json)、[E02-06](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E02-06.json)、[E03-11](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-11.json)、[E03-01](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-01.json)、[E03-02](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-02.json)、[E03-06](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E03-06.json)

![图3：组织Graph的两种谱系](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p4-dag-evolution/figures/p4-fig3-artifact-lineage.png)

**图3。**组织Graph的两种谱系。上层为原生generation父子链；候选与各代的对应来自报告。下层实线是Git直接父提交，虚线仅为Git已核祖先关系，不声称直接父子；G8缺独立代码关系核验，故不补边。

### 4.3 局部补证据可以保留候选，重复建链仍需单独归因

Insight的处置记录在视觉证据不足时选择保留已有候选与技术验收，只替换视觉复验及下游交付节点，随后出现终验报告。该片段展示了局部演化的具体用途：补充缺失的验证工作，而不要求重新组织全部实现。[E04-06](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-06.json)、[E04-08](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-08.json)

同案另有重复创建五个G9节点并最终废弃的记录，其中一个节点曾进入running。参与者将其归因于读取截断，但完整工具响应和并发轨迹缺失，尚不能排除系统原因。重复事实与局部补证据因此分别保留：后继终验不证明重复有益，重复也不否定局部调整的用途。该重复片段不计入设计负效应或成本估算。[E04-03](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-03.json)、[E04-10](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-10.json)、[E04-12](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-12.json)、[E04-13](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E04-13.json)、[E04-14—18](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-evolution-cases-008.md)

### 4.4 新要求与后继合同改变了“完成”的判定范围

Spark Host中的Mac、多实例及默认本地制品服务来自用户新增要求。后续部署报告区分已部署健康与尚缺sudo授权、真实Linux和公开证书目标，新链要求复用已有部署完成余项；剩余任务最终因项目清理废弃，全矩阵验收仍未证实。新增工作和外部等待具有独立来源，不能都编码为同一合同的重复失败。[E05-06](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-06.json)、[E05-08](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-08.json)、[E05-12](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-12.json)、[E05-14](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-14.json)、[E05-24](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-24.json)、[E05-25](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-25.json)

Access则先撤回旧整页PASS，用户随后修改信息架构，新Plan在新合同下获得终验报告。新结果适用于新候选和新范围，不能追认已撤回的旧判断。两个案例共同说明，完成状态需要绑定产品范围、候选与交付层级；停止执行或合法废弃也不同于技术验收通过。[E06-01](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-01.json)、[E06-03](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-03.json)、[E06-04](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-04.json)、[E06-09](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-09.json)

### 4.5 演化通常保留历史节点，并持续改变累计结构

169个父子转移中，149次保留全部父节点，占88.2%；按转移计算的平均保留率为98.0%。每次新增节点中位数为3，四分位区间2–4；新增节点占子代累计节点的中位数为9.5%，四分位区间5.8%–16.0%。表3显示，新增节点、删除或改写边以及删除历史节点是不同的变化类型。

**表3。169个父子转移的结构变化**

| 变化类型 | 转移数 | 占169个转移 |
|---|---:|---:|
| 新增节点（可增边），不删除既有节点或边 | 60 | 35.5% |
| 新增节点并删除／改写边，保留父节点 | 83 | 49.1% |
| 存在父节点删除（可同时新增） | 20 | 11.8% |
| 节点不变，仅边改变 | 3 | 1.8% |
| 节点、边均不变 | 3 | 1.8% |

结构变化在Plan之间分布不均。记录最多的三个Plan贡献40.8%的转移；转移等权的平均新增占比为12.7%，先在Plan内平均再对20个Plan等权时为18.9%。两种汇总保留不同权重，不能把长链上的多次变化视为多个独立任务。

26个有generation的Plan中，累计节点数中位数从根记录的8增至最后记录的17.5；逐Plan最后与根的比值中位数为2.04，四分位区间1.17–3.31，范围1.00–7.79（图4）。这些指标描述历史保留与局部增量的累积，尚不能代表实际工作量、模型读取量或认知负担。

![图4：局部改图与累计增长的定量比较](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p4-dag-evolution/figures/p4-fig4-evolution-quantification.png)

**图4。**（a）169次转移的父节点保留率与边Jaccard经验累积分布；（b）边元组增删联合分布，点面积按15+10n平方点显示同坐标转移数；（c）26个Plan根／最后累计节点数，两轴以2为底，虚线是数量不变；（d）新增节点占子代比例，分别按169次转移等权和20个Plan等权。结构统计排除显式夹具，但不是无bug效果样本。

### 4.6 拓扑相同的快照仍可能记录任务状态变化

高节点保留率并不意味着边保持不变。169次转移的边Jaccard中位数为0.680，四分位区间0.500–0.833；新增边元组中位数为3［2–5］，删除边元组为1［0–3］。元组包含端点和边属性，因此属性变化也可能表现为一删一增。

共有Task字段进一步呈现了拓扑之外的变化：160次转移至少改变一个Task状态，16次改变负责人，1次改变描述（表4）。这里按转移与按Task×转移分别计数；同一转移可以改变多个字段，行数不能相加。

**表4。共有Task字段变化的两种计数单位**

| 字段 | 至少改一项的转移数／169 | Task×转移变化次数 |
|---|---:|---:|
| status | 160 | 592 |
| assignee_ref | 16 | 22 |
| description | 1 | 16 |
| title | 0 | 0 |
| delivery_contract | 0 | 0 |

三次拓扑未变的转移全部存在共有Task状态变化，分别涉及2、2、1个Task（图5）。这直接限制了将其称为“完全重复快照”的解释。状态变化可能是其它动作被快照记录的结果，并不证明改图推动了状态；同样，`delivery_contract`字段未变也不代表验收范围未变，相关要求还可能存在于消息和其它对象中。

![图5：拓扑之外的任务字段演化](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p4-dag-evolution/figures/p4-fig5-task-attribute-evolution.png)

**图5。**（a）20个有子代Plan内，至少改变一项共有Task字段的转移比例，括号为该Plan转移分母；（b）169次转移的字段变化计数；（c）三次拓扑不变记录的状态、负责人改动数。只检查列出的五个Task字段和归一拓扑，未声称完整序列化快照全字段等价；新建或删除Task不计入共有Task字段变化。

结构差分因此只能回答“记录中改变了什么”。变化是否解决交付缺口，仍需与候选产物、检查信息和适用合同连接。

### 4.7 图视图未改变候选和合同选择，原动作评分需排除分类歧义

256次请求全部返回，原严格得分两模型均96/128。候选与合同字段却均128/128，全部64次不匹配集中在D5与D8的动作标签。复核发现，D5已说明新范围写入合同，却要求revise_scope，模型均选择补验证；D8已有有效回执但未说明是否挂接，模型均选择attach_evidence，并明确旧合同不会恢复。两者都是原材料与唯一答案之间的有效性问题，不能计作DAG或模型的错误。

排除D5/D8所有条件后，两模型在六个情境中均96/96；累计／活动视图、产物联表／分表与合同联表／分表的配对差均为0，每因素48对。局部修复、新检查、补回执、合理等待、恢复缺失能力与同树复用均获得正确处置。这些是明确协议下的正面证据，反对将图历史或关系分表本身预设为必然致错；短题天花板仍不能证明真实长期策略等价。

**表5。图视图实验的原评分与材料审计**

| 分析层 | Sonnet 5.5 | Haiku 5.5 | 解释 |
|---|---:|---:|---|
| 原严格评分 | 96/128 | 96/128 | D5/D8动作分类有歧义 |
| 原候选、合同均正确 | 128/128 | 128/128 | 不能由动作标签推断产物或范围混淆 |
| 排除D5/D8全部条件 | 96/96 | 96/96 | 六情境；三因素配对差均为0 |
| 材料修复后新调用 | 32/32 | 32/32 | 事后冻结；与原分母分开 |

为检验材料解释，另固定64次新调用：D5改为范围已获授权但尚未记入合同，D8补明回执已挂接但判定尚未登记，并区分动作含义。两模型分别32/32与32/32。它只说明明确待办状态后的分类行为，不能给原歧义题补上并不存在的初始信息，也不构成DAG演化提高交付率的证据。

![图6：图视图、关系呈现与有效性审计](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p4-dag-evolution/figures/p4-fig6-controlled-mechanisms.png)

**图6。**上左显示排除D5/D8后的全条件严格得分（每格12次），上右保留八情境原评分并标注缺陷题；下部是每因素48对的比较和另行材料修复结果。D5/D8的零分代表冻结答案标签不匹配，不能作为图或产物失败。没有执行真实图编辑、代码修复或静态／动态策略对照。

## 5 机制分析与设计启示

### 5.1 根据变化的前提选择恢复形式

一次generation可能同时承载组织调整、代码变化、补充检查、新需求与外部等待。表6将其分开，要求每项效果主张对应到能够支持它的记录。

| 表6：变化因素 | 案例中可识别的内容 | 评价边界 |
|---|---|---|
| 图编辑 | Mermaid复验、Insight只补视觉证据 | 解释后续工作安排，不等于代码修复 |
| 代码改变 | 渲染队列、向导覆盖、Risk与typed-delete | 检查精确候选与行为，不由节点数代理 |
| 新检查信息 | cursor、多effect、错基线 | 可构成修复理由，不预设角色净收益 |
| 用户新要求 | Mac多实例、本地制品、Access新入口 | 保留合同变更，不能视为固定任务复试 |
| 外部条件 | sudo、真实Linux、公开证书目标 | 显式等待，不靠改图创造资源 |

本文建议在创建新图前说明发生变化的前提、预期新增产物和仍有效的成果。原任务续修适用于局部实现缺口；局部增图可以承载新增责任或独立复验；只缺证据时可保留候选并补交；缺授权或现实环境时则应显式等待。选择需要依据实际条件，不能把每次拒绝预先映射为同一种恢复流程。[E01—E06](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-evolution-cases-008.md)

拓扑不变的处置也可能记录历史整理、已有恢复链延续和等待正式环境。它们提供管理与审计信息，评价时应与产品变化分开，不必为使图“看起来推进”而创建可运行的占位任务。[008-M1](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-evolution-008.md)

### 5.2 为保留成果建立可核的产物与验收关系

图保留节点、代码继承成果和验收继续有效是三个不同判断。组织Graph的缺口与Spark的同树反例表明，单看代际顺序或单看一个SHA是否位于祖先链中都不充分。表7给出互补的证据对象。

| 表7：证据对象 | 能够核实的内容 | 仍不能推出 |
|---|---|---|
| 图及代际 | 节点、边、父代、保留或废弃决策 | 当前运行正确、代码已继承 |
| 精确候选与Git关系 | 指定祖先、完整树、文件存在 | 产品行为与完整合同已通过 |
| 验收及其撤回 | 对某候选、某合同的历史判断 | 后继所有SHA或更大范围都合格 |
| 环境回执 | 指定部署与前提是否得到确认 | 未执行平台或授权路径已通过 |

后继声明应列出保留能力、精确候选及适用合同。内容等价时说明依据，内容或范围改变时明确哪些检查可复用、哪些必须重做；环境与授权条件也应随交接保留。该建议用于约束继承主张，尚未实现为经效果验证的门禁。[008-GIT](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/logs/source-checks.json)、[E05-12](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E05-12.json)、[E06-03](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-03.json)

### 5.3 用关联重建修正结果归属

交付谱系比较中，完整单Plan证据B1已经保留本地的后继叙述，B2进一步增加明确关联端点。表8显示，新增信息可能补全候选验收、核对替代关系或补充祖先证据，原有撤回与未知发布范围仍然保留。关联的价值是说明结果属于哪个目标和合同，不能用“翻转多少失败”代替评价。[004-C](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/derived/comparisons.json)

| 表8：案例 | B1已能判断 | B2新增内容 | 不变的边界 |
|---|---|---|---|
| Insight | 旧Plan废弃且已承接新Plan | 新开发合同的后继终验报告 | 生产发布仍未知 |
| Access | 旧PASS撤回、新合同已出现 | 新候选终验及合同关联 | 旧PASS仍撤销 |
| Operation | 本地已有替代交付叙述 | 替代Task字段与归属核对 | 没有结果翻转 |
| Mermaid | 同Plan修复、PASS与集成 | 指定祖先关系补证 | 漂移后的分支tip未获旧验收 |

据此，演化记录可以以一次明确前提下的处置为单位，连接仍有效的产物、待满足合同和下一动作。界面也可分别呈现当前可执行工作与累计历史，避免用历史图大小代表当前负担。后续对照需要固定或记录任务、合同、环境和权限，比较不同恢复形式的交付与协调成本，才能评价动态图的净价值。

### 5.4 任务图是控制表示，不是全部交付状态

把完整状态分为任务图G、候选产物A、验收合同C与环境E，验收应写成对特定A在C和E下的判断，而非G的单一属性。相同任务图可以关联不同代码树、不同范围或不同环境；保留一个完成节点，不能使其对应能力自动出现在后继文件中。本轮四对构造保持G相同，分别改变能力保留、合同范围、判定撤回和真实环境证据，使正确处置不同。它们证明单看图不足以判定交付，不证明图无用或某次生产故障由图引起。

这种分层也解释了两类相反的真实证据。组织Graph案例报告后继候选缺少旧成果，说明组织继承与产物继承不能互相替代；Spark不同提交同完整树则说明，提交标识变化也不必然意味着需要从头验收。可复用性还取决于合同、环境及既有回执是否仍有效，仅有同树关系不自动恢复发布授权。图编辑、代码修复和验收更新因此需要分别记录，再说明相互关联。

### 5.5 演化的价值取决于它改变了哪一个前提

改图能够为新增义务分配工作，不能自行修复代码、制造环境权限或补出不存在的事实。已有任务中的局部缺陷可能只需续修；有效回执未关联可能只需补证；新范围可能需要更新合同和任务；真实外部请求已发出则可能需要等待。DAG evolution的候选收益是组织这些变化，其候选代价是重复恢复状态、重建责任和使旧证据失去明确归属。新generation数量无法直接度量任一项。

累计历史和活动前沿承担不同用途：前者用于审计，后者支持当前选择。本轮删去十八个无活动依赖的已完成节点细节，同时保留历史计数和当前任务，测量的是输入视图；它没有禁用真实改图，也没有运行长期恢复策略。即使两视图得分一致，也不能据此取消历史保存；即使存在差异，也不能直接将其解释为动态图净收益。

要检验“重复折腾”，必须把固定合同下的无效重试与新增范围、补事实、合法拒绝分开。更有鉴别力的后续实验应固定初始产物、工具、权限和目标，比较原任务续修、局部增图和必须新代际三种策略；保持相同检查机会，观察接受质量、回归、状态恢复成本、人工补充及未完成删失。当前的恢复选项实验只提供决定下一步的行为证据，尚未完成这项长程策略比较。

### 5.6 深层解释与尚缺的因果检验

**表9。DAG演化的竞争机制**

| 解释 | 当前证据与反证 | 未完成检验 |
|---|---|---|
| 图保存就等于成果继承 | Graph缺口与同图异产物构造反对；同树不同SHA也需保留 | 固定目标的产物继承门禁对照 |
| 关系分散导致恢复错误 | 六有效短情境均全对，未支持分表必然致错 | 增大候选与合同数量、控制长度 |
| 累计历史阻碍当前处置 | 当前48配对／模型差0；历史有审计价值 | 必要历史与无关历史交叉的长期任务 |
| 更多generation意味着修复有效 | 修复可先于改图；原任务续修也有效 | 固定代码与新信息，比较恢复策略 |
| 后继PASS改善旧结果 | Access旧结论撤回不能被新合同恢复 | 固定／变化合同分别评价，处理删失 |
| 重复改图必然无用 | 未知根因重复链已排除，补检查与新义务有用途 | 测额外义务覆盖、协调代价与人工输入 |

## 6 局限

本研究没有静态图与动态图的随机或匹配对照，不能估计成功率、token成本、耗时或认知负荷变化。案例是目的性选择，结构统计也不是无故障效果样本；累计Task、消息和generation均不能直接作为活动执行分母。[008-M1](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-evolution-008.md)

历史验收主要来自参与者报告，Git核查只支持指定祖先、文件与整树关系，未独立确认全部产品行为。部分源码读取失败，完整模型输入、部署版本与工具响应也未恢复，重复建链和错误基线的形成机制仍不明确。上述缺口不作为DAG设计负效应。[008-GIT](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/logs/source-checks.json)

合同也可能在历史过程中发生修改，当前描述不能倒推最初要求。B2不是结果真值，后继PASS不能用来比较单角色与多角色方案。全部分析尚未人工复核，谱系与演化研究相互复用案例，不构成独立重复验证。[004-M](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-lineage-004.md)

新增实验是八个固定英语短情境的交叉呈现，不是128个独立任务，也没有多次随机重复。两个顺序版本只反转记录/块顺序，冻结协议概述中的“标识映射”未实现，偏差保留在实施记录。两固定模型不代表模型总体；CLI报告的模型标识可查，平台不可见输入仍未完全审计。两组共享明确的处置规范，部分目标直接提示动作，存在较强提示与天花板边界；没有把格式错误当作内部认知或责任错误。

P4比较历史视图和证据关联呈现，没有让模型实际改图、修代码或运行验收。十八个已完成节点没有活动依赖；不能推广到历史节点携带必要约束的情况。产物和合同元数据被设置为完整可信，未测错误元数据的恢复。新独立检查与授权新范围的处置遵循实验给定分类，现实中可能由同一次图操作承载；这里的标签错误未必意味着实际调度错误。静态/动态策略、长期成本与最终交付的随机对照仍未执行。

## 7 结论

DAG演化的直接作用是组织新增义务与恢复路径，代码能力、合同有效性和环境前提需要分别核对。图保留不推出产物继承，提交变化不必然改变内容，后继PASS也不恢复撤回的旧结论。这些边界解释了为何有些局部改图有用，而另一些处置应是续修、补证或等待。

受控短题的有效情境中，两模型在累计／活动视图及联表／分表条件下均正确；原负分集中于实验动作分类歧义，已排除并另行修复复测。当前证据支持区分图、产物、合同和环境，尚未证明动态图的净交付收益或某种恢复策略更经济。
<!-- BODY_END -->

## 参考文献

[1] J. Zhang et al., “AFlow: Automating Agentic Workflow Generation,” arXiv:2410.10762v4, 2024. DOI: [10.48550/arXiv.2410.10762](https://doi.org/10.48550/arXiv.2410.10762). [核定版本](https://arxiv.org/abs/2410.10762v4).

[2] Y. Wang et al., “DyFlow: Dynamic Workflow Framework for Agentic Reasoning,” arXiv:2509.26062v1, 2025. DOI: [10.48550/arXiv.2509.26062](https://doi.org/10.48550/arXiv.2509.26062). [核定版本](https://arxiv.org/abs/2509.26062v1).

[3] S. Wang et al., “AgentConductor: Topology Evolution for Multi-Agent Competition-Level Code Generation,” arXiv:2602.17100v1, 2026. DOI: [10.48550/arXiv.2602.17100](https://doi.org/10.48550/arXiv.2602.17100). [核定版本](https://arxiv.org/abs/2602.17100v1).

[4] X. Zhao et al., “Failure as a Process: An Anatomy of CLI Coding Agent Trajectories,” arXiv:2607.09510v1, 2026. DOI: [10.48550/arXiv.2607.09510](https://doi.org/10.48550/arXiv.2607.09510). [核定版本](https://arxiv.org/abs/2607.09510v1).

[5] W. M. P. van der Aalst, “Object-Centric Process Mining: Dealing with Divergence and Convergence in Event Data,” *Software Engineering and Formal Methods*, 2019. DOI: [10.1007/978-3-030-30446-1_1](https://doi.org/10.1007/978-3-030-30446-1_1). [出版记录](https://link.springer.com/chapter/10.1007/978-3-030-30446-1_1).

[6] J. N. Adams, D. Schuster, S. Schmitz, G. Schuh, and W. M. P. van der Aalst, “Defining Cases and Variants for Object-Centric Event Data,” arXiv:2208.03235v1, 2022. DOI: [10.48550/arXiv.2208.03235](https://doi.org/10.48550/arXiv.2208.03235). [核定版本](https://arxiv.org/abs/2208.03235v1).

[7] N. F. Liu et al., “Lost in the Middle: How Language Models Use Long Contexts,” *Transactions of the Association for Computational Linguistics*, vol. 12, pp. 157–173, 2024. DOI: [10.1162/tacl_a_00638](https://doi.org/10.1162/tacl_a_00638). [出版记录](https://aclanthology.org/2024.tacl-1.9/).

## 研究声明

**数据可用性。**冻结提取、证据副本、差分和Git命令日志保存在私有run-001目录。附录为本机绝对路径；公开共享、许可及脱敏方案待作者确认，不能由这些链接推断数据已经公开。 本轮合成输入、全部模型响应、评分、反馈与图源另存于[实验附录](controlled-experiments.md)，逐次结果见[实验底表](experiment-results.csv)，修改前稿和可回放差分见[修订记录](REVISION-011.md)。

**伦理与隐私。**使用既有开发消息、稳定身份、任务及产物记录，没有新增用户实验。伦理审查状态、使用授权与外部发布范围待补；最终发表应核查敏感配置、内部路径与对话的披露权限。

**作者贡献。** 署名作者为 oopslink；具体 CRediT 贡献角色待补充。AI 不列为作者。

**利益冲突。** 待作者声明。

**资助。** 本研究未获得资助。

**AI使用。**001—009原研究及010写作阶段的AI参与记录保留；本轮011由同一AI完成实验实现、分析、写作和有界自查，未新增独立评阅。实验调用固定claude-sonnet-5-5和claude-haiku-5-5、low effort，版本标识与原始响应保存；历史部署模型和参数未知处仍不补造。模型输出不作为内部推理或训练来源真值。原研究与本轮分析均尚未人工复核；未重跑历史产品验收。

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
