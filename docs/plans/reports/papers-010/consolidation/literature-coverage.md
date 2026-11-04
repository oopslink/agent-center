# 文献覆盖与核验（Task 2 产出）

## 1 两篇的参考文献规模

| | 现有（五篇初稿去重） | 新增 | 合计 |
|---|---:|---:|---:|
| A 篇｜现场研究 | 30 | 25 | **55** |
| B 篇｜受控实验 | 15 | 24 | **39** |

五篇初稿的 `references.bib` 共 42 条、去重后 30 个唯一 key、0 个重名冲突。初稿各篇只有 7–10 条，是设计稿 §8 第 15 项登记的缺陷。

主题到论文的分配：

| 主题 | 新增条数 | 进 A 篇 | 进 B 篇 |
|---|---:|:---:|:---:|
| 1 失效分类／协调错位／可靠性实证 | 5 | ✓ | ✓ |
| 2 计划修复／复用／重规划 | 6 | ✓ | |
| 3 组织记忆／知识治理／规则适用范围 | 8 | ✓ | |
| 4 harness／工具接口边界／责任与权限 | 6 | ✓ | ✓ |
| 5 工作流拓扑／角色分工／协调开销 | 6 | | ✓ |
| 6 证据／凭据／来源认可 | 7 | | ✓ |

主题 1 与 4 进两篇：A 篇用于定位现象与责任表达，B 篇用于论证"必须在真实运行器上做实验、且必须固定 harness 版本"（`sghaier2026dont`）与凭据—执行绑定失效（`wang2026approval`）。参考文献重叠不违反证据不重叠要求——后者只针对原生部署记录。

## 2 核验

检索与初次核验由一个子agent 执行，**其结论未被直接采信**；下列复核由主执行者独立重做：

| 复核项 | 方法 | 结果 |
|---|---|---|
| 24 条 arXiv 预印本 | 一次批量 `export.arxiv.org/api/query?id_list=…`，逐条比对 bib 标题与 arXiv 返回标题的前 45 个字符（去标点、小写） | **24/24 命中且标题一致** |
| 14 条有出版社 DOI 的条目 | 逐条 `api.crossref.org/works/<doi>`，比对标题前 40 字符与年份 | **14/14 命中**；1 条标题"不符"为 Crossref 把副标题单列（`sadowski2018modern` 的 `Modern code review` + 副标题 `a case study at Google`），bib 的合并写法正确 |
| 撤稿状态 | 子agent 报告 38 条 OpenAlex `is_retracted` 全为 false | 未独立重跑；**若要作为公开主张，需在投稿前复核一次** |

**核验口径的必要偏离（经确认成立）：** arXiv 自发 DOI（`10.48550/arXiv.*`）注册在 DataCite 而非 Crossref，`api.crossref.org` 对其一律返回 404（用已有的 2503.13657 实测确认）。因此预印本改走"arXiv API 取元数据 + OpenAlex 取撤稿状态"双源核验。这不是放宽标准，而是换成该类标识的正确注册方。

**舍弃而未编造的条目：** Fox et al. 2006（ICAPS 论文集无 DOI）、van der Krogt & de Weerdt 2005 ICAPS 原文、Hammond 1989 专著。三条均因查不到可核验出版记录而舍弃，改用主题等价且有 DOI 的替代（`babli2023plan`、同作者 LNCS 2005 版、`veloso1993derivational`）。宁缺勿假。

## 3 仍存在的缺口

- **主题 6 的人类基线偏薄。** 只有 `sadowski2018modern` 一条实证研究讲人类评审者如何依赖自动化证据作接受决定；B 篇若要把模型行为与人类实践对照，这一条承载过重。
- **主题 2 的 LLM 侧只有一条直接可比的实证（`yuan2026replan`）。** 经典规划文献充足，但"已承诺计划遭扰动后怎么改"在 LLM agent 侧的实证基线稀少，A 篇 §4.4 的定位需明示这一点。
- **arXiv 预印本占比高（24/38）。** 2026 年的预印本尚未经同行评审，A/B 两篇引用时应注明预印本身份，不与正式出版文献等同处理。
- **arXiv license 未逐篇核对。** 检索 skill 的 ToS 通知已生成，但各篇 arXiv 论文的 license 不同（部分为非独占分发许可，不等于 CC）。若后续要在论文中复制图表或长段文字，须逐篇核对。本阶段只做引用，不涉及复制。
- **未做系统综述。** 检索是按六个主题的定向检索（每主题 1–2 条检索式），不是 PRISMA 式系统检索。两篇的相关研究节必须写明这一点，不得声称覆盖完整。

