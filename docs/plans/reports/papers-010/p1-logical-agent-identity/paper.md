# 一个逻辑 Agent，两类执行体：Supervisor/executor 的身份混淆风险

## 中文摘要

为突破一个 Agent 只能串行执行任务、身份与执行进程绑定的限制，Agent Center 将常驻 Supervisor 与按任务创建、完成后退出的 executor 封装为一个逻辑 Agent，期望兼顾并行、按任务选模型和统一回写。本文先重建设计动机，再考察这一方案引入的身份混淆风险。本文把“身份认同”操作化为行动者、能力和交付责任的归属，基于 Agent Center 冻结记录开展探索性案例分析。8 月 6 日的主案例呈现了将 Task 状态恢复误当中心自动创建 executor、经用户指出启动主体后改述并报告显式 fork 的过程；7 月的片段提供责任外推表述，但其线程和工作区故障不计入设计负效应。历史身份契约表明责任统一与执行隔离可以并存，明确自有交付仍拒绝缺证据产物的反例，以及 Access、Spark 的语义检查，则说明监督可能保留实际价值。作为工程复盘，本文认为这种依赖模型维持内部角色与统一责任映射的方案不适合作为本部署的默认解法；这一评价基于可见责任断裂与人工纠正，不等于已隔离证明架构净收益为负。原研究与本轮补查均为人工智能（AI）探索性分析，尚未人工复核；本轮另有 AI 写作和审阅。本文提出将并发隔离与模型身份职责分开设计等建议，效果尚待检验。（证据 A00–A14）

**关键词：**逻辑 Agent；Supervisor/executor；责任归属；执行生命周期；agent harness

## Abstract

Agent Center packaged a persistent supervisor and task-scoped executors as one logical agent to overcome serial task execution and the coupling of identity to a single process. The design sought parallel execution, per-task model selection, and centralized writeback. We reconstruct this motivation before examining the resulting identity-mapping risk. This exploratory case study operationalizes identity as the attribution of actions, capabilities, and delivery responsibility. In an August 6 episode from Agent Center, an agent expected task-state recovery to create an executor automatically, then revised that interpretation after a user identified the responsible actor. Native task events corroborate state transitions, while executor creation remains a participant report. July episodes contain responsibility-shifting language, but thread and workspace failures are excluded from claims about architectural harm. A historical identity contract, a justified rejection of the agent's own artifact, and substantive Access and Spark reviews show that shared accountability can coexist with execution isolation and useful supervision. Our engineering assessment is that relying on the model to reconcile internal roles with unified accountability is a poor default solution for this deployment: selected episodes required human correction of responsibility attribution. This assessment does not isolate architectural causation or establish a negative net benefit. The underlying research and supplemental analysis are AI exploratory work, with additional AI writing and review in this round and no completed human verification. Separating concurrency and isolation mechanisms from model-facing identity duties is proposed but remains untested. (Evidence A00–A14)

**Keywords:** logical agent; supervisor/executor; accountability; execution lifecycle; agent harness

## 1 引言

Agent Center 引入 Supervisor 与临时 executor，首先是为了突破既有执行模型的限制。2026 年 6 月 28 日首次提交的并发设计草案记载，串行锁位于 Agent 粒度：同一 Agent 已在执行任务时，新任务会遇到 `agent_busy`，只能排队；中心工具连接的身份又在进程启动时固定，使一个身份与一个执行进程绑定。设计要解决的具体问题是：同一个长期 Agent 如何同时执行多个任务，并为不同任务选择不同模型。（A14 §1）

方案把长期身份与短期执行拆开。Agent 继续承载任务归属、配置与中心交互身份；一个常驻 Supervisor 负责接收消息、组织任务上下文、选择模型和启动执行；多个 executor 各做一项任务，在独立上下文和工作区中运行，完成后退出。执行结果通过文件交回 Supervisor，由其统一更新中心和记忆。初稿称常驻角色为 orchestrator／监工，本文沿用后续身份契约中的 Supervisor；这项术语对齐不表示各时点实现完全相同。（A14 §2–7；A01）

继续将这些执行体封装为一个逻辑 Agent，是为了在执行方式变化时保留稳定的中心交互主体。按该草案，执行单元可以增减、模型可以不同，但中心连接、配置和结果出口仍由同一身份承载；Supervisor 作为唯一写入者，旨在避免并行会话争写共享状态，并聚合同一问题在多个对话中的信息。临时 executor 则提供任务级隔离。这些是方案选择的理由，不是本文已经测得的吞吐、成本或交付收益。（A14 §3–8）

这一封装也给大语言模型（LLM）增加了需要理解的关系：当前会话负责控制，实际工作在另一个会话执行，但交付仍算自己的，内部执行失败后也不能只等“对方”交付。7 月 23 日的身份契约进一步明确 executor 不是外部承包者，隔离不转移责任。随后选编记录却出现 Supervisor 等中心创建自己的执行体、用户追问“你在等谁创建”后才更正归属的过程。需要复盘的正是：为并发而引入的分层，为何又成为自动推进中的责任解释问题。（A01、A04）

本文将“身份混淆”限定为行动者、能力和交付责任的误归属，不指意识或内在自我。相关研究已讨论角色规范与协调失败，本文不另造失败分类，而按“原始约束—方案选择—可见风险—设计评价”的顺序分析这一部署。[1]<!--ref:R01--><!--anchor:section:4--> 本文的工程判断是，这种依赖模型自行维持内部角色与统一责任关系的封装，不适合作为本部署的默认并发方案；后文用案例说明判断依据，也保留有效监督的反证。没有架构对照试验，不能把这一判断扩写为所有 Supervisor/executor 结构必然更差。（A04、A10–A14）

