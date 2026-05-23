> ⚠ **v1-era doc** — pending v2 update. v2 撤回了 Bridge BC + 飞书集成 (per [ADR-0031](decisions/drafts/0031-v2-drop-bridge-vendor-integration.md))；ADR-0017/0021/0022 superseded by [ADR-0039](decisions/drafts/0039-conversation-business-model-v2-unified.md). 本文中 Bridge / vendor / 飞书 引用是 v1 残留。

# agent-center DDD 设计蓝图（Blueprint + Plan & Status）

> 本文档是 DDD 设计推进的 **living plan & status**。每次跟 DDD 相关的讨论 / 设计开始前先过一遍这里，定位"哪里到了 / 下一步做啥"。
>
> 顶层方法论约定见 [conventions § 0](../rules/conventions.md#-0-设计方法论ddd--统一语言)。
>
> 跟 [roadmap.md](roadmap.md) 区别：roadmap 是"v1 不做 / 推迟功能"的功能维度 plan；本文档是"DDD 设计深度"的方法论维度 plan。

最后更新：2026-05-20（立 [ADR-0021](decisions/drafts/0039-conversation-business-model-v2-unified.md) Issue ↔ Conversation 1:1：IssueComment 删独立表 = Message，Issue 跟 Task 路线对称；推翻 [ADR-0009](decisions/drafts/0031-v2-drop-bridge-vendor-integration.md) § 1 解耦 + § 3 Bound Card 字段；同日把 [ADR-0020](decisions/drafts/0031-v2-drop-bridge-vendor-integration.md) 中间方案 supersede；落地 P3 Discussion BC 重组 + Conversation/Bridge/Strategic 跟随更新；**P3 全部完成 7/7** + **P5/P6/P7 中优全部完成** + **P8a/P8b 低优 Repository 全链路完成** ✅：DDD 战术 + 中优架构清晰度 + Repository 架构-实现层全部收口（剩 P9 Saga 视需要））。

---

## § 1. 状态总览

按 DDD 三层（战略 / 战术 / 高级模式）盘点。✅ 完成 / ⚠️ 部分 / ❌ 缺。

### 1.1 战略设计（Strategic）

| 概念 | 状态 | 位置 / 说明 |
|---|---|---|
| **Bounded Context** | ✅ | [03-bounded-contexts § 2](architecture/strategic/03-bounded-contexts.md#-2-限界上下文bounded-contexts)，7 个 BC（TaskRuntime / Discussion / Workforce / Cognition / Observability / Conversation / ~~Bridge~~（v2 删 per ADR-0031）；BC1 Scheduling + BC4 Execution 合并为 BC1 TaskRuntime，详 [ADR-0019](decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md)）|
| **Ubiquitous Language** | ✅ | [03-bounded-contexts § 1](architecture/strategic/03-bounded-contexts.md#-1-通用语言ubiquitous-language)；conventions § 0 立"统一语言"为方法论根基 |
| **Context Map** | ✅ | [03-bounded-contexts § 3](architecture/strategic/03-bounded-contexts.md#-3-上下文映射context-map)（图 + 上下游表） |
| **Subdomain 标注**（Core / Supporting / Generic）| ✅ | [01-subdomain-classification](architecture/strategic/01-subdomain-classification.md)：Core 3 / Supporting-Essential 2 / Supporting-Peripheral 2 / Generic 0 |
| **Big Picture / Vision** | ✅ | [00-domain-vision](architecture/strategic/00-domain-vision.md)（DDD Domain Vision Statement：thesis + 4 stance + 2 boundary）|
| **架构层物理分层** | ✅ | architecture/ 拆 strategic/ + tactical/{BC}/ + tactical/{theme}/（agent-harness / presentation；2026-05-18）；详见 [架构 README](architecture/README.md) |

### 1.2 战术设计（Tactical）

| 概念 | 状态 | 位置 / 说明 |
|---|---|---|
| **Entity** | ✅ | P3 各 BC § 1.2 子从属 Entity 表已明示（Artifact / Message / WorkerProjectMapping / DecisionRecord）+ 各 AR 自己是 Entity；字段表在各聚合文档内 |
| **Value Object** | ✅ | P3 各 BC § 1.3 VO 表完成（含 reason+message pair / DispatchEnvelope / WorkspaceMode / Priority / IssueResolution / ChannelBinding / TokenUsage / ScopeKey / EventType / etc.）|
| **Aggregate**（含 Invariants）| ✅ | P3 各 BC § 1.1 AR 表 + § 2 Invariants 索引 / 各 AR 文件 Invariants 节完成（含 Task / TaskExecution / InputRequest / Issue / Worker / Project / WorkerProjectProposal / SupervisorInvocation / Memory / Conversation / Identity / Event）|
| **Aggregate Root** | ✅ | P3 各 BC § 5 Repositories 节"约定"栏明示"外部只通过 Root.id 引用"约束；[conventions § 0.3](../rules/conventions.md#-03-自检清单) 加 AR 访问自检（P6） |
| **Domain Event** | ✅ 充分 | [03-bounded-contexts § 2](architecture/strategic/03-bounded-contexts.md#-2-限界上下文bounded-contexts) 各 BC 列；[observability/00-overview § 7.5](architecture/tactical/observability/00-overview.md) 事件总览；ADR-0014（事件溯源 L1）/ ADR-0015（agent_trace 不进 events）；[conventions § 16](../rules/conventions.md#-16-错误--状态信息双字段reason--message) reason+message |
| **Domain Service** | ✅ | P3 各 BC § 3 完成（P5）：TaskRuntime (Dispatch/Reconcile/Timeout/IssueConcludeSpawn/Kill) / Discussion (IssueLifecycle/IssueConcludeSpawn-caller) / Workforce (WorkerEnroll/ProposalDiscovery/ProposalReview/MappingInvalidation) / Cognition (WakeScheduler/InvocationFactory/InvocationTimeoutHandler/InvocationCrashRecovery/DecisionWriter) / Observability (EventSink/Projection/Query/TraceArchive) / Conversation (ConversationLifecycle/IdentityRegistration/~~BridgeInbound-caller~~) / ~~Bridge (OutboundDelivery/InboundRouting/SlashCommand/CardLifecycle)~~ (v2 删 Bridge per ADR-0031) |
| **Repository** | ✅ 架构 + 实现层均完成 | P3 + P8a 各 BC § 5（2026-05-20）：Go-style 接口签名（含 `ctx context.Context`）+ domain error types（sentinel error pattern，BC 前缀）全部列出；7 BC 共 ~22 个 Repository 接口。P8b 实现层（2026-05-20）：[implementation/02-persistence-schema.md](implementation/02-persistence-schema.md) 立 SQLite-only / ULID / version CAS / tx via ctx / golang-migrate 等元层规则 + 代表性 BC 切片 |
| **Factory** | ✅ | P3 各 BC § 4 Factories 节完成：TaskRuntime (TaskFactory 5-caller / TaskExecutionFactory / InputRequestFactory) / Discussion (IssueFactory 5-caller / IssueCommentFactory-facade / ConversationFactory-caller) / Workforce (Worker/Proposal/Project/MappingFactory) / Cognition (InvocationFactory / MemorySkeletonFactory) / Conversation (ConversationFactory 6-caller / MessageFactory / IdentityFactory) / Observability (无独立 Factory，各 BC 自 emit) / ~~Bridge (无，仅调领域 API)~~ (v2 删 Bridge per ADR-0031) |
| **跨聚合一致性策略** | ✅ 已散立 | ADR-0014 § 2 显式"tx 内同时改两聚合"；ADR-0017 + ADR-0021 显式跨聚合双写（task + conversation / issue + conversation）；P3 各 BC § 6 跨聚合引用 + § 7 跨 BC 交互详化 —— **决定不再立通用 ADR**（详见 § 3.4） |

### 1.3 高级模式（Strategic Patterns）

| 概念 | 状态 | 位置 / 说明 |
|---|---|---|
| **Anti-Corruption Layer**（ACL）| ✅ | [03-bounded-contexts § 3.2](architecture/strategic/03-bounded-contexts.md#32-anti-corruption-layers)，3 处（~~Bridge~~（v2 删 per ADR-0031）/ Agent CLI Adapter / BlobStore Adapter） |
| **Shared Kernel** | ✅ | [03-bounded-contexts § 3.1](architecture/strategic/03-bounded-contexts.md#31-上下游关系一览) 上下游表 + P3 各 BC § 6 / § 7：Discussion↔Conversation 1:1（[ADR-0021](decisions/drafts/0039-conversation-business-model-v2-unified.md)）/ TaskRuntime↔Conversation 1:1（[ADR-0017](decisions/drafts/0039-conversation-business-model-v2-unified.md)）/ TaskRuntime↔Workforce / Discussion↔Workforce |
| **Customer-Supplier** | ✅ | 03-bounded-contexts § 3.1 上下游表 + P3 各 BC § 7.4 Customer-Supplier 汇总 |
| **Open Host Service** | ✅ | [03-bounded-contexts § 6 Published Language](architecture/strategic/03-bounded-contexts.md) 完成（P7）：events 表 schema + CLI 命令构成 PL；Observability BC 是 PL 订阅方 |
| **Published Language** | ✅ | [03-bounded-contexts § 6](architecture/strategic/03-bounded-contexts.md) 明示：events schema + CLI 双层；演进策略 + 不属于 PL 的清单 + 自检 |
| **Saga / Process Manager** | ❌ | 未讨论（Issue → Task spawn → Execution 链是事件驱动 + 状态机协同；如果未来撞协调问题再立，P9）|
| **CQRS / Read Model** | ✅ | ADR-0014 明示驳回 CQRS；读模型存在（[observability/00-overview § 1.4 Read Models](architecture/tactical/observability/00-overview.md)）|

---

## § 2. 已完成的设计要素（详细）

### 2.1 跟 DDD 直接相关的 ADR

| ADR | 主题 | DDD 维度 |
|---|---|---|
| [0007](decisions/0007-conversation-as-unified-session.md) | Conversation 作为统一会话层 | 定 Conversation BC + 聚合 |
| ~~0009~~ (deleted per [ADR-0031](decisions/drafts/0031-v2-drop-bridge-vendor-integration.md)) | Issue ↔ Conversation 解耦 | ~~聚合边界 / Bridge ACL~~ (v2 删 Bridge per ADR-0031) |
| [0010](decisions/0010-task-execution-two-layer-model.md) | Task / TaskExecution 两层模型 | 聚合分层 + 实体身份 |
| [0011](decisions/0011-dispatch-reliability-protocol.md) | 派单可靠性协议 | Domain Service (Dispatch) 协议 |
| [0012](decisions/0012-memory-file-based.md) | Memory file-based | Memory 聚合的物理形态选择 |
| [0013](decisions/0013-supervisor-invocation-concurrency.md) | Supervisor 并发模型 | SupervisorInvocation 聚合并发约束 |
| [0014](decisions/0014-event-sourcing-level.md) | 事件溯源走 L1 | Domain Event 策略 + 跨聚合事务原则 |
| [0015](decisions/0015-agent-trace-not-in-events-table.md) | agent_trace 不进 events 表 | Domain Event vs trace 数据的区分 |
| [0017](decisions/drafts/0039-conversation-business-model-v2-unified.md) | Task ↔ Conversation 1:1 | 跨 BC / 跨聚合强引用 + 同事务双写 |
| [0018](decisions/0018-detached-agent-via-per-execution-shim.md) | Detached agent + per-execution shim 模型 | TaskRuntime BC 内新运行时角色（Shim）；影响 UL（待回填到 03-bounded-contexts § 1.1）|
| [0019](decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md) | BC1 + BC4 合并为 TaskRuntime | 战略级 BC 边界调整：8→7 BC；P3 模板升级为聚合骨架重组 |
| ~~0020~~ (deleted per [ADR-0031](decisions/drafts/0031-v2-drop-bridge-vendor-integration.md)) | ~~Card 限制在 Bridge BC（中间方案）~~ (v2 删 Bridge per ADR-0031) | **Superseded by 0021**（同日升级为统一方案；本 ADR 决策不实施）|
| [0021](decisions/drafts/0039-conversation-business-model-v2-unified.md) | Issue ↔ Conversation 1:1，统一 Issue/Task 模式 | 推翻 ADR-0009 § 1 + § 3 + ADR-0020：Issue ↔ Conversation 1:1（kind=issue），IssueComment 删独立表（= Message），Bound Card 概念消失，Issue 跟 Task 路线完全对称；Refines ADR-0017 |

### 2.2 已完成的架构文档（DDD 视角）

| 文档 | 层 | DDD 内容 |
|---|---|---|
| [00-domain-vision](architecture/strategic/00-domain-vision.md) | 战略 | Domain Vision Statement（thesis + strategic stance + boundary）|
| [01-subdomain-classification](architecture/strategic/01-subdomain-classification.md) | 战略 | 7 BC 的 Core/Supporting/Generic 分类 + Essential/Peripheral sub-label + 投入策略 |
| [03-bounded-contexts](architecture/strategic/03-bounded-contexts.md) | 战略 | BC + UL + Context Map + ACL 识别 |
| [task-runtime/00-overview](architecture/tactical/task-runtime/00-overview.md) | 战术 (BC1) | TaskRuntime BC 入口：聚合清单 + Domain Service + Factory + Repo + 跨聚合引用 |
| [task-runtime/01-task](architecture/tactical/task-runtime/01-task.md) | 战术 (BC1) | Task 聚合（状态机 / 字段 / 依赖 / Conversation 绑定 / Invariants） |
| [task-runtime/02-task-execution](architecture/tactical/task-runtime/02-task-execution.md) | 战术 (BC1) | TaskExecution 聚合（状态机 / 字段 / Workspace / worker 运行时 / kill 进程级 / Artifact / Invariants） |
| [task-runtime/03-input-request](architecture/tactical/task-runtime/03-input-request.md) | 战术 (BC1) | InputRequest 聚合 + 协议 + 三响应路径 + fallback + Invariants |
| [discussion/00-overview](architecture/tactical/discussion/00-overview.md) | 战术 (BC2) | Issue 聚合（单聚合 BC，IssueComment 已删，议事走 Conversation Message，[ADR-0021](decisions/drafts/0039-conversation-business-model-v2-unified.md)）+ § X.1-X.6 wrap |
| [workforce/00-overview](architecture/tactical/workforce/00-overview.md) | 战术 (BC3) | Workforce BC 入口 + § X.1-X.6 wrap（Worker / WorkerProjectProposal / Project 三聚合；按 [ADR-0019](decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md) carve）|
| [workforce/01-worker](architecture/tactical/workforce/01-worker.md) | 战术 (BC3) | Worker 聚合（AR） + WorkerProjectMapping（Entity，子从属）|
| [workforce/02-project](architecture/tactical/workforce/02-project.md) | 战术 (BC3) | Project 聚合（独立 AR）|
| [workforce/03-worker-project-proposal](architecture/tactical/workforce/03-worker-project-proposal.md) | 战术 (BC3) | WorkerProjectProposal 聚合（独立 AR）+ 4 态状态机 |
| [cognition/00-overview](architecture/tactical/cognition/00-overview.md) | 战术 (BC4) | Cognition BC 入口 + § X.1-X.6 wrap（SupervisorInvocation + Memory 两聚合）|
| [cognition/01-supervisor-invocation](architecture/tactical/cognition/01-supervisor-invocation.md) | 战术 (BC4) | SupervisorInvocation AR + DecisionRecord 子从属（4 态状态机 / WakeScheduler / 决策审计）|
| [cognition/02-memory](architecture/tactical/cognition/02-memory.md) | 战术 (BC4) | Memory AR（file-based + git 仓；7 种 scope；ancestor walk）|
| [observability/00-overview](architecture/tactical/observability/00-overview.md) | 战术 (BC5) | Observability BC 入口 + § X.1-X.6 wrap（Event AR + 读模型 projections + 5 个 CLI 动词 + Fleet View）|
| [conversation/00-overview](architecture/tactical/conversation/00-overview.md) | 战术 (BC6) | Conversation BC 入口 + § X.1-X.6 wrap（Conversation + Identity 两聚合）|
| [conversation/01-conversation](architecture/tactical/conversation/01-conversation.md) | 战术 (BC6) | Conversation AR + Message 子从属（6 种 kind + 6 种 content_kind + Invariants）|
| [conversation/02-identity](architecture/tactical/conversation/02-identity.md) | 战术 (BC6) | Identity AR + ChannelBinding 子 VO（v1 单用户简化 + 自动绑定）|
| ~~tactical/bridge/* (v2 deleted per ADR-0031)~~ | 战术 (BC7) | ~~Bridge BC 入口 + § X.1-X.6 wrap（**无业务聚合 ACL**；OutboundDelivery / InboundRouting / SlashCommand / CardLifecycle 四 svc）~~ |
| ~~tactical/bridge/* (v2 deleted per ADR-0031)~~ | 战术 (BC7) | ~~FeishuBridge vendor-specific 实现（一次性设置 / 网络方向 / Inbound / Outbound / 渲染规则 / 三模式 D1-D3）~~ |

### 2.3 已立的横切方法论

| 规则 | 内容 |
|---|---|
| [conventions § 0](../rules/conventions.md#-0-设计方法论ddd--统一语言) | DDD 是方法论根基；统一语言以 03-bounded-contexts § 1 为权威 |
| [conventions § 12](../rules/conventions.md#-12-命名一致) | 代码 / 文档 / CLI / event_type / 表名 同一组词 |
| [conventions § 16](../rules/conventions.md#-16-错误--状态信息双字段reason--message) | reason + message 双字段约定（事实上的 ReasonMessage VO 雏形） |

---

## § 3. 待完成（Plan）

### 3.1 高优（影响实现，应近期推进）

#### P3. 按聚合骨架重组每个 BC 战术文档 + 补 § X.1-X.6 wrap

**目的**：DDD 战术设计按**聚合**（不按主题）组织内容，每个 BC 自治讲。取消历史的"主题切"骨架（dispatch / kill / workspace / retry / timeout）—— 这是 BC1+BC4 leaky 的根源（详 [ADR-0019](decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md)）。

**文件组织**：

每个 BC 在 `tactical/{bc}/` 下组织：

- `00-overview.md`：BC 入口；承载 § X.1 聚合清单 + § X.2 Invariants 索引 + § X.3 Domain Service + § X.4 Factory + § X.5 Repository + § X.6 跨聚合引用 + 跨 BC 交互 + Out-of-Scope + References
- `0N-{aggregate}.md`：每聚合一份；内容组织为「聚合状态机 → 字段 → lifecycle ops → Invariants」

**按 BC 体量规则**：

- **多聚合 BC（≥2 聚合）**：`00-overview.md` + `0N-{aggregate}.md` 多文件
- **单聚合 BC**：`00-overview.md` 内合并聚合详情（不另起 01-）
- **无业务聚合 BC**（~~如 Bridge — v2 删 per ADR-0031~~）：仅 `00-overview.md`，说明"无聚合 / 仅 ACL 翻译职责"

**组织反例（明令禁止）**：

- 不按"主题"切（dispatch / kill / workspace / retry / timeout）—— BC1+BC4 leaky 根源
- 不按"协议层 / 实现层"切 —— ADR-0019 已驳回此切分线

**覆盖范围**（按推进顺序）：

| # | BC | 文件 | 聚合数 / 文件数 | 现状 |
|---|---|---|---|---|
| 1 | TaskRuntime | `tactical/task-runtime/00-overview.md` + `01-task.md` + `02-task-execution.md` + `03-input-request.md` | 3 / 4 | ✅ 完成（[ADR-0019](decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md)） |
| 2 | Discussion | `tactical/discussion/00-overview.md` | 1 / 1（单聚合，聚合详情合并） | ✅ 完成（[ADR-0021](decisions/drafts/0039-conversation-business-model-v2-unified.md)：删 IssueComment 实体；议事走 Conversation Message） |
| 3 | Workforce | `tactical/workforce/00-overview.md` + `01-worker.md` + `02-project.md` + `03-worker-project-proposal.md` | 3 / 4 | ✅ 完成（Worker / Project / Proposal 三聚合；WorkerProjectMapping 子从属于 Worker） |
| 4 | Cognition | `tactical/cognition/00-overview.md` + `01-supervisor-invocation.md` + `02-memory.md` | 2 / 3 | ✅ 完成（SupervisorInvocation + DecisionRecord 子从属 + Memory）|
| 5 | Observability | `tactical/observability/00-overview.md` | 1 / 1（单聚合 BC，Event AR + 读模型 projections）| ✅ 完成 |
| 6 | Conversation | `tactical/conversation/00-overview.md` + `01-conversation.md` + `02-identity.md` | 2 / 3 | ✅ 完成（Conversation + Message 子从属 + Identity + ChannelBinding 子 VO）|
| ~~7~~ | ~~Bridge~~ | ~~`tactical/bridge/00-overview.md` + `01-feishu-integration.md` (v2 deleted per ADR-0031)~~ | ~~0 / 2~~ | ~~v2 删 per ADR-0031~~ |

**跨聚合视角**：不单立 `00-domain-model.md`。跨 BC 引用关系仍归 [strategic/03-bounded-contexts § 3 上下文映射](architecture/strategic/03-bounded-contexts.md#-3-上下文映射context-map)，在 § 3.1 上下游表里补"引用基数 / 强弱 / 一致性窗口"列。

**依赖**：无（直接做）。
**影响范围**：7 个 BC 目录；strategic/03-bounded-contexts § 3.1 表扩列。

### 3.2 中优（架构清晰度）— 全部 ✅ 完成（2026-05-20）

| ID | 项 | 产出形态 | 状态 |
|---|---|---|---|
| **P5** | **Domain Service 明示** | P3 § X.3 各 BC 文档完成（详见 § 1.2 Domain Service 行）| ✅ |
| **P6** | **Aggregate Root 访问约束**写明 | [conventions § 0.3](../rules/conventions.md#-03-自检清单) 加 AR 访问自检；P3 各 BC § 5 Repositories 节"约定"栏明示 | ✅ |
| **P7** | **Published Language** 明示 | [strategic/03-bounded-contexts § 6](architecture/strategic/03-bounded-contexts.md) 加 Published Language 节（events schema + CLI 命令双层 + 演进策略 + 自检）| ✅ |

### 3.3 低优 / 视需求 — P8 全部完成 ✅

| ID | 项 | 时机 |
|---|---|---|
| **P8a** | **Repository 接口签名 + Domain Error types** | ✅ 完成（2026-05-20）：7 个 BC § 5 全部扩为 Go-style 接口签名 + sentinel error pattern domain errors |
| **P8b** | **Repository 实现层（SQL schema + dialect + tx 传递）** | ✅ 完成（2026-05-20）：[implementation/02-persistence-schema.md](implementation/02-persistence-schema.md) 首版 —— SQLite-only / ULID / version CAS / WithTx-TxFromCtx / golang-migrate；代表性 BC（TaskRuntime + Observability）DDL 展开；conventions § 9.2 同步加 v1 SQLite-only 例外 |
| P9 | **Saga / Process Manager** | 等真撞协调问题（v1 不必） |

> **2026-05-20 修订**：原 P8 把 Repository 接口签名跟 implementation 层 SQL schema 选型耦合是错的（违反 hexagonal architecture / DDD 经典分层）。Repository 接口签名 + domain error types **归领域 / 架构层**（各 BC § 5 Repositories 节扩列）；SQL schema / dialect 适配 / tx context 传递机制等才归 implementation 层。详见 [§ 1.2 Repository 行说明](#12-战术设计tactical)。

### 3.4 已驳回的方法论方向（决议留痕）

> **注**：下列两项原拟用 ADR-0019 / ADR-0020 编号，但 2026-05-19 ADR-0019 被实际占用于 BC 合并（[ADR-0019](decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md)）。当前下列"驳回项"无 ADR 号。

- ~~**P1. 跨聚合引用与一致性策略 拟 ADR**~~（2026-05-18 驳回；曾拟 ADR-0019，现 0019 已另用）
  - **理由**：拟列的 5 条规则里，2 条是 DDD 教科书条款（Root 守门、ID 引用，无须 ADR）；2 条已被 ADR-0014 § 2 / § 5 立过（同事务双写 / 驳回 CQRS）；剩下 1 条（强引用 vs 弱关联）是 design space 不是规则。
  - **替代**：跨聚合关系在各 BC 文档 § X.6 就地讨论；strategic/03-bounded-contexts § 3.1 扩列承载全局视角。
  - **方法论教训**：抽通用规则前先盘具体场景；证据少 / 已立过 / 例外多 → 不抽。
- ~~**P2. Value Object 系统化 拟 ADR**~~（2026-05-18 驳回；曾拟 ADR-0020）
  - **理由**：VO 候选清单是具体的，但"VO 实现约束"作为方法论 ADR 在落代码前太抽象；候选 VO 直接在 P3 § X.1 三栏盘点时随手命名 + 标角色更高效。
  - **替代**：P3 中各 BC 文档 § X.1 直接给 VO 角色标签；落代码时若发现 Go struct 层需要统一的 VO 约束，再考虑写 ADR。

---

## § 4. 推进路径建议

```
P3 (按聚合骨架重组各 BC 战术文档 + 补 § X.1-X.6 wrap)
  └─→ 7 个 BC 目录分别重组（00-overview + 0N-{aggregate}.md）
        └─→ 顺手 strategic/03-bounded-contexts § 3.1 扩列（跨聚合一致性窗口）
              └─→ P5/P6 自然落入 P3 § X.1/§ X.3
                    └─→ P7（Published Language）独立短节
```

**A.** ✅ 已完成（7 个 BC 全部 P3 落地，2026-05-19 ~ 2026-05-20）
**B.** ✅ 已完成（每个 BC 单独 commit，TaskRuntime 含 strategic + tactical 两 commit；其它 BC 多以 1 个 commit 完成）
**C.** ⚠️ 待跟进（strategic/03-bounded-contexts § 3.1 上下游表扩列"引用基数 / 强弱 / 一致性窗口" 列）—— 各 BC § X.6 已实质承载，待集中收口

低优 P8 全部完成 ✅（P8a 接口签名 / P8b 实现层）；P9 Saga 视需要。

---

## § 5. 引用文档

- 方法论：[conventions § 0](../rules/conventions.md#-0-设计方法论ddd--统一语言)
- 架构层索引（含分层说明）：[architecture/README.md](architecture/README.md)
- 通用语言：[strategic/03-bounded-contexts § 1](architecture/strategic/03-bounded-contexts.md#-1-通用语言ubiquitous-language)
- 限界上下文：[strategic/03-bounded-contexts § 2](architecture/strategic/03-bounded-contexts.md#-2-限界上下文bounded-contexts)
- 上下文映射：[strategic/03-bounded-contexts § 3](architecture/strategic/03-bounded-contexts.md#-3-上下文映射context-map)
- 事件总览：[tactical/observability/01-observability](architecture/tactical/observability/00-overview.md)
- Invariants 系统化样板：[tactical/task-runtime/02-task-execution § 13](architecture/tactical/task-runtime/02-task-execution.md)（按 ADR-0019 升级模板，TaskExecution Invariants 节固定 § 13）
- 跟 DDD 相关 ADR：§ 2.1
