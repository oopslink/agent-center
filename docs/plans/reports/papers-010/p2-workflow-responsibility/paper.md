# 多智能体工作流中的协调失效与责任分配

*Coordination Failures and Responsibility Allocation in Multi-Agent Workflows*

**作者：** oopslink

**作者主页：** [https://github.com/oopslink](https://github.com/oopslink)

## 中文摘要

多Agent工作流可以具有明确角色，却没有把当前动作、对象和授权连接到可行动主体。本文结合Agent Center冻结案例与256次受控短题响应，区分信息缺失、关系联结、局部验收及流程活性。历史案例中，用户补入分配主体与分支阶段，下游核查纠正重复建分支的错误前提；独立验收和合格NO-GO报告保留为正面证据。实验交叉等价gate表达、责任联表与无关记录，原严格得分为Sonnet 107/128、Haiku 102/128，但审计发现两题存在状态矛盾或合同缺失，不能用于模型负面归因。排除两题全部条件后得分为91/96与96/96，前者剩余差异均为格式；事后内嵌JSON诊断的字段均96/96。另冻结64次材料修复复测，与原结果分开保存。本文将深层问题收敛为行动关系和剩余义务是否完备，而未确认复杂度的语义致错效应、团队停摆或训练原因。全部分析尚未人工复核。

**关键词：**多 Agent 协作；责任闭合；工作流；独立验收；agent harness

## Abstract

Agent workflows may define clear roles without binding the next action, object, and authorization to an actionable owner. This study combines frozen Agent Center cases with 256 controlled short-task responses to distinguish missing information, relational inference, local acceptance, and workflow liveness. Historical interventions supplied assignment ownership and branch-stage mappings, while downstream fact checking prevented redundant branch creation. Independent verification and valid NO-GO reports remain positive evidence. The experiment crossed equivalent gate representations, responsibility-table joins, and unrelated records. Frozen strict scores were 107/128 for Sonnet and 102/128 for Haiku, but a validity audit found contradictory status or unspecified contracts in two scenarios. Excluding all conditions of those scenarios yielded 91/96 and 96/96; the remaining Sonnet discrepancies were formatting failures, with correct fields in all 96 embedded JSON objects in a post-hoc diagnostic. A separately frozen 64-call repair study is retained apart from the primary denominator. The analysis locates plausible mechanisms in incomplete action relations and untransferred obligations, without establishing semantic effects of complexity, team deadlock, or training origins. The work has not undergone human verification.

**Keywords:** multi-agent collaboration; accountability; workflow; independent review; agent harness

## 1 引言

Agent 团队可以拥有明确角色、依赖和验收条件，仍需要用户指出当前应由谁完成下一步。在一个启动案例中，协调角色PD（沿用原记录标签）知道任务必须有负责人，却将分配表达为外部待决定事项；在另一个发布案例中，各方接受相同角色原则，却把“迭代主干”理解为不同分支。本文研究这些局部责任断裂怎样阻碍流程推进，以及哪些协调和停止仍具有正面用途。（B01–B03）

本文以“责任悬空”描述所需动作在当前解释中未绑定到可行动主体的状态。“扯皮”仅是责任转交与反复解释的行为类比，不推断大语言模型（LLM）具有懒惰或推诿动机。正常权限申请、独立验收和真实依赖等待需要另行判断。（B01、B07、B09）

已有研究覆盖角色规范、步骤重复和协调错位。[1]<!--ref:R01--><!--anchor:section:4--> 本研究进一步将角色定义与具体动作中的对象、阶段、授权及验收范围分开。中心发现是：流程规则仍可能留下需要临场解释的责任关系；用户和下游角色通过补动作、澄清对象或核查事实使其重新明确。本文据此提出显式工作项表示，尚不将这些案例推广为复杂工作流的净因果效果。（B02、B03、B05、B07）

## 2 相关研究与概念

职责、能力、权限、验收权和目标责任需要分开。职责描述应做哪类工作；能力说明是否具备工具和环境；权限决定是否被允许执行；验收权决定谁能判断候选符合某个合同；目标责任则要求追踪尚未满足的交付目标。本文把流程复杂度描述为需要协调的角色、对象、阶段和判定边界，而不以消息数或节点数替代成本测量。W01 涉及分配职责，W02 涉及分支对象与发布阶段，Graph 涉及验收范围。（B01–B05）

MAST（多 Agent 系统失败分类）已区分角色规范违背、重复步骤、协调错位和验证不完整，并报告改善角色规范及高层验收的正向干预。它支持分析行为发生在哪个边界，却不支持预先把多角色当成故障。本文不移植其分类频率，也不把生产案例作为其基准的复现。[1]<!--ref:R01--><!--anchor:section:4--> 从过程分析出发，面向命令行接口（CLI）编码 Agent 轨迹的 Failure as a Process 强调区分错误起点、演进与恢复，同样的表面错误可能有不同机制，重复尝试未必无效。本文相应保留纠正前后，而不只读取最终状态。[2]<!--ref:R05--><!--anchor:section:II-->

模型—harness 边界研究提示，某些故障只有结合模型行为与运行机制才能解释；其问题报告也可能缺少上下文。因此，本文不把已排除的调度或 lease 故障用作流程复杂度的证据，外部 bug 不能填补本地机制缺口。[3]<!--ref:R03--><!--anchor:section:I--> 生产部署调查中，受约束的结构化流程、人类评估与可控自治是常见选择，可靠性仍是挑战；这构成“一切流程都有害”的实践反证，而非复杂度因果试验。[4]<!--ref:R04--><!--anchor:section:5.4, 7.1-->

AgentConductor 则在竞赛代码场景中按执行反馈更新分层有向无环图，并随任务难度调节图密度。其结果说明结构应按任务设计，图更稀疏或更复杂都不是固定目标；基准的准确率和 token 指标不能直接换算成本系统的消息或 generation 收益。[5]<!--ref:R29--><!--anchor:section:2.1.2, 3.2.2--> 本文因此只提出边界表示的改进，不由个别协调失误推出删去全部流程。

本文所说的责任闭合，因此不是要求每个角色都接受全部工作，而是要求一个尚未完成的事项能追溯到下一动作及有权采取该动作的主体。拒绝一个不合格候选仍可符合这一要求，前提是拒绝之后的未满足事项没有在交接中消失。（B07、B09）

上下文呈现可能构成另一种竞争解释。Lost in the Middle在所研究的问答和检索任务中观察到位置敏感性，同时部分模型在简单检索上接近全对。该反例提示，简单题的天花板不能回答复杂流程中的信息使用问题；本研究没有据此诊断部署模型的注意力机制或认知过载。[6]<!--ref:R32--><!--anchor:section:2.3, 3.2-->

## 3 材料与方法

### 3.1 冻结部署资料与案例重建

本研究复用同一部署的冻结消息、流程研究片段和监督案例。定向检索从26,961条消息得到429条去重候选，围绕11个锚点保存并阅读全文67条不同原生消息，另对既有分支案例原文进行复读。这是目的性筛查，不用于估计总体故障率；检索范围、固定区间和历史摘要纠错记录见[补充附录](evidence-details.md)。（B00、B03）

案例沿“任务要求—当前解释—转交或等待—纠正—后续记录”重建。对分配案例W01，首次启动拒绝来自PD报告，后续分配由原生generation与系统通知核对；对分支案例W02，逐条区分发送者、分支对象和发布阶段。监督与报告案例用于检验不同验收范围、合理拒绝和结果同步，避免将全部往返都编码为失效。（B01–B09）

定量分析采用两个互补层次：恢复W01、W02固定区间内全部28条消息，描述人工参与和发送者顺序；对排除显式夹具后的26个Plan、195个累计generation快照计算依赖结构，并配对比较根与最后记录。消息间隔不是完成时长，累计图也不等于活动任务集合。表中的IQR表示四分位区间；统计单位、公式和缺失值处理见补充附录。

明显调度、依赖消费和运行失联问题不承担流程设计负效应。历史提示与授权未完整恢复，Graph检查部分依赖参与者报告；本研究未重跑历史部署与验收，全部分析尚未人工复核。（B00、B04、B07）

表1和图1展示W01后续分配的五节点依赖链，四个不同assignee分别承担实现、独立验收、集成和运行验收。这里的四种职能不等于配置中的四种role枚举。

**表1。W01原生初始generation中的五个节点**

| 拓扑顺序 | Task | 任务短名 | assignee显示标签 |
|---:|---|---|---|
| 1 | `task-a1e1746e` | 实现状态机 | dev1 |
| 2 | `task-b9ff74f6` | 历史迁移 | dev1 |
| 3 | `task-effb5a81` | 独立验收 | tester1 |
| 4 | `task-84279343` | 合并 main | integration |
| 5 | `task-b38e6bee` | runtime 验收 | tester2 |

![图1：W01任务与责任结构](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p2-workflow-responsibility/figures/p2-fig1-assignment-structure.png)

**图1。**W01任务与责任结构。上部为职责入口示意；下部为generation-709baba8的原生节点、依赖与assignee映射，箭头按“前置→依赖者”绘制，与原生from依赖to的字段方向相反。首次空assignee及API拒绝仍是PD报告，不能由后续快照反推为已核前态。

### 3.2 受控责任映射实验

另以八种合成情境检验责任与对象映射，包括分配、迭代集成、补运行验收、请求审批、合理等待、原任务修复、NO-GO报告交付和既有证据复用。交叉三个二水平因素：一个合取门槛／分解为八个gate的等价表达（含最终门槛），责任关系联表／分表，以及无／有12条无关scope记录。中间gate是同一条件的表示，不要求额外执行工具，也没有新增工作量。联表与分表携带完全相同的行动、角色和主体关系。因此，本实验测量表示及关系计算，不能替代真实流程深度或团队规模的干预。

固定Sonnet 5.5与Haiku 5.5，每模型8情境×8条件×2呈现版本为128次，两模型共256次。模型通过同一CLI的新隔离进程运行，无项目记忆或真实工具；两呈现只调整记录顺序，不是独立随机重复。协议、全部输入、评分和运行代码在首个正式输出前冻结。合格输出须同时选对action、actor、object与contract，四项分别统计，格式错误另列；正确等待和升级不被预设为失败。错误输出按事前规则至多接受一次错误字段反馈，不给答案，不代表生产恢复实验。全部结果及同一AI自查过程见[实验附录](controlled-experiments.md)，尚未人工复核。

另构造四对“可见角色原则相同、正确动作不同”的状态，检查受限表示的充分性。它们是形式反例，不是新增生产案例或模型错误数。实验不把参与者的事后解释视为模型训练或内部动机的证据。

输出读取后另做题目有效性审计；预检的表示等价与程序测试不能保证规范答案唯一。发现缺陷的情境按全部条件排除负面归因，原分数不改写。另固定本篇两题的全部条件、顺序和模型，共64次材料修复复测，属于事后诊断；不是新增预定独立样本，也不与主分母合并。具体问题及结果见研究发现。

## 4 研究发现

### 4.1 清楚的启动前提未自动转化为协调者的行动

W01中，PD识别出启动要求每个任务有负责人，也知道实现与独立验收需要分开，却将下一步表述为“需要先确定”负责人。用户要求其自行分配后，原生初始generation记录了五个节点分给四个不同assignee，系统随后通知首任务ready。后续分配具有结构化证据，图2展示了纠正与记录之间的时序。（B01、B02）

用户在这次交互中补入的是分配动作的承担者，没有取消独立验收，也没有提供新仓库或执行环境。可见的协调缺口位于“前提不满足”与“由谁补足前提”之间。保留分配门槛有助于暴露缺失条件，但门槛本身没有在PD的当次解释中落实行动责任。（B01c–B01e）

该过程未呈现双方反复拒绝的循环。首次拒绝仍是报告级证据，后续分配也不能证明更早授权与状态完全相同。因此，W01支持一次需要用户补动作绑定的协调片段，不能据此确定永久停摆或无人介入时的结果。（B01、B02）

![图2：W01动作责任纠正与后续记录](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p2-workflow-responsibility/figures/p2-fig2-action-timeline.png)

**图2。**W01动作责任纠正与后续记录。用户指令后31.295246秒保存带分配的generation，31.872839秒出现首任务ready消息。分别标识报告、指令、原生快照与系统通知；不将这些间隔称为人工干预效果或停摆时长。

### 4.2 相同角色原则仍可能指向不同工作对象

W02中，integration与PD都接受开发期合并和发布职责的划分，却对“迭代主干”指向不同对象。PD将迭代主干表述为main，并要求integration执行相应合并；用户随后明确区分开发分支、版本迭代分支与发布main，并分别指定合并责任。角色共识没有自动形成对象和阶段共识。（B03.1–B03.3、B03x）

纠正角色映射后，PD又以迭代分支不存在为前提要求重建。integration核对远端后报告该分支已包含开发成果与验收证据，拒绝重复创建。这一检查具有正面作用：下游没有仅因指令来自协调者或由用户转贴，就将错误前提作为事实继续执行。（B03.4–B03.6）

表2区分了职责、对象、事实和发布顺序的补充。该案例的机制在于，抽象职责仍须落到具体源对象、目标对象和阶段；有效事实核查则能纠正交接中的错误。发布报告保留为历史叙述，不作为本研究重新验证部署成功的依据。（B03.7–B03.10）

**表2。W02的对象、责任与事实纠正（2026-06-16 UTC）**

| 时间 | 记录主体 | 记录内容 | 对判断的作用 |
|---|---|---|---|
| 01:41:52.993389 | PD报告 | 将迭代主干称为main，并交给integration合并 | 职责口号相同，目标分支仍被误映射 |
| 01:45:51.965601 | 用户 | 澄清开发分支→迭代主干→main及ship责任 | 补充对象、阶段与行动者 |
| 01:53:27.945879 | integration报告 | 核对后称迭代主干已存在，不应重建 | 下游事实核对具有正面作用；未新做Git复验 |
| 02:00:39.671089 | 用户 | 纠正构建、文档、tag、合main与清理次序 | 是发布操作顺序纠正，不是明确提前done |

### 4.3 验收结论依赖范围，不能仅按PASS与REJECT对立判断

Graph案例中，Supervisor已报告跨页合并、计数与回归通过，Tester对同一候选继续检查点击聚合边后的证据请求，指出计数正确仍不足以证明两页证据进入详情视图。后续候选又暴露跨项目请求使用错误作用域的问题。这些判定对应逐步展开的检查范围，不能仅以标签不同认定责任推诿。（B04、B05）

该案例将目标责任具体化为覆盖关系：谁负责连接已经证明的局部行为与尚未覆盖的产品要求。独立验收的增量来自新的输入、断言和作用域检查。由于相应源码未能完整读取，本篇保留这些历史检查报告，不据其计算监督误判率。（B05a–B05f）

### 4.4 合格的停止、报告与同步具有不同用途

Spark报告任务要求给出可审计的GO或NO-GO，而非保证产品可发布。报告被接受时仍列明输入、环境和实现方面的发布缺口，持久报告也保存了NO-GO结果。因此，报告任务完成与产品尚不可发布可以同时成立；目标层仍需追踪剩余条件。（B07）

四组近似消息承担了Task到Plan的结果传播或完成状态补记，未见相应新增产品检查。它们提供同步证据，不能累计为独立验收。另一个反例中，Agent明确承担自身候选责任，却因缺生产证据拒绝放行。这些行为提醒流程评价同时保留同步、质量控制与外部依赖的用途。（B08、B09）

### 4.5 人工参与主要补充动作、对象与操作顺序

两个固定区间包含28条消息，其中12条由用户发出。W01的6条消息中有3条用户消息；W02的22条消息中有9条（表3）。这些比例描述所选案例的沟通构成，不代表全系统人工干预率。

**表3。固定区间的全部消息与下一条Agent消息间隔**

| 区间 | 消息数 | 用户消息 | Agent消息／不同发送者 | 系统消息 | 用户消息至下一条Agent消息：中位秒数 [IQR] |
|---|---:|---:|---:|---:|---|
| W01 | 6 | 3 | 2／1 | 1 | 40.11 [37.69, 280.32]，n=3 |
| W02 | 22 | 9 | 13／6 | 0 | 110.77 [32.68, 210.30]，n=9 |

12条用户消息中，5条是启动或继续要求，5条补充或质询责任、对象及操作顺序，另有1条转贴指令和1条追问完成状态（表4）。用户补偿的内容由此可以具体定位到控制关系；这些消息没有提供新执行器或独立运行测量，但确实包含新的动作和语义信息。

**表4。十二条用户消息的内容编码**

| 互斥主类别 | 消息数 | 实际补充内容 |
|---|---:|---|
| 启动／继续要求 | 5 | W01两次启动、W02一次ship及两次继续 |
| 责任、对象或顺序纠正／质询 | 5 | 谁分配、角色边界、错误交接质询、三层分支映射、发布顺序 |
| 转贴操作指令 | 1 | 重建分支请求；随后Agent以已存在分支的报告反驳 |
| 完成状态追问 | 1 | 是否已经合入main |

转贴的重建指令随后被Agent的分支核查反驳，说明人类参与并不天然提供正确事实。补齐完整区间还发现，较早的回复仍可能包含对象误解。因此，消息回应与责任纠正需要分别编码，下一条Agent消息的时间不能作为问题已解决的时间。精确窗口与旧选编修正见补充附录。

### 4.6 工作流结构增长并不等同于成员增加

W01的5对相邻消息中有4对切换发送者，W02的21对中有18对（表5、图3）。这些顺序统计展示了往返分布；它们没有识别直接回复关系，也不把每次换人发言解释为无效协调。

**表5。两个固定区间的发送者切换**

| 区间 | 全部消息 | 相邻消息对 | 发送者不同的相邻对 | 未发生切换 |
|---|---:|---:|---:|---:|
| W01 | 6 | 5 | 4 | 1 |
| W02 | 22 | 21 | 18 | 3 |

![图3：完整区间沟通序列与响应分布](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p2-workflow-responsibility/figures/p2-fig3-coordination-sequences.png)

**图3。**（a–b）完整有界区间按发送者绘制，每点一条消息，菱形为用户；（c）W02相邻发送者计数，行是前一条、列是下一条，不意味着直接回复；（d）全部用户消息至下一条Agent消息的间隔，黑线为中位数，重合点可能遮挡且多个间隔可共用回复。区间由既有案例首尾确定，不是系统总体样本。

在26个Plan的根与最后累计快照之间，节点数中位数从8增至17.5，seq最长路径从3条边增至6条边，最大拓扑层从3个节点增至6.5个，而不同assignee数中位数均为6.5（表6）。结构扩展发生在任务和依赖层面，成员数量不足以概括这种变化。

**表6。根／最后快照的配对结构中位数**

| 指标 | 配对Plan数 | 根记录 | 最后记录 |
|---|---:|---:|---:|
| 累计节点数 | 26 | 8 | 17.5 |
| 不同assignee数 | 26 | 6.5 | 6.5 |
| seq最长路径，边数 | 26 | 3 | 6 |
| 最大拓扑层节点数 | 26 | 3 | 6.5 |
| 跨assignee边占比 | 21 | 100.0% | 93.8% |

两端均有可比较边的21个Plan中，跨assignee边占比中位数从100.0%变为93.8%。它描述跨负责人连接程度，不是交接错误率。最大拓扑层也不是实际并发容量，因为累计快照可能保留已完成节点，并受到其它依赖与同一负责人约束。图4保留每个Plan的配对关系，避免把汇总中位数理解为所有Plan均按同一方式增长。

![图4：根与最后快照的配对复杂度](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p2-workflow-responsibility/figures/p2-fig4-topology-comparison.png)

**图4。**四面板分别比较不同负责人、seq深度、跨负责人边比例与最大拓扑层；每条细线连接同一Plan，黑线为该面板配对支持上的中位数。不同负责人、seq深度和最大拓扑层各26对，跨负责人比例21对；拓扑层轴为对数。未进行随机干预或复杂度—失败回归。

这些结果将“流程复杂”分解为成员、依赖深度、层规模和交接边界。它们为后续比较提供可操作指标，尚未建立复杂度与停摆之间的因果关系。

### 4.7 对照未提供复杂度致错的有效证据，评分审计发现材料缺陷

256次请求均正常返回且模型标识一致，冻结规则原评分为Sonnet 107/128、Haiku 102/128（表7）。这一差距不能直接解释为责任推理差异：W2同时给出待集成目标与integration_ok=true，W8没有说明回执适用哪份合同，却预设release为唯一答案。两题共64次响应中42次未匹配，不能承担模型或流程负效应归因；全部原始响应与原评分保留，未仅删除不利输出。

排除这两题的全部条件后，Sonnet为91/96，Haiku为96/96。Sonnet其余五次均在正确JSON前附加解释，违反指定输出接口；事后从原文提取唯一内嵌JSON，六个情境的动作、主体、对象与合同两模型均为96/96。该诊断不把格式失败改写为合格响应。Sonnet原始collapsed减expanded差4.69个百分点，排除两题后为6.25个百分点，但全部来自格式；Haiku排除后各因素差均为0。联表的原始优势同样不能脱离题目缺陷解释。

**表7。原评分、有效性排除与另行复测（不合并分母）**

| 分析层 | Sonnet 5.5 | Haiku 5.5 | 含义 |
|---|---:|---:|---|
| 原冻结严格评分 | 107/128 | 102/128 | 含矛盾／缺信息题，保留审计用途 |
| 排除W2、W8全部条件 | 91/96 | 96/96 | 六情境；Sonnet剩五次为格式不合规 |
| 上行内嵌JSON字段诊断 | 96/96 | 96/96 | 事后提取，不覆盖严格得分 |
| W2、W8材料修复后新调用 | 32/32 | 32/32 | 事后冻结复测，未混入主分母 |
| 事前一次字段反馈 | 未抽到 | 0/24 | 全为缺陷题；不能估计真实纠错能力 |

事前反馈按固定排序取前24个未匹配输出，恰好全属Haiku的W2/W8，均未达到原判据。由于输入本身存在问题，该结果只说明此反馈没有消除原评分不匹配，不能称模型坚持错误。另冻结修复W2状态与W8合同信息后的64次新调用，两模型分别为32/32与32/32；这是材料修复诊断，不能证明架构简化有效，也不能补成历史生产事故的对照。

![图5：流程对照与材料有效性审计](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p2-workflow-responsibility/figures/p2-fig5-controlled-mechanisms.png)

**图5。**上左为排除缺陷题后的全条件严格得分（每格12次）；上右保留原八情境分数，星号标记不能承担负面归因的W2/W8；下部为六情境的配对差（每模型每因素48对）及另行材料修复结果。格式问题与语义字段分开解释，非团队停摆率。原128次／模型底表、全部反例和修复输入差分见实验附录。

## 5 机制分析与设计启示

### 5.1 责任悬空发生在规则到具体动作的映射处

W01中的规则限制了不合格分配，却未在协调者的解释中生成应由自己完成的合格分配；W02中的角色原则没有固定合并的源分支、目标分支和发布阶段。两者共同揭示，职责说明与当前行动之间仍存在需要模型推导的映射。反复协调可能围绕这些未被显式表达的关系展开。（B01、B03）

因此，减少角色数未必针对已观察的问题。W01纠正后仍保留四个不同负责人的分工，W02下游核查也具有明确用途。更具体的简化目标是减少必须反复通过对话确认的控制关系，同时保留增加质量证据的独立工作。生产研究和动态拓扑研究同样支持按任务评估结构的可控性与密度，而非预设角色越少越好。[4]<!--ref:R04--><!--anchor:section:5.4, 7.1-->[5]<!--ref:R29--><!--anchor:section:2.1.2, 3.2.2-->（B02、B03.6）

### 5.2 为工作项绑定对象、动作与授权

本文建议将目标owner、下一动作、动作对象、输入、授权和结束条件一并表达。对W01，工作项应明确由PD完成任务分配并重新启动，同时保留独立验收约束；对W02，则应明确由谁将哪个源分支合入哪个目标分支。该表示是从案例导出的候选方案；本轮只比较关系联表的短题行为，完整工作项协议的交付效果尚待比较。（B01、B03）

工具拒绝可以进一步区分可自行补足的前提与真正的外部依赖。前者指向当前主体可执行的准备动作，后者保留具体缺失输入、决策者和升级条件。缺生产证据的候选不能靠催促或承诺放行；同样，已经具备条件的准备工作也不应只留下模糊的“等待确认”。（B01、B09）

### 5.3 将局部验收与剩余目标一起交接

验收结果应绑定候选、合同范围和判定者，并明确哪些要求仍未覆盖。Graph的计数、点击取证与跨项目取证是不同检查；NO-GO报告的合格交付也不同于产品可发布。将剩余义务交给有能力且获授权的主体，可以避免局部完成状态遮蔽未完成目标。（B05、B07、B09）

结果同步则应保留原始来源和用途。相同检查结果传播到另一层级时，可以引用已有候选与结论；只有增加新检查时，才将其记录为新增验证。当前记录不足以决定删除哪一条消息，后续评价应同时观察同步是否被消费及独立检查是否保留。（B08）

### 5.4 按补偿内容评价人工兜底

用户补动作、澄清分支与下游核事实，是不同类型的补偿。后续对照应固定任务、权限与验收条件，检验显式工作项是否减少这类澄清，同时保留正确拒绝、独立检查和合理升级。对话更少或回复更快都不足以单独表示改善；还需要检查未满足义务是否被继续追踪。（B01、B03、B08、B09）

### 5.5 责任分配首先是关系完备性问题

可将当前工作项写成关系组 `〈目标，动作，主体，对象，合同，授权，前提〉`。角色定义只约束其中部分字段；例如“集成者负责合并”没有确定本次合并的目标分支，也没有确定当前阶段是否允许发布。W01的分配动作与W02的分支映射分别暴露了主体和对象的缺口；下游拒绝重复建分支则显示，明确的协调指令仍需事实核查。此处的深层问题不是成员数量本身，而是工作项是否足以决定合法下一步。（B01–B03）

形式上，令完整状态为s、可见表示为φ(s)、合法处置为y(s)。若两种合法状态满足φ(s₁)=φ(s₂)，但y(s₁)≠y(s₂)，只依赖φ的确定性决策就不可能同时正确。本轮四对构造分别改变当前负责人、阶段目标、报告与发布合同、外部请求是否已发出，而保持相应受限视图不变。它们证明这些受限表示不充分，不证明历史模型当时只看到了该表示，也不计入模型失败次数。与之相对，分表实验保留全部关系，测的是恢复联结的行为难度；缺信息和有信息但联结错误不能混为一种解释。

### 5.6 局部正确如何与全局不推进并存

工作流中的每个主体可以正确完成自己的局部动作，整体仍未获得交付：测试者正确拒绝，协调者正确转述，开发者尚未收到带对象与授权的修复事项。其候选机制是交接没有传递未满足的义务，而非任一成员具有推诿动机。Spark的NO-GO报告正是重要反例：报告任务已经完成，产品目标仍未完成；将两者都记为一个done或blocked会遮蔽应继续追踪的内容。（B07、B08）

一个可检验的流程条件是：对尚未完成且不存在已记录外部等待的义务，应能定位至少一个可授权的下一动作及其承担者。该条件仅排除一种责任悬空，还不是活性保证；循环依赖、资源不可用、无人调度和持续生成无效动作仍可阻止完成。八种情境的单步处置测量没有执行这些时序过程，因此本文未复现真实团队停摆，也不能把表述深度的差异称为死锁概率。

设计上，应让拒绝输出同时保留未满足断言、精确候选、责任主体及恢复条件；把未知事实、无权决策和已请求的外部等待分别记录。人工补充则按实际内容编码：补授权、补事实、补关系或仅催促。后续长程实验须在相同工具与真实授权下比较这些交接协议，观察终态质量、义务丢失、重复动作及人工补偿，而不只统计对话多少。

### 5.7 竞争解释及目前能够区分的层次

**表8。责任与复杂度的竞争解释**

| 解释 | 当前支持与反证 | 仍需怎样检验 |
|---|---|---|
| 角色到动作的关系缺失 | W01/W02需补主体或对象；形式反例说明受限表示不足 | 恢复实际输入，比较完整工作项与角色规则 |
| 关系齐全但联结困难 | 短题排除材料缺陷后未见有效语义错误；不支持普遍困难 | 固定信息，增加合法对象数量与关联深度 |
| 局部完成掩盖剩余义务 | Graph及NO-GO案例区分局部与全局；合法拒绝是反证 | 真实多步交接，追踪义务是否丢失 |
| 复杂表述增加负担 | Sonnet严格格式有差异，未形成语义错误证据 | 匹配长度，操纵必要推理深度与真实依赖 |
| 授权或事实不足 | 人工补充类型不同；缺生产证据时拒绝有用 | 分开干预授权、事实、催促，记录恢复路径 |
| 模型训练或协调动机 | 本轮未干预训练，也无动机测量 | 跨模型训练／提示干预；不能从reason倒推 |

因此，目前最稳固的分析单位是当前行动关系和未满足义务，而不是“像人类扯皮”的心理解释。实验材料本身的缺陷还表明，唯一答案必须由模型实际可见的事实推出；这个测量教训不能冒充一项新的生产失败发现。

## 6 局限

材料来自单部署、目的性检索和既有案例窗口。历史提示与授权未完整恢复，用户后续要求可能改变当时的行动条件。因而本文不能估计全量人工依赖率、永久停摆或无人介入时的结果。（B00、B01）

结构指标描述累计快照，未被校准为成本或认知负荷，也没有经过随机操纵。跨角色的W02发生在后来的内部Supervisor/executor设计之前，不用于解释内部身份架构。Graph检查依赖历史报告，不同验收范围亦不允许直接计算监督误判率。过程研究提供分析视角，不替代本系统的效果测量。[2]<!--ref:R05--><!--anchor:section:II-->（B00、B04）

明显系统故障继续排除设计归因，未知运行条件也不作为流程净效应证据。全部分析尚未人工复核，历史验收没有重跑，四篇论文复用部分材料。旧摘要纠错及用户转贴错误指令的记录说明，二手归纳和人工参与都仍需原文与事实核对。（B03、B07、B09）

新增实验是八个固定英语短情境的交叉呈现，不是128个独立任务，也没有多次随机重复。两个顺序版本只反转记录/块顺序，冻结协议概述中的“标识映射”未实现，偏差保留在实施记录。两固定模型不代表模型总体；CLI报告的模型标识可查，平台不可见输入仍未完全审计。两组共享明确的处置规范，部分目标直接提示动作，存在较强提示与天花板边界；没有把格式错误当作内部认知或责任错误。

P2的合取分解没有增加真实工具调用或工作量，部分处置也无需对整条release条件求值。联表和分表长度不同，额外记录只覆盖十二条清楚标记的无关scope；本实验未隔离token数量、位置和关系计算的全部影响，也未操纵真实参与者数量或长程依赖。一次错误字段反馈只检验改答，不能确定无人介入的持续性、停摆率或恢复时间。角色、授权与当前对象在生产中可能不完整，而实验显式提供，结果不能代替历史根因归属。

## 7 结论

真实案例将不推进的局部机制定位到主体、动作、对象、授权和剩余义务之间的关系缺口，独立验收与下游事实核查仍有具体用途。这个解释比成员数量或“推诿动机”更接近可检验的设计对象，但历史输入缺失仍限制根因识别。

受控实验没有给出复杂度导致语义责任错误的有效证据：原评分中的主要不匹配来自材料矛盾与缺信息，其余有效情境的差异为输出格式。修复诊断和原结果分别保存，未据短题正确率宣称长期流程可靠。后续应检验完整工作项与义务交接在真实多步协作中的作用，保留正确拒绝与外部等待。

## 参考文献

[1] M. Cemri et al., “Why Do Multi-Agent LLM Systems Fail?,” arXiv:2503.13657v3, 2025. doi: [10.48550/arXiv.2503.13657](https://doi.org/10.48550/arXiv.2503.13657). [已核版本](https://arxiv.org/abs/2503.13657v3).

[2] X. Zhao, H. Li, S. Li, T. Zhao, E. T. Barr, F. Sarro, and H. Ye, “Failure as a Process: An Anatomy of CLI Coding Agent Trajectories,” arXiv:2607.09510v1, 2026. doi: [10.48550/arXiv.2607.09510](https://doi.org/10.48550/arXiv.2607.09510). [已核版本](https://arxiv.org/abs/2607.09510v1).

[3] J. Chen, S. Chen, H. Zhu, J. Cao, J. Shen, and S.-C. Cheung, “Understanding Agent-Reactive Bugs at the Model-Harness Boundary: An Empirical Study of LLM Agent Issue Reports,” arXiv:2607.15684v1, 2026. doi: [10.48550/arXiv.2607.15684](https://doi.org/10.48550/arXiv.2607.15684). [已核版本](https://arxiv.org/abs/2607.15684v1).

[4] M. Z. Pan et al., “Measuring Agents in Production,” arXiv:2512.04123v4, 2025. doi: [10.48550/arXiv.2512.04123](https://doi.org/10.48550/arXiv.2512.04123). [已核版本](https://arxiv.org/abs/2512.04123v4).

[5] S. Wang et al., “AgentConductor: Topology Evolution for Multi-Agent Competition-Level Code Generation,” arXiv:2602.17100v1, 2026. doi: [10.48550/arXiv.2602.17100](https://doi.org/10.48550/arXiv.2602.17100). [已核版本](https://arxiv.org/abs/2602.17100v1).

[6] N. F. Liu et al., “Lost in the Middle: How Language Models Use Long Contexts,” *Transactions of the Association for Computational Linguistics*, vol. 12, pp. 157–173, 2024. DOI: [10.1162/tacl_a_00638](https://doi.org/10.1162/tacl_a_00638). [出版记录](https://aclanthology.org/2024.tacl-1.9/).

## 证据附录

正文 B 编号对应项目原始材料，不是外部文献编号。消息支持当时表述，generation 支持指定快照状态，报告及源码只按实际读取层级使用。所有消息时间均为协调世界时（UTC）。C07-E07 按发布顺序纠正解读；C07-E10 的发送者为 PD，与 E09 同源，不计为第二角色独立核验。

| 编号 | 用途 | 原生 ID / 类型 | UTC 时间 | 原文路径 |
|---|---|---|---|---|
| B00 | 010检索、纳排、C07更正与证据范围 | 文档/派生索引 | 不适用 | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/findings.md) |
| B01a | 第一次启动要求 | `01M14JPXVK1Z8CKDYMTD3TSPAX` | 2026-08-28T16:19:40.531485Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01M14JPXVK1Z8CKDYMTD3TSPAX.json) |
| B01b | 再次启动要求 | `01M14K5QQTGJF62VANSQC5WW38` | 2026-08-28T16:27:45.786608Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01M14K5QQTGJF62VANSQC5WW38.json) |
| B01c | PD报告assignee拒绝并待确定负责人 | `01M14K6T665DY5ZSR50T9ACMW4` | 2026-08-28T16:28:21.062508Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01M14K6T665DY5ZSR50T9ACMW4.json) |
| B01d | 用户要求自行分配 | `01M14KC5XZJD4PGDV4614XEWSV` | 2026-08-28T16:31:16.927437Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01M14KC5XZJD4PGDV4614XEWSV.json) |
| B01e | PD报告分配与启动 | `01M14KDD3C9JDTE6PYV3Y01MB1` | 2026-08-28T16:31:57.036572Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01M14KDD3C9JDTE6PYV3Y01MB1.json) |
| B01f | 原生首任务ready系统消息 | `01M14KD520A5X362XVHJ64EKC7` | 2026-08-28T16:31:48.800276Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01M14KD520A5X362XVHJ64EKC7.json) |
| B02 | 五节点、四个不同负责人的初始分配，仅读初始generation | `generation-709baba8` | 2026-08-28T16:31:48.222683Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/pm_plan_generations-plan-2a8bc24c.json) |
| B03.1 | 旧006 C07-E01原文；sender和顺序按010核对 | `01KV71B56MYTKFX58W48VARSTA` | 2026-06-16T01:39:41.396427Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E01.json) |
| B03.2 | 旧006 C07-E02原文；sender和顺序按010核对 | `01KV71F5Q1DGQ5MP1Q29XMQ2H4` | 2026-06-16T01:41:52.993389Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E02.json) |
| B03.3 | 旧006 C07-E03原文；sender和顺序按010核对 | `01KV71PF2XKD4YY5ERFD4ACJNV` | 2026-06-16T01:45:51.965601Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E03.json) |
| B03.4 | 旧006 C07-E04原文；sender和顺序按010核对 | `01KV71VRQBZC147BD2RTTQ0KYE` | 2026-06-16T01:48:45.675353Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E04.json) |
| B03.5 | 旧006 C07-E05原文；sender和顺序按010核对 | `01KV71XZ0F9CXTWEMS6YEAP4DK` | 2026-06-16T01:49:57.64719Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E05.json) |
| B03.6 | 旧006 C07-E06原文；sender和顺序按010核对 | `01KV724CC96MF8A8SKPZACE06C` | 2026-06-16T01:53:27.945879Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E06.json) |
| B03.7 | 旧006 C07-E07原文；sender和顺序按010核对 | `01KV72HHZQN6EXSGA9DCWZ7ZS7` | 2026-06-16T02:00:39.671089Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E07.json) |
| B03.8 | 旧006 C07-E08原文；sender和顺序按010核对 | `01KV72MY58X8TTJ7PCJ3P7DXA9` | 2026-06-16T02:02:30.440646Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E08.json) |
| B03.9 | 旧006 C07-E09原文；sender和顺序按010核对 | `01KV730J1MXKES7PXRRD3WJXNJ` | 2026-06-16T02:08:51.252117Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E09.json) |
| B03.10 | 旧006 C07-E10原文；sender和顺序按010核对 | `01KV736EDS5M1P398Y28DJXKPF` | 2026-06-16T02:12:04.153627Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E10.json) |
| B03x | integration明确开发期职责、移交ship | `01KV71C7NM79RDSTBBVEM5K6BY` | 2026-06-16T01:40:16.692144Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KV71C7NM79RDSTBBVEM5K6BY.json) |
| B04 | 009合同范围、源码不可读及调查限制 | 文档/派生索引 | 不适用 | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/reports/exact/agent-harness-review-increment-cases-009.md) |
| B05a | Graph G7 Supervisor局部通过 | `01M1KYSW1EP98VCC0DFPJMXQB1` | 2026-09-03T15:39:36.366098Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S04-02.json) |
| B05b | Graph G7 Tester点击取证拒绝 | `01M1KYWDAFAM06Z867W7QFMVVX` | 2026-09-03T15:40:59.599882Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S04-03.json) |
| B05c | Graph G8同项目通过 | `01M1KZNHMCRFAE8VWDHE36XFHT` | 2026-09-03T15:54:43.212456Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S05-02.json) |
| B05d | Graph G8跨项目拒绝 | `01M1KZSCCMZHXY1PQXAM1FNNRS` | 2026-09-03T15:56:48.916638Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S05-04.json) |
| B05e | Graph G9 scope通过 | `01M1M0YT5A3RYX7GE2NE3XEXSB` | 2026-09-03T16:17:15.434128Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S05-07.json) |
| B05f | Graph G9 Tester最终通过 | `01M1M1CV7QBV5NABRJ817XFCWH` | 2026-09-03T16:24:55.287485Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S05-09.json) |
| B07a | Task task-045a426e创建合同：给出GO/NO-GO报告 | `generation-57b10f96` | 2026-09-01T13:31:02.01844Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/C05.json) |
| B07b | 报告验收通过而产品NO-GO | `01M1EPVGFF85ZSCH2KNSDSKGHX` | 2026-09-01T14:44:29.295627Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S06-02.json) |
| B07c | 持久NO-GO报告及已测实现标识；未重跑命令 | 文档/派生索引 | 不适用 | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/sources/spark-release-README.md) |
| B08.1a | R01 Task结论 | `01M0QM59RA181QMKVGFFYGHDR9` | 2026-08-23T15:34:52.426323Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S01-05.json) |
| B08.1b | R01 Plan同步 | `01M0QM5VPGR8P4F4377DGRB2PN` | 2026-08-23T15:35:10.800032Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S01-06.json) |
| B08.2a | R02 Task结论 | `01M1EH0A4N0RMSPQ2C1ZJHCHX1` | 2026-09-01T13:02:15.189491Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S02-04.json) |
| B08.2b | R02 Plan同步 | `01M1EH0JHBMX3Y4V7HZT9HKAZX` | 2026-09-01T13:02:23.787519Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S02-05.json) |
| B08.3a | R03 Task结论 | `01M1KZNHMCRFAE8VWDHE36XFHT` | 2026-09-03T15:54:43.212456Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S05-02.json) |
| B08.3b | R03 Plan同步 | `01M1KZPB1G13XSA48KKT8BY36P` | 2026-09-03T15:55:09.232526Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S05-03.json) |
| B08.4a | R04同Task第一条 | `01M1M0YT5A3RYX7GE2NE3XEXSB` | 2026-09-03T16:17:15.434128Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S05-07.json) |
| B08.4b | R04同Task第二条 | `01M1M0Z2N5WM8TNNXMSEAGKFYW` | 2026-09-03T16:17:24.133138Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S05-08.json) |
| B08 | 四组配对与会话归属派生索引 | 文档/派生索引 | 不适用 | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/derived/relay-pairs.json) |
| B09a | I05明确自身责任 | `01KZDJ41E4XMR6RSRDW2342DCE` | 2026-08-07T07:31:07.844252Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZDJ41E4XMR6RSRDW2342DCE.json) |
| B09b | I05生产证据门槛与拒绝 | `01KZDKN7K7XRNZ0DPE7G9V0GQA` | 2026-08-07T07:57:59.783637Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZDKN7K7XRNZ0DPE7G9V0GQA.json) |
| B09c | I05后续补证或回撤计划 | `01KZDKS2M645VTZJ9GR8SW0MDJ` | 2026-08-07T08:00:05.766163Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZDKS2M645VTZJ9GR8SW0MDJ.json) |

