# Agent 不需要人格化名字，但需要按角色共享上下文

**论文类型：**案例驱动的研究与立场工作稿；作者、单位及通讯信息待补。**状态：**人工智能（AI）辅助分析与写作，尚未人工复核。

## 中文摘要

自动交付与团队体验对 Agent 身份提出不同要求。本研究以 Agent Center 的冻结运行资料、命名偏好、身份配置、共享规则机制及一位开发者的回顾性自述为材料，区分显示名、稳定身份、角色、权限、持久记忆与本次上下文。007阶段对26961条消息进行词法筛查，后续目的性编码包含42个证据用途、41项不同源记录；本稿重新读取所用证据，分析尚未人工复核。案例显示，用户希望名称可读且可导航，同时要求底层保留稳定引用；同一角色称呼不自动赋予规则治理权；命名成员已经共享事实与规则，230条规则加载审计对应34条版本化规则，其中30条被至少两个Agent读取；加载不能解释为有效遵守。单人自述还表明团队领导和掌控体验具有独立产品意义。本文提出，自动交付不应以长期人格化名字为必要条件；共享上下文可围绕角色履责需要组织，并受项目、任务、权限、阶段和版本约束。该方案是待验证的设计立场，现有资料没有匿名对照，也没有检验角色共享相对于其它方案的净收益。

**关键词：**Agent harness；角色上下文；身份分离；共享记忆；开发者体验

## Abstract

Automated delivery and the experience of managing an agent team impose different requirements on identity. This case-grounded working paper examines frozen Agent Center records, naming preferences, identity configurations, shared-rule mechanisms, and one developer's retrospective account. It distinguishes display names, stable identifiers, roles, authorization, persistent memory, and the context supplied to a particular execution. The earlier identity study screened 26,961 messages and purposefully coded 42 evidence uses from 41 distinct source records. The evidence used here was reread, but the analysis has not undergone human review. The developer requested readable names while retaining stable machine references. Role labels did not themselves grant rule-governance authority, and named agents already shared facts and rules. The 230 stored rule-load audits cover 34 versioned rules, 30 of which were read by at least two agents; these records do not establish understanding or compliance. The developer's account also identifies team leadership and perceived control as distinct experiential values. We propose that automated delivery should not require a persistent personified name for every execution unit. Role-oriented context views may organize shared information, subject to project, task, authorization, phase, and version boundaries. This is a design position, not a validated performance improvement: the available data contain neither an anonymous-agent control nor a comparative evaluation of role-oriented sharing.

**Keywords:** agent harness; role-oriented context; identity separation; shared memory; developer experience

<!-- BODY_START -->
## 1 引言

“Agent 不需要名字”容易把两个问题压成一个判断：系统能否完成工作，以及人是否愿意把它当作一支团队。本研究采用更窄的立场：自动交付不应要求每个执行单元拥有长期人格化名字，但可读标签与团队体验可以保留为产品价值。这不是关于名称无效的实验结论。现有资料同时出现了明确的命名偏好与消息底层使用标识符的要求，两者并不冲突。[N01-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E01.json)、[N01-E04](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E04.json)

真正需要解释的是，名称为何经常与职责、权限和记忆被一起讨论。在固定机制中，个人目录按稳定身份定位，团队记忆另有共享作用域，运行时又按阶段与版本加载规则。因此，保留或删除一个显示字符串都不足以决定知识能否被另一个执行器使用。[S04、S06、S09](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/source-evidence.md) 若把身份的可读性直接当成上下文组织原则，就会忽略这些实际边界；若把知识共享简单理解为所有人读取全部历史，又会取消必要的选择和隔离。

本文据此提出三个问题：可读名称在本案例中承担哪些可观察用途；命名身份与共享知识能否并存；如何把履责需要转化为上下文选择原则。贡献是将六类概念分开，并从具体证据导出一个受约束的角色共享方案，而非提出新算法或估计命名的效应量。角色在此表示当前职责及检查视角，既不是人格描述，也不自带权限。后文先限定相关研究能够回答的问题，再说明材料与分析方法，以案例中的偏好、授权、治理和加载证据支撑设计讨论。