## 4 逐条覆盖表与检索记录

以下为检索执行记录，含每条新增文献回答什么、与已有 30 条的区别、实际检索式与命中数、以及核验失败或主动删减的候选清单。

| 主题 | 新增条目 key | 该条回答什么 | 与已有 30 条的区别 |
|---|---|---|---|
| 1 失效分类 / 协调错位 / 可靠性实证 | `mao2025towards` | 把「under-specification / coordination misalignment / inappropriate verification」三类缺陷归因到缺少 SE 结构化原则，并用显式行为契约 + 生命周期验证做干预，按 MAST 度量失效下降 | MAST 只给分类法，本条给出「契约化绑定」作为干预并量化效果；是 A 篇「绑定关系断裂」命题最接近的既有工程对策 |
| 1 | `shah2026characterizing` | agentic AI 故障的类型 / 症状 / 根因三层分类，覆盖单 agent 与工具链故障 | MAST 聚焦 multi-agent 对话失效；本条以 fault–symptom–root cause 三轴组织，可直接对齐 A 篇「现象→机制」的描述层定位 |
| 1 | `zhou2026messages` | LLM MAS 中通信故障（消息内容 / 时序 / 路由）的实证刻画，cs.SE | 已有 2610.19342 讲 private states 失效分类；本条专攻「消息本身出错」，覆盖 A 篇的信息绑定（消费者拿到的事实不足/不对） |
| 1 | `pitre2026understanding` | 同主题独立证据：通信失效的归因与传播 | 与 `zhou2026messages` 互为独立复现；A 篇可用「两组独立研究均指向通信边界」支撑外部效度 |
| 1 | `albayaydh2026beyond` | 跨 benchmark 综合 tool-use / planning / reasoning 三类失效，指出排行榜掩盖了失效结构 | 已有条目都是单系统或单分类法；本条是综述型锚点，用于说明「为何需要部署现场的描述性研究」 |
| 2 计划修复 / 复用 / 重规划 | `nebel1995plan` | 经典理论+实证：plan reuse 在最坏情况下并不优于 from-scratch generation（保守复用的复杂度下界） | 已有 Kambhampati & Hendler 1992 讲 plan modification 机制；本条给出「复用不一定省」的负面结果，是 A 篇「新计划是否该继承已验证产物」的理论约束 |
| 2 | `babli2023plan` | plan commitment 视角下系统比较 replanning 与 plan repair，给出承诺保持（stability）与成本的权衡 | 补上 A 篇最缺的一环：承诺（commitment）在重规划时如何被保留或丢弃；与 Cohen & Levesque 1990 的意图/承诺理论形成「理论—算法」对接 |
| 2 | `vanderkrogt2005coordination` | 把 plan repair 当作多 agent 协调机制：修复一个 agent 的计划时如何不破坏他人承诺 | 已有计划文献全是单 agent；本条是「修复跨越责任边界」的最早明确处理，直击 A 篇的责任绑定 |
| 2 | `veloso1993derivational` | derivational analogy：复用的是推导轨迹而非成品计划，含 case acquisition/storage/utilization 三环 | 为 A 篇「成果绑定」提供反例设计空间：复用轨迹（含验证理由）而非只复用产物，可解释旧 PASS 为何不可直接继承 |
| 2 | `yuan2026replan` | LLM agent 侧统一实证：扰动下 replan / repair / edit 三种修订策略的对照评测 | 已有 LLM agent 文献（AFlow / DyFlow）优化的是工作流生成，不是「已承诺计划遭扰动后怎么改」；本条是 LLM 侧唯一直接可比的实证基线 |
| 2 | `komenda2012decentralized` | 去中心化多 agent 计划修复，动态环境下的局部修复与再协商 | 与 `vanderkrogt2005coordination` 互补：强调「修复发起方不是全局控制者」，对应 A 篇 Supervisor/executor 的非对称结构 |
| 3 组织记忆 / 团队知识治理 / 规则适用范围 | `schmidt1996coordination` | CSCW 经典：coordination mechanism = 协议 + 承载它的 artifact；机制的适用范围由其具体化（reification）决定 | 已有 Malone & Crowston 1994 给协调理论的任务分解框架；本条给的是「机制被物化成制品后如何被误用」，正是团队规则落成文件后的适用范围问题 |
| 3 | `ackerman2000reexamining` | 组织记忆在实践中不是检索问题而是「上下文被剥离」问题：记忆片段脱离原情境后不可用 | 已有 Walsh & Ungson 1991 是组织记忆的概念框架；本条是实证/批判层，直接解释 A 篇「规则条件与任务阶段不匹配」为何系统性发生 |
| 3 | `olivera2000memory` | 实证调查组织记忆系统的收集 / 存储 / 访问三类机制及其失效 | 给 A 篇的团队规则记忆一个可对标的机制分类，区别于 LLM 记忆文献（MemGPT / A-Mem / G-Memory）只谈存储与检索 |
| 3 | `argote2000knowledge` | 知识在组织单元间迁移的载体（人/工具/任务）与「迁移会失败」的条件 | 对应 A 篇跨角色交接：知识载体变化时适用性不自动继承；已有 30 条无任何知识迁移文献 |
| 3 | `feldman2003reconceptualizing` | 组织例程的 ostensive（规则的抽象形式）与 performative（实际执行）之分 | 为 A 篇「规则适用范围绑定」提供核心概念：规则文本与规则执行是两个对象，断裂发生在二者之间；RBAC 只管权限不管例程 |
| 3 | `joshi2026deontic` | 用道义逻辑（义务/许可/禁止）表达运行期 agent 治理策略，含条件与适用范围 | 已有 Instruction Hierarchy 讲指令优先级，RBAC 讲静态权限；本条提供「带条件的规范」形式化，可用于表述「规则的阶段条件」 |
| 3 | `qi2026memory` | 何时**不要**写记忆：相关性高的 agent 轨迹会把偶然共现错误晋升为团队规则 | 已有记忆文献全在优化「怎么记更多/更准」；本条是唯一讨论「写入闸门与误晋升」的，直接支撑 A 篇规则适用范围过宽的机制叙述 |
| 3 | `kirchdorfer2026organizational` | 把组织记忆做成 agentic 业务流程执行的一等构件，含记忆的作用域与生命周期 | 把经典组织记忆概念接到 agent 系统上；已有 G-Memory 是层次化记忆结构，不含组织/治理语义 |
| 4 harness / 工具接口边界 / 责任与权限 | `wang2026approval` | **Approval Laundering**：系统化刻画 coding-agent harness 中「批准—执行绑定」失效（批准的对象与真正执行的对象不是同一个） | 这是已有 30 条里完全缺失、却与 A 篇凭据绑定/责任绑定最同构的一条：同样是「某张许可/回执是否对应当前候选」。已有 2607.15684 讲 model–harness 边界的 agent-reactive bug，不涉及批准绑定 |
| 4 | `arora2026specifying` | 需求工程视角：如何显式规定「委派自治边界」——哪些目标在一次委派后仍归委派方 | 直接对应 A 篇责任绑定（一次执行结束后目标还归谁）；已有 CodeDelegator / AgentConductor 是机制实现，不做边界规范 |
| 4 | `macedo2026harness` | 给出 agent harness 的充要条件定义 | 为 A/B 两篇提供「harness」这个统一语言术语的可引用定义，避免自造定义 |
| 4 | `sghaier2026dont` | 实证：同一模型下 harness 版本演进对 coding agent 质量的影响大于模型差异 | B 篇用它论证「必须在真实运行器上做实验、且必须固定 harness 版本」这一实验设计约束；已有 Measuring Agents in Production 讲生产度量，不做 harness 版本对照 |
| 4 | `yang2026privileges` | 实证 LLM agent 选工具时的过度授权（更低权限足够完成任务却选了高权限工具） | 与 RBAC（Sandhu 1996）的静态模型互补：这是 agent 侧的动态权限选择偏差，对应 A 篇权限/责任在工具边界的表达 |
| 4 | `marla2026permission` | 授权与恢复的交互：重试 / 恢复路径上授权如何被重新评估或被跳过 | 补「失败后重试」这一 A 篇高频场景的授权语义；已有条目无一涉及 retry 时的权限再绑定 |
| 5 工作流拓扑 / 角色分工 / 协调开销 | `orogat2026architectural` | 对照实验：架构（拓扑/角色划分）而非模型能力主导 multi-agent LLM 性能 | B 篇两角色 vs 四角色对照的最直接先例；已有 AutoGen / MetaGPT / ChatDev 给的是系统，不做架构因子的受控对照 |
| 5 | `hong2026learning` | 按任务难度选择协作拓扑：协作量与收益非单调，过度协作有净损失 | 给 B 篇「四角色不一定更好」的预期方向与效应量参考；AFlow / DyFlow 搜索工作流但不量化协调开销本身 |
| 5 | `zhou2026token` | 以 token 计量分工的协调成本，并用计算性分工降低该成本 | B 篇协调成本的可比度量口径（token/轮次）来源；已有 30 条没有任何把协调开销当因变量的研究 |
| 5 | `liu2026know` | 拓扑条件化的失效诊断：不同拓扑下失效模式与定位难度不同 | 把主题 1 与主题 5 接起来：角色数变化不只改成本，也改失效分布；支撑 B 篇把失效类型作为次要因变量 |
| 5 | `zhang2026silo` | Silo-Bench：可扩展环境，专门评测 MAS 的分布式协调（信息分隔条件下） | 给 B 篇「跨会话信息不共享」提供既有可比实验范式；与 LongMemEval（长记忆检索）不同轴 |
| 5 | `mediratta2026decentralized` | 共享代码库上的去中心化多 agent 协调与通信 | 最接近 B 篇被试环境（真实代码库、并发 agent）的实证研究；已有条目都在对话或抽象任务上 |
| 6 证据 / 凭据 / 来源认可 | `simmhan2005survey` | e-science 数据溯源的分类综述：溯源记录的用途、粒度与信任模型 | 为 B 篇「凭据认可」提供溯源领域的统一语言；已有 object-centric process mining 关注事件日志结构，不处理证据的信任与认可 |
| 6 | `moreau2010foundations` | 溯源在 Web 规模下的形式基础：溯源断言的归属、可验证性与跨域接受 | 直接给出「另一方签发的断言，此方是否接受」的理论框架，正是 B 篇跨会话凭据认可的抽象形式 |
| 6 | `muto2026attestable` | 可证实的构建链：让可复现构建产出可被第三方验证的 attestation | B 篇「有效测试回执」的工程对照物：证据与候选产物的绑定如何被密码学固定 |
| 6 | `sadowski2018modern` | Google 规模下现代代码评审的实证：评审者如何依赖自动化证据（测试/分析结果）作出接受决定 | 给 B 篇「控制者接受他人签发证据」的人类基线；已有 Runeson & Höst 2009 只是方法论，不含评审实证 |
| 6 | `asad2026kettle` | Kettle：以 attested build 产出可验证软件溯源 | 与 `muto2026attestable` 互为独立实现证据；展示「证据—产物」绑定可被实现，佐证 B 篇把认可失败归于协议缺失而非不可实现 |
| 6 | `castillo2026evidence` | 证据驱动的可信 CI 协议：流水线每步产出可验证证据并逐步累积 | 最贴近 B 篇设计的一条——它正是「CI 中的构建/测试证明如何被下游接受」；已有条目完全无 CI 证据协议 |
| 6 | `tamanna2024analyzing` | 实证访谈：SLSA 溯源框架在真实部署中落地的障碍 | 提供「证据机制存在但不被采用/不被认可」的实证证据，呼应 B 篇观察到的认可失败不是技术不可行 |