图1把原始并发需求、常驻协调入口和临时执行单元放在同一结构中；逻辑身份边界与执行隔离是不同层次。

![图1：逻辑 Agent 的设计结构](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p1-logical-agent-identity/figures/p1-fig1-architecture.png)

**图1。**逻辑 Agent 的设计结构。依据6月28日初稿A14与7月23日契约A01重建；三个executor仅为示意，数量不代表本研究观测。统一回写与执行隔离是预期机制，不是已测净收益。

## 2 相关研究与概念

本文区分五个概念。逻辑身份是中心记录权限、配置和任务归属的主体；模型会话是承载输入与生成的交互上下文；执行体是运行工作的一次进程或单元；权限边界决定可执行的操作；责任主体承担任务的对外交付。它们可以关联，却不能互换。历史契约将常驻 Supervisor 定义为控制面，将 executor 定义为隔离的任务执行单元，并明确进程隔离不转移责任。这是本研究的系统语义依据，不是模型已正确理解该语义的证明。（A01）

MAST（多 Agent 系统失败分类）将角色规范违背、重复步骤、上下文丢失、协调错位和验证不完整分别分析。该工作提示，相似的“等待”或“没有交付”可能落在不同问题中，不能从表面措辞倒推唯一原因。其角色规范和高层验收干预也具有正向结果，故角色数量本身不是负面判据。本文采用这一辨别要求，不迁移其发生率，也不将内部执行体直接视为另一独立 Agent。[1]<!--ref:R01--><!--anchor:section:4-->

模型—harness 边界研究进一步说明，故障可能产生于模型行为与工具、运行机制的相互作用，其引言包含子代理沿用编排者角色并继续派生的报告。这个例子支持调查角色解释与运行机制的接口，却不能证明所有分层架构都会混淆身份。尤其是本研究已经排除的线程和工作区故障，不能通过外部 bug 文献重新获得设计因果地位。[2]<!--ref:R03--><!--anchor:section:I-->

另一方面，CodeDelegator 分离长期规划者与短期 Coder，以结构化输入返回及执行状态隔离控制上下文污染，并在其基准中报告收益。它对“分离执行角色必然有害”构成直接反证；但其角色、通信和状态设计是联合变化，不能单独证明统一身份的效果。本文因此把评价问题落在具体边界是否被理解、审查是否增加证据，而非是否存在两个执行角色。[3]<!--ref:R14--><!--anchor:section:4.1, 5.2, 5.3-->

据此，本文把身份契约理解为关系约束：某个执行单元属于谁、某个控制动作由谁完成、某个产物由谁负责。这比模型是否会复述“我是 Supervisor”更严格。能够复述角色名而仍把执行创建归给中心，仍属于本文要研究的行为风险。（A01、A04）

## 3 材料与方法

材料来自 Agent Center 同一部署的冻结记录、006 的历史身份契约、009 的监督增量案例及 010 定向补查；本次修订另固定读取 6 月 28 日并发设计初稿的 Git 版本，作为动机证据，不将其视为已部署事实。（A14）补查在 26961 条消息上用三组固定正则得到 429 条去重词法命中，再定向选择 11 个锚点及相邻窗口，保存并阅读全文共 67 条不同原生消息。词法命中不是故障样本，窗口也不是随机抽样；本文使用其中与内部身份问题直接相关的片段，并保留原生标识符（ID）、协调世界时（UTC）时间和文件路径。（A00）

本文按“先前归属—用户纠正—后续表述或动作—独立可核记录”重建过程。消息支持参与者当时说了什么，动作表支持状态变化，固定源码支持特定实现差异；三者不能互相替代。8 月 6 日主案例定向核对九条 Task 动作，控制事件和执行镜像查询均为空。空结果不证明历史动作不存在，但使显式 fork、事件增长和运行状态仍停留在 Agent 报告层。（A04、A05）

纳入条件是能定位到行动者或交付责任的具体误归属；明显运行故障只作背景。7 月两个案例保留责任外推表述，排除损坏线程、缺工作树造成的交付后果。7 月 23 日的修订讨论单列为设计反思。反例采用明确承责仍拒绝产物的片段，以及能与源码对应的监督检查，以避免只选失败。（A02、A03、A06、A10–A12）

原研究是单 AI 探索；本轮补查、文献核验、写作和审阅由分工 AI 完成，均尚未人工复核。没有重跑历史模型、恢复全部提示或开展架构消融。阅读日志记录本次实际读取范围，外部文献采用冻结语料的允许主张与章节定位；本文的建议与观察结果分别陈述。（A00）

证据核对也区分“发出恢复计划”与“实现恢复”。例如一条回复同时出现认错、工具命令和交付承诺时，本文只把原始动作表能够支撑的部分提升为状态事实；后续 delivery 仍标为交付报告。这样既保留纠正的行为意义，也避免用参与者的肯定语气补齐缺失遥测。（A04c、A04e、A05）

表1给出材料规模与分析单位；筛查命中、消息和状态动作不能相加为独立失败样本。本轮从已保存原文提取时序并重算时间差，未新增模型试验。[制图底表与原生定位](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P1P2/data.json) · [源文件、字段与哈希](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P1P2/source-provenance.json)

**表1。P1材料与证据单位**