这一立场也明确了标题中的“需要”所指的规范目标：承担相同职责的执行者应有获得相关共享事实的途径，并不要求所有同角色执行都得到相同输入。研究关心的是知识能否跨执行连续使用，以及这种连续性由什么规则保证。只有先把这一目标与名字的体验功能分开，才可能设计有解释力的比较。

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

## 3 材料与方法

本研究复用007身份专项的冻结 SQLite 提取物、证据副本、配置映射和固定源码。原专项对26961条消息执行三组正则，得到835条去重候选；33条用户命中逐条筛选后，23条归入偏好或设计输入。研究者目的性选择19个锚点，抽取90条去重邻接消息，最终形成42个证据用途、41项不同源记录。重复使用的消息不计为新的观测，本稿也不把候选量解释为故障量。[M1](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/coverage.md)

分析单位是一个有明确用途的证据片段：用户偏好用于解释交互目标，原生记录用于核实配置或加载事实，参与者报告用于恢复当时的判断，源码用于解释给定版本可实现的机制。对正文涉及的原记录副本进行重读，并保留读取清单。配置覆盖16个 Agent，加载聚合覆盖230行；它们分别是对象快照和事件记录，不是用户样本或独立试验。[M2](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/derived/metrics.json)

明显的身份键错配、描述未接入提示以及运行版本不明的路由故障，不承担名字或角色方案的负效应推断。此排除原则在原研究部分检索之后修订，不能称为事前注册。[X](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/agent-harness-identity-cases-007.md) 全部分析与本稿仍未人工复核；没有随机改名、匿名对照、完整模型输入或生产率测量。本文采用反证检查：凡能由权限、作用域或版本解释的观察，均不改写为名称造成的效果，并保留命名成员成功共享的证据。

解释过程先把每个片段归入偏好、配置、报告或机制，再询问它最多支持哪一步推断。例如，授权行可以确认记录存在，却不能还原每次权限检查；加载行可以确认特定正文被读取，却不能判断它是否改变了答案。同一消息在N02与N04承担授权结果和治理结果两种用途，只计一次源记录。这一规则使论证能够复用证据而不扩大样本，并把“观察到什么”与“建议如何设计”分开。[M1](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/coverage.md)

本次从冻结JSON重算配置映射和全部规则加载行，结果见表2。版本化规则按`(team_id, commit, rule_slug)`去重；同一规则的不同版本分开计数。这里的“14”是有团队角色的Agent数，配置实际有7种团队角色定义。[制图底表与原生定位](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P3/data.json) · [源文件、字段与哈希](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P3/source-provenance.json)

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

主团队贡献228行、11个Agent及6个commit；另一团队贡献2行、1个Agent及2个commit。跨团队不合并同名规则为同一版本，观察范围为2026-08-14至2026-09-21的已保存加载行。

## 4 案例与结果

### 4.1 可读名称与稳定引用可以同时满足

用户对名称的要求具有具体交互对象。6月15日要求名称可点击并打开 Agent 详情，6月29日要求页面优先显示名字，8月21日又要求提及时使用名字。这些记录支持辨认、导航与指代偏好，不支持名称提高交付质量或降低操作时间。[N01-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E01.json)、[N01-E02](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E02.json)、[N01-E03](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E03.json)

更有辨别力的证据是7月3日的要求：消息使用稳定标识符（ID），展示时再转成名称；名称式提及和 ID 式提及应具有相同功能。随后实现报告称名称可以打开活动侧栏，原 ID 留在悬停信息中。由此可见，人机交互中的可读性与机器记录的稳定性是两个可分别实现的目标。[N01-E04](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E04.json)、[N01-E05](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E05.json)、[N01-E06](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E06.json) 删除显示名将直接违背已知偏好，却不会自动改变目录、权限或记忆组织，因而不是本文建议的起点。

