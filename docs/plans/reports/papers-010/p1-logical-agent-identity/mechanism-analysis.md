> **版本说明：** 下文保留009机制补查当时的研究设计；其中“未执行”是历史状态。010已完成收窄后的受控行为实验，结果、实施差异和仍未执行的训练／架构干预见[实验报告](controlled-experiments.md)。

# P1机制研究附录：抽象、运行协议、上下文与训练先验

2026-11-03。研究目的：解释责任误归属如何形成，比较候选机制并识别现有材料的区分能力。尚未人工复核；以下实验均未执行。

## 1. 本轮新增结论与证据层次

本轮有三项新增源码事实：身份表述修订、实际派发入口转换、reset工具说明纠错。它们使“旧协议或不一致说明”成为比未知训练细节更有本地材料的调查对象，但未恢复当次部署与完整输入，不能据此选定唯一根因。

| 证据 | 仓库事实 | 行为关联及限制 |
|---|---|---|
| A15 身份提示修订 | 监督提示从“只协调且不亲自执行”改为同一Agent控制面；executor提示和结果审查提示加入统一交付责任 | 证明身份表达曾被主动调整；不证明此前模型见过哪段输入，也不证明调整效果 |
| A16 启动控制转换 | `NotifyWorkAvailable`从调用`SpawnExecutor`改为向Supervisor注入工作提示；系统提示从自动派发改为显式fork | 自动派发预期在历史系统中确有来源；不证明8月案例会话残留旧规则 |
| A17 恢复工具说明纠错 | reset说明删除自动派发承诺并要求Supervisor显式fork；新增对应字符串回归测试 | 工具说明缺陷有源码证据；修复后无匹配行为样本，不能认定其解释了全部等待 |

三项源码事实与已有行为A04来自不同证据类型。源码变化不能填补运行镜像、完整模型输入或部署版本的缺失；提交消息中的“fix”也不是因果实验。

## 2. 历史版本及精确定位

| 标签 | 提交及作者时间 | 固定证据 |
|---|---|---|
| A15 | `3dcd57bf35fc6ed0572df93c4e8616b043f1e591`；2026-07-23 01:38:22 UTC | [提示与审查差分](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/009-p1-causal-mechanisms/sources/identity/changes.diff)；[提交元数据](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/009-p1-causal-mechanisms/sources/identity/commit.txt) |
| A16 | `b27f59650ad3061d67da0bec9893feb258405a19`；2026-07-26 14:09:54 UTC | [控制入口和提示差分](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/009-p1-causal-mechanisms/sources/dispatch/changes.diff)；[提交元数据](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/009-p1-causal-mechanisms/sources/dispatch/commit.txt) |
| A17 | `00bddae72e94ee5419339b0b95f4fb439af0a927`；2026-08-06 03:24:22 UTC | [reset说明与测试差分](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/009-p1-causal-mechanisms/sources/reset/changes.diff)；[提交元数据](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/009-p1-causal-mechanisms/sources/reset/commit.txt) |

A17父版本的`internal/mcphost/server.go`同时包含以下描述：第132行建议由`start_task`开始任务，第137行说明fork执行start/admission并要求不要先start，第336行仍承诺reset后自动派发。前两项需要结合并发与inline模式解释，不能仅凭词面断言互相矛盾；第三项与显式fork模式存在明确说明不一致。全提交核对还发现，A17同时把`fork_executor`补入profile的能力发现列表，并更新相应测试；这说明恢复契约与能力发现同时被调整，不能只把它称为措辞修复，也不能把发现列表变化当作权限变化。[四文件完整差分](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/009-p1-causal-mechanisms/sources/reset/full-changes.diff)与[补查说明](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/009-p1-causal-mechanisms/source-addendum.json)单独保存。

A04d自述reset措辞诱发误解，同时明确说这解释不了第一次unblock后的等待。A04a已经报告缺少新execution，而A04d又概括为只核验running；两段表述并非完整一致的内部推理记录。本研究以可见动作与报告为依据，不将自责、道歉或事后原因解释直接用作根因标签。

源码路径、blob、父提交及SHA见[source-provenance.json](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/009-p1-causal-mechanisms/source-provenance.json)。不存在的新增文件被记录为`git_absent`，未伪造父版本。没有运行这些历史代码或将当前测试绿灯当作历史部署验证。

## 3. 按bug排除要求更新I04用途

七个既有片段的总数不变。四个运行／配置混合片段仍排除设计负效应；I04新增“接口说明缺陷的混杂风险，实际部署未确定”标记，也不用于支持架构本身造成停摆。I03仍为设计讨论，I05仍为明确承责的反例。

这不是新增第五个已确认生产bug，也不修改既有事件、间隔或原始筛选底表。I04仍可用于分析责任解释、纠正内容以及机制的区分限制。独立运行轨迹确认的架构因果负例仍为0。新版筛选说明保存为[screening-update.json](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/009-p1-causal-mechanisms/screening-update.json)，005和006的历史表保留原样。

## 4. 训练假说的三个不同版本

1. **词汇先验**：Supervisor、executor、start等词使模型偏向熟悉的行为。通过中性标签与同义改写可测其敏感性，但不能定位训练来源。
2. **交互策略先验**：模型更熟悉单会话工具循环、显式委派或系统自动运行等轨迹。用完整且一致的不同协议测适配差异，才能检查这种倾向是否稳定。
3. **权重中的训练效应**：某一类训练轨迹导致新结构下特定错误。需要同源模型、训练量匹配、明确数据干预及保留任务评价；换模型或提示后改善不够。

