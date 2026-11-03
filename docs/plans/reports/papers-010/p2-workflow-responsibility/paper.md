# 复杂工作流中的责任悬空：Agent 团队为何会反复协调而停止推进

## 中文摘要

更多角色、依赖和验收条件是否会使 Agent 团队陷入反复协调？本文以 Agent Center 的冻结记录开展探索性案例研究，将“扯皮”限定为责任转交、职责解释不一致及下一动作悬空的行为类比，不推断模型具有消极动机。主案例中，协调角色 PD（沿用原记录标签）将正常 assignee 校验解释为需要先确定负责人；用户要求其自行分配后，初始 generation 记录了五节点、四个不同负责人（assignee）的分配。六月案例显示，各方虽复述相同角色原则，却把迭代主干与发布 main 映射为不同对象，须经用户澄清和下游事实核对。Graph 的分层验收、合格 NO-GO（不可发布）报告及结果转述则说明，等待、重复和局部通过不能一律算作流程失效。观察支持“角色定义未自动闭合当前行动责任”，但未识别复杂度的因果效应、永久停摆或净成本。原研究与本轮补查均为人工智能（AI）探索性分析，尚未人工复核；本轮另有 AI 写作和审阅。本文据此提出将 owner、下一动作、输入、授权和验收范围绑定的待检验设计建议。（证据 B00–B09）

**关键词：**多 Agent 协作；责任闭合；工作流；独立验收；agent harness

## Abstract

Can additional roles, dependencies, and acceptance conditions leave agent teams repeatedly coordinating instead of advancing? This exploratory case study examines frozen Agent Center records and treats organizational buck-passing as a behavioral analogy, without attributing avoidance motives to models. In the main episode, a coordinator described a normal assignee prerequisite as a decision still to be made; following a user's instruction to assign owners, a native initial generation recorded five nodes allocated to four distinct assignees. A June episode shows that agents repeated the same role rule while mapping iteration and release branches differently. Layered Graph reviews, a valid NO-GO report, and result relays provide counterexamples to treating every pause or repetition as workflow failure. The findings support a specific concern: role definitions do not automatically determine ownership of the next action. They do not identify the causal effect of complexity, permanent deadlock, or net cost. The original research and supplemental work are AI exploratory analyses, with additional AI writing and review in this round and no completed human verification. The paper proposes explicit action-owner-input-authorization-acceptance records for future evaluation. (Evidence B00–B09)

**Keywords:** multi-agent collaboration; accountability; workflow; independent review; agent harness

## 1 引言

一个计划已经获得启动指令，PD 报告中心返回“每个任务必须有负责人”，协调者却继续解释需要先确定谁负责。用户再次要求它分配后，五个节点被分给实现、验收和集成角色，计划出现初始 generation 与首任务 ready 消息。这一片段中的阻碍不是校验语句不可理解，也不是必须把所有工作交给同一人，而是流程前提没有被转成当前协调者的下一动作。（B01、B02）

这与人类团队中“每个人都知道职责，事情仍无人往前推”的经验相似，但类比需要落到可观察行为。本文使用“责任悬空”描述这样一种局部状态：目标已有承担者，所需下一步也可被识别，却没有在当前解释中绑定到可行动的主体。“扯皮”仅指往返解释和责任转交的行为外观，不表示大语言模型（LLM）懒惰、恶意或有规避工作动机；正常权限申请与依赖等待亦不自动属于这一状态。（B01、B07、B09）

既有失败研究已经覆盖角色规范、步骤重复和协调错位，不能因为生产记录措辞更像组织对话就另立一套失败分类。[1]<!--ref:R01--><!--anchor:section:4--> 本文的问题更具体：角色规则为何有时未能确定下一动作；用户究竟补入了什么；如何区分协调问题与有价值的审查或等待。六月分支案例与八月分配案例提供前两项的过程材料，009 的审查与报告任务用于约束第三项。（B00、B03–B08）

本文的中心论点是，流程定义角色与条件，并不自动完成对象、阶段和行动者之间的映射。观察到的协调依赖足以支持改进责任表示，但不足以证明复杂工作流总体更差。本文通过原生消息、初始 generation 与验收合同的交叉阅读，提出以具体工作项闭合责任的设计方向，并保留独立验证所增加的证据。（B02、B03、B05、B07）