固定实现也支持这一拆分：Profile的名称可更新，Identity重命名不改稳定ID，运行目录则由worker和Agent ID定位。[S01—S04](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/source-evidence.md) 因此，给一个临时执行分配易读标签，并不必然赋予其长期记忆；让一个长期身份改变显示名，也不必然失去过去记录。这是可实现的分离关系，不是改名后质量保持不变的实测结论。

### 4.2 角色称呼并不授予治理权

8月13日03:09协调世界时（UTC），原记录的协调角色标签PD对应成员报告晋升规则时收到 `not_memory_curator`，并说明当前可以读取和提案，不能晋升或拒绝。快照中同一身份的 Curator 记录创建于04:10；06:42，该身份报告完成提案处理。记录时序与权限独立于称呼的解释一致，但创建时间不能替代完整授权历史。[N02-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N02-E01.json)、[N02-E02](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N02-E02.json)、[N02-E03](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N02-E03.json)

这里的拒绝具有明确保护作用：没有治理权的身份停止改变规则，而非依靠“PD”这一名称绕过边界。配置还显示16个对象的 Profile.name 与 Identity.display_name 全部对应，14个有团队角色，同一身份可以兼任 PD 与 PM，同一 Dev 或 Tester 角色也可以对应多个名字。[M3](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/derived/identity-role-map.json) 因而角色共享应围绕职责组织可见信息，授权仍须独立判断；不能把同角色当成可以读取相同项目秘密的充分条件。

### 4.3 命名成员已经共享可行动的事实

v2.14记录提供了名称与共享并存的直接例子。开发成员指出规格与实际数据库的差异，包括真实表名、SQLite 类型约束和空值索引问题。后续 finding 明确报告使用 NULL-aware 谓词修正单活索引，说明共享内容具有具体下游消费者。[N03-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N03-E01.json)、[N03-E02](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N03-E02.json) 这里可追踪的价值在于新事实和针对性修正；没有证据把这一价值分配给名称字符串。

同一源包还有向下游注入19条 finding 的系统消息，包含事实、约束和补丁摘要。[N03-E03](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N03-E03.json) 传递存在不等于所有内容均被使用，但已经足以否定“命名必然阻止共享”的强命题。反过来，这条记录也不证明共享全部历史最优：若要判断哪条信息影响了哪个动作，仍需保留内容版本和使用证据。

共享记录还保留了一个重要的分析区别：原始差异报告含有具体数据库事实，系统注入消息则是这些事实和其它约束的聚合视图。前者适合追溯某项修改的理由，后者适合证明下游有机会取得材料。只有把两类记录联系起来，才能研究信息传播；如果只计算广播条数，就会把无消费者的消息和被具体修正引用的事实混为一谈。本文没有完整消费轨迹，因此只将已经出现具体修正的链作为有限正例。[N03-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N03-E01.json)、[N03-E02](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N03-E02.json)

### 4.4 共享知识需要治理，加载证据需要分层

规则治理案例把“共同拥有知识”细化为可以检查的步骤。PD 报告20条待处理提案存在重叠，建议建立规范条目并记录替代关系；后来报告8条晋升、12条拒绝以及待处理清零。它们是参与者对治理过程与结果的叙述，研究没有独立取得完整记忆仓库正文。[N04-E02](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E02.json)、[N04-E03](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E03.json) 数量说明需要选择和整理，不能直接解释为浪费或名称造成的负担。

用户随后追问规则如何进入成员上下文，并担忧膨胀；又否决在每个任务节点硬编码 work_type 的方案。修订后的设计采用短索引和按需读取正文，记录已读规则，并将不可违反的权限继续交给代码约束。[N04-E04](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E04.json)、[N04-E06](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E06.json)、[N05-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E01.json)、[N05-E02](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E02.json) 这段取舍表明，角色相关性可以作为检索视角，但不必变成所有节点的固定分类。

