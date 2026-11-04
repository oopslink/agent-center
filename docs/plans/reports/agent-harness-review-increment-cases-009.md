# 009：Supervisor与流程增量的逐案记录

2026-11-02，案例时间UTC。**单AI探索，尚未人工复核。** 六个片段来自三个已研究Plan，相互关联，不是六次独立试验。编码只比较可见记录；executor delivery消息是简短交付通知，不等于完整模型输入。当前Task描述与五份创建generation快照逐字核对一致，未用当前描述擅自回填历史。

功能标签：F事实/检查、J语义/合同判断、A执行/整合动作、R转述/通知、W等待/停止、U未知，可以并存，不是好坏标签。每个片段保留反证与根因限制。[完整证据索引](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/index.md) · [结构化编码](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/derived/episodes.json)

## S01 Access：Supervisor给出了具体修复，而不只说不通过

`plan-e91d02b8`，`task-75dedc1f`。08-23 15:19 executor通知`20bf78ed`已推送；15:28 Supervisor指出R09缺Risk字段、R12删除确认预填且按钮默认启用，要求原分支修正并加verify；15:33通知新SHA `06183d8c`；15:34同Task报告PASS；15:35向Plan同步。[拒绝 S01-03](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S01-03.json) · [通过 S01-05](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S01-05.json)

本轮直接读取这两个Git版本：旧HTML编辑drawer没有Risk选择器，删除输入预填stable key且按钮无disabled；新HTML增加Risk selector，删除输入为空、按钮disabled，input事件比较exact stable key；新verify增加对应静态断言。[前版](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/sources/access-before/access-ram-roles.html:68) · [后版](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/sources/access-after/access-ram-roles.html:92) · [verify](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/sources/access-after/verify.mjs:52)

**F/J增量成立于可见链路：**具体合同检查→可操作拒绝→对应代码变化→复验报告。新源码支持参与者叙述，但没有独立重跑浏览器。相同Agent身份承担executor交付通知与Supervisor审查符合内部S/E语义；不能据此称为两个独立Agent投票。任务快照dispatch_mode为空，内部执行由当时明确的executor/监督消息支持，不从空字段强推模式。

## S02 Spark：测试全绿仍缺近半合同

`plan-5a5f20b6`，09-01 11:26交付`ce5bbe69`；11:29 Supervisor报告npm test 4/4、build/lint通过，却只有13类向导，而原根generation早已要求25类；测试仅断言至少12类。13:00后续`87eb1749`交付，13:02报告25类通过。[拒绝 S02-02](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S02-02.json) · [通过 S02-04](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S02-04.json)

本轮固定源码确认旧测试使用`toBeGreaterThanOrEqual(12)`，新测试列出25个ID并断言唯一数和长度都是25；不是仅修改测试名称。[旧断言](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/sources/spark-before/App.test.ts:29) · [新断言](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/sources/spark-after/App.test.ts:28)

**F/J增量：**审查检查了测试是否足以证明需求，超出“测试命令是否绿”。普通产品遗漏不属于要排除的harness运行bug；期间stale/pending等恢复另行排除，不估算纯审查节省或新增成本。代码及新测试并不能独立证明25项所有真实后端行为完整，局部UI合同与发布结果仍分开。

## S03 Spark：不是只认SHA，关键是保留了什么

13:02 Supervisor接受不同SHA重建的前置成果：`ce5bbe69`与`20e942f6`具有同parent、同完整tree。13:11却拒绝新候选`5033340b`：它没有纳入已验收Web候选`87eb1749`，且自己的系统报告仍NO-GO。之后重建合同，后续`d4cc8f6c`报告含正确候选。[同树解释 S02-04](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S02-04.json) · [拒绝 S03-02](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S03-02.json)

008已独立Git核对这些祖先/树关系，本轮字节一致复用[原命令结果](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/sources/008-git-checks.json)，没有声称再次独立验证。**F/J增量：**确定性产物关系服务于实际交付合同；相同SHA、旧main祖先和“有推送”都不能独自回答是否保留已接受成果。错误基线来源未明，不能归为S/E或DAG机制导致；后续还有lease恢复，不能把所有进展归为一次监督拒绝。

## S04 Graph G7：计数正确，不代表点击后证据齐全

`plan-236dadc8`，09-03。15:36 `task-5fdf7720`交付`4d4ccf32`；15:39同Agent Supervisor报告祖先、跨页合并、evidence计数和全Web回归通过。15:40另一Agent Tester在同SHA上REJECT：聚合边只保留一个effect ID，drawer只请求一个endpoint；新增测试仅断言“evidence 2”文本，没有点击取齐两页证据。[Supervisor S04-02](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S04-02.json) · [Tester S04-03](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S04-03.json)