这也要求将“没有进展”拆开观察：没有提交可能是等待真实输入，也可能是未执行自己能完成的前提；已有提交也不代表交付目标已达成。只有把下一步所需条件和负责主体一并读出，才能判断停止是否需要纠正。（B01、B07、B09）

## 2 相关研究与概念

职责、能力、权限、验收权和目标责任需要分开。职责描述应做哪类工作；能力说明是否具备工具和环境；权限决定是否被允许执行；验收权决定谁能判断候选符合某个合同；目标责任则要求追踪尚未满足的交付目标。本文把流程复杂度描述为需要协调的角色、对象、阶段和判定边界，而不以消息数或节点数替代成本测量。W01 涉及分配职责，W02 涉及分支对象与发布阶段，Graph 涉及验收范围。（B01–B05）

MAST（多 Agent 系统失败分类）已区分角色规范违背、重复步骤、协调错位和验证不完整，并报告改善角色规范及高层验收的正向干预。它支持分析行为发生在哪个边界，却不支持预先把多角色当成故障。本文不移植其分类频率，也不把生产案例作为其基准的复现。[1]<!--ref:R01--><!--anchor:section:4--> 从过程分析出发，面向命令行接口（CLI）编码 Agent 轨迹的 Failure as a Process 强调区分错误起点、演进与恢复，同样的表面错误可能有不同机制，重复尝试未必无效。本文相应保留纠正前后，而不只读取最终状态。[2]<!--ref:R05--><!--anchor:section:II-->

模型—harness 边界研究提示，某些故障只有结合模型行为与运行机制才能解释；其问题报告也可能缺少上下文。因此，本文不把已排除的调度或 lease 故障用作流程复杂度的证据，外部 bug 不能填补本地机制缺口。[3]<!--ref:R03--><!--anchor:section:I--> 生产部署调查中，受约束的结构化流程、人类评估与可控自治是常见选择，可靠性仍是挑战；这构成“一切流程都有害”的实践反证，而非复杂度因果试验。[4]<!--ref:R04--><!--anchor:section:5.4, 7.1-->

AgentConductor 则在竞赛代码场景中按执行反馈更新分层有向无环图，并随任务难度调节图密度。其结果说明结构应按任务设计，图更稀疏或更复杂都不是固定目标；基准的准确率和 token 指标不能直接换算成本系统的消息或 generation 收益。[5]<!--ref:R29--><!--anchor:section:2.1.2, 3.2.2--> 本文因此只提出边界表示的改进，不由个别协调失误推出删去全部流程。

本文所说的责任闭合，因此不是要求每个角色都接受全部工作，而是要求一个尚未完成的事项能追溯到下一动作及有权采取该动作的主体。拒绝一个不合格候选仍可符合这一要求，前提是拒绝之后的未满足事项没有在交接中消失。（B07、B09）

## 3 材料与方法

研究使用同一部署的冻结消息、006 C07 既有片段、010 定向补查及 009 审查案例。010 在 26961 条消息上执行三组固定正则，得到 429 条去重命中，随后选取 11 个锚点及相邻窗口，保存并完整阅读 67 条不同原生消息；C07 十条原文另行复读，重叠记录不新增样本。这是目的性筛查，既不是全量故障标注，也不是随机样本。（B00、B03）

本篇重建“任务要求—当前解释—转交或等待—纠正—可见后续”的链条。W01 的首次拒绝和原先空 assignee 来自 PD 报告，没有恢复该次应用程序接口（API）响应；后续分配则核对原生初始 generation 的五个节点及系统 ready 消息。W02 逐条核对发送者、分支名和阶段，尤其不沿用旧摘要中对发布顺序与发言者的错误归纳。（B01、B02、B03）

结果分成三类：需要外部补动作或语义的协调片段；不同合同产生不同判定的验收片段；正常等待、报告完成和转述等边界案例。明显 inline/fork、REJECT 后 ready、旧 condition、依赖未消费及 lease 失联问题不进入设计负效应分析。六月 C07 仅讨论跨角色协作，不用于证明后来内部 Supervisor/executor 架构的风险。（B00）

原研究为单 AI 探索，本轮补查、引用核验、写作与审阅使用分工 AI，均尚未人工复核。本文实际读取原文并保存日志，未重跑历史测试、未恢复全部模型输入或版本。009 Graph 部分源码在旧研究中不可读，本文保留报告级证据；发布矩阵读取的是合同与持久报告，不是本次重新完成的部署验收。（B04、B07）

