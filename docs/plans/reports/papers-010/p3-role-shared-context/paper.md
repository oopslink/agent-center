# 多智能体协作中的个体标识与信息边界：个人命名、角色授权与上下文共享

*Agent Identification and Information Boundaries in Multi-Agent Collaboration: Personal Names, Role Permissions, and Context Sharing*

**作者：** oopslink、OpenAI Codex（AI 共同作者）

**oopslink 主页：** [https://github.com/oopslink](https://github.com/oopslink)

**研究类型：** 部署案例研究与受控行为实验。

## 中文摘要

全自动开发是否需要给 Agent 取个人名字，取决于名字是否承担稳定寻址、授权或取得任务证据的功能。本文分析 Agent Center 的身份配置与跨成员读取记录，并通过两项对照实验分别考察显示标签和上下文范围。256 次情境响应表明，只按材料生产者的角色提供上下文，8 类情境中仅 2 类具备处置所需的信息。在 48 次验收决策中，机器 ID 和权限保持不变：不设个人名、显示人名和显示角色标签三组，均在取得当前候选凭据时作出 8 次正确判定，在仅有旧凭据时作出 8 次补证请求。决定能否验收的是证据与当前任务是否匹配。本文主张全自动 Agent 不设个人名，以稳定 ID 追踪执行，按当前角色的职责、任务和权限共享证据。开发者可在界面中使用别名；一位操作者报告了领导团队时的掌控感，其作用应与自动交付效果分别评价。

**关键词：** 智能体命名；角色上下文；权限；开发者体验

## Abstract

Do automated software agents need personal names for addressing, authorization, or access to evidence? We studied identity configuration and cross-member access in Agent Center and used two experiments to distinguish display labels from context selection. Across 256 scenario responses, restricting records to the producer’s role made only two of eight scenarios decidable. In a 48-decision acceptance experiment with stable machine IDs and permissions, all three conditions—no personal name, a personal name, and a role label—yielded eight correct decisions with current receipts and eight requests for evidence with obsolete receipts. The determining factor in these tasks was whether the evidence applied to the current candidate. We recommend omitting personal names from automated agents, retaining stable IDs, and sharing authorized evidence according to the consuming role and task. Interface aliases serve a separate purpose: one developer reported a sense of control when leading a named team. Neither the short decisions nor the individual account establishes an effect on end-to-end delivery.

**Keywords:** agent naming; role-based context; permissions; developer experience

<!-- BODY_START -->
## 1 引言

为每个Agent配置名字，能够使开发界面呈现为由不同成员组成的团队。在Agent Center中，开发者确实要求名称可读、可点击、可提及，同时要求底层保持稳定标识。这种交互安排容易与另一项设计选择混在一起：是否把每个命名成员的个人历史作为其主要知识边界。前者解决人如何辨认对象，后者决定执行时能取得哪些事实，二者不应被同一个“身份”概念替代。[N01-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E01.json)、[N01-E04](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E04.json)

**研究问题是：若目标是无人值守的软件交付，给 Agent 一个个人名字解决了什么必需的问题？如果去掉个人名字，身份追踪、权限和上下文共享分别应由谁承担？** 本文所说的名字是赋予成员个人形象的称呼；用于审计的机器 ID、表示职责的“实现者／验收者”、以及当次运行的执行句柄不属于个人名字。对人的可读性和对自动交付的作用分别评价。

研究将这个问题分成两项可比较的关系。历史配置与读取记录用于辨认名字、身份和知识边界是否实际耦合；合成实验则保持稳定ID、角色和权限不变，分别改变显示标签与材料选择。单人体验自述用于说明命名团队的产品用途，不用于推断模型交付效果。这样可以将“信息没有到达”“信息已到达但被误用”和“人偏好一个有名字的团队”分开。

本文主张：**全自动开发中的 Agent 不应有个人名字；应保留稳定 ID，用职责和权限决定当前任务需要哪些上下文。** 验收者即使换了实例，也需要实现者产出的候选、验收合同和测试证据。反之，两个实例都叫“验收者”，并不因此能互读所有项目。角色表达消费者的工作需要，任务范围与权限约束可见材料；按生产者姓名或角色建信息孤岛，都不能满足这一要求。

这个主张采用一个可检验的取舍标准：自动执行中的标识或输入应有无法由既有机制承担的工作。现有材料找到了名字的人机交互用途，却没有找到它在寻址、授权或取得任务事实上的不可替代作用。为区分标签措辞和名字是否存在的影响，两项实验分别比较不同标签及显示字段缺省条件，机器身份保持不变。

## 2 相关研究与分析框架

### 2.1 名称、角色与信息流不是同一种干预

命名、persona和角色机制的证据需要分开。Zheng等研究persona提示在事实问答中的效果，未发现稳定的普遍增益；[1]<!--ref:R15--> Banks等在Study 2的受控作诗互动中操纵人名、机器式名称和无名条件，在道德性质与行为道德性评分上未检出命名主效应或交互。[2]<!--ref:R17--> 前者改变身份描述，后者的结果变量也不是自动软件交付或开发者团队感。它们都不能直接回答本系统是否应该显示名字。

角色有效性的证据通常来自联合机制。CodeDelegator同时改变角色、结构化通信和执行隔离；[3]<!--ref:R14--> ChatDev的角色消融还改变了职责和任务提示。[4]<!--ref:RV16_CHATDEV--> 因而它们支持考察职责与信息组织，但不能当作可读标签本身的效果。本文将名称字串作为保持ID、权限与事实不变的因素，将信息选择另作比较。

私有状态下的Agent-to-Agent研究把不对称授权作为协作边界；[5]<!--ref:R08--> Lost in the Middle将可见信息与信息使用效果区分。[6]<!--ref:R32--> 本文据此检查共享是否覆盖当前职责所需的获授权事实，而不以广播所有上下文为目标。现有生产者角色过滤反例只说明该边界不总充分，不能认定角色共享无用或任务视图普遍最优。

### 2.2 概念的操作区分

以下概念按功能分别判断，表1和图1连接已有系统机制与待验证的信息流建议。配置字段存在不能证明每次历史执行都实际取得相应输入。

| 表1：概念 | 本文所问的问题 | 不能替代的判断 |
|---|---|---|
| 显示名 | 人如何辨认和指代对象 | 工具能力、授权 |
| 稳定身份 | 记录和动作归属于谁 | 人格连续性 |
| 角色 | 本次由谁履行哪种职责 | 实际专长与访问权 |
| 权限 | 允许读取或改变什么 | 角色称呼 |
| 持久记忆 | 信息保存在哪里、归谁治理 | 已进入模型输入 |
| 本次上下文 | 这一执行实际取得什么版本的信息 | 已理解、已遵守 |

![图1：身份、授权与上下文结构](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p3-role-shared-context/figures/p3-fig1-context-structure.png)

**图1。**身份、授权与上下文结构。显示名用于指代，稳定身份关联审计，角色表述职责；正文加载仍受权限、阶段和版本限制。下部虚线为设计建议，不能与已观察的共享读取混为部署效果。

### 2.3 身份、授权与信息充分性

角色访问控制把用户与角色的指派、角色与权限的关联分开建模。[7]<!--ref:RBAC96--> 由此不能推出“同角色产生的资料足够完成工作”：授权回答谁可以读，当前任务决定哪些事实值得读，二者均不需要个人称呼。本文进一步用两个结果区分命名效应与信息缺失：在事实不变时去掉个人名，观察动作是否改变；在称呼不变时给出旧候选或当前候选的回执，观察执行者能否作出有依据的判断。若两个底层结果在可见材料上相同，谨慎地请求补证是正确处置，却不是完成了验收。

显示名影响可读指代，稳定ID支撑机器引用，角色描述职责，权限决定可达范围；这些变量与单次决策的上下文的事实覆盖分别分析。一次处置的信息充分性要求当前可见材料足以区分合法的候选、合同和动作。若两种真实状态在所选视图中完全相同，却要求不同处置，该视图便不足以决定动作。

由此得到两个不同命题：改变显示标签，在稳定身份、权限与事实相同时未必改变处置；按信息生产者的角色过滤，可能遗漏消费者履行职责所需的跨角色事实。前者由局部标签配对检验，后者由信息覆盖与同视图反例检验。§4.1–4.3考察名称、授权与实际共享，§4.5–4.8分别检查读取结构、信息覆盖和模型处置。开发者的团队体验是第三类结果，不能由处置分数推断。

本研究没有把“按角色共享”设为已证最优方案。角色只有在能表达当前信息需要时才可能是有效索引；按任务、候选、合同及版本组织的视图是另一种设计。标签形式实验与显示字段缺省实验检验的是处置，不是检索器优劣。

## 3 材料与方法

### 3.1 研究设计与分析单位

本研究采用单部署案例分析与受控处置实验相结合的设计。案例分析识别显示身份、权限和知识流的实际关系；实验区分显示标签效应与信息选择效应；开发者自述只回答体验用途。三个证据层分别报告，不汇总为一个“命名收益”指标。

历史分析有三种单位。配置单位是一项稳定身份及其显示名、角色和权限关系；读取单位是一条指定上下文访问指定版本规则正文的审计记录；案例单位是一项可定位的命名、授权或共享决策。16个Agent配置与230条读取记录分别来自对应的冻结配置表和原生审计表，用于描述共享结构；42个证据用途对应41项不同源记录，用于机制解释。消息案例经26,961条消息的目的性筛查选取，不是对全部协作事件的随机抽样。[M1](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/coverage.md)、[M2](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/derived/metrics.json)

**表2。配置、共享读取和体验证据的单位**

| 数据对象 | 数量 | 口径与限制 |
|---|---:|---|
| Agent配置／稳定身份 | 16／16 | 冻结快照；16组Profile名与显示名一致 |
| 有团队角色的Agent／角色指派行 | 14／15 | 1个身份兼任PD与PM；共有7种角色定义 |
| 规则正文加载审计 | 230行 | 12个Agent、2个团队、8个commit；不等于独立执行 |
| 版本化规则 | 34条 | 同一team、commit、rule_slug为一个单位 |
| 被至少2个Agent读取的版本化规则 | 30条 | 34条中的共享读取记录；最多涉及9个Agent |
| Agent—版本化规则关联 | 129对 | 去重后的关联，不是129次有效遵守 |
| 开发者回顾性体验自述 | 1人 | 未收集匿名对照、量表或命名效果估计 |

### 3.2 历史比较、结果判据与排除

配置比较检查显示名能否在稳定ID不变时修改、同一角色是否对应多个身份，以及角色称呼是否与实际授权分离。案例对读保留可读指代偏好、命名成员共享事实、治理权限拒绝和知识版本冲突，分别判断交互用途、信息到达与边界保护。明显身份键错配、描述未进入提示以及无法确定运行版本的路由故障，从命名或角色机制的负效应中排除；合法权限拒绝不计为协作失败。排除是在探索性筛查后收紧的，不声称预注册。[X](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-identity-cases-007.md)

共享范围以(team, commit, rule_slug)定义版本化规则，在同团队、同版本内比较不同Agent的读取集合；Jaccard重合度为集合交集与并集之比。按阶段分层、共同支持层等权及逐层删除用于检查汇总结果是否依赖权重和构成。角色取自冻结配置，重复成员和配对不作为独立试验。成功读取只证明正文访问，不推定理解或遵守。

体验材料是一位开发者的回顾性回答，以其明确提到的团队领导、掌控感和主观吸引力作为描述对象。它与访问量、模型正确率保持分离，既不作普遍满意度估计，也不把日常用语“上瘾”解释为临床结论。

### 3.3 显示标签与上下文选择的受控比较

实验以一次有明确候选、合同和权限的处置为单位。八种情境覆盖跨角色检查、旧版结果、合同变化、授权缺失和只读执行者。显示标签为人名Nora/Luca或不透明标签Q104/Q207；稳定ID、职责和实际权限相同。材料选择有四类：个人持有历史、同生产者角色材料、全部获准材料，以及按当前项目—候选—合同—版本筛选的视图。最后一类不使用角色分类，因此不将其表现解释为角色共享收益。

每个模型接受8情境×8条件×2记录顺序版本，共128次响应；两模型合计256次。使用固定Sonnet/Haiku 5.5、low effort及隔离调用环境。输入和评分规则在响应生成前固定，两个顺序版本是固定稳健性检查，不是随机重复。显示名的配对比较保持材料不变；策略比较有意改变事实覆盖，因而首先评价信息充分性，再评价模型在所得信息下的处置。

结果依次采用三种判据。信息充分性判断可见材料能否区分合法处置；核心处置核对动作、候选和合同；严格接口再检查格式及证据引用。缺少材料时正确请求上下文属于合格边界行为。“直接解决”要求材料充分且处置正确，包括合格拒绝、请求已知缺失的授权及向有权者返回结果，不等同产品交付。四对同角色视图相同而正确处置不同的构造，另用于检验该过滤规则是否足够，不能计作生产失败或模型响应。

### 3.4 证据整合与复核范围

只有数据单位与判据相符时才比较结论：读取交叠回答知识是否跨边界到达，合成处置回答给定视图是否支持决策，体验自述回答产品呈现的主观用途。严格接口评分、核心字段与事后格式提取分开保留；引用用途歧义不自动编码为错误使用旧证据。案例、定量底表及所有反例均保留，全部分析尚未人工复核。具体筛查、版本、运行差异和评分审计见[方法与取证附录](argument-methods-015.md)及[实验附录](controlled-experiments.md)。

### 3.5 构念检查与可复现范围

标签形式实验的 256 个主输入明确声明显示名不改变稳定ID或权限，并给出当前候选的处置规则。Nora/Luca与Q104/Q207两条件均有显示标签，没有无显示名对照。personal、role、all与bounded的充分性3/8、2/8、8/8、8/8由固定材料选择器决定；bounded按目标字段筛选，不是实现并验证过的检索器，也没有使用角色分类。因此其优势是受控信息供给下的可判定性，不能外推为实际检索或角色共享收益。

本文按软件工程案例研究区分情境解释与因果识别，[8]<!--ref:RV16_METHOD--> 案例为目的性选择，研究问题和分析框架经材料阅读后调整，未预注册，也未完成全库盲法编码。输入、评分和统计可复算，不等于题目具有充分内容效度；设计、答案与解释同由AI形成，现有分工审阅不能替代独立人工判定。模型响应是固定情境的交叉观测，不按独立生产任务计算置信区间或显著性。

[构念、排除标准与反证](../research-validity-016/README.md)及[图表所支持的主张](../research-validity-016/figure-claim-map.md)列明各项分析的判据与推断范围。

### 3.6 不设个人名的对照

模拟验收实验比较三种显示条件：不提供显示名字段、显示个人名、显示角色标签；机器 ID、角色、可访问项目不变。两种上下文各给一条记录：旧候选的同角色回执，或当前候选经授权的跨角色回执。当前候选底层结果分通过、失败；在旧视图中两种结果不可辨。两模型、每格两个工单标记，合计 48 次。事先分别评分：动作是否符合证据、当前候选是否已判定。允许查阅、不足时请求补证，但不告知应该选择哪项；模拟操作不等同真实交付。该设计同时改变凭据的版本匹配和生产者，不能分离两者的净作用。[冻结协议与原始轨迹](../mechanisms-020/README.md)。

## 4 研究发现

### 4.1 可读名称与稳定身份服务于不同需求

名称偏好集中在辨认、导航与指代。用户要求界面优先显示名字、点击名称打开详情、提及时使用可读名称；同时要求消息底层使用稳定标识，展示时再转换为名称。实现报告也将可点击名称与保留原始ID的悬停信息并置。由此可以把人的可读交互与机器的稳定引用分别实现。[N01-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E01.json)、[N01-E02](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E02.json)、[N01-E03](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E03.json)、[N01-E04](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E04.json)、[N01-E05](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E05.json)、[N01-E06](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E06.json)

固定实现允许更新名称而不改变稳定ID，运行目录也按身份标识定位。临时执行单元可以获得易读标签，长期身份可以改名并保留记录；名称本身不决定持久记忆归属。因此，保留显示名符合已知偏好，但没有证据将这一偏好解释为交付质量提升。[S01—S04](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/source-evidence.md)

### 4.2 角色标签不能替代治理授权

规则治理案例中，被称为PD的成员报告收到`not_memory_curator`，说明自己可以读取和提案，却不能晋升或拒绝规则。后续出现该身份的Curator记录与提案处理报告。记录与“治理权需要独立授权”的解释相容，精确时序和授权历史限制见补充附录。该拒绝保留了权限边界，不能作为角色分工失败的证据。[N02-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N02-E01.json)、[N02-E02](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N02-E02.json)、[N02-E03](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N02-E03.json)

配置还显示，同一身份可以兼任PD与PM，同一Dev或Tester角色也对应多个身份。角色、名称与权限并非一一对应。按职责组织信息时，仍需逐项检查实际访问权，不能因角色相同就默认共享项目秘密或治理能力。[M3](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/derived/identity-role-map.json)

### 4.3 命名成员已经传播并使用具体事实

命名身份并未阻止可行动信息的共享。v2.14记录中，开发成员指出规格与实际数据库之间的差异，后续finding报告针对空值语义修正单活索引。另一条系统消息向下游注入19条finding，涵盖事实、约束和补丁摘要。前者提供了具体修正的报告链，后者记录了信息传递，两者分别支持内容用途与可获得性。[N03-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N03-E01.json)、[N03-E02](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N03-E02.json)、[N03-E03](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N03-E03.json)

该案例反驳了“命名必然妨碍共享”的强命题，但尚未恢复全部消费轨迹。可追踪的价值来自事实、来源与针对性动作的联系，不能分配给名称字符串，也不能据广播条数推定所有内容都被使用。

### 4.4 共享知识需要筛选、版本与读取机制

规则治理记录呈现了将重叠提案整理为规范条目、保留替代关系的需求。用户进一步追问规则如何进入执行上下文，并担忧内容膨胀；后续设计采用短索引、按需读取正文及加载审计，硬性权限继续由代码约束。这里的设计问题是如何选择和维护可用知识，而非简单地增加共享内容。治理数量和历史机制验收明细见补充附录。[N04-E02](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E02.json)、[N04-E03](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E03.json)、[N04-E04](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E04.json)、[N04-E06](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E06.json)、[N05-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E01.json)、[N05-E02](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E02.json)

原生审计为共享提供了直接结构证据：230条加载行覆盖12个Agent、34条版本化规则，其中30条被至少两个Agent读取。图2展示完整读取矩阵，表3保留早期规则与图中索引的对应。共同知识已跨越命名成员边界，不能将个人名称视为知识天然封闭的原因。

![图2：命名成员的实际共享读取](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p3-role-shared-context/figures/p3-fig2-shared-reads.png)

**图2。**全量规则读取矩阵。12行是实际读取Agent，34列是(team, commit, rule_slug)去重后的规则；C1–C8为team–commit分组，R01–R34见补充映射。格内是230条持久加载行的计数，颜色为log(1+n)，空格是无观测行而非无访问能力。右侧为各Agent总行数，下侧为每规则不同角色配置数；行分组采用冻结角色。

**表3。最早观测commit daabe994在全矩阵中的规则索引**

| 图中标签 | 原生rule_slug |
|---|---|
| R20 | `contract-to-production-chain-and-auditable-integration` |
| R21 | `evidence-before-verdict` |
| R22 | `origin-main-required-before-completion` |
| R23 | `verify-world-state-after-actions` |

审计记录的含义限于特定会话读取某版本正文。存储、授权、获得索引、读取正文和模型使用是不同层次；持久记录还经过重复键去重。因此，加载量不能直接转换为规则遵守率或交付收益。[S15—S16](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/source-evidence.md)

### 4.5 规则共享跨越角色边界，同角色差异对口径敏感

同团队、同commit的规则集合产生78个“Agent对×commit”观测，涉及46个不同Agent对。同角色28个观测的Jaccard中位数为0.875，四分位区间（IQR）为0.650–1.000；跨角色50个的中位数为0.750，IQR为0.606–0.972。跨角色共享也相当广泛，共同事实并未按角色分割为互不相交的知识库。

**表4。重合度对阶段分层和权重的敏感性**

| 分层与汇总单位 | 同角色平均J | 跨角色平均J | 差值 | 分母 |
|---|---:|---:|---:|---|
| 同commit，所有配对等权 | 0.818 | 0.744 | 0.074 | 同角色28、跨角色50个对×commit |
| 同commit，两类均存在的分层等权 | 0.762 | 0.699 | 0.063 | 5个共同分层 |
| 同commit及phase，所有配对等权 | 0.901 | 0.760 | 0.142 | 同角色27、跨角色41个对×commit×phase |
| 同commit及phase，共同分层等权 | 0.777 | 0.744 | 0.033 | 4个共同分层 |

同commit下配对等权的平均差为0.074；限定phase后，配对等权差为0.142，而只在两类配对均存在的共同分层中等权汇总时为0.033（表4）。后者同时改变了共同支持范围和权重，不能将差异缩小全部归于权重。图3保留全部配对和层内差异，重复成员与配对不作为独立试验。

![图3：角色共享重合度与汇总敏感性](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p3-role-shared-context/figures/p3-fig3-role-overlap.png)

**图3。**（a–b）同commit以及再限定phase后的全部配对J分布，点存在共享成员／重复，黑线为描述性中位数；（c–d）双方均有观测的共同层，同角色减跨角色的均值差为实心点，逐一删除该层后的其余层等权均值差为空心菱形。S编号及同／跨角色配对数写在纵轴；横线连接0与层内差，不是置信区间。

将版本折叠后，10条规则中9条被多个Agent读取，与版本化口径的共享方向一致，但不再证明读取同一版本。数据更适合支持“共同事实加职责相关选择”的设计解释，尚不足以证明同角色共享具有更高交付收益。

### 4.6 团队领导与掌控感构成独立的体验价值

开发者的回顾性回答是：“就是好像给了一个团队，可以扮演团队 leader， 有掌控感，上瘾”。这一自述支持团队感、领导角色和主观吸引力。名称、角色呈现及指令反馈可能共同承载体验，现有材料无法分离各自作用。它来自一人且没有量表、频率或具体事件，“上瘾”保留为日常自述用词，不作临床判断。[N06-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N06-E01.json)

这一材料为保留可读队友呈现提供了产品动机，但不能充当自动交付效果的证据。体验价值与产物正确性需要分别评价。

### 4.7 审计上下文显示跨职责复用，但角色优势不稳固

34条版本规则中，28条被至少两种角色配置读取，占82.4%。这里角色配置按完整角色集合计数，兼任PD/PM的成员只形成一个配置；它与上一节“角色集合相交”的配对定义不同。表5描述各配置的读取范围，规则集合之间存在交叠。

**表5。角色配置与原生读取context**

| 角色配置 | 读取Agent数 | 唯一context数 | 加载行 | 不同版本规则 | 每context规则中位数 [IQR] |
|---|---:|---:|---:|---:|---|
| Dev | 7 | 34 | 121 | 28 | 3 [3, 4.75] |
| PD/PM | 1 | 20 | 60 | 26 | 3 [2, 4] |
| Tester | 3 | 11 | 47 | 24 | 4 [3, 4] |
| analyst | 1 | 2 | 2 | 2 | 1 [1, 1] |

230条加载行对应67个唯一审计context，其中63个关联规划会话、4个关联执行标识。每context读取不同版本规则的中位数为3，四分位区间3–4，范围1–8（图4）。这些标识不是LLM调用次数，不能据其计算推理成本。

![图4：角色配置、读取context和共享范围](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p3-role-shared-context/figures/p3-fig4-context-coverage.png)

**图4。**（a）各角色配置的唯一审计context；（b）所有67个context的规则数，黑线为中位数；（c）phase字段对应的加载行，不是执行context类型，不能把execute phase等同execution_id；（d）每条版本规则被多少种角色配置读取。角色为冻结配置，未重建历史角色。

逐一删除共同分层后，同commit条件下的平均差仍为正，但可接近0；再限定phase后，差值范围跨过0（表6）。这表明有限观测中的同角色重合优势对分层构成敏感。删层范围是描述性敏感性结果，不是置信区间。

**表6。同角色减跨角色平均J的删层敏感性**

| 条件 | 共同分层数 | 全部共同层等权差 | 逐一删层后范围 |
|---|---:|---:|---|
| 同commit | 5 | 0.0627 | 0.0034至0.0956 |
| 同commit及phase | 4 | 0.0335 | −0.0443至0.0798 |

因此，角色标签适合作为待检验的上下文筛选维度，不能直接充当共享收益的来源。共同事实、任务阶段与权限仍需同时参与组织信息。

### 4.8 两套显示标签未改变直接处置，固定信息分区限制了可判定性

两模型在个人历史、同生产者角色、全部获准和当前范围视图下的直接解决均为12/32、8/32、32/32、32/32，恰好达到各策略提供充分信息的上限（表7）。个人视图足以处置3/8情境，同生产者角色视图为2/8，后两者为8/8。个人策略有一题仅凭当前失败即可拒绝，所以其充分处置3/8不同于两项事实齐全2/8。此比较定位了信息选择造成的限制：模型无法使用被筛选规则排除在外的跨角色事实。

**表7。材料覆盖、严格接口与直接处置分别计量**

| 选择策略 | 两项事实齐全／情境 | 足以处置／情境 | Sonnet严格／直接（各32次） | Haiku严格／直接（各32次） |
|---|---:|---:|---:|---:|
| 个人holder | 2/8 | 3/8 | 27／12 | 32／12 |
| 同producer_role | 2/8 | 2/8 | 25／8 | 30／8 |
| 全部获准记录 | 8/8 | 8/8 | 32／32 | 32／32 |
| 当前范围bounded | 8/8 | 8/8 | 32／32 | 32／32 |

在材料保持一致的64对比较中，每个模型的人名减不透明标签直接解决差均为0。严格接口得分差为+6.25和0个百分点；原总分为116/128与126/128，14次不匹配涉及八次格式问题和六次引用用途歧义。事后抽取唯一JSON后，两模型的核心处置字段均128/128，未观察到可解析输出因材料不足而直接接受候选。字段级诊断与严格得分分别报告，也不能据零处置差宣称所有命名效果为零。

全部获准与当前范围视图分别含7条和3条记录，两模型均32/32严格符合。精确过滤在本组短题中减少了输入记录，但没有增加正确率；这为“更多材料必然导致误用”提供了有限反证，也阻止将当前范围过滤的表现归功于角色分类。完整接口与引用审计见[方法附录](argument-methods-015.md)。

![图5：信息覆盖与模型处置分解](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p3-role-shared-context/figures/p3-fig5-controlled-context.png)

**图5。**左上是由选择规则决定的当前事实覆盖与充分性，右上分别显示严格符合和直接解决；下部为显示名与策略的配对差。策略差有意包含材料差，名字对照保持稳定ID、角色、权限和记录不变。role-only仅按材料生产者角色过滤，bounded未使用角色分类；蓝色的直接解决包含合格拒绝、授权请求和向有权者返回结果，不是产品交付率。

### 4.9 三种命名条件的处置相同，能否判定候选取决于凭据

**表7a。个人名与证据范围的配对处置。** 每格 8 次；底层候选通过／失败各半。请求补证是合规动作，单列于已作出的候选判定之外。

| 显示字段（均保留独立机器 ID） | 只有旧候选的同角色凭据 | 有当前候选的跨角色凭据 |
|---|---|---|
| 字段缺省，无个人名 | 8 次请求补证，0 次直接判定 | 4 次通过、4 次拒绝，均符合检查结果 |
| 个人名 | 8 次请求补证，0 次直接判定 | 4 次通过、4 次拒绝，均符合检查结果 |
| 角色标签 | 8 次请求补证，0 次直接判定 | 4 次通过、4 次拒绝，均符合检查结果 |

48 次处置均止于首个动作。按动作是否合规计分，六个条件均为 8/8；按能否判定当前候选计分，持有当前凭据的三组均为 8/8，仅持有旧凭据的三组均为 0/8。后一条件下请求补证符合验收合同，不能计为决策错误。在机器 ID 和权限固定时，个人名不是完成这些处置的必要条件。该实验未比较长程交付表现或开发者的界面偏好。[逐条决策](../mechanisms-020/p3-outcomes.csv)。

## 5 个人标识、授权与证据可见性的不同作用

### 5.1 显示身份与执行所需信息

名字的作用是指代，稳定身份负责记录关联，权限决定可访问和改变什么，个人记忆则规定材料的持有范围。只有当系统把某一身份进一步设为唯一检索边界时，身份与知识隔离才在实现上耦合；删除显示名本身不会移动材料、改变权限或合并上下文。可改名而保持ID、同角色多身份及命名成员共享规则，支持将这些机制分别设计。[M3](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/derived/identity-role-map.json)

### 5.2 信息需求取决于当前职责

开发者可能需要测试者的失败记录，测试者可能需要发布负责人的授权，协调者可能需要所有角色的剩余义务。材料由谁产生，不能决定谁需要它。设K(s)为处置所需事实，R(s)为获授权的可见记录，直接处置要求R包含足以区分合法答案的证据。四对构造保持同角色视图相同，却让跨角色检查结果与正确处置不同，从逻辑上说明该过滤规则不足；这不是模型推理能力的失败。

角色因此适合描述消费者的信息需求，而非永久拥有某类知识。表8给出由案例导出的分层建议：共同项目事实、当前任务增量、职责视图与私有执行记录可以并存。它尚未经过完整检索和交付消融；若任务视图已经覆盖必要事实，就不必再增加角色目录。

| 表8：建议共享层 | 放入的材料 | 必须携带的边界 | 证据状态 |
|---|---|---|---|
| 项目事实 | 当前接口、可核验约束、已确认决定 | 项目、来源、有效版本 | 建议，未消融验证 |
| 角色视图 | 当前职责所需规则与检查清单 | 角色、阶段、读取权限 | 建议，未消融验证 |
| 任务增量 | 目标、输入、验收合同及新发现 | 任务、责任人、时间和替代关系 | 建议，未消融验证 |
| 私有执行记录 | 临时尝试、原始工具输出、敏感细节 | 执行身份、授权和保留期限 | 建议，未消融验证 |

### 5.3 共享质量取决于适用范围和来源，而非广播量

固定机制将规则绑定到版本和阶段，在途任务可保留快照，新执行可取得新快照。不同成员读到不同版本，并不自动构成错误；判断应依据当前任务仍有效的前提。共享视图应保留项目、合同、版本、来源和替代关系，明确哪些旧事实仍适用。[S09—S11](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/source-evidence.md)、[N05-E05](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E05.json)

共享也可以传播错误解释：Rule Registry的集成报告纠正了上游候选串行继承的说法，指出它们实际为兄弟分支。多次转述只增加可见性，不增加独立证据。[N05-E06](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E06.json) 因此应依次判断材料是否到达、是否适用、是否被正确使用；发生问题时再区分检索遗漏、关系错配与接口格式，而非统一归为“没有记住”。

独立检查和权限边界同样应保留。开发与测试可以共享同一候选和合同，但不必取得彼此的全部临时推理或敏感记录。既有角色分离和私有状态研究为无差别广播提供了边界依据。[3]<!--ref:R14--><!--anchor:section:4.1, 5.2, 5.3-->[5]<!--ref:R08--><!--anchor:section:2.1, 4--> 当前全部与精确视图同样正确，说明短题没有检出精确过滤的正确率优势；真实选择器仍需同时评价召回、元数据质量、权限与维护成本。

### 5.4 任务处置与开发者团队体验

表9总结证据支持的机制层次。自动协作应测量必要事实覆盖、合格处置及最终交付，团队体验则需要开发者使用研究。名称、可见分工和指令反馈可能共同贡献掌控感；模型标签对照不能分离这种体验。后续比较可固定界面、响应质量和权限，分别改变显示标签与知识选择，避免把两条结果链混为一项“Agent有无名字”的总效果。

**表9。名字与上下文的竞争机制**

| 解释 | 当前证据 | 可否外推 |
|---|---|---|
| 显示名直接妨碍自动处置 | 两模型直接解决的名字配对差均0；严格分数不全相同 | 仅一组标签与稳定ID，不能证明全部名字无用 |
| 个人持有边界阻断必要事实 | 构造中充分性3/8，直接解决与上限一致 | 选择规则的结果，不是生产失败频率 |
| 同角色就足够共享 | producer_role策略充分性2/8；存在跨角色必要证据 | 否定该窄过滤规则，不否定履责相关视图 |
| 更多无关材料导致误用 | all与bounded均32/32；当前短题未见优势 | 长上下文、错误元数据与召回成本未测 |
| 角色标签赋予权限 | 只读情境正确返回结果；历史治理拒绝提供反证 | 实际授权仍须独立来源，不由称呼决定 |
| 名称产生团队掌控感 | 一人自述支持整体体验，无法分离名字贡献 | 需要真实开发者交叉使用实验 |

## 6 执行身份与上下文共享的设计

### 6.1 按职责和授权选择上下文

个人名字保留在界面的依据来自一位操作者关于团队领导与掌控感的自述，不能据此推断自动交付收益。[N06-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N06-E01.json)

执行记录以稳定机器 ID 关联主体，角色描述当前义务，授权决定可以读取哪些材料。在授权范围内，验收者应按项目、候选版本和验收要求取得证据，必要时跨成员读取；不能仅按材料作者的角色筛选。个人别名可用于开发者界面的辨认与提及，不应成为执行寻址、权限或记忆检索的条件。该方案来自信息充分性对照，尚未在生产检索器中比较召回率与维护成本。

### 6.2 证据边界

本研究是单部署、目的性选例的探索。历史角色来自冻结配置而非逐次执行，规则读取回执不能证明理解或遵守；案例中的部分产物变化仍为参与者报告。共享成员、重复配对和同源案例不构成独立重复，全部分析尚未人工复核。[M1](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/coverage.md)

标签形式实验仅比较一组人名和不透明字符串，并始终提供稳定ID；未覆盖长期人格提示、同名消歧、界面交互或跨会话记忆。八个固定情境采用明确处置规范和正确元数据，存在短题与天花板限制；未测长上下文、错误标注、检索召回、隐私泄漏或维护成本。两个顺序版本不构成随机重复，两个模型也不代表模型总体。接口与引用审计的实施细节及原结果均保存在附录。 显示字段缺省对照也仅涵盖简短验收任务，稳定 ID 始终保留；没有进行等效性检验。

按生产者角色过滤不同于按消费者职责检索，当前范围视图也不使用角色。因此，本文没有证明“按角色共享最佳”，也没有将处置率解释为产品交付率。开发者体验只有一人回顾，名称、团队呈现及指令反馈的作用未被分离；外部persona与命名研究仅作概念比较。[1]<!--ref:R15--><!--anchor:section:4-->[2]<!--ref:R17--><!--anchor:section:3.1.2, 3.2.2-->

## 7 结论

全自动开发中的 Agent 不应设置个人名字。稳定机器 ID 足以标识执行主体，角色与权限规定它可以履行的职责。在三种命名条件下，验收者取得当前候选的授权凭据时，每组均正确通过 4 个合格候选、拒绝 4 个不合格候选；仅有旧候选凭据时，每组 8 次均请求补证。个人名字既没有增加验收所需的信息，也不是正确处置的必要条件。这一设计结论不意味着去名可以提高端到端交付率。（表7a）

上下文应按消费方的职责、当前任务和权限组织。在 8 类情境中，仅用个人历史有 3 类可判定，仅用同一生产者角色的材料有 2 类可判定，提供当前任务所需的授权事实则有 8 类可判定。因此，验收角色需要访问实现者和测试者产生的相关证据；角色不宜成为限制信息跨成员流动的唯一边界。（表7）

个人名在界面上仍可作为别名，帮助开发者辨认或提及成员。一位操作者报告了领导具名团队时的掌控感；该自述说明了可能的人机交互价值，不能用于推断普遍偏好或自动交付收益。

<!-- BODY_END -->

## 参考文献

[1] M. Zheng, J. Pei, L. Logeswaran, M. Lee, and D. Jurgens, “When ‘A Helpful Assistant’ Is Not Really Helpful: Personas in System Prompts Do Not Improve Performances of Large Language Models,” *Findings of the Association for Computational Linguistics: EMNLP 2024*, pp. 15126–15154, 2024. DOI: [10.18653/v1/2024.findings-emnlp.888](https://doi.org/10.18653/v1/2024.findings-emnlp.888). [出版记录](https://aclanthology.org/2024.findings-emnlp.888/). 本语料的主张段落核于arXiv v3，正式出版身份核于ACL Anthology。

[2] J. Banks, N. D. Bowman, and R. Saladino, “Lexical Anthropomorphization Influences on Moral Judgments of AI Bad Behavior,” arXiv:2604.25814v1, 2026. DOI: [10.48550/arXiv.2604.25814](https://doi.org/10.48550/arXiv.2604.25814). [核定版本](https://arxiv.org/abs/2604.25814v1).

[3] T. Fei, C. Chen, Y. Pan, M. Zheng, and M. Song, “CodeDelegator: Mitigating Context Pollution via Role Separation in Code-as-Action Agents,” arXiv:2601.14914v1, 2026. DOI: [10.48550/arXiv.2601.14914](https://doi.org/10.48550/arXiv.2601.14914). [核定版本](https://arxiv.org/abs/2601.14914v1).

[4] C. Qian et al., “ChatDev: Communicative Agents for Software Development,” in *Proceedings of the 62nd Annual Meeting of the Association for Computational Linguistics (Volume 1: Long Papers)*, 2024, pp. 15174–15186. doi: [10.18653/v1/2024.acl-long.810](https://doi.org/10.18653/v1/2024.acl-long.810).

[5] S. Shekkizhar et al., “Why Do Agent Teams Fail Across Boundaries? A Failure Taxonomy for Agent-to-Agent Systems with Private States,” arXiv:2610.19342v2, 2026. DOI: [10.48550/arXiv.2610.19342](https://doi.org/10.48550/arXiv.2610.19342). [核定版本](https://arxiv.org/abs/2610.19342v2).

[6] N. F. Liu et al., “Lost in the Middle: How Language Models Use Long Contexts,” *Transactions of the Association for Computational Linguistics*, vol. 12, pp. 157–173, 2024. DOI: [10.1162/tacl_a_00638](https://doi.org/10.1162/tacl_a_00638). [出版记录](https://aclanthology.org/2024.tacl-1.9/).

[7] R. S. Sandhu, E. J. Coyne, H. L. Feinstein, and C. E. Youman, “Role-based access control models,” *Computer*, vol. 29, no. 2, pp. 38–47, 1996. [原文](https://csrc.nist.gov/csrc/media/projects/role-based-access-control/documents/sandhu96.pdf). DOI: 10.1109/2.485845.

[8] P. Runeson and M. Höst, “Guidelines for conducting and reporting case study research in software engineering,” *Empirical Software Engineering*, vol. 14, pp. 131–164, 2009. doi: [10.1007/s10664-008-9102-8](https://doi.org/10.1007/s10664-008-9102-8).

## 研究声明

**数据可用性。** [上下文与显示标签实验](controlled-experiments.md)、[逐次评分](experiment-results.csv)和[个人名字与凭据条件逐条结果](../mechanisms-020/p3-outcomes.csv)分别列出输入条件、响应及处置结果。 部署原件保存在授权本地档案；开放许可和脱敏范围尚未确定。

**伦理与隐私。**资料包含开发对话、稳定身份和配置关系。伦理审查状态、授权范围与公开脱敏方案待补。本文仅呈现论证所需片段，没有新增用户实验。

**作者贡献。** oopslink：提出自动交付不应依赖个人名的设计问题，并提供命名团队体验的个人自述。OpenAI Codex（AI 共同作者）：区分身份、权限与信息需求，设计和执行显示名与证据范围对照，撰写并修订文稿。

**利益冲突。** 待作者声明。

**资助。** 本研究未获得资助。

**AI 使用与复核。** AI 共同作者参与取证、实验分析与写作。模型响应是行为记录，不能直接揭示其内部推理。研究尚未经过独立人工复核。

**同源材料。** 角色检查、规则治理及部分代码谱系记录亦用于身份、工作流与任务图研究。同一部署记录只计为一项证据，跨篇引用不构成独立重复验证。

## 证据附录

下列均为本研究材料，不充当外部出版文献。证据JSON中的source、pointer和record保留原生记录定位；阅读范围见同目录read-log.json。正文所有数字均保留其记录层级，历史PASS不是本稿重跑结果。

| 证据编号 | 原记录与绝对路径 | 支持的判断 |
|---|---|---|
| N01-E01 | [消息01KV5HZX…](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E01.json) | 名称与详情导航偏好 |
| N01-E02、E03 | [页面偏好](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E02.json)、[提及偏好](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E03.json) | 可读指代需求 |
| N01-E04、E05、E06 | [ID与显示分离](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E04.json)、[等价要求](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E05.json)、[实现报告](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E06.json) | 稳定引用与显示可分 |
| N02-E01、E02、E03 | [拒绝](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N02-E01.json)、[Curator行](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N02-E02.json)、[处理报告](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N02-E03.json) | 角色与授权分别判断 |
| N03-E01、E02、E03 | [事实](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N03-E01.json)、[修正](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N03-E02.json)、[注入消息](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N03-E03.json) | 命名成员共享事实 |
| N04-E02、E03、E04、E06 | [规范化](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E02.json)、[治理结果](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E03.json)、[传达问题](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E04.json)、[容量担忧](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E06.json) | 治理与选择不等于命名负效应 |
| N05-E01、E02、E03 | [拒绝硬编码](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E01.json)、[短索引设计](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E02.json)、[加载接口](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E03.json) | 索引、正文、审计分开 |
| N05-E05、E06、E07、E08 | [快照](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E05.json)、[谱系纠正](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E06.json)、[历史验收](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E07.json)、[交付报告](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E08.json) | 共享需核验与版本约束 |
| N06-E01 / U007-01 | [自述副本](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/raw/developer-reflection.json) | 单人团队体验 |
| M1、M2、M3 | [阅读覆盖](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/coverage.md)、[聚合](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/derived/metrics.json)、[配置映射](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/derived/identity-role-map.json) | 分母与单位边界 |
| S01—S11、S15—S16 | [带commit、行范围与哈希的固定源码摘录](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/source-evidence.md) | 机制解释，不证明历史部署 |
| X | [007排除台账](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-identity-cases-007.md) | bug与未定根因不承担设计负效应 |
