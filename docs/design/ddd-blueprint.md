# agent-center DDD 设计蓝图（Blueprint + Plan & Status）

> 本文档是 DDD 设计推进的 **living plan & status**。每次跟 DDD 相关的讨论 / 设计开始前先过一遍这里，定位"哪里到了 / 下一步做啥"。
>
> 顶层方法论约定见 [conventions § 0](../rules/conventions.md#-0-设计方法论ddd--统一语言)。
>
> 跟 [roadmap.md](roadmap.md) 区别：roadmap 是"v1 不做 / 推迟功能"的功能维度 plan；本文档是"DDD 设计深度"的方法论维度 plan。

最后更新：2026-05-20（立 [ADR-0021](decisions/0021-issue-as-conversation.md) Issue ↔ Conversation 1:1：IssueComment 删独立表 = Message，Issue 跟 Task 路线对称；推翻 [ADR-0009](decisions/0009-issue-conversation-decoupled-via-bridge.md) § 1 解耦 + § 3 Bound Card 字段；同日把 [ADR-0020](decisions/0020-card-confined-to-bridge-bc.md) 中间方案 supersede）。

---

## § 1. 状态总览

按 DDD 三层（战略 / 战术 / 高级模式）盘点。✅ 完成 / ⚠️ 部分 / ❌ 缺。

### 1.1 战略设计（Strategic）

| 概念 | 状态 | 位置 / 说明 |
|---|---|---|
| **Bounded Context** | ✅ | [03-bounded-contexts § 2](architecture/strategic/03-bounded-contexts.md#-2-限界上下文bounded-contexts)，7 个 BC（TaskRuntime / Discussion / Workforce / Cognition / Observability / Conversation / Bridge；BC1 Scheduling + BC4 Execution 合并为 BC1 TaskRuntime，详 [ADR-0019](decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md)）|
| **Ubiquitous Language** | ✅ | [03-bounded-contexts § 1](architecture/strategic/03-bounded-contexts.md#-1-通用语言ubiquitous-language)；conventions § 0 立"统一语言"为方法论根基 |
| **Context Map** | ✅ | [03-bounded-contexts § 3](architecture/strategic/03-bounded-contexts.md#-3-上下文映射context-map)（图 + 上下游表） |
| **Subdomain 标注**（Core / Supporting / Generic）| ✅ | [01-subdomain-classification](architecture/strategic/01-subdomain-classification.md)：Core 3 / Supporting-Essential 2 / Supporting-Peripheral 2 / Generic 0 |
| **Big Picture / Vision** | ✅ | [00-domain-vision](architecture/strategic/00-domain-vision.md)（DDD Domain Vision Statement：thesis + 4 stance + 2 boundary）|
| **架构层物理分层** | ✅ | architecture/ 拆 strategic/ + tactical/{BC}/ + tactical/{theme}/（agent-harness / presentation；2026-05-18）；详见 [架构 README](architecture/README.md) |

### 1.2 战术设计（Tactical）

| 概念 | 状态 | 位置 / 说明 |
|---|---|---|
| **Entity** | ⚠️ 散落 | [03-bounded-contexts § 1.1](architecture/strategic/03-bounded-contexts.md#11-核心实体按上下文归属) 标了 entity / 聚合根 / 从属；字段表散在各 BC 文档；**P3 中各 BC 文档统一收口** |
| **Value Object** | ❌ 严重缺 | 仅 `ChannelBinding` 明示为 VO；事实 VO（未标）：`DispatchEnvelope` / Reason+Message 对 / `FailedReason` taxonomy / `KilledReason` / `IssueConcludeSpec` / `DispatchAck` / `DispatchNack` / `WorkspaceMode` / `Priority` / `BlobRef` / `IdentityRef`；**P3 中各 BC 文档统一收口** |
| **Aggregate**（含 Invariants）| ⚠️ 散落 | 各 BC 列了"核心聚合"清单；**Invariants 系统化的只有 [task-runtime/02-task-execution § 13](architecture/tactical/task-runtime/02-task-execution.md)（Execution 部分；Task / InputRequest 在 01 / 03 各自 § 末尾）**；Issue / Conversation / Worker / Memory / SupervisorInvocation / WorkerProjectProposal 等的不变量未单独列；**P3 中各 BC 文档统一收口** |
| **Aggregate Root** | ⚠️ 标了不足 | 03-bounded-contexts § 1.1 标"根"，但**未明示"外部仅通过 Root.id 引用，不持有内部 Entity 句柄"** 约束 |
| **Domain Event** | ✅ 充分 | [03-bounded-contexts § 2](architecture/strategic/03-bounded-contexts.md#-2-限界上下文bounded-contexts) 各 BC 列；[observability/01-observability](architecture/tactical/observability/01-observability.md) 事件总览；ADR-0014（事件溯源 L1）/ ADR-0015（agent_trace 不进 events）；[conventions § 16](../rules/conventions.md#-16-错误--状态信息双字段reason--message) reason+message |
| **Domain Service** | ❌ 未明示 | 实际存在：Supervisor 决策（[cognition/01-supervisor-model](architecture/tactical/cognition/01-supervisor-model.md)）/ Dispatch / KillCoordinator / TimeoutScanner / ReconcileService / IssueConcludeSpawn（[task-runtime/00-overview § 3](architecture/tactical/task-runtime/00-overview.md)）—— 待 P3 中各 BC 文档 § X.3 显式标 |
| **Repository** | ❌ TBD | [implementation/02-persistence-schema.md](implementation/) 待写；conventions § 9 提了 mutator 封装是前置 |
| **Factory** | ❌ 未明示 | 实际有：Issue conclude → batch spawn N tasks / Task create / TaskExecution create / Conversation create —— 没标 "Factory" |
| **跨聚合一致性策略** | ✅ 已散立 | ADR-0014 § 2 显式"tx 内同时改两聚合"；ADR-0017 显式跨聚合双写（task + conversation）—— **决定不再立通用 ADR**（详见 § 3.4） |

### 1.3 高级模式（Strategic Patterns）

| 概念 | 状态 | 位置 / 说明 |
|---|---|---|
| **Anti-Corruption Layer**（ACL）| ✅ | [03-bounded-contexts § 3.2](architecture/strategic/03-bounded-contexts.md#32-anti-corruption-layers)，3 处（Bridge / Agent CLI Adapter / BlobStore Adapter） |
| **Shared Kernel** | ⚠️ | [03-bounded-contexts § 3.1](architecture/strategic/03-bounded-contexts.md#31-上下游关系一览) 标了 TaskRuntime↔Workforce / Discussion↔Workforce / TaskRuntime↔Conversation；未集中讨论 |
| **Customer-Supplier** | ✅ | 03-bounded-contexts § 3.1 上下游表 |
| **Open Host Service** | ⚠️ | Observability 标为 subscribe-only；**Published Language** 是事件 schema 还是 CLI 未明示 |
| **Saga / Process Manager** | ❌ | 未讨论（Issue → Task spawn → Execution 链是事件驱动 + 状态机协同；如果未来撞协调问题再立） |
| **CQRS / Read Model** | ✅ | ADR-0014 明示驳回 CQRS；读模型存在（[observability/01-observability Fleet View](architecture/tactical/observability/01-observability.md) Projection） |

---

## § 2. 已完成的设计要素（详细）

### 2.1 跟 DDD 直接相关的 ADR

| ADR | 主题 | DDD 维度 |
|---|---|---|
| [0007](decisions/0007-conversation-as-unified-session.md) | Conversation 作为统一会话层 | 定 Conversation BC + 聚合 |
| [0009](decisions/0009-issue-conversation-decoupled-via-bridge.md) | Issue ↔ Conversation 解耦 | 聚合边界 / Bridge ACL |
| [0010](decisions/0010-task-execution-two-layer-model.md) | Task / TaskExecution 两层模型 | 聚合分层 + 实体身份 |
| [0011](decisions/0011-dispatch-reliability-protocol.md) | 派单可靠性协议 | Domain Service (Dispatch) 协议 |
| [0012](decisions/0012-memory-file-based.md) | Memory file-based | Memory 聚合的物理形态选择 |
| [0013](decisions/0013-supervisor-invocation-concurrency.md) | Supervisor 并发模型 | SupervisorInvocation 聚合并发约束 |
| [0014](decisions/0014-event-sourcing-level.md) | 事件溯源走 L1 | Domain Event 策略 + 跨聚合事务原则 |
| [0015](decisions/0015-agent-trace-not-in-events-table.md) | agent_trace 不进 events 表 | Domain Event vs trace 数据的区分 |
| [0017](decisions/0017-task-as-conversation.md) | Task ↔ Conversation 1:1 | 跨 BC / 跨聚合强引用 + 同事务双写 |
| [0018](decisions/0018-detached-agent-via-per-execution-shim.md) | Detached agent + per-execution shim 模型 | TaskRuntime BC 内新运行时角色（Shim）；影响 UL（待回填到 03-bounded-contexts § 1.1）|
| [0019](decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md) | BC1 + BC4 合并为 TaskRuntime | 战略级 BC 边界调整：8→7 BC；P3 模板升级为聚合骨架重组 |
| [0020](decisions/0020-card-confined-to-bridge-bc.md) | Card 限制在 Bridge BC（中间方案）| **Superseded by 0021**（同日升级为统一方案；本 ADR 决策不实施）|
| [0021](decisions/0021-issue-as-conversation.md) | Issue ↔ Conversation 1:1，统一 Issue/Task 模式 | 推翻 ADR-0009 § 1 + § 3 + ADR-0020：Issue ↔ Conversation 1:1（kind=issue），IssueComment 删独立表（= Message），Bound Card 概念消失，Issue 跟 Task 路线完全对称；Refines ADR-0017 |

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
| [discussion/01-issue-discussion](architecture/tactical/discussion/01-issue-discussion.md) | 战术 (BC2) | Issue / IssueComment 聚合 + 状态机 |
| [workforce/01-worker-model](architecture/tactical/workforce/01-worker-model.md) | 战术 (BC3) | Worker / Project / Mapping / Proposal 聚合（注：Commit 2 carve 后，原 BC4 运行时内容迁出到 task-runtime/02） |
| [cognition/01-supervisor-model](architecture/tactical/cognition/01-supervisor-model.md) | 战术 (BC4) | Supervisor / SupervisorInvocation / DecisionRecord / Memory 聚合 |
| [observability/01-observability](architecture/tactical/observability/01-observability.md) | 战术 (BC5) | Domain Event 总览 + 投影读模型 |
| [conversation/01-conversation](architecture/tactical/conversation/01-conversation.md) | 战术 (BC6) | Conversation / Message / Identity / ChannelBinding（**唯一明示 VO**）|

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
- **无业务聚合 BC（如 Bridge）**：仅 `00-overview.md`，说明"无聚合 / 仅 ACL 翻译职责"

**组织反例（明令禁止）**：

- 不按"主题"切（dispatch / kill / workspace / retry / timeout）—— BC1+BC4 leaky 根源
- 不按"协议层 / 实现层"切 —— ADR-0019 已驳回此切分线

**覆盖范围**（按推进顺序）：

| # | BC | 文件 | 聚合数 / 文件数 | 现状 |
|---|---|---|---|---|
| 1 | TaskRuntime | `tactical/task-runtime/00-overview.md` + `01-task.md` + `02-task-execution.md` + `03-input-request.md` | 3 / 4 | ⚠️ ADR-0019 / 重组实施中 |
| 2 | Discussion | `tactical/discussion/00-overview.md` (+ 可选 0N-{aggregate}) | 1 / 1-2 | ❌ 待重组 |
| 3 | Workforce | `tactical/workforce/00-overview.md` + 多聚合文件 | 3-4 / 多 | ❌ 待重组（含 BC4 carve 后留 BC3 真内容） |
| 4 | Cognition | `tactical/cognition/00-overview.md` + 多聚合文件 | 2 / 多 | ❌ 待重组 |
| 5 | Observability | `tactical/observability/00-overview.md` | 1 / 1 | ❌ 待重组 |
| 6 | Conversation | `tactical/conversation/00-overview.md` + 多聚合文件 | 2 / 多 | ❌ 待重组 |
| 7 | Bridge | `tactical/bridge/00-overview.md` | 0 / 1 | ❌ 待重组（无业务聚合；仅 overview，说明 ACL 翻译职责） |

**跨聚合视角**：不单立 `00-domain-model.md`。跨 BC 引用关系仍归 [strategic/03-bounded-contexts § 3 上下文映射](architecture/strategic/03-bounded-contexts.md#-3-上下文映射context-map)，在 § 3.1 上下游表里补"引用基数 / 强弱 / 一致性窗口"列。

**依赖**：无（直接做）。
**影响范围**：7 个 BC 目录；strategic/03-bounded-contexts § 3.1 表扩列。

### 3.2 中优（架构清晰度）

| ID | 项 | 产出形态 |
|---|---|---|
| **P5** | **Domain Service 明示** | P3 § X.3 中各 BC 文档显式标 |
| **P6** | **Aggregate Root 访问约束**写明 | P3 § X.1 中各 BC 文档标"外部仅通过 Root.id 引用内部 Entity"约定；以及 conventions § 9 在 mutator 封装时贴此约束 |
| **P7** | **Published Language** 明示 | 在 strategic/03-bounded-contexts.md 或 tactical/observability/01-observability.md 加一节："Published Language = events 表 schema + CLI 命令" |

### 3.3 低优 / 视需求

| ID | 项 | 时机 |
|---|---|---|
| P8 | **Repository 接口固化** | 等 implementation 层 02-persistence-schema 时一并 |
| P9 | **Saga / Process Manager** | 等真撞协调问题（v1 不必） |

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

**A.** 顺序推进 7 个 BC（TaskRuntime 第 1 个，作为聚合骨架重组样板；ADR-0019 同步落地）
**B.** 每个 BC 重组 commit 拆 strategic + tactical 两条（参考 TaskRuntime：commit 1 = BC 边界 + 蓝图措辞 + 路径搬家；commit 2 = 内容按聚合骨架重写）
**C.** 完成 7 个 BC 后，strategic/03-bounded-contexts § 3.1 扩列（统一更新跨聚合表）

低优 P8 / P9 不阻塞主线。

---

## § 5. 引用文档

- 方法论：[conventions § 0](../rules/conventions.md#-0-设计方法论ddd--统一语言)
- 架构层索引（含分层说明）：[architecture/README.md](architecture/README.md)
- 通用语言：[strategic/03-bounded-contexts § 1](architecture/strategic/03-bounded-contexts.md#-1-通用语言ubiquitous-language)
- 限界上下文：[strategic/03-bounded-contexts § 2](architecture/strategic/03-bounded-contexts.md#-2-限界上下文bounded-contexts)
- 上下文映射：[strategic/03-bounded-contexts § 3](architecture/strategic/03-bounded-contexts.md#-3-上下文映射context-map)
- 事件总览：[tactical/observability/01-observability](architecture/tactical/observability/01-observability.md)
- Invariants 系统化样板：[tactical/task-runtime/02-task-execution § 13](architecture/tactical/task-runtime/02-task-execution.md)（按 ADR-0019 升级模板，TaskExecution Invariants 节固定 § 13）
- 跟 DDD 相关 ADR：§ 2.1