分析时不把一句“已理解”编码为职责已经正确落地，而继续检查它实际指向哪个分支、谁发出后续请求，以及是否有独立状态相符。也不把相同候选的两条通过消息编码为两个独立判断。阅读日志记录本篇实际使用的消息与快照；本文没有声称对所有词法命中都完成了深读或逐条标注。（B02、B03.2、B08）

表1与图1直接还原W01初始generation中的分配与依赖。五节点形成四条依赖边、最长依赖路径含五个节点，但这些结构量不是耗时或复杂度效应。四个不同assignee对应本片段的四种职能分工，并非四种配置role枚举。[制图底表与原生定位](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P1P2/data.json) · [源文件、字段与哈希](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P1P2/source-provenance.json)

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

## 4 案例与结果

### 4.1 前置条件清楚，但行动责任仍被悬置

8 月 28 日，用户先后要求启动 `plan-2a8bc24c`。16:28:21，PD 报告中心拒绝启动，理由是每个节点必须有 assignee，并说五节点均为空。它正确识别了问题，也意识到实现、迁移、独立验收、合并和 runtime 验收不能全部交给同一个角色。然而回复止于“需要先确定”负责人，形成了等待分配决定的姿态，没有在这次回复中把分配落实为自己的行动。（B01a–B01c）

16:31:16，用户指出返回信息已经足够明确，要求 PD 自行分配。16:31:57，PD 承认前一步处理不完整，报告实现与迁移归 dev1、独立验收归 tester1、合并归 integration、运行验收归 tester2。初始 `generation-709baba8` 在 16:31:48 保存的五个节点实际具有这些 assignee；同秒系统消息通知首任务 ready。这使后续分配有结构化记录佐证，而不只依赖 Agent 的自我更正。（B01d–B01f、B02）

这里可见的链条是：独立验收约束意味着不能任意合并角色，因此产生了分配选择；PD 将该选择表达为外部待确定事项；用户补上“由你完成分配”的动作绑定。用户没有在这条纠正中提供新执行器、仓库或验收结果，也没有取消独立验收。责任问题因而不应归为调度器异常，亦不需推断模型故意推诿。（B01c–B01e）

证据也没有呈现双方互相拒绝的循环。正常启动前提被报告为拒绝，未取回的 API 响应仍不能当作本次独立核验；初始 generation 证明后来的分配，不自动证明更早全部字段为空。后续按依赖等待则符合该计划结构。本文能够说协调者一度没有完成可行动前提，不能说没有用户就必然永久停摆。（B01、B02）

因此，本例批评的不是 assignee 门槛本身。它成功暴露了不能启动的前提，且后续分配保留了实现与验收分离。可见缺口在门槛到行动的转换：PD 没有把清晰错误解释成一个自己可以组织完成的准备步骤。若取消门槛，可能只是让未分配的计划进入运行状态，并没有回答谁负责工作；本研究没有测试也不建议用这种方式消除表面的拒绝。（B01c–B01e、B02）

![图2：W01动作责任纠正与后续记录](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p2-workflow-responsibility/figures/p2-fig2-action-timeline.png)

**图2。**W01动作责任纠正与后续记录。用户指令后31.295246秒保存带分配的generation，31.872839秒出现首任务ready消息。分别标识报告、指令、原生快照与系统通知；不将这些间隔称为人工干预效果或停摆时长。

图2把“稍后报告启动”与原生结构变化分开。用户此次补入的是由PD完成分配的动作绑定，未同时交付新执行环境或验收结果；这一具体差别支持责任解释，但不能识别无用户时的反事实结果。[制图底表与原生定位](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P1P2/data.json) · [源文件、字段与哈希](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P1P2/source-provenance.json)

### 4.2 同一角色口号，指向不同分支

6 月 16 日 01:39:41，用户限定 integration 只在开发期把开发分支合入迭代主干。integration 随后明确将验收编排与 ship 移交 PD。01:41:52，PD 同样表示理解，却在自己的复述中写“迭代主干（main）”，再要求 integration 把两条 dev 分支并入 main。表面上双方都接受角色边界，真正分歧却隐藏在主干一词所指的具体对象。（B03.1、B03.2、B03x）

