# P1 补充附录：记录定位与统计口径

本附录随007正文修订整理。正文按研究发现组织；本文件保留时序、标识、查询边界与复算入口。007部分均来自既有冻结材料，未新增运行核验。009新增历史源码核对及I04接口混杂说明，见文末E节。原生记录的完整ID、时间戳与路径继续保留在论文“证据附录”。

**014增量。** [纵向分析附录](longitudinal-analysis.md)补入早期反复纠偏、提示修订及9月4日后续责任中断／局部恢复。旧表中的设计因果样本限制，不表示未观察到身份／责任问题；技术故障触发与故障后的责任响应分别取证。原短题数据不变，其解释收窄为完整显式责任条件下的动作选择，不能否定真实运行中的人工校准负担。新增原件、筛选和图5见[014数据包](longitudinal-014/README.md)。

## A. 主案例的事件与证据类型

案例I04，Task为`task-e12e4fb9`，以下时间为2026-08-06 UTC。秒级显示沿用此前正文，原始精度见A04、A05。

| 时间 | 记录 | 证据类型与定位 |
|---|---|---|
| 01:43:21 | reset | 原生Task动作，A05.4 |
| 01:43:34 | agent_started | 原生Task动作，A05.5；不是executor创建事件 |
| 01:44:03 | 报告无新execution、要求中心修复；同时记录blocked | 消息A04a与动作A05.6分别核对 |
| 01:45:22 | 用户询问executor是否应由Supervisor创建 | 用户消息A04b |
| 01:45:52 | unblocked | 原生Task动作A05.7 |
| 01:46:29 | Agent修正解释并报告显式fork | 消息A04c；command ID、execution ID及启动时间均为消息内容 |
| 01:54:50 | 事后解释reset重新派发与显式fork的混淆 | Agent消息A04d，不是独立根因轨迹 |
| 02:03:02 | 报告推送分支与交付SHA | Agent消息A04e；不等于最终Task完成 |
| 02:05:37.994988 | 再次blocked，涉及合同缺口 | 原生Task动作A05.8 |
| 04:06:20.857687 | unblocked | 原生Task动作A05.9 |

图2保留用户纠正至unblocked的29.639493秒，以及至显式fork报告的66.458005秒。它们是记录间隔，不是干预效果或模型推理时间。`fork_executor`需要显式调用、reset被理解为自动创建执行体的说法来自A04d事后解释；原文“auto-assigned and re-dispatched”用于定位工具语义线索，不视为已经恢复了当次完整工具输入。

两个混合片段分别发生于2026-07-22 07:20:47与16:48:13。前者伴随损坏thread，后者伴随空工作区及工作树配置关闭；这些交付后果不计入设计负效应。2026-07-23的身份修订讨论属于设计反思，2026-08-07的明确承责拒绝属于反例，不能仅据先后日期判断修订已部署或失效。（A02、A03、A06、A10）

## B. 阅读与可观测范围

010补查在26,961条消息上以三组固定正则得到429条去重候选，选取11个锚点及邻接窗口，保存并阅读全文67条不同原生消息。候选、消息、片段与Task动作是不同单位。主案例定向核对九条Task动作；控制事件与执行镜像查询为空，不能据空结果推定历史上未执行。

设计由来依据固定Git提交`28bb445e8dd5aa9535bf8b4826019f7fa0322f9d`中的2026-06-28初稿；原始术语orchestrator／监工与后续Supervisor对应，仅用于概念对齐，不推定部署实现相同。正文保留设计目标，版本与原始约束定位见A14。

## C. 动作审计的计数和归一

2,527条动作关联2,221个Task，无孤立Task标识。1,238个Task有任意动作行。`actor_ref=system:awi-migration`的597条迁移记录单独分离，余下1,930条非迁移动作覆盖927个Task；311个仅有迁移行，983个没有动作行。

