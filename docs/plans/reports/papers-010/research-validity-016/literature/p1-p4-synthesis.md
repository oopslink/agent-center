# P1–P4：按研究问题组织的文献核对与定位（016）

核对日期：2026-11-03（America/Los_Angeles；最后核对时间 2026-11-04 07:32 UTC）。执行者：AI 文献审阅分工；未人工复核。对象：当前四稿的 23 个参考条目、去重 16 篇，另补 3 篇基础工作。此处是有针对性的原始文献比较，不是系统综述；没有覆盖率、穷尽性或首次创新保证。

## 1. 核对结论及处理优先级

16 篇既有引用均找到匹配的原始出版记录或作者预印本，未发现来源不存在。实际读取的原始记录未显示撤稿、撤回或关注声明；这不是对所有外部数据库的无撤稿认证。引用是否存在、主张是否获支持、研究能否定稿是三个独立问题。逐项元数据、读取范围和边界见 [references-audit.json](references-audit.json)。

需要整合的修改：

| 项目 | 当前问题或易误读点 | 建议 |
|---|---|---|
| P1／CodeDelegator | 把持续承责写得像新提出的机制 | 其 §4.1 已让长期 Delegator 在 Coder 结束后继续推进、重试或重规划。P1研究的是这种责任在本部署为何未稳定落实及人工补偿，不是首次提出监督者应恢复任务 |
| P2／MAP | 继续只标为 2025 预印本；可能混淆样本分母 | 正式版本为 ICML 2026，PMLR 306:95649–95695；306 是全问卷，主文筛选 86 个部署/试点系统，另有 20 个访谈案例。[正式记录](https://proceedings.mlr.press/v306/pan26a.html) |
| P3／Banks 等 | “情景式道德判断”容易被理解为仅阅读情景 | 改为“受控 LLM 作诗互动中的道德判断”；研究确实有命名操纵，但没有开发者团队体验终点 |
| P3／persona | 容易把 persona 无稳定问答收益当名字无用 | Persona 描述、显示名、职责指令、权限和信息路由是不同干预；不能互相替代 |
| P4／AFlow | 若写成现有方法无法处理开放任务，将不成立 | 附录 F 有开放任务与 LLM 评分。区别应是工作流优化单位与生产目标/产物/验收谱系不同 |
| 跨稿／Lost in the Middle | 反复充当通用“认知过载”理论 | 只作为信息位置与简单题天花板的比较，不解释本系统内部注意力机制 |

未改四稿或 Bib 文件；此文件提供可整合建议。正式排版前仍应复查最终引用编号与条目对应。OpenReview 的直接访问遇到验证页；相关主张使用可读的作者原始 arXiv 正文，未假装读到被挡住的出版 PDF。R26 只读出版社摘要，其用途必须保持一般性。

## 2. 已有研究覆盖了什么

| 研究问题 | 原始工作已覆盖 | 本组研究仍能回答 | 当前不能声称 |
|---|---|---|---|
| 执行分离后谁承担失败恢复 | CodeDelegator 已把执行隔离与持续控制责任结合；MAST 已区分角色、协调与终止问题 | 在一个长期部署中，逻辑归属如何未被转化为持续承责，人工补充什么关系，正例为何不同 | Supervisor–Executor 普遍有害；失败由某种训练结构引起 |
| 多角色流程为何停摆 | AutoGen 明确分开角色能力配置与会话控制；MetaGPT/ChatDev 联合设计职责、结构化产物、交接和反馈；MAST 已有失败分类 | 本地失效发生于哪条当前行动关系缺失，哪些人工介入补全它，哪些检查确实增加证据 | 角色越多越差；复杂度净因果效应；所有检查都是扯皮 |
| 名称与上下文如何区分 | Persona 问答研究与命名互动研究使用不同干预和终点；MetaGPT 早已有共享池与相关消息订阅 | 区分稳定标识、显示名、职责与信息选择，展示按生产者角色过滤如何漏掉消费者所需证据 | 全局“名字不需要”；名字造成上瘾；现有实验已证明按角色共享最佳 |
| 改图是否带来交付收益 | AFlow 搜索工作流；DyFlow 动态阶段规划；AgentConductor 题内反馈拓扑；对象中心过程研究连接多个业务对象 | 解释生产改图与代码修复、信息补充、合同变化如何共同发生，产物可继承范围如何改变判断 | 新的图优化算法；generation 次数等于收益；对象连通即目标等价 |

这些局部问题不必包装成“填补国际空白”。实际价值在于把已经可预见的设计风险落实为可审计的失败条件和可执行的修正要求。文献提供竞争方案与反例；本地证据负责证明本部署发生了什么。

## 3. 可整合进正文的主题段落

以下是建议文字，引用编号由主稿统一分配；不是新增实证结果。

### P1：分离执行与持续承责

执行分离已有明确的正向设计依据。CodeDelegator 用长期规划者组织短期执行，并将执行后的继续推进、重试和重规划仍交给长期控制者。[CodeDelegator §4.1](https://arxiv.org/html/2601.14914v1) 因此，问题不在于监督者与执行者是否必须处于同一进程，而在于执行失败后控制责任是否仍连续。本文的部署材料考察统一逻辑归属为何没有稳定实现这种连续性，以及人工提醒如何补回缺失的责任关系；它不能据此否定执行隔离本身。

角色规范、协调错位和提前终止已有失败分类。MAST 的干预既有正向结果，也存在随模型变化而不显著的情形，说明同类设计不能脱离运行条件评价。[MAST §4 与附录 H](https://arxiv.org/html/2503.13657v3) 本文的独特材料是长期维护中反复出现的承责纠偏及其后续行为，而不是新的失败大类。AgentTuning 和 Instruction Hierarchy 只能说明行为可能受训练塑造；它们不包含本部署模型的训练记录，不能把这里的责任外置归因于训练内化的组织结构。[AgentTuning](https://aclanthology.org/2024.findings-acl.181/)，[Instruction Hierarchy](https://arxiv.org/html/2404.13208v1)

### P2：角色描述不足以构成可执行交接

多角色协作的基础框架并未把角色名称当作完整流程。AutoGen 分别配置可交互主体与会话控制，允许用代码规定终止、工具执行和人类介入条件。[AutoGen §2](https://arxiv.org/html/2308.08155v2) MetaGPT 把角色职责与结构化产物、消息订阅及执行反馈结合；ChatDev 则组织阶段通信并要求针对具体缺口补充信息。[MetaGPT §3](https://arxiv.org/html/2308.00352v7)，[ChatDev §3](https://aclanthology.org/2024.acl-long.810.pdf) 这些工作说明，增加分工的同时必须落实对象、输入和控制转换，不能仅凭角色较多断言机制低效。

本部署材料据此检验更窄的问题：职责已经写明时，当前任务的行动者、交付对象、依据和后继动作是否仍需人工补全。有效独立检查属于反证，因为它可能补出之前缺失的验收证据。受约束流程在生产部署中也很常见，相关调查不能证明流程复杂度的因果效果，却足以反对“一切流程都应删除”的建议。[MAP](https://proceedings.mlr.press/v306/pan26a.html)

### P3：名称、任务角色与信息路由不能合并为一个变量

与本文最相关的角色研究实际改变了任务指令和通信机制。MetaGPT 的角色配置同时包含目标、约束、工具与订阅信息；ChatDev 的角色消融删除了程序员和审查者的职责提示，不能解释为仅去掉显示名。[MetaGPT §3.1–3.2](https://arxiv.org/html/2308.00352v7)，[ChatDev 消融](https://aclanthology.org/2024.acl-long.810.pdf) 因此，本文将名称作为寻址及界面呈现，角色作为任务契约，信息选择作为单独机制，分别评价其作用。

Persona 提示在问答中未显示稳定普遍收益，并不证明开发者不需要可读名字。[Persona 研究](https://aclanthology.org/2024.findings-emnlp.888/) Banks 等在受控作诗互动中操纵人名、机器名和无名，Study 2 的道德判断未检出命名主效应或交互；其终点不包含团队感、掌控感或长期使用体验。[命名研究 §3.1–3.2](https://arxiv.org/pdf/2604.25814) 本文的一人回顾因此只能报告一种产品体验，不能将它归因于名字。自动交付部分则应考察证据是否送达承担当前职责的消费者，而不是仅按信息生产者的角色分类。

### P4：图优化与交付谱系重建回答不同问题

现有动态图方法的比较需要先对齐优化单位。AFlow 在给定任务及评价函数下搜索代码表示的工作流；DyFlow 在执行过程中形成阶段子图；AgentConductor 根据同一题的反馈重组协作拓扑。[AFlow §3](https://arxiv.org/html/2410.10762v4)，[DyFlow §3](https://arxiv.org/html/2509.26062v1)，[AgentConductor](https://arxiv.org/html/2602.17100v1) 它们构成改图可能有效的正面证据，却没有使生产计划的 generation 自动成为可比较的效果单位。AFlow 也探索开放任务，因而本文的差别不是“是否开放”，而是目标、产物和验收是否跨版本延续。

对象中心过程研究提供了关联多个对象的分析依据，避免将每个 Plan 独立解释；但对象相连仍不等于业务目标相同。[对象中心过程挖掘](https://link.springer.com/chapter/10.1007/978-3-030-30446-1_1)，[Cases and Variants §IV](https://arxiv.org/html/2208.03235v1) 本文需要独立核对目标连续性、产物版本和验收范围，再判断改图后的推进究竟来自新增路径、代码修正、人工信息还是目标变化。贡献是对生产改图效果的分解和审计边界，当前材料不支持单独估计改图的净因果收益。

## 4. 三篇补充基础工作的引用建议

优先保持与实际读取版本一致。AutoGen 和 MetaGPT 的正文已核作者 arXiv 版本，引用时明确为预印本；搜索另发现 AutoGen 的 COLM 2024 出版首面，但标题及版本有所差异，勿把元数据拼接。ChatDev 使用正式 ACL 版本。可仅将 AutoGen 用于 P2，MetaGPT/ChatDev 用于 P2/P3，避免四稿机械重复。

- Q. Wu et al., “AutoGen: Enabling Next-Gen LLM Applications via Multi-Agent Conversation,” arXiv:2308.08155v2, 2023, doi: [10.48550/arXiv.2308.08155](https://doi.org/10.48550/arXiv.2308.08155).
- S. Hong et al., “MetaGPT: Meta Programming for A Multi-Agent Collaborative Framework,” arXiv:2308.00352v7, 2023, revised Nov. 2024, doi: [10.48550/arXiv.2308.00352](https://doi.org/10.48550/arXiv.2308.00352).
- C. Qian et al., “ChatDev: Communicative Agents for Software Development,” in *Proceedings of the 62nd Annual Meeting of the Association for Computational Linguistics (Volume 1: Long Papers)*, 2024, pp. 15174–15186, doi: [10.18653/v1/2024.acl-long.810](https://doi.org/10.18653/v1/2024.acl-long.810).

BibTeX 建议另存 [foundational-candidates.bib](foundational-candidates.bib)，尚未加入论文库。既有 MAP 可更新为正式会议条目，阅读定位保留 arXiv v4；AFlow 可保留已经核读的 v4，以免在受访问限制时声称已经完整核读最终会议版。

## 5. 检索与读取可追溯性

- **检索范围：**先按四稿既有题名/标识打开 arXiv、ACL Anthology、Springer 原始记录，再读取主张对应正文；补查 AutoGen、MetaGPT、ChatDev；按准确题名检索 OpenReview/PMLR 出版状态。
- **记录方式：**[本地文献档案](/Users/oopslink/works/research/agent-harness-study/run-001/papers-010/revisions/016-research-validity/literature)中的 `*-web.json` 保存工具返回文本及原始 URL、打开/查找参数和本轮工具定位。它们是检索快照与摘取结果，不是论文全本的永久副本。每篇实际阅读范围见 JSON，不能把页面“总行数”当已阅读全文。
- **期刊通知补检：**对本组唯一期刊论文 Lost in the Middle 使用 `site:retractionwatch.com "Lost in the Middle" "Liu"` 和 `"10.1162/tacl_a_00638" (retraction OR correction OR "expression of concern")`。未获得匹配的原始撤稿/更正通知；第三方聚合站的完整性结论不作为证据；未下载 Retraction Watch 全量数据库。
- **访问缺失：**OpenReview forum/PDF 直接打开为验证页；R26 正文订阅受限。它们不影响已核的 arXiv 主张和 R26 摘要范围的一般概念，但不能据此宣布全部出版史与通知均已穷尽。
- **结果边界：**没有新文献效应量合并、统计重算、产品实验或人工复核。此处的正反证据用于限定本地解释，不把外部基准当本地复现实验。

