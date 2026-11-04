# Supervisor–Executor 架构中的身份抽象与责任归属

*Identity Abstraction and Responsibility Attribution in Supervisor–Executor Architectures*

**作者：** oopslink

**作者主页：** [https://github.com/oopslink](https://github.com/oopslink)

## 中文摘要

为支持并发执行与统一回写，Agent Center将常驻Supervisor与临时executor封装为一个逻辑Agent。本文结合冻结协作记录、历史源码和受控动作实验，分析统一归属如何转化为跨会话的行动责任。案例中出现启动责任被归给中心的表述，但历史派发协议变更与工具说明缺陷使其不能直接证明架构造成停摆。为区分表述与上下文因素，本文在相同权限和状态信息下，运行两模型、16条件、8情境、2呈现版本的512次动作选择，并开展32次默认预期诊断。工具说明一致且无旧规则时，两模型均为64/64首次到达控制边界，统一身份与组件表述的配对差异为0。全部条件下，Sonnet为256/256，Haiku为253/256；三个未首步启动的输出均处于旧历史条件，选择了额外的合法准备步骤，事后反馈观察均在下一步启动。默认诊断全部回答主体未指定。因此，本测量未检出统一身份的独立不利效应，也未复现身份误归属或停摆；训练结构冲突仍未获得因果支持。结果提示应将身份、动作顺序与长期失效分开测量，保留明确的责任协议和有效监督。实验为合成短题，不代表生产交付，全部分析尚未人工复核。（证据A00–A19）

**关键词：**逻辑 Agent；Supervisor/executor；责任归属；执行生命周期；agent harness

## Abstract

Agent Center combined a persistent supervisor with temporary executors under one logical agent to support concurrent execution and unified writeback. We examine responsibility attribution using frozen collaboration records, historical source code, and controlled action-selection experiments. A historical episode attributed executor creation to the center, but changes in dispatch policy and inconsistent tool descriptions prevent attributing its stall to the architecture. We conducted 512 action selections across two models, 16 conditions, eight synthetic scenarios, and two presentation variants, plus 32 incomplete-protocol probes. With consistent descriptions and no legacy rules, both models reached the specified control boundary in 64/64 first actions; the paired difference between unified-agent and component framings was zero. Across all conditions, Sonnet reached 256/256 boundaries and Haiku 253/256. All three non-progressing first actions occurred with legacy history and inserted a legal preparation step. In a separately labeled post hoc continuation, each enqueued an executor on its next action. All incomplete-protocol probes answered that the actor was unspecified. These tests detected no independent disadvantage of unified identity framing and did not reproduce actor misattribution or stalling. They do not identify a training-induced conflict. The findings distinguish identity attribution, action sequencing, and sustained failure, while motivating explicit operational responsibility. The synthetic short tasks do not establish production delivery performance, and the analyses have not been human-verified. (Evidence A00–A19)

**Keywords:** logical agent; supervisor/executor; accountability; execution lifecycle; agent harness

## 1 引言

Agent Center 将常驻 Supervisor 与临时 executor 封装为一个逻辑 Agent，起点是并发执行的工程需求。早期设计中，串行锁位于 Agent 粒度，同一 Agent 只能排队处理任务；中心连接的身份又在进程启动时固定。设计希望在保留长期任务归属的同时，让同一个 Agent 并行处理多个任务，并按任务选择模型。（A14）

这一方案将中心交互与具体执行分开。Supervisor 接收消息、组织上下文、选择模型并启动执行；executor 在独立上下文和工作区中完成单项任务，结束后退出。结果交回 Supervisor，由其统一更新中心和记忆。逻辑 Agent 则继续承载配置、身份和交付责任。唯一写入入口旨在约束并发状态更新，临时执行单元提供任务级隔离，图1展示了这些设计关系。（A14 §2–8；A01）

分离进程以后，模型还需要理解另一组关系：当前会话负责控制，具体实现发生在其它会话，而后续修复和最终交付仍由同一逻辑 Agent 承担。历史身份契约明确规定，执行隔离不转移交付责任。选编记录却出现了已识别执行缺失、仍等待中心创建 executor，以及将自身任务描述为等待外部交付的表述。本文考察这一责任映射为何需要用户介入纠正。（A01、A03、A04）

本文把“身份混淆”限定为行动者、能力和交付责任的误归属，不涉及模型的意识或内在自我。相关研究已有角色规范与协调失败分类。[1]<!--ref:R01--><!--anchor:section:4--> 本文进一步比较三层解释：逻辑身份抽象是否诱发关系混用，架构和上下文是否充分表达行动协议，以及模型既有行为模式是否与外部结构不匹配。历史源码补查揭示了派发协议变更与工具说明缺陷；因此研究重点是区分这些机制及其证据，不能用混合案例直接判定统一身份封装无效。（A04、A14–A17）

![图1：逻辑 Agent 的设计结构](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p1-logical-agent-identity/figures/p1-fig1-architecture.png)

**图1。**逻辑 Agent 的设计结构。依据6月28日初稿A14与7月23日契约A01重建；三个executor仅为示意，数量不代表本研究观测。统一回写与执行隔离是预期机制，不是已测净收益。

## 2 相关研究与概念

本文区分五个概念。逻辑身份是中心记录权限、配置和任务归属的主体；模型会话是承载输入与生成的交互上下文；执行体是运行工作的一次进程或单元；权限边界决定可执行的操作；责任主体承担任务的对外交付。它们可以关联，却不能互换。历史契约将常驻 Supervisor 定义为控制面，将 executor 定义为隔离的任务执行单元，并明确进程隔离不转移责任。这是本研究的系统语义依据，不是模型已正确理解该语义的证明。（A01）

MAST（多 Agent 系统失败分类）将角色规范违背、重复步骤、上下文丢失、协调错位和验证不完整分别分析。该工作提示，相似的“等待”或“没有交付”可能落在不同问题中，不能从表面措辞倒推唯一原因。其角色规范和高层验收干预也具有正向结果，故角色数量本身不是负面判据。本文采用这一辨别要求，不迁移其发生率，也不将内部执行体直接视为另一独立 Agent。[1]<!--ref:R01--><!--anchor:section:4-->

模型—harness 边界研究进一步说明，故障可能产生于模型行为与工具、运行机制的相互作用，其引言包含子代理沿用编排者角色并继续派生的报告。这个例子支持调查角色解释与运行机制的接口，却不能证明所有分层架构都会混淆身份。尤其是本研究已经排除的线程和工作区故障，不能通过外部 bug 文献重新获得设计因果地位。[2]<!--ref:R03--><!--anchor:section:I-->

另一方面，CodeDelegator 分离长期规划者与短期 Coder，以结构化输入返回及执行状态隔离控制上下文污染，并在其基准中报告收益。它对“分离执行角色必然有害”构成直接反证；但其角色、通信和状态设计是联合变化，不能单独证明统一身份的效果。本文因此把评价问题落在具体边界是否被理解、审查是否增加证据，而非是否存在两个执行角色。[3]<!--ref:R14--><!--anchor:section:4.1, 5.2, 5.3-->

据此，本文把身份契约理解为关系约束：某个执行单元属于谁、某个控制动作由谁完成、某个产物由谁负责。这比模型是否会复述“我是 Supervisor”更严格。能够复述角色名而仍把执行创建归给中心，仍属于本文要研究的行为风险。（A01、A04）

训练来源需要单独检验。AgentTuning以交互轨迹微调模型，说明agent行为会受训练数据影响；Instruction Hierarchy通过训练塑造对不同消息来源的优先级处理。[4]<!--ref:R30--><!--anchor:section:2.2, 3.3, 3.4-->[5]<!--ref:R31--><!--anchor:section:3, 4--> 两者提供行为可学习的依据，却没有验证本系统模型的训练内容或Supervisor–Executor冲突。本文将该解释保留为待检验假说，并与外部提示和历史上下文分开。

## 3 材料与方法

### 3.1 历史案例与源码

本研究对同一部署的冻结记录开展目的性案例分析，结合历史身份契约、设计初稿和监督检查案例，分别考察设计意图、责任解释与后续动作。定向检索与阅读的范围见表1，精确记录、检索过程和复算入口见[补充附录](evidence-details.md)。设计文档用于重建方案由来，不能替代历史部署状态。（A00、A01、A14）

过程重建以“原先的行动责任解释—用户纠正—后续表述或动作—原生记录核对”为顺序。消息用于识别参与者的判断，Task 动作用于确认状态转换，固定源码用于核对指定产物的变化。执行体创建若只有 Agent 报告，始终保留为报告级证据。主案例的控制事件和执行镜像未能恢复，因此能够分析责任解释的变化，无法独立确认全部启动细节。（A04、A05）

纳入分析的片段须能定位到具体行动或交付责任。明显线程、工作区及配置故障不承担设计负效应；混合片段只保留可分离的责任表述。同时纳入明确承责仍拒绝不合格产物、以及监督检查增加具体要求的反例。全部材料来自探索性研究，尚未人工复核；本研究没有重跑历史模型或实施架构消融。（A02、A03、A10–A12）

本轮机制补查另检查三次历史提交及其父版本的身份提示、派发控制与工具说明，并核对两篇训练研究。采用竞争解释比较：对每项机制分别记录支持、反证、替代解释和可区分干预。源码仅证明指定版本的实现，消息仅证明可见表述；二者不能自动拼成当次模型输入。新增来源与筛选处置见[机制研究附录](mechanism-analysis.md)。（A15–A17）

**表1。P1材料与证据单位**

| 层次 | 规模 | 可支持内容与限制 |
|---|---:|---|
| 010检索消息 | 26,961条 | 检索范围，不是已逐条标注的案例 |
| 去重词法命中 | 429条 | 候选，不是故障数 |
| 选定窗口原生消息 | 67条 | 11个锚点的窗口；目的性选择 |
| I04原生Task动作 | 9条 | Task状态时序，不等于9次执行 |
| 表2对读片段 | 4个 | I01、I02、I04及反例I05，不估计发生率 |

### 3.2 受控动作实验

为先排除可控的接口和上下文混杂，另构造八类状态情境，覆盖尚未启动、恢复后无执行体、已有待受理请求、健康执行、健康未知、执行会话无中心权限、拒绝后修复和外部审批缺失。工具描述与确定性状态转换逐项核对；模型输出一个JSON动作，外部模拟器判断其主体、权限与状态效果。正确等待和合法上报均可达到对应控制边界，多余检查则单列为合法未推进，不自动编码成身份混淆。这些是从案例机制提炼的合成短题，不是历史生产事故重放。（A18）

实验交叉四个二水平因素：工具说明一致／保留reset自动派发旧措辞，无旧协议／带明确标为旧版的自动派发记录，一个逻辑Agent内部两种角色／两个共享任务归属的运行组件，以及Supervisor–Executor／Control unit–Work unit标签。两身份组的归属、权限、当前行动者、上下文边界与交付责任完全相同，只改变一句身份表述。因此，该对照识别等信息条件下的表述效应，并未改变进程拓扑。干净子集只包含说明一致、无旧规则条件，其余用于干扰诊断。

固定模型为`claude-sonnet-5-5`与`claude-haiku-5-5`，经同一Claude CLI 2.1.317调用，effort均为low。每次以新进程、空工作目录、无真实工具、无项目定制与会话持久化运行；归档历史只按因素有意加入。CLI与服务可能仍有不可见固定提示，隔离条件不等于纯模型权重。16条件×8情境×2呈现版本×2模型共512次动作选择；两呈现版本调整工具顺序、归档位置和对象ID，是固定稳健性检查，不是独立随机重复。另以32次规则不完备的诊断观察默认主体选择，允许回答“未指定”，不计算正确率。

输入、规则和调用脚本在正式任务输出前以哈希冻结；2次连通性请求不计入分析。内容错误不重试，传输失败最多重试一次。预定的16组恢复观察复用首步，只有未推进时才追加至多两步模拟器反馈。所有有效内容与失败记录均保留，身份和标签效应在干净子集内按情境及呈现版本配对，分别报告模型结果。固定情境的交叉组合不作为独立生产任务，不作总体发生率或训练来源推断。完整协议、原始返回和评分依据见[实验附录](controlled-experiments.md)。（A18、A19）

## 4 研究发现

### 4.1 识别执行缺失后，启动责任仍被归于中心

主案例呈现了状态识别与行动责任解释之间的脱节。Agent 已注意到 Task 恢复为 running 后仍未出现新的 execution，却据此报告需要中心修复调度，并再次阻塞任务。用户指出 executor 应由 Supervisor 创建后，Agent 修正了自己的解释，并报告已显式调用 `fork_executor`。这一交互记录的是对“谁应完成下一步”的解释纠正。（A04a–A04c）

Task 动作日志确认了恢复、启动状态、阻塞和解除阻塞的顺序，图2将其与用户纠正及 Agent 报告对齐。执行体创建和后续推送仍只有参与者报告；同一任务随后又因合同缺口被阻塞。因此，该片段支持启动责任误归属及纠正过程，不足以确认完整执行恢复或最终交付。（A04e、A05）

这一过程提示，执行状态识别与动作主体选择需要分别分析。Agent的事后说明将reset措辞列为误解线索，同时承认它解释不了更早一次unblock后的等待。该自述也将问题概括为只核验running，与此前明确报告缺少execution并不完全一致，不能作为完整的内部推理记录。（A04a、A04d）

本轮源码补查发现，系统曾从自动派发改为Supervisor显式fork；修订前的reset说明仍承诺自动派发，之后才删除这一承诺并补上显式fork要求。该工具说明缺陷与Agent自述相容，但部署版本、当次可见说明及首次等待的原因仍未知。因此，I04保留为责任误归属及纠正案例，不以其停摆后果支持架构本身有害。（A16、A17）

![图2：I04关键时序](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p1-logical-agent-identity/figures/p1-fig2-responsibility-timeline.png)

**图2。**I04关键时序。横轴按真实记录间隔绘制；形状区分原生Task动作、用户消息与Agent报告。用户纠正后29.639493秒出现unblocked，66.458005秒后出现显式fork报告；后者仍非取回的执行控制事件。02:05后仍有合同缺口导致的blocked，不能将这一窗口解释为最终交付。

### 4.2 交付责任外推与真实资源缺口需要分别处理

两个混合案例出现了将内部执行问题交回外部的表述：一个要求平台修复执行失败，另一个以验收外部交付的口吻描述自己的未完成任务。用户分别指出 executor 属于当前 Agent，以及当前主体仍承担完成任务的责任。它们与主案例相近之处，是执行隔离被解释为交付责任的分离。（A02、A03）

但这两个案例同时存在损坏线程和空工作区，后者还伴随工作树配置关闭。即使责任解释得到纠正，缺失的仓库与环境条件也不会随之恢复。因而这些事故的交付后果不用于评价架构损害；设计需要同时说明下一动作归谁，以及该主体是否真正具备执行条件。（A02a–A02c、A03a–A03e）

### 4.3 明确承责仍然可以拒绝自己的产物

责任统一与质量门槛可以并存。反例中的 Agent 明确将 executor 视为自己的执行单元，并承担最终裁决责任，但仍拒绝缺少生产窗口、迁移和消费者归零证据的删除候选。后续补证或回撤仍是待处理工作。该案例说明，拒绝候选本身不能用作身份混淆指标；关键在于拒绝后是否继续追踪未满足事项及其行动者。（A10）

监督还可能增加具体的质量判断。Access 检查指出缺少 Risk 字段及删除确认交互问题，后版源码出现相应修正；Spark 检查指出“至少十二类”的测试不足以覆盖二十五类合同，后版增加了类别、唯一性与总数断言。这里可见的是检查要求与产物变化之间的对应，而非多一层角色名称所带来的收益。（A11、A12）

表2并列责任误归属、混合故障和有效监督，表明需要解释的是行动责任如何延续到修复与验收，而非将所有阻塞或所有监督统一判为失败。历史契约和修订讨论属于设计意图证据，不能据其日期推定每次执行已加载相同说明。（A01、A06）

**表2。身份风险与反证的逐案边界**

| 片段 | 可观察责任关系 | 人工补充或正面证据 | 不计入设计负效应 |
|---|---|---|---|
| I01，7月22日 | 将内部恢复外推给平台 | 用户指出同一Agent的执行责任 | 线程损坏；后续计划不等于恢复已验 |
| I02，7月22日 | 将自身未完成任务描述为验收外部交付 | 用户纠正完成者定位；PD反对缺仓库盲重试 | 空工作区与配置问题；承责不能替代资源 |
| I04，8月6日 | 将Task恢复外推为中心自动建executor | 用户补充启动主体；Task动作支持时序 | 源码存在恢复说明缺陷；当次部署、prompt及原生fork事件缺失 |
| I05，8月7日 | 明确自身负责，仍拒绝缺证据候选 | 监督判断与统一责任可以并存 | 后续接管、权限变化与误提及混杂 |

### 4.4 排除明显故障后，材料用于机制比较而非架构归因

七个目的性选编片段的数量不变：四个混有运行或配置问题，一项属于开发者观察和设计回应，一项是行为主例，一项是明确承责的反例。本轮对主例I04新增接口说明缺陷的混杂风险标记。缺陷存在于历史源码，尚未证明当次生产使用该版本，故不能把它计为第五个已确认生产bug；也不能继续把I04视为排除实现问题后的干净架构负例。

**表3。身份材料的筛选与严格敏感性分析；009更新用途**

| 分组 | 片段数 | 片段 | 可用于哪种判断 |
|---|---:|---|---|
| 运行／配置混合，排除设计负效应 | 4 | I01、I02、I06、I07 | 仅保留可分离的责任语言；交付失败不计入架构后果 |
| 开发者观察与设计回应 | 1 | I03 | 设计动机与当时诊断，不是独立行为重复 |
| 行为主例，新增接口混杂风险 | 1 | I04 | 责任误归属与纠正过程；停摆不支持架构负效应 |
| 明确承责的反例 | 1 | I05 | 责任明确仍可合理拒绝自己的产物；后续混合交接不纳入 |

若要求以独立运行轨迹确认架构根因，合格因果负例仍为0。现有材料能够发现责任解释的问题、约束候选机制和设计后续对照，不能估计频率，也不能将片段数换算为架构失败率。005与006的原始筛选底表保留，新处置另行记录以避免改写研究历史。（A15–A17）

记录覆盖还限制了生命周期重建。2,221个 Task 中，1,238个关联到动作日志（55.7%），但执行镜像和控制事件在快照中没有记录。这两类缺失限制了对 executor 创建和模型输入的核验，不代表历史上没有执行。下一节进一步区分实时行为与迁移重建记录，以明确可观测范围。

### 4.5 稳定的审计身份不能代替行动责任理解

全部2,527条 Task 动作中，597条由迁移过程重建。其余1,930条非迁移动作覆盖927个 Task，占全部 Task 的41.7%；另有311个 Task 只有迁移记录，983个没有动作行。因此，任何动作行的55.7%覆盖率与非迁移行为的41.7%覆盖率回答的是不同问题。

归一内部 Agent ID 与身份 ID 后，819条 `agent_started` 中有818条的触发主体与所涉 Agent 对应同一逻辑实体（表4）。审计系统通常能够保存这一归属关系，但记录中的统一身份无法回答当时模型是否理解了应由谁启动 executor。主案例中的语义误归属可以与字段层面的正确归属同时存在。

**表4。非迁移动作中的触发主体与所涉Agent**

| 动作 | 记录数 | 不同Task数 | 同一逻辑Agent | 另一逻辑Agent | 无法比较 |
|---|---:|---:|---:|---:|---:|
| agent_started | 819 | 778 | 818 | 0 | 1 |
| blocked | 453 | 269 | 453 | 0 | 0 |
| unblocked | 331 | 182 | 151 | 180 | 0 |
| retried | 10 | 6 | 5 | 5 | 0 |

331条 `unblocked` 中，180条由另一 Agent 触发，显示跨主体恢复也是系统行为的一部分。两字段分别表示动作触发者与所涉 Agent，不是 Supervisor 与 executor 的进程身份。图3呈现的是记录覆盖和动作归属；其中仍含运行恢复与未知故障，不将 blocked 或 failed 的数量解释为架构失败次数。

![图3：动作记录覆盖与责任指向](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p1-logical-agent-identity/figures/p1-fig3-action-attribution.png)

**图3。**（a）2221个Task按无动作行、仅迁移、存在非迁移动作互斥分组；（b）非迁移记录的角色配置×动作计数，颜色采用log(1+n)，格内为原数值，空格为0；（c）四类动作内actor与所涉Agent的关系，分母是各行动记录数。角色来自冻结配置，并非历史角色；start表示领域动作日志，不能等同executor进程启动。

### 4.6 排除可控混杂后，未检出统一身份表述的不利效应

说明一致、无旧协议的干净子集中，两模型均在64/64次首次动作到达规定的控制边界。每个模型的统一身份组与组件表述组均为32/32，32个配对的推进率差为0；常用角色标签与中性标签也是各32/32、差0。这个结果对“统一身份本身足以造成责任误归属”提供了有界反证：在当前状态、权限和义务明确时，两种表述都可以被正确使用。它不能证明两种架构等价，也不能估计信息较少或长上下文中的表示负担。（A18、A19）

**表5。合成动作实验与默认预期诊断的分层结果**

| 条件或指标 | Sonnet 5.5 | Haiku 5.5 | 统计含义 |
|---|---:|---:|---|
| 一致说明＋无旧协议 | 64/64 | 64/64 | 首次到达规定控制边界 |
| 一致说明＋旧协议记录 | 64/64 | 63/64 | 同上；非推进为额外start步骤 |
| 遗留reset说明＋无旧协议 | 64/64 | 64/64 | 同上 |
| 遗留reset说明＋旧协议记录 | 64/64 | 62/64 | 同上 |
| A全部条件 | 256/256 | 253/256（98.83%） | 有限合成题集，不是交付率 |
| 主体匹配且动作合法 | 256/256 | 256/256 | 不将额外合法步骤算作越权 |
| B诊断回答“未指定” | 16/16 | 16/16 | 选择分布，不是正确率 |

三个未首次启动的输出均来自Haiku的S1、组件表述和呈现版本0，并带有旧协议记录。它们正确选择controller，却将获取lease、置为running作为fork之前的步骤；当前协议实际允许从open直接入队。这一可见行为是多做一次合法准备，没有出现等待中心自动创建的动作。说明一致时，旧历史相对无旧历史的配对差为−1.5625个百分点（63对共同推进、1对仅干净条件推进）；Sonnet为0。无旧历史时，遗留reset说明的配对差在两模型均为0。差异集中于同一短情境，不能解释为一般历史干扰率或工具缺陷无害。（A19）

三个输出的对应统一身份、无旧规则和呈现版本1对照均直接fork。正反材料因而都不支持把非推进归咎于统一封装；也不足以把少数反向差异解释成统一封装更优。合理等待、未知执行体先检查、worker上报无权执行的中心动作、拒绝后安排获授权修复和缺审批先请求等行为，均按各自情境保留为正面结果。

![图4：受控动作实验的完整矩阵与配对差异](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p1-logical-agent-identity/figures/p1-fig4-controlled-experiments.png)

**图4。**（a）两模型×四种说明／历史条件×八情境，每格汇总两身份×两标签×两呈现的8输出，合计512次；框线标示未全部首步推进的单元。（b）预定配对率差，身份／标签仅取干净子集，每模型32对；历史仅取一致工具、说明仅取无旧历史，各64对，单位为百分点。（c）两类不完备协议诊断：D1创建执行体、D2发起修复，每模型每题8次。固定情境的描述结果不附总体置信区间；底表、全部异常与矢量文件见实验附录。

### 4.7 额外准备步骤没有在本次观察中发展为停摆

预定恢复观察的16个干净组合都在首步达到控制边界，未触发后续调用，因此这一部分没有测到纠错能力。为判断三个非推进输出是否会持续等待，主实验后另固定事后诊断协议，纳入全部三例并仅回填模拟器的真实状态反馈。三者下一步均选择fork_executor，共追加3次调用，没有人工指定下一动作。首步的253/256仍保留；二步到达入队边界既不能改写为首步正确，也不能外推完整交付成功。至少在这些短序列中，多一个合法步骤与停摆并不等价。（A19）

32个不完备协议诊断全部回答主体“未指定”，没有从统一身份或Supervisor标签直接推出固定的创建者、修复者。其可见解释区分了统一任务归属与动作分配。这是未检出显式默认主体偏好的结果；题目明确允许并提示不确定时回答未指定，可能压低默认补全倾向，故不能据此断言模型没有行为先验，更不能判定训练来源。（A18、A19）

## 5 机制分析与设计启示

“抽象模型问题”“架构问题”与“训练所得行为模式”作用于不同环节，可能共同导致同一错误。本文以行为记录约束解释：需要解释的是，模型为何把一个应由当前控制会话发起的动作归给中心，或者为何在拒绝自身产物后没有延续修复责任。以下六项机制是待区分的解释；受控实验为其中部分提供了有界反证，并非确认了六个根因。

### 5.1 抽象层：统一归属不等于统一行动者

将多个执行单元归于一个Agent，在软件模型上可以自洽。身份契约可以规定统一任务归属和对外交付责任，同时允许不同会话拥有不同权限、上下文及生命周期。危险出现在从“属于同一个Agent”继续推出“知道同样的信息、具有同样的能力、承担同一个下一动作”。这种推导并非原抽象的必然要求，却可能由笼统的身份说明诱发。（A01、A14）

表6将统一身份中需要分别绑定的关系展开。对于某项任务，归属主体可以始终是同一Agent，但创建executor的动作主体是Supervisor，写代码的是executor，资源分配由运行时完成；这些关系不会因为共享名称而自动成立。因而，抽象层的待检验问题是：统一身份表述是否让模型把不同类型的关系混用，而非“一个Agent包含多个进程”是否在逻辑上错误。

**表6。逻辑Agent中的关系分解及可能的错误推导**

| 关系 | 本设计中的含义 | 需要阻止的推导 |
|---|---|---|
| 任务归属 | 任务持续属于逻辑Agent | 同属一个Agent，所以任一会话都能执行全部动作 |
| 当前动作主体 | Supervisor发起fork；executor执行获分配的工作 | 任务已归属，所以中心会自动完成创建 |
| 权限 | executor不能写中心；Supervisor持有相应入口 | executor无权限，所以整个Agent只能等待外部人员 |
| 知识与记忆 | 会话各自持有输入；结果通过接口回传 | 子执行单元已经知道的信息，监督会话也必然知道 |
| 交付义务 | 拒绝候选后仍需安排获授权的修复、补证或升级 | 监督者已作出拒绝，自己的交付责任也已结束 |

这里的“自己”至少可能指当前模型会话或逻辑责任主体。把两者写成同一个代词，会增加解释负担；直接给出任务归属、当前角色、允许动作与结果接收者，是可以检验的替代表示。有效监督反例表明，模型有时能够同时保持统一责任与独立审查，故这种混用不是必然结果。（A10）

### 5.2 架构层：业务状态没有唯一确定执行动作

Task状态机与executor生命周期是两套相关但不等价的状态。恢复任务、取得lease、排入控制队列和真正创建执行体，可以发生在不同步骤。只返回running而不说明哪一步已经发生，会把跨状态机的关系推断留给模型。主案例尤其说明，单纯补充“execution为空”仍不充分：Agent已经注意到空缺，却把处理空缺的责任归给中心。（A04、A05）

历史源码进一步给出了需要区分的两种情况。若运行时确实采用显式fork，但工具仍承诺自动派发，这是接口说明缺陷，应先修复并从架构负效应中排除。若工具完全一致，仍要求模型在多个入口间自行拼接启动与恢复协议，则可以进一步研究这种分解带来的决策负担。把两者合并，会将可修正的实现问题错误升级为结构缺陷。（A16、A17）

因此，可比较的设计变量是确定性调度由程序还是模型承担，以及接口是否直接提供合法下一动作。自动启动能够减少选择步骤，但仍需处理权限、存活执行体、幂等和资源条件。不能仅因减少了一次模型决策，就假定交付效果或成本必然改善。

### 5.3 上下文层：角色指令、恢复闭环与历史规则可能错位

源码中的身份表述和启动协议曾分别改变。较早的Supervisor提示强调协调、监督及不亲自执行；身份修订强化同一Agent的交付责任，却仍保留系统自动派发。后续提交才将启动改为Supervisor显式调用fork。可见，知道“执行体属于我”与知道“这一次由我启动”是两个独立命题，补充前者不能替代后者。（A15、A16）

审查指令也可能形成局部闭环：核验产物、发现缺口、执行block，每一步均符合要求，但没有为缺口安排修复主体和恢复动作。模型于是可能正确完成“审查”子目标，却没有推进整个交付目标。历史提示确实列出了审查与阻塞出口，也包含帮助解决阻塞的要求；尚不能据此断言修复义务完全缺失。需要检验的是义务能否在具体失败状态下转化为可执行动作。（A15；A03、A10）

另一项竞争解释是旧规则残留于会话、摘要或持久记忆。较早版本中的自动派发预期有实际系统来源，不必先诉诸模型训练。只有恢复了当次输入，才能判断新指令与旧记录是否同时出现，或者旧规则是否仍被当作当前规则。提交日期早于行为日期，不足以证明旧上下文被保留或新版本已部署。（A16、A17）

### 5.4 模型层：行为先验冲突是合理假说，但不是已知训练事实

作者提出的训练解释，可以更精确地表述为：模型从训练中学到的角色分工、工具调用与任务完成模式，可能不适配外部系统要求的责任映射。这不需要假定模型内部存在一个固定的Agent组织结构。可观察的预测是，在输入、工具语义和权限均明确时，模型仍倾向于选择某种熟悉的分工或启动方式。

AgentTuning通过交互轨迹微调改变模型的agent行为，其消融说明训练数据组成会影响表现；它并未测量Supervisor–Executor身份冲突。[4]<!--ref:R30--><!--anchor:section:2.2, 3.3, 3.4--> Instruction Hierarchy则说明消息来源的优先级行为可以通过训练塑造；该层级不是业务中的监督者与执行者层级。[5]<!--ref:R31--><!--anchor:section:3, 4--> 两项研究支持“训练可以塑造行为模式”，不能证明本部署模型学过哪种架构，或其错误由该训练引起。

还须把权重中的倾向与外部CLI自带提示、当前工具描述、会话历史分开。即使不同模型在同一harness中表现不同，也只说明模型与接口可能存在交互；若模型能力、训练数据和解码方式同时不同，不能将差异单独归因于“内化了原生Agent结构”。角色改名后改善同样只能支持对措辞敏感，不能定位这种敏感来自预训练、后训练还是本轮上下文。

### 5.5 竞争解释及当前证据强度

源码支持契约和接口变化的存在，历史行为记录支持责任解释的变化；受控短题则约束了哪些解释能够在明确协议下复现。表7将三类证据分开。当前实验削弱了直接以统一身份解释全部异常的说法；它没有在抽象、历史上下文与模型训练之间选出唯一根因。

**表7。候选机制与受控实验后的证据判断**

| 机制 | 历史支持或线索 | 本轮实验及反向证据 | 尚缺判别 |
|---|---|---|---|
| H1 身份关系混用 | 同一归属下权限和上下文不同；有责任外推表述 | 干净条件身份配对差为0；三例额外步骤均在组件组 | 长上下文与实际多会话下的表示及拓扑对照 |
| H2 状态与启动协议分离 | 无execution仍等待；恢复说明曾不一致 | 三例多做start，反馈后均fork；仅改reset文字未复现停摆 | 更贴近恢复路径的工具调用与确定性调度对照 |
| H3 审查子目标替代交付闭环 | 审查与block出口突出 | 获授权修复情境均正确安排修复；历史也有有效监督 | 跨轮审查后是否仍延续修复，而非只看单步 |
| H4 历史上下文残留 | 自动派发转显式fork；无实际会话快照 | Haiku一致说明下有1个配对差异；Sonnet无差异 | 更多任务与独立重复，区分干扰和采样变化 |
| H5 训练所得模式冲突 | 外部研究说明行为可由训练塑造 | 明确协议下无错主体；默认诊断均未指定 | 同源模型训练干预；商业模型比较不足 |
| H6 一般推理或状态跟踪失误 | 多步恢复可能遗漏与重复 | 三例更接近额外前置步骤推断，未见主体错误 | 无角色语义的同构长程状态任务 |

H1–H6仍可能叠加。用户提醒同时改变主体信息、动作提示和上下文焦点；本轮模拟器反馈又改变了任务状态。二者都不能被当作针对某种内部心理机制的单一纠正。应分别记录哪个输出变量改变，再决定需要哪一层干预。

### 5.6 已执行对照与下一层因果识别

本轮完成的是等信息表述、说明一致性、旧规则和角色标签的交叉短题，而非009拟议的全部干预。将“统一宣言与完整关系表”直接比较会同时改变信息量，因此本轮先把身份对照收窄为一句表述；增加结构化下一动作则暂缓，先核对说明缺陷。这样的收窄使变量更可解释，也限制了结论覆盖范围。

**表8。已执行的比较与仍需研究的因果层次**

| 对照 | 本轮状态 | 可识别内容与后续要求 |
|---|---|---|
| 等信息的统一身份／组件表述 | 已执行，干净子集每模型32对，差0 | 单句表示效应；未改变拓扑或上下文边界 |
| 一致／遗留reset说明 | 已执行，无旧历史每模型64对，差0 | 指定文字干预；需覆盖真实reset恢复任务 |
| 无旧协议／旧版本记录 | 已执行，一致说明每模型64对 | Haiku差−1.5625个百分点，Sonnet差0；需独立重复 |
| 常用／中性角色标签 | 已执行，干净子集每模型32对，差0 | 局部词汇敏感性；工具名仍含executor |
| 简短宣言／完整关系表／结构化下一动作 | 未执行 | 固定事实与任务，分离信息量、表示和提示强度 |
| 模型调度／确定性状态协调器 | 未执行 | 比较真实交付、错误恢复、延迟和成本 |
| 同源模型的成对轨迹训练 | 未执行 | 固定基础模型与训练量，检验训练对特定责任映射的因果作用 |

下一步应在协议仍一致的条件下增加长程状态变换和真实工具执行，预先固定任务、反例与评分，再比较身份表示和调度机制。不能为得到错误而有选择地删除必要信息。模型训练的解释需要受控训练干预；更换商业模型、观察模型自述或角色改名均不能独立识别训练来源。

### 5.7 设计启示：显式表达责任关系，保留有效监督

本研究据此提出三个有条件的设计判断。第一，统一Agent可以保留为任务归属和审计标识，模型输入应分别说明当前角色、可用权限、结果去向与下一动作，避免要求模型从“executor也是你”推导全部行为。第二，运行协议变更必须同步提示、工具说明和恢复入口，并处理旧会话规则；这是先消除实现混杂的前提。第三，可确定的启动与恢复关系应尽量由明确协议承载，再比较交给模型判断的增量收益。

监督的价值仍应由它增加的具体证据衡量。Access与Spark案例中的语义检查不能因身份问题而被一并否定；拒绝不合格产物后，修复、补证或升级应继续关联有能力且获授权的主体。（A10–A12）CodeDelegator的分工结果也约束了推广边界：角色分离具有可行的正面实例，本部署的混合案例不能推出该类结构普遍无效。[3]<!--ref:R14--><!--anchor:section:4.1, 5.2, 5.3-->

## 6 局限

本研究是单部署、目的性选例的探索，未估计身份混淆的发生率、持续时间或相对架构风险。用户纠正同时改变主体信息、动作提示及上下文焦点，不是单变量干预；Agent的事后解释也可能是合理化。主案例可确认责任解释与状态记录的对应，无法恢复其完整内部过程。（A00、A04、A05）

本轮恢复了指定Git版本的提示、派发入口与工具说明，仍未恢复当次部署版本、完整模型输入和执行控制轨迹。接口说明缺陷排除于架构负效应；历史协议变化也不能自动证明会话残留旧规则。模型—harness研究提供解释视角，不能替代本地缺失的因果证据。[2]<!--ref:R03--><!--anchor:section:I-->（A15–A17）

受控实验是八类规则清晰的合成短题，不包含真实代码交付、长上下文压力或完整进程拓扑。两呈现版本不是随机重复；干净条件100%的表现带来天花板限制。旧说明仅修改reset文字，旧协议又明确标为历史，未覆盖生产中各种隐蔽残留；中性标签保留了动作名中的executor。默认预期题提示不确定时回答未指定，也限制了对隐含倾向的测量。三个额外步骤集中于同一情境，事后续步不属于预定恢复检验，不能估计一般停摆或纠错率。

模型版本来自当次返回，不代表未知历史模型。相同CLI减少了外部调用差异，仍不能审计服务全部固定提示或分离模型能力、训练和解码。没有同源训练干预，公开训练研究和本轮行为结果均不能逆推训练来源。实验首步推进率不是生产交付率，未检出差异不等于证明架构等价。

监督案例的源码核对只支持指定变化，没有重跑历史浏览器或生产验收。全部分析尚未人工复核，四篇论文复用同一部署与部分案例，不能视为独立重复验证。吞吐、成本、认知负荷及替代方案收益仍需在固定条件下测量。（A00、A11、A12）

## 7 结论

将Supervisor与临时executor归为同一逻辑Agent，源于并发执行与统一回写的工程需求。历史案例暴露了任务归属与行动责任之间的解释问题，但源码中的派发变更和说明缺陷限制了架构归因。两模型的受控短题进一步显示：说明一致、无旧规则且关系明确时，统一身份与组件表述均正确完成全部64次首步选择，没有检出身份表述或常用角色词汇的不利效应。（A14–A19）

旧历史条件下的三个额外准备步骤在下一步均启动执行，默认预期诊断也未暴露固定主体偏好。由此，责任误归属、动作顺序冗余和长期停摆必须分别测量，训练结构冲突仍是尚未识别的解释。设计上应明确当前角色、权限、结果去向与后续责任，并保留能增加证据的监督；统一身份、执行隔离及程序调度的净收益仍需在长程真实任务中比较。（A10–A12、A18、A19）

## 参考文献

[1] M. Cemri et al., “Why Do Multi-Agent LLM Systems Fail?,” arXiv:2503.13657v3, 2025. doi: [10.48550/arXiv.2503.13657](https://doi.org/10.48550/arXiv.2503.13657). [已核版本](https://arxiv.org/abs/2503.13657v3).

[2] J. Chen, S. Chen, H. Zhu, J. Cao, J. Shen, and S.-C. Cheung, “Understanding Agent-Reactive Bugs at the Model-Harness Boundary: An Empirical Study of LLM Agent Issue Reports,” arXiv:2607.15684v1, 2026. doi: [10.48550/arXiv.2607.15684](https://doi.org/10.48550/arXiv.2607.15684). [已核版本](https://arxiv.org/abs/2607.15684v1).

[3] T. Fei, C. Chen, Y. Pan, M. Zheng, and M. Song, “CodeDelegator: Mitigating Context Pollution via Role Separation in Code-as-Action Agents,” arXiv:2601.14914v1, 2026. doi: [10.48550/arXiv.2601.14914](https://doi.org/10.48550/arXiv.2601.14914). [已核版本](https://arxiv.org/abs/2601.14914v1).

[4] A. Zeng et al., “AgentTuning: Enabling Generalized Agent Abilities for LLMs,” in Findings of the Association for Computational Linguistics: ACL 2024, pp. 3053–3077, 2024. doi: [10.18653/v1/2024.findings-acl.181](https://doi.org/10.18653/v1/2024.findings-acl.181). [正式论文](https://aclanthology.org/2024.findings-acl.181/).

[5] E. Wallace, K. Xiao, R. Leike, L. Weng, J. Heidecke, and A. Beutel, “The Instruction Hierarchy: Training LLMs to Prioritize Privileged Instructions,” arXiv:2404.13208v1, 2024. doi: [10.48550/arXiv.2404.13208](https://doi.org/10.48550/arXiv.2404.13208). [已核版本](https://arxiv.org/abs/2404.13208v1).

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
| A15 | 身份提示、执行提示与审查回写的历史修订 | Git `3dcd57bf35fc6ed0572df93c4e8616b043f1e591`及父版本 | 2026-07-23 01:38:22 UTC | [固定差分](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/009-p1-causal-mechanisms/sources/identity/changes.diff) |
| A16 | 自动派发改为Supervisor显式fork：提示与实际入口 | Git `b27f59650ad3061d67da0bec9893feb258405a19`及父版本 | 2026-07-26 14:09:54 UTC | [固定差分](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/009-p1-causal-mechanisms/sources/dispatch/changes.diff) |
| A17 | reset自动派发说明纠错；不证明当次部署 | Git `00bddae72e94ee5419339b0b95f4fb439af0a927`及父版本 | 2026-08-06 03:24:22 UTC | [固定差分](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/009-p1-causal-mechanisms/sources/reset/changes.diff) · [来源哈希](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/009-p1-causal-mechanisms/source-provenance.json) |
| A18 | 合成实验冻结协议、全部输入与确定性评分 | 512动作＋32诊断；2模型 | 2026-11-03 | [协议](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/protocol.md)、[冻结哈希](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/design-lock.json)、[方法](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/method-details.md) |
| A19 | 全量结果、正反证据与恢复边界 | 544主输出；另3次事后续步；16预定组合无新增调用 | 2026-11-03 | [报告](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/report.md)、[逐行评分](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/analysis/rows.csv)、[事后续步](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/analysis/posthoc-recovery-summary.json) |

## 声明

**数据可用性。** 本稿使用既有冻结研究包，原生消息与代码片段按附录路径可在授权本地环境中查阅，阅读范围见同目录 `read-log.json`。这些本地路径不代表公共数据仓库或开放许可。对外共享范围、脱敏方式和许可仍需数据持有人确认。 本轮实验的冻结输入、原始返回、状态评分、全部非推进输出、正例、图源及事后诊断单独归档于[受控实验目录](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p1-identity-001/README.md)。

**伦理与隐私。** 材料包含用户和 Agent 协作记录。伦理审查机构、批准或豁免状态尚未由作者声明；本文不声称已获批准或无需审查。公开前应由责任作者复核可识别信息、访问授权与必要脱敏，原生标识用于本地可追溯性。

**作者贡献。** 署名作者为 oopslink；具体 CRediT 贡献角色待补充。AI 不列为作者。

**利益冲突。** 待作者声明，不据缺少信息推断不存在利益冲突。

**资助。** 本研究未获得资助。

**AI 使用。** 既有研究为单AI探索；010早期使用分工AI补查、引用核验、起草与审阅。009机制修订及本轮受控实验由同一AI设计、执行、分析与自查，未新增独立评阅。本轮保存547次正式模型调用及2次不计样本的连通性检查；未重跑历史部署，未知历史模型与参数不予补造。原研究与本轮工作均尚未人工复核，责任作者需核查事实、引用与最终论述。

**同源与跨篇重合。** 本文与责任悬空、角色上下文及 有向无环图（DAG）演化工作稿来自同一部署和冻结快照，复用部分 006–009 与 010 证据；尤其 I05 和监督案例也用于边界分析。复用记录保持原生标识，不累计为新增独立样本，不宣称四篇相互独立验证。
