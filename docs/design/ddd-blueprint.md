# agent-center DDD 设计蓝图（Blueprint + Plan & Status）

> 本文档是 DDD 设计推进的 **living plan & status**。每次跟 DDD 相关的讨论 / 设计开始前先过一遍这里，定位"哪里到了 / 下一步做啥"。
>
> 顶层方法论约定见 [conventions § 0](../rules/conventions.md#-0-设计方法论ddd--统一语言)。
>
> 跟 [roadmap.md](roadmap.md) 区别：roadmap 是"v1 不做 / 推迟功能"的功能维度 plan；本文档是"DDD 设计深度"的方法论维度 plan。

最后更新：2026-05-18（架构层物理分层：strategic/ + tactical/{BC}/ + tactical/{theme}/，theme = agent-harness / presentation）。

---

## § 1. 状态总览

按 DDD 三层（战略 / 战术 / 高级模式）盘点。✅ 完成 / ⚠️ 部分 / ❌ 缺。

### 1.1 战略设计（Strategic）

| 概念 | 状态 | 位置 / 说明 |
|---|---|---|
| **Bounded Context** | ✅ | [03-bounded-contexts § 2](architecture/strategic/03-bounded-contexts.md#-2-限界上下文bounded-contexts)，8 个 BC（Scheduling / Discussion / Workforce / Execution / Cognition / Observability / Conversation / Bridge） |
| **Ubiquitous Language** | ✅ | [03-bounded-contexts § 1](architecture/strategic/03-bounded-contexts.md#-1-通用语言ubiquitous-language)；conventions § 0 立"统一语言"为方法论根基 |
| **Context Map** | ✅ | [03-bounded-contexts § 3](architecture/strategic/03-bounded-contexts.md#-3-上下文映射context-map)（图 + 上下游表） |
| **Subdomain 标注**（Core / Supporting / Generic）| ✅ | [01-subdomain-classification](architecture/strategic/01-subdomain-classification.md)：Core 3 / Supporting-Essential 3 / Supporting-Peripheral 2 / Generic 0 |
| **Big Picture / Vision** | ✅ | [00-domain-vision](architecture/strategic/00-domain-vision.md)（DDD Domain Vision Statement：thesis + 4 stance + 2 boundary）|
| **架构层物理分层** | ✅ | architecture/ 拆 strategic/ + tactical/{BC}/ + tactical/{theme}/（agent-harness / presentation；2026-05-18）；详见 [架构 README](architecture/README.md) |

### 1.2 战术设计（Tactical）

| 概念 | 状态 | 位置 / 说明 |
|---|---|---|
| **Entity** | ⚠️ 散落 | [03-bounded-contexts § 1.1](architecture/strategic/03-bounded-contexts.md#11-核心实体按上下文归属) 标了 entity / 聚合根 / 从属；字段表散在各 BC 文档；**P3 中各 BC 文档统一收口** |
| **Value Object** | ❌ 严重缺 | 仅 `ChannelBinding` 明示为 VO；事实 VO（未标）：`DispatchEnvelope` / Reason+Message 对 / `FailedReason` taxonomy / `KilledReason` / `IssueConcludeSpec` / `DispatchAck` / `DispatchNack` / `WorkspaceMode` / `Priority` / `BlobRef` / `IdentityRef`；**P3 中各 BC 文档统一收口** |
| **Aggregate**（含 Invariants）| ⚠️ 散落 | 各 BC 列了"核心聚合"清单；**Invariants 系统化的只有 [scheduling/01-task-model § 13](architecture/tactical/scheduling/01-task-model.md#-13-不变量invariants)（Task/Execution 8 条）**；Issue / Conversation / Worker / Memory / SupervisorInvocation / WorkerProjectProposal 等的不变量未单独列；**P3 中各 BC 文档统一收口** |
| **Aggregate Root** | ⚠️ 标了不足 | 03-bounded-contexts § 1.1 标"根"，但**未明示"外部仅通过 Root.id 引用，不持有内部 Entity 句柄"** 约束 |
| **Domain Event** | ✅ 充分 | [03-bounded-contexts § 2](architecture/strategic/03-bounded-contexts.md#-2-限界上下文bounded-contexts) 各 BC 列；[observability/01-observability](architecture/tactical/observability/01-observability.md) 事件总览；ADR-0014（事件溯源 L1）/ ADR-0015（agent_trace 不进 events）；[conventions § 16](../rules/conventions.md#-16-错误--状态信息双字段reason--message) reason+message |
| **Domain Service** | ❌ 未明示 | 实际存在：Supervisor 决策（[cognition/01-supervisor-model](architecture/tactical/cognition/01-supervisor-model.md)）/ Dispatch 调度（[scheduling/01-task-model § 4](architecture/tactical/scheduling/01-task-model.md)）/ Issue conclude spawn（[scheduling/01-task-model § 11](architecture/tactical/scheduling/01-task-model.md)）/ Worker reconcile（[scheduling/01-task-model § 4.5](architecture/tactical/scheduling/01-task-model.md)）—— 都未标 "Domain Service" 角色 |
| **Repository** | ❌ TBD | [implementation/02-persistence-schema.md](implementation/) 待写；conventions § 9 提了 mutator 封装是前置 |
| **Factory** | ❌ 未明示 | 实际有：Issue conclude → batch spawn N tasks / Task create / TaskExecution create / Conversation create —— 没标 "Factory" |
| **跨聚合一致性策略** | ✅ 已散立 | ADR-0014 § 2 显式"tx 内同时改两聚合"；ADR-0017 显式跨聚合双写（task + conversation）—— **决定不再立通用 ADR**（详见 § 3.4） |

### 1.3 高级模式（Strategic Patterns）

| 概念 | 状态 | 位置 / 说明 |
|---|---|---|
| **Anti-Corruption Layer**（ACL）| ✅ | [03-bounded-contexts § 3.2](architecture/strategic/03-bounded-contexts.md#32-anti-corruption-layers)，3 处（Bridge / Agent CLI Adapter / BlobStore Adapter） |
| **Shared Kernel** | ⚠️ | [03-bounded-contexts § 3.1](architecture/strategic/03-bounded-contexts.md#31-上下游关系一览) 标了 Scheduling↔Workforce / Discussion↔Workforce / Scheduling↔Conversation；未集中讨论 |
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
| [0018](decisions/0018-detached-agent-via-per-execution-shim.md) | Detached agent + per-execution shim 模型 | Execution BC 内新运行时角色（Shim）；影响 UL（待回填到 03-bounded-contexts § 1.1）|

### 2.2 已完成的架构文档（DDD 视角）

| 文档 | 层 | DDD 内容 |
|---|---|---|
| [00-domain-vision](architecture/strategic/00-domain-vision.md) | 战略 | Domain Vision Statement（thesis + strategic stance + boundary）|
| [01-subdomain-classification](architecture/strategic/01-subdomain-classification.md) | 战略 | 8 BC 的 Core/Supporting/Generic 分类 + Essential/Peripheral sub-label + 投入策略 |
| [03-bounded-contexts](architecture/strategic/03-bounded-contexts.md) | 战略 | BC + UL + Context Map + ACL 识别 |
| [scheduling/01-task-model](architecture/tactical/scheduling/01-task-model.md) | 战术 (BC1) | Task / TaskExecution 聚合 + Invariants（§ 13）+ 状态机 + DispatchEnvelope（事实 VO） |
| [scheduling/02-input-required](architecture/tactical/scheduling/02-input-required.md) | 战术 (BC1) | InputRequest 独立聚合 + 协议 |
| [discussion/01-issue-discussion](architecture/tactical/discussion/01-issue-discussion.md) | 战术 (BC2) | Issue / IssueComment 聚合 + 状态机 |
| [workforce/01-worker-model](architecture/tactical/workforce/01-worker-model.md) | 战术 (BC3+4) | Worker 聚合 + worker daemon 运行时 |
| [cognition/01-supervisor-model](architecture/tactical/cognition/01-supervisor-model.md) | 战术 (BC5) | Supervisor / SupervisorInvocation / DecisionRecord / Memory 聚合 |
| [observability/01-observability](architecture/tactical/observability/01-observability.md) | 战术 (BC6) | Domain Event 总览 + 投影读模型 |
| [conversation/01-conversation](architecture/tactical/conversation/01-conversation.md) | 战术 (BC7) | Conversation / Message / Identity / ChannelBinding（**唯一明示 VO**）|

### 2.3 已立的横切方法论

| 规则 | 内容 |
|---|---|
| [conventions § 0](../rules/conventions.md#-0-设计方法论ddd--统一语言) | DDD 是方法论根基；统一语言以 03-bounded-contexts § 1 为权威 |
| [conventions § 12](../rules/conventions.md#-12-命名一致) | 代码 / 文档 / CLI / event_type / 表名 同一组词 |
| [conventions § 16](../rules/conventions.md#-16-错误--状态信息双字段reason--message) | reason + message 双字段约定（事实上的 ReasonMessage VO 雏形） |

---

## § 3. 待完成（Plan）

### 3.1 高优（影响实现，应近期推进）

#### P3. 补完每个 BC 文档的「§ X. 战术设计」小节

**目的**：DDD 战术设计落到每个 BC 自治讲（符合"按领域为范围"原则，不抽出新文档统一汇总，避免 SSOT 漂移）。

**模板**（每个 tactical/{BC}/*.md 都加一节，对应 § X.1-X.6 六项）：

```
§ X. 战术设计

§ X.1 聚合清单（Aggregate Root / Entity / VO 三栏明示）
§ X.2 Invariants（聚合内不变量；以 scheduling/01-task-model § 13 为模板）
§ X.3 Domain Service（跨聚合或纯领域逻辑的服务）
§ X.4 Factory（复杂创建逻辑）
§ X.5 Repository（聚合的持久化抽象接口；具体 schema 仍在 implementation/02-persistence-schema.md）
§ X.6 跨聚合引用出方向（本 BC 主动持的引用 + 引用强弱 + 一致性策略）
```

**覆盖范围**（按推进顺序）：

| # | BC | 文档 | 现状 |
|---|---|---|---|
| 1 | Scheduling | [tactical/scheduling/01-task-model.md](architecture/tactical/scheduling/01-task-model.md) | 已有 § 13 Invariants（最齐）；补 X.1 / X.3-X.6 |
| 2 | Scheduling | [tactical/scheduling/02-input-required.md](architecture/tactical/scheduling/02-input-required.md) | 全部补 |
| 3 | Discussion | [tactical/discussion/01-issue-discussion.md](architecture/tactical/discussion/01-issue-discussion.md) | 全部补 |
| 4 | Workforce+Execution | [tactical/workforce/01-worker-model.md](architecture/tactical/workforce/01-worker-model.md) | 全部补 |
| 5 | Cognition | [tactical/cognition/01-supervisor-model.md](architecture/tactical/cognition/01-supervisor-model.md) | 全部补 |
| 6 | Observability | [tactical/observability/01-observability.md](architecture/tactical/observability/01-observability.md) | 全部补 |
| 7 | Conversation | [tactical/conversation/01-conversation.md](architecture/tactical/conversation/01-conversation.md) | 已有 ChannelBinding VO 标；补其余 |
| 8 | Bridge | [tactical/bridge/01-feishu-integration.md](architecture/tactical/bridge/01-feishu-integration.md) | 全部补（Bridge BC 无业务聚合，但仍要说明：无聚合 / 无 Invariants / 仅 ACL 翻译职责） |

**跨聚合视角**：不单立 `00-domain-model.md`。跨 BC 引用关系仍归 [strategic/03-bounded-contexts § 3 上下文映射](architecture/strategic/03-bounded-contexts.md#-3-上下文映射context-map)，在 § 3.1 上下游表里补"引用基数 / 强弱 / 一致性窗口"列。

**依赖**：无（直接做）。
**影响范围**：8 份 BC 文档；strategic/03-bounded-contexts § 3.1 表扩列。

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

- ~~**P1. ADR-0019 — 跨聚合引用与一致性策略**~~（2026-05-18 驳回）
  - **理由**：拟列的 5 条规则里，2 条是 DDD 教科书条款（Root 守门、ID 引用，无须 ADR）；2 条已被 ADR-0014 § 2 / § 5 立过（同事务双写 / 驳回 CQRS）；剩下 1 条（强引用 vs 弱关联）是 design space 不是规则。
  - **替代**：跨聚合关系在各 BC 文档 § X.6 就地讨论；strategic/03-bounded-contexts § 3.1 扩列承载全局视角。
  - **方法论教训**：抽通用规则前先盘具体场景；证据少 / 已立过 / 例外多 → 不抽。
- ~~**P2. ADR-0020 — Value Object 系统化**~~（2026-05-18 驳回）
  - **理由**：VO 候选清单是具体的，但"VO 实现约束"作为方法论 ADR 在落代码前太抽象；候选 VO 直接在 P3 § X.1 三栏盘点时随手命名 + 标角色更高效。
  - **替代**：P3 中各 BC 文档 § X.1 直接给 VO 角色标签；落代码时若发现 Go struct 层需要统一的 VO 约束，再考虑写 ADR。

---

## § 4. 推进路径建议

```
P3 (各 BC 文档补完 § X. 战术设计 小节)
  └─→ 8 份 tactical/ 文档分别补
        └─→ 顺手 strategic/03-bounded-contexts § 3.1 扩列（跨聚合一致性窗口）
              └─→ P5/P6 自然落入 P3 § X.1/§ X.3
                    └─→ P7（Published Language）独立短节
```

**A.** 顺序推进 8 个 BC（建议从 Scheduling 开始，已有最齐的 § 13 Invariants 作为模板参考）
**B.** 每个 BC 单独 commit；commit message 用 `docs(design): {BC} 战术设计落地 — § X.1-X.6`
**C.** 完成 8 个 BC 后，strategic/03-bounded-contexts § 3.1 扩列（统一更新跨聚合表）

低优 P8 / P9 不阻塞主线。

---

## § 5. 引用文档

- 方法论：[conventions § 0](../rules/conventions.md#-0-设计方法论ddd--统一语言)
- 架构层索引（含分层说明）：[architecture/README.md](architecture/README.md)
- 通用语言：[strategic/03-bounded-contexts § 1](architecture/strategic/03-bounded-contexts.md#-1-通用语言ubiquitous-language)
- 限界上下文：[strategic/03-bounded-contexts § 2](architecture/strategic/03-bounded-contexts.md#-2-限界上下文bounded-contexts)
- 上下文映射：[strategic/03-bounded-contexts § 3](architecture/strategic/03-bounded-contexts.md#-3-上下文映射context-map)
- 事件总览：[tactical/observability/01-observability](architecture/tactical/observability/01-observability.md)
- Invariants 系统化样板：[tactical/scheduling/01-task-model § 13](architecture/tactical/scheduling/01-task-model.md#-13-不变量invariants)
- 跟 DDD 相关 ADR：§ 2.1
