# 长程任务中的计划修复与成果继承：智能体任务图演化的纠错机制及其边界

*Plan Repair and Artifact Inheritance in Long-Horizon Tasks: How Evolving Agent Task Graphs Support Correction*

**作者：** oopslink、OpenAI Codex（AI 共同作者）

**oopslink 主页：** [https://github.com/oopslink](https://github.com/oopslink)

**研究类型：** 部署案例研究与受控行为实验。

## 中文摘要

长程软件任务要求系统根据执行反馈修订计划，同时保留仍有效的成果。本文重建 Agent Center 中 26 个 Plan 的 195 份任务图快照，并将 169 次父子转移与任务状态、Git 谱系和验收记录对照。166 次拓扑改变中，101 次同时出现共有任务转为完成；3 次拓扑不变的转移中也有 1 次出现这一状态变化。案例表明，保留合格候选、只补充缺失的视觉复验可以推进交付；保留已完成的任务节点却不保证后继代码继承了成果。另一恢复链重复创建的 5 个节点最终全部废弃。DAG 演化通过重组未完成工作支持反馈纠错，但不能替代代码修复或独立验收。改图前应核对当前候选、验收合同与已有恢复任务，只为变化的义务修订依赖；对缺少新证据的重复状态应设置停止条件。有限重复的记录不足以判定无限循环或其技术根因。

**关键词：** DAG 演化；长程任务；反馈纠正；重复规划；产物继承

## Abstract

Long-horizon software tasks require plans to respond to feedback without discarding validated work. We reconstructed 195 task-graph snapshots across 26 plans in Agent Center and compared 169 parent–child transitions with task states, Git ancestry, and acceptance records. Of 166 topology changes, 101 coincided with a shared task becoming complete; one of three unchanged topologies did so as well. Adding a missing visual check while retaining a valid candidate supported delivery. Conversely, preserving completed nodes did not guarantee that later code inherited their artifacts, and five duplicate recovery nodes were eventually discarded. DAG evolution supports correction by reorganizing outstanding obligations; it does not itself repair code or validate a candidate. A planner should compare the candidate, contract, and active recovery work before changing dependencies, and stop recreating work when no relevant state has changed. The observed finite repetition does not establish an infinite loop or its technical cause.

**Keywords:** DAG evolution; long-horizon tasks; feedback-driven correction; repeated planning; artifact inheritance

<!-- BODY_START -->
## 1 引言

开发任务的计划会被执行反馈改变：实现可能暴露缺陷，验收可能要求补证，用户也可能增加范围。Agent Center用有向无环图（DAG）组织任务，并以generation记录演化。这个机制为重新安排工作提供了载体，也带来一个评价困难：后继任务通过，究竟是改图发挥了作用，还是代码修复、人工补充信息或验收范围改变带来的结果？仅统计代际增长，无法区分这些变化。[008-M1](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-evolution-008.md)

**研究问题是：DAG 演化怎样利用执行和验收反馈推进长程任务、纠正原计划？为什么同一机制有时会反复改图而不推进，怎样避免？** 推进是原目标下尚未满足的义务得到执行或独立复验；自我纠正是工作流根据反馈改变后续工作并检查结果。反馈可以来自工具、Agent 或人，本文分别追踪来源。“自我纠正”描述流程能修正原路径，不预设系统可以独立发现和修复所有问题。

成果继承揭示了这种区分的必要性。历史图可以保留已完成节点，后继代码却从缺少相应能力的基线构建；两个不同提交标识也可以对应相同文件树。相似地，新范围下的终验报告与旧范围下撤回的PASS可以同时成立。研究因此同时追踪任务图、候选产物、验收合同和环境，并比较局部改图、原任务续修和其它恢复形式。[008-GIT](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/logs/source-checks.json)

结论是：**当反馈改变了剩余工作的内容或依赖关系时，DAG 演化可以有效组织长程推进与自我纠正。** 对已有候选只增补缺失的复验，收益在于后续工作有对象、有执行顺序，也能保留已经成立的结果。若候选、验收要求、外部条件和待办义务均未变，复制同一条恢复链并不构成进展。两类情况应按实际执行与验收分别验证，不能用代际数代替。

机制分析对照三种处置：保留产物、局部改图以安排新工作；在原任务修已有缺陷；以及重复创建没有新义务的工作。产物来自哪个基线、谁补充了信息、验收范围是否改变，均独立于图的代际记录核对。比较的对象是这些具体处置，不是动态图和静态图的总体胜率。

## 2 相关研究与分析框架

### 2.1 工作流优化与持续交付谱系的不同问题

AFlow以任务及评价函数搜索代码表示的工作流，其附录也包含开放任务；DyFlow与AgentConductor分别研究动态组织和按反馈演化。[1]<!--ref:R11-->[2]<!--ref:R28-->[3]<!--ref:R29--> 因此，本文不能以“已有研究无法处理开放任务”建立空白。这里的区别是评价对象：优化任务评价函数中的流程，与重建持续生产交付中哪个目标、产物和验收版本仍然有效，并不是同一个问题。

过程失败研究要求保留起点、演进与恢复；[4]<!--ref:R05--> 对象中心过程研究则说明，多个相互关联对象不总能由单一case标识充分表达。[5]<!--ref:R26-->[6]<!--ref:R27--> 这些已有思想为关联Plan、generation、Task、候选与合同提供分析依据。本文不提出新的过程挖掘算法，局部补充是说明仅按Plan或图代际判断，怎样误读成果继承与验收撤回。

信息呈现仍可能影响处置，但长上下文研究的发现不能替代实际改图对照。[7]<!--ref:R32--> 本文保留图视图实验作为显式规则下的行为探针；对DAG用途的判断主要依靠改图、代码变化、补充证据及合同变化的分别重建，而不将短题准确率解释为演化收益。

### 2.2 计划修复：哪些依赖与成果仍然有效

Kambhampati 与 Hendler 从维持既有步骤有效的条件来解释计划修改与复用：情境改变后，需要定位失效的关系，而非无条件重建全部计划。[8]<!--ref:KH92--> 在软件交付中，加一道复验可以修复工作安排，但无法补回旧代码基线缺失的能力。本文将理论问题转为两个可独立推翻的判断：新安排是否覆盖反馈揭示的缺口，保留的候选是否仍具备先前已经验证的成果。只要第二项不成立，保留全部父节点也不是成功复用。若两项均未改变，再复制相同恢复任务便有跨代空转风险。

本文将一次交付状态分为工作图、候选产物、验收合同与环境。图定义工作及其依赖；产物记录实现内容；合同规定判断标准；环境决定检查适用范围。改图直接改变第一项，其它状态只有经相应操作和证据核验才随之改变。因此，新增generation、保留旧节点或后继PASS都不是独立的交付进展指标。

过程命题是：**图演化只有在组织了变化后的工作义务时才提供明确的协调用途，成果继承和验收沿用则依赖另外的关系。** §4.1检查改图与代码修复的时间和功能差别，§4.2检查节点与Git产物是否共同继承，§4.3–4.4检查补证及合同改变。若既有候选仍有效且只缺回执，补证即可；若外部条件未满足，合法等待是反例，不能用新建节点代替。

这一分析不是动态改图优于静态流程的理论证明。结构盘点描述图怎样变化，过程案例解释某次变化承担什么工作，单步图视图实验评价给定规则下的选择；三者的结果变量不合并。

## 3 材料与方法

### 3.1 研究设计与分析单位

本研究采用单部署的描述性结构分析、机制案例对读和受控处置实验。主分析单位是一次恢复或适应性处置：给定未满足目标，判断其改变了哪些义务、产物、合同或环境条件。结构分析以相邻generation快照的父子转移为单位；合成实验以固定状态下的一次处置回答为单位。三个单位分别用于描述演化、解释用途和比较表示，不作为可互换的成功率分母。

结构资料覆盖108个Plan、205条generation；排除六个显式夹具后，102个Plan中26个保存了195条generation，其中20个有子代，形成169个父子转移（表1）。案例采用目的性选择，包含六个主案例及两个混合排除案例，以覆盖局部复验、原任务续修、产物缺失与等价、合同变化和外部等待。另用交付谱系研究的六组比较、13个合同片段和27个事件分析单Plan视图与明确关联视图的差异，重合记录不重复计样本。[008-M3](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/reading/coverage.json)、[004-M](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-lineage-004.md)

**表1。Plan与generation记录范围**

| 范围 | Plan数 | 有generation的Plan | generation总数 | 根记录 | 拓扑改变子代 | 无拓扑改变子代 |
|---|---:|---:|---:|---:|---:|---:|
| 全部冻结记录 | 108 | 32 | 205 | 32 | 170 | 3 |
| 显式夹具 | 6 | 6 | 10 | 6 | 4 | 0 |
| 排除显式夹具 | 102 | 26 | 195 | 26 | 166 | 3 |

26个有记录Plan的generation数中位数为4.5，范围1–33（图1）。其余76个Plan没有这类记录，不能据此判断从未发生其它图编辑。这里的数量描述保存的演化历史，不是失败或恢复次数。

![图1：非显式夹具Plan的代际记录分布](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p4-dag-evolution/figures/p4-fig1-generation-distribution.png)

**图1。**26个非显式夹具Plan按generation记录数排序，每行保留同一Plan的（a）记录数，（b）根与最后累计节点数，（c）根与最后seq依赖深度。空心圆为根、实心菱形为最后记录；节点轴为以2为底的对数，重合表示数值相同。合计195条记录，G编号见补充映射；不表示无bug样本或成功终局。

### 3.2 四类状态、比较关系与结果判据

每个案例分开核对任务图G、候选产物A、验收合同C和环境E。图记录用于确认任务与依赖如何改变；精确Git对象用于检查祖先、文件存在和完整树等价；消息与验收记录用于恢复当时的要求、判定及撤回；环境回执用于判断授权与部署前提是否满足。参与者声称的修复或通过始终与原生状态和独立产物核查区分。

评价依次回答三件事：图操作是否为改变后的目标增加或重排了义务和依赖；仍有效的成果是否进入后继候选；已有验收是否仍适用于当前候选、合同与环境。将图编辑与代码变化、新检查信息、人工新要求、外部条件分别识别后，再检查它们的时间与对象关系。先于改图的修复不能归因于后发生的图操作；后继合同通过也不能追认已撤回的旧合同。这个程序识别具体功能与替代解释，不估计净因果收益。

比较保留相反方向的材料：局部增图与原任务续修相对照，产物丢失与不同提交同树相对照，新合同通过与旧结论撤回同时保留。产品代码缺陷属于正常修复输入；harness故障、读取截断及未知运行根因不承担DAG设计的负效应。重复记录仍保存，但不因其看似低效就计算为无效改图。全部分析尚未人工复核。[008-X](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-evolution-cases-008.md)

### 3.3 结构统计与交付谱系比较

结构统计比较父子快照的节点集合、归一边元组及共有Task字段，报告父节点保留率、边Jaccard、新增占比与字段变化。既按169次转移等权，也先在各Plan内汇总再对20个有子代Plan等权，以显示长链对总体的影响。累计图包含已完成或废弃节点，所以这些指标描述历史组织，不能充当活动工作量或模型负担。

交付谱系比较固定同一目标的本地证据，再加入有明确承接、替代或验收关系的端点。比较结果是新增关联如何补全候选、合同和验收归属，而不是后继记录能否把所有旧失败改判为成功。公式、原生标识和Git检查命令保存在[方法与取证附录](argument-methods-015.md)及[证据附录](evidence-details.md)。

### 3.4 图视图与证据关联的受控比较

实验包含八种恢复状态：已有代码缺陷、新独立检查、已有回执待挂接、外部资源等待、授权新范围、能力丢失后的产物恢复、同树不同提交和替代合同通过。交叉三个二水平因素：累计或活动图视图、产物关系联表或分表、合同关系联表或分表。累计视图展开18个无活动依赖的已完成节点；活动视图保留其计数和无活动依赖事实。两种视图的当前义务相同，联表和分表也保留相同事实，只改变呈现与关联方式。

两个固定模型各接受8情境×8条件×2顺序版本，共128次响应，合计256次；使用Sonnet/Haiku 5.5、low effort及隔离调用环境。输入与评分在响应生成前固定，分别核对动作、候选和合同。模型没有实际改图、修改代码或执行验收，因此本实验检验表示下的处置选择，不是静态图与动态图的交付策略对照。

输出后的有效性审计发现两题的材料与唯一动作答案存在歧义。两题全部条件从负面归因中排除，原分数保持；另固定其全部条件、顺序和模型开展64次事后材料修复复测，与主分母分开。四对同图而产物、合同、判定或环境不同的状态另作分析性反例，检验单凭图是否足以判断交付，不计为生产事故或模型错误。完整运行、偏差与答案审计见[实验附录](controlled-experiments.md)。

### 3.5 构念检查与可复现范围

全部256个主输入直接规定图历史、产物和验收范围分离，指定必要成果丢失时恢复基线，并声明关闭历史没有当前执行依赖。动作选择实验据此测规则已明示后的选择，未检验这些原则能否被自主发现，也未操纵真实静态／动态图策略。D5/D8的动作分类重叠涉及2/8情境，全部64个输出保留为材料问题；修复后结果属于事后诊断而非独立确认。

本文按软件工程案例研究区分情境解释与因果识别，[9]<!--ref:RV16_METHOD--> 案例为目的性选择，研究问题和分析框架经材料阅读后调整，未预注册，也未完成全库盲法编码。输入、评分和统计可复算，不等于题目具有充分内容效度；设计、答案与解释同由AI形成，现有分工审阅不能替代独立人工判定。模型响应是固定情境的交叉观测，不按独立生产任务计算置信区间或显著性。

[构念、排除标准与反证](../research-validity-016/README.md)及[图表所支持的主张](../research-validity-016/figure-claim-map.md)列明各项分析的判据与推断范围。

### 3.6 拓扑与状态的联合比较

剔除六个显式夹具，用相同 Plan 的父子快照核对 169 次转移。分别标记节点／边集合是否改变、父子共有任务是否改状态、是否有共有任务从其他状态进入完成。新建任务不纳入“共有任务”；一个父子区间可含多个真实动作，故不能把状态变化归因于改图。另逐次留出一个 Plan 重算父节点保留率，并重放十项只读 Git 检查，核对案例中的提交祖先、文件存在性及文件树等价。[逐次转移](../mechanisms-020/p4-transitions.csv) · [Git 核对](../mechanisms-020/git-replay.json)。

## 4 研究发现

### 4.1 图编辑组织后续工作，代码修复可以独立发生

Mermaid案例中，真实浏览器检查暴露多图渲染缺口，开发成员随后报告修复候选已推送，之后才出现增加修复与复验节点的generation，最后对同一候选报告复验通过。表2显示修复报告早于改图，故改图在这里能够表达正式后续路径，不能解释为先产生了代码修复。

| 表2：Mermaid 案例的处置阶段（按发生顺序） | 记录事件 | 对解释的约束 |
|---|---|---|
| 浏览器验收 | 浏览器缺陷REJECT，E01-01 | 新检查暴露具体产品缺口 |
| 修复候选推送 | 修复候选6855b0bd已推送，E01-02 | 修复报告已经存在 |
| 任务图修订 | 新generation补修复与复验节点，E01-04 | 晚于修复报告46.486869秒 |
| 同一候选复验 | 同一候选复验报告PASS，E01-03 | 只覆盖该候选与检查范围 |

图2呈现了新增两个节点和一条依赖边的局部调整。报告时间用于确定记录顺序，不等于实际编码完成时间；同案中旧集成在拒绝后变为ready的调度故障，继续排除设计负效应归因。[E01-01](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-01.json)、[E01-02](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-02.json)、[E01-04](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-04.json)、[E01-03](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E01-03.json)

Access提供了另一种恢复路径。局部候选因缺Risk字段与删除确认问题被拒后，负责人要求在原分支追加修正，后续报告同一Task通过；原生Plan只保存一个初始generation和六个任务。这说明一次拒绝可以在原任务中继续处理，不必机械地触发新代。两个案例共同把选择条件指向责任、依赖和验收路径是否发生变化，而非是否出现REJECT标签。[E06-07](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-07.json)、[E06-08](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-08.json)、[E06-10](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/evidence/E06-10.json)

![图2：Mermaid的真实图增量与事件顺序](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p4-dag-evolution/figures/p4-fig2-mermaid-delta.png)

**图2。**Mermaid的真实图增量与事件顺序。generation-384637ec相对父代增加修复、复验两个节点及一条依赖边，累计由3节点2边变为5节点3边。记录中没有从复验到原集成的依赖边；下方事件按顺序排列、不按时长比例。修复报告比改图早46.486869秒。

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

### 4.5 大部分演化保留历史，代际增长不等于交付进展

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

### 4.7 充分事实下，两种图视图与关联呈现均支持正确处置

在六个材料与规范答案可判定的情境中，两模型均96/96正确；累计／活动视图、产物联表／分表和合同联表／分表的配对差均为0，每因素48对。模型能在所给规则下区分局部续修、新检查、补回执、合法等待、恢复能力和同树复用。该正面结果说明，关系分表或保留历史节点本身不必然导致错误；它没有比较真实长期演化策略。

全部 256 次响应按预设答案评分，两模型严格得分均96/128，但候选与合同字段均128/128，全部64次动作不匹配集中于D5和D8。材料审计将其识别为动作分类歧义，故不作为DAG或模型的负面证据。另行材料修复后，两模型各32/32匹配；事后诊断与预设条件分别计分。歧义情境的定义和输入差异见[方法附录](argument-methods-015.md)。

**表5。图视图实验的严格评分与歧义情境分析**

| 分析层 | Sonnet 5.5 | Haiku 5.5 | 解释 |
|---|---:|---:|---|
| 原严格评分 | 96/128 | 96/128 | D5/D8动作分类有歧义 |
| 原候选、合同均正确 | 128/128 | 128/128 | 不能由动作标签推断产物或范围混淆 |
| 排除D5/D8全部条件 | 96/96 | 96/96 | 六情境；三因素配对差均为0 |
| 材料修复后新调用 | 32/32 | 32/32 | 事后冻结；与原分母分开 |

为检验任务材料的歧义，事后复测包含 64 次调用：D5改为范围已获授权但尚未记入合同，D8补明回执已挂接但判定尚未登记，并区分动作含义。两模型分别32/32与32/32。它只说明明确待办状态后的分类行为，不能给原歧义题补上并不存在的初始信息，也不构成DAG演化提高交付率的证据。

![图6：图视图、关系呈现与有效性审计](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p4-dag-evolution/figures/p4-fig6-controlled-mechanisms.png)

**图6。**上左显示排除D5/D8后的全条件严格得分（每格12次），上右呈现八个情境的严格评分并标注缺陷题；下部是每因素48对的比较和另行材料修复结果。D5/D8的零分代表冻结答案标签不匹配，不能作为图或产物失败。没有执行真实图编辑、代码修复或静态／动态策略对照。

### 4.8 改图、任务状态推进和成果继承是三种不同事件

**表5a。父子快照中，图拓扑与共有任务的状态变化。** 分母为转移，不是任务数；后两列可重叠。完成是任务字段值，不是重新核验过的交付。

| 转移类型 | 次数 | 至少一项共有任务改变状态 | 至少一项共有任务转为完成 |
|---|---:|---:|---:|
| 节点或依赖改变 | 166 | 157 | 101 |
| 节点与依赖均未改变 | 3 | 3 | 1 |

只数改图次数会把两类现象混在一起：166 次拓扑改变中有 65 次没有共有任务转为完成，其中可以包含安排未来工作；3 次拓扑相同则都记录了状态变化。反过来，只保留旧节点也不是产物继承：逐一留出 20 个 Plan，保留全部父节点的比例仍在 85.4%–91.1% 之间，但重放 Git 检查仍确认组织 Graph 的 G5 并非 G6 的祖先，G5 中已验证的后端文件在 G6 缺失，G7 才重新包含 G5 祖先。结构统计说明调度器常保留工作历史；祖先与文件核对才能判断具体成果是否随候选保留。

由此可区分两种外观相似的重复。Insight 的视觉复验沿用已有候选，补上新的验收义务；重复创建的另一组 G9 节点却未留下新交付证据，五个节点最终废弃。防循环应比较活动恢复任务、候选、合同版本与未满足的检查；若这些都未改变，先复用现有工作。仅凭拓扑不变就停止，会误拦仍在推进的任务。此处提出的是停止条件的设计依据，不是现场策略对比或无限循环的发生率。[逐 Plan 敏感性](../mechanisms-020/p4-leave-one-plan-out.csv)。

## 5 计划修复、成果继承与跨代重复

### 5.1 以改变后的义务选择恢复形式

改图、代码改变、新检查、用户要求与环境变化可以共同出现在一次恢复中，但它们改变的对象不同（表6）。新增独立检查或责任交接可以需要新的任务及依赖；既有任务的实现缺口可以直接续修；已有有效回执只需补充关联；已经发出的外部请求则可能需要等待。这样的选择依据是剩余义务发生了什么变化，而不是每个拒绝都必须新建代际。

| 表6：变化因素 | 案例中可识别的内容 | 评价边界 |
|---|---|---|
| 图编辑 | Mermaid复验、Insight只补视觉证据 | 解释后续工作安排，不等于代码修复 |
| 代码改变 | 渲染队列、向导覆盖、Risk与typed-delete | 检查精确候选与行为，不由节点数代理 |
| 新检查信息 | cursor、多effect、错基线 | 可构成修复理由，不预设角色净收益 |
| 用户新要求 | Mac多实例、本地制品、Access新入口 | 保留合同变更，不能视为固定任务复试 |
| 外部条件 | sudo、真实Linux、公开证书目标 | 显式等待，不靠改图创造资源 |

Mermaid的修复报告先于改图，Access在原任务续修，Insight保留候选而只补视觉证据，分别说明组织恢复不等同于代码修复、换代也不是恢复的唯一形式。未知根因的重复G9链仍被排除，不能用它计算重复改图的代价。要估计重复规划的代价，还需在固定合同、初始产物及检查机会下，比较续修、局部改图与强制换代策略，而不是用代际数量代替效果。[E01—E06](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-evolution-cases-008.md)

### 5.2 将任务图与产物、合同和环境显式关联

任务图G描述义务组织，产物A承载实际能力，合同C规定通过条件，环境E限定验证前提。验收是对指定A在C和E下的判断，不是G的单一属性。四对同图构造只改变其中一项非图状态便产生不同正确处置，说明图信息不能独立判定交付；真实案例则展示这些关系如何脱节。

| 表7：证据对象 | 能够核实的内容 | 仍不能推出 |
|---|---|---|
| 图及代际 | 节点、边、父代、保留或废弃决策 | 当前运行正确、代码已继承 |
| 精确候选与Git关系 | 指定祖先、完整树、文件存在 | 产品行为与完整合同已通过 |
| 验收及其撤回 | 对某候选、某合同的历史判断 | 后继所有SHA或更大范围都合格 |
| 环境回执 | 指定部署与前提是否得到确认 | 未执行平台或授权路径已通过 |

后继声明应列出保留能力、精确候选和适用合同。祖先关系可以检查指定变更是否进入历史，完整树可以识别不同提交的内容等价，两者都不能替代行为验收。范围、产物或环境变化时，应标明哪些旧检查仍有效、哪些必须重做。该建议约束继承主张，目前尚未作为门禁进行效果比较。[008-GIT](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/logs/source-checks.json)

### 5.3 以关联谱系解释结果归属

单Plan证据可能已经记录后继交付的叙述，增加关联端点的价值在于核对其候选、合同与验收对象。表8比较单一 Plan 证据视图 B1 与跨 Plan 关联视图 B2：后者能识别部分后继验收及其对应产物，同时保留旧判断撤回及发布未知。关联重建不是将旧失败翻转成成功的规则。[004-C](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/derived/comparisons.json)

| 表8：案例 | B1已能判断 | B2新增内容 | 不变的边界 |
|---|---|---|---|
| Insight | 旧Plan废弃且已承接新Plan | 新开发合同的后继终验报告 | 生产发布仍未知 |
| Access | 旧PASS撤回、新合同已出现 | 新候选终验及合同关联 | 旧PASS仍撤销 |
| Operation | 本地已有替代交付叙述 | 替代Task字段与归属核对 | 没有结果翻转 |
| Mermaid | 同Plan修复、PASS与集成 | 指定祖先关系补证 | 漂移后的分支tip未获旧验收 |

当前可执行工作与累计历史也应分别呈现。前者支持下一步选择，后者承担审计和承接解释。三次拓扑不变仍有Task状态变化，说明组织记录可以有用途而不改变代码；它们既不自动构成重复劳动，也不证明修复取得进展。受控实验只改变十八个无活动依赖历史节点的呈现，不能据其零差异取消历史保存或判定真实动态图没有成本。

### 5.4 依据功能边界评价演化，而非追求更多代际

表9将当前支持的功能解释与尚缺的策略检验分开。设计上值得保留的是对新增工作、局部复验和合法承接的表达能力；应避免的是将图更新当作产物修复或验收通过的替代物。未来若比较不同恢复策略，应同时记录合格验收、成果回归、状态重建、人工补充和未完成任务，而不能只比较最终PASS或图大小。

**表9。DAG演化的竞争机制**

| 解释 | 当前证据与反证 | 未完成检验 |
|---|---|---|
| 图保存就等于成果继承 | Graph缺口与同图异产物构造反对；同树不同SHA也需保留 | 固定目标的产物继承门禁对照 |
| 关系分散导致恢复错误 | 六有效短情境均全对，未支持分表必然致错 | 增大候选与合同数量、控制长度 |
| 累计历史阻碍当前处置 | 当前48配对／模型差0；历史有审计价值 | 必要历史与无关历史交叉的长期任务 |
| 更多generation意味着修复有效 | 修复可先于改图；原任务续修也有效 | 固定代码与新信息，比较恢复策略 |
| 后继PASS改善旧结果 | Access旧结论撤回不能被新合同恢复 | 固定／变化合同分别评价，处理删失 |
| 重复改图必然无用 | 未知根因重复链已排除，补检查与新义务有用途 | 测额外义务覆盖、协调代价与人工输入 |

### 5.5 单代无环为何仍可能出现跨代重复

每代的 DAG 无环，约束的是该版本中任务之间的依赖。若验收未通过便触发重规划，规划器可能反复生成相同的恢复链。如果每次都复制同一缺口、同一候选和同一检查，这个循环发生在多次规划之间，任何一张图上的环检测都发现不了。判别它的依据是相邻处置有没有改变交付状态，而非仅比较两代图的拓扑相似度。

Insight 展示了两条不同路径。一条保留已有候选与技术验收，只替换缺失的视觉验证及其下游工作；后续出现终验报告。改图据此指定了缺失的检查和后续交付条件。另一条在已有 G9 的情况下又创建五个节点，其中一个进入运行，五个最终全部废弃，未见这组重复节点带来新的交付证据。记录证明有限重复，不证明会无限运行；“读到的计划被截断”只是参与者解释，不能把潜在系统 bug 或这五个节点计作架构导致的损失。（§4.3；[原 G9 与重复 G9 证据](evidence-details.md)）

可从三个位置检查循环成因。其一，系统是否把新代号当作新进展，未识别已有的活动恢复任务；Insight 与此解释相容，但原因未确定。其二，后继是否从错误的基线重新引入已修缺陷；组织 Graph 的 G6 保留了 G5 节点却缺失相应代码，说明这种退步可以用 Git 核对，但该例没有证明反复退步。其三，失败是否来自未改变的外部前提；此时重复规划不能创造资源。若用户确实改变了验收范围，则应记录新合同，不能将它误报为循环。

## 6 局部改图与重复规划的停止条件

### 6.1 根据义务与产物决定是否改图

据此提出一条尚待验证的停止规则：改图前读取当前活动版本，核对目标和验收版本、候选文件树、未满足的检查及外部前提。若已有任务处理同一缺口，继续或复用它；仅当出现新义务或依赖变化时，才局部改图并指定下次验收。连续返回同一状态且没有新证据时，停止自动重建，标明待变化的条件或交给有权限的人决断。该规则旨在限制重复建链，同时允许在同一候选上补一次真正缺失的检查。

### 6.2 证据边界

本研究没有静态图与动态图的随机或匹配对照，无法估计净成功率、耗时、token成本或认知负荷。目的性案例适合辨认功能与反例，不能给出无效改图发生率；结构统计保留运行恢复与未知根因，也不是无故障效果样本。[008-M1](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-evolution-008.md)

历史验收主要来自参与者报告，Git核查只确认指定对象的祖先、文件和完整树关系，未独立重跑产品行为。完整模型输入、历史部署和工具响应仍有缺口，错误基线与重复建链的形成原因尚未确定，不能据此将失败归因于 DAG 的设计。合同可能随时间变化，关联视图也不是真值；后继PASS的适用范围必须保留。[008-GIT](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/logs/source-checks.json)、[004-M](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-lineage-004.md)

受控实验是八个固定短情境，两种顺序呈现不是随机重复，两个固定模型不代表模型总体。明确处置规范、完整可信元数据及无活动依赖的历史节点形成较强提示和天花板边界；实验未测错误元数据、必要历史遗漏或长期状态恢复。D5/D8的材料歧义及64次事后复测分开保留。执行环境、未实现的标识映射与评分细节见附录，不据格式或分类不匹配推断内部认知原因。

全部研究尚未人工复核；结构、谱系及其它论文复用同一部署与部分案例，不构成独立重复验证。本文给出的是改图的功能边界及评价条件，尚未确定哪种恢复策略最经济。

## 7 结论

DAG 演化能把执行反馈转化为长程任务中可执行、可验收的后续工作。Insight 案例保留已有候选和有效的技术验收，只重新安排缺失的视觉复验；Mermaid 案例则先产生代码修复，任务图随后组织复验。改图在两例中均支持了流程纠正，但对代码修复、人工补充信息和验收范围变化仍须分别归因。（§4.1、§4.3）

每代任务图无环，并不保证跨代规划持续推进。169 次转移中，149 次保留全部父节点，仍有后继候选从旧基线构建而丢失成果的实例。另有 5 个重复恢复节点最终全部废弃。上述记录证明了成果继承可能中断、规划可能有限重复，并未证明无限循环或确定重复的技术根因。

局部改图适用于反馈改变了待办义务或依赖的情形。系统应核对候选、验收合同、剩余缺口和已有恢复任务；同一缺口已有工作承接时应复用，在外部前提未变且无新证据时停止重复建链。案例支持这种处置原则，但不能估计其相对静态计划的平均净收益，停止策略本身仍需实地检验。

<!-- BODY_END -->

## 参考文献

[1] J. Zhang et al., “AFlow: Automating Agentic Workflow Generation,” arXiv:2410.10762v4, 2024. DOI: [10.48550/arXiv.2410.10762](https://doi.org/10.48550/arXiv.2410.10762). [核定版本](https://arxiv.org/abs/2410.10762v4).

[2] Y. Wang et al., “DyFlow: Dynamic Workflow Framework for Agentic Reasoning,” arXiv:2509.26062v1, 2025. DOI: [10.48550/arXiv.2509.26062](https://doi.org/10.48550/arXiv.2509.26062). [核定版本](https://arxiv.org/abs/2509.26062v1).

[3] S. Wang et al., “AgentConductor: Topology Evolution for Multi-Agent Competition-Level Code Generation,” arXiv:2602.17100v1, 2026. DOI: [10.48550/arXiv.2602.17100](https://doi.org/10.48550/arXiv.2602.17100). [核定版本](https://arxiv.org/abs/2602.17100v1).

[4] X. Zhao et al., “Failure as a Process: An Anatomy of CLI Coding Agent Trajectories,” arXiv:2607.09510v1, 2026. DOI: [10.48550/arXiv.2607.09510](https://doi.org/10.48550/arXiv.2607.09510). [核定版本](https://arxiv.org/abs/2607.09510v1).

[5] W. M. P. van der Aalst, “Object-Centric Process Mining: Dealing with Divergence and Convergence in Event Data,” *Software Engineering and Formal Methods*, 2019. DOI: [10.1007/978-3-030-30446-1_1](https://doi.org/10.1007/978-3-030-30446-1_1). [出版记录](https://link.springer.com/chapter/10.1007/978-3-030-30446-1_1).

[6] J. N. Adams, D. Schuster, S. Schmitz, G. Schuh, and W. M. P. van der Aalst, “Defining Cases and Variants for Object-Centric Event Data,” arXiv:2208.03235v1, 2022. DOI: [10.48550/arXiv.2208.03235](https://doi.org/10.48550/arXiv.2208.03235). [核定版本](https://arxiv.org/abs/2208.03235v1).

[7] N. F. Liu et al., “Lost in the Middle: How Language Models Use Long Contexts,” *Transactions of the Association for Computational Linguistics*, vol. 12, pp. 157–173, 2024. DOI: [10.1162/tacl_a_00638](https://doi.org/10.1162/tacl_a_00638). [出版记录](https://aclanthology.org/2024.tacl-1.9/).

[8] S. Kambhampati and J. A. Hendler, “A validation-structure-based theory of plan modification and reuse,” *Artificial Intelligence*, vol. 55, nos. 2–3, pp. 193–258, 1992. [原文](https://www.sciencedirect.com/science/article/pii/0004370292900564). DOI: 10.1016/0004-3702(92)90056-4.

[9] P. Runeson and M. Höst, “Guidelines for conducting and reporting case study research in software engineering,” *Empirical Software Engineering*, vol. 14, pp. 131–164, 2009. doi: [10.1007/s10664-008-9102-8](https://doi.org/10.1007/s10664-008-9102-8).

## 研究声明

**数据可用性。** [动作选择实验](controlled-experiments.md)、[逐次评分](experiment-results.csv)、[父子转移数据](../mechanisms-020/p4-transitions.csv)和[Git 对象核验](../mechanisms-020/git-replay.json)给出经验分析依据。Mermaid 案例的精确时序见[事件时间表](../case-timestamps.md)。 部署原件保存在授权本地档案；开放许可和脱敏范围尚未确定。

**伦理与隐私。**使用既有开发消息、稳定身份、任务及产物记录，没有新增用户实验。伦理审查状态、使用授权与外部发布范围待补；最终发表应核查敏感配置、内部路径与对话的披露权限。

**作者贡献。** oopslink：提出 DAG 演化如何推进任务及何时反复改图的研究问题，并提供系统背景。OpenAI Codex（AI 共同作者）：重建代际、产物和验收谱系，复核 Git 关系并分析重复恢复案例，撰写并修订文稿。

**利益冲突。** 待作者声明。

**资助。** 本研究未获得资助。

**AI 使用与复核。** AI 共同作者参与取证、实验分析与写作。模型响应是行为记录，不能直接揭示其内部推理。研究尚未经过独立人工复核。

**同源材料。** Mermaid、Access 及部分角色检查记录亦用于工作流与身份研究。同一部署记录只计为一项证据，跨篇引用不构成独立重复验证。

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