机制报告还区分仅修改记忆内容与新增读取接口、加载审计。验收报告称，在100条、每条32KiB的合成正文条件下，execute 索引为25条且序列化小于16KiB；集成报告称相关成果进入主线。[N05-E03](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E03.json)、[N05-E07](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E07.json)、[N05-E08](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E08.json) 这是历史机制验收，不是本研究重跑的模型效果或生产 token 收益。

原生聚合中的230条加载记录覆盖12个命名 Agent、两个团队和8个规则 commit。固定源码将其定义为特定执行或规划会话读取某一版本正文的持久事实，重复键不重复插入。[M2](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/derived/metrics.json)、[S15—S16](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/source-evidence.md) 因此，存储存在、具有读取权、获得索引、读取正文、模型使用是五层不同证据；最后一层不能由前四层自动推出。

这些层级还解释了为何加载数不宜成为共享质量指标。一次持久记录可以对应重复读取后的去重结果，一个执行也可以读取多条规则；用行数除以任务数不会得到遵守率。要检验遵守，必须再定义规则的适用动作与可观察结果，并对具体执行作核对。[S15—S16](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/source-evidence.md)

![图2：命名成员的实际共享读取](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p3-role-shared-context/figures/p3-fig2-shared-reads.png)

**图2。**全量规则读取矩阵。12行是实际读取Agent，34列是(team, commit, rule_slug)去重后的规则；C1–C8为team–commit分组，R01–R34见补充映射。格内是230条持久加载行的计数，颜色为log(1+n)，空格是无观测行而非无访问能力。右侧为各Agent总行数，下侧为每规则不同角色配置数；行分组采用冻结角色。

**表3。最早观测commit daabe994在全矩阵中的规则索引**

| 图中标签 | 原生rule_slug |
|---|---|
| R20 | `contract-to-production-chain-and-auditable-integration` |
| R21 | `evidence-before-verdict` |
| R22 | `origin-main-required-before-completion` |
| R23 | `verify-world-state-after-actions` |

图2提供了比总加载量更直接的结构证据：多个有名字的成员确实读取了同一团队、同一commit下的同一规则。因此名称并未在这些记录中构成共享读取的必要障碍。各规则覆盖的成员不同，但缺少任务适用性与完整输入，不能把未出现的格子判为失忆或不遵守；也不能把较多加载行判为更好交付。[制图底表与原生定位](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P3/data.json) · [源文件、字段与哈希](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P3/source-provenance.json)

### 4.5 同角色与跨角色的共享重合度

为检验共享是否只发生在同角色内部，本文将230条加载行按`(team, commit, agent)`去重为规则集合R，再对同team、同commit且有读取记录的Agent对计算Jaccard重合度：`J(a,b)=交集规则数／并集规则数`。同角色定义为冻结配置中两者角色集合有交集；跨角色要求双方角色已知且无交集。这里使用的是快照角色标签，未重建每次加载时的历史角色，也不测完整上下文或规则遵守。

8个team–commit分层中，2个只有一个Agent，无法配对；其余产生78个“Agent对×commit”观测，但只涉及46个不同Agent对。同角色28个观测的J中位数为0.875 [0.650, 1.000]，跨角色50个为0.750 [0.606, 0.972]；方括号为四分位区间。配对重复和成员共享意味着78不能作为78次独立实验。

**表4。重合度对阶段分层和权重的敏感性**

| 分层与汇总单位 | 同角色平均J | 跨角色平均J | 差值 | 分母 |
|---|---:|---:|---:|---|
| 同commit，所有配对等权 | 0.818 | 0.744 | 0.074 | 同角色28、跨角色50个对×commit |
| 同commit，两类均存在的分层等权 | 0.762 | 0.699 | 0.063 | 5个共同分层 |
| 同commit及phase，所有配对等权 | 0.901 | 0.760 | 0.142 | 同角色27、跨角色41个对×commit×phase |
| 同commit及phase，共同分层等权 | 0.777 | 0.744 | 0.033 | 4个共同分层 |

