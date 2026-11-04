# 五篇研究问题与结论修订（018）

本轮按作者对研究对象的更正，修订五篇的题名、中英文摘要、引言、讨论或结论以及[对照总表](../agent-harness-papers-010.md)。研究依据为既有现场记录和已完成的对照；没有新增案例、模型调用或新的人工复核。原始输出、失败例、明显系统故障排除与仪器缺陷记录仍按原版本保存。

| 稿件 | 解释发生的变化 | 可核查的资料与仍未识别的问题 |
|---|---|---|
| [P1](p1-logical-agent-identity/paper.md) | 以“Agent 被缩小为 Supervisor、executor 被排除在外”作为待解释的早期现象；把失败后的责任外置改为行为判据，区分执行隔离、监督口吻、接口语义与训练先验。 | [早期纠偏与后期反例](p1-logical-agent-identity/longitudinal-analysis.md)；[多轮对照](runtime-experiments-017/README.md)。两种表述均能恢复，无法估计现场频率，程序化返回仍是待验证干预。 |
| [P2](p2-workflow-responsibility/paper.md) | 以同一目标下新增交接的代价与质量收益为主问题；区分规则写法、新的独立检查、重复确认，并解释固定启动预算怎样放大认可分歧。 | [全部条件与原始轨迹](runtime-experiments-017/README.md)；[唯一停摆例的回执核查](runtime-experiments-017/review/receipt-boundary-case.json)。每格三个小任务，不能由一次停摆估计发生率。 |
| [P3](p3-role-shared-context/paper.md) | 明确全自动 Agent 不应设个人名这一设计结论。机器 ID、权限、角色需要的上下文和开发者界面分别处理。 | [标签与信息选择对照](p3-role-shared-context/controlled-experiments.md)；[现场与单人自述](p3-role-shared-context/evidence-details.md)。两种标签均非无名组，未测去名对实际交付率的影响。 |
| [P4](p4-dag-evolution/paper.md) | 从改图的归因进一步回答怎样根据反馈推进长程工作；新增 §5.5，对照局部复验、重复 G9、旧基线退步和外部等待，并提出停止条件。 | [案例、Git 关系与重复节点](p4-dag-evolution/evidence-details.md)。五个废弃节点是有限重复，不提供无限循环或系统 bug 的根因证据；防循环规则未经干预验证。 |
| [P5](p5-memory-applicability/paper.md) | 将规则冲突放回团队记忆架构的整体评价：分别分析私有写入、项目作用域、团队发布、跨成员读取与实际采用。 | [记忆实现与作用域](../agent-harness-memory-implementation-011.md)；[Git 与部署补查、正反案例](../agent-harness-memory-study-011.md)。Team 传播与个案采用可核对，私有／项目长期复用率和各层净收益不可比较。 |

五篇的原图表、参考文献、证据附录、无资助及作者声明保留。正文计数字段随新稿更新，不据此认定投稿就绪。[修订前后快照、逐行差分与核对记录](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/018-research-questions/README.md)。
