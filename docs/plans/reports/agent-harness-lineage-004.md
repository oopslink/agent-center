# Agent Harness 交付目标谱系与验收版本重建

版本：0.4。沿用run-001冻结快照（2026-11-01当地建立）；实际提取时间以UTC日志为准。

**单一AI探索分析，尚未人工复核。** 本轮完成已有4条关联线索与2个对照的有界重建，没有重跑历史验收、调用付费模型或修改生产数据。前序：[30例探索研究](agent-harness-research-002.md)、[文献比较与贡献定位](agent-harness-literature-003.md)。逐案数据、时间线和原始证据入口见[重建附录](agent-harness-lineage-cases-004.md)。

## 1. 主要发现

**跨Plan关联的实际价值主要是补全后继结果、限定验收适用范围和恢复任务归属，而不是把失败统一改判成功。** 六个目的性案例组中，Insight与Access补上了后继Plan结果；v2.10.3补上了两个功能的验收和发布记录；Operation只补强已有结论；两个对照的结论不变。这是逐案差异分类，不是成功率或准确率改善估计。

本轮最有解释力的案例是Access：同一个旧候选曾获PASS，用户指出与mock不一致后，PD撤回结论，审计记录确认Plan重开；随后又修改产品信息架构与canonical矩阵，新Plan对新SHA报告PASS。**“旧PASS被撤回”与“新合同下后来报告PASS”必须同时保留。** 只取当前done或最后一句PASS都会丢失这段历史。撤回本身在原Plan会话内已经可见，不能算作跨Plan关联独有的发现。[证据A-OLD-PASS、A-USER、A-REVOKE、A-REOPEN-AUDIT、A-CONTRACT、A-FINAL，见附录](agent-harness-lineage-cases-004.md)

旧谱系中的一条“验收转交”还需要修正：`plan-2d41d1da`在快照中没有端点记录，消息只声称它负责sites v2.10.2模块验收。另循两个明确Task ID找到的`plan-f7f98cfe`，实际承载v2.10.3附件与Issue工具的验收。两者不能混为一个“全Plan通过”的证据。[证据V-TRANSFER、V-FEATURES、V-T171、V-T172](agent-harness-lineage-cases-004.md)

## 2. 方法、范围与证据边界

### 2.1 有界案例选择

在新提取前保存[协议v1](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/protocol.md)。分析者已知前两轮发现，故不宣称盲评或全新预注册。纳入0.2轮登记的4条关系全部端点，另预选“done但拒绝收口”和“同Plan修复后交付”两个对照，不按本轮是否出现结论变化筛选。

允许明确引用的一跳扩展：a77描述中的前驱569，以及7fa消息中两个验收Task所属的f7。相似标题、同项目或同Issue本身均不足以认定同一目标。关系有类型和范围，验收转交不会自动把两个Plan视为完全等价。

| 本轮保存范围 | 数量与说明 |
|---|---|
| 研究案例组 | 6；不是6个已确认独立目标，也不是随机样本 |
| 端点 | 10个存在的Plan、1个缺失Plan ID、1个没有plan_id的替代Task |
| 所属记录，按稳定ID去重 | 268 Task、3,086 Message、63 generation；另保存verdict、finding、action、audit和文件引用 |
| 全库字面检索 | 扫描26,961条Message，保存1,758条端点ID命中；没有逐条完成筛选 |
| 引用证据 | 52条源记录＋7个持久文件/命令证据，共59项；源记录原文与哈希均保留 |
| 研究编码 | 9条类型化关系（6条对象关系、3条合同/撤销关系）、13个合同片段、27个验收/转交事件 |

合同片段包含父目标、子目标、部分可见合同和仅宣布的未来门禁，**不是13个完整执行过的合同版本**。导出全部记录也不表示全文逐条阅读；本轮采取定向阅读，未审查命中和旧样本cross-plan线索单列待审。

### 2.2 三个比较视图

