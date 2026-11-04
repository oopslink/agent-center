# 交付状态的绑定断裂：一个多智能体软件开发部署的现场研究

**作者：** oopslink

**主页：** [https://github.com/oopslink](https://github.com/oopslink)

**研究类型：** 单部署回顾性案例研究。

> **撰写中。** §1–§3 为定稿，§4–§7 由后续任务填入。
> 取材依据 [证据台账](evidence-ledger.md)，不进入本篇的内容与理由见 [删除与降级决定](exclusions.md)。

## 摘要

多个大语言模型 Agent 协作交付软件时，会话、代际、规则作用域、角色与权限各自形成一条边界。本文研究一个持续运行的部署：当一项尚未满足的交付义务跨过这些边界时，它与承担者、凭据和合同之间的绑定关系在哪里失去确定性。材料为该部署冻结快照中的消息、任务动作、任务图代际、只读 Git 对象核验、团队规则治理事件与规则读取回执，按五级证据标注，技术故障的后果排除在设计层面的归因之外。

记录呈现六类绑定断裂：责任、凭据、信息、成果与合同、适用范围、剩余义务。审计层面的归属与语义层面的归属可以不一致——819 条 `agent_started` 中 818 条的触发主体与所涉 Agent 对应同一逻辑实体，而同期记录中存在把 executor 失败交给外部主体的解释。保留任务图历史不等于继承产物：169 次父子转移中 149 次保留了全部父节点，只读 Git 核验仍确认一个中间候选未以前一候选为祖先、相应后端文件从存在变为缺失；而两个不同提交拥有相同完整文件树，说明提交变化不必然丢失内容。166 次拓扑改变中有 65 次没有任何共有任务转为完成，3 次拓扑未变的转移则全部记录了状态变化。团队规则的 57 个事件与 57 个 Git 提交逐一对应、230 条读取回执覆盖 12 个 Agent，但 27 个已审核提案中有 16 个作者与审核者相同，而一条要求所有任务先合入主分支的规则与中间候选的验收合同形成了循环。

本文据此提出六项设计要求，均为"把某个被跨越的关系显式写下来"，而非增加流程。本研究是单部署、目的性选例的回顾性案例研究，不估计任何断裂的发生率、持续时间或相对架构风险；<!--allow-banned:发生率-->分类的一部分有作者对随机子集的人工判定，其余只有 AI 编码。

**关键词：** 多智能体软件交付；责任连续性；凭据认可；计划修复；团队规则适用性

## 1 引言

### 1.1 这个部署是什么，为什么会有这些边界

Agent Center 是一套让多个大语言模型（LLM）Agent 协作完成软件交付的系统，自 2026 年 6 月起持续运行。它的五项机制各自解决一个具体问题，同时各自引入一条边界。

常驻 Supervisor 接收消息、组织上下文并统一回写中心状态，临时 executor 在隔离的会话与工作区中执行具体任务。这样做是为了让同一个 Agent 不必串行处理任务，也避免多个执行单元并发写入中心状态；代价是**会话边界**——executor 的工具历史不在 Supervisor 的上下文里。

任务以有向无环图（DAG）组织，并以 generation 记录演化。这样做是为了让执行反馈能够改变后续工作安排；代价是**代际边界**——新一代保留了哪些节点，与后继候选是否继承了已验证的代码，是两件不同的事。

跨成员的工程规范经提案、审核与版本化发布成为团队规则，会话按索引读取规则正文并留下加载回执。这样做是为了让一个成员的经验能被后续任务复用；代价是**作用域边界**——一条规则在什么阶段、对什么对象成立，必须随规则一起表达。

实现与独立验收由不同角色承担，验收者拒绝不合格候选后，修复、补证或升级仍需安排。这样做是为了让缺陷在交付前被发现；代价是**角色边界**——产出凭据的会话与认可凭据的会话不是同一个。

权限按角色分配，executor 不能写中心状态，治理动作需要单独授权。这样做是为了限制故障的影响范围；代价是**权限边界**——有义务的主体与有能力执行的主体可能不是同一个。

这五条边界都是设计意图，不是缺陷。本文研究的是：当一次执行、一个会话或一条规则的作用范围结束时，跨过这些边界的那部分交付义务会发生什么。

### 1.2 研究问题

**研究问题是：在这个部署中，交付义务跨越会话、代际、作用域、角色与权限边界时，它与承担者、凭据和合同之间的绑定关系在哪里断裂，断裂时记录里留下了什么？**

把问题这样提出，是为了把三件容易混在一起的事分开。第一件是**能力**：模型能不能写出正确的代码。第二件是**运行条件**：线程、工作区、配置与工具说明是否完好。第三件是**绑定**：一项尚未满足的义务此刻归谁、依据哪份凭据、按哪一版合同判定。本文只研究第三件。前两件进入分析只有一种方式——作为需要排除的替代解释。

相应地，本文采取描述与机制解释的定位，不做因果识别。单部署、回顾性、目的性选例的材料可以回答"这种断裂长什么样、记录里能看到哪些环节、人工补充了什么"，不能回答"这种封装相对其它架构的净效应是多少"。凡是后一类问题，本文的处理是明确指出需要什么条件才能回答，而不是用现有材料给一个数。

本文的贡献因此是一份**解剖**：一个运行数月的真实多智能体软件开发部署中，六类绑定断裂的形态、可核验的证据层级，以及各自的替代解释与排除依据。这类材料在公开记录中少见——不是因为现象罕见，而是因为同时具备长期协作记录、原生状态日志与可核验产物历史的部署很少对外开放。所有定量描述都给出可复算的来源，案例判定的一部分另有作者的人工判定与一致率（§3.4）。

## 2 框架与相关研究

### 2.1 把一次交付拆成可检查的绑定关系

协调可以界定为对活动之间依赖关系的管理。[1]<!--ref:MC94--> 这个界定有一个对本文关键的含意：角色数量、消息条数、节点个数都只是表面指标，真正需要管理的是具体的依赖——测试者产生一张回执，集成者需要判断这张回执是否对应当前候选，这才是依赖本身。

本文据此把一个工作项可执行的条件写成一组需要分别确定的关系：

```
〈目标，动作，主体，对象，合同，授权，前提〉
```

- **目标**：哪一项尚未满足的交付义务；
- **动作**：下一步要做什么；
- **主体**：由谁做；
- **对象**：针对哪个具体候选、分支或产物；
- **合同**：按哪一版验收标准判定；
- **授权**：该主体是否有权执行该动作；
- **前提**：该动作依赖的外部条件是否已满足。

**绑定断裂**指这组关系中至少有一项在边界处失去确定性，而参与者仍按局部规则行动。判定一段记录是否构成断裂，需要同时满足三条：存在一项**尚未满足**的义务；发言主体在当时**有可行动的合法路径**；它没有把该义务接续下去。三条缺一条就不算。只有 failed 状态、只有"另一个会话"这样的措辞、或者主体确实无权行动，都不构成断裂。这组判据同时用于 §3.4 的人工盲编码。

这样提问的方式有既有依据。协调机制可以理解为"协议加上承载它的制品"，机制的适用范围由它被物化成制品的方式决定。[2]<!--ref:schmidt1996coordination--> 组织例程也可以分成规则的抽象形式与规则的实际执行两个对象，二者并不自动一致。[3]<!--ref:feldman2003reconceptualizing--> 本文的六元组切分方式与"断裂发生在边界处"这一组织方式，是在本部署材料上提炼的，不是从这些理论推导出来的。

据此，本文把观察到的断裂归为六类：**责任**（一次执行终止后目标还归谁）、**凭据**（某张回执对应哪个候选、谁认可它）、**信息**（消费者此刻的职责需要哪些事实）、**成果与合同**（新计划是否继承了已验证产物、旧判定是否适用新合同）、**适用范围**（规则的阶段条件与任务阶段）、**剩余义务**（局部完成与未满足的交付义务并存）。这六类不是穷尽分类，而是本部署材料能够支撑的六种形态。

### 2.2 已有研究覆盖什么

多智能体 LLM 系统的失效已有系统化的分类工作。MAST 把失败归入规范与角色、协调错位、终止与验证三大类，[4]<!--ref:R01--> 另有工作按故障类型、症状与根因三轴组织 agentic AI 的故障，[5]<!--ref:shah2026characterizing--> 也有跨 benchmark 的综合指出排行榜掩盖了失效结构。[6]<!--ref:albayaydh2026beyond--> 通信边界是其中被反复定位到的位置：两组独立研究分别刻画了 LLM 多智能体系统中消息内容、时序与路由层面的故障及其传播。[7]<!--ref:zhou2026messages-->[8]<!--ref:pitre2026understanding--> 过程视角的工作则强调要保留失败的起点、演进与恢复过程，而不只是终局标签，[9]<!--ref:R05--> 模型与 harness 边界的研究进一步说明运行机制本身会改变可见行为。[10]<!--ref:R03--> harness 这一术语在本文按已有的充要条件定义使用。[11]<!--ref:macedo2026harness-->

这些工作提供了定位现象的语言。它们没有覆盖的是本文的证据类型：同一部署中跨数月的协作记录、原生状态日志与可核验的产物历史被放在一起对读。分类法告诉我们某类失败存在，不能告诉我们在一个具体部署里，一项义务是在哪一步、因为哪条边界失去承担者，以及当时人工补充了什么。

分工机制本身也有明确的设计基础，且分离执行与持续承担恢复责任可以共存：长期 Delegator 在短期 Coder 返回后选择继续、重试或重规划，[12]<!--ref:R14--> AutoGen 区分角色配置与会话控制，[13]<!--ref:RV16_AUTOGEN--> MetaGPT 把角色、标准作业过程、结构化产物与消息订阅联合设计，[14]<!--ref:RV16_METAGPT--> ChatDev 把分工与阶段通信、具体澄清结合。[15]<!--ref:RV16_CHATDEV--> 因此本部署中的失效不能外推为这类分工普遍不可行。工作流形态可以被搜索或按反馈演化，[16]<!--ref:R11-->[17]<!--ref:R28-->[18]<!--ref:R29--> 生产环境的调查也表明可靠性与可控自治是实际关注点。[19]<!--ref:R04--> 另有工作把不对称授权与私有状态作为协作边界来研究失效。[20]<!--ref:R08-->

长期记忆的保存、选择与更新同样有明确方案：记忆流与反思支持情境行为，[21]<!--ref:park2023generative--> 上下文内外分层管理有限容量，[22]<!--ref:packer2023memgpt--> 新经历可以链接并更新既有记忆，[23]<!--ref:xu2025amem--> 多智能体交互与经验可按任务与角色分层筛选，[24]<!--ref:zhang2025gmemory--> 索引、检索与阅读也可以分开评价。[25]<!--ref:wu2025longmemeval--> 这些机制不能被概括为只做静态存储。本文要补的不是检索质量，而是一条已经被正确读到的规则，其适用范围与接收任务的阶段是否相容。

最后，把相互关联的对象组织成过程记录时，单一 case 标识不足以表达多个对象之间的关系。[26]<!--ref:R26-->[27]<!--ref:R27--> 本文关联 Plan、generation、Task、候选产物与合同时采用同样的分析取向，但不提出新的过程挖掘算法。

与本文最接近的既有工程对策是把"规范不足、协调错位、验证不当"三类缺陷归因于缺少结构化约束，并以显式行为契约加生命周期验证作为干预。[28]<!--ref:mao2025towards--> 它给出的是干预与度量，本文给出的是干预之前那份现场记录。

本文不声称填补算法空白。它补的是一份具体部署的、带产物级核验的现场记录。

### 2.3 本研究借用的概念来源

五条既有判别标准被本文直接使用，借用限度一并说明。

**一次尝试结束，本身不是放弃目标的理由。** 持续目标理论把"行动者在什么条件下可以放弃目标"放在中心位置。[29]<!--ref:CL90_COMMIT--> 本文借用这条判别，不假定语言模型拥有该理论形式化的信念或意图；分析对象是软件记录中可观察的关系：谁承担目标、谁执行本次动作、执行结束后谁仍能采取合法的下一步。承诺在重规划时如何被保留或丢弃，在规划研究中也有系统比较。[30]<!--ref:babli2023plan-->

**计划修改要定位哪些关系失效，而不是无条件重建。** 情境改变后需要找出不再成立的那部分，而非丢弃全部计划。[31]<!--ref:KH92--> 同时，复用并不必然比重建省力，最坏情况下保守复用有复杂度下界。[32]<!--ref:nebel1995plan--> 本文据此只问两件可独立推翻的事：新安排是否覆盖反馈揭示的缺口；保留的候选是否仍具备先前已验证的成果。

**角色与权限是两层关系。** 角色访问控制把用户到角色的指派与角色到权限的关联分开建模。[33]<!--ref:RBAC96--> 由此不能推出"同角色产生的材料足够完成工作"：授权回答谁可以读，当前任务决定哪些事实值得读。

**组织记忆要分获取、保留与提取。** 这一分解说明组织如何使用或误用过去的经验。[34]<!--ref:WU91--> 更贴近本文的是实证与批判层面的发现：组织记忆在实践中往往不是检索问题，而是记忆片段脱离原情境后不再可用。[35]<!--ref:ackerman2000reexamining--> 本文对"组织"的借用限于跨成员保存与传播的机制，不把数据库读取当成认知。

## 3 方法

### 3.1 分析单位与时间窗

**分析单位是一次"义务未满足后的处置片段"**：从某项目标或前提尚未满足开始，包含参与者对下一步的解释、转交或等待，以及随后的纠正或继续记录。单位不是一条消息，也不是一个 failed 状态——同一条消息可以既陈述技术故障又不涉及处置选择，而一个 failed 状态本身不说明义务去了哪里。

材料取自**冻结快照**，不是整个部署。该部署在本文写作时仍在运行，因此本文描述的是若干时间窗内的状态；所有抽取在冻结快照上一次完成，不随部署继续而更新。

**各证据层的时间窗并不相同，这一点影响可比性，须先说明。** 消息与任务动作记录覆盖 2026-06-16 至 2026-09-03；源码修订的核对范围从 2026-06-28 的并发设计初稿起；规则读取审计只覆盖 2026-08-14 至 2026-09-21，即该审计机制上线之后，因此对更早发布的规则存在**左截断**；团队记忆治理与 Git 事件的统计截止于 2026-11-02。跨层比较因此只能在各自窗口内成立：例如不能用读取回执的缺失推断某条更早的规则从未被读过。

各层规模如下。分母不同，不能串接为一个成功率或转化漏斗。

| 证据层 | 规模 | 时间窗 | 本文的用途 |
|---|---:|---|---|
| 冻结消息库 | 26,961 条 | 2026-06-16 – 09-03 | 目的性检索的范围，非逐条故障编码 |
| Task 与动作日志 | 2,221 个 Task；2,527 条动作 | 同上 | 确认状态顺序与动作归属 |
| Plan 与 generation 快照 | 26 个 Plan；195 条 generation；169 次父子转移 | 同上 | 重建任务图演化 |
| 只读 Git 对象核验 | 10 项 | 执行于本研究期间 | 核对祖先、文件存在性与完整树等价 |
| 团队规则治理与 Git 事件 | 30 个提案；57 个事件与 57 个提交 | 截止 2026-11-02 | 核对发布与版本一致性 |
| 规则正文读取审计 | 230 条去重回执 | 2026-08-14 – 09-21（左截断） | 核对跨成员访问 |

### 3.2 片段选择与证据级别

片段采用**目的性选择**：从消息检索与既有案例扩展中，纳入能够定位未满足义务、相关解释或后续动作的片段，同时保留正确承责、有效监督与合法等待的对照材料。这不是随机抽样，因此本文不报任何外推性的发生率、比率或趋势；在完整记录集上的计数（如 169 次转移中的分布）仍然给出，并标明分母。

每条证据按五级标注，正文的表述强度受其约束：

| 级别 | 含义 | 正文允许的表述 |
|---|---|---|
| 原生记录 | 冻结数据库中的消息、任务动作、generation 快照、读取回执、治理事件 | "记录显示……" |
| 产物核验 | 本研究实际执行过的只读 Git 对象检查 | "核验确认……" |
| 参与者报告 | Agent 或用户在消息里的自述，未被独立复核 | "……报告称" |
| 源码静态阅读 | 指定 commit 的提示、工具说明或入口实现 | "该版本源码中……" |
| AI 分析 | 本研究的编码、分类与解释 | "本文判定……"，并受 §3.4 约束 |

**技术故障的后果不计入设计层面的归因。** 线程损坏、工作区为空、配置缺失、工具说明与实际入口不一致、调度未触发、读取被截断，这些造成的停摆排除在绑定断裂之外；同一段材料中可以分离出来的责任外置表述仍然保留，用于机制分析。提交日期不视为部署日期：源码中存在某个版本的工具说明，不证明某次执行加载了该版本。

逐条的来源定位、证据级别与排除理由见 [证据台账](evidence-ledger.md)；不进入本篇的内容及其理由见 [删除与降级决定](exclusions.md)。

### 3.3 这项研究做不到什么

本文按软件工程案例研究的惯例区分情境解释与因果识别。[36]<!--ref:RV16_METHOD--> 以下限制适用于全文，后续各节不再重复。

- **单部署、目的性选例、事后框架。** 研究问题与六类断裂的切分是在阅读材料之后形成的，未预注册，也未完成全库盲法编码。因此本文不估计发生率、持续时间，也不比较本架构与其它架构的风险。
- **历史输入不可恢复。** 当次部署版本、完整模型输入与执行控制轨迹均未保留。部分案例因此永久停在参与者报告级别。
- **AI 参与了取证、编码、分析与写作。** 作者对其中一个子集做了人工判定（§3.4），其余部分的分类只有 AI 编码。模型响应是行为记录，不能直接揭示其内部推理。
- **记录覆盖本身有缺口。** 执行镜像与控制事件在快照中没有记录；定向查询的空结果不作为"历史上不存在该执行"的证明。
- **人工介入同时改变多个变量。** 用户的一次回复往往同时提供主体信息、动作提示与上下文焦点，因此不能把随后的行为变化归给其中某一项。

### 3.4 人工子集判定与一致率

为了让案例分类不是只有 AI 编码，作者对一个随机子集做了独立于本文结论的判定。

**抽样框**是台账中证据级别为原生记录或参与者报告、且能定位到单条原始记录的片段，共 50 条。以固定随机种子抽取 25 条，生成的编码表只含片段编号、原生标识、时间与原文路径，不含任何既有分类或解读性描述；判据定义单独成文，其示例全部为说明判据而构造，不取自样本。四类互斥：责任外置、合法升级或等待、技术故障、无法判定。判据与 §2.1 的三条判定条件一致。

**覆盖范围有限，必须明示。** 抽样框中的 50 条全部来自责任归属与凭据认可两类材料，对应 §4.1、§4.2 与 §4.6 的取材。§4.3 的信息绑定、§4.4 的成果与合同绑定、§4.5 的适用范围绑定**没有人工判定**：上述四类判据只回答"义务落在谁身上"，把规则治理事件或 Git 谱系核验套进这四类，只会批量产出"无法判定"。这三节的分类因此仍只有 AI 编码，§6.3 会再次说明这一点。

一致率、分歧条目及其处置见 §6.3。分歧本身作为结果报告，不向既有编码调整。本节的做法**不构成独立复核**：作者是该系统的所有者，不是独立编码者；可以报告的是"作者对子集作了人工判定，一致率为若干"，不能报告为盲法双编码的一致性检验。

<!-- 以下为 §4–§7 骨架，由 Task 6、Task 7 填入。 -->

## 4 六类绑定断裂

### 4.1 责任绑定：一次执行终止后，目标还归谁

记录中反复出现两种形态。第一种是把 executor 的失败解释为当前 Agent 之外的阻塞，需要外部主体接手；第二种是把 Supervisor 定位为验收者，拒绝不合格产物后即结束自身工作。两者都承认执行结果有问题，都没有把剩余的交付义务继续留在当前承担者身上。

2026-07-22 的两段记录属于第一种。dev2 报告内部恢复失败并表示需要外部介入，用户指出 executor 的执行责任属于同一个 Agent 后，它更正了解释并给出行动计划；同日 integration-dev 把自身尚未完成的任务描述为"验收外部交付"，用户纠正完成者定位后，它承认了责任外推。同一会话中另一名 PD 报告工作区为空与配置问题，并反对在未改配置的情况下盲目重试——这是**合法等待**的对照：主体说明了缺什么、为何此刻不能推进。

2026-08-06 的 I04 展示了状态识别与行动主体选择之间的脱节。Agent 已经注意到 Task 恢复为 running 后仍没有新的 execution，却据此报告需要中心修复调度，并再次阻塞任务。用户询问 executor 应由谁创建后，它修正解释并报告已显式调用 fork。原生动作日志确认了 reset、agent_started、blocked、unblocked 的顺序：用户纠正后 29.64 秒出现 unblocked，66.46 秒后出现显式 fork 的报告。**该片段止于参与者报告**——执行体创建与后续推送只有 Agent 自述，快照中没有对应的执行控制事件；同一任务随后又因合同缺口被阻塞。该版本源码中确实存在 reset 工具说明仍承诺自动派发、而实际入口已改为显式 fork 的缺陷，这与 Agent 的事后自述相容，但提交日期不证明当次执行加载了该版本。因此 I04 用于说明责任误归属与纠正过程，其停摆后果不计入设计层面的归因。

第二种形态在 2026-08 下旬到 09 月仍然出现：tester3 与 dev3 把"不合格结果对应的 Task 结束"误作自身责任结束，用户再次纠偏。这里有一处时序必须写明：**dev3 的修复任务在具体行动指令之后、第二条身份提醒之前就已创建**，且后来仍未成功交付。因此这组材料支持人工参与了责任重建，不能把恢复单独归因于一句身份提醒。

**明确承责与质量门槛可以并存。** 2026-08-07 的 I05 是反例：Agent 明确把 executor 视为自己的执行单元并承担最终裁决，但仍拒绝缺少生产窗口、迁移与消费者归零证据的删除候选，并把补证或回撤列为待处理工作。拒绝候选本身因此不能用作责任外置的指标；关键在于拒绝之后未满足事项是否仍有承担者。

**字段层面的归属正确，可以与语义层面的误归属同时存在。** 2,527 条 Task 动作中，597 条由迁移过程重建；其余 1,930 条非迁移动作覆盖 927 个 Task，占全部 2,221 个 Task 的 41.7%。另有 311 个 Task 只有迁移记录、983 个没有动作行，因此"存在任何动作行"的覆盖率（1,238 个 Task，55.7%）与"存在非迁移行为"的覆盖率回答的是不同问题。归一内部 Agent ID 与身份 ID 后，非迁移动作中的触发主体与所涉 Agent 的关系如下。

**表 1。非迁移动作中的触发主体与所涉 Agent。** 分母是各类动作的记录数；两个字段表示动作触发者与所涉 Agent，不是 Supervisor 与 executor 的进程身份。

| 动作 | 记录数 | 不同 Task 数 | 同一逻辑 Agent | 另一逻辑 Agent | 无法比较 |
|---|---:|---:|---:|---:|---:|
| agent_started | 819 | 778 | 818 | 0 | 1 |
| blocked | 453 | 269 | 453 | 0 | 0 |
| unblocked | 331 | 182 | 151 | 180 | 0 |
| retried | 10 | 6 | 5 | 5 | 0 |

审计系统保存了这层归属关系：819 条 `agent_started` 中有 818 条的触发主体与所涉 Agent 对应同一逻辑实体。但记录中的统一身份无法回答当时模型是否理解了应由谁启动 executor——I04 的语义误归属正发生在字段归属完全正确的情况下。331 条 `unblocked` 中有 180 条由另一个 Agent 触发，说明跨主体恢复也是该系统的正常行为之一，因此"由别人解除阻塞"本身不构成断裂。

### 4.2 凭据绑定：这张回执对应哪个候选，谁认可它

需要管理的依赖不是"谁负责合并"，而是"这张凭据适用于哪个对象、处于哪个阶段"。2026-06-16 的分支交接记录显示，职责共识不能消除对象歧义：integration 与 PD 都接受开发期合并与发布职责的划分，却对"迭代主干"指向不同对象——PD 把它表述为 main 并要求 integration 执行相应合并。用户随后明确区分开发分支、版本迭代分支与发布 main，并分别指定合并责任。角色共识没有自动形成对象与阶段共识。

同一组记录也包含一次有效的下游核对。角色映射被纠正后，PD 又以迭代分支不存在为前提要求重建；integration 核对远端后报告该分支已包含开发成果与验收证据，拒绝重复创建。下游没有因为指令来自协调者、或由用户转贴，就把错误前提当作事实继续执行。**该核对结论是参与者报告**：本研究没有重新执行当时的 Git 验证。此外，这组记录早于后来的内部 Supervisor/executor 设计，只用于跨角色协作的分析。

**独立验收的作用在于产生上游没有覆盖的事实。** 2026-09-03 的 Graph 记录中，Supervisor 已报告跨页合并、计数与回归通过，Tester 对同一候选继续要求点击聚合边之后的取证，指出计数正确仍不足以证明两页证据进入了详情视图；后续候选又暴露跨项目请求使用错误作用域的问题。这些判定对应逐步展开的检查范围，不是同一事实的重复确认。相应源码未能完整读取，因此这些案例只支持对检查报告本身的分析，不足以估计监督误判的比例。

这一类依赖在其它领域有成熟的处理方式可作参照：数据溯源研究把溯源记录的用途、粒度与信任模型分开讨论，[37]<!--ref:simmhan2005survey--> 而大规模代码评审的实证显示人类评审者同样依赖自动化证据（测试与分析结果）作出接受决定。[38]<!--ref:sadowski2018modern--> 两者都说明"证据由谁签发、适用于哪个对象"是可以被显式表达的，本部署的记录缺的正是这层表达。

### 4.3 信息绑定：消费者此刻的职责需要哪些事实

显示名、稳定标识、角色与权限在该部署中是四种分别实现的东西，混用其中任意两者都会得出错误推论。冻结配置中有 16 个 Agent 配置与 16 个稳定身份，Profile 名与显示名一致；14 个配置带有团队角色，共 15 条角色指派行、7 种角色定义；其中一个身份同时兼任 PD 与 PM，Dev 与 Tester 角色各对应多个身份。显示名可以在稳定标识不变的前提下修改，运行目录按身份标识定位。因此角色不等于身份，身份不等于权限。

治理记录给出一个直接的反证：被称为 PD 的成员报告收到 `not_memory_curator`，说明自己可以读取和提案，却不能晋升或拒绝规则。**这是合法的权限边界，不计为协作失败**——角色称呼不自动携带治理授权。

命名也没有阻止可行动信息的传播。v2.14 的记录中，开发成员指出规格与实际数据库之间的差异，后续 finding 报告针对空值语义修正了单活索引；另一条系统消息向下游注入 19 条 finding，涵盖事实、约束与补丁摘要。前者提供了具体修正的报告链，后者记录了信息的可获得性。该案例反驳了"命名必然妨碍共享"这一强命题，但没有恢复全部消费轨迹，也不能据广播条数推定所有内容都被使用。

原生审计给出跨成员访问的结构证据。230 条去重正文回执覆盖 12 个 Agent、2 个团队、8 个提交。按 `(团队, 提交, 规则 slug)` 计为版本化规则时共 34 条，其中 30 条被至少两个 Agent 读取；把版本折叠、只按规则 slug 计时共 10 条，其中 9 条被至少两个 Agent 读取。230 条回执对应 67 个唯一审计上下文，其中 63 个关联规划会话（211 条回执）、4 个关联执行标识（19 条）。按首次留存回执的 phase 统计，execute 125、plan 38、review 40、recovery 27。

**回执能证明什么要写清楚。** 回执由服务端成功查询正文后登记，因此比消息中的"已读"声明更接近实际访问；但它不证明客户端完整收到、模型理解或正确执行。各规则的回执数差异很大——契约到生产链 60、写后回读 49、main 门禁 46、中间候选例外 19，最少的一条只有 2——而需求暴露程度、规则存续时间、Agent 活跃度与上下文重启频率都不同，因此**读取分布不能用于判断哪条规则对交付更有作用**，也没有有效复用机会的共同分母。审计表中的空格表示没有该条审计记录，不表示该 Agent 没有访问能力。

一位开发者在回顾中提到，给 Agent 配上名字使界面呈现为一个可以扮演 leader 的团队，带来掌控感。这是**单人回顾性自述**，用于说明可读呈现有独立的人机交互用途，不进入任何关于自动交付的判断。

### 4.4 成果与合同绑定：保留了节点不等于继承了产物

**改图组织后续工作，代码修复可以独立发生。** Mermaid 记录的顺序是：真实浏览器检查暴露多图渲染缺口并给出 REJECT；开发成员报告修复候选已推送；**之后**才出现增加修复与复验节点的新 generation；最后对同一候选报告复验通过。改图比修复报告晚 46.49 秒。因此这次改图表达的是正式的后续路径，不能解释为它产生了代码修复。Access 提供了另一条路径：局部候选因缺 Risk 字段与删除确认问题被拒后，负责人要求在原分支追加修正，后续报告同一 Task 通过；该 Plan 的原生记录只有一个初始 generation 与六个任务。一次拒绝不必机械地触发新一代。

**保留历史节点不等于代码继承了成果。** 组织 Graph 的后继代际保留了此前的工作记录，但一个中间候选从旧 main 构建，没有继承已获局部验收的能力。只读 Git 核验确认：前一候选不是该中间候选的祖先，相应后端文件也从存在变为缺失；后续恢复候选重新包含了预期的祖先关系。**这是产物核验级别的证据**，与"图中保留了节点"是两件事。错基线的来源尚未隔离，因此该案例支持增加产物核查，不证明任务图机制造成了代码丢失。

反方向的证据同样存在：Spark 的两个不同提交拥有相同父提交与完整相同的文件树，因此提交标识变化不必然意味着内容丢失。祖先检查与整树检查需要配合使用——前者发现真实缺口，后者避免把内容等价的替代误判为丢失。

**合同范围变化会改变"完成"的含义。** Spark 的初始合同要求覆盖二十五类，而测试只断言"至少十二类"，补齐后才对齐；其最终发布矩阵仍为 NO-GO，局部恢复不等于产品完成。Access 则先撤回旧的整页 PASS，用户随后修改信息架构，新 Plan 在新合同下获得终验报告——新结果适用于新候选与新范围，**不追认已撤回的旧判断**。

结构统计说明调度器通常保留工作历史，但这件事与产物继承无关。169 次父子转移中，149 次（88.2%）保留了全部父节点；逐一留出单个 Plan 重算，该比例仍在 85.4%–91.1% 之间。然而把"改了图"与"推进了交付"分开数之后，差别就出来了：

**表 2。父子快照中的拓扑变化与共有任务状态变化。** 分母是转移，不是任务数；后两列可以重叠。"完成"是任务字段取值，不是重新核验过的交付。新建任务不计入"共有任务"。

| 转移类型 | 次数 | 至少一项共有任务改变状态 | 至少一项共有任务转为完成 |
|---|---:|---:|---:|
| 节点或依赖改变 | 166 | 157 | 101 |
| 节点与依赖均未改变 | 3 | 3 | 1 |

166 次拓扑改变中有 65 次没有任何共有任务转为完成——其中可以包含安排未来工作，但不能计为推进；3 次拓扑完全未变的转移则全部记录了共有任务的状态变化，其中 1 次有任务转为完成。因此"拓扑相同即重复快照"的读法不成立，而"代际增长即进展"的读法同样不成立。同一批 Plan 的累计节点数中位数从根记录的 8 增至最后记录的 17.5，这描述的是历史保留与局部增量的累积，不是实际工作量。

**两种外观相似的重复必须分开。** Insight 的处置记录在视觉证据不足时保留了已有候选与技术验收，只替换视觉复验及其下游交付节点，随后出现终验报告——这是补上一次真正缺失的检查。同一案例另有重复创建五个节点并最终全部废弃的记录，其中一个曾进入 running。参与者把原因归于读取截断，但完整工具响应与并发轨迹缺失，**尚不能排除系统原因，因此这五个节点不计入设计层面的归因或成本估算**。

### 4.5 适用范围绑定：规则的阶段条件与任务阶段

团队规则的发布路径留下了可追溯的过滤。截止 2026-11-02，共 30 个提案，全部为 `add` 操作——没有观察到 update、disable 或 delete 提案；27 个已审核（26 次由一个 agent curator 完成、1 次由用户完成），3 个仍 pending；15 个晋升、12 个拒绝。两个非空团队仓库的 57 个提交与数据库中的 57 个团队事件逐一对应，事件的提交集合与 Git 提交集合相同，57 条状态全部一致。发布结果是 10 条 canonical 规则与 5 个条目。

12 条拒绝理由中，11 条明确指向被 canonical 条目吸收、拆分或替代；剩余 1 条认为当时尚未稳定的 DAG 实现不宜升级为全局硬规则；另有一条指出机器本地绝对路径不适合作为团队通则。治理层因此做了可见的边界整理，而不只是存储所有经验。**但授权不等于独立核查**：27 个已审核提案中有 16 个作者与审核者相同。因此"拒绝了 12 条"不能读作"阻止了 12 条错误知识"。

审核等待时间不能用一个中位数概括。27 次审核的合并中位数为 1,041.89 分钟，但分团队看，26 个提案的中位数为 1,042.09 分钟、最小 0.15 分钟、最大 4,077.31 分钟，另一团队唯一的已审核提案为 4.18 分钟。这组分布同时包含一次集中整理与多次即时追加两种使用方式，用合并中位数描述会同时掩盖两者。

**本节的核心记录是一次适用范围错配。** 一条团队规则要求所有任务在完成前都合入主分支，而候选任务的合同又要求先独立验收才能合并；下游验收依赖候选完成，两条规定因此形成循环。任务的原生阻塞理由直接指出了这一矛盾。把候选改为 evidence-only 并重建流程之后，后继节点仍因规则没有例外而阻塞——**改图改变了任务组织与名称，没有改变行为规范的适用条件**。要求最终交付进入主分支有明确用途，问题在于它被无条件用于中间候选。这个解释不依赖读取失败或调度故障；owner 在对话中也强调了同一约束，因此不能把全部影响专归于持久记忆。

后来发布的规则明确区分中间实现／验收与最终集成：前者可依据唯一、不可变、远端可达的候选完成，后者仍需满足主分支门禁。该规则有 19 条正文回执、覆盖 8 个 Agent，并在后续任务中被 PD 与 tester 明确引用，原生事件显示独立验收节点完成、随后集成节点完成。可复用的内容因此是与候选标识和任务阶段相连的判断条件，而不是"遵守正确流程"这类笼统要求。但该片段同时存在人工提示、已有验收报告与新增集成节点，**能够确认的是条件化规则被用于一次交接，不能确认它单独带来了恢复**。

**纠正采用追加而非替换：旧的宽规则仍处于 enabled，新规则也 enabled。** 这保留了最终交付门禁，却把两条规则如何组合的解释留给了消费者。现有材料支持"规则更新应表达替代、细化或例外关系"这一设计要求，没有测出冲突频次或额外推理成本。

**发布、读取与采用是三种状态。** 一个团队的三个提案在快照截止时仍 pending，等待 41.87 至 54.01 天；但其中一种新工作方法在提案之前的同一会话输出中就已出现，而之后读取的仓库提交虽然包含新提案，所读到的仍是旧的 canonical 规则。仓库更新不等于规则生效，尚未发布也不等于当次没有采用。

关于版本新鲜度有一条反证值得记录：全部 230 条回执使用的提交都不早于读取之前最新已入库的团队变更，规则正文 blob 也一致（230/230）。这反对"提交冻结必然导致旧知识误用"的一般说法；但它只覆盖有审计的读取时点（2026-08-14 起），不能证明长会话的后续输入始终新鲜，也不排除绕过该接口的访问。

私有层的材料只支持有限结论。恢复到的是两个新建 home 的初始化历史与 23 份遗留 Markdown 文件，按作用域分布为 task 15 份、project 3 份、issue 3 份、legacy 根目录 2 份、agent 根目录 4 份。这些笔记保存了用户要求、候选与验收缺口，具有追溯价值，但**没有把写入内容与后续任务决策相连的连续记录**。因此本文不比较私有、项目与团队三层记忆的复用效果——私有历史的缺失使这个比较在当前材料上无法成立，而不是比较出了某个结果。

### 4.6 剩余义务：局部完成与未满足的交付义务并存

门槛可以暴露缺口，却不自动确定补足者。2026-08-28 的 W01 记录中，流程要求每个任务都有负责人，PD 也知道实现与独立验收需要分开，但它把下一步表述为"需要先确定"负责人。用户要求其自行分配后，原生初始 generation 记录了五个节点分给四个不同的 assignee，系统随后通知首个任务 ready——用户指令后 31.30 秒保存了带分配的 generation，31.87 秒出现 ready 消息。可见的缺口位于"前提不满足"与"由谁补足前提"之间：用户明确了分配动作的承担者，没有取消独立验收，也没有提供新的仓库或执行环境。**首次空 assignee 与启动被拒仍只有 PD 的报告**，后续分配才有结构化证据；因此该片段支持一次需要人工补足动作绑定的协调过程，不能据此确定无人介入时的结果。

**工作项合同与产品目标必须分别追踪。** Spark 的报告任务合同是给出可审计的 GO 或 NO-GO，而不是保证产品可发布。报告被接受时仍列明了输入、环境与实现方面的发布缺口，持久化的报告也保存了 NO-GO 结果。因此"报告任务完成"与"产品尚不可发布"可以同时成立，目标层仍需追踪剩余条件。与此相对，另有四组近似消息承担了 Task 结论向 Plan 层的传播或完成状态补记，未见相应的新增产品检查——它们提供同步证据，**不能累计为独立验收**。

人工补充的内容可以具体定位。两个固定区间共 28 条消息，其中 12 条由用户发出（W01 的 6 条中有 3 条，W02 的 22 条中有 9 条）。按互斥主类别编码，12 条用户消息中 5 条是启动或继续要求，5 条补充或质询责任、对象及操作顺序，1 条是转贴的操作指令，1 条是完成状态追问。相邻消息的发送者切换为 W01 的 5 对中 4 对、W02 的 21 对中 18 对。

这些比例描述所选案例的沟通构成，不代表全系统的人工干预率；下一条 Agent 消息的时间也不等于问题解决的时间。**而且人工参与并不天然提供正确事实**：那条转贴的重建指令随后被 Agent 的分支核查反驳。因此"人工介入"不是一种干预，而是至少四种——补主体、补对象与阶段、纠正操作顺序、催促继续，评价时需要分别记录。

## 5 机制与竞争解释

### 5.1 每类断裂的候选机制、反证与仍缺的判别

本节把每类断裂的候选机制与它的反向记录并列。**每一行都有反向记录**，这不是修辞上的平衡：一个只有支持证据的机制说明取证时没有找反例，而不是该机制更可靠。

**表 3。六类断裂的候选机制、反向记录与仍缺的判别条件**

| 断裂类别 | 候选机制 | 本部署的反向记录 | 要把该机制与替代解释分开，还缺什么 |
|---|---|---|---|
| 责任 | 执行隔离被进一步解释为责任隔离：审查子目标完成被误作目标责任完成 | I05 明确承责仍拒绝自己的候选；819 条 `agent_started` 中 818 条字段归属正确 | 在工具一致、无旧上下文、恢复路径可用的条件下，分别操纵身份表述与恢复义务的表达 |
| 凭据 | 认可条件未在交接时被一致理解，接收方以"我没有直接观察到"替代"不存在" | 下游核对远端后拒绝按错误前提重建分支 | 固定任务与权限，操纵凭据的可查询性与签发者约束 |
| 信息 | 按材料生产者的身份或角色过滤，遗漏消费者当前职责所需的跨角色事实 | 命名成员之间确实传播了可行动的事实；34 条版本化规则中 30 条被多个 Agent 读到 | 实现一个真实检索器，在同预算下比较角色视图、任务视图与两者结合 |
| 成果与合同 | 图中保留节点被当作产物已继承；旧判定被当作对新合同仍然有效 | 不同提交同完整树说明提交变化不必然丢内容；Access 在原任务续修即可 | 在相同初始产物与合同下，实际执行续修、局部改图与重规划三种策略并独立验收 |
| 适用范围 | 局部经验以无条件通则发布，扩大了规则与任务合同的不相容范围 | 条件化规则被用于一次交接；230/230 回执的提交不早于最新已入库事件 | 同一候选与合同下回放四种规则版本，再用未见任务比较实际采用 |
| 剩余义务 | 局部合同完成掩盖了产品层未满足的条件 | NO-GO 报告任务完成而产品未可发布被正确区分 | 操纵剩余义务的显式登记，测无提示推进与义务丢失 |

四条外部线索与这些机制相容，可用于定位，但都不是对本部署的实证。**凭据**一类最接近的既有工作系统化了 coding-agent harness 中"批准—执行绑定"的失效，即被批准的对象与真正执行的对象不是同一个；[39]<!--ref:wang2026approval--> 溯源研究则给出了"他方签发的断言此方是否接受"的形式框架。[40]<!--ref:moreau2010foundations--> **责任**一类对应需求工程侧关于显式规定委派自治边界的讨论，[41]<!--ref:arora2026specifying--> 以及重试与恢复路径上授权如何被重新评估或跳过。[42]<!--ref:marla2026permission--> **信息**一类可与 Agent 侧工具选择中的过度授权现象对读，[43]<!--ref:yang2026privileges--> 以及可见信息与信息被使用之间的差距；[44]<!--ref:R32--> 显示身份本身的效应在人名、机器式名称与无名条件下未检出稳定主效应，[45]<!--ref:R17--> persona 提示在事实问答中也未显示稳定增益，[46]<!--ref:R15--> 这两项都不是软件交付的结果变量。**成果**一类的设计空间包括复用推导轨迹而非成品，[47]<!--ref:veloso1993derivational--> 以及把计划修复当作跨责任边界的协调机制。[48]<!--ref:vanderkrogt2005coordination-->[49]<!--ref:komenda2012decentralized--> **适用范围**一类有两条直接相关的线索：写入闸门不足会把偶然共现的经验晋升为团队规则，[50]<!--ref:qi2026memory--> 而带条件的规范可以用道义逻辑显式表达义务、许可与适用范围。[51]<!--ref:joshi2026deontic--> 组织记忆的收集、存储与访问机制及其失效，[52]<!--ref:olivera2000memory--> 知识在组织单元间迁移时载体变化导致适用性不自动继承，[53]<!--ref:argote2000knowledge--> 以及把组织记忆作为 agentic 流程的一等构件，[54]<!--ref:kirchdorfer2026organizational--> 共同说明这一类问题不是本部署独有。

**六类机制可以叠加，本研究无法分离它们。** 用户的一次提醒同时改变主体信息、动作提示与上下文焦点；一次改图同时改变任务组织与命名。因此表 3 最后一列不是"未来工作"的客套话，而是本研究给出的具体识别条件：每一行都指明了要分离哪两个变量。

### 5.2 训练先验为何只能列为未识别解释

一种与所有六类记录相容的解释是：模型从训练中习得的角色分工、工具调用与任务完成模式，不适配本系统要求的责任映射。这个解释不需要假定模型内部存在固定的 Agent 组织结构，它的可观察预测是：在输入、工具语义与权限都明确时，模型仍倾向于选择某种熟悉的分工或启动方式。

外部研究支持"训练可以塑造交互行为"：通过交互轨迹微调可以改变 agent 行为，且训练数据组成会影响表现；[55]<!--ref:R30--> 消息来源的优先级行为也可以通过训练塑造。[56]<!--ref:R31--> 但这两项都没有检验本系统的封装，也不揭示历史部署所用模型的训练内容；后者的层级是系统／开发者／用户消息的优先级，不是业务中的监督者与执行者层级。

因此本文把训练先验列为**未识别的竞争解释**，而不是候选原因之一。要让它成为可检验的原因，需要固定基础模型与训练量、对同源模型做成对的轨迹训练干预。本研究没有这个条件，也不能从模型自述的 reason 文本反推训练来源——模型响应是行为记录，不是其内部推理的转录。还须把权重中的倾向与外部 CLI 自带提示、当前工具描述、会话历史分开；即使不同模型在同一 harness 中表现不同，也只说明模型与接口存在交互。

### 5.3 单代无环为何不排除跨代重复

每一代任务图无环，约束的是**该版本内**任务之间的依赖关系。如果验收未通过就触发重规划，而规划器每次都复制同一缺口、同一候选与同一检查，这个循环发生在**多次规划之间**——任何单张图上的环检测都发现不了它。

本部署的记录支持这种现象存在，且只支持到"有限重复"：Insight 案例中有一组重复创建的五个节点，其中一个曾进入 running，最终五个全部废弃，未见这组节点带来新的交付证据。同一案例的另一条路径则保留已有候选、只替换缺失的视觉复验，随后出现终验报告。**两条路径外观相似，区别在于后一条补上了一项真正缺失的义务。**

判别循环成因有三个可检查的位置。其一，系统是否把新代号当作新进展，未识别已有的活动恢复任务——Insight 与此相容，但根因未定，参与者关于读取被截断的说明无法核验。其二，后继是否从错误的基线重新引入已修缺陷——组织 Graph 的产物核验说明这种退步可以用 Git 检查发现，但该例没有证明反复退步。其三，失败是否来自未改变的外部前提——此时重复规划不能创造资源，而若用户确实改变了验收范围，则应记录为新合同而不是报告为循环。

据此可以提出一条**尚待验证**的停止规则：改图前读取当前活动版本，核对目标与验收版本、候选文件树、未满足的检查及外部前提；若已有任务处理同一缺口，继续或复用它；仅当出现新义务或依赖变化时才局部改图。连续返回同一状态且没有新证据时，停止自动重建并交给有权限者决断。这条规则的依据是上述三个检查位置，不是对比实验的结果。要检验它，需要在固定初始产物与合同的条件下实际执行三种策略；已有一项在行程规划领域对扰动后 replan／repair／edit 三种修订策略做统一评测的工作，其任务域与软件交付不同，可借的是实验范式而不是结论。[57]<!--ref:yuan2026replan--> 仅凭拓扑未变就停止是错的：本部署中 3 次拓扑完全未变的转移全部伴随共有任务的状态变化。

## 6 证据边界

### 6.1 这些记录支持到哪一层

本文的全部结论停在两层：**描述**（记录里有什么）与**局部机制解释**（某一段记录中，义务是在哪一步失去承担者的）。没有任何结论属于因果识别。

具体地说，本文**不给**以下数值，且这不是因为计算未完成，而是因为材料在原理上不支持：

- **发生率与比率。** 片段是目的性选择的，没有候选片段的完整纳排台账，因此分子有而分母无。完整记录集上的计数（如 169 次转移中 149 次保留全部父节点）仍然给出，并标明分母；这类计数不能外推到窗口之外，也不能换算为"每多少次交付出现一次断裂"。
- **持续时间。** 消息间隔只描述所选区间内相邻记录的时间差。下一条 Agent 消息的时间不是问题解决的时间，也不是无人介入时的停摆时长。
- **相对架构风险。** 没有对照组。本部署的混合案例不能推出"Supervisor–executor 这类分工普遍不可行"；既有工作中存在该类分工的可行正例。
- **改善趋势。** 操作者关于"早期频繁、提醒后改善、后来偶发"的回顾得到部分同期记录的支持（多主体纠偏、提示修订、正确承责与后续错误并存），但没有全时段的机会分母。不同日期涉及不同 Agent、会话与未知的部署版本，因此这不能称为同一模型学会后又遗忘。

还有一类限制作用于全部解释：**人工介入同时改变多个变量**。用户的一次回复往往同时提供主体信息、动作提示与上下文焦点，Agent 的事后说明也可能是合理化。§4.1 中 dev3 的修复任务早于第二条身份提醒这一时序，正是用来阻止"一句身份提醒导致了修复"这种读法。

### 6.2 记录覆盖的缺口

**两类对象在快照中没有记录：执行镜像与 worker 控制事件。** 对它们的定向查询返回空结果。空结果**不作为**"历史上不存在该执行"的证明——它只说明这两类记录没有进入快照。直接后果是 I04 的执行体创建与后续推送无法核验，该片段永久停在参与者报告级别。

其他已知缺口：

- **历史输入不可恢复。** 当次部署版本、完整模型输入与执行控制轨迹均未保留。源码中存在某个版本的工具说明，不证明某次执行加载了该版本；提交日期不是部署日期。
- **部分结论只有参与者报告。** W02 的远端分支状态、I04 的 fork 调用、Mermaid 的修复推送、各案例的历史 PASS，均未由本研究重新执行验证。只读 Git 核验覆盖的是 10 项指定对象的祖先、文件存在性与完整树关系，不是重跑产品验收。
- **读取审计左截断。** 规则正文回执只覆盖 2026-08-14 之后，更早发布的规则在该审计中必然缺失。审计表中的空格表示没有该条记录，不表示没有访问能力；回执也经过重复键去重，不代表读取次数。
- **跨层窗口不同。** 消息与动作、读取审计、治理与 Git 统计三层的时间窗不同（§3.1），跨层推断只在各自窗口内成立。
- **私有层历史不完整。** 恢复到的只有两个新建 home 的初始化内容与 23 份遗留文件，因此本文不比较私有、项目与团队三层记忆——这是材料缺失，不是比较得出了结果。

### 6.3 人工判定覆盖的范围

作者对一个随机子集做了独立于本文结论的判定，其**覆盖范围有限且不对称**，必须按下表读：

| 小节 | 分类是否有人工判定 |
|---|---|
| §4.1 责任绑定 | 是（在抽样框内） |
| §4.2 凭据绑定 | 是（在抽样框内） |
| §4.6 剩余义务 | 是（在抽样框内） |
| §4.3 信息绑定 | **否，仅 AI 编码** |
| §4.4 成果与合同绑定 | **否，仅 AI 编码** |
| §4.5 适用范围绑定 | **否，仅 AI 编码** |

抽样框是台账中证据级别为原生记录或参与者报告、且能定位到单条原始记录的片段，共 50 条，以固定种子抽取 25 条。§4.3–4.5 没有人工判定的原因在 §3.4 已说明：四类判据只回答"义务落在谁身上"，套到规则治理事件或 Git 谱系核验上只会批量产出"无法判定"。这一限制的风险是明确的——**这三节的分类错误不会被任何人工环节拦住**，读者应按"AI 编码、未经人工复核"对待其分类，而其中的定量描述（回执计数、治理事件、Git 核验结果）不依赖分类，仍可按各自的证据级别采信。

本节的做法不构成独立复核：作者是该系统的所有者，不是独立编码者。可以报告的是"作者对子集作了人工判定，一致率为若干"，不能报告为盲法双编码的一致性检验。**一致率与分歧条目待作者完成判定后填入**；分歧将作为结果报告，不向既有编码调整。本文在该结果填入之前，上表中"是"的三节同样应按"仅 AI 编码"对待。

## 7 结论

### 7.1 六类断裂与由此得到的设计要求

在这个部署的记录里，交付失败很少是因为模型写不出代码。反复出现的是另一件事：一项尚未满足的义务跨过某条边界之后，它与承担者、凭据或合同之间的绑定关系失去了确定性，而参与各方仍在按局部规则正确行动。六类断裂是这件事的六种形态。

把它们并排放在一起，可以读出一条共同的设计要求：**边界两侧都需要显式携带那条被跨越的关系**。

| 断裂类别 | 系统应持续记录的内容 |
|---|---|
| 责任 | 目标负责人独立于某次执行结果与单个任务终态保存；执行失败后向有权限者提供候选、未满足的验收项与可用的恢复动作 |
| 凭据 | 回执标注候选版本、检查范围、结果与签发者，使接收方能独立核对而不必让另一角色重述 |
| 信息 | 按消费者当前职责、任务与授权组织证据，而不是按材料生产者的身份或角色过滤 |
| 成果与合同 | 后继声明列出保留的能力、精确候选与适用合同；范围变化时标明哪些旧检查仍有效、哪些必须重做 |
| 适用范围 | 规则随文记录适用阶段、触发条件、例外与替代版本；更新表达替代关系而不是追加并列 |
| 剩余义务 | 局部合同完成时，仍未满足的产品层条件保持可定位，并关联有能力且获授权的主体 |

其中"凭据"一项在软件供应链领域已有可借的工程形态：可证实的构建链让可复现构建产出可被第三方验证的 attestation，[58]<!--ref:muto2026attestable--> 证据驱动的持续集成协议则让流水线每一步产出可验证证据并逐步累积。[59]<!--ref:castillo2026evidence--> 这说明把回执与候选产物绑定在技术上不是难题；本部署缺的是在交接协议里要求它。

这些要求来自本部署的记录，**不是经过对照验证的改进方案**。它们各自要求的是"把某个关系写下来"，而不是"增加一道流程"。本文的材料能支持的只有一件事：这些关系在当前实现中可以同时为真而未被表达。

因此评价这类设计时，应当分别观察四个结果，而不是合成一个成功率：模型是否承接了义务；恢复动作是否真的生效；候选是否通过了独立验收；以及人工补足了多少次、补的是哪一类关系（主体、对象与阶段、操作顺序、还是催促继续）。把这四项合并成一个指标，会让"正确拒绝"与"义务丢失"落在同一侧。

最后，有三件事本文明确没有做到：没有估计任何断裂的发生率，没有识别封装本身的因果作用，也没有把现场现象归因于模型的训练先验。<!--allow-banned:发生率-->要回答这些，需要的是表 3 最后一列列出的那些条件——固定权限与事实，一次只操纵一个变量。

## 声明

**作者贡献。** oopslink 是本文唯一作者：提出研究问题、提供部署背景与现场解释、决定研究范围，并对一个随机子集完成人工判定。

**AI 使用。** 本研究在取证、编码、定量复算、实验脚本撰写与文稿起草中使用了 AI 助手（Claude Opus 5）。AI 不是本文作者——生成式 AI 无法对文稿内容负责，也无法同意投稿平台的条款。AI 生成的分类与解释由作者审阅后保留或修改；§3.4 与 §6.3 说明了人工判定覆盖的范围与未覆盖的部分。作为分析对象的模型响应属于方法与材料，不属于作者贡献。模型响应是行为记录，不能直接揭示其内部推理。

**伦理与隐私。** 本研究使用作者自有系统的既有工程协作记录，属于非人类被试研究，未新增用户实验，未采集第三方个人数据。本文不声称已获机构审查批准，也不声称经判定豁免——作者未就此征询审查机构。记录中包含用户与 Agent 的协作内容、内部路径与稳定标识；公开前由作者复核可识别信息并作必要脱敏，原生标识保留用于本地可追溯性。

**数据可用性。** 部署原件保存在授权本地档案，不公开。公开的是只含派生物的复现包：动作日志派生表、读取回执审计的脱敏版、169 次父子转移表、Git 对象哈希与核验脚本。原始协作对话不在其中，因此本文的案例可按标识追溯，但不可还原到具体个人。

**利益冲突。** 作者是本文所研究系统的开发者与运营者。这一关系同时构成本研究的材料来源与其主要局限：作者不是独立编码者，§6.3 据此限定了人工判定的表述。

**资助。** 本研究未获得资助。

**同源材料。** 本文与同批的受控实验研究使用**互不重叠**的证据：本文只用部署记录，该研究只用新采集的实验数据。本文内部，§4.3 与 §4.5 共用同一批 230 条读取回执，已在相应位置标明不重复计数。

## 参考文献

[1] Malone, Thomas W., Crowston, Kevin, “The interdisciplinary study of coordination,” *ACM Computing Surveys*, vol. 26, no. 1, pp. 87–119, 1994. DOI: [10.1145/174666.174668](https://doi.org/10.1145/174666.174668).

[2] Schmidt, Kjeld, Simone, Carla, “Coordination mechanisms: Towards a conceptual foundation of CSCW systems design,” *Computer Supported Cooperative Work (CSCW)*, vol. 5, no. 2-3, pp. 155–200, 1996. DOI: [10.1007/BF00133655](https://doi.org/10.1007/BF00133655).

[3] Feldman, Martha S., Pentland, Brian T., “Reconceptualizing Organizational Routines as a Source of Flexibility and Change,” *Administrative Science Quarterly*, vol. 48, no. 1, pp. 94–118, 2003. DOI: [10.2307/3556620](https://doi.org/10.2307/3556620).

[4] Cemri, Mert, Pan, Melissa Z., Yang, Shuyi, Agrawal, Lakshya A., Chopra, Bhavya, Tiwari, Rishabh, Keutzer, Kurt, Parameswaran, Aditya, Klein, Dan, Ramchandran, Kannan, Zaharia, Matei, Gonzalez, Joseph E., Stoica, Ion, “Why Do Multi-Agent LLM Systems Fail?,” arXiv:2503.13657, 2025. DOI: [10.48550/arXiv.2503.13657](https://doi.org/10.48550/arXiv.2503.13657). （预印本，未经同行评审）

[5] Shah, Mehil B., Morovati, Mohammad Mehdi, Rahman, Mohammad Masudur, Khomh, Foutse, “Characterizing Faults in Agentic AI: A Taxonomy of Types, Symptoms, and Root Causes,” arXiv:2603.06847 [cs.SE], 2026. DOI: [10.48550/arXiv.2603.06847](https://doi.org/10.48550/arXiv.2603.06847). （预印本，未经同行评审）

[6] Albayaydh, Wael, Zhao, Rui, Flechais, Ivan, “Beyond the Leaderboard: A Synthesis of Tool-Use, Planning, and Reasoning Failures in Large Language Model Agents,” arXiv:2607.05775 [cs.AI], 2026. DOI: [10.48550/arXiv.2607.05775](https://doi.org/10.48550/arXiv.2607.05775). （预印本，未经同行评审）

[7] Zhou, Jinhao, Zhang, Xiaoyu, Yu, Jiongchi, Chen, Fahao, Wu, Jun, “When Messages Go Wrong: Understanding Communication Faults in LLM-Based Multi-Agent Systems,” arXiv:2610.15459 [cs.SE], 2026. DOI: [10.48550/arXiv.2610.15459](https://doi.org/10.48550/arXiv.2610.15459). （预印本，未经同行评审）

[8] Pitre, Priya, Hou, Jun, Fang, Yi, Ramakrishnan, Naren, Wang, Xuan, “Understanding Communication Failures in Multi-Agent LLM Systems,” arXiv:2610.32060 [cs.AI], 2026. DOI: [10.48550/arXiv.2610.32060](https://doi.org/10.48550/arXiv.2610.32060). （预印本，未经同行评审）

[9] Zhao, Xiangxin, Li, Han, Li, Shuaiting, Zhao, Tianyi, Barr, Earl T., Sarro, Federica, Ye, He, “Failure as a Process: An Anatomy of CLI Coding Agent Trajectories,” arXiv:2607.09510, 2026. DOI: [10.48550/arXiv.2607.09510](https://doi.org/10.48550/arXiv.2607.09510). （预印本，未经同行评审）

[10] Chen, Jingyi, Chen, Songqiang, Zhu, Hengcheng, Cao, Jialun, Shen, Jiasi, Cheung, Shing-Chi, “Understanding Agent-Reactive Bugs at the Model-Harness Boundary: An Empirical Study of LLM Agent Issue Reports,” arXiv:2607.15684, 2026. DOI: [10.48550/arXiv.2607.15684](https://doi.org/10.48550/arXiv.2607.15684). （预印本，未经同行评审）

[11] de Macedo, Sanderson Oliveira, “What makes a harness a harness: necessary and sufficient conditions for an agent harness,” arXiv:2606.10106 [cs.SE], 2026. DOI: [10.48550/arXiv.2606.10106](https://doi.org/10.48550/arXiv.2606.10106). （预印本，未经同行评审）

[12] Fei, Tianxiang, Chen, Cheng, Pan, Yue, Zheng, Mao, Song, Mingyang, “CodeDelegator: Mitigating Context Pollution via Role Separation in Code-as-Action Agents,” arXiv:2601.14914, 2026. DOI: [10.48550/arXiv.2601.14914](https://doi.org/10.48550/arXiv.2601.14914). （预印本，未经同行评审）

[13] Wu, Qingyun, Bansal, Gagan, Zhang, Jieyu, Wu, Yiran, Li, Beibin, Zhu, Erkang, Jiang, Li, Zhang, Xiaoyun, Zhang, Shaokun, Liu, Jiale, Awadallah, Ahmed Hassan, White, Ryen W., Burger, Doug, Wang, Chi, “AutoGen: Enabling Next-Gen LLM Applications via Multi-Agent Conversation,” arXiv:2308.08155, 2023. DOI: [10.48550/arXiv.2308.08155](https://doi.org/10.48550/arXiv.2308.08155). （预印本，未经同行评审）

[14] Hong, Sirui, Zhuge, Mingchen, Chen, Jiaqi, Zheng, Xiawu, Cheng, Yuheng, Zhang, Ceyao, Wang, Jinlin, Wang, Zili, Yau, Steven Ka Shing, Lin, Zijuan, Zhou, Liyang, Ran, Chenyu, Xiao, Lingfeng, Wu, Chenglin, Schmidhuber, Jürgen, “MetaGPT: Meta Programming for A Multi-Agent Collaborative Framework,” arXiv:2308.00352, 2023. DOI: [10.48550/arXiv.2308.00352](https://doi.org/10.48550/arXiv.2308.00352). （预印本，未经同行评审）

[15] Qian, Chen, Liu, Wei, Liu, Hongzhang, Chen, Nuo, Dang, Yufan, Li, Jiahao, Yang, Cheng, Chen, Weize, Su, Yusheng, Cong, Xin, Xu, Juyuan, Li, Dahai, Liu, Zhiyuan, Sun, Maosong, “ChatDev: Communicative Agents for Software Development,” *Proceedings of the 62nd Annual Meeting of the Association for Computational Linguistics (Volume 1: Long Papers)*, pp. 15174–15186, 2024. DOI: [10.18653/v1/2024.acl-long.810](https://doi.org/10.18653/v1/2024.acl-long.810).

[16] Zhang, Jiayi, Xiang, Jinyu, Yu, Zhaoyang, Teng, Fengwei, Chen, Xionghui, Chen, Jiaqi, Zhuge, Mingchen, Cheng, Xin, Hong, Sirui, Wang, Jinlin, Zheng, Bingnan, Liu, Bang, Luo, Yuyu, Wu, Chenglin, “AFlow: Automating Agentic Workflow Generation,” arXiv:2410.10762, 2024. DOI: [10.48550/arXiv.2410.10762](https://doi.org/10.48550/arXiv.2410.10762). （预印本，未经同行评审）

[17] Wang, Yanbo, Xu, Zixiang, Huang, Yue, Wang, Xiangqi, Song, Zirui, Gao, Lang, Wang, Chenxi, Tang, Xiangru, Zhao, Yue, Cohan, Arman, Zhang, Xiangliang, Chen, Xiuying, “DyFlow: Dynamic Workflow Framework for Agentic Reasoning,” arXiv:2509.26062, 2025. DOI: [10.48550/arXiv.2509.26062](https://doi.org/10.48550/arXiv.2509.26062). （预印本，未经同行评审）

[18] Wang, Siyu, Lu, Ruotian, Yang, Zhihao, Wang, Yuchao, Zhang, Yanzhou, Xu, Lei, Xu, Qimin, Yin, Guojun, Chen, Cailian, Guan, Xinping, “AgentConductor: Topology Evolution for Multi-Agent Competition-Level Code Generation,” arXiv:2602.17100, 2026. DOI: [10.48550/arXiv.2602.17100](https://doi.org/10.48550/arXiv.2602.17100). （预印本，未经同行评审）

[19] Pan, Melissa Z., Arabzadeh, Negar, Cogo, Riccardo, Zhu, Yuxuan, Xiong, Alexander, Agrawal, Lakshya A, Mao, Huanzhi, Shen, Emma, Pallerla, Sid, Patel, Liana, Liu, Shu, Shi, Tianneng, Liu, Xiaoyuan, Davis, Jared Quincy, Lacavalla, Emmanuele, Basile, Alessandro, Yang, Shuyi, Castro, Paul, Kang, Daniel, Sen, Koushik, Song, Dawn, Gonzalez, Joseph E., Stoica, Ion, Zaharia, Matei, Ellis, Marquita, “Measuring Agents in Production,” *Proceedings of the 43rd International Conference on Machine Learning*, vol. 306, pp. 95649–95695, 2026.

[20] Shekkizhar, Sarath, Jwalapuram, Prathyusha, Ke, Zixuan, Patil, Vaidehi, Wang, Jiayu, Bansal, Srijan, Nguyen, Trong Huy, Jiang, Dingfeng, Liu, Dongqi, Zhao, Ruochen, Lu, Wei, Carenini, Giuseppe, Bansal, Mohit, Savarese, Silvio, Joty, Shafiq, “Why Do Agent Teams Fail Across Boundaries? A Failure Taxonomy for Agent-to-Agent Systems with Private States,” arXiv:2610.19342, 2026. DOI: [10.48550/arXiv.2610.19342](https://doi.org/10.48550/arXiv.2610.19342). （预印本，未经同行评审）

[21] Park, Joon Sung, O'Brien, Joseph C., Cai, Carrie J., Morris, Meredith Ringel, Liang, Percy, Bernstein, Michael S., “Generative Agents: Interactive Simulacra of Human Behavior,” *Proceedings of the 36th Annual ACM Symposium on User Interface Software and Technology*, 2023. DOI: [10.1145/3586183.3606763](https://doi.org/10.1145/3586183.3606763).

[22] Packer, Charles, Wooders, Sarah, Lin, Kevin, Fang, Vivian, Patil, Shishir G., Stoica, Ion, Gonzalez, Joseph E., “MemGPT: Towards LLMs as Operating Systems,” arXiv:2310.08560, 2023. DOI: [10.48550/arXiv.2310.08560](https://doi.org/10.48550/arXiv.2310.08560). （预印本，未经同行评审）

[23] Xu, Wujiang, Liang, Zujie, Mei, Kai, Gao, Hang, Tan, Juntao, Zhang, Yongfeng, “A-Mem: Agentic Memory for LLM Agents,” *Advances in Neural Information Processing Systems*, vol. 38, 2025. DOI: [10.52202/085713-0593](https://doi.org/10.52202/085713-0593).

[24] Zhang, Guibin, Fu, Muxin, Wan, Guancheng, Yu, Miao, Wang, Kun, Yan, Shuicheng, “G-Memory: Tracing Hierarchical Memory for Multi-Agent Systems,” arXiv:2506.07398 [cs.MA], 2025. DOI: [10.48550/arXiv.2506.07398](https://doi.org/10.48550/arXiv.2506.07398). （预印本，未经同行评审）

[25] Wu, Di, Wang, Hongwei, Yu, Wenhao, Zhang, Yuwei, Chang, Kai-Wei, Yu, Dong, “LongMemEval: Benchmarking Chat Assistants on Long-Term Interactive Memory,” *International Conference on Learning Representations*, 2025.

[26] Wil M. P. van der Aalst, “Object-Centric Process Mining: Dealing with Divergence and Convergence in Event Data,” 2019. DOI: [10.1007/978-3-030-30446-1_1](https://doi.org/10.1007/978-3-030-30446-1_1).

[27] Adams, Jan Niklas, Schuster, Daniel, Schmitz, Seth, Schuh, Günther, van der Aalst, Wil M. P., “Defining Cases and Variants for Object-Centric Event Data,” arXiv:2208.03235, 2022. DOI: [10.48550/arXiv.2208.03235](https://doi.org/10.48550/arXiv.2208.03235). （预印本，未经同行评审）

[28] Mao, Zhenyu, Keung, Jacky, Zhang, Fengji, Liu, Shuo, Wang, Yifei, Li, Jialong, “Towards Engineering Multi-Agent LLMs: A Protocol-Driven Approach,” arXiv:2510.12120 [cs.SE], 2025. DOI: [10.48550/arXiv.2510.12120](https://doi.org/10.48550/arXiv.2510.12120). （预印本，未经同行评审）

[29] Cohen, Philip R., Levesque, Hector J., “Intention is choice with commitment,” *Artificial Intelligence*, vol. 42, no. 2-3, pp. 213–261, 1990. DOI: [10.1016/0004-3702(90)90055-5](https://doi.org/10.1016/0004-3702(90)90055-5).

[30] Babli, Mohannad, Sapena, \'Oscar, Onaindia, Eva, “Plan commitment: Replanning versus plan repair,” *Engineering Applications of Artificial Intelligence*, vol. 123, pp. 106275, 2023. DOI: [10.1016/j.engappai.2023.106275](https://doi.org/10.1016/j.engappai.2023.106275).

[31] Kambhampati, Subbarao, Hendler, James A., “A validation-structure-based theory of plan modification and reuse,” *Artificial Intelligence*, vol. 55, no. 2-3, pp. 193–258, 1992. DOI: [10.1016/0004-3702(92)90056-4](https://doi.org/10.1016/0004-3702(92)90056-4).

[32] Nebel, Bernhard, Koehler, Jana, “Plan reuse versus plan generation: a theoretical and empirical analysis,” *Artificial Intelligence*, vol. 76, no. 1-2, pp. 427–454, 1995. DOI: [10.1016/0004-3702(94)00082-C](https://doi.org/10.1016/0004-3702(94)00082-C).

[33] Sandhu, R.S., Coyne, E.J., Feinstein, H.L., Youman, C.E., “Role-based access control models,” *Computer*, vol. 29, no. 2, pp. 38–47, 1996. DOI: [10.1109/2.485845](https://doi.org/10.1109/2.485845).

[34] Walsh, James P., Ungson, Gerardo Rivera, “Organizational Memory,” *The Academy of Management Review*, vol. 16, no. 1, pp. 57–91, 1991. DOI: [10.5465/amr.1991.4278992](https://doi.org/10.5465/amr.1991.4278992).

[35] Ackerman, Mark S., Halverson, Christine A., “Reexamining organizational memory,” *Communications of the ACM*, vol. 43, no. 1, pp. 58–64, 2000. DOI: [10.1145/323830.323845](https://doi.org/10.1145/323830.323845).

[36] Runeson, Per, Höst, Martin, “Guidelines for conducting and reporting case study research in software engineering,” *Empirical Software Engineering*, vol. 14, pp. 131–164, 2009. DOI: [10.1007/s10664-008-9102-8](https://doi.org/10.1007/s10664-008-9102-8).

[37] Simmhan, Yogesh L., Plale, Beth, Gannon, Dennis, “A survey of data provenance in e-science,” *ACM SIGMOD Record*, vol. 34, no. 3, pp. 31–36, 2005. DOI: [10.1145/1084805.1084812](https://doi.org/10.1145/1084805.1084812).

[38] Sadowski, Caitlin, S\"oderberg, Emma, Church, Luke, Sipko, Michal, Bacchelli, Alberto, “Modern code review: a case study at Google,” *Proceedings of the 40th International Conference on Software Engineering: Software Engineering in Practice (ICSE-SEIP)*, pp. 181–190, 2018. DOI: [10.1145/3183519.3183525](https://doi.org/10.1145/3183519.3183525).

[39] Wang, Yang, “Approval Laundering: Systematizing Approval--Execution Binding Failures in AI Coding-Agent Harnesses,” arXiv:2609.38983 [cs.CR], 2026. DOI: [10.48550/arXiv.2609.38983](https://doi.org/10.48550/arXiv.2609.38983). （预印本，未经同行评审）

[40] Moreau, Luc, “The Foundations for Provenance on the Web,” *Foundations and Trends in Web Science*, vol. 2, no. 2--3, pp. 99–241, 2010. DOI: [10.1561/1800000010](https://doi.org/10.1561/1800000010).

[41] Arora, Chetan, Vogelsang, Andreas, Sharma, Abbi, “Specifying the Delegated-Autonomy Boundary: Requirements Engineering for Agentic AI,” arXiv:2607.17225 [cs.SE], 2026. DOI: [10.48550/arXiv.2607.17225](https://doi.org/10.48550/arXiv.2607.17225). （预印本，未经同行评审）

[42] Marla, Deekshith, Bhatnagar, Kushagra, “Permission to Retry: A Study of Authorization and Recovery in AI Agent Workflows,” arXiv:2610.31916 [cs.AI], 2026. DOI: [10.48550/arXiv.2610.31916](https://doi.org/10.48550/arXiv.2610.31916). （预印本，未经同行评审）

[43] Yang, Kaiyue, Bu, Yuyan, Yi, Jingwei, Wang, Yuchi, Zhou, Biyu, Dai, Juntao, Hu, Songlin, Yang, Yaodong, “When Lower Privileges Suffice: Investigating Over-Privileged Tool Selection in LLM Agents,” arXiv:2606.20023 [cs.SE], 2026. DOI: [10.48550/arXiv.2606.20023](https://doi.org/10.48550/arXiv.2606.20023). （预印本，未经同行评审）

[44] , “,”

[45] Banks, Jaime, Bowman, Nicholas David, Saladino, Roman, “Lexical Anthropomorphization Influences on Moral Judgments of AI Bad Behavior,” arXiv:2604.25814, 2026. DOI: [10.48550/arXiv.2604.25814](https://doi.org/10.48550/arXiv.2604.25814). （预印本，未经同行评审）

[46] Zheng, Mingqian, Pei, Jiaxin, Logeswaran, Lajanugen, Lee, Moontae, Jurgens, David, “When "A Helpful Assistant" Is Not Really Helpful: Personas in System Prompts Do Not Improve Performances of Large Language Models,” *Findings of the Association for Computational Linguistics: EMNLP 2024*, pp. 15126–15154, 2024. DOI: [10.18653/v1/2024.findings-emnlp.888](https://doi.org/10.18653/v1/2024.findings-emnlp.888).

[47] Veloso, Manuela M., Carbonell, Jaime G., “Derivational Analogy in PRODIGY: Automating Case Acquisition, Storage, and Utilization,” *Machine Learning*, vol. 10, no. 3, pp. 249–278, 1993. DOI: [10.1023/A:1022686910523](https://doi.org/10.1023/A:1022686910523).

[48] van der Krogt, Roman, de Weerdt, Mathijs, “Coordination Through Plan Repair,” *Advances in Artificial Intelligence -- IBERAMIA 2005*, pp. 264–274, 2005. DOI: [10.1007/11579427_27](https://doi.org/10.1007/11579427_27).

[49] Komenda, Anton\'\in, Nov\'ak, Peter, P\vechou\vcek, Michal, “Decentralized Multi-agent Plan Repair in Dynamic Environments,” arXiv:1202.2773 [cs.AI], 2012. DOI: [10.48550/arXiv.1202.2773](https://doi.org/10.48550/arXiv.1202.2773). （预印本，未经同行评审）

[50] Qi, Yijiashun, Xu, Xiang, Li, Yuxuan, “When Not to Write Memory: Governing False Promotion from Correlated Agent Traces,” arXiv:2607.02579 [cs.SE], 2026. DOI: [10.48550/arXiv.2607.02579](https://doi.org/10.48550/arXiv.2607.02579). （预印本，未经同行评审）

[51] Joshi, Anupam, Finin, Tim, Joshi, Karuna, Kagal, Lalana, “Deontic Policies for Runtime Governance of Agentic AI Systems,” *2026 IEEE International Conference on Web Services (ICWS)*, pp. 1255–1264, 2026. DOI: [10.1109/ICWS72778.2026.00159](https://doi.org/10.1109/ICWS72778.2026.00159).

[52] Olivera, Fernando, “Memory Systems in Organizations: An Empirical Investigation of Mechanisms for Knowledge Collection, Storage and Access,” *Journal of Management Studies*, vol. 37, no. 6, pp. 811–832, 2000. DOI: [10.1111/1467-6486.00205](https://doi.org/10.1111/1467-6486.00205).

[53] Argote, Linda, Ingram, Paul, “Knowledge Transfer: A Basis for Competitive Advantage in Firms,” *Organizational Behavior and Human Decision Processes*, vol. 82, no. 1, pp. 150–169, 2000. DOI: [10.1006/obhd.2000.2893](https://doi.org/10.1006/obhd.2000.2893).

[54] Kirchdorfer, Lukas, Rebmann, Adrian, Warmuth, Christian, Kampik, Timotheus, Heilker, Theiss, Berg, Gregor, “Organizational Memory for Agentic Business Process Execution,” arXiv:2607.03228 [cs.AI], 2026. DOI: [10.48550/arXiv.2607.03228](https://doi.org/10.48550/arXiv.2607.03228). （预印本，未经同行评审）

[55] Zeng, Aohan, Liu, Mingdao, Lu, Rui, Wang, Bowen, Liu, Xiao, Dong, Yuxiao, Tang, Jie, “AgentTuning: Enabling Generalized Agent Abilities for LLMs,” *Findings of the Association for Computational Linguistics: ACL 2024*, pp. 3053–3077, 2024. DOI: [10.18653/v1/2024.findings-acl.181](https://doi.org/10.18653/v1/2024.findings-acl.181).

[56] Wallace, Eric, Xiao, Kai, Leike, Reimar, Weng, Lilian, Heidecke, Johannes, Beutel, Alex, “The Instruction Hierarchy: Training LLMs to Prioritize Privileged Instructions,” arXiv:2404.13208, 2024. DOI: [10.48550/arXiv.2404.13208](https://doi.org/10.48550/arXiv.2404.13208). （预印本，未经同行评审）

[57] Yuan, Xiaofei, Zhang, Yan, Qiao, Shaobo, He, Huangleshuai, Ni, Leyan, Ju, Mingchen, Yang, Lujia, Xu, Sijia, Tang, Yifu, Yang, Zhengyi, “Replan, Repair, or Edit? A Unified Empirical Evaluation of Travel Agents for Itinerary Revision under Resource Disruptions,” arXiv:2609.19654 [cs.AI], 2026. DOI: [10.48550/arXiv.2609.19654](https://doi.org/10.48550/arXiv.2609.19654). （预印本，未经同行评审）

[58] Muto, Kenichiro, Suzaki, Kuniyasu, “Attestable Build Chain: Enabling Trust in Reproducible Builds,” *2026 56th Annual IEEE International Conference on Dependable Systems and Networks Workshops (DSN-W)*, pp. 196–203, 2026. DOI: [10.1109/DSN-W70714.2026.00051](https://doi.org/10.1109/DSN-W70714.2026.00051).

[59] Castillo, Fernando, Brito, Eduardo, Pullonen-Raudvere, Pille, Werner, Sebastian, Tai, Stefan, “An Evidence-driven Protocol for Trustworthy CI Pipelines,” arXiv:2605.21089 [cs.CR], 2026. DOI: [10.48550/arXiv.2605.21089](https://doi.org/10.48550/arXiv.2605.21089). （预印本，未经同行评审）

## 证据附录

本附录的编号沿用五篇工作稿的原始编号，便于与本地档案对照；它们不是外部文献编号。
消息只证明当时可见的表述，Task 事件证明对应的状态动作，源码静态阅读只证明指定位置的实现差异。
以下时间均为协调世界时（UTC）。未重新执行历史测试。

### §1.1 的证据

| 编号 | 用途 | 原生 ID / 类型 | UTC 时间 | 原文 |
|---|---|---|---|---|
| A01 | 2026-07-23历史身份契约 | 文档/源码 | 不适用 | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/sources/agent-supervisor-executor-identity-contract.md) |
| A14 | 2026-06-28并发设计初稿：原始约束与预期机制；设计意图，非部署效果 | Git `28bb445e8dd5aa9535bf8b4826019f7fa0322f9d`；该文献第 1–8、11–12 节 | 首次提交 2026-06-28T07:29:47Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/002-p1-design-origin/sources/agent-concurrent-execution-first.md) |

### §3.2 的证据

| 编号 | 用途 | 原生 ID / 类型 | UTC 时间 | 原文 |
|---|---|---|---|---|
| A00 | 010检索范围、案例筛选、排除与原始材料导航 | 文档/源码 | 不适用 | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/findings.md) |
| B00 | 010检索、纳排、C07更正与证据范围 | 文档/派生索引 | 不适用 | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/findings.md) |
| X | bug与未定根因不承担设计负效应 | [007排除台账](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-identity-cases-007.md) | 不适用 | [原文](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-identity-cases-007.md) |

### §4.1 的证据

| 编号 | 用途 | 原生 ID / 类型 | UTC 时间 | 原文 |
|---|---|---|---|---|
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
| A05.1 | Task动作：agent_started | `01KZ8WKNMFVY3YXBYYZ5R7CJC7` | 2026-08-05T11:58:13.645778Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/pm_task_action_logs-selected.json) |
| A05.2 | Task动作：blocked | `01KZ8XR3C69NRCX6KDFWCQJ0WX` | 2026-08-05T12:18:07.366266Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/pm_task_action_logs-selected.json) |
| A05.3 | Task动作：unblocked | `01KZ966KGSZCPSZF17W4AZBMQ4` | 2026-08-05T14:45:51.255316Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/pm_task_action_logs-selected.json) |
| A05.4 | Task动作：reset | `01KZABTH2T53KGBM29XXJZTX0Z` | 2026-08-06T01:43:21.433874Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/pm_task_action_logs-selected.json) |
| A05.5 | Task动作：agent_started | `01KZABTY199NCZQG05NWKXC3NX` | 2026-08-06T01:43:34.694818Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/pm_task_action_logs-selected.json) |
| A05.6 | Task动作：blocked | `01KZABVT5A8ZQK8JZE7ZPEE524` | 2026-08-06T01:44:03.497908Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/pm_task_action_logs-selected.json) |
| A05.7 | Task动作：unblocked | `01KZABZ4G4PRHBPYRZBSQXB5SR` | 2026-08-06T01:45:52.384647Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/pm_task_action_logs-selected.json) |
| A05.8 | Task动作：blocked | `01KZAD3AABJJ32SB0CSHQSZJ6Y` | 2026-08-06T02:05:37.994988Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/pm_task_action_logs-selected.json) |
| A05.9 | Task动作：unblocked | `01KZAM0BDXPP3YVCZP2CN3SEHT` | 2026-08-06T04:06:20.857687Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/pm_task_action_logs-selected.json) |
| A04d | I04工具语义与状态/生命周期事后解释 | `01KZACFHV3BFM5CP04CR07Q79W` | 2026-08-06T01:54:50.339993Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZACFHV3BFM5CP04CR07Q79W.json) |
| A16 | 自动派发改为Supervisor显式fork：提示与实际入口 | Git `b27f59650ad3061d67da0bec9893feb258405a19`及父版本 | 2026-07-26 14:09:54 UTC | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/009-p1-causal-mechanisms/sources/dispatch/changes.diff) |
| A17 | reset自动派发说明纠错；不证明当次部署 | Git `00bddae72e94ee5419339b0b95f4fb439af0a927`及父版本 | 2026-08-06 03:24:22 UTC | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/009-p1-causal-mechanisms/sources/reset/changes.diff) |
| A21 | 多时点人工纠偏、9月责任断裂与原生Task事件；分离故障和责任响应 | 149条用户角色词命中中的定向扩展；原生消息、动作与领域事件 | 2026-07至09 | [原文](../p1-logical-agent-identity/longitudinal-analysis.md) |
| A10a | I05明确自有交付责任 | `01KZDJ41E4XMR6RSRDW2342DCE` | 2026-08-07T07:31:07.844252Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZDJ41E4XMR6RSRDW2342DCE.json) |
| A10b | I05缺生产证据而拒绝自己的候选 | `01KZDKN7K7XRNZ0DPE7G9V0GQA` | 2026-08-07T07:57:59.783637Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZDKN7K7XRNZ0DPE7G9V0GQA.json) |
| A10c | I05补证或回撤计划 | `01KZDKS2M645VTZJ9GR8SW0MDJ` | 2026-08-07T08:00:05.766163Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZDKS2M645VTZJ9GR8SW0MDJ.json) |
| A15 | 身份提示、执行提示与审查回写的历史修订 | Git `3dcd57bf35fc6ed0572df93c4e8616b043f1e591`及父版本 | 2026-07-23 01:38:22 UTC | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/009-p1-causal-mechanisms/sources/identity/changes.diff) |

### §4.2 的证据

| 编号 | 用途 | 原生 ID / 类型 | UTC 时间 | 原文 |
|---|---|---|---|---|
| B03.1 | 旧006 C07-E01原文；sender和顺序按010核对 | `01KV71B56MYTKFX58W48VARSTA` | 2026-06-16T01:39:41.396427Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E01.json) |
| B03.2 | 旧006 C07-E02原文；sender和顺序按010核对 | `01KV71F5Q1DGQ5MP1Q29XMQ2H4` | 2026-06-16T01:41:52.993389Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E02.json) |
| B03.3 | 旧006 C07-E03原文；sender和顺序按010核对 | `01KV71PF2XKD4YY5ERFD4ACJNV` | 2026-06-16T01:45:51.965601Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E03.json) |
| B03x | integration明确开发期职责、移交ship | `01KV71C7NM79RDSTBBVEM5K6BY` | 2026-06-16T01:40:16.692144Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KV71C7NM79RDSTBBVEM5K6BY.json) |
| B03.4 | 旧006 C07-E04原文；sender和顺序按010核对 | `01KV71VRQBZC147BD2RTTQ0KYE` | 2026-06-16T01:48:45.675353Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E04.json) |
| B03.5 | 旧006 C07-E05原文；sender和顺序按010核对 | `01KV71XZ0F9CXTWEMS6YEAP4DK` | 2026-06-16T01:49:57.64719Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E05.json) |
| B03.6 | 旧006 C07-E06原文；sender和顺序按010核对 | `01KV724CC96MF8A8SKPZACE06C` | 2026-06-16T01:53:27.945879Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E06.json) |
| B03.7 | 旧006 C07-E07原文；sender和顺序按010核对 | `01KV72HHZQN6EXSGA9DCWZ7ZS7` | 2026-06-16T02:00:39.671089Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E07.json) |
| B03.8 | 旧006 C07-E08原文；sender和顺序按010核对 | `01KV72MY58X8TTJ7PCJ3P7DXA9` | 2026-06-16T02:02:30.440646Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E08.json) |
| B03.9 | 旧006 C07-E09原文；sender和顺序按010核对 | `01KV730J1MXKES7PXRRD3WJXNJ` | 2026-06-16T02:08:51.252117Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E09.json) |
| B03.10 | 旧006 C07-E10原文；sender和顺序按010核对 | `01KV736EDS5M1P398Y28DJXKPF` | 2026-06-16T02:12:04.153627Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E10.json) |
| B05a | Graph G7 Supervisor局部通过 | `01M1KYSW1EP98VCC0DFPJMXQB1` | 2026-09-03T15:39:36.366098Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S04-02.json) |
| B05b | Graph G7 Tester点击取证拒绝 | `01M1KYWDAFAM06Z867W7QFMVVX` | 2026-09-03T15:40:59.599882Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S04-03.json) |
| B05c | Graph G8同项目通过 | `01M1KZNHMCRFAE8VWDHE36XFHT` | 2026-09-03T15:54:43.212456Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S05-02.json) |
| B05d | Graph G8跨项目拒绝 | `01M1KZSCCMZHXY1PQXAM1FNNRS` | 2026-09-03T15:56:48.916638Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S05-04.json) |
| B05e | Graph G9 scope通过 | `01M1M0YT5A3RYX7GE2NE3XEXSB` | 2026-09-03T16:17:15.434128Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S05-07.json) |
| B05f | Graph G9 Tester最终通过 | `01M1M1CV7QBV5NABRJ817XFCWH` | 2026-09-03T16:24:55.287485Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S05-09.json) |

### §4.3 的证据

| 编号 | 用途 | 原生 ID / 类型 | UTC 时间 | 原文 |
|---|---|---|---|---|
| N01-E01 | 名称与详情导航偏好 | [消息01KV5HZX…](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E01.json) | 不适用 | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E01.json) |

### §4.4 的证据

| 编号 | 用途 | 原生 ID / 类型 | UTC 时间 | 原文 |
|---|---|---|---|---|
| 008-GIT | 祖先、父提交、文件、同树及失败读取的命令与退出码 | [source-checks.json](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/logs/source-checks.json) | 不适用 | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/evolution-008/logs/source-checks.json) |

### §4.6 的证据

| 编号 | 用途 | 原生 ID / 类型 | UTC 时间 | 原文 |
|---|---|---|---|---|
| B01a | 第一次启动要求 | `01M14JPXVK1Z8CKDYMTD3TSPAX` | 2026-08-28T16:19:40.531485Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01M14JPXVK1Z8CKDYMTD3TSPAX.json) |
| B01b | 再次启动要求 | `01M14K5QQTGJF62VANSQC5WW38` | 2026-08-28T16:27:45.786608Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01M14K5QQTGJF62VANSQC5WW38.json) |
| B01c | PD报告assignee拒绝并待确定负责人 | `01M14K6T665DY5ZSR50T9ACMW4` | 2026-08-28T16:28:21.062508Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01M14K6T665DY5ZSR50T9ACMW4.json) |
| B01d | 用户要求自行分配 | `01M14KC5XZJD4PGDV4614XEWSV` | 2026-08-28T16:31:16.927437Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01M14KC5XZJD4PGDV4614XEWSV.json) |
| B01e | PD报告分配与启动 | `01M14KDD3C9JDTE6PYV3Y01MB1` | 2026-08-28T16:31:57.036572Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01M14KDD3C9JDTE6PYV3Y01MB1.json) |
| B01f | 原生首任务ready系统消息 | `01M14KD520A5X362XVHJ64EKC7` | 2026-08-28T16:31:48.800276Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01M14KD520A5X362XVHJ64EKC7.json) |
| B02 | 五节点、四个不同负责人的初始分配，仅读初始generation | `generation-709baba8` | 2026-08-28T16:31:48.222683Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/pm_plan_generations-plan-2a8bc24c.json) |
| B07a | Task task-045a426e创建合同：给出GO/NO-GO报告 | `generation-57b10f96` | 2026-09-01T13:31:02.01844Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/C05.json) |
| B07b | 报告验收通过而产品NO-GO | `01M1EPVGFF85ZSCH2KNSDSKGHX` | 2026-09-01T14:44:29.295627Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S06-02.json) |
| B07c | 持久NO-GO报告及已测实现标识；未重跑命令 | 文档/派生索引 | 不适用 | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/sources/spark-release-README.md) |
| B08.1a | R01 Task结论 | `01M0QM59RA181QMKVGFFYGHDR9` | 2026-08-23T15:34:52.426323Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S01-05.json) |
| B08.1b | R01 Plan同步 | `01M0QM5VPGR8P4F4377DGRB2PN` | 2026-08-23T15:35:10.800032Z | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S01-06.json) |
| B08 | 四组配对与会话归属派生索引 | 文档/派生索引 | 不适用 | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/derived/relay-pairs.json) |

### §6.2 的证据

| 编号 | 用途 | 原生 ID / 类型 | UTC 时间 | 原文 |
|---|---|---|---|---|
| A05 | 定向查询空结果，不作不存在历史执行的证明 | 文档/源码 | 不适用 | [原文](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/worker_control_events-selected.json) |
