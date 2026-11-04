# 五篇论文的行文修订与核验（022）

五篇论文分别以身份边界、交接依赖、任务信息需求、计划修复和规则适用性为主线。各篇通过方法说明比较对象和判据，在结果中呈现观察及其反例，在机制分析中评价竞争解释，随后提出具体设计并说明依据，最后回答研究问题。[五篇研究对照表](../agent-harness-papers-010.md)可横向比较研究方法、问题、成因、方案和结论。

| 论文 | 编辑决定 | 保留的判别条件或反证 |
|---|---|---|
| [P1：身份归属](p1-logical-agent-identity/paper.md) | 用目标负责人、发言会话与执行句柄区分“我是 Supervisor”和“交付责任已转移”。 | 明确外置、合理升级和未生效的分派请求分开。修复任务先于身份提醒建立的案例不能证明提醒引发了修复。 |
| [P2：流程复杂度](p2-workflow-responsibility/paper.md) | 以真实交接、可核验回执和预算解释停摆，区分实现合格与完成集成。 | 报告双方均成功的 11 对及停摆例；独立检查发现新缺陷的部署案例仍然保留。 |
| [P3：名字与上下文](p3-role-shared-context/paper.md) | 按命名条件、凭据版本与处置结果报告，区分合规请求补证和正式判定。 | 三种命名条件均保留稳定 ID。开发者的掌控感是单人自述，不能作为交付指标。 |
| [P4：任务图演化](p4-dag-evolution/paper.md) | 分别追踪工作安排、代码成果和验收要求，以发生顺序界定对改图的归因。 | Mermaid 的修复报告先于改图；重复创建且废弃的五个节点证明有限重复，不能证明无限循环。 |
| [P5：团队记忆](p5-memory-applicability/paper.md) | 以谁写入、谁可读取、哪些规则被任务采用来说明架构和效果。 | 阅读回执不代表正确采用；宽规则条件中一次阻断与三次继续均报告。 |

正文中的版本增量与研究讨论用语改为可独立理解的实验条件或案例名称；两种语言的摘要和结论按相同证据表述。精确时间仍见[事件时间表](case-timestamps.md)和各篇证据附录。会影响效度的信息（仪器缺陷及整批排除、事后设计、判据歧义、缺少人工复核）仍保留在方法或局限中。没有采集新样本，没有修改原始响应、统计底表、图件或参考文献。

本地档案：[修订前后快照、差分、编辑脚本与核验说明](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/022-standalone-prose/README.md) · [原版本附记与 AI 参与记录](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/022-standalone-prose/removed-editorial-notes.md) · [核验清单](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/022-standalone-prose/validation.json)。