| 视图 | 可用信息 | 防止的误差 |
|---|---|---|
| B0 状态展示 | 快照Plan/Task状态 | 仅展示代理，不定义done=成功、discarded=目标失败 |
| B1 单Plan本地证据 | 本Plan及所属Task的描述、记录、会话和本地引用文件 | 保留已有跨对象结果叙述，不故意删除有利于B1的消息；不查后继端点来验证 |
| B2 关联重建 | B1＋明确关系端点＋对应合同、SHA、判定和产物 | 分开补全结果、范围修正、证据加强和不变；不把最后的PASS回填到所有旧对象 |

三个视图沿用相同冻结快照。主表以各案例原始入口Plan作为B1锚点；[附录](agent-harness-lineage-cases-004.md)同时给出全部10个Plan各自的本地判断，避免只展示最不完整的旧Plan而隐藏后继Plan本地已知信息。B1不是盲评，B2也不是真实结果金标准。

E0表示状态；E1表示可定位的系统/参与者记录；E2只表示某份持久报告、历史代码对象或Git关系已读取核对；E3表示独立重跑。本轮无E3。**读到“测试通过”的报告，只能核实报告的存在和内容，不能独立确认测试确实执行且覆盖充分。**

### 2.3 不能把当前验收表当成历史事件流

除旧导出的`pm_gate_verdicts`，本轮补读了`pm_plan_review_verdicts`、`pm_plan_decision_outcomes`、continuations和loop-rounds，范围内分别获得19、10、2、0条记录。DDL明确review表以`(plan_id, task_id)`为主键，采用latest-wins覆盖；因此当前行不能枚举所有历史验收变化。重建将消息、reopened审计和精确候选并列，数据库record version、generation编号与研究contract_id互不替代。[DDL与查询](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/logs/schema.json)

## 3. 单Plan与关联重建的逐案差异

| 案例与B1锚点 | B0状态 | B1：完整本地证据 | B2：关联重建 | 实际变化 |
|---|---|---|---|---|
| G01 Insight，d7ec8ee4 | discarded | 新Plan929已创建，A–F与release边界明确；后继终验未知 | 929报告main `c558505c` A–F终验PASS，原始报告可读；生产release仍未知 | **补上后继结果及适用范围**，旧Plan仍废弃 |
| G02 Access，a77ec987 | done | 旧`7700038b` PASS已撤回、重开；知道新文档和e91创建 | 569→a77→e91的Access部分承接；新合同下`1b5804fd`报告终验PASS，持久报告可读 | **补上后继结果与合同谱系**；撤回不是B2独有发现 |
| G03 Operation，d1680545 | paused；旧Task discarded | 本地已经报告替代Task、交付`cda9136c`及main `ae578a8d` | 无plan_id替代Task有delivery字段，含首次non_delivery和后续push；Git对象未找到 | **仅交叉核对和恢复归属**；没有成功翻转 |
| G04 v2.10.3，7fa6888f | done | 合并门、两个功能验收Task转交；另有sites转交主张 | f7内T167/T170报告GO，发布树报告可读，main/tag有叙述；sites完整模块验收仍未知 | **分项结果补全并纠正误连**，不给整个旧Plan统一PASS |
| G05 节点移出，becb5960 | done；8 Task completed | 明确terminal REJECT、no Ship；done仅拒绝收口 | decision=reject交叉佐证；仍未交付 | **结果不变** |
| G06 Mermaid，67822059 | done | 同Plan中deccd拒绝→6855通过→b0c40集成；7ca新tip未验收 | 只补证6855是b0c40祖先；候选/合同边界不变 | **结果不变** |

G01/G02是后继结果补全，G04是分项结果补全；不能合计成“3个失败改判成功”。G03说明完整本地会话已经足以恢复一部分跨任务事实；G05/G06说明合理的单Plan分析本身能避免许多状态误读。没有独立金标准，故不报告误分类率、precision/recall或准确率提升。

## 4. 验收版本重建的关键时间线

### 4.1 Access：撤销、合同变化与新候选通过是三件事

以下时间均为2026-08-23 UTC，完整时间戳及证据ID见附录。