## 检索式与命中数

arXiv（全部经 `literature-search-arxiv` skill 的 `search_arxiv.py` 发起，`--max_results 20`）：

| # | 主题 | 检索式 | 返回 | 入选 |
|---|---|---|---|---|
| q1a | 1 | `abs:"multi-agent" AND abs:"LLM" AND (abs:"failure" OR abs:"failures") AND (abs:"taxonomy" OR abs:"empirical")` | 20 | 5 |
| q2a | 2 | `abs:"plan repair" OR abs:"plan reuse" OR (abs:"replanning" AND abs:"agent")` | 20 | 2 |
| q2b | 2 | `(abs:"LLM agent" OR abs:"language model agent") AND (abs:"replanning" OR abs:"plan revision" OR abs:"long-horizon") AND abs:"task"` | 20 | 0 |
| q3a | 3 | `(abs:"norms" OR abs:"rules" OR abs:"policy") AND abs:"agent" AND (abs:"scope" OR abs:"applicability" OR abs:"versioning" OR abs:"governance") AND (cat:cs.SE OR cat:cs.MA)` | 20 | 1 |
| q3b | 3 | `(abs:"team" OR abs:"organizational") AND abs:"memory" AND abs:"knowledge" AND (cat:cs.SE OR cat:cs.HC OR cat:cs.MA)` | 20 | 1 |
| q4a | 4 | `(abs:"tool" AND abs:"agent" AND (abs:"permission" OR abs:"authorization" OR abs:"least privilege" OR abs:"delegation")) AND cat:cs.SE` | 20 | 4 |
| q4b | 4 | `(abs:"agent harness" OR abs:"tool interface" OR abs:"Model Context Protocol") AND (abs:"empirical" OR abs:"study" OR abs:"analysis")` | 20 | 2 |
| q5a | 5 | `(abs:"multi-agent" AND (abs:"topology" OR abs:"role specialization" OR abs:"division of labor") AND (abs:"coordination" OR abs:"overhead" OR abs:"cost")) AND (cat:cs.SE OR cat:cs.MA OR cat:cs.AI)` | 20 | 4 |
| q5b | 5 | `(abs:"single-agent" AND abs:"multi-agent") AND (abs:"compare" OR abs:"comparison" OR abs:"ablation") AND abs:"token" AND abs:"cost"` | 10 | 2 |
| q6a | 6 | `(abs:"provenance" OR abs:"attestation" OR abs:"build provenance" OR abs:"SLSA") AND (abs:"software supply chain" OR abs:"continuous integration" OR abs:"artifact")` | 20 | 3 |
| q6b | 6 | `(abs:"code review" OR abs:"pull request") AND (abs:"evidence" OR abs:"test results" OR abs:"CI" OR abs:"trust") AND (abs:"empirical" OR abs:"study")` | 20 | 0 |