| 层次 | 规模 | 可支持内容与限制 |
|---|---:|---|
| 010检索消息 | 26,961条 | 检索范围，不是已逐条标注的案例 |
| 去重词法命中 | 429条 | 候选，不是故障数 |
| 选定窗口原生消息 | 67条 | 11个锚点的窗口；目的性选择 |
| I04原生Task动作 | 9条 | Task状态时序，不等于9次执行 |
| 表2对读片段 | 4个 | I01、I02、I04及反例I05，不估计发生率 |

## 4 案例与结果

### 4.1 等待中心创建“自己的”执行体

8 月 6 日 01:44:03，Task `task-e12e4fb9` 的 Agent 报告，reset 后任务回到 open，start 后成为 running，但没有新 execution，因此需要中心修复 dispatch。它没有把状态显示等同于真实工作已经发生：报告明确指出没有可工作的 executor。错误在于据此选择了等待外部修复，并再次 block，而不是定位哪个主体负责启动执行。01:45:22 用户直接追问 executor 是否应由 Supervisor 创建。（A04a、A04b）

01:46:29 的回复改变了责任归属。Agent 称自己误把解除阻塞、恢复状态理解为中心随后自动 fork，承认重做 reset/start 复制了这一判断，并报告已显式调用 `fork_executor`。该消息给出 command ID、新 execution ID 和启动时间，但这些标识在本研究中是消息内容，不是已恢复的原生命令事件。可见的结论是主体解释发生改变，并转向了具体启动动作；不应把这层结论扩写成全部执行细节均已独立验证。（A04c、A05）

Task 动作表提供较窄、但与叙述相容的交叉证据：01:43:21 reset，01:43:34 agent_started，01:44:03 blocked，01:45:52 unblocked。02:03:02 后续消息报告已有推送分支及交付 Git 提交标识（SHA）。这一序列把纠正放回了真实状态变化之中，却不证明首次等待的全部持续时间，也不证明最终任务已经完成；同一动作表稍后仍出现针对合同缺口的 block。因此，本文把它称为启动主体误归属及恢复过程，不称一次提醒消除了所有问题。（A04e、A05）

01:54:50 的进一步说明提出了两种容易混合的对象：Task 状态机与 executor 生命周期。Agent 自述 `fork_executor` 的说明明确要求显式 fork，而 reset 的“auto-assigned and re-dispatched”被它外推为自动建执行体。这为工具语义的局部歧义提供了候选解释，但仍是事后自述。无完整模型输入和部署版本，无法裁定统一身份、工具措辞、状态可见性或当次推理各自贡献多少。可以确定的是，单看 running 不能回答由谁完成了启动这一动作。（A04d）

主案例还显示，责任误归属不必表现为把任务交给一个有名字的外部 Agent。这里的被期待主体是“中心”，而用户纠正的是 Supervisor 应执行的动作。表面上 Agent 仍自称负责监管与验收，却把产生可监管对象的步骤排除在自己的行动清单之外。因而只检查它有没有宣称负责，会遗漏责任链在启动处的断裂。（A04a–A04d）

另一处值得保留的差别是，状态核验本身并非完全缺失：Agent 已经比较新旧 execution，知道执行没有增长。由此可见，追加更多状态信息未必独自解决问题；信息需要与有权执行恢复的主体和入口结合。这个解释比“模型没有看状态”更贴近原文，也为后续接口测试提供了具体对象。（A04a、A04d）

![图2：I04关键时序](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p1-logical-agent-identity/figures/p1-fig2-responsibility-timeline.png)

**图2。**I04关键时序。横轴按真实记录间隔绘制；形状区分原生Task动作、用户消息与Agent报告。用户纠正后29.639493秒出现unblocked，66.458005秒后出现显式fork报告；后者仍非取回的执行控制事件。02:05后仍有合同缺口导致的blocked，不能将这一窗口解释为最终交付。

图2使两个判断可分别检查：状态变化确有原生动作支持；执行创建和交付仍主要来自报告。记录间隔不衡量模型推理速度、架构导致的停摆或人工节省的时间。[制图底表与原生定位](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P1P2/data.json) · [源文件、字段与哈希](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P1P2/source-provenance.json)

### 4.2 两种责任外推，以及必须剥离的故障

7 月 22 日 07:20:47，另一片段将无新执行与损坏 thread 联系起来，要求平台 reset 或替代任务。用户指出 executor 由该 Agent 调度、属于它自身；Agent 随后承认将恢复失败误判为外部介入，并提出换工作树继续实现。这一来回支持责任归属被纠正，却不能证明 thread 损坏是身份设计造成的，也不能证明只需改变称谓便足以恢复。本文据此保留表述层证据，不把整段事故计为架构负效应。（A02a–A02c）

同日 16:48:13，建分支任务的承担者报告“交付未产生，无法通过验收”，请求提供仓库；用户纠正其是任务完成者，不能只等待验收他者交付。Agent 承认把 executor 失败当作外部阻塞。这比一般报告 blocker 更具体：争议涉及当前会话是否仍承担实现责任。但随后协调角色 PD（沿用原记录标签）报告该 Agent 的工作树配置关闭，executor 实际得到空目录，并明确说不改配置再 fork 仍会失败。承责不能生成不存在的仓库，这一物质前提使“认同自己后便完成”的解释不成立。（A03a–A03e）

两个片段共同呈现了交付责任外推的语言，却不具有相同的故障原因。尤其 I02 的“验收”既可能是必要质量判断，也可能遮蔽后续执行责任。分析的关键不是禁止 Supervisor 验收，而是检查它拒绝候选之后是否仍为修复、补证或正确升级指定行动。下一节的反例说明，自有产物与拒绝验收并不矛盾。（A03、A10）