用户于 01:45:51 再次说明三层分支：开发分支先由 integration 合到版本迭代主干，迭代主干到 main 才是 PD 的 ship 操作。这个纠正同时绑定了对象、阶段与行动者，补充了抽象“开发期合并”的遗漏。它不是让 integration 更积极越界，而是要求 PD 完成自己阶段内的合并责任。若只把下游坚持边界叫“扯皮”，就会把遵守刚确定职责误判为负面行为。（B03.3）

纠正后还出现第二层前提错误。PD 判断迭代主干不存在，要求从 main 重建；用户转贴这一指令。integration 核对远端后报告分支已存在，已经包含开发合并和验收证据，拒绝重复创建并说明可能覆盖既有证据。该核对纠正了上游交接的事实前提。因此这个片段既有职责映射失误，也有下游检查的正面价值，不能压缩成“所有人只会推给别人”。（B03.4–B03.6）

此后用户纠正构建、文档、tag、合 main 与清分支的顺序，PD 记录按纠正顺序发布。旧 C07-E07 摘要不能被扩写为 Task 提前 done 的证据；C07-E10 与 E09 都由 PD 发出，也不是 integration 的第二份独立确认。本文保留发布报告这一后续状态，但没有重验当时部署。两项修正避免把同一次叙述算成更多角色共同证实。（B03.7–B03.10）

表2保留W02中不同类型的补充，避免把所有往返都计成无效协调。十一条源记录中包含用户澄清、错误前提的转贴和下游核查，末两条发布说明均来自PD，不能算两个独立验收者。[制图底表与原生定位](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P1P2/data.json) · [源文件、字段与哈希](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P1P2/source-provenance.json)

**表2。W02的对象、责任与事实纠正（2026-06-16 UTC）**

| 时间 | 记录主体 | 记录内容 | 对判断的作用 |
|---|---|---|---|
| 01:41:52.993389 | PD报告 | 将迭代主干称为main，并交给integration合并 | 职责口号相同，目标分支仍被误映射 |
| 01:45:51.965601 | 用户 | 澄清开发分支→迭代主干→main及ship责任 | 补充对象、阶段与行动者 |
| 01:53:27.945879 | integration报告 | 核对后称迭代主干已存在，不应重建 | 下游事实核对具有正面作用；未新做Git复验 |
| 02:00:39.671089 | 用户 | 纠正构建、文档、tag、合main与清理次序 | 是发布操作顺序纠正，不是明确提前done |

### 4.3 局部通过不等于覆盖完整目标

009 Graph 中，Supervisor 对 `4d4ccf32` 报告跨页合并、evidence 计数和回归通过；Tester 随后对同一 Git 提交标识（SHA）拒绝，指出点击聚合边只请求一个 effect 的 endpoint，计数正确未证明两页证据都进入 drawer。开发与独立验收合同范围并不完全相同，不能把 PASS（通过）/REJECT（拒绝）当同一命题的直接矛盾。可见增量是新增了点击行为和共享、独有事件的覆盖条件。（B04、B05a、B05b）

后续 `782cbde3` 已检查两个 endpoint，却在同一 project 内构造测试；Tester 又指出跨 P1/P2 合并时请求共用了第一个 project scope。到 `1ba42c24`，Supervisor 与 Tester 的报告才都覆盖每个 effect 的 project 对应关系。这不是同一拒绝重复出现，而是对象范围逐步扩展。源码不可读限制了本篇独立复验，所述实现问题保持为参与者检查报告。（B05c–B05f）

该案例为责任闭合提供的是覆盖问题：谁负责把局部测试已经证明的事项，与产品目标尚未覆盖的事项连接起来。多个 PASS 不能累加成成功概率，最后未发现缺陷的检查也不因此无用。是否增加独立角色，应看其新增输入、视角或覆盖要求，而非其消息数量。（B04、B05）

### 4.4 有些停止与重复本来就是交付的一部分

Spark 发布报告 Task 的创建合同要求保存发布矩阵并给出可审计 GO（可发布）/NO-GO（不可发布），而不是保证 GO。监督消息接受报告，同时明确命令路由、类别不一致、缺输入及环境门槛等 NO-GO 原因；持久报告也保存实现 SHA、原始记录索引与 NO-GO。报告任务完成和产品尚不可发布由此可以同时正确。只有把前者替代后者，才会丢失目标责任；本例没有证明界面已经如此替代。（B07a–B07c）