分层等权先在每层内分别求两类配对的平均J，再只对同时存在两类的层等权平均。因此它既改变权重，也限制共同支持范围，不能把0.142到0.033的差异全部解释为纯粹的权重效果。阶段条件下19个分层共68个配对观测，只有4层可作两类共同比较。图3展示观测分布、共同层差异与删层敏感性；它们不是随机处理效应或显著性检验。

![图3：角色共享重合度与汇总敏感性](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p3-role-shared-context/figures/p3-fig3-role-overlap.png)

**图3。**（a–b）同commit以及再限定phase后的全部配对J分布，点存在共享成员／重复，黑线为描述性中位数；（c–d）双方均有观测的共同层，同角色减跨角色的均值差为实心点，逐一删除该层后的其余层等权均值差为空心菱形。S编号及同／跨角色配对数写在纵轴；横线连接0与层内差，不是置信区间。

跨角色配对的重合度并不低，且同角色优势对阶段、共同支持和权重敏感。另将版本折叠为`(team, rule_slug)`后，10条规则中9条被多个Agent读取；与30/34条版本化规则共享的结论方向一致，但该敏感性口径不再证明读取同一版本。数据因此支持共同事实并未被命名身份封闭，也不支持将角色等同彼此隔离的知识库。更合适的设计解释是共同事实之上的职责相关筛选；“按角色共享优于其它组织方式”仍需对照验证，不能由读取重合度推出。[逐对底表与公式](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/data/P3-pairs.json) · [阶段条件分析](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/data/P3-phase-pairs.json) · [全部结果](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/summary.json)

### 4.6 团队体验并非交付指标的附属品

开发者在研究中的回顾性回答是：“就是好像给了一个团队，可以扮演团队 leader， 有掌控感，上瘾”。[N06-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N06-E01.json) 该回答直接支持团队感、领导角色与主观吸引力，没有提供具体日期事件、量表或使用频率。“上瘾”按自述词记录，不作临床判断。名称、角色呈现和指令反馈可能共同承载这种体验，现有资料无法隔离其贡献。保留体验价值，同时拒绝用它替代交付质量，是本文设计取舍的最后一条依据。

### 4.7 共享范围、读取context与稳健性

全量矩阵将图2从单commit示例扩展为12个读取者×34条版本规则，覆盖230条加载行。28/34条规则被至少两种角色配置读取（82.4%），并非只在同角色内部共享。这里角色配置以冻结角色集合为单位，PD/PM合为一个配置，避免将同一成员重复计算为两个读者；这与前节“角色集合相交”的配对定义不同。

**表5。角色配置与原生读取context**

| 角色配置 | 读取Agent数 | 唯一context数 | 加载行 | 不同版本规则 | 每context规则中位数 [IQR] |
|---|---:|---:|---:|---:|---|
| Dev | 7 | 34 | 121 | 28 | 3 [3, 4.75] |
| PD/PM | 1 | 20 | 60 | 26 | 3 [2, 4] |
| Tester | 3 | 11 | 47 | 24 | 4 [3, 4] |
| analyst | 1 | 2 | 2 | 2 | 1 [1, 1] |

230条审计对应67个`(agent, team, context-kind, context-id)`组合，优先采用execution_id，否则采用planning_session_id：其中4个execution、63个planning。每context读取不同版本规则的中位数为3，IQR 3–4，范围1–8。context标识不是LLM turn，不能以230/67估算模型调用成本；各配置的规则数有交叠，不能相加作为全库规则总数。