| 时间 | 发生的事 | 对判定的约束 |
|---|---|---|
| 11:01 | a77报告`7700038b`终验PASS；部分Team mapping/CAS证据沿用父任务 | 这是当时参与者的验收主张，不是独立真值 |
| 13:56前后 | 用户指出与mock完全不符；13:56:08审计记reopened→paused，13:56:16 PD明确撤回收口 | 当前done不能抹去撤回；B1已经能识别 |
| 15:01 | `7232c04d`文档与ADR-0059确立两入口，RAM Roles作为Team Role属性 | 产品IA发生明确变更，不能假定同一不变合同 |
| 15:06 | e91创建，6任务、全部由PD承接 | 执行组织也变化，不能归因于单一DAG因素 |
| 15:28→15:34 | 新canonical仍因Risk/typed-delete被拒，修正版`06183d8c`获PASS | 同一矩阵下候选修复；canonical PASS不等于产品终验PASS |
| 17:32 | 集成候选`1b5804fd`报告S6终验PASS；`aadffe3a`中有持久报告 | 只适用于新合同、新候选；不恢复旧7700的PASS |

历史ADR和矩阵确实可读：删除独立mapping产品入口、把RAM Roles编辑归入Team Role上下文，同时保留CAS、audit、effective-access行为。S6报告明确候选SHA和fresh binary，但本轮没有逐张截图重验、逐项覆盖核验或独立重跑。全部任务归同一PD也使最终验收独立性弱于真正独立的人类评审。[历史ADR、矩阵、S6报告](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/evidence/access-s6-report.md)

前驱569的旧候选`9a512ef9`也有PASS。我们只确认它的Access有效未终态工作被a77明确承接，**没有将a77针对7700的撤回自动扩大成“569全部历史验收失效”**。该链是带范围变更的目标延续，不是三个完全等价的实验重复。

### 4.2 Insight：开发终验与生产发布分开

旧Plan当前描述覆盖Insight IA、Project交付态势和Evolution。重建消息明确新A–F开发验收，禁止把Gate/Runtime/recovery平台修复继续加入，并把生产release保留为owner独立动作。后来`c558505c`的持久报告支持“新开发合同下报告终验PASS”；它不能证明生产发布已经完成，也不能说明旧Plan的每项历史要求原样满足。

因此，若研究问题是“开发合同是否出现可定位的最终验收”，B2补出了肯定的报告证据；若研究问题是“原需求对应生产是否部署成功”，本轮仍未确认。**结果取决于明确的交付层级，而不只是换一个Plan聚类键。**[I-HANDOFF、I-ARTIFACT](agent-harness-lineage-cases-004.md)

### 4.3 v2.10.3：按子目标传递验收，不按容器整包传递

7fa中明确提交的两个验收Task后来属于f7：T171负责data/API与授权，T172负责浏览器及服务契约。`402a5cea238543a29f6f460a1eec7da54b51df2d`的历史树中，两份报告仍可读取，范围均为T167附件与T170 Issue工具。报告还说明agent侧部分用服务层集成测试替代直接驱动worker工具链，不能转述为全部真实多agent端到端验收。

sites链则存在不同证据：缺失2d41端点、综合报告拟并轨，以及“sites两项按指示直接过”的参与者叙述。最终发版消息声称包含sites，不能补造其逐模块独立验收；“Pages会自动部署”也不是部署成功回执。这一负例要求谱系保留**分项范围和未确认分支**。[V-SITES-LIMIT、V-RELEASE、V-ARTIFACT1/2](agent-harness-lineage-cases-004.md)

### 4.4 对照：合同变化不等于候选变化

Mermaid的deccd→6855是同需求下修复候选，未找到足以新建合同版本的要求变化；随后同名分支漂移到7ca，不继承6855的PASS。反之，节点移出案例明确宣布下一轮必须fresh-main新SHA并扩大race×10覆盖，但这只是未来门禁，不能计为本轮已经执行通过的合同。[M-REJECT、M-PASS、M-INTEGRATION、N-REJECT](agent-harness-lineage-cases-004.md)

## 5. 关联如何改变计数归属