009 四组近似消息则包含 Task 到 Plan 的结果同步及同 Task 补完成状态，未见相应新增产品检查。转述可以服务下游同步和审计，既不能算第二次独立验收，也不能凭文本相近估计浪费。另一个反例中，Agent 明确承担自身删除候选责任，却因缺生产证据而拒绝放行；需要讨论的是后续补证或回撤由谁继续做，而非要求它突破门槛。（B08、B09）

### 4.5 完整案例区间中的人工参与和响应

仅统计选编消息会漏掉协调行为。本轮以W01和W02原选编首尾消息为固定边界，重新只读查询同一conversation内的全部消息。W01区间为2026-08-28 16:19:40.531485至16:31:57.036572 UTC。W02为2026-06-16 01:39:41.396427至02:12:04.153627 UTC。W01原六条消息已覆盖该区间；W02原十一条选编之外另有十一条，其中五条来自用户。新增材料纳入本轮统计，旧选编不覆盖重写。两区间均由已知案例与结果报告界定，不能外推全系统人工干预率。

**表3。固定区间的全部消息与下一条Agent消息间隔**

| 区间 | 消息数 | 用户消息 | Agent消息／不同发送者 | 系统消息 | 用户消息至下一条Agent消息：中位秒数 [IQR] |
|---|---:|---:|---:|---:|---|
| W01 | 6 | 3 | 2／1 | 1 | 40.11 [37.69, 280.32]，n=3 |
| W02 | 22 | 9 | 13／6 | 0 | 110.77 [32.68, 210.30]，n=9 |

消息构成分别为3/6和9/22来自用户；这是两个选定区间的沟通构成，不是独立任务成功率。响应间隔定义为用户消息到同区间时间上下一条Agent消息，可能只是报告或承认，也可能仍包含错误解释。它不是确认完成、CPU／LLM推理时长、因流程停摆的时长或人工节省的时间；连续用户消息还可能指向同一条后续回复，不能把12个间隔当独立试验。

**表4。十二条用户消息的内容编码**

| 互斥主类别 | 消息数 | 实际补充内容 |
|---|---:|---|
| 启动／继续要求 | 5 | W01两次启动、W02一次ship及两次继续 |
| 责任、对象或顺序纠正／质询 | 5 | 谁分配、角色边界、错误交接质询、三层分支映射、发布顺序 |
| 转贴操作指令 | 1 | 重建分支请求；随后Agent以已存在分支的报告反驳 |
| 完成状态追问 | 1 | 是否已经合入main |

这12条用户消息没有提供新executor、仓库访问条件或独立运行测量；但其中的主体、分支映射和操作顺序是具体信息，不能因此称为“没有新信息”。五条纠正／质询显示本地协调负担的内容，转贴错误重建请求及Agent反证则说明人类不天然提供正确事实。W02的六个Agent发送者也不能直接解释为六层LLM调用。

完整区间会改变时间重建。例如三层分支纠正后，旧选编中的下一条PD消息在173.71秒后；补齐区间后可见更早的一条PD回复，间隔为32.68秒，但该回复仍未完整绑定分支对象。因此“回应更快”也不等于“纠正已完成”。该比较支持先恢复完整沟通序列，再讨论流程推进；不支持把数值差归因某项架构干预。[查询、原生28条消息与逐条编码](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/data/P2-interventions.json) · [方案及执行后增补](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/protocol.md)

### 4.6 沟通往返与依赖结构的定量分解

图3把W01/W02全部28条消息、相邻发送者及12个响应间隔共同展示。W01五对相邻消息中四对换了发送者；W02二十一对中十八对换了发送者。相邻关系只表顺序，不是回复关系或因果关系；它让用户—PD往返与一次性参与者可见，不能把发送者切换都称为扯皮。

**表5。两个固定区间的发送者切换**

| 区间 | 全部消息 | 相邻消息对 | 发送者不同的相邻对 | 未发生切换 |
|---|---:|---:|---:|---:|
| W01 | 6 | 5 | 4 | 1 |
| W02 | 22 | 21 | 18 | 3 |

![图3：完整区间沟通序列与响应分布](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p2-workflow-responsibility/figures/p2-fig3-coordination-sequences.png)

**图3。**（a–b）完整有界区间按发送者绘制，每点一条消息，菱形为用户；（c）W02相邻发送者计数，行是前一条、列是下一条，不意味着直接回复；（d）全部用户消息至下一条Agent消息的间隔，黑线为中位数，重合点可能遮挡且多个间隔可共用回复。区间由既有案例首尾确定，不是系统总体样本。

