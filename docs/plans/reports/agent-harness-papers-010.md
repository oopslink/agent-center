# Agent Center四篇论文工作稿（010）

**作者：** oopslink

**作者主页：** [https://github.com/oopslink](https://github.com/oopslink)

2026-11-02。按作者改定的四个问题，**已完成四篇完整论文工作稿，并补入可复算数据、图表与结构图；研究仍为AI探索，尚未人工复核。** 每篇包含中英文摘要、相关研究、方法、案例、讨论、局限、参考文献和证据附录。已完成007正文重组、P1实验010与P2–P4机制实验011；正文计数与图表／图注分别保存。

## 论文与各自要回答的问题

| 论文 | 研究问题 | 论证边界 |
|---|---|---|
| [P1 Supervisor–Executor 架构中的身份抽象与责任归属](papers-010/p1-logical-agent-identity/paper.md) | 统一归属如何转为行动责任；抽象、架构、上下文与训练模式分别可能起什么作用？ | 干净合成条件未检出身份表示效应；训练原因和真实交付效果尚未识别 |
| [P2 多智能体工作流中的协调失效与责任分配](papers-010/p2-workflow-responsibility/paper.md) | 为什么清楚的前置条件仍被交回用户决定，角色口号为何不能确定下一动作？ | 不把正常守门、等待授权、不同范围的验收叫扯皮；不声称模型有推诿动机或已测得停摆率 |
| [P3 多智能体系统中的命名、角色与上下文共享](papers-010/p3-role-shared-context/paper.md) | 哪些需求需要标签、哪些需要稳定身份和职责相关知识，团队体验与自动交付如何分开？ | 名字直接处置差为0；按生产者角色过滤会漏跨角色事实，完整职责视图的净收益仍未验证 |
| [P4 智能体任务图演化中的产物继承与验收语义](papers-010/p4-dag-evolution/paper.md) | 改图什么时候表达了真实新工作，代码、产物与验收变化各自起什么作用？ | 不以代数、任务数或后继PASS衡量收益；不把未知根因的重复改图计为设计失效 |

四篇共用同一个部署、同一冻结快照及部分案例。P1分析同一逻辑Agent内部的身份/行动责任；P2分析跨角色与任务的推进责任；P3分析身份与知识组织；P4分析恢复与产物。复用证据会明确标注，不当作四次独立验证。

**P1后续修订。** 按作者意见，先解释“Agent级串行锁与身份/进程绑定 → 常驻Supervisor加临时executor → 并行、按任务选模型、统一回写的预期”，再分析身份混淆及方案不足。新增固定的2026-06-28首次设计稿作为A14，设计动机与实测收益分开；保留有效监督反证及明显系统bug排除。中英文摘要仅作相应局部调整，未整篇重新生成。[修订、原稿、差分与定向核对](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/002-p1-design-origin/README.md)

## 010补查如何改变写作基础

**最新作者信息修订（013）。** 四篇统一署名oopslink，作者主页为[https://github.com/oopslink](https://github.com/oopslink)；移除过期的作者占位说明，保留未声明的CRediT分工状态。研究正文、中英文摘要、数据及图表不变。[修改与检查](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/013-author-metadata/README.md)。


**此前题名修订（012）。** 四篇题名改为具体研究对象与分析问题，去掉重复的长方法副标题，并统一中英文题名。正文、摘要、数据、图表和引用逐字节保留。[题名对照、依据与检查](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/012-academic-titles/README.md)。


**此前机制实验修订（011）。** P2–P4各256次主调用，另24次事前字段反馈和128次事后材料修复，共920请求、921传输尝试。原125次评分不匹配中106次落在四个有缺陷的情境，13次为格式，6次为引用用途歧义；缺陷情境全部条件排除负面归因，原评分不改写。修复后128/128。P3两模型个人／同生产者角色／全部／当前范围视图的直接处置均12/32、8/32、32/32、32/32；名字直接差均0，但严格分数并非都相同。P2/P4有效短题没有提供语义负效应证据；未执行真实长期协作、用户体验或静态／动态策略对照。新增三图六表，四篇合计20图34表。全部尚未人工复核。

[三篇修订全过程](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/011-p234-controlled-mechanisms/README.md) · [完整实验报告](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/report.md) · [原评分与材料审计](/Users/oopslink/works/research/agent-harness-study/run-001/experiments/p234-mechanisms-002/analysis/validity-audit.json)。


**此前实验修订（010）。** P1完成512次动作选择＋32次默认诊断。干净条件两模型均64/64，统一身份与组件表述的配对差为0；全部条件Sonnet256/256、Haiku253/256。三个额外start均在旧历史下，并在另行标记的事后续步中启动，未观察到停摆。32诊断均未指定主体，训练冲突未获因果支持。新增三面板图与结果表，四篇合计17图28表。[P1修改记录](papers-010/p1-logical-agent-identity/REVISION-010.md) · [实验报告](papers-010/p1-logical-agent-identity/controlled-experiments.md)。

**此前机制修订（009）。** P1补查三次历史源码变更，比较六项竞争解释并新增三表；四篇当前合计16图、27张主文表。I04存在接口说明缺陷的混杂风险，停摆不用于架构负效应。训练所得行为模式与外部结构冲突保留为假说；16条件对照尚未执行。中英文摘要局部同步，无新增模型效果量。[P1修改记录](papers-010/p1-logical-agent-identity/REVISION-009.md)。

**题名修订（008）。** P1改为《Supervisor–Executor 架构中的责任归属问题：基于 Agent Center 的案例研究》，并补入对应英文题名；正文、摘要、图表、数据与引文保留。[修订说明与差分](papers-010/p1-logical-agent-identity/REVISION-008.md)。

**正文修订（007）。** 四篇按“研究发现—机制分析—设计启示”组织正文，逐秒时序、原生标识、查询明细与历史摘要纠错集中到各篇补充附录。16幅图、24张主文表及其数据保留；P2图4仅澄清配对分母表述。中英文摘要未重新生成，相关研究、参考文献及声明保留。[逐篇修改与明细入口](papers-010/revision-007.md)。本轮为同一AI定向修订与自查，未新增独立评阅或人工复核，旧审阅不自动覆盖当前文字。

**此前扩展分析与图形重组（006）。** 四篇现有16图、24表；本轮新增5图、6表，重绘全部图形。七幅机制／时序图保留证据关系并压缩版式，九幅统计图覆盖完整矩阵、配对分布和多面板分析。[全部图册](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/006-dense-evidence/gallery.md) · [16页矢量校样](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/006-dense-evidence/figure-proof.pdf) · [方法、结果与底表](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/006-dense-evidence/README.md)。

新增证据包括：597条迁移动作剥离后，非迁移记录覆盖927/2221个Task；195个快照的seq依赖均无环，26Plan负责人中位数保持6.5而深度由3到6；230条规则审计对应67个context，28/34规则跨角色配置共享；169父子转移中160次改状态、16次改负责人，3次拓扑未变仍有状态改动。统计单位和反证都保留，不能解释为无bug样本或架构因果效果；仍未人工复核。

**此前定量分析（005）。** 四篇新增5张表、2幅双分图，合计11幅图、18张表（含概念／案例表）。[完整分析、底表与复算](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/README.md) · [新图预览](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/gallery.md)。本轮是定向自查，尚未独立AI评阅或人工复核；历史审阅不覆盖新增正文。

| 稿件 | 新增分析与关键结果 | 仍不能推出 |
|---|---|---|
| P1 | 7个身份片段筛选，4个运行／配置混合片段排除负效应归因；快照中1238/2221个Task有动作日志 | 架构失败率或净因果效果；行为主例也缺独立根因轨迹 |
| P2 | 恢复两个完整有界区间的28条消息；12条用户消息中5条启动／继续、5条责任／对象／顺序纠正或质询 | 全系统人工干预率；下一条回复不代表问题已解决 |
| P3 | 78个Agent对×commit观测，同角色平均J=0.818，跨角色0.744；阶段共同分层等权差缩至0.033 | 名字或角色组织的交付效果；配对并非独立样本 |
| P4 | 169父子转移中149次保留全部父节点；新增节点占子代中位9.5%；26Plan最后／根节点比值中位2.04倍 | 保留节点即无效重复，或图增长即认知负担 |

W02旧选编11条，完整区间实际22条，新增5条用户消息；同一纠正后的下一条PD回复从旧选编173.71秒变为32.68秒，但早回复仍有对象误解。这是恢复记录后的测量修正，不能当流程改善。P3分层比较同时改变共同支持与权重，P4按转移／Plan等权平均新增占比分别12.7%／18.9%，均在正文解释。


**插图版式更新（004）。** 九幅图已重绘为统一175 mm双栏宽度、8–9 pt字号、细线结构与蓝灰配色，图内保留英文短标签，中文图注及正文不变。[九图预览](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/004-publication-figures/gallery.md) · [矢量PDF校样](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/004-publication-figures/figure-proof.pdf) · [变更与自查](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/004-publication-figures/README.md)。本轮为图形自查，不将003旧图的独立审阅冒充为当前图片已独立审阅。

**此前数据与图表修订（003）。** 四篇共新增9幅图、7张数据／案例对照表，图源、底表、提取脚本、PNG/SVG/PDF和原生定位均已保存。[完整图表与复算入口](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/README.md)

| 稿件 | 可直接查看的图表 |
|---|---|
| P1 | 逻辑Agent架构；I04状态／用户／报告时间线；正反案例及bug排除矩阵 |
| P2 | 原生五节点依赖和分派图；责任纠正时间线；分支对象与行动责任对照 |
| P3 | 身份与context结构；9×4同版本规则读取矩阵；34条版本化规则的读取覆盖分布 |
| P4 | 排除显式夹具后的代际分布；Mermaid实际改图；generation与Git成果谱系对照 |

复算显示：230条加载行对应34条版本化规则，30条被至少两个Agent读取；排除6个显式夹具后，102个Plan中26个有generation，共195条。P2的“四角色”改准为“四个不同assignee”，不混同配置角色枚举。这些是描述性结构与时序，不是名字、架构或DAG的因果效果。

原有009较偏重审查与验收边界，不足以直接回答作者要求的身份和责任问题。因此本轮追加了有界查询，而非只改论文标题。

- **身份主例：8月6日。** Supervisor将Task恢复到running后等待中心创建executor，后来承认自身应显式fork。原生Task动作表佐证reset/start/block/unblock，后续有交付消息；完整控制事件和执行镜像缺失，因此不把Agent事后自述当成独立根因证明。
- **流程主例：8月28日。** PD报告五个节点无assignee导致正常启动拒绝，转为等待确定负责人；用户要求自行分配后，原生generation记录五节点分配到四个不同负责人，并出现ready消息。可讨论下一动作责任没有闭合，不宣称双方恶意推诿或复杂度因果已证。
- **保留反证。** 7月22日的责任外推话语伴随thread/工作树故障，只保留可分离的责任表述；8月7日同Agent责任明确仍拒绝缺生产证据的自有产物；009中的有效监督检查仍保留。
- **纠正旧摘要。** 006 C07-E07实际是发布操作次序纠正，不是明确的Task提前完成；C07-E10发送者仍是PD，不能算integration的独立验证。旧报告原样保留，论文使用此次原文复核结果。

检索26961条消息得到429条词法候选，实际窗口阅读67条，加旧C07复读去重共76条原生消息，组织9个分析片段。候选量、消息数、片段数都不是故障发生率或独立样本量。[补查与逐案允许主张](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/findings.md)

## 引用与稿件状态

作者选择引用存在性和撤稿政策均严格处理。本轮从既有30条文献中定向核验14条，13条允许引用，保留精确身份、版本、正文节定位和核验范围。R16的出版记录指向伦理批准/知情同意勘误，勘误全文未核实，暂不采用其研究结论；不把勘误误称撤稿。[核验范围与排除](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_literature/README.md)

正文采用简体中文和中英文摘要、IEEE引用及Markdown工作稿。作者已声明无资助，四篇写明“本研究未获得资助”。作者署名及主页已明确；机构、CRediT、利益冲突及伦理状态仍等待作者声明；没有指定投稿会议或取得人工审阅，不声称已可投稿。AI写作/审阅不能替代人工复核或历史产品验收重跑。

四篇初稿及003版分别经过独立于起草调用的AI审阅，历史版本未留下未解决的Critical/Major问题；P3英文摘要将复数Users改为The developer，P1/P3的编辑差分另行核对。这里的“独立”仅指调用与输入隔离，不代表独立人类判断、不同模型或实验复现。[最终审阅处置与正文哈希](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase6_review/final-disposition.json)

上述四篇初次审阅各有历史版本绑定；P1设计由来修订另作定向审阅，不把原稿审阅结果直接套给新稿。最新绑定与本次核对范围均保留在同一最终处置入口。

数据／图表修订同样另有当前稿件与图像哈希绑定；历史审阅仅适用于对应旧版。63份不同来源文件经过哈希核对，三个提取脚本重复生成的数据与打包底表字节一致；这些机械检查不新增实验有效性或人工复核结论。

已核对引文编号、允许来源、节定位、DOI对应、原文证据链接和声明。技能缩写检查的非阻断建议及专名/状态字面量处理另见[编辑核对说明](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase5_validation/acronym-disposition.md)。这些检查不构成论文科学有效性证明。

## 数据和过程入口

- [010完整写作档案](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/README.md)
- [作者四篇改定与严格政策](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/intake/author-decision-002.json)
- [四篇结构与证据分配](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase2_structure/four-paper-outlines.md)
- [原三篇方案（历史版本）](agent-harness-paper-proposal-010.md)
- [当前失败复盘](agent-harness-failure-review.md) · [009综合](agent-harness-synthesis-009.md)

本轮仅研究与写作；011完成920次模型请求，另保留010的P1实验；未修改产品、生产数据或Git引用，未提交Git。完整私有库与对话不是公开数据包，论文写作不代表对外发布原始材料。
