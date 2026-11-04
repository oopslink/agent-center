# P2 补充附录：消息区间、对象定位与结构指标

本附录随007修订集中原正文的日志与复算细节。全部范围沿用既有冻结研究，不新增故障编码或运行试验；完整原生消息ID见论文证据附录。

## A. W01的分配与启动记录

Plan为`plan-2a8bc24c`，初始generation为`generation-709baba8`。2026-08-28 16:28:21 UTC，PD报告因五节点无assignee而被拒；16:31:16用户要求PD自行分配；16:31:48保存带分配的原生快照，同秒出现首任务ready通知；16:31:57为PD后续报告。首次拒绝及前态仍是PD报告，后续快照不反证此前全部字段已被独立核验。

五节点的Task标识及分配保留在正文表1、图1。用户指令到快照和系统通知的间隔分别为31.295246与31.872839秒（图2）；它们不衡量停摆总时长或人工节省时间。

## B. W02的对象映射与旧摘要修正

2026-06-16 UTC，01:39:41用户限定integration的开发期合并职责；01:41:52 PD将迭代主干写成main；01:45:51用户明确开发分支、迭代主干与main的三层映射；01:53:27 integration报告远端迭代分支已经存在。表2保留其精确时间和判断用途。

旧C07-E07实际是构建、文档、tag、合main与清理次序纠正，不是明确的Task提前done。C07-E10与E09的发送者均为PD，不能作为integration的第二次独立确认。上述修正沿用010原文核对，不删除旧研究版本。（B03.7–B03.10）

Graph检查中的候选映射如下。其行为缺口保持报告级证据，未重新取得全部源码或复跑验收。

| 候选SHA | 当时记录的检查范围 |
|---|---|
| `4d4ccf32` | 跨页合并、计数与回归通过报告；后续指出点击聚合边只请求一个effect |
| `782cbde3` | 两个endpoint检查，但测试局限于同project；后续指出跨项目作用域问题 |
| `1ba42c24` | Supervisor与Tester报告覆盖各effect的project对应 |

## C. 完整消息区间与响应定义

| 区间 | 首条UTC时间 | 末条UTC时间 | 原选编／区间全部消息 |
|---|---|---|---|
| W01 | 2026-08-28 16:19:40.531485 | 2026-08-28 16:31:57.036572 | 6／6 |
| W02 | 2026-06-16 01:39:41.396427 | 2026-06-16 02:12:04.153627 | 11／22 |

以原选编首尾为固定边界，读取同conversation内全部消息；W02补入十一条，其中五条为用户消息。旧选编保持原样作为历史研究产物。用户消息到同区间时间上下一条Agent消息的间隔作为描述性响应量，连续用户消息可对应同一回复，12个间隔不是独立实验。

三层分支纠正后的下一条PD消息，旧选编显示173.71秒，完整区间显示32.68秒；后者仍包含对象误解。这是资料补全导致的测量修正，不是系统优化效果。表4的互斥主编码沿用5条启动／继续、5条责任对象顺序纠正／质询、1条转贴、1条完成追问。

相邻发送者切换只计顺序不同，既不证明回复关系，也不编码消极动机。明细与全部28条消息在原005、006数据入口保留。

## D. 配对结构指标与排除规则

26个非显式夹具Plan共195个generation，全部seq边子图通过无环检查。根与按时间最后记录配对；seq深度为最长前置路径的边数，最大拓扑层为最长前置距离相同的节点数。累计节点可包含完成或废弃任务，不代表活动执行集合。

跨assignee边比例以两端负责人已知的全部归一边元组为分母，根与最后均有可比较边的21个Plan参与配对。没有可比较边不填为0%。不同负责人、seq深度和最大拓扑层均有26对；93.8%是21对的末记录中位数。

明显inline/fork、REJECT后ready、旧condition、依赖未消费和lease失联问题排除设计负效应；结构普查仍是描述性记录集合，不声称无bug。W02早于后来内部Supervisor/executor设计，仅用于跨角色协作分析。

## E. 原始来源与复算入口

| 原来源标签 | 既有材料 |
|---|---|
| 制图底表与原生定位 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P1P2/data.json) |
| 源文件、字段与哈希 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/003-data-figures/data/P1P2/source-provenance.json) |
| 查询、原生28条消息与逐条编码 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/data/P2-interventions.json) |
| 方案及执行后增补 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/005-quantitative-analysis/protocol.md) |
| 全部快照与配对指标 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/006-dense-evidence/data/P2-topology.json) |
| 消息序列底表 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/006-dense-evidence/data/P2-sequences.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/findings.md) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01M14JPXVK1Z8CKDYMTD3TSPAX.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01M14K5QQTGJF62VANSQC5WW38.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01M14K6T665DY5ZSR50T9ACMW4.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01M14KC5XZJD4PGDV4614XEWSV.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01M14KDD3C9JDTE6PYV3Y01MB1.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01M14KD520A5X362XVHJ64EKC7.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/raw/pm_plan_generations-plan-2a8bc24c.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E01.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E02.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E03.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E04.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E05.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E06.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E07.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E08.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E09.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/inherited/C07-E10.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KV71C7NM79RDSTBBVEM5K6BY.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/reports/exact/agent-harness-review-increment-cases-009.md) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S04-02.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S04-03.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S05-02.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S05-04.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S05-07.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S05-09.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/C05.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S06-02.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/sources/spark-release-README.md) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S01-05.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S01-06.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S02-04.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S02-05.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S05-03.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S05-08.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/derived/relay-pairs.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZDJ41E4XMR6RSRDW2342DCE.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZDKN7K7XRNZ0DPE7G9V0GQA.json) |
| 原文 | [打开](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/phase1_evidence/records/01KZDKS2M645VTZJ9GR8SW0MDJ.json) |


## 011机制对照

新增256次短题模型响应，与历史消息、图快照及十二对结构反例分开计数。全部反例、规定正例、字段反馈和方法偏差见[实验附录](controlled-experiments.md)。未重跑历史生产验收，明显系统bug继续排除设计归因。
