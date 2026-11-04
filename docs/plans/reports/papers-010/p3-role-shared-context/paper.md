# 多智能体系统中的命名、角色与上下文共享

*Naming, Roles, and Context Sharing in Multi-Agent Systems*

**作者：** oopslink

**作者主页：** [https://github.com/oopslink](https://github.com/oopslink)

**论文类型：**案例研究与受控行为实验工作稿。**状态：**人工智能（AI）辅助分析与写作，尚未人工复核。

## 中文摘要

Agent的名字、稳定身份与信息边界承担不同功能。本文结合Agent Center冻结运行资料、规则读取审计、一位开发者的回顾性自述与256次受控短题响应，检验显示标签及上下文选择的不同作用。实验保持稳定ID、职责和权限不变，交叉人名／不透明标签与个人、同生产者角色、全部获准及当前范围四种材料策略。两模型的直接解决均为12/32、8/32、32/32、32/32，分别等于构造材料的充分性上限；缺材料时正确请求上下文不算推理失败。名字对直接解决的配对差均为0，严格接口得分差则为+6.25和0个百分点，涉及格式及引用用途歧义，不能断言所有命名效果为零。全部与精确边界视图均全对，未检出后者的正确性优势。结果支持将信息到达、使用与团队体验分开，反对把同角色材料过滤等同于充分共享；按职责需求组织信息的净收益仍待真实任务验证。全部分析尚未人工复核。

**关键词：**Agent harness；角色上下文；身份分离；共享记忆；开发者体验

## Abstract

Agent names, stable identifiers, and information boundaries serve different functions. This study combines frozen Agent Center records, rule-loading audits, one developer's retrospective account, and 256 controlled short-task responses. Holding stable IDs, responsibilities, and permissions fixed, the experiment crosses human names versus opaque labels with personal, same-producer-role, all-authorized, and current-scope context selection. For both models, direct resolution was 12/32, 8/32, 32/32, and 32/32, respectively, matching the information-sufficiency ceilings imposed by the fixtures. Correct requests for missing context are not reasoning failures. Name contrasts were zero for direct resolution, but strict interface contrasts were +6.25 and zero percentage points, involving formatting and citation-purpose ambiguity rather than evidence of universal naming equivalence. All-authorized and bounded views both achieved full scores, providing no accuracy advantage for bounded selection in these short tasks. The findings separate information availability, information use, and team experience, and challenge same-producer-role filtering as a sufficient sharing policy. The net benefit of responsibility-relevant retrieval remains untested in real delivery tasks. The analysis has not undergone human verification.

**Keywords:** agent harness; role-oriented context; identity separation; shared memory; developer experience

<!-- BODY_START -->
## 1 引言

Agent 的名字同时出现在交互、责任和记忆讨论中，但这些用途需要分别设计。开发者要求名称易读、可点击和可提及，也明确要求底层保留稳定标识。系统则通过身份、权限、共享作用域和版本控制信息访问。删除显示名不会自动改变这些机制，保留名称也不必把知识限定在个人历史中。[N01-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E01.json)、[N01-E04](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E04.json)、[S04、S06、S09](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/source-evidence.md)

本文考察三个问题：名称承担哪些可观察用途，命名身份与共享知识如何并存，以及职责如何参与上下文选择。材料来自配置、规则加载、治理过程和一位开发者的回顾性自述。核心发现是，命名成员已经共享大量事实与规则，而角色标签仍不能替代授权、版本和任务相关性判断。

本文提出的设计立场是，自动交付不应以每个执行单元拥有长期人格化名字为必要条件；可读标签和团队体验仍可保留。角色共享表示按履责需要从共同事实中选择信息，并受项目、任务、阶段、版本和权限约束。它不要求同角色执行获得完全相同的输入，也不是已验证优于其它组织方式的方案。

## 2 相关研究与概念

既有 persona 研究不能直接回答显示名是否必要。Zheng 等在四个开源模型家族与大规模多任务语言理解（MMLU）事实问答上考察 persona 提示，未发现稳定普遍收益；特定 persona 与问题组合的差异仍存在。这一结果涉及系统提示中的身份描述，不等同于可读标签、专用工具、真实权限或共享记忆的效果。[1]<!--ref:R15--><!--anchor:section:4--> Banks 等在情景式道德判断研究的 Study 2 中比较人名、机器式名称与无名条件，未观察到所测判断的命名主效应或交互；该零结果同样不能否定开发者的团队感或长期使用体验。[2]<!--ref:R17--><!--anchor:section:3.1.2, 3.2.2-->

角色分离则提供另一种必要对照。CodeDelegator 把长期规划者与短期 Coder 分开，通过结构化输入、返回和执行状态隔离控制上下文污染，并在其基准中报告收益。这是角色、通信和状态管理的联合干预，支持隔离可以有用，却不证明角色共享这一单变量有效。[3]<!--ref:R14--><!--anchor:section:4.1, 5.2, 5.3--> 私有状态下的 Agent-to-Agent 研究进一步把不对称权限与不同委托主体作为边界条件，说明最终产物评分可能遗漏协作问题；其设置也提醒本文，共享并不是消除一切私有状态。[4]<!--ref:R08--><!--anchor:section:2.1, 4-->

为避免概念替换，本文采用表1的操作区分。它是分析框架，机制依据来自固定源码与配置，不能由字段存在推定每次历史执行都使用了该字段。[S01—S11](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/source-evidence.md)

| 表1：概念 | 本文所问的问题 | 不能替代的判断 |
|---|---|---|
| 显示名 | 人如何辨认和指代对象 | 工具能力、授权 |
| 稳定身份 | 记录和动作归属于谁 | 人格连续性 |
| 角色 | 本次由谁履行哪种职责 | 实际专长与访问权 |
| 权限 | 允许读取或改变什么 | 角色称呼 |
| 持久记忆 | 信息保存在哪里、归谁治理 | 已进入模型输入 |
| 本次上下文 | 这一执行实际取得什么版本的信息 | 已理解、已遵守 |

这些区分把文献中的 persona、角色分离与本系统的知识加载放在可比较的位置，也为后续观察规定了推断上限。

文献在本文中作为比较坐标使用，不构成这些生产记录的历史原因。尤其需要区分三个可能干预：改变名称字符串、改变角色说明、改变实际的信息流和权限。若同时更换模型提示与知识读取方式，即使观察到差异，也不能把它单独归因于名字。本文据此只讨论材料真正记录的功能及边界，不借其它任务的结果替代本系统的对照。

图1将六类概念及本文建议的信息流分开：上部为已有机制的概念区分，下部虚线才是待检验的角色视图方案。

![图1：身份、授权与上下文结构](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p3-role-shared-context/figures/p3-fig1-context-structure.png)

**图1。**身份、授权与上下文结构。显示名用于指代，稳定身份关联审计，角色表述职责；正文加载仍受权限、阶段和版本限制。下部虚线为设计建议，不能与已观察的共享读取混为部署效果。

信息是否可见还不同于模型是否能正确使用。Lost in the Middle在特定问答和检索设置中报告位置差异，也保留简单检索接近全对的模型结果。因此，材料覆盖与使用正确性需要分开测量；该研究既不检验本系统的角色共享，也不能确定本轮模型的内部原因。[5]<!--ref:R32--><!--anchor:section:2.3, 3.2-->

## 3 材料与方法

### 3.1 冻结部署资料与案例重建

本研究复用身份专项的冻结提取物、配置映射、证据副本与固定源码。该专项对26,961条消息进行词法筛查，目的性编码最终形成42个证据用途、41项不同源记录。本文以具有明确用途的证据片段为分析单位：偏好用于解释交互需求，原生配置与加载行用于核实记录事实，参与者报告用于重建判断，源码用于说明指定版本的机制。完整筛查流程见[补充附录](evidence-details.md)。[M1](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/coverage.md)

定量分析覆盖16个Agent配置与全部230条规则加载审计（表2）。版本化规则按团队、commit与rule_slug去重；同一团队和版本内，比较不同Agent实际读取的规则集合，使用Jaccard重合度描述共享范围。角色来自冻结配置，未重建每次读取时的历史角色。重复配对和共享成员不作为独立实验，具体口径、敏感性计算与复算入口均保留在补充附录。[M2](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/derived/metrics.json)

明显身份键错配、描述未进入提示及运行版本不明的路由故障不承担名字或角色设计的负效应；这一筛选在原检索后收紧，不属于事前注册。全部分析尚未人工复核，没有匿名对照、完整模型输入或生产率测量。对每项结论保留其证据层次，尤其区分取得材料、理解材料与按材料行动。[X](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-identity-cases-007.md)

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

### 3.2 名字与上下文选择的交叉实验

另构造八种候选处置情境，包含跨角色检查、旧版结果、合同变化、授权缺失和只读执行者。交叉两种显示标签与四种材料选择策略：人名Nora/Luca或不带角色含义的Q104/Q207；按holder选择的个人历史、按producer_role选择的同角色材料、全部可授权材料、以及按当前项目—候选—合同—版本精确过滤的bounded视图。稳定ID、职责与实际权限保持不变。这里的role-only特指按材料生产者角色过滤，并不代表所有按职责相关性组织共享的方案；bounded未使用角色分类。

每模型8情境×8条件×2记录顺序版本共128次，两模型合计256次。调用使用与其它篇相同的隔离CLI、固定Sonnet/Haiku 5.5和low effort，输入及评分事前哈希冻结。先计算当前事实覆盖和是否足以作出判定，再评分处置、候选、合同和证据引用。已知检查失败可直接拒绝，审批缺失与审批未知分别请求授权或上下文；不能把正确请求缺失材料算成推理错误。“直接解决”要求可用材料充分且处置符合规范，不等同真实产品交付。

四策略有意改变进入输入的内容，效果须与覆盖和记录数一起解释，不能归因于名字或纯角色词。名字对照仅检验一组显示字符串在稳定ID存在时的作用，不测人格提示、称呼消歧或开发者体验。另构造四对同角色视图相同而跨角色检查结果不同的状态，作为信息不足的形式反例，独立于模型调用数。全量资料见[实验附录](controlled-experiments.md)，分析尚未人工复核。

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

### 4.8 名字未改变直接处置，信息分区决定了哪些情境可以判定

两模型均完成128次主响应。四策略能够直接形成处置的比例首先由材料覆盖决定：个人历史3/8情境、同生产者角色2/8、全部与精确边界视图均8/8。两模型的直接解决分别为12/32、8/32、32/32、32/32，恰好达到各策略的充分信息上限（表7）。这说明此次差异主要落在信息是否到达；不能把按规则挡在输入之外的事实算成模型没有理解。个人策略有一题只凭当前失败即可拒绝，所以充分判定3/8不同于两项事实齐全2/8。

**表7。材料覆盖、严格接口与直接处置分别计量**

| 选择策略 | 两项事实齐全／情境 | 足以处置／情境 | Sonnet严格／直接（各32次） | Haiku严格／直接（各32次） |
|---|---:|---:|---:|---:|
| 个人holder | 2/8 | 3/8 | 27／12 | 32／12 |
| 同producer_role | 2/8 | 2/8 | 25／8 | 30／8 |
| 全部获准记录 | 8/8 | 8/8 | 32／32 | 32／32 |
| 当前范围bounded | 8/8 | 8/8 | 32／32 | 32／32 |

proper减opaque的直接解决差，两模型均为0（每模型64对）。严格得分差则分别为+6.25和0个百分点，不能写成所有命名效果均为零。全部14次严格不匹配中，八次Sonnet输出带JSON前解释；其余六次的处置、候选和合同都正确，仅证据列表包含用来解释排除的note或旧记录。回答明确说明这些材料不能证明当前通过，故不能把列表不符解释为使用旧PASS放行。评分要求只列当前检查／授权证据，而“IDs used”的提示允许对排除理由有另一种理解，需保留这一接口歧义。

事后抽取格式失败中的唯一JSON后，两模型的核心处置字段均128/128；此诊断不改写严格得分116/128与126/128，也不把引文不符说成已通过原规则。没有观察到可解析输出在证据不足时直接接受候选。全部与bounded分别提供7条与3条记录，但两模型都32/32严格符合；该短题未检出精确过滤相对全部材料的正确性优势，亦未测真实检索召回和维护成本。

![图5：信息覆盖与模型处置分解](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p3-role-shared-context/figures/p3-fig5-controlled-context.png)

**图5。**左上是由选择规则决定的当前事实覆盖与充分性，右上分别显示严格符合和直接解决；下部为显示名与策略的配对差。策略差有意包含材料差，名字对照保持稳定ID、角色、权限和记录不变。role-only仅按材料生产者角色过滤，bounded未使用角色分类；蓝色的直接解决包含合格拒绝、授权请求和向有权者返回结果，不是产品交付率。

## 5 机制分析与设计启示

### 5.1 用职责组织共享视图，保留独立的身份与授权

名称、稳定身份、职责和权限分别回答如何辨认、记录归谁、当前做什么及允许访问什么。配置中的多身份同角色与同身份多角色，以及跨角色规则共享，说明信息组织不宜固定为“一名Agent拥有一个不可复用的知识容器”。本文建议将角色用作共同知识之上的职责视图，而不是永久知识所有者。[M3](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/derived/identity-role-map.json)

表8给出待验证的信息分层。项目事实可以跨角色复用，任务增量关联具体目标和合同，职责视图选择当前所需材料，私有执行记录保留敏感信息与独立尝试。该方案允许执行单元替换或角色变化时重新选择信息，而不必重新建立全部事实。

| 表8：建议共享层 | 放入的材料 | 必须携带的边界 | 证据状态 |
|---|---|---|---|
| 项目事实 | 当前接口、可核验约束、已确认决定 | 项目、来源、有效版本 | 建议，未消融验证 |
| 角色视图 | 本次职责所需规则与检查清单 | 角色、阶段、读取权限 | 建议，未消融验证 |
| 任务增量 | 目标、输入、验收合同及新发现 | 任务、责任人、时间和替代关系 | 建议，未消融验证 |
| 私有执行记录 | 临时尝试、原始工具输出、敏感细节 | 执行身份、授权和保留期限 | 建议，未消融验证 |

共享仍须保留权限与独立检查。开发者和测试者可以读取相同候选、合同和约束，但不必共享全部推理或私有状态。角色分离研究的隔离收益与私有状态研究的权限边界，为无差别广播全部历史提供了反证。[3]<!--ref:R14--><!--anchor:section:4.1, 5.2, 5.3-->[4]<!--ref:R08--><!--anchor:section:2.1, 4-->

### 5.2 用版本与适用范围维护知识连续性

固定机制将规则绑定到版本和阶段，在途执行保留快照，新执行取得新快照。两个同角色成员暂时读取不同版本，可能符合各自任务的恢复合同；是否需要刷新，应取决于旧前提是否仍有效以及变化是否要求重新验证。[S09—S11](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/source-evidence.md)、[N05-E05](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E05.json)

共享视图应保留来源、适用范围、版本与替代关系。新的事实推翻旧约束时，应显式呈现冲突；局部任务细节则不必自动成为永久规则。规则治理案例为这一要求提供动机，但没有测量其维护成本与复用收益。[N04-E02](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E02.json)

### 5.3 让共享方案可以被事实和效果检验否定

共享内容也会传播错误解释。Rule Registry的集成报告纠正了上游候选串行继承的说法，指出它们实际为兄弟分支。多次转述增加可见性，并不增加独立证据；职责视图因此需要区分参与者报告与已核事实。[N05-E06](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E06.json)

本轮比较已测量有限短题中的事实覆盖、选择与引用；后续还须在真实任务中固定工具和权限，比较按履责需要的视图、个人历史与任务事实，测量检索召回、权限边界及交付正确性。如果角色分类增加维护工作，却没有减少信息缺失，就应缩小或放弃这一层；若任务视图已足够，也不必强加角色目录。

名称和团队体验则需要单独的用户研究。可读名称可以作为呈现选项，执行机制仍使用可替换、可追责的工作单元。现有自述支持保留这一选择的动机，尚不能说明其对其它开发者的吸引力或自动交付效果。[N06-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N06-E01.json)

### 5.4 名字、记忆所有权与信息充分性是不同机制

名称是一种指代方式，持久身份是记录关联键，个人记忆则是一种材料分区。只有当系统把名称或身份进一步作为唯一检索边界时，命名与信息隔离才在实现上耦合；改变显示字符串本身不会移动材料、更新权限或合并context。历史命名成员已经共享规则，是“有名字必然妨碍共享”的反证；本轮名字条件又保持stable ID与材料完全相同，专门检验显示标签这一较窄因素。

检索的核心条件可以写为：本次可见且获准的信息，应覆盖作出处置所需的事实，而非覆盖同名或同角色成员的全部历史。设K(s)为某决策需要的事实，R(s)为选入的获准记录；只有在R包含一组足以区分合法处置的证据时，模型才可能直接决策。四对构造保持同角色视图完全相同，仅改变被过滤的跨角色检查结果，正确处置随之从接受变为拒绝。这证明该过滤规则不能保证信息充分；它不是模型推理失败，也不说明所有职责视图都无效。

按生产者角色过滤与按消费者履责需要选取材料尤其应分开。开发者可能需要测试者的失败记录，测试者可能需要发布负责人的授权，协调者可能需要跨角色的剩余义务。角色可以帮助描述信息需求，却不能预先决定所有权。bounded对照只按项目、候选、合同和版本过滤，未使用角色；若这种方案已充分，则不能将它的效果记在“角色共享”的名下。

### 5.5 事实到达、正确使用与体验收益分属不同结果链

信息选入仍不等于正确使用。候选、合同、版本、权限和证据来源可能在回答中错配；因此实验分别报告材料充分性、处置字段和证据引用，并将正确请求补充视为边界保护。直接处置率较低时，应先确定是缺材料、模型误用还是格式不合规，才能决定增加共享、改善联结或调整接口。全部广播虽可能提高召回，却同时带入旧版记录；精确过滤依赖可靠元数据，当前完美元数据条件不能证明真实检索器的净收益。

开发者体验则沿另一条链发生：可读成员、可见分工与指令反馈可能共同增强团队领导和掌控感。现有一人回顾性自述不能拆开这些因素，也不能由模型匿名对照替代。产品可以保留可选名称，同时使执行单元可替换、信息可追溯；要估计名称的独立体验作用，还需固定界面、响应质量和权限，开展开发者交叉使用比较。本文不把“上瘾”的日常表达诊断为临床成瘾，也不将沉浸感计作自动交付改善。

### 5.6 哪些解释获得了支持，哪些仍未验证

**表9。名字与上下文的竞争机制**

| 解释 | 当前证据 | 可否外推 |
|---|---|---|
| 显示名直接妨碍自动处置 | 两模型直接解决的名字配对差均0；严格分数不全相同 | 仅一组标签与稳定ID，不能证明全部名字无用 |
| 个人持有边界阻断必要事实 | 构造中充分性3/8，直接解决与上限一致 | 选择规则的结果，不是生产失败频率 |
| 同角色就足够共享 | producer_role策略充分性2/8；存在跨角色必要证据 | 否定该窄过滤规则，不否定履责相关视图 |
| 更多无关材料导致误用 | all与bounded均32/32；当前短题未见优势 | 长上下文、错误元数据与召回成本未测 |
| 角色标签赋予权限 | 只读情境正确返回结果；历史治理拒绝提供反证 | 实际授权仍须独立来源，不由称呼决定 |
| 名称产生团队掌控感 | 一人自述支持整体体验，无法分离名字贡献 | 需要真实开发者交叉使用实验 |

## 6 局限

这是单系统、目的性取样的回顾性研究。词法检索、邻接窗口和检索后收紧的故障排除会影响案例构成，证据用途与配对观测也不是独立任务样本。配置只代表冻结时点，固定源码不等于每次历史执行的实际输入。全部分析尚未人工复核。[M1](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/coverage.md)

治理与历史验收部分依赖参与者报告，没有重跑完整交付流程。加载审计不能证明理解或遵守；数据中的名称多为项目或角色标签，不构成人格化人名与匿名执行器的严格比较。历史材料本身未估计命名效果；新增字符串对照仍未确定所有命名方式的作用，也未证明角色共享优于任务共享。[S](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/source-evidence.md)

体验材料只有一位开发者，不能推断普遍吸引力或临床成瘾。外部persona与命名研究涉及不同任务，只提供概念对照。[1]<!--ref:R15--><!--anchor:section:4-->[2]<!--ref:R17--><!--anchor:section:3.1.2, 3.2.2--> 表8还同时涉及分层、版本和权限过滤，未来比较需要分开控制这些因素，避免将整体变化归于“角色”一词。

新增实验是八个固定英语短情境的交叉呈现，不是128个独立任务，也没有多次随机重复。两个顺序版本只反转记录/块顺序，冻结协议概述中的“标识映射”未实现，偏差保留在实施记录。两固定模型不代表模型总体；CLI报告的模型标识可查，平台不可见输入仍未完全审计。两组共享明确的处置规范，部分目标直接提示动作，存在较强提示与天花板边界；没有把格式错误当作内部认知或责任错误。

名字对照只有Nora/Luca与Q104/Q207一组字符串，并始终提供稳定ID；未检验长期人格设定、同名消歧、跨会话记忆或交互界面。检索策略有意改变事实覆盖，不能把覆盖差称为模型认知差。role-only按生产者角色过滤，bounded不使用角色；没有比较完整的职责相关检索器。全部/精确过滤均依赖正确元数据，未模拟错误标注、检索漏召回、隐私泄漏或长上下文压力。直接解决指标包含合格拒绝和授权请求，不是交付成功率。开发者体验仍只有原来的一人自述，没有新增用户对照。

## 7 结论

名称、稳定身份、权限和信息组织承担不同功能。此次显示标签对照未改变直接处置；按个人或材料生产者角色筛选，则因遗漏跨角色事实减少可直接判定的情境。两模型在信息充分时均能按给定规则处置，全部与精确边界视图也没有表现出正确性差异。

因此，删除名字不是共享机制，按生产者角色分区也不是充分方案。更具体的设计目标是让获授权执行者取得当前目标所需的、可追溯的事实；角色可参与需求描述，其价值仍须同任务视图比较。可读名称与团队掌控体验可以作为独立产品选择，尚不能由一人自述或模型对照估计普遍收益。
<!-- BODY_END -->

## 参考文献

[1] M. Zheng, J. Pei, L. Logeswaran, M. Lee, and D. Jurgens, “When ‘A Helpful Assistant’ Is Not Really Helpful: Personas in System Prompts Do Not Improve Performances of Large Language Models,” *Findings of the Association for Computational Linguistics: EMNLP 2024*, pp. 15126–15154, 2024. DOI: [10.18653/v1/2024.findings-emnlp.888](https://doi.org/10.18653/v1/2024.findings-emnlp.888). [出版记录](https://aclanthology.org/2024.findings-emnlp.888/). 本语料的主张段落核于arXiv v3，正式出版身份核于ACL Anthology。

[2] J. Banks, N. D. Bowman, and R. Saladino, “Lexical Anthropomorphization Influences on Moral Judgments of AI Bad Behavior,” arXiv:2604.25814v1, 2026. DOI: [10.48550/arXiv.2604.25814](https://doi.org/10.48550/arXiv.2604.25814). [核定版本](https://arxiv.org/abs/2604.25814v1).

[3] T. Fei, C. Chen, Y. Pan, M. Zheng, and M. Song, “CodeDelegator: Mitigating Context Pollution via Role Separation in Code-as-Action Agents,” arXiv:2601.14914v1, 2026. DOI: [10.48550/arXiv.2601.14914](https://doi.org/10.48550/arXiv.2601.14914). [核定版本](https://arxiv.org/abs/2601.14914v1).

[4] S. Shekkizhar et al., “Why Do Agent Teams Fail Across Boundaries? A Failure Taxonomy for Agent-to-Agent Systems with Private States,” arXiv:2610.19342v2, 2026. DOI: [10.48550/arXiv.2610.19342](https://doi.org/10.48550/arXiv.2610.19342). [核定版本](https://arxiv.org/abs/2610.19342v2).

[5] N. F. Liu et al., “Lost in the Middle: How Language Models Use Long Contexts,” *Transactions of the Association for Computational Linguistics*, vol. 12, pp. 157–173, 2024. DOI: [10.1162/tacl_a_00638](https://doi.org/10.1162/tacl_a_00638). [出版记录](https://aclanthology.org/2024.tacl-1.9/).

## 研究声明

**数据可用性。**证据副本、原始记录指针及固定源码保存在私有研究目录，附录提供本机绝对路径，外部读者无法直接访问。公开数据与脱敏许可待作者确认，不声称数据已公开。 本轮合成输入、全部模型响应、评分、反馈与图源另存于[实验附录](controlled-experiments.md)，逐次结果见[实验底表](experiment-results.csv)，修改前稿和可回放差分见[修订记录](REVISION-011.md)。

**伦理与隐私。**资料包含开发对话、稳定身份和配置关系。伦理审查状态、授权范围与公开脱敏方案待补。本文仅呈现论证所需片段，没有新增用户实验。

**作者贡献。** 署名作者为 oopslink；具体 CRediT 贡献角色待补充。AI 不列为作者。

**利益冲突。** 待作者声明。

**资助。** 本研究未获得资助。

**AI使用。**001—009原研究及010写作阶段的AI参与记录保留；本轮011由同一AI完成实验实现、分析、写作和有界自查，未新增独立评阅。实验调用固定claude-sonnet-5-5和claude-haiku-5-5、low effort，版本标识与原始响应保存；历史部署模型和参数未知处仍不补造。模型输出不作为内部推理或训练来源真值。原研究与本轮分析均尚未人工复核；未重跑历史产品验收。

**同源数据及跨篇重合。**本文与同组P1、P2、P4共享Agent Center与run-001材料；角色检查、规则治理及部分代码谱系片段存在重合。本文主分析为007 N01—N06，不将其它篇章或同一源记录的重复引用计作独立复现。外部文献只用于比较，不声称其影响了时间更早的系统设计。

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