为避免只用“流程复杂”作定性判断，另对26个非显式夹具Plan的195个generation计算结构指标。所有快照的seq边子图均通过无环检查；顺序深度为该子图最长路径的边数，最大拓扑层为最长前置距离相同的节点数。跨负责人边采用两端assignee已知的全部边元组。根与时间最后记录配对，节点和边均为累计快照，不代表当时仍在工作的集合。

**表6。根／最后快照的配对结构中位数**

| 指标 | 配对Plan数 | 根记录 | 最后记录 |
|---|---:|---:|---:|
| 累计节点数 | 26 | 8 | 17.5 |
| 不同assignee数 | 26 | 6.5 | 6.5 |
| seq最长路径，边数 | 26 | 3 | 6 |
| 最大拓扑层节点数 | 26 | 3 | 6.5 |
| 跨assignee边占比 | 21 | 100.0% | 93.8% |

最后一行只取根与最后快照均有可比较边的21个Plan；不把零边Plan填为0%。不同负责人数中位数没有增加，而深度和拓扑层规模增加，说明“有多少成员”不足以概括工作流结构。93.8%也只是这些累计图的跨负责人连接程度，不是交接错误率。最大拓扑层尤其不是实际并发容量，因为其中可含已完成节点、其它边约束或相同assignee。

![图4：根与最后快照的配对复杂度](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p2-workflow-responsibility/figures/p2-fig4-topology-comparison.png)

**图4。**四面板分别比较不同负责人、seq深度、跨负责人边比例与最大拓扑层；每条细线连接同一Plan，黑线为该面板配对支持上的中位数。前三个计数指标及拓扑层各26对，跨负责人比例21对；拓扑层轴为对数。未进行随机干预或复杂度—失败回归。

这些结构结果使后续对照更具体：可分别改变下一动作表示、顺序依赖和交接边界，而不是把角色数当作单一复杂度。它们不证明复杂度造成停摆，也不将已知系统bug混入失效效果。[全部快照与配对指标](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/006-dense-evidence/data/P2-topology.json) · [消息序列底表](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/006-dense-evidence/data/P2-sequences.json)

## 5 讨论与设计含义

### 5.1 为什么角色清楚仍可能反复协调

W01 显示角色约束与行动约束并不是同一件事。“实现者不能验收自己”排除了一个不合格选择，却没有在 PD 的当次解释中生成一个合格分配。它知道要有负责人，却把选择负责人的工作留成待决定事项。W02 则显示，“integration 负责合并”如果不说明源分支、目标分支与发布阶段，就可能同时支持互不相容的行动。流程不是没有规则，而是规则仍需要模型临场完成语义绑定。（B01、B03）

这种悬空可以发生在两个层次。一个是动作所有者未确定：知道缺少 assignee，却没有确定谁填它。另一个是动作对象未确定：知道由 integration 合并，却混同迭代主干和 main。用户分别补上行动绑定和对象映射后，记录才出现相应更正。这是对已观察过程的解释，不是声称角色越多就越容易失败；同样的缺口也可能存在于只有一个执行者的接口中。（B01d–B01f、B03.3）

因此，“减少复杂度”若只变成减少角色数，未必针对这些片段。W01 后续保留了四个不同负责人的分配，W02 下游独立核查保住了已有分支和验收证据。需要减少的候选对象是必须反复通过对话才能解开的责任关系，而不是已经增加质量信息的独立工作。生产研究与动态拓扑研究同样提醒，结构的可控性和密度应服务具体任务，不能被预设为纯开销。[4]<!--ref:R04--><!--anchor:section:5.4, 7.1-->[5]<!--ref:R29--><!--anchor:section:2.1.2, 3.2.2-->（B02、B03.6）

### 5.2 以工作项闭合当前责任

本文建议为需要推进的事项同时记录目标 owner、下一动作、所需输入、行动权限和结束条件。这里 owner 不是一个孤立名字：在 W01 中，它应说明“PD 分配五节点，独立验收由非实现者承担，分配后重新启动”；在 W02 中，应说明“integration 从 dev 合入版本分支，PD 从版本分支合入 main”。这是从案例导出的候选表示，尚未实施对照。（B01、B03）