Crossref `query.bibliographic`（rows=4，用于非 arXiv 经典文献）：

| 检索式 | 返回 | 入选 |
|---|---|---|
| Coordination mechanisms towards a conceptual foundation of CSCW systems Schmidt Simone | 4 | 1 |
| Reexamining organizational memory Ackerman Halverson | 4 | 1 |
| Memory systems in organizations Olivera | 4 | 1 |
| Knowledge transfer a basis for competitive advantage in firms Argote Ingram | 4 | 1 |
| Reconceptualizing organizational routines Feldman Pentland | 4 | 1 |
| Plan reuse versus plan generation a theoretical and empirical analysis Nebel Koehler | 4 | 1 |
| Plan stability replanning versus plan repair Fox Gerevini Long Serina | 4 | 1（命中的是 Babli 2023，非 Fox 原文） |
| Plan repair as an extension of planning van der Krogt de Weerdt | 4 | 1（命中的是 IBERAMIA 2005 版） |
| Case-based planning Hammond derivational analogy Veloso | 4 | 1 |
| A survey of data provenance in e-science Simmhan Plale Gannon | 4 | 1 |
| The foundations for provenance on the web Moreau | 4 | 1 |
| Reproducible builds break a trust dependency | 4 | 1（命中 Muto & Suzaki DSN-W 2026） |
| Why dont software developers use static analysis tools | 4 | 0 |
| Modern code review a case study at Google Sadowski | 4 | 1 |