### 4.3 设计意图与正面反例

7 月 23 日，开发者直接提出所有 Agent 都有身份认知问题，PD 将问题解释为把 executor 写成外部委托者，并记录提示、工具描述、界面和测试的修订任务。这里的“所有”是参与者观察，未经过全量审计；修订范围是行动意图，不是实际输入覆盖率。固定 identity contract 则明确了统一责任与隔离权限并存的设计。8 月再出现类似表述，不能反推 7 月修订已部署到当时输入，更不能单凭日期判定修订无效。（A06a–A06d、A01）

8 月 7 日的反例中，Agent 明确说 owner 是自己，executor 是同一 Agent 的执行单元，最终裁决责任也属于自己。它仍拒绝删除候选，因为缺生产窗口、迁移和消费者归零证据，不能假定证据存在。后续用户要求继续解决，Agent 提出补证或回撤不当删除。这说明“拒绝自己的产物”可以是正确承责的一部分；拒绝候选和结束修复责任需要分别判断，不能把 block 直接编码成身份混淆。（A10a–A10c）

009 两条链进一步呈现监督的可见增量。Access 审查指出缺 Risk 字段、删除确认预填且按钮初始启用，后版源码增加字段与空输入、禁用按钮及精确匹配处理。Spark 审查指出测试全绿只支持至少十二类，而合同要求二十五类；后版断言列出二十五个标识并核唯一数与总数。这里有具体问题、可操作修正与产物变化，超出重复转述“通过”。源码核对未替代浏览器和后端复验，但足以反对把 Supervisor 全部视为冗余角色。（A11、A12）

表2并列四个片段，保留可以分离的责任表述与必须排除的故障。它是目的性选例的证据矩阵，不把三段误归属与一段反例换算为失败率。[制图底表与原生定位](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P1P2/data.json) · [源文件、字段与哈希](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P1P2/source-provenance.json)

**表2。身份风险与反证的逐案边界**

| 片段 | 可观察责任关系 | 人工补充或正面证据 | 不计入设计负效应 |
|---|---|---|---|
| I01，7月22日 | 将内部恢复外推给平台 | 用户指出同一Agent的执行责任 | 线程损坏；后续计划不等于恢复已验 |
| I02，7月22日 | 将自身未完成任务描述为验收外部交付 | 用户纠正完成者定位；PD反对缺仓库盲重试 | 空工作区与配置问题；承责不能替代资源 |
| I04，8月6日 | 将Task恢复外推为中心自动建executor | 用户补充启动主体；Task动作支持时序 | 原生fork事件、实际prompt和版本缺失 |
| I05，8月7日 | 明确自身负责，仍拒绝缺证据候选 | 监督判断与统一责任可以并存 | 后续接管、权限变化与误提及混杂 |

### 4.4 定量筛选、敏感性与可估计量

为明确身份风险主张有多大的实证基础，本文对既有I01–I07七个目的性选编片段重算筛选流向，而不以检索命中数估计发生率。表3沿用既有分类：四个运行或配置混合片段不承担设计负效应，一项是设计自述；剩余为一个行为主例和一个明确承责的反例。七个片段中四个被排除，说明排除策略实质改变了负面证据的规模，不能继续用未筛选事故堆积架构批评。

**表3。身份材料的筛选与严格敏感性分析**

| 分组 | 片段数 | 片段 | 可用于哪种判断 |
|---|---:|---|---|
| 运行／配置混合，排除设计负效应 | 4 | I01、I02、I06、I07 | 仅保留可分离的责任语言；交付失败不计入架构后果 |
| 开发者观察与设计回应 | 1 | I03 | 设计动机与当时诊断，不是独立行为重复 |
| 行为主例，架构根因未识别 | 1 | I04 | 责任误归属与纠正过程；非架构因果估计 |
| 明确承责的反例 | 1 | I05 | 责任明确仍可合理拒绝自己的产物；后续混合交接不纳入 |

若进一步要求“架构根因有独立运行轨迹核验”，I04也不能作为已确认的因果负例，该口径下合格因果负例为0；这不等于架构没有风险，而是现有材料不能识别其发生率或效应。尤其不能把剩下的一主例一反例写成50%失败率，因为它们不是随机抽取、连续普查或等价任务比较。

只读快照汇总也说明测量边界：2221个Task中，1238个能关联动作日志（55.7%），共2527行动作；不存在孤立动作Task标识。但execution mirror和worker control event两表当前均为0行，不能由状态日志恢复executor创建、Supervisor实际输入或两层模型成本。这是本快照的记录可用性，不能断言历史上未执行，也不能将其余Task全部认定为未推进。本文由此给出的是有边界的行为案例研究；净吞吐、失败概率、身份提示干预效果与token节约仍不可估计。

筛选表、只读SQL、每项分母及敏感性处理见[定量分析005](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/README.md)、[P1筛选底表](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/data/P1-screening.json)和[快照可用性审计](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/data/snapshot-audit.json)。原始数据库沿用冻结备份清单，本轮没有重新哈希42 GB快照或重放历史执行。

### 4.5 动作记录的来源与责任指向

进一步审计全部2527条动作记录发现，597条由`system:awi-migration`重建。它们保留历史映射线索，但不能与实时动作等权计作当时的Agent行为。剔除这部分后，1930条非迁移动作来自927个Task，占全2221个Task的41.7%；另有311个Task仅有迁移记录，983个完全没有动作行。因此前节55.7%是“任何动作行”的覆盖率，不能当作非迁移行为覆盖率。没有记录也不能当作没有执行。