![图4：角色配置、读取context和共享范围](/Users/oopslink/works/codes/oopslink/agent-center/docs/plans/reports/papers-010/p3-role-shared-context/figures/p3-fig4-context-coverage.png)

**图4。**（a）各角色配置的唯一审计context；（b）所有67个context的规则数，黑线为中位数；（c）phase字段对应的加载行，不是执行context类型，不能把execute phase等同execution_id；（d）每条版本规则被多少种角色配置读取。角色为冻结配置，未重建历史角色。

进一步逐一删除共同分层后重算等权均值差，检查结果是否由个别层驱动（图3c–d）。同commit条件下，删层后的差值保持正但接近0；同commit及phase条件下，差值可以转负。它们是有限数据敏感性范围，不是置信区间。

**表6。同角色减跨角色平均J的删层敏感性**

| 条件 | 共同分层数 | 全部共同层等权差 | 逐一删层后范围 |
|---|---:|---:|---|
| 同commit | 5 | 0.0627 | 0.0034至0.0956 |
| 同commit及phase | 4 | 0.0335 | −0.0443至0.0798 |

该结果进一步限制“同角色更应共享”的实证强度：观察到的差值并不稳定，不能把角色标签本身当作共享收益来源。共同事实、任务和阶段相关的选择仍应分开设计；名字的体验价值也不能由这些加载数字代替。[context及全矩阵](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/006-dense-evidence/data/P3-contexts.json) · [完整规则／commit映射](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/006-dense-evidence/supplement.md)

## 5 讨论与设计含义

### 5.1 从人格容器转向履责视图

以上证据支持一个有限而实用的设计命题：把角色看成共享知识的视图，而非永久拥有全部上下文的个体。名称负责可读交互，稳定身份负责归属和审计，角色说明当前所需的判断材料，权限限制可访问内容。这个方案并不要求销毁连续身份；它要求每次把信息加入上下文时，都能说明该信息与当前职责的关系。案例中同一角色可对应多人、同一身份可承担多角色，正好说明这种对应不应僵化为一人一库。[M3](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/derived/identity-role-map.json)

表7把建议拆成四类信息，以免“角色共享”退化为把旧的个人记忆复制到新的角色目录。每行都是待验证方案，并非本系统已经部署或完成消融的功能。

| 表7：建议共享层 | 放入的材料 | 必须携带的边界 | 证据状态 |
|---|---|---|---|
| 项目事实 | 当前接口、可核验约束、已确认决定 | 项目、来源、有效版本 | 建议，未消融验证 |
| 角色视图 | 本次职责所需规则与检查清单 | 角色、阶段、读取权限 | 建议，未消融验证 |
| 任务增量 | 目标、输入、验收合同及新发现 | 任务、责任人、时间和替代关系 | 建议，未消融验证 |
| 私有执行记录 | 临时尝试、原始工具输出、敏感细节 | 执行身份、授权和保留期限 | 建议，未消融验证 |

这个分层允许共享事实而保留独立检查。测试者可以获得候选版本、合同和已知约束，同时保留自己的验证过程；开发者也可以读取同一事实，却不因此获得测试者的所有私有状态。CodeDelegator 的隔离收益与私有状态研究的权限边界构成具体反证：若把所有历史等量广播，便不再是本文所说的有选择共享。[3]<!--ref:R14--><!--anchor:section:4.1, 5.2, 5.3-->[4]<!--ref:R08--><!--anchor:section:2.1, 4-->

### 5.2 版本和阶段比“大家记得”更可核查

角色视图还需要回答信息何时刷新。现有机制将规则绑定到 commit 与阶段，在途执行保留快照，新执行取得新快照；运行报告也明确描述了恢复沿用索引的合同。[S09—S11](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/source-evidence.md)、[N05-E05](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E05.json) 在这样的语义下，两个同角色成员暂时看到不同版本，并不自动构成失忆或共享失败。应先判断其任务是否允许继续使用旧前提，以及改变前提是否要求重新验证。