核验调用：Crossref `works/<doi>` 15 次（15/15 命中），OpenAlex `works/doi:<doi>` 38 次（38/38 命中，`is_retracted` 全为 false）。

## 查不到或核验失败而舍弃的候选

| 候选 | 舍弃原因 |
|---|---|
| Fox, Gerevini, Long & Serina, "Plan stability: replanning versus plan repair"（ICAPS 2006） | Crossref 检索不到该篇的出版记录（ICAPS 2006 论文集未注册 DOI）。不编造 DOI/页码，改用主题等价、有 DOI 的 `babli2023plan` |
| van der Krogt & de Weerdt, "Plan repair as an extension of planning"（ICAPS 2005） | 同上，ICAPS 2005 无 DOI 记录；改用同作者同主题、有 Springer DOI 的 "Coordination Through Plan Repair"（LNCS 2005） |
| Hammond, *Case-Based Planning*（1989 专著） | Crossref 未返回可核验的书目记录（卷期页/DOI 缺失），改用 `veloso1993derivational` |
| 2608.30391 "Using Grounded Theory for Agent Behavior Analysis at Scale" | 主题偏方法论（定性编码流程），与主题 1「失效分类」只擦边；A 篇方法论位已由 Runeson & Höst 2009 占据 |
| 2606.19464 的 arXiv 版 | 已有正式出版版本（IEEE ICWS 2026，DOI 10.1109/ICWS72778.2026.00159），只收出版版，避免同一工作双条目 |
| 2509.20512 / 2502.15030 "CHOIR"（CHI '26，DOI 10.1145/3772318.3791314，核验通过且未撤稿） | 核验通过但主题 3 已有 8 条、超出 4–6 条目标；CHOIR 聚焦科研实验室聊天机器人中介的组织记忆，与两篇论文的团队规则治理相关性弱于入选的 8 条，故主动删减 |
| 2510.04964 "Why Software Signing (Still) Matters"（核验通过，未撤稿） | 同上，主题 6 已达 7 条；其「签名的信任边界」与 `muto2026attestable`/`asad2026kettle` 重叠度高，主动删减 |
| 2610.34933 "What Will Replace Pull Requests?" / 2606.26721 "Knowledge-Based Pull Requests" | 定位为愿景/立场性工作而非实证研究，B 篇需要可比的实证或机制基线，未入选（未做 DOI 核验） |
| 2604.14228 "Dive into Claude Code" / 2609.08371 "Authority Is Not a String" | 主题 4 已满额；前者为设计空间综述、后者为防护机制提案，与 A 篇「绑定断裂的描述」相关性低于入选 6 条 |