**表4。非迁移动作中的触发主体与所涉Agent**

| 动作 | 记录数 | 不同Task数 | 同一逻辑Agent | 另一逻辑Agent | 无法比较 |
|---|---:|---:|---:|---:|---:|
| agent_started | 819 | 778 | 818 | 0 | 1 |
| blocked | 453 | 269 | 453 | 0 | 0 |
| unblocked | 331 | 182 | 151 | 180 | 0 |
| retried | 10 | 6 | 5 | 5 | 0 |

比较先将冻结配置中的内部Agent ID和identity ID映射到同一逻辑实体，再比较`actor_ref`与`agent_ref`。前者是触发主体，后者是动作所涉Agent；两者不是Supervisor和executor的两个身份。819条started中818条可归一为同一Agent，说明审计归属并不普遍缺失；但这是字段层反证，不能证明LLM理解自己该启动谁。331条unblocked中180条由另一Agent触发，说明跨主体恢复在记录中实际存在，也不能统一解释为越权或责任外推。图3同时显示记录覆盖、动作分工及这组条件分布。

![图3：动作记录覆盖与责任指向](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p1-logical-agent-identity/figures/p1-fig3-action-attribution.png)

**图3。**（a）2221个Task按无动作行、仅迁移、存在非迁移动作互斥分组；（b）非迁移记录的角色配置×动作计数，颜色采用log(1+n)，格内为原数值，空格为0；（c）四类动作内actor与所涉Agent的关系，分母是各行动记录数。角色来自冻结配置，并非历史角色；start表示领域动作日志，不能等同executor进程启动。

这1930条记录仍包含运行恢复和未知故障，因此本节只分析记录来源、字段归属与控制结构，不把453条blocked或190条failed作为架构失败。程序可以可靠记录一个Agent，却仍没有恢复该模型当次所见的身份说明；案例中的语义误归属和表中的稳定标识由此可以并存。[全部原生动作与逐行归一](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/006-dense-evidence/data/P1-actions.json) · [字段含义的当前源码说明](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/006-dense-evidence/sources/task-action-field-semantics.txt)。该源码只解释字段，不反推历史部署版本。

## 5 讨论与设计含义

### 5.1 并发需求合理，身份封装并非合适的默认解法

回到设计起点，任务并行、按任务选模型、工作区隔离与共享状态有序写入，都是初稿明确面对的需求。但这些需求与“让常驻 LLM 监督自己名下多个临时执行者，并维持同一主体的责任理解”是两层选择。独立执行单元提供并发与隔离，唯一写入入口提供状态约束；是否还需要模型反复解释自己、执行者与中心的关系，应单独论证，不能把前者的合理性直接算作后者的收益。（A14）

I04 的问题不在于没有发现执行停滞，而在于发现之后仍把创建执行体归给中心；I02 则以验收外部交付的口吻描述自身未完成的任务，尽管空工作区这一实际故障必须另外排除。封装在中心侧维持了一个主体，并未保证模型在行动时维持同一责任链；用户仍需补上“谁应做下一步”的解释。这就是本文认为该方案不适合作为本部署默认解法的依据：为获得并发能力，又引入了需要人工修补的责任映射。（A03、A04）

这一评价不意味着并发能力没有实现，也不意味着总体收益已经测得为负。当前没有相同任务下的等价架构对照，更没有成本与吞吐的完整测量。CodeDelegator 的分工与状态隔离在自身基准中显示收益，反对把两类执行角色一概判为无用。[3]<!--ref:R14--><!--anchor:section:4.1, 5.2, 5.3--> 本文批评的是该部署把运行分层交给模型解释、却未可靠闭合行动责任的方案，而非所有执行隔离或语义审查。

因此，候选改进是保留任务级执行、权限隔离与有增量的审查，将确定性的调度和状态关系尽量交由明确接口或程序表达。I05 提醒，统一责任也不能消除真实权限边界：负责自己的交付，仍应拒绝缺少生产证据的候选。下一节提出的简化方向尚未验证，不是本文已经证明更优的替代架构。（A01、A10、A14）

### 5.2 把抽象改成当前可执行的说明

基于 I04，本文建议在模型可见状态中并列呈现当前模式、责任主体、可用动作、活动 execution 和下一步条件。例如恢复 Task 后，响应应明确“状态已恢复；是否已创建新的执行单元；由哪个主体执行哪个动作”。这是候选接口设计，不是已部署有效的修复。其目的不是增加身份宣言，而是避免模型从 running 或“重新派发”这样的词自行推导不存在的生命周期动作。（A04、A05）

对确定性关系，可以考虑用程序检查替代临场推理：启动成功的报告应关联可核执行标识，状态变化与执行创建分开记账；缺少启动证据时，返回允许的恢复入口及其权限要求。但程序化不能简单写成“没有 execution 就自动 fork”，因为旧执行是否存活、任务是否具备工作区等前提仍需判定。I02 显示盲重试可能保持原来的能力缺口。候选改进应围绕具体前置条件，而非用更积极的语气替代资源准备。（A03、A04）

责任表达也应跨提示、工具结果和审查入口保持一致。历史契约已经列出这些消费者，但记录未恢复每次实际加载的版本，故不能声称增加一条系统提示足够。可检验的要求是：无论从 Task 通知、恢复响应还是 executor 结束消息进入，同一会话都能识别自己仍承担什么、当前可采取什么，以及哪一项需要外部授权。统一术语只是入口，能否选择正确下一动作才是验收对象。（A01、A06）

