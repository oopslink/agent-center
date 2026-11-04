# 五篇论文的实质效度修订（016）

**本轮发现的是研究设计限制，不只是表达问题。** 五篇已完成与现有证据一致的修订；实际架构效果、长期自治和三层记忆收益仍未验证，全部研究尚未人工复核。保留五个研究方向，不声称已经形成五篇可以定稿的独立研究。

## 改了什么

- 收窄研究问题和中英文题名，突出本部署中可追溯的责任外置、行动关系缺口、信息遗漏、谱系错配与规则适用冲突。
- 将综述改为已有机制、评价对象和本地证据的比较；补协作基础与五项记忆工作，纠正研究语境/出版身份，保留严格版本核验。
- 给每篇加入事后分析框架，把构念、可观察判据、反例与正文发现连接起来。
- 全量核查1280个原实验主动作输入，将共同给定的原则与真正被操纵的变量分开；不再将短题遵循解释为长期架构验证。
- 保存11项核心主张的证据、反证及替代解释，映射24幅图的支持范围；原38张主文表与原数值保留。没有新增模型或生产实验，也没有为凑定量结果新增p值。

## 当前能说到哪里

| 工作稿 | 本地证据支持的结论 | 关键未解决研究 |
|---|---|---|
| [P1](p1-logical-agent-identity/paper.md) | 统一逻辑归属未稳定保持失败后的承责，多次人工提醒后仍有残留 | 抽象、实际架构、恢复入口与训练先验的独立作用 |
| [P2](p2-workflow-responsibility/paper.md) | 已有职责没有在选定交接中落实为明确动作、对象和合同；有效验收仍有价值 | 实际流程/交接复杂度对停摆、人工介入及质量的效应 |
| [P3](p3-role-shared-context/paper.md) | 命名成员已共享；固定生产者角色过滤能遗漏必要事实；两套标签直接处置相同 | 无名对照、真实检索器、独立团队体验研究 |
| [P4](p4-dag-evolution/paper.md) | 改图承载变化义务，产物与验收连续性需另行重建 | 相同初态下改图、续修、重规划的净质量与成本比较 |
| [P5](p5-memory-applicability/paper.md) | 可达的Team规则仍会因缺阶段条件与中间合同冲突 | 同任务规则条件化干预、跨会话复用、私有层历史 |

这不是对七类问题全部“验收通过”。[逐项审计](research-validity-016/README.md)区分已修正的论述与仍缺证据的实质问题；[后续方案](research-validity-016/next-studies.md)均未执行。

## 过程与保留

每篇保存REVISION-016.md及从015工作稿起算的revision-016.diff；旧修订与原始材料不覆盖。[主张表](research-validity-016/claim-ledger.md)、[方法审计](research-validity-016/methods-review.md)、[文献核查](research-validity-016/literature/references-audit.json)、[P5文献核查](research-validity-016/literature/p5-evidence.json)及[IEEE重编号](research-validity-016/citation-renumbering.json)可逐项查看。

完整网页检索记录、定点阅读、引用载体差异、初稿快照和最终校验保存在[本地过程档案](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/016-research-validity/README.md)。原论文PDF只保留于本地核验档案，不复制到仓库。引用核验为有范围检查，既不声称系统综述，也不声称穷尽撤稿数据库。

## 方法与软件披露

本轮应用[academic-paper技能](/Users/oopslink/.codex/skills/academic-paper/SKILL.md)的revision流程及[literature-review技能](/Users/oopslink/.agents/skills/literature-review/SKILL.md)进行有范围的叙述性比较，使用分工AI作方法审计与定点文献核查。分工审阅是同一AI辅助流程的一部分，不属于独立人工编码或重复实验。安装环境缺少的官方ARS/缩写辅助程序没有被宣称为已运行；实际检查采用保存的脚本、差分与SHA。

literature-review技能对过程组织有实质使用，按其软件披露要求引用：T. Kassis, V. Agarwal, Y. He, D. Patel, and A. M. Brueckner, “Scientific Agent Skills: A Library of Procedural Knowledge for Research Agents,” arXiv:2609.00065, 2026, doi: [10.48550/arXiv.2609.00065](https://doi.org/10.48550/arXiv.2609.00065)。该引用仅说明工具来源，不提供本组论文有效性或技能收益的证据。

案例研究方法参考Runeson与Höst的[原始方法论文](https://doi.org/10.1007/s10664-008-9102-8)，用于区分情境解释、效度与可复现范围，不意味着当前研究已满足全部方法要求。作者统一oopslink，无资助。本轮未提交或推送Git。
