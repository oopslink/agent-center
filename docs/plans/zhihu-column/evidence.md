# 七篇文章的证据与图表索引

本轮核对日期：2026-11-04。结束阶段工作树 HEAD：`aeebf47cdc1885618fdbe6b93e0ff4edc2b0f4a8`。仓库由多个会话共同使用，本文只编辑 `docs/plans/zhihu-column/`；以下入口用于复查正文主张，不能将整个工作树视为本轮产生的改动。

## 每篇主张与来源

| 稿件 | 工程观点 | 支持它的具体事实 | 证据边界 |
|---|---|---|---|
| 02 | 身份、授权、名字、信息充分性各有职责 | `Identity`、`Member`、`Agent`、`identityRef`；P3 表7与表7a | 短情境首步处置，不代表长期团队体验 |
| 03 | 保留控制/执行分离，按最终交付评价模型路由 | `ResolveExecutor` 的实际分支；`JudgeEnabled` 默认关闭；P1 有责任中断和正确承责案例 | 没有据此声称现有路由已达到最优成本；Slot 仅核实为已接受设计，当前 Pool 未见实现 |
| 04 | Task 管业务状态，运行事实独立观测 | `agent/service/appservices.go` 明确移除 WorkItem；`Task` 带租约与动作日志；`reads.go` 区分已分配和可运行 | 设计主张，不是两种领域模型的随机性能实验 |
| 05 | 角色按新增缺陷发现能力设置，优先修证据交接 | P2 表9、表10和§4.10 | 12对探索性函数任务；不是全部软件任务的组织结论 |
| 06 | 图编辑必须同时核对候选成果继承 | `EditPlanTopology`；P4 195快照、169转移及候选谱系 | 改图前检查是建议，未宣称已实验验证停止规则收益 |
| 07 | 规则发布必须带阶段和替代关系 | `RuleSnapshot`；P5 §4.2—4.6与表4 | 230回执不是采用率；32次实验未测检索或端到端交付 |
| 08 | 人保留领域边界、证据标准和终态验收 | conventions §0.4反例；测试规约；运行器整批排除与复验 | 任务模板是建议，不是原始开发对话 |

## 开篇案例

2026-11-05 开头修订：七篇使用同一段无标题引文；各自的需求动机从以下实际记录展开。这里列出写作依据，不把既有论文的探索性分析升级为独立复核结果。

| 稿件 | 切入现象 | 已核对来源 | 写作边界 |
|---|---|---|---|
| 02 身份与上下文 | 被称为 PD 的成员报告 not_memory_curator，职责称呼没有带来治理权限 | P3 §4.2；另外读取 N02-E01 原始消息及 N02-E02 Curator 行 | 权限拒绝保护边界；后续记录不构成完整授权历史 |
| 03 Supervisor–Executor | 看见没有执行体，却把启动责任留给中心；用户纠正后更改解释 | P1 §4.2、A04a–A04e、A05 | 状态日志与参与者启动报告分开；不宣称提醒后完成交付 |
| 04 Task | 点击恢复只得到重试提示，实际原因是 Agent 正在处理另一项工作 | ADR-0048 的 T101 背景、根因和修复决定 | 合法的并发约束被 UI 隐藏；不是双状态同步错误 |
| 05 工作流 | 代码与测试通过，重复审查却耗尽集成前的启动预算 | P2 §4.9、表 10 | 隔离运行器中的真实执行实验，非生产原样回放；只描述该例 |
| 06 Plan/DAG | Graph 的后续计划保留工作记录，候选代码却缺少此前已验后端文件 | P4 §4.2、组织 Graph 的 Git 与文件核查 | 任务关系与成果继承脱节；不归因于 DAG 导致代码丢失 |
| 07 团队记忆 | 合入 main 才能完成的通则阻断了先交候选再独立验收的中间环节 | P5 §4.3、M02 | 操作者也强调同一规则；不把全部影响归给持久记忆 |
| 08 开发实践 | 三个进程直读数据库后，曾提议用单机架构解释现状并修改文档 | conventions §0.4 的 v2.0 GA 事件记录 | 复盘已记录的绕界与被否决提议，不虚构原始开发对话 |

N02 原始材料本轮读取位置：`/Users/oopslink/works/research/agent-harness-study/run-001/identity-007/evidence/N02-E01.json`、`N02-E02.json`。公开稿件引用 P3 的相应章节，不把本机路径当作可公开访问的证据入口。

原有登录修复、候选 A/B、成本阈值等说明性情境继续保留“假设”或“示例”标记，用来解释机制，不承担实际案例的证据角色。后文对同一案例的展开补充代码、时间线、表格和反思，开头负责交代读者能理解的现象。

## 原始入口

