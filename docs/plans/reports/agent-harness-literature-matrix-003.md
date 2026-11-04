# Agent Harness 文献—发现对照表

研究截止：2026-11-01。单AI定向范围比较，尚未人工复核。30篇登记文献，28篇定向审阅正文、2篇仅摘要背景；不是穷尽系统综述。

[综合判断与贡献定位](agent-harness-literature-003.md)。本表的“差异”是有限阅读后的定位，不是全球新颖性证明。arXiv未核实发表状态的材料按预印本处理；作者注明接收与独立确认同行评审分开。

| 文献 / 使用版本 | 方法、已覆盖内容 | 对本研究的影响 / 证据限制 |
|---|---|---|
| [R01 Why Do Multi-Agent LLM Systems Fail?](https://arxiv.org/abs/2503.13657v3)<br>2503.13657v3；正文定向 | 多框架执行轨迹、grounded theory分类与干预；含角色违背、反复步骤、上下文丢失、停止与验证错误。<br>**覆盖：**直接覆盖职责混淆、流程重复和验证不完整；已有14类taxonomy，不能重命名为新的失败分类。 | 我们的补充应是历史源码中的具体契约冲突和跨Plan结果追踪。<br>**限制：**框架/任务和Judge选择限制推广；部分结构干预有效，不能据此断言所有复杂结构更差。 |
| [R02 An Empirical Study of Harness Design for Coding Agents](https://arxiv.org/abs/2609.20804v1)<br>2609.20804v1；正文定向 | 固定执行循环，比较planning、动作接口、context配置；四模型、两基准、176配置。<br>**覆盖：**直接覆盖组件依赖模型/预算的作用；规划并非必然浪费，上下文压缩/召回收益也不一致。 | 生产规则冲突区别于组件效应；可借鉴固定其他变量的实验方式。<br>**限制：**规划/动作接口只在指定context条件比较；动作接口同时改变状态追踪和验证，不能解释成单独工具数量效应。 |
| [R03 Understanding Agent-Reactive Bugs at the Model-Harness Boundary: An Empirical Study of LLM Agent Issue Reports](https://arxiv.org/abs/2607.15684v1)<br>2607.15684v1；正文定向 | 四开源项目255个Agent-Reactive问题，双人编码症状、触发行为和修复目标。<br>**覆盖：**直接覆盖模型—harness相互作用、silent error和归因争议。 | 将建议动作与实际权限的矛盾落实为可回放生产片段；不宣称首次发现边界bug。<br>**限制：**issue是用户报告且可缺上下文；修复归属不等于已证明根因。 |
| [R04 Measuring Agents in Production](https://arxiv.org/abs/2512.04123v4)<br>2512.04123v4；正文定向 | 20访谈案例与86个部署系统的从业者调查，描述架构、评价及部署挑战。<br>**覆盖：**生产系统偏向可控流程已有实证；不应宣称首个生产Agent研究。 | 本研究可提供同一系统跨时间、数据库与代码关联的细节，而非更广泛的部署代表性。<br>**限制：**网络/参与选择与时间偏差；访谈实践偏好不能证明简化架构的因果收益。 |
| [R05 Failure as a Process: An Anatomy of CLI Coding Agent Trajectories](https://arxiv.org/abs/2607.09510v1)<br>2607.09510v1；正文定向 | Terminal-Bench三scaffold七模型，3843收集轨迹筛至1794；标注失败起点、演化和恢复。<br>**覆盖：**直接覆盖错误诊断后无效修复、重复动作、成功中仍有错误以及终局自报不可信。 | 延伸至跨管理对象重建的交付过程；不能宣称首次过程性失败分析或恢复成本研究。<br>**限制：**排除不完整/异常终止，主分析单位是benchmark trajectory；所谓不可恢复是轨迹内经验判断，非所有反事实不可恢复。 |
| [R06 From Failed Trajectories to Reliable LLM Agents: Diagnosing and Repairing Harness Flaws](https://arxiv.org/abs/2606.06324v2)<br>2606.06324v2；正文定向 | HarnessFix以HTIR关联trace数据/控制流和实现锚点，聚合缺陷、限定修复并回归验证；四基准。<br>**覆盖：**直接覆盖跨层契约、失效证据、局部修复、保留部分产物与验证；与我们的主线高度重叠。 | 贡献不能只是trace+code+artifact；需要生产目标谱系中的判定改变及新增可复现缺陷。<br>**限制：**依赖LLM诊断及可用trace；作者报告的修复收益不自动迁移到本系统，也未由本研究复验。 |
| [R07 Trial by Trace: Benchmarking AI Agent Harnesses for Reliability and Failure Attribution](https://arxiv.org/abs/2610.32285v1)<br>2610.32285v1；正文定向 | TraceTrial-Bench将任务结果与harness可靠性分开，以原生trace、外部状态和产物形成分层证据；95任务×4harness×9模型。<br>**覆盖：**直接覆盖成功与harness故障并存、证据权威、执行父子谱系；31.1%是作者特定评测结果。 | 我们不能声称首创分层结果或谱系；剩余候选是跨Plan/验收契约版本的真实交付归并。<br>**限制：**一cell一次选定执行、针对性任务和配置差异限制因果及外推；candidate link不等于已验证传播。 |
| [R08 Why Do Agent Teams Fail Across Boundaries? A Failure Taxonomy for Agent-to-Agent Systems with Private States](https://arxiv.org/abs/2610.19342v2)<br>2610.19342v2；正文定向 | 私有状态、不同principal的A2A运行；环境状态、产物正确性、行为分别评价。<br>**覆盖：**直接覆盖聚合成功掩盖协作/验证失败；私有上下文需要边界而非无条件共享。 | Agent Center同owner下的角色边界与跨组织principal不同，应明确场景边界。<br>**限制：**特殊场景与隔离环境；不能外推所有命名身份都妨碍共享。 |
| [R09 Don't Blame the Large Language Model: How Agent Harness Evolution Shapes Coding Agent Quality](https://arxiv.org/abs/2607.03691v2)<br>2607.03691v2；正文定向 | 固定自托管模型，35个Qwen Code发布版本、50个SWE-bench任务，每任务两次。<br>**覆盖：**harness软件演化影响质量已有纵向设计；与运行中DAG改图不同。 | 生产研究必须分版本，不能把年代不同的无generation/多generation样本作消融。<br>**限制：**单harness/固定模型与任务抽样，版本同时改变多个组件；不是全部组件的独立因果实验。 |
| [R10 Recursive Agent Harnesses](https://arxiv.org/abs/2606.13643v1)<br>2606.13643v1；正文定向 | 完整harness递归子调用，在Oolong长context任务比较。<br>**覆盖：**层级和隔离context可有正向效果，反驳普遍反层级论。 | 我们需要研究在哪些契约/任务条件下分层失败，而非只数层级。<br>**限制：**单一主基准；论文明确缺少详细成本及递归深度等消融；不能当软件交付上的直接效果。 |
| [R11 AFlow: Automating Agentic Workflow Generation](https://arxiv.org/abs/2410.10762v4)<br>2410.10762v4；正文定向 | AFlow在代码表示的工作流空间用MCTS搜索，验证集反馈后测试。<br>**覆盖：**工作流结构优化和反馈有效已有研究。 | 区别离线模板优化与生产同一目标中的运行时修复，避免evolution同名误比。<br>**限制：**研究数值可评任务；搜索资源与推理资源需分别核算。 |
| [R12 Automated Design of Agentic Systems](https://arxiv.org/abs/2408.08435v2)<br>2408.08435v2；正文定向 | ADAS/Meta Agent Search用不断增长的设计档案生成和评价Agent代码。<br>**覆盖：**自动设计/结构改写不是独特贡献。 | 研究运行时交付恢复，不声称提出通用自动Agent设计。<br>**限制：**搜索空间、验证任务与模型依赖；设计搜索不等于生产状态迁移。 |
| [R13 A Dynamic LLM-Powered Agent Network for Task-Oriented Agent Collaboration](https://arxiv.org/abs/2310.02170v2)<br>2310.02170v2；正文定向 | DyLAN以贡献排序选择团队，并在推理时调整通信网络。<br>**覆盖：**直接覆盖少而合适的agent、角色组合与动态通信。 | 名字效应不能从团队选择结果推导；我们可观察角色契约是否与权限一致。<br>**限制：**较早模型/函数与推理基准，非长周期软件产品验收。 |
| [R14 CodeDelegator: Mitigating Context Pollution via Role Separation in Code-as-Action Agents](https://arxiv.org/abs/2601.14914v1)<br>2601.14914v1；正文定向 | CodeDelegator用持久Delegator与新建Coder隔离调试context并保留结构化产物；τ²-bench/MCPMark评价。<br>**覆盖：**与Supervisor+executor很接近的结构可获益；隔离context不等于坏设计。 | 候选解释应收窄为契约不一致/信息接口失配；名字、角色、session不可混在一起。<br>**限制：**同时改变角色、状态隔离和消息模式；论文结果不能单独归因角色名称或一般context共享。 |
| [R15 When "A Helpful Assistant" Is Not Really Helpful: Personas in System Prompts Do Not Improve Performances of Large Language Models](https://arxiv.org/abs/2311.10054v3)<br>2311.10054v3；正文定向 | 162种persona、4模型家族、2410个MMLU问题，比较有无persona提示。<br>**覆盖：**persona提示不稳定增益是邻接证据。 | persona修辞≠具有工具权限和验收责任的功能角色；不能用它证明功能角色无用。<br>**限制：**仅事实选择题/开放模型；不是命名持久Agent或协作交付实验。 |
| [R16 When AI gets Personal: Employee emotional responses to anthropomorphic AI agents in a virtual workspace](https://doi.org/10.1016/j.chbah.2025.100189)<br>archived publisher/author copy；正文定向 | 20名白领在虚拟工作环境与多种具身AI交互，访谈与观察定性分析。<br>**覆盖：**拟人化与连接、愉悦及挫折等情绪已有直接HCI证据。 | 可为开发者反思访谈提供维度，不能把Agent互相称赞编码为人的情绪价值。<br>**限制：**约30分钟、具身/语音/外观等捆绑；非开发者长期体验，也未隔离姓名。 |
| [R17 Lexical Anthropomorphization Influences on Moral Judgments of AI Bad Behavior](https://arxiv.org/abs/2604.25814v1)<br>2604.25814v1；正文定向 | 四实验研究语言拟人化与设计线索；Study2操纵人名/机器名/无名，评价AI出错后的道德判断。<br>**覆盖：**已有姓名操纵研究，因此不能说命名无人研究。 | 开发者情绪和自动交付是不同结果变量，现有研究不能直接回答用户假设。<br>**限制：**Study2未见相关主效应/交互不等于所有情绪或生产率效应为零。 |
| [R18 Longitudinal Study on Social and Emotional Use of AI Conversational Agent](https://arxiv.org/abs/2504.14112v1)<br>2504.14112v1；摘要背景 | 社会情绪使用的纵向探索研究。<br>**覆盖：**仅作为HCI检索范围背景，不承担姓名或开发者贡献判断。 | 若后续正式HCI研究再深读全文。<br>**限制：**本轮未做正文证据提取；不据此判断研究空白。 |
| [R19 From Question Answering to Task Completion: A Survey on Agent System and Harness Design](https://arxiv.org/abs/2606.20683v1)<br>2606.20683v1；摘要背景 | 从问答到任务完成的模型—harness综述。<br>**覆盖：**提供宽泛领域定位。 | 正文新颖性依赖直接实证文献，不能依赖综述摘要。<br>**限制：**本轮仅元数据与摘要筛读，不承担负面覆盖判断。 |
| [R20 From Static Templates to Dynamic Runtime Graphs: A Survey of Workflow Optimization for LLM Agents](https://arxiv.org/abs/2603.22386v1)<br>2603.22386v1；正文定向 | 区分模板、实例化图与trace，按结构决定时机归类工作流优化。<br>**覆盖：**动态改图、反馈机制和结构/成本报告要求均有综述覆盖。 | 使用其分类澄清evolution；向后追查DyFlow和AgentConductor原文。<br>**限制：**综述不是新干预效果证据；引用列举不等于本研究逐篇读过。 |
| [R21 When Tools Fail: Benchmarking Dynamic Replanning and Anomaly Recovery in LLM Agents](https://arxiv.org/abs/2606.05806v1)<br>2606.05806v1；正文定向 | ToolMaze在已知工具DAG解空间注入显/隐、暂时/永久扰动，分别评价完成、恢复与恢复开销。<br>**覆盖：**恢复与最终成功区分、无效重试代价已有直接基准。 | 生产中未知最短恢复路径，不能直接套其最优路径归一化指标。<br>**限制：**受控生成拓扑而非开放生产需求；恢复定义包含合理放弃，与交付成功不同。 |
| [R22 StateWise: Diagnosing and Repairing Persistent Operational State Before Agent Actions](https://arxiv.org/abs/2610.05241v1)<br>2610.05241v1；正文定向 | StateWise核实失效持久记录、限定范围保存纠正与lineage；含真实会话审计和跨任务重置评价。<br>**覆盖：**直接覆盖跨任务持久证据及纠正传播；削弱泛化的‘跨任务首次’主张。 | 区别事实/内存修订谱系与同一产品目标的Plan替换及验收契约修订。<br>**限制：**受接口契约和核验器支持范围限制；真实会话审计是candidate链，受控结果不是生产因果结果。 |
| [R23 AgentTether: Graph-Guided Diagnosis and Runtime Intervention for Reliable LLM Agent Operation](https://arxiv.org/abs/2607.06273v1)<br>2607.06273v1；正文定向 | AgentTether用轨迹图做事后诊断，再运行时监督纠正；τ-bench状态任务。<br>**覆盖：**针对根因恢复、保持纠正有效、过多guard反而伤害，都有先行证据。 | 保留真实开发流程中的恢复失败与成功例，补跨Plan权限/验收边界。<br>**限制：**辅助LLM判断与延迟成本；论文明确代码生成、多Agent及生产观测为扩展方向。 |
| [R24 LLM-as-Scheduler: Agentic Workflow Dynamic Scheduling](https://aclanthology.org/2026.acl-long.581/)<br>archived publisher/author copy；正文定向 | LLM-as-Scheduler用轻量gate和调度器决定提前退出/继续验证；代码与数学基准。<br>**覆盖：**并非每项任务需要完整验证链，按难度/输出信号分配控制成本已有直接研究。 | 应比较等价质量门禁下的必要/冗余控制，而非全面删除门禁。<br>**限制：**gate可能跨域失准；短任务结果不能直接外推发布/部署工作流。 |
| [R25 SWE-chat: Coding Agent Interactions From Real Users in the Wild](https://arxiv.org/abs/2604.20779v2)<br>2604.20779v2；正文定向 | SWE-chat公开开发会话连接checkpoint和commit，分析交互、代码留存及用户纠正。<br>**覆盖：**真实日志、用户修正、产物追踪本身已有大规模公开研究。 | 本系统的优势应是编排/验收内部状态与历史源码联结，不是规模或首个真实数据集。<br>**限制：**自愿公开日志有选择偏差；LLM会话成功评分与commit留存都不等于产品验收/部署正确。 |
| [R26 Object-Centric Process Mining: Dealing with Divergence and Convergence in Event Data](https://doi.org/10.1007/978-3-030-30446-1_1)<br>archived publisher/author copy；正文定向 | 对象中心过程挖掘已指出单一case ID扁平化会丢失多对象关联。<br>**覆盖：**多对象谱系思想本身不是新贡献。 | 可将目标、Plan、Task、artifact、verdict映射为关联对象，贡献在领域规则和验证。<br>**限制：**通用方法论文，不直接评价agent模型、验收契约或恢复有效性。 |
| [R27 Defining Cases and Variants for Object-Centric Event Data](https://arxiv.org/abs/2208.03235v1)<br>2208.03235v1；正文定向 | 将case推广为相关对象事件图，支持连通分量/leading type及真实贷款多offer案例。<br>**覆盖：**废弃后新对象承接同一目标，与既有多offer过程有方法相似性。 | 必须说明为何只按明确目标/替代证据关联；同项目不能整体合并。<br>**限制：**抽取方式决定观察粒度，连通性本身不能证明语义上目标相同。 |
| [R28 DyFlow: Dynamic Workflow Framework for Agentic Reasoning](https://arxiv.org/abs/2509.26062v1)<br>2509.26062v1；正文定向 | DyFlow的designer与executor按中间反馈生成stage subgraph并保留规划历史/输出。<br>**覆盖：**直接覆盖运行时改图与反馈、记忆延续；DAG evolution有用不是新机制。 | 生产证据可补需求/验收变化与长期失败恢复，而非提出动态图本身。<br>**限制：**受训练设计者、operator和推理基准范围约束；不是持久生产Plan状态机的评价。 |
| [R29 AgentConductor: Topology Evolution for Multi-Agent Competition-Level Code Generation](https://arxiv.org/abs/2602.17100v1)<br>2602.17100v1；正文定向 | AgentConductor根据代码执行反馈同题多轮更新分层DAG，并按难度调整密度。<br>**覆盖：**直接覆盖同实例多轮图演化及成本，不能把‘有反馈地改DAG’当空白。 | 要识别哪些前提/验收证据发生变化，而非只记录generation数量。<br>**限制：**算法训练/模型/任务受控但与生产权限、人工改需求和部署不同；不能直接移植效果量。 |
| [R30 Complex Agents, Shallow Tests: Demystifying and Enhancing Test Adequacy of Agent Harness in the Wild](https://arxiv.org/abs/2610.04921v1)<br>2610.04921v1；正文定向 | HarnessTester显式利用跨组件契约生成有效测试并覆盖LLM依赖代码。<br>**覆盖：**harness契约测试与mock保真已有直接研究。 | 下一步最小回放应作为本系统证据升级，不能仅因写了契约测试就宣称新方法。<br>**限制：**代码覆盖/变异/检出bug不等于端到端交付质量；仍需范围及回归验证。 |

## 原文定位与出版状态

- **R01**：§3–5、附录A/H/J；本地 `text/2503.13657.txt` 行 31,34,37,43-51,58-62,229-234。arXiv版本；未在本轮独立确认同行评审状态。
- **R02**：§2–4、Limitations；本地 `text/2609.20804.txt` 行 14,16,18,21,35-42,47-56,82。arXiv版本；未在本轮独立确认同行评审状态。
- **R03**：§III–VI；本地 `text/2607.15684.txt` 行 75,78-82,85-99,101-102。arXiv版本；未在本轮独立确认同行评审状态。
- **R04**：§3、5.4、6–7；本地 `text/2512.04123.txt` 行 31-40,70-71,78-83,88,92。arXiv作者注明发表/接收：Accepted to the 43rd International Conference on Machine Learning (ICML 2026) as Oral Presentation。
- **R05**：§II–III、V；本地 `text/2607.09510.txt` 行 24-31,33-47,63-75,94-96。arXiv版本；未在本轮独立确认同行评审状态。
- **R06**：§II–VI；本地 `text/2606.06324.txt` 行 22,26,39,43,49,53,61-65,79-81,90-99,111-125。arXiv版本；未在本轮独立确认同行评审状态。
- **R07**：§2–5；PDF§6.1；本地 `text/2610.32285.txt` 行 21,24-25,38,64,66,71-74,85,93,108,134,164-168。arXiv版本；未在本轮独立确认同行评审状态。
- **R08**：§2–4；本地 `text/2610.19342.txt` 行 20-31,48-50,83-89。arXiv版本；未在本轮独立确认同行评审状态。
- **R09**：§3.1–3.4；本地 `text/2607.03691.txt` 行 52-58,62-72。arXiv版本；未在本轮独立确认同行评审状态。
- **R10**：§3–5；本地 `text/2606.13643.txt` 行 16,22,25,27,36,38,43。arXiv版本；未在本轮独立确认同行评审状态。
- **R11**：§3–5；本地 `text/2410.10762.txt` 行 35,51,53,55,59,61,65,68。arXiv版本；未在本轮独立确认同行评审状态。
- **R12**：§3、4、6；本地 `text/2408.08435.txt` 行 17-19,22-35,42-44。arXiv版本；未在本轮独立确认同行评审状态。
- **R13**：§3–4；本地 `text/2310.02170.txt` 行 20-21,40-45,49-57,72,85-86。arXiv作者注明发表/接收：Published in COLM2024. Code Repo: this https URL。
- **R14**：§3–5；本地 `text/2601.14914.txt` 行 19-24,28-43,56,68,73,75,81,85,89-91。arXiv版本；未在本轮独立确认同行评审状态。
- **R15**：§3–4、8–9；本地 `text/2311.10054.txt` 行 18-24,30-35,65,69。arXiv作者注明发表/接收：Accepted by Findings of EMNLP 2024。
- **R16**：§4.2–4.3、5–6；本地 `text/hci-workplace-raw.txt` 行 419-451,479-495,714-726,751-763,1024-1043。出版记录与原文已核对。
- **R17**：§2–3，Study2；本地 `text/2604.25814-raw.txt` 行 24-36,185-205,244-254,410-430。arXiv版本；未在本轮独立确认同行评审状态。
- **R18**：摘要；本地 `text/2504.14112.txt` 行 2。arXiv版本；未在本轮独立确认同行评审状态。
- **R19**：摘要；本地 `text/2606.20683.txt` 行 1-2。arXiv版本；未在本轮独立确认同行评审状态。
- **R20**：§2、4、6；本地 `text/2603.22386.txt` 行 30,40,49,53-55,87-89,110-119。arXiv版本；未在本轮独立确认同行评审状态。
- **R21**：§3、Limitations；本地 `text/2606.05806.txt` 行 23-35,43,48,50-55,58-59,88-90。arXiv版本；未在本轮独立确认同行评审状态。
- **R22**：§1、3–5；本地 `text/2610.05241.txt` 行 5-15,64-69,96-105,145-154。arXiv版本；未在本轮独立确认同行评审状态。
- **R23**：§II–V；本地 `text/2607.06273.txt` 行 21-26,28-39,47-58,66-74,93,95-101。arXiv版本；未在本轮独立确认同行评审状态。
- **R24**：§1、5.1、8；本地 `text/scheduler.txt` 行 14-41,377-400,599-610。出版记录与原文已核对。
- **R25**：§2–5、Ethics；本地 `text/2604.20779.txt` 行 17-25,42,45-47,65,67,74,83。arXiv作者注明发表/接收：Accepted at COLM 2026。
- **R26**：摘要/§1；本地 `text/ocpm.txt` 行 13-36,39-55。出版记录与原文已核对。
- **R27**：§IV、VII；本地 `text/2208.03235.txt` 行 32-49,82-83。arXiv版本；未在本轮独立确认同行评审状态。
- **R28**：§3–4；本地 `text/2509.26062.txt` 行 21-25,28-34,49-51,73-74。arXiv版本；未在本轮独立确认同行评审状态。
- **R29**：§2–3；本地 `text/2602.17100.txt` 行 19-25,37,40,44,65,67,72-75。arXiv版本；未在本轮独立确认同行评审状态。
- **R30**：§3、6、9；本地 `text/2610.04921.txt` 行 27-34,64-67,69,92。arXiv版本；未在本轮独立确认同行评审状态。

R07另核查同版本PDF §6.1（`text/tracetrial-pdf.txt:1349–1353`）。完整原文、摘要页、下载URL/时间/哈希、作者元数据、提取卡片及BibTeX保存在[本地文献研究目录](/Users/oopslink/works/research/agent-harness-study/run-001/literature-003/README.md)。本地行号对应冻结转换文本，不是出版页码。