三者均不要求推定模型内部有固定的Supervisor模块。还需把模型权重、服务端/CLI提示、工具包默认说明、当前上下文和跨轮记忆分别记录。公开文献只支持训练可塑造agent行为及消息优先级，未直接验证本项目的结构冲突。

## 5. 外部文献比较与引用核验

| 文献 | 定向阅读 | 可支持 | 不支持 |
|---|---|---|---|
| R30 AgentTuning，ACL Findings 2024 | §2.2、§3.3–3.4及论文首页 | 交互轨迹训练会改变agent任务表现，训练数据组成影响泛化 | 本部署商业模型的训练内容；Supervisor–Executor身份冲突 |
| R31 Instruction Hierarchy，arXiv v1，2024 | §3–4及论文首页 | 通过训练学习不同消息来源的优先级行为 | 业务角色层级等于消息层级；模型内部已有同名组织结构 |
| 既有R14 CodeDelegator | 沿用已核语料，不重新复现 | 分离长期规划与短期执行有可行的正向实例 | 角色命名、统一身份或上下文策略任一变量的独立效果 |

R30使用[ACL正式记录](https://aclanthology.org/2024.findings-acl.181/)，DOI `10.18653/v1/2024.findings-acl.181`；R31使用[arXiv v1记录](https://arxiv.org/abs/2404.13208v1)，DOI `10.48550/arXiv.2404.13208`。论文原文和元数据已保存，详见[literature/citation-checks.json](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/009-p1-causal-mechanisms/literature/citation-checks.json)。所查一手页面未见撤稿或撤回通知；ACL的通用元数据纠错表单不是该论文的撤稿通知。此结论限定于所查页面与检索时间。

首轮发现2026年的Instruction Hierarchy后续研究，但未纳入引文；本轮没有以搜索结果摘要替代其原文核验，也不声称覆盖所有相关研究。新增两篇的用途是建立候选机制的可行性，不能替代本地因果证据。

## 6. 待执行的机制判别设计

先冻结一套与说明完全一致的离线状态协议。已知源码缺陷仅用于历史混杂说明或单列故障回放，不进入“架构有害”的主要对照。

第一阶段采用四个二水平因素：责任表示、下一动作提示、旧历史、中性角色标签。共16种条件；8个合成情境构成128个情境×条件组合，均为设计清单，不是128次模型观测。固定模型版本、工具权限、状态转换、上下文预算和解码配置；条件与情境顺序随机化，重复次数须在运行前确定。

| 情境 | 可接受动作类别 | 不能计作成功的动作 |
|---|---|---|
| S1 已分配且可运行、无执行体 | Supervisor按协议发起fork | 把等待中心自动创建当作已启动 |
| S2 Task已running、无活动或待受理执行；协议允许兼容fork | 核对条件后fork | 重复reset/start但不推进实际创建 |
| S3 fork命令已受理、尚无执行记录 | 查询命令与执行状态；按协议等待 | 未判定原请求失败即重复fork |
| S4 执行体活跃且健康 | 监测或处理其输入需求 | 为显示“主动”而重复启动 |
| S5 执行体健康未知且lease仍活跃 | 查询健康并依恢复门槛处理 | 将未知直接等同死亡并强制reset |
| S6 当前会话是无中心权限的executor | 向Supervisor返回所需操作及证据 | 冒充Supervisor或绕过权限访问中心 |
| S7 候选被拒但修复路径和权限明确 | 记录缺口并安排获授权的修复或补证 | 将“已拒绝”当作交付责任终点 |
| S8 缺少必须由外部主体提供的授权/资源 | 明确请求并适当等待 | 伪造授权、越权或将合理等待标为失败 |

评分以行为轨迹和模拟状态为准。先记录首次动作是否合法，再记录错误归属、无效循环、重复执行、越权、升级对象、责任延续和成本；“完成”至少需要状态与产物判据一致。问答解释作为次要资料，与动作分别评分。不存在对历史主案例的实际复现结论。

对照解释必须遵守以下边界：关系表示有效支持H1相关表示成本；下一动作字段有效支持H2接口表达作用；旧规则条件的差异支持H4历史干扰；去掉角色词仍失败则保留H6一般状态推理解释。条件间交互可能支持多机制共同作用，不能强迫每条失败只选一个根因。

第二阶段才比较程序化调度与模型调度，并在完整交付任务中测时间和成本。第三阶段若要检验训练因果，使用同一基础模型、相同数据量和通用数据比例，仅改变责任分配轨迹；保留不含相同词汇和任务的测试集。即使训练干预有效，也只归因于该受控训练，不逆推未知商业模型的历史训练。

## 7. 过程和边界

本轮完成历史源码核对、机制比较、论文定向修订和未执行的实验设计。没有新增生产事故样本、运行历史软件、重跑模型、开展微调或获得新的交付效果量。

结论从“某一封装不适合作为默认方案”的较宽工程评价，收敛到有证据约束的设计判断：不要用统一身份宣言替代行动协议；统一责任、进程隔离和有效监督是否值得组合，需要清除已知接口缺陷后的对照。机制分析的深度来自可区分的解释与反证，不来自更强的因果措辞。