- 04 历史补证（2026-11-05）：引入提交 `25d46144c919f6d81e3056dab6571aab3226e854`；删除提交 `7a9b12a9a8e550c9d06ce815f294318dc28cc0a4`；删除前快照 `2b6af1de45b1157425ea234b4dcc716454a7e726` 的 `internal/agent/work_item.go`、`internal/projectmanager/service/work_item_projector.go` 和 `task_status_sync_projector.go`。支持工作项是跨多轮的逻辑承接记录、转派取代旧项、两个状态机双向联动及完成映射的论述。历史代码不代表当前实现。
- [ADR-0048：暂停与恢复](../../design/decisions/0048-pause-resume-recovery.md)：Task running / WorkItem paused 是合法组合；T101 是 `agent_busy` 被 UI 泛化的问题，不作为状态同步失败的例子。
- [Task 服务中的撤销说明](../../../internal/agent/service/appservices.go)、[Task 模型](../../../internal/projectmanager/task.go)、[可运行/已分配读取面](../../../internal/projectmanager/service/reads.go)。
- [模型路由函数](../../../internal/agentruntime/modelrouter/router.go)、[Judge](../../../internal/agentruntime/orchestrator/judge.go)、[运行时装配与 fork](../../../internal/agentruntime/executor_runtime.go)、[并发池](../../../internal/agentruntime/executor/pool.go)。
- [Identity](../../../internal/identity/identity.go)、[Member](../../../internal/identity/member.go)、[Agent](../../../internal/agent/agent.go)、[LocalRuntime](../../../internal/agentruntime/local_runtime.go)。
- [Plan 依赖边](../../../internal/projectmanager/plan_dag.go)、[拓扑编辑事务](../../../internal/projectmanager/service/plan_topology_edit.go)。
- [Memory 作用域](../../../internal/cognition/memory/path.go)、[团队快照与刷新语义](../../../internal/cognition/memory/centergit/consumer.go)。
- [五篇论文总入口](../reports/agent-harness-papers-010.md)、[P1](../reports/papers-010/p1-logical-agent-identity/paper.md)、[P2](../reports/papers-010/p2-workflow-responsibility/paper.md)、[P3](../reports/papers-010/p3-role-shared-context/paper.md)、[P4](../reports/papers-010/p4-dag-evolution/paper.md)、[P5](../reports/papers-010/p5-memory-applicability/paper.md)。

## 正式插画与数据图表

篇次调整后，图片文件名继续使用稳定的素材编号；例如 `02-task-*` 归属现第 4 篇，`04-workflow-*` 归属现第 5 篇，`05-identity-*` 归属现第 2 篇。素材名前缀不再代表当前阅读顺序，正文、目录和审阅页以新篇次为准。

2026-11-05 配图修订：正式稿使用 26 张 Guizang 材质风格 PNG，统一为 Q 版人形机器人与丰富协调的配色。数值按下表及正文来源录入；说明性成本算例标明假设。费用与读取分布采用数字表，以避免生成式柱形比例失真。审阅页 `figure-gallery.html` 展示全部 31 张正文图片；制作过程和检查见 [material-illustrations/README.md](material-illustrations/README.md)。旧版 `figures/` 保留程序绘制的 SVG/PNG 及联系表，仅作早期制图存档。

| 图片 | 类型 | 数据或来源 |
|---|---|---|
| [02-task-journey.png](material-illustrations/assets/02-task-journey.png) | 说明性场景 | 登录修复的示例路径；等待、恢复和必要时转派，不是完整状态机或真实事件记录 |
| [02-workitem-origin.png](material-illustrations/assets/02-workitem-origin.png) | 历史模型示意 | 删除前 WorkItem 模型；任务与先后承接者为机制示意，非真实运行记录 |
| [02-task-boundaries.png](material-illustrations/assets/02-task-boundaries.png) | 架构示意 | Task / 读取门禁 / NotifyWorkAvailable / SpawnExecutor |
| [03-model-routing.png](material-illustrations/assets/03-model-routing.png) | 分支图 | Router.ResolveExecutor 函数体；显式默认优先于池首项 |
| [04-workflow-results.png](material-illustrations/assets/04-workflow-results.png) | 数据图 | P2 两角色12/12实现、12/12交付；四角色12/12实现、11/12交付；新增决策控制端52、工作端39 |
| [05-context-coverage.png](material-illustrations/assets/05-context-coverage.png) | 数据图 | P3 表7：个人3/8、同生产者角色2/8、全部获准8/8、当前范围8/8足以处置 |
| [06-two-lineages.png](material-illustrations/assets/06-two-lineages.png) | 概念示意 | 任务依赖与候选继承为两条关系；不表示图编辑服务会操作 Git |
| [07-memory-phases.png](material-illustrations/assets/07-memory-phases.png) | 数据图 | P5 表4：中间交接4/4、3/4、4/4、4/4；最终暂缓均4/4 |
| [08-evidence-levels.png](material-illustrations/assets/08-evidence-levels.png) | 方法图 | 依据仓库约束和本文工程主张整理，无实验计量含义 |
| [04-price-change.png](material-illustrations/assets/04-price-change.png) | 同组均价增幅 | P2 表 9，两角色与四角色各格均价之比减一 |
| [06-topology-outcomes.png](material-illustrations/assets/06-topology-outcomes.png) | 父子快照转移 | P4 表 5a，101/166 与 1/3 区间有共有任务转完成 |
| [07-memory-reads.png](material-illustrations/assets/07-memory-reads.png) | 去重正文回执 | P5 §4.2，执行125、计划38、审查40、恢复27 |