### 5.3 监督的价值由新增判断体现

Access 与 Spark 表明，监督可以把“代码已经推送”推进到“当前证据是否覆盖合同”的判断。这种增量与内部执行者是否拥有独立人格没有关系，也不需要把两次输出当两个独立 Agent 投票。应保存候选标识、所查要求、发现的缺口和后续变化，才能判断审查是否增加了有用信息，而非仅数审查层数或 PASS（通过）次数。（A11、A12）

相应地，拒绝决定应连到后续责任。发现缺口后，系统可以要求记录修复动作、owner、输入和升级条件；对于超出权限的事项，则记录具体证据来源与请求。这一建议来自 I02 与 I05 的差别：前者的能力缺口不能靠承诺抹去，后者的门槛不能因自己负责而降低。设计目标是让责任可持续追踪，而不是迫使所有任务无条件完成。（A03、A10）

后续验证可围绕这条映射链构造同任务对照：保持执行隔离与验收合同不变，仅比较当前角色说明、工具响应中的责任指引和状态投影。评价不应只看最终完成率，还应记录是否选择正确启动入口、是否把未授权事项正确升级、是否在拒绝候选后保留修复责任。这里列出的是未来评估维度，本文没有实施该试验，也没有预测数值收益。（A03、A04、A10）

同样，若将某个确定性动作移给程序，仍应比较迁移前后保留了什么判断。Access 的字段合同核对与 Spark 的需求覆盖解释不同于重复读取相同状态，它们可能仍需语义判断。可简化的不是带有 Supervisor 标签的所有动作，而是已有充分结构化前提、却反复要求模型重新推导的操作关系；当前证据仅支持检验这种划分。（A11、A12）

## 6 局限

本研究是单部署、目的性选例的探索，不能估计身份混淆的频率、持续时间或相对其它架构的风险。词法筛查受用语影响，窗口限制可能遗漏更早授权与后续修复；参与者在被纠正后给出的解释也可能是合理化，而非可靠的内部原因报告。因而 I04 的强项是可见归属变化与状态时序相互关联，弱项是没有隔离产生该变化的机制。（A00、A04、A05）

历史提示、模型参数和部署版本未完整恢复，控制事件与执行镜像的空结果也限制了生命周期核验。本文没有单会话等价对照，没有测认知负荷、token 成本或“增强身份”干预效果。I01/I02 的运行问题已从设计负效应剥离；这减少了可主张范围，却不能保证剩余表述完全不受工具语义或其它状态影响。模型—harness 研究提供的是解释警觉，而非本项目的因果替代证据。[2]<!--ref:R03--><!--anchor:section:I-->（A02–A05）

009 的源码核对支持局部修正，未复跑历史浏览器或生产验收。原探索、本轮补查和 AI 写作审阅均尚未人工复核，引用核验也不是系统综述。四篇工作稿复用同一部署与部分案例，其相互引用不增加独立样本。进一步研究需要在固定任务、权限和版本条件下比较接口映射，并同时观察正确启动、合理升级及审查增量。（A00、A11、A12）

此外，用户的纠正不是受控实验处理；用户同时可能改变任务优先级和注意焦点。纠正后的行动变化不能自动归因于“自身”一词，本文仅确认这次交互补上了具体主体解释。（A04b、A04c）

## 7 结论

Supervisor 与临时 executor 被封装为一个逻辑 Agent，源于具体工程需求：突破 Agent 级串行执行，支持按任务选模型，并在并发时保留统一的中心交互与结果回写。临时执行单元解决的是执行粒度问题；把它们与常驻监督会话视为同一责任主体，还要求模型理解跨会话的行动、能力和交付关系。两者不能因同属一个封装就被当作已经同时解决。（A14、A01）

所选记录呈现了等待中心创建自身执行体、把未完成任务当作外部交付验收，以及用户纠正后的归属变化。基于这些责任断裂，本文认为依赖模型自行维持上述身份关系的封装，不是本部署解决并发需求的好默认方案。这个工程判断不计入线程损坏、空工作区等明显故障，也不等于已测得架构净收益为负。（A02–A05）

有效监督仍应保留：Access、Spark 的具体检查及对缺证据产物的拒绝都有依据。更值得检验的方向，是分别设计并发执行、权限隔离、审查和责任接口，减少模型为了完成任务而必须自行解释的内部主体关系。该替代方向尚无效果对照，不能作为已验证结论。（A10–A12）

## 参考文献