比较`actor_ref`与`agent_ref`前，将冻结配置中的内部Agent ID和identity ID归一到同一逻辑Agent。前者是触发主体，后者是动作所涉Agent，不表示两个执行进程。`agent_started`也不等于executor创建。字段说明取自当前源码，只解释字段，不倒推历史部署。记录中的453条blocked与190条failed未被转为设计失败次数。

七片段筛选及“独立轨迹确认架构根因”的严格敏感性口径保留在表3。007口径留下行为主例与反例，后者的合格因果负例为0；009另为I04标注接口说明缺陷的混杂风险，停摆不支持架构负效应。两轮均不提供发生率分母。

## D. 原始来源与复算入口

| 原来源标签 | 既有材料 |
|---|---|
| 制图底表与原生定位 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P1P2/data.json) |
| 源文件、字段与哈希 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P1P2/source-provenance.json) |
| 定量分析005 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/README.md) |
| P1筛选底表 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/data/P1-screening.json) |
| 快照可用性审计 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/data/snapshot-audit.json) |
| 全部原生动作与逐行归一 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/006-dense-evidence/data/P1-actions.json) |
| 字段含义的当前源码说明 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/006-dense-evidence/sources/task-action-field-semantics.txt) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/findings.md) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/division-006/sources/agent-supervisor-executor-identity-contract.md) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY4B5KS3272X3AMXDXHQMEC0.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY4B74V6XGRFG85EV29RS1RT.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY4B7K5QXK434NNHY9K1FKEF.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY5BMKX9ZM03BTJTENTQSKDB.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY5BYKS2T49YEXAGRPQQ9XWN.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY5BZ3804ZA0ETH34RA1XEH1.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY5C4XXCTE2AGYB373M42A9Y.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY5C5HW683A4YZG91P23RC7G.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZABVT594GFNYTWZ7AZRHF30.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZABY7HSVA1FF42EH7XYX7GX.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZAC08EKE041V1XB1C2XWE5V.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZACFHV3BFM5CP04CR07Q79W.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZACYJWJHHWTFHEWE7EHAVH8.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY68C1K5SHCFA4SFNZSS7C4C.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY68TD24Y07FCY5CZY56JEDZ.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY699NV8C4KRZ8DNXC4858KM.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KY69AW978C5TVS6GBM5TXK6N.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZDJ41E4XMR6RSRDW2342DCE.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZDKN7K7XRNZ0DPE7G9V0GQA.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZDKS2M645VTZJ9GR8SW0MDJ.json) |
| 动作原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/pm_task_action_logs-selected.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/worker_control_events-selected.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/agent_execution_mirrors-selected.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S01-03.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S01-05.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S02-02.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S02-04.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/sources/access-before/access-ram-roles.html) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/sources/access-after/access-ram-roles.html) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/sources/access-after/verify.mjs) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/sources/spark-before/App.test.ts) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/sources/spark-after/App.test.ts) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/reports/exact/agent-harness-review-increment-cases-009.md) |
| 固定原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/002-p1-design-origin/sources/agent-concurrent-execution-first.md) |
| 版本与哈希 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/002-p1-design-origin/sources/provenance.json) |


## E. 009补查：源码契约与I04归因边界

历史源码已保存身份提示修订、自动派发改显式fork，以及reset说明纠错三个提交及其父版本。它们不等于当次部署与完整输入的恢复。I04保留为行为材料，停摆不再用于支持架构负效应；不存在新增生产bug确认数。[机制附录](mechanism-analysis.md)包含精确版本、引用核验与未执行的实验设计。005/006历史底表不覆盖；新筛选说明单独保存。


## F. 010受控实验与历史案例分离

新增512次动作选择、32次默认预期诊断及3次事后续步；与生产消息、Task动作和七个选编片段分别计数。16个预定恢复组合均首步成功，没有追加恢复调用。主结果、全部三个额外start步骤及各自单因素反例见[实验报告](controlled-experiments.md)。模型原始文本不作为训练原因的直接证据。历史事件、005/006底表与009源码归因边界均保留。