本文建议为每次共享保留来源、适用范围、版本和替代关系。若新事实推翻旧约束，视图应展示冲突和失效理由；若只是补充一个局部任务的细节，则不必升格为团队永久规则。治理案例中的规范化和替代关系为这种建议提供了现实动机，却没有测量该流程的净成本。[N04-E02](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N04-E02.json) 因而评价重点宜放在错误材料是否被纠正、所需材料是否及时可得，而不是规则总数越少越好。

### 5.3 共享还需要事实核验与可失败的评价

共享内容也可能包含错误解释。Rule Registry 集成报告指出两个上游候选实际上是兄弟分支，纠正了此前关于串行继承的说法。[N05-E06](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N05-E06.json) 这使角色视图必须区分参与者声称与可核验事实，不能只因多名成员重复了一段叙述就把它升级为知识。重复转发增加的是可见性，不是独立证据。

后续评价可以在保留显示名的条件下，比较个人历史、任务事实和角色视图等组织方式，固定任务、模型、工具与权限，并分别考察所需事实到达、过期信息引用、权限拒绝、交付正确性及主观体验。若角色视图引入额外分类工作却没有减少信息缺失，便应缩小或放弃它；若任务视图已足够，也没有理由强加角色目录。这样的可失败条件是设计主张的一部分，避免以“共享更好”预先替代实验。

团队体验则应另行询问。名称可以作为可选呈现，临时执行单元可以使用自动生成标签；用户仍能追踪责任与成果。由单人自述提出这个产品方向是合理的，但是否适用于其它开发者，需要新的体验研究，而不能从230条加载记录中间接推出。[N06-E01](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N06-E01.json)

角色视图的维护也应有明确退出条件。例如，若同一任务由开发转入验收，系统可重新选择适用检查材料，而不要求为角色迁移重建全部事实；若项目授权不允许某条内容被共享，就应给出可审计的缺失状态而非悄悄用另一角色的权限补齐。这样的设计把信息不足暴露为可以处理的条件。它仍需要实现后检验，尤其要比较维护视图和核对来源的成本是否抵消信息复用收益。

## 6 局限

本研究是单系统、目的性取样的回顾性分析。词法命中受检索词约束，邻接抽取不能还原全部会话，已编码证据也不是独立任务样本。部分排除标准在检索后收紧，可能改变留下的案例构成，因此本文不报告总体故障率或以正反例数量比较净收益。[M1](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/coverage.md)

配置是冻结时点的快照，固定源码只支持特定版本的机制解释。历史验收与提案处理主要依赖参与者报告，没有独立重跑界面、加载及交付流程；缺少完整模型输入，也无法把读取正文等同于理解规则。[S](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/reading/source-evidence.md) 数据中的名字多为项目或角色标签，不构成人格化人名与匿名执行器的严格比较。

体验材料来自一位开发者，缺少具体事件和量表，不能推断普遍吸引力、依赖风险或临床成瘾。外部 persona 与命名研究涉及不同任务，提供概念对照而非效果迁移。[1]<!--ref:R15--><!--anchor:section:4-->[2]<!--ref:R17--><!--anchor:section:3.1.2, 3.2.2--> 六维区分与角色视图是分析和设计工具，尚未证明它们优于现有组织方式。AI分析与写作仍待人工核查，独立AI评阅不构成人工复核。

另一个构念风险在于“按角色共享”的方案尚未被完整实现为单一干预。表7同时包含信息分层、版本绑定和权限过滤；未来若直接整体部署并比较前后表现，仍可能分不清哪一部分产生变化。因此应先定义最小可比较单位，避免把多项改动后的整体改善重新归给角色一词。本文的设计表用于提出这种检验要求，不能作为实现完成或效果已知的证据。

## 7 结论