[1] M. Cemri et al., “Why Do Multi-Agent LLM Systems Fail?,” arXiv:2503.13657v3, 2025. doi: [10.48550/arXiv.2503.13657](https://doi.org/10.48550/arXiv.2503.13657). [已核版本](https://arxiv.org/abs/2503.13657v3).

[2] J. Chen, S. Chen, H. Zhu, J. Cao, J. Shen, and S.-C. Cheung, “Understanding Agent-Reactive Bugs at the Model-Harness Boundary: An Empirical Study of LLM Agent Issue Reports,” arXiv:2607.15684v1, 2026. doi: [10.48550/arXiv.2607.15684](https://doi.org/10.48550/arXiv.2607.15684). [已核版本](https://arxiv.org/abs/2607.15684v1).

[3] T. Fei, C. Chen, Y. Pan, M. Zheng, and M. Song, “CodeDelegator: Mitigating Context Pollution via Role Separation in Code-as-Action Agents,” arXiv:2601.14914v1, 2026. doi: [10.48550/arXiv.2601.14914](https://doi.org/10.48550/arXiv.2601.14914). [已核版本](https://arxiv.org/abs/2601.14914v1).

## 证据附录

正文 A 编号是项目原始材料定位，不与外部文献编号混用。消息仅证明可见表述，Task 事件证明对应状态动作，源码静态阅读证明指定位置的实现差异。以下时间均为 UTC；未重新执行历史测试。

| 编号 | 用途 | 原生 ID / 类型 | UTC 时间 | 原文路径 |
|---|---|---|---|---|
| A00 | 010检索范围、案例筛选、排除与原始材料导航 | 文档/源码 | 不适用 | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/findings.md) |
| A01 | 2026-07-23历史身份契约 | 文档/源码 | 不适用 | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/sources/agent-supervisor-executor-identity-contract.md) |
| A02a | I01恢复失败与外部介入表述 | `01KY4B5KS3272X3AMXDXHQMEC0` | 2026-07-22T07:20:47.651234Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY4B5KS3272X3AMXDXHQMEC0.json) |
| A02b | I01用户纠正 | `01KY4B74V6XGRFG85EV29RS1RT` | 2026-07-22T07:21:37.894896Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY4B74V6XGRFG85EV29RS1RT.json) |
| A02c | I01责任更正与行动计划 | `01KY4B7K5QXK434NNHY9K1FKEF` | 2026-07-22T07:21:52.567652Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY4B7K5QXK434NNHY9K1FKEF.json) |
| A03a | I02交付验收表述 | `01KY5BMKX9ZM03BTJTENTQSKDB` | 2026-07-22T16:48:13.737573Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY5BMKX9ZM03BTJTENTQSKDB.json) |
| A03b | I02用户纠正完成责任 | `01KY5BYKS2T49YEXAGRPQQ9XWN` | 2026-07-22T16:53:41.282145Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY5BYKS2T49YEXAGRPQQ9XWN.json) |
| A03c | I02承认责任外推 | `01KY5BZ3804ZA0ETH34RA1XEH1` | 2026-07-22T16:53:57.120322Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY5BZ3804ZA0ETH34RA1XEH1.json) |
| A03d | I02 PD报告空工作区与配置 | `01KY5C4XXCTE2AGYB373M42A9Y` | 2026-07-22T16:57:08.268484Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY5C4XXCTE2AGYB373M42A9Y.json) |
| A03e | I02 PD反对未改配置盲重试 | `01KY5C5HW683A4YZG91P23RC7G` | 2026-07-22T16:57:28.71098Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY5C5HW683A4YZG91P23RC7G.json) |
| A04a | I04等待中心修复dispatch | `01KZABVT594GFNYTWZ7AZRHF30` | 2026-08-06T01:44:03.497609Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZABVT594GFNYTWZ7AZRHF30.json) |
| A04b | I04用户询问启动主体 | `01KZABY7HSVA1FF42EH7XYX7GX` | 2026-08-06T01:45:22.745154Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZABY7HSVA1FF42EH7XYX7GX.json) |
| A04c | I04责任更正及fork报告 | `01KZAC08EKE041V1XB1C2XWE5V` | 2026-08-06T01:46:29.203159Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZAC08EKE041V1XB1C2XWE5V.json) |
| A04d | I04工具语义与状态/生命周期事后解释 | `01KZACFHV3BFM5CP04CR07Q79W` | 2026-08-06T01:54:50.339993Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZACFHV3BFM5CP04CR07Q79W.json) |
| A04e | I04后续delivery报告 | `01KZACYJWJHHWTFHEWE7EHAVH8` | 2026-08-06T02:03:02.930448Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZACYJWJHHWTFHEWE7EHAVH8.json) |
| A06a | I03开发者观察 | `01KY68C1K5SHCFA4SFNZSS7C4C` | 2026-07-23T01:10:21.541167Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY68C1K5SHCFA4SFNZSS7C4C.json) |
| A06b | I03 PD设计诊断 | `01KY68TD24Y07FCY5CZY56JEDZ` | 2026-07-23T01:18:12.036841Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY68TD24Y07FCY5CZY56JEDZ.json) |
| A06c | I03修订请求 | `01KY699NV8C4KRZ8DNXC4858KM` | 2026-07-23T01:26:32.552729Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY699NV8C4KRZ8DNXC4858KM.json) |
| A06d | I03修订任务范围 | `01KY69AW978C5TVS6GBM5TXK6N` | 2026-07-23T01:27:11.911017Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY69AW978C5TVS6GBM5TXK6N.json) |
| A10a | I05明确自有交付责任 | `01KZDJ41E4XMR6RSRDW2342DCE` | 2026-08-07T07:31:07.844252Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZDJ41E4XMR6RSRDW2342DCE.json) |
| A10b | I05缺生产证据而拒绝自己的候选 | `01KZDKN7K7XRNZ0DPE7G9V0GQA` | 2026-08-07T07:57:59.783637Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZDKN7K7XRNZ0DPE7G9V0GQA.json) |
| A10c | I05补证或回撤计划 | `01KZDKS2M645VTZJ9GR8SW0MDJ` | 2026-08-07T08:00:05.766163Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZDKS2M645VTZJ9GR8SW0MDJ.json) |
| A05.1 | Task动作：agent_started | `01KZ8WKNMFVY3YXBYYZ5R7CJC7` | 2026-08-05T11:58:13.645778Z | [动作原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/pm_task_action_logs-selected.json) |
| A05.2 | Task动作：blocked | `01KZ8XR3C69NRCX6KDFWCQJ0WX` | 2026-08-05T12:18:07.366266Z | [动作原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/pm_task_action_logs-selected.json) |
| A05.3 | Task动作：unblocked | `01KZ966KGSZCPSZF17W4AZBMQ4` | 2026-08-05T14:45:51.255316Z | [动作原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/pm_task_action_logs-selected.json) |
| A05.4 | Task动作：reset | `01KZABTH2T53KGBM29XXJZTX0Z` | 2026-08-06T01:43:21.433874Z | [动作原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/pm_task_action_logs-selected.json) |
| A05.5 | Task动作：agent_started | `01KZABTY199NCZQG05NWKXC3NX` | 2026-08-06T01:43:34.694818Z | [动作原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/pm_task_action_logs-selected.json) |
| A05.6 | Task动作：blocked | `01KZABVT5A8ZQK8JZE7ZPEE524` | 2026-08-06T01:44:03.497908Z | [动作原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/pm_task_action_logs-selected.json) |
| A05.7 | Task动作：unblocked | `01KZABZ4G4PRHBPYRZBSQXB5SR` | 2026-08-06T01:45:52.384647Z | [动作原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/pm_task_action_logs-selected.json) |
| A05.8 | Task动作：blocked | `01KZAD3AABJJ32SB0CSHQSZJ6Y` | 2026-08-06T02:05:37.994988Z | [动作原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/pm_task_action_logs-selected.json) |
| A05.9 | Task动作：unblocked | `01KZAM0BDXPP3YVCZP2CN3SEHT` | 2026-08-06T04:06:20.857687Z | [动作原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/pm_task_action_logs-selected.json) |
| A05 | 定向查询空结果，不作不存在历史执行的证明 | 文档/源码 | 不适用 | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/worker_control_events-selected.json) |
| A05 | 定向查询空结果，不作不存在历史执行的证明 | 文档/源码 | 不适用 | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/agent_execution_mirrors-selected.json) |
| A11a | Access具体拒绝要求 | `01M0QKRYCMG9X9JWGJ1TWCYRVV` | 2026-08-23T15:28:07.572206Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S01-03.json) |
| A11b | Access修订后通过报告 | `01M0QM59RA181QMKVGFFYGHDR9` | 2026-08-23T15:34:52.426323Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S01-05.json) |
| A12a | Spark测试绿但覆盖不足 | `01M1EBNQ71F9HZF9QZ4DVVMXJ6` | 2026-09-01T11:29:05.249479Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S02-02.json) |
| A12b | Spark二十五类覆盖通过报告 | `01M1EH0A4N0RMSPQ2C1ZJHCHX1` | 2026-09-01T13:02:15.189491Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S02-04.json) |
| A11c | Access前版字段与删除确认 | 文档/源码 | 不适用 | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/sources/access-before/access-ram-roles.html) |
| A11d | Access后版字段与删除交互 | 文档/源码 | 不适用 | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/sources/access-after/access-ram-roles.html) |
| A11e | Access新增静态断言 | 文档/源码 | 不适用 | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/sources/access-after/verify.mjs) |
| A12c | Spark旧至少十二类断言 | 文档/源码 | 不适用 | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/sources/spark-before/App.test.ts) |
| A12d | Spark新二十五类断言 | 文档/源码 | 不适用 | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/sources/spark-after/App.test.ts) |
| A13 | 009逐案报告与源码/核验范围 | 文档/源码 | 不适用 | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/reports/exact/agent-harness-review-increment-cases-009.md) |
| A14 | 2026-06-28并发设计初稿：原始约束与预期机制；设计意图，非部署效果 | Git `28bb445e8dd5aa9535bf8b4826019f7fa0322f9d`；§1–8、§11–12 | 首次提交 2026-06-28T07:29:47Z | [固定原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/002-p1-design-origin/sources/agent-concurrent-execution-first.md) · [版本与哈希](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/002-p1-design-origin/sources/provenance.json) |

