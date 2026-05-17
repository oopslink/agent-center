# agent-center DDD 设计蓝图（Blueprint + Plan & Status）

> 本文档是 DDD 设计推进的 **living plan & status**。每次跟 DDD 相关的讨论 / 设计开始前先过一遍这里，定位"哪里到了 / 下一步做啥"。
>
> 顶层方法论约定见 [conventions § 0](../rules/conventions.md#-0-设计方法论ddd--统一语言)。
>
> 跟 [roadmap.md](roadmap.md) 区别：roadmap 是"v1 不做 / 推迟功能"的功能维度 plan；本文档是"DDD 设计深度"的方法论维度 plan。

最后更新：2026-05-17（ADR-0017 接入完成后）。

---

## § 1. 状态总览

按 DDD 三层（战略 / 战术 / 高级模式）盘点。✅ 完成 / ⚠️ 部分 / ❌ 缺。

### 1.1 战略设计（Strategic）

| 概念 | 状态 | 位置 / 说明 |
|---|---|---|
| **Bounded Context** | ✅ | [01 § 2](architecture/01-bounded-contexts.md#-2-限界上下文bounded-contexts)，8 个 BC（Scheduling / Discussion / Workforce / Execution / Cognition / Observability / Conversation / Bridge） |
| **Ubiquitous Language** | ✅ | [01 § 1](architecture/01-bounded-contexts.md#-1-通用语言ubiquitous-language)；conventions § 0 立"统一语言"为方法论根基 |
| **Context Map** | ✅ | [01 § 3](architecture/01-bounded-contexts.md#-3-上下文映射context-map)（图 + 上下游表） |
| **Subdomain 标注**（Core / Supporting / Generic）| ❌ | 未识别 8 个 BC 哪些是核心竞争力、哪些支撑、哪些通用；影响 v2+ 投入策略 |
| **Big Picture / Vision** | ⚠️ | [00-system-overview](architecture/00-system-overview.md) 有部分；DDD 视角下的"系统使命陈述"未集中 |

### 1.2 战术设计（Tactical）

| 概念 | 状态 | 位置 / 说明 |
|---|---|---|
| **Entity** | ⚠️ 散落 | [01 § 1.1](architecture/01-bounded-contexts.md#11-核心实体按上下文归属) 标了 entity / 聚合根 / 从属；字段表散在各 BC 文档 |
| **Value Object** | ❌ 严重缺 | 仅 `ChannelBinding` 明示为 VO；事实 VO（未标）：`DispatchEnvelope` / Reason+Message 对 / `FailedReason` taxonomy / `KilledReason` / `IssueConcludeSpec` / `DispatchAck` / `DispatchNack` / `WorkspaceMode` / `Priority` / `BlobRef` / `IdentityRef` |
| **Aggregate**（含 Invariants）| ⚠️ 散落 | 各 BC 列了"核心聚合"清单；**Invariants 系统化的只有 [02 § 13](architecture/02-task-model.md#-13-不变量invariants)（Task/Execution 8 条）**；Issue / Conversation / Worker / Memory / SupervisorInvocation / WorkerProjectProposal 等的不变量未单独列 |
| **Aggregate Root** | ⚠️ 标了不足 | 01 § 1.1 标"根"，但**未明示"外部仅通过 Root.id 引用，不持有内部 Entity 句柄"** 约束 |
| **Domain Event** | ✅ 充分 | [01 § 2](architecture/01-bounded-contexts.md#-2-限界上下文bounded-contexts) 各 BC 列；[05-observability](architecture/05-observability.md) 事件总览；ADR-0014（事件溯源 L1）/ ADR-0015（agent_trace 不进 events）；[conventions § 16](../rules/conventions.md#-16-错误--状态信息双字段reason--message) reason+message |
| **Domain Service** | ❌ 未明示 | 实际存在：Supervisor 决策（[06](architecture/06-supervisor-model.md)）/ Dispatch 调度（[02 § 4](architecture/02-task-model.md)）/ Issue conclude spawn（[02 § 11](architecture/02-task-model.md)）/ Worker reconcile（[02 § 4.5](architecture/02-task-model.md)）—— 都未标 "Domain Service" 角色 |
| **Repository** | ❌ TBD | [implementation/02-persistence-schema.md](implementation/) 待写；conventions § 9 提了 mutator 封装是前置 |
| **Factory** | ❌ 未明示 | 实际有：Issue conclude → batch spawn N tasks / Task create / TaskExecution create / Conversation create —— 没标 "Factory" |
| **跨聚合一致性策略** | ❌ 无 ADR | ADR-0014 § 2 隐性允许"tx 内同时改两聚合"；ADR-0017 显式跨聚合双写（task + conversation）—— 未专门 ADR 把"刻意偏离 DDD 经典聚合边界"的取舍记录 |

### 1.3 高级模式（Strategic Patterns）

| 概念 | 状态 | 位置 / 说明 |
|---|---|---|
| **Anti-Corruption Layer**（ACL）| ✅ | [01 § 3.2](architecture/01-bounded-contexts.md#32-anti-corruption-layers)，3 处（Bridge / Agent CLI Adapter / BlobStore Adapter） |
| **Shared Kernel** | ⚠️ | [01 § 3.1](architecture/01-bounded-contexts.md#31-上下游关系一览) 标了 Scheduling↔Workforce / Discussion↔Workforce / Scheduling↔Conversation；未集中讨论 |
| **Customer-Supplier** | ✅ | 01 § 3.1 上下游表 |
| **Open Host Service** | ⚠️ | Observability 标为 subscribe-only；**Published Language** 是事件 schema 还是 CLI 未明示 |
| **Saga / Process Manager** | ❌ | 未讨论（Issue → Task spawn → Execution 链是事件驱动 + 状态机协同；如果未来撞协调问题再立） |
| **CQRS / Read Model** | ✅ | ADR-0014 明示驳回 CQRS；读模型存在（[05 Fleet View](architecture/05-observability.md) Projection） |

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

### 2.2 已完成的架构文档（DDD 视角）

| 文档 | DDD 内容 |
|---|---|
| [01-bounded-contexts](architecture/01-bounded-contexts.md) | BC + UL + Context Map + ACL 识别 |
| [02-task-model](architecture/02-task-model.md) | Task / TaskExecution 聚合 + Invariants（§ 13）+ 状态机 + DispatchEnvelope（事实 VO） |
| [03-issue-discussion](architecture/03-issue-discussion.md) | Issue / IssueComment 聚合 + 状态机 |
| [04-input-required](architecture/04-input-required.md) | InputRequest 独立聚合 + 协议 |
| [05-observability](architecture/05-observability.md) | Domain Event 总览 + 投影读模型 |
| [06-supervisor-model](architecture/06-supervisor-model.md) | Supervisor / SupervisorInvocation / DecisionRecord / Memory 聚合 |
| [07-worker-model](architecture/07-worker-model.md) | Worker 聚合 + worker daemon 运行时 |
| [12-conversation](architecture/12-conversation.md) | Conversation / Message / Identity / ChannelBinding（**唯一明示 VO**）|

### 2.3 已立的横切方法论

| 规则 | 内容 |
|---|---|
| [conventions § 0](../rules/conventions.md#-0-设计方法论ddd--统一语言) | DDD 是方法论根基；统一语言以 01 § 1 为权威 |
| [conventions § 12](../rules/conventions.md#-12-命名一致) | 代码 / 文档 / CLI / event_type / 表名 同一组词 |
| [conventions § 16](../rules/conventions.md#-16-错误--状态信息双字段reason--message) | reason + message 双字段约定（事实上的 ReasonMessage VO 雏形） |

---

## § 3. 待完成（Plan）

### 3.1 高优（影响实现，应近期推进）

#### P1. ADR-0018 — 跨聚合引用与一致性策略

**目的**：把 ADR-0014 § 2 / ADR-0017 隐含的"同事务跨聚合双写"取舍显式化。

**要点**（待 ADR 落地）：
- 跨聚合永远用 ID 引用，不持 direct entity reference
- **显式允许跨聚合同事务双写**（v1 单库 SQLite + 单进程；事务原子性 > 最终一致性复杂度）
- 强引用（FK，1:1 / N:1，生命周期联动）vs 弱关联（JSON id list，N:N，无联动）何时用哪个
- 聚合根访问约束：外部仅通过 Root.id 引用内部 Entity
- 显式驳回 Saga / 最终一致性 outbox（v1）

**产出**：ADR-0018（独立 ADR）+ 顺手在 02 / 03 / 12 等加引用。

**影响范围**：实现层（mutator 封装契约）+ 后续所有跨聚合设计（Issue ↔ Task / Task ↔ Conversation / etc.）。

#### P2. ADR-0019 — Value Object 系统化

**目的**：把散在字段里的事实 VO 命名 + 立约定，避免落代码时散成字段。

**要点**（待 ADR 落地）：
- VO 定义：不可变 / 由属性定义 / 可替换 / 基于属性相等
- 列 v1 候选 VO 清单（≈ 10-15 个，见 § 1.2 Value Object 行）
- 实现层代码约束（struct + 无 setter / 工厂方法构造 / 值比较 / 序列化时整体）
- 哪些"事实 VO"维持字段约定不打包（如 ReasonMessage 维持双字段约定 vs 提取为单类型）—— 拍板

**产出**：ADR-0019 + 02 / 03 / 12 / 等 BC 文档加 VO 标签。

**影响范围**：实现层（Go struct 定义）+ 各 BC 文档字段表加角色标签。

#### P3. `architecture/00-domain-model.md`（新文档）

**目的**：DDD 战术设计总览。01-bounded-contexts 只列 BC + UL + Context Map（战略层），00-domain-model 补战术细节。

**内容大纲**（待写）：
- § 1. 各 BC 的聚合清单（Aggregate Root / Entity / VO 三栏明示）
- § 2. **聚合 Invariants 汇总**（从 02 § 13 模板扩展到 Issue / Conversation / Worker / Memory / SupervisorInvocation / WorkerProjectProposal / Project / Identity 等）
- § 3. 跨聚合引用一览表（强 / 弱 + 引用方向 + 一致性策略，落实 ADR-0018）
- § 4. Domain Service 清单（Dispatch 调度 / Supervisor 决策 / Issue conclude spawn / Worker reconcile 等跨聚合纯领域逻辑）
- § 5. Factory 清单（Issue conclude batch spawn / Task create / TaskExecution create / Conversation create）

**依赖**：P1 + P2 先立 ADR。

**影响范围**：架构层新文档；不改其它现有文档（但 D 顺带做）。

### 3.2 中优（架构清晰度）

| ID | 项 | 产出形态 |
|---|---|---|
| **P4** | **Subdomain 标注**（Core / Supporting / Generic）| 短 ADR 或 00-domain-model § 加一节 |
| **P5** | **Domain Service 明示** | 在 P3 / 各 BC 文档加角色标签 |
| **P6** | **Aggregate Root 访问约束**写明 | P1 ADR 顺带写入或独立短 ADR |
| **P7** | **Published Language** 明示 | 在 01-bounded-contexts 或 05-observability 加一节："Published Language = events 表 schema + CLI 命令" |

### 3.3 低优 / 视需求

| ID | 项 | 时机 |
|---|---|---|
| P8 | **Factory / Repository** 明确 | 等 implementation 层（02-persistence-schema 时一并） |
| P9 | **Saga / Process Manager** | 等真撞协调问题（v1 不必） |

---

## § 4. 推进路径建议

```
P1 ─┐
    ├─→ P3 (00-domain-model.md) ─→ D. 改各 BC 文档加角色标签
P2 ─┘
```

**A. ADR-0018**（P1，独立）
**B. ADR-0019**（P2，独立）
↓
**C. 00-domain-model.md**（P3，依赖 A + B）
↓
**D. 各 BC 文档加 VO / Aggregate Root / Entity / Domain Service 标签**（落实 A+B+C）

中优 P4-P7 可穿插在 C / D 之间做（P4 可独立短 ADR，P5/P6/P7 顺手并入 C）。

低优 P8 / P9 不阻塞主线。

---

## § 5. 引用文档

- 方法论：[conventions § 0](../rules/conventions.md#-0-设计方法论ddd--统一语言)
- 通用语言：[architecture/01-bounded-contexts.md § 1](architecture/01-bounded-contexts.md#-1-通用语言ubiquitous-language)
- 限界上下文：[architecture/01-bounded-contexts.md § 2](architecture/01-bounded-contexts.md#-2-限界上下文bounded-contexts)
- 上下文映射：[architecture/01-bounded-contexts.md § 3](architecture/01-bounded-contexts.md#-3-上下文映射context-map)
- 事件总览：[architecture/05-observability.md](architecture/05-observability.md)
- Invariants 唯一系统化样板：[architecture/02-task-model.md § 13](architecture/02-task-model.md#-13-不变量invariants)
- 跟 DDD 相关 ADR：§ 2.1