工具拒绝也可以返回可修复前提与相应动作，而不只返回状态标签。但不能将所有拒绝都自动恢复：有些需要外部权限或资源。区分标准应是缺少输入是什么、由谁有权提供、当前主体能否自行准备。W01 的用户只补了分配指令；缺生产窗口证据的 I05 却不能凭承责自行伪造。将两者都处理成“请用户继续催促”，会混淆协调缺口和真实外部依赖。（B01、B09）

对真正需要外部决策的事项，升级应保留一个可审查的请求：待决定的选项、影响、谁有权决定，以及当前还能推进什么。对已授权且具备能力的前提，则应明确由当前 owner 完成或说明具体缺口。这样的表示可以减少模糊的“等待确认”，但本文没有度量节省多少对话，更不把及时回应等同于无条件越权执行。（B01、B09）

### 5.3 把验收范围与目标责任一起保留

Graph 说明，收口状态应绑定候选、合同范围和判定者。计数检查、点击后两页取证、跨 project 取证是不同命题；它们可以形成累积覆盖，却不能只用同一个 PASS 标签代表全部目标。建议在交接中列明已证明项和未覆盖项，让后续 owner 知道自己接的是实现候选、局部验收结果还是产品级结论。（B05）

合格 NO-GO 报告尤其需要这种分层。报告任务可以完成，目标层仍保留未满足的发布条件及后续责任。如果把报告人持续保留为唯一修复者，也可能错误赋予其实现或生产权限；更合理的候选方案是将每个剩余义务交给有能力且获授权的主体，并保存与原报告的关系。本文没有证据断言现有系统已实现或遗漏了这套完整机制，这是一项设计推论。（B07、B09）

转述则应保持来源和用途，避免被后续读者误当新增验证。Task 结果传播到 Plan 可以只携带同一候选和结论的引用，再补后续调度信息；有新增检查时再明确新增范围。当前配对记录支持区别同步与验证，却不足以确定哪条消息可以删除，因为下游消费、审计需求和实际模型 turn 未恢复。（B08）

### 5.4 人类补偿应按内容分析

用户在 W01 补的是下一动作，在 W02 补的是分支语义；integration 补的是远端事实核对。这三类补偿不同于提供新凭据或不可取得的生产证据。记录人类到底补了什么，比把“用户介入”统一算失败更接近可改进机制。未来评估可固定任务和权限，比较显式工作项是否减少无谓澄清，同时观察独立验收覆盖是否下降；本研究没有进行这一比较。（B01、B03、B09）

这种评估还应区分信息复用与判断重做。相同结论被同步到另一会话，和同一检查被再次执行，具有不同的潜在收益与成本；现有消息无法直接完成这一划分。减少协调轮次可以作为候选指标，但须与事实检查是否保留、未满足义务是否可追踪一起解释。（B08）

## 6 局限

这些案例来自同一部署、同一冻结快照及目的性检索，不具有代表性抽样基础。原始语句中的“不能擅自”可能受未恢复的提示和授权历史影响；后续能够分配不证明此前所有前提完全相同。本文确认的是在可见交互中用户补了动作指令，不能据此估计全量人工依赖率、停止时长或无人介入时的结果。（B00、B01）

流程复杂度没有被操纵，角色数、节点数和消息数也未被校准为成本指标。W02 发生在后来内部 S/E 设计之前，不能据其解释内部身份架构；Graph 的报告级证据和不同验收合同，也不允许估计 Supervisor 误判率。明显运行故障始终排除，不以旧事故数量补强设计批评。过程研究提示保留起点与恢复差异，但其基准结论亦不能外推成本系统的成本。[2]<!--ref:R05--><!--anchor:section:II-->（B00、B04）

原研究与本轮 AI 分工均未经过人工复核，历史部署和测试未重跑。C07 两项摘要错误说明二手归纳会改变责任解释，本篇纠正也仍需要作者核对。四篇论文复用部分证据，不能累计为独立验证。下一阶段需要固定版本、授权和目标，在保留质量门槛的条件下比较责任表示；评价既包括进展，也包括错误放行与不当越权，不能只奖励更少等待。（B03、B07、B09）

本文也未证明用户总能提供正确纠正。C07 中用户转贴过错误重建指令，最终由 integration 核事实纠正；这限制了将人工介入统一视为可靠外部裁决的解释。（B03.5、B03.6）

## 7 结论