其后合同要求两个endpoint返回共享+独有事件，点击后共享只出现一次、两端独有事件均在。**增量是具体行为覆盖，不能只归为“又多一层”。** 开发Task写聚合/去重/不回退，独立验收明确要求drawer覆盖两页；两者范围不完全相同，不能将PASS/REJECT直接当逻辑矛盾或Supervisor错误率样本。可以确认的是Supervisor PASS没有证明产品整体取证完整。

本轮尝试读取该版本及后两版Graph源码，6个blob均不可读；保留报告级证据，不宣称独立复现。早先runtime engine不可用、迟到ready和最后交付检测问题退出负效应分析。

## S05 Graph G8/G9：增加反例，而非重复原检查

15:49交付G8 `782cbde3`；15:54 Supervisor报告两个effect端点与共享/独有事件测试PASS；15:56 Tester指出**两个effect都在P1**，组织级合并边跨P1/P2时仍错误共用第一个project scope。后续G9合同明确每个贡献者的effect/project对应关系。[G8监督 S05-02](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S05-02.json) · [跨项目缺口 S05-04](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S05-04.json)

16:13 G9 `1ba42c24`交付；16:17 Supervisor核候选、scope实现与P1/P2断言，16:24 Tester在同SHA报告PASS，并补全量Go/integration/e2e覆盖。[G9监督 S05-07](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S05-07.json) · [最终独立检查 S05-09](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S05-09.json)

前一轮没有主动覆盖P2，下一轮补入P2，这是可见的信息和测试输入增量；不能称同一个REJECT重复三次。最后没发现新缺陷的独立检查也不能据此判无用。另一方面，两层都围绕已给定测试执行时，不能推断它们的错误统计独立，或按两次PASS累加成功概率。本轮没有完整模型上下文，也没有独立执行这些历史测试。

## S06 发布报告：任务完成与产品NO-GO可以同时正确

`task-045a426e`的创建generation明确要求完成发布矩阵并给出可审计GO/NO-GO，**不是保证得到GO**。14:41交付证据SHA `42934559`，14:44 Supervisor接受报告，但明确命令404、类别不一致、gRPC问题、缺task-input与Docker不可用造成NO-GO。[合同 C05](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/C05.json) · [监督 S06-02](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/evidence/S06-02.json)

本轮固定Git中的[持久报告](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/sources/spark-release-README.md)确实记录NO-GO、所测实现SHA、缺失task-input及原始证据引用。只读报告，不重跑漏洞/环境扫描。此例反对“Task通过但产品不行就是监督自相矛盾”的解释：评审对象不同。系统后续若把报告Task完成显示为产品可发布，才会发生证据替代；本轮不凭这一条记录断言具体UI已经如此展示。

## R01—R04：没有新增产品检查的转述

会话归属由数据库`conversations.owner_ref`核实，不按措辞猜测。

| 对照 | 两条消息 | 可见用途 | 当前判断 |
|---|---|---|---|
| R01 Access S1 | 15:34:52 Task → 15:35:10 Plan | 重述同SHA检查，补下游已派发状态 | 结果同步；不是第二次独立验证 |
| R02 Spark G7 | 13:02:15 Task → 13:02:23 Plan | 重述同树继承、25向导与测试结果 | 未见新增检查，不能据此叫浪费 |
| R03 Graph G8 | 15:54:43 Task → 15:55:09 Plan | 重述同SHA、同project取证与测试 | 为Plan传播结论；不能重复计质量收益 |
| R04 Graph G9 | 16:17:15 → 16:17:24，同Task | 候选、祖先、scope修复、P1/P2和测试相同，补完成/下游提示 | 近重复表达的简化候选；未知实际turn与消费需求 |

消息逐字原文和会话ID见[四组配对](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/derived/relay-pairs.json)。没有对全库算冗余比例，没有把消息文本重复等同于测试重跑，也没有估计token/人力节省。

## 排除、可用性与读取限制

3Plan源包100Task/712消息；窗口198条中含63条system、134条Agent及1条user。窗口是上下文范围，不是198条全部人工式标注。Access23条全文与关键Graph/Spark片段深读，其他以预览筛选；选编41项原生证据。每个完整输入是否可恢复另行检查，不能凭简短delivery通知宣布Supervisor创造了所有新知识。

8Task execution mirror为0行；usage有28行且覆盖8Task，均source=report，但schema没有supervisor/executor、message/turn/execution关联键，且未验证完整性，不能直接衡量重复审查成本。6个源码读取失败、runtime恢复、局部与产品合同差异均写入[排除台账](/Users/oopslink/works/research/agent-harness-study/run-001/synthesis-009/derived/exclusions.json)。

报告判断仍停在能力、证据和适用条件层面，不作为删除Supervisor、独立Reviewer、隔离权限或改现有架构的授权。[四重点综合取舍](agent-harness-synthesis-009.md)