## 声明

**数据可用性。** 使用既有冻结研究包，原生消息与合同按附录的本地路径可在授权环境内定位，实际阅读范围见 `read-log.json`。本地可查不等于公开发布或开放许可，公共共享与脱敏安排待数据持有人确认。 本轮合成输入、全部模型响应、评分、反馈与图源另存于[实验附录](controlled-experiments.md)，逐次结果见[实验底表](experiment-results.csv)，修改前稿和可回放差分见[修订记录](REVISION-011.md)。

**伦理与隐私。** 记录含用户与 Agent 的协作内容。伦理机构、批准或豁免情况尚未由作者声明；本文不推断无需审查。公开前须由责任作者复核访问授权、可识别信息和必要脱敏，原生标识符（ID）供本地追溯。

**作者贡献。** 署名作者为 oopslink；具体 CRediT 贡献角色待补充。AI 不列为作者。

**利益冲突。** 待作者声明，未据资料缺失写作“无利益冲突”。

**资助。** 本研究未获得资助。

**AI使用。**001—009原研究及010写作阶段的AI参与记录保留；本轮011由同一AI完成实验实现、分析、写作和有界自查，未新增独立评阅。实验调用固定claude-sonnet-5-5和claude-haiku-5-5、low effort，版本标识与原始响应保存；历史部署模型和参数未知处仍不补造。模型输出不作为内部推理或训练来源真值。原研究与本轮分析均尚未人工复核；未重跑历史产品验收。

**同源与跨篇重合。** 四篇工作稿基于同一部署和冻结快照，存在目的性选例重合。本文的 Graph、Spark 与 I05 边界材料亦供其它篇目分析，但问题分别是责任闭合、监督或产物继承；同一消息保持原生标识，不累计为不同实验，不把跨篇一致性当独立重复验证。