Agent 团队的反复协调，在所选记录中可以被还原为具体的责任悬空：正常前提被解释为外部待决定事项，同一职责口号没有绑定相同的分支对象和阶段，局部通过也不天然闭合产品目标。用户和下游角色通过补动作、补语义或核对事实使这些关系重新明确。这些观察比“模型像人一样推诿”更可检验，也保留了作者所关心的团队不推进问题。（B01–B05）

相应的设计方向是减少需要临场解释的责任关系，给工作项同时附上 owner、下一动作、输入、授权和验收范围。它不要求取消独立测试、否定 NO-GO 报告或删除所有转述，因为这些行为在记录中有质量控制和同步用途。更少角色、更少消息并非已证明更好；复杂度的净因果效果和改进收益仍待评估。（B05–B09）

当前可以成立的结论是：流程写出了“有哪些角色”，还必须回答“现在由谁对什么采取下一步”。把这一回答连接到真实权限、证据和结束条件，才可能让程序化流程帮助承担责任，而不把责任留给又一轮协调。（B01、B03、B07）

这一方向保留了流程的质量门槛，同时把可推进的准备工作落实到行动者；是否改善总体交付，仍应以固定条件下的过程和结果共同检验。（B01、B05、B07）

## 参考文献

[1] M. Cemri et al., “Why Do Multi-Agent LLM Systems Fail?,” arXiv:2503.13657v3, 2025. doi: [10.48550/arXiv.2503.13657](https://doi.org/10.48550/arXiv.2503.13657). [已核版本](https://arxiv.org/abs/2503.13657v3).

[2] X. Zhao, H. Li, S. Li, T. Zhao, E. T. Barr, F. Sarro, and H. Ye, “Failure as a Process: An Anatomy of CLI Coding Agent Trajectories,” arXiv:2607.09510v1, 2026. doi: [10.48550/arXiv.2607.09510](https://doi.org/10.48550/arXiv.2607.09510). [已核版本](https://arxiv.org/abs/2607.09510v1).

[3] J. Chen, S. Chen, H. Zhu, J. Cao, J. Shen, and S.-C. Cheung, “Understanding Agent-Reactive Bugs at the Model-Harness Boundary: An Empirical Study of LLM Agent Issue Reports,” arXiv:2607.15684v1, 2026. doi: [10.48550/arXiv.2607.15684](https://doi.org/10.48550/arXiv.2607.15684). [已核版本](https://arxiv.org/abs/2607.15684v1).

[4] M. Z. Pan et al., “Measuring Agents in Production,” arXiv:2512.04123v4, 2025. doi: [10.48550/arXiv.2512.04123](https://doi.org/10.48550/arXiv.2512.04123). [已核版本](https://arxiv.org/abs/2512.04123v4).

[5] S. Wang et al., “AgentConductor: Topology Evolution for Multi-Agent Competition-Level Code Generation,” arXiv:2602.17100v1, 2026. doi: [10.48550/arXiv.2602.17100](https://doi.org/10.48550/arXiv.2602.17100). [已核版本](https://arxiv.org/abs/2602.17100v1).

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

**数据可用性。** 使用既有冻结研究包，原生消息与合同按附录的本地路径可在授权环境内定位，实际阅读范围见 `read-log.json`。本地可查不等于公开发布或开放许可，公共共享与脱敏安排待数据持有人确认。

**伦理与隐私。** 记录含用户与 Agent 的协作内容。伦理机构、批准或豁免情况尚未由作者声明；本文不推断无需审查。公开前须由责任作者复核访问授权、可识别信息和必要脱敏，原生标识符（ID）供本地追溯。

**作者贡献。** 作者名单、单位、通讯作者及 CRediT 分工待作者确认，AI 不列为作者。

**利益冲突。** 待作者声明，未据资料缺失写作“无利益冲突”。

**资助。** 本研究未获得资助。

**AI 使用。** 001–009 原研究为单 AI 探索；010 本轮由分工 AI 补查、核验引用、写作并审阅。原研究及本轮结果均尚未人工复核，未重跑历史模型、测试或部署。历史模型配置未知处不补造，最终事实与论述需责任作者核查。

**同源与跨篇重合。** 四篇工作稿基于同一部署和冻结快照，存在目的性选例重合。本文的 Graph、Spark 与 I05 边界材料亦供其它篇目分析，但问题分别是责任闭合、监督或产物继承；同一消息保持原生标识，不累计为不同实验，不把跨篇一致性当独立重复验证。