## 新增配图的证据与用途

| 图片 | 类型 | 来源与边界 |
|---|---|---|
| [02-state-sync.png](material-illustrations/assets/02-state-sync.png) | 历史机制 | 删除前两条事件投影；图中概括双向同步；Task 完成时按状态收尾的映射见正文 |
| [02-recovery-trace.png](material-illustrations/assets/02-recovery-trace.png) | 诊断路径 | 正文逐段核对建议及 P1 显式分派责任案例；不是一条完整原始 trace |
| [03-execution-lanes.png](material-illustrations/assets/03-execution-lanes.png) | 单个 Agent 内部运行分工 | LocalRuntime / executor_runtime 的委派、选模与回传；外框为同一逻辑 Agent，Supervisor 与 Executor 不是独立团队成员 |
| [03-cost-threshold.png](material-illustrations/assets/03-cost-threshold.png) | 假设曲线 | 正文算例 C直接=10、C路由=5+10p；p=50% 持平，非实测 |
| [04-handoff-contract.png](material-illustrations/assets/04-handoff-contract.png) | 协议建议 | 候选、依据、证据、剩余义务；未声称所有字段已落地 |
| [04-review-loop.png](material-illustrations/assets/04-review-loop.png) | 案例复盘 | P2 表10：实现8/8，自编测试6/6，7张成功回执，4次审查启动，集成0次 |
| [05-identity-map.png](material-illustrations/assets/05-identity-map.png) | 边界图 | Identity / Member / Agent 模型及当前任务上下文职责 |
| [05-context-selection.png](material-illustrations/assets/05-context-selection.png) | 说明性示例 | 正文候选 A/B 假设情境；筛选步骤是设计建议 |
| [06-edit-transaction.png](material-illustrations/assets/06-edit-transaction.png) | 实现流程 | EditPlanTopology 的基线、操作副本、最终验证与事务提交 |
| [06-repair-triage.png](material-illustrations/assets/06-repair-triage.png) | 方法建议 | 正文三种缺口处置；未验证该停止/修复规则的实验收益 |
| [07-memory-pipeline.png](material-illustrations/assets/07-memory-pipeline.png) | 机制与检查 | Memory 路径、团队发布与快照；行为核对是评估要求 |
| [07-rule-deadlock.png](material-illustrations/assets/07-rule-deadlock.png) | 案例机制 | P5 无条件 main 规则与候选先交接再验收的冲突；非原始事件逐条重放 |
| [08-development-loop.png](material-illustrations/assets/08-development-loop.png) | 实践归纳 | doc-first、领域边界与测试规约；每轮回到原症状验收 |
| [08-review-packet.png](material-illustrations/assets/08-review-packet.png) | 建议模板 | 正文任务输入模板；非聊天截图或产品页面 |

视觉口径：暖白陶瓷与纸张材质，蓝、珊瑚橙、薄荷绿、柔黄、淡紫辅助区分角色和对象；不以颜色作为唯一编码。图中使用短标签，正文与图注交代来源、样本分母、单位和结论边界，区分历史模型、机制概括、实验数据、案例归纳与方法建议。原页面截图、原论文图保持原文件。

## 直接引用的留存图片

- `assets/screenshots/web-console-work-board.png`：04文章中的真实页面，已查看；留存截图，不标作本轮当前部署截图。
- `assets/screenshots/web-console-plan-dag.png`：06文章中的真实页面，已查看；同上。
- P1 `figures/p1-fig2-responsibility-timeline.png`：03引用原论文图，图注保留原生事件与Agent报告的区别。
- P2 `figures/p2-fig7-paired-decisions.png`：05引用原论文图，展示全部12个配对，已查看。
- P4 `figures/p4-fig3-artifact-lineage.png`：06引用原论文图，展示代际链与实际 Git 谱系差别，已查看。

## 发布使用说明

Markdown 中相对图片链接供本地预览和仓库浏览。知乎编辑器应上传对应 PNG，Mermaid 可作为补充源图；真实页面和论文图的原始文件保留在各自目录。正文的 GitHub 链接是可读源码入口，精确复核应结合上述本地文件及当时工作树版本。