## 声明

**数据可用性。** 本稿使用既有冻结研究包，原生消息与代码片段按附录路径可在授权本地环境中查阅，阅读范围见同目录 `read-log.json`。这些本地路径不代表公共数据仓库或开放许可。对外共享范围、脱敏方式和许可仍需数据持有人确认。

**伦理与隐私。** 材料包含用户和 Agent 协作记录。伦理审查机构、批准或豁免状态尚未由作者声明；本文不声称已获批准或无需审查。公开前应由责任作者复核可识别信息、访问授权与必要脱敏，原生标识用于本地可追溯性。

**作者贡献。** 作者名单、单位、通讯作者及 CRediT 角色待作者确认；AI 不被列为作者。

**利益冲突。** 待作者声明，不据缺少信息推断不存在利益冲突。

**资助。** 本研究未获得资助。

**AI 使用。** 既有研究为单 AI 探索；010 本轮使用分工 AI 进行补查、引用核验、起草与审阅。本文未重跑模型实验，未知历史模型和运行参数不予补造。原研究与本轮工作均尚未人工复核，责任作者需核查事实、引用与最终论述。

**同源与跨篇重合。** 本文与责任悬空、角色上下文及 有向无环图（DAG）演化工作稿来自同一部署和冻结快照，复用部分 006–009 与 010 证据；尤其 I05 和监督案例也用于边界分析。复用记录保持原生标识，不累计为新增独立样本，不宣称四篇相互独立验证。