| 关联范围 | 单容器记录量 | 关联后按ID去重 | 合理解释 |
|---|---|---|---|
| Insight旧/新Plan | 148/59 Task；33/12 generation | 207 Task、45 generation | 旧Plan废弃不能让前期工作消失；包含支持性与平台工作，不能当纯目标工作量 |
| Access 569/a77/e91 | 15/15/6 Task；1/10/1 generation | 36 Task、12 generation | 带范围变更的Access承接；不把三个done视作三个独立成功，也不声称所有Task都属于相同目标 |
| Spark父Plan＋替代Task | 父Plan6 Task、1 generation | 7 Task、1 generation；其中旧/新Operation Task为2个 | 修复plan_id遗漏的归属；不能把全部7个Task当Operation成本或六领域已完成 |
| v2.10.3开发＋功能验收Plan | 3/2 Task、均无generation | 5 Task、0 generation | 仅记录所读容器；不含缺失sites Plan，不能声称覆盖全部验收工作 |

这些都是**累计数据库记录规模**，不是同时活跃图大小、有效执行次数、token成本、耗时或生产率。没有做成本推断，也不比较“207对36谁更低效”。generation多或少不能从这里推出evolution有效或无效。

## 6. 对研究贡献定位的更新

[上一轮文献比较](agent-harness-literature-003.md)已确认：多对象分析、执行谱系、分层结果、契约测试和动态工作流均有先行研究。本轮增加的是可审计的生产测量实例：

1. **有范围的谱系规则**：区分目标承接、Task替换、验收责任转交与未决引用，展示错误合并怎样把两个功能的GO传播成整Plan通过。
2. **可撤销、绑定版本的验收记录**：在具体SHA、合同片段和交付层级下保存PASS/REJECT/撤回，而不是只保留末态。Access形成较完整的纵向例证。
3. **比状态代理更强的对照**：把完整本地B1作为基线，明确Operation及两个对照没有结果改善，避免夸大关联价值。

当前可主张的是**应用方法候选及纵向案例证据**，尚未证明新算法、首次发现、普遍效果或因果机制。适合下一步形成“版本化交付目标如何改变生产Agent结果判定”的案例研究；不能写成“多Agent结构越简单越好”或“重建证明DAG evolution提高成功率”。

对harness设计的候选建议也更具体：显式记录`supersedes/acceptance-for`及其子目标范围；将验收作为可撤销事件绑定合同与产物SHA；让界面分别展示管理终态、验收、集成和部署。它们由案例支持为待验证设计方向，本轮没有修改产品实现或验证效果。

## 7. 保存、复现与验证

完整本地产物位于[run-001/lineage-004](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/README.md)，原始业务消息保存在仓库外。包含协议、只读SQL/参数、端点完整记录、全库字面命中、Schema、历史Git/Blob原字节、阅读范围、纳排/修订记录、结构化关系/合同/事件/比较、逐Plan计数和哈希清单。

- [比较数据](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/derived/comparisons.json) · [全部Plan本地判断](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/derived/plan-local-comparisons.json)
- [关系](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/derived/relations.json) · [合同片段](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/derived/contracts.json) · [验收事件](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/derived/acceptance-events.json)
- [59项证据索引](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/derived/evidence.json) · [过程与判断修订](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/logs/decisions.md) · [验证结果](/Users/oopslink/works/research/agent-harness-study/run-001/lineage-004/logs/validation.json)

脚本能从冻结提取重建表格、计数与附录；语义标注由AI作出，脚本重跑不会独立验证判断本身。校验范围包括证据记录与哈希、引用存在、候选/关系端点、稳定ID去重、报告计数、既有输入未变及本轮清单。没有代码提交，未运行应用Go/Web测试；这些不构成历史验收重跑。

## 8. 限制与下一步

样本来自既有问题线索，不代表全部108个Plan；三个视图非盲评且无独立金标准；多数交付真实性仍依赖参与者报告；历史合同只部分恢复；当前描述不能回填创建时合同；Blob与数据库非原子备份；缺失端点、Spark Git缺对象和sites完整验收未知均保留。上述限制不应被“谱系重建”这个名称掩盖。

下一阶段仍按总计划：针对I166“建议fork而执行模式禁止fork”的历史矛盾，冻结当时版本、可见输入、预期行为与最小回放边界，分开静态契约冲突和实际执行影响。人工复核、全量关系发现与付费模型消融继续列为后续工作，本轮未执行。