本文将“不需要名字”限定为自动交付不应依赖每个执行单元拥有长期人格化身份的设计立场。可读名称已有明确用户偏好，命名成员也已有共享事实和规则的记录；这些证据共同否定了通过简单删名解决上下文问题的推论。[N01-E04](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N01-E04.json)、[N03-E03](/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N03-E03.json)

更可操作的方向是分开显示、归属、职责、授权、持久知识和本次输入，再围绕履责需要构建受项目、任务、阶段与版本约束的共享视图。事实可以复用，执行过程可以隔离，规则加载仍须与实际使用区分。该方案留下清楚的验证问题：它是否减少信息缺失与过期前提，而不增加无益分类工作或越权暴露。名称与团队体验可继续存在，其价值应以体验证据评价；自动交付则须以产物和验收证据评价，两者不互相充当证明。
这一结论允许两类产品选择并存：面向使用者保留易辨认的队友呈现，面向执行机制提供可替换且有明确责任的工作单元。设计取舍的关键不是名称是否像人，而是上下文与责任能否在替换、恢复和换角色时保持可追溯。现有材料足以提出该要求，但尚不足以宣告某种共享架构已经胜出。
<!-- BODY_END -->

## 参考文献

[1] M. Zheng, J. Pei, L. Logeswaran, M. Lee, and D. Jurgens, “When ‘A Helpful Assistant’ Is Not Really Helpful: Personas in System Prompts Do Not Improve Performances of Large Language Models,” *Findings of the Association for Computational Linguistics: EMNLP 2024*, pp. 15126–15154, 2024. DOI: [10.18653/v1/2024.findings-emnlp.888](https://doi.org/10.18653/v1/2024.findings-emnlp.888). [出版记录](https://aclanthology.org/2024.findings-emnlp.888/). 本语料的主张段落核于arXiv v3，正式出版身份核于ACL Anthology。

[2] J. Banks, N. D. Bowman, and R. Saladino, “Lexical Anthropomorphization Influences on Moral Judgments of AI Bad Behavior,” arXiv:2604.25814v1, 2026. DOI: [10.48550/arXiv.2604.25814](https://doi.org/10.48550/arXiv.2604.25814). [核定版本](https://arxiv.org/abs/2604.25814v1).

[3] T. Fei, C. Chen, Y. Pan, M. Zheng, and M. Song, “CodeDelegator: Mitigating Context Pollution via Role Separation in Code-as-Action Agents,” arXiv:2601.14914v1, 2026. DOI: [10.48550/arXiv.2601.14914](https://doi.org/10.48550/arXiv.2601.14914). [核定版本](https://arxiv.org/abs/2601.14914v1).

[4] S. Shekkizhar et al., “Why Do Agent Teams Fail Across Boundaries? A Failure Taxonomy for Agent-to-Agent Systems with Private States,” arXiv:2610.19342v2, 2026. DOI: [10.48550/arXiv.2610.19342](https://doi.org/10.48550/arXiv.2610.19342). [核定版本](https://arxiv.org/abs/2610.19342v2).

## 研究声明

**数据可用性。**证据副本、原始记录指针及固定源码保存在私有研究目录，附录提供本机绝对路径，外部读者无法直接访问。公开数据与脱敏许可待作者确认，不声称数据已公开。

**伦理与隐私。**资料包含开发对话、稳定身份和配置关系。伦理审查状态、授权范围与公开脱敏方案待补。本文仅呈现论证所需片段，没有新增用户实验。

**作者贡献。**人类作者、单位、通讯作者及CRediT贡献角色待确认。AI不是作者，不替代人类作者责任。

**利益冲突。** 待作者声明。

**资助。** 本研究未获得资助。

**AI使用。**001—009原研究为单AI探索；010本轮有分工AI补查、引用核验、写作及审阅。原研究与本轮分析均尚未人工复核；AI评阅不等于独立人工复核或跨模型复现。精确模型版本及参数未完整记录。最终作者须核对事实、引文、数据权限及结论。

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