## 使用到的论文 URL（literature-search-arxiv skill 产出）

https://arxiv.org/abs/2510.12120 ·
https://arxiv.org/abs/2603.06847 ·
https://arxiv.org/abs/2610.15459 ·
https://arxiv.org/abs/2610.32060 ·
https://arxiv.org/abs/2607.05775 ·
https://arxiv.org/abs/2609.19654 ·
https://arxiv.org/abs/1202.2773 ·
https://arxiv.org/abs/2607.02579 ·
https://arxiv.org/abs/2607.03228 ·
https://arxiv.org/abs/2609.38983 ·
https://arxiv.org/abs/2607.17225 ·
https://arxiv.org/abs/2606.10106 ·
https://arxiv.org/abs/2607.03691 ·
https://arxiv.org/abs/2606.20023 ·
https://arxiv.org/abs/2610.31916 ·
https://arxiv.org/abs/2602.03128 ·
https://arxiv.org/abs/2609.13890 ·
https://arxiv.org/abs/2610.08155 ·
https://arxiv.org/abs/2610.10126 ·
https://arxiv.org/abs/2603.01045 ·
https://arxiv.org/abs/2611.00314 ·
https://arxiv.org/abs/2605.08363 ·
https://arxiv.org/abs/2605.21089 ·
https://arxiv.org/abs/2409.05014 ·
https://arxiv.org/abs/2510.04964（候选，未入库）
