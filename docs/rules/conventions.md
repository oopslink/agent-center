# 项目规约 (Project Conventions)

> **所有 agent-center 设计 / 开发 / 测试前必读。** 任何新功能 / 新决定都要按本文档列出的原则做自检。提交前过一遍 [§ 15 自检清单](#-15-新功能--新设计自检清单)。

本文档列出**跨切原则**（cross-cutting principles）—— 不属于某个特定模块，但每个模块都必须遵守。模块特定的细节归 [docs/design/](../design/)。

## § 0. 设计方法论：DDD + 统一语言

**agent-center 使用 [DDD（Domain-Driven Design）](https://en.wikipedia.org/wiki/Domain-driven_design)作为项目设计与实现方法论**。所有设计文档、ADR、代码注释、提交信息、issue 讨论、code review 评论都必须使用 DDD 统一语言（Ubiquitous Language）。

### § 0.1 统一语言（Ubiquitous Language）

**项目通用语言以 [`docs/design/architecture/01-bounded-contexts.md` § 1](../design/architecture/strategic/03-bounded-contexts.md#-1-通用语言ubiquitous-language) 为权威**。包含：

- 核心实体 / 聚合根 / 值对象的术语（Task / TaskExecution / Issue / IssueComment / Worker / Conversation / Message / Identity / ChannelBinding / SupervisorInvocation / Memory / Artifact / InputRequest / ...）
- 行为动词（Dispatch / Conclude / Spawn / Escalate / Enroll / Adopt / Withdraw / Open / Cancel / Add-message / Comment / Deliver / ...）
- 状态机词汇（Task: open / suspended / done / abandoned；TaskExecution: submitted / working / input_required / completed / failed / killed；等）
- 易混淆术语对照（Supervisor 不叫 Brain；Issue 不叫 Suggestion；TaskExecution 不叫 AgentSession 等）

**违反命名一致性见 [§ 12](#-12-命名一致)**：代码包 / 文档 / CLI / event_type 字符串 / 数据库表名都用同一组词。

### § 0.2 DDD 术语自检

讨论 / 文档 / 代码里出现的所有领域概念，必须能在以下范畴里被归类：

| 范畴 | 含义 | 举例 |
|---|---|---|
| **Bounded Context** | 限界上下文 | v2: TaskRuntime / Discussion / Workforce / Cognition / Observability / Conversation / SecretManagement（7 个；v1 Bridge BC v2 已撤回 per [ADR-0031](../design/decisions/0031-v2-drop-bridge-vendor-integration.md)；v1 Scheduling + Execution 已合并到 TaskRuntime per [ADR-0019](../design/decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md)）|
| **Aggregate / Aggregate Root** | 聚合 / 聚合根 | Task（根）+ TaskExecution（实体，从属）；Issue（根）+ IssueComment（实体，从属）；Conversation（根）+ Message（实体，从属）；... |
| **Entity** | 实体（有 identity，跨时间可变） | 各聚合内的从属实体；详见 01 § 1.1 |
| **Value Object** | 值对象（由属性定义、不可变） | ChannelBinding / DispatchEnvelope / Reason+Message 对 / FailedReason taxonomy / IssueConcludeSpec / 等（v1 散落，正在系统化）|
| **Domain Event** | 领域事件 | `task.created` / `issue.concluded` / `conversation.message_added` / ... 详见 [observability/00-overview.md](../design/architecture/tactical/observability/00-overview.md) |
| **Domain Service** | 跨聚合的纯领域逻辑 | Supervisor 决策 / Dispatch 调度 / Issue conclude → spawn tasks |
| **Repository** | 聚合的持久化访问（**接口签名 + domain error types 归领域层 / 架构层**；SQL schema / dialect / tx context 传递归实现层）| 接口签名见各 BC § 5 / domain errors 见各 BC 自有 errors 清单；实现层 schema 见 [implementation/02-persistence-schema.md](../design/implementation/) (TBD) |
| **Factory** | 复杂聚合的创建逻辑 | Issue conclude batch spawn N tasks / Task create / TaskExecution create |
| **Anti-Corruption Layer (ACL)** | 跟外部系统的翻译层 | Bridge（每 vendor 一个）/ Agent CLI Adapter / BlobStore Adapter（[01 § 3.2](../design/architecture/strategic/03-bounded-contexts.md#32-anti-corruption-layers)）|
| **Shared Kernel / Customer-Supplier** | 上下文映射模式 | 见 [01 § 3.1](../design/architecture/strategic/03-bounded-contexts.md#31-上下游关系一览) |

### § 0.3 自检清单

- 我引入的新概念，**有没有恰当归类到上述某个 DDD 范畴**？没有的话，是真新概念还是该归到某个已有类别？
- 我用的术语，是不是 [01 § 1.1 通用语言表](../design/architecture/strategic/03-bounded-contexts.md#-1-通用语言ubiquitous-language)里的同一个词？还是无意中造了同义词（Brain ≠ Supervisor / Session ≠ TaskExecution / Card ≠ Message 等）？
- 跨聚合写操作，符合 [ADR-0014 § 2](../design/decisions/0014-event-sourcing-level.md) 的同事务原则吗？跨聚合**强引用**（如 task.conversation_id）vs **弱关联**（如 issue.related_conversation_ids）的选择有依据吗？
- 我引入的值对象是不是有"由属性定义、不可变、可替换"特征？是的话明示为 VO，不要散成"几个字段"
- **外部对聚合的访问，是不是只通过 Root.id**？聚合根**守门**：外部代码 / 跨聚合引用一律持 Root.id，不持内部 Entity / 子从属的句柄；要操作内部 Entity 必经 Root 暴露的方法（详见 P3 各 BC § 5 Repositories 约定栏）|
- **BC 物理隔离**：我新增的表 / 列归属唯一 BC 吗？跨 BC 数据流走 events / RPC，不共享物理表（详见 [§ 9.z](#-9z-bc-间不共享底层数据表)）

### § 0.4 DDD 原则下做正确的事（顶层架构铁律）

**领域 Application Service 是访问领域状态的唯一入口。** 所有持久化（DB / 文件 / cache）只能由 BC 内部的 Repository / AppService 触碰；BC 外部的代码——无论 CLI / worker / Web 前端 / 其它 BC——一律通过 AppService 表面，**不允许直读底层存储**。

这是 DDD 保证一致性的核心：聚合根守门 invariant（[§ 0.3](#-03-自检清单)）、跨聚合写遵守 same-tx double-write（[ADR-0014 § 2](../design/decisions/0014-event-sourcing-level.md)）、版本 CAS / event emission / 状态机过渡——全部 **only inside the AppService** 才能生效。绕过 AppService 直读直写 = 这些 invariant 全部失效，**SQLite WAL 等底层并发安全保证 ≠ BC invariant 一致性**。

#### Single-host 不是绕过 transport 的借口

| ❌ 错误观念 | ✅ 正确架构 |
|---|---|
| "都是本机，CLI/worker 直读 sqlite 简化" | CLI/worker 走 unix domain socket → server AppService |
| "single-host 不需要 RPC" | single-host vs multi-host **只是 transport 不同**（unix socket vs TCP），AppService 表面**完全相同** |
| "WAL 模式允许多 process 共享 DB 文件" | WAL 只保证 SQL 语句原子性，**不保证 BC invariant**——两个 process 各自的 AppService 各自持 in-memory aggregate snapshot，版本 CAS 必然冲突、event 必然漏发 |

**v2.0 GA 的反面教材**：v2 ship 时 CLI / worker daemon / server 三者都 `persistence.Open(cfg.Server.SqlitePath)` 直读同一 sqlite 文件，server 进程实质不参与数据面，被 @oopslink 在 2026-05-24 firsthand 部署验证时识别为重大架构缺陷。整改 plan 见 [docs/plans/v2.2-transport-layer.md](TBD)。

#### 架构整改起点（必答铁律）

> **Any architecture remediation must start from "what does the domain model require", not "what's the smallest patch on current code".**
>
> 任何架构整改方案的起点都必须是"领域模型要求什么"，不是"现状最小补丁"。

整改提议在落笔之前先答：

| 必答问题 | 不及格的回答（红旗） |
|---|---|
| 这条整改路径是不是先回到 BC / AppService / Aggregate invariant 看"该怎样"？ | "保留现状最小化改动" / "向后兼容优先" / "渐进迁移" → 这些是 transport / sequencing 关切，**不能取代起点** |
| 我的方案是不是把领域不变量（version CAS / same-tx event / 状态机过渡 / Repository ownership）继续保住？ | "可接受的临时违规" / "v2.x 先这样 v3 再做" → 不允许把违规升格为 architecture-of-record |
| 我提议的"简化路径"是不是省了 transport / 省了 AppService / 省了 Repository 这一层？ | "single-host 简化" / "都本机不需要 RPC" / "测试可以直读" → 都不接受 |
| `NoopSender / nil Spawner` 这类 mock-as-default 在 production wiring path 上有 `// FIXME(prod-wiring)` tag 吗？ | 没有就是漏 |

**反面教材（v2.0 GA 重述）**: 我在 2026-05-24 self-audit 时提议 "Option A: 承认 CLI/worker 直读 sqlite 是 single-host 架构，文档对齐就行"。这正好踩中红旗——起点是"现状最小化改动"，把 DDD 违规升格为 architecture-of-record。被 @oopslink 立即驳回。**正确的起点应该是：领域模型要求 AppService 是唯一入口 → 因此必须有 transport → unix socket vs TCP 只是 transport 形式**。

#### 一般性自检

- 我新增的 CLI 命令 / worker daemon 入口 / 任何"非 server 进程"代码——**是直接 `persistence.Open` 还是走 AppService transport**？前者必须改为 client-over-transport。
- 我提议的"简化路径"是不是把 transport 这一层省了？省 transport ≠ 简化 ≠ 可接受。
- 我看到 `dispatch.NoopSender{} / kill.NoopKillSender{} / SetSupervisorSpawner == nil` 这类 mock-as-default 在 production wiring path 上——是不是有 `// FIXME(prod-wiring)` tag？没有就是漏。

#### 跨域边界破坏必先确认（@oopslink 确认门）

> **凡是可能打破实体或服务的领域边界的技术方案，都需要和 @oopslink 确认后方可推进。**

适用范围（任一中即触发）：

| 触发条件 | 例子 |
|---|---|
| 跨 BC 直接调用聚合根 / 直读对方 BC 的 Repository | Agent BC 代码直接 `pm.TaskRepo.FindByID()`（应走 Customer-Supplier / ACL / Shared Kernel 明示） |
| 跨聚合写 same-tx double-write 的方案 | 一个 transaction 内同时改 ProjectManager.Task + Agent.WorkItem 不走 outbox/projector |
| 引入跨 BC 的共享 schema / 共享表 / 共享数据 | 新建表被 2+ BC 直接读写 |
| 把一个 BC 的 invariant 下放到 DB 约束之外的位置 | "靠 service 层串行化代替 DB UNIQUE" → DB+service 双层防御已是 minimum，单层不允许 |
| 修改 ADR 已定的 BC 边界 / Aggregate 结构 / 集成关系（CF/CS/ACL/SK/OHS）| 重画 Context Map / 拆合聚合根 / 改变 BC 间通信模式 |
| Repository / AppService 表面变更（删/改/新方法 with cross-BC semantics）| 新增 Repository method 让外部 BC 直访本聚合的内部状态 |
| "我的方案是 XX, 暂时绕过 YY 边界, 等 vN+1 再修" | 任何把违规升格为 architecture-of-record 的措辞（参考 § 0.4 反面教材） |

**确认流程**:

1. 提议人（Dev / Dev2 / PD / 任何角色）在 PR / task / 设计文档**显式标记** "跨域边界破坏" 标签
2. 在 PR 描述 / task 描述里**列出**: 触发了哪一条 / 影响哪些 BC / 替代方案 / 为什么选这个方案
3. **@mention @oopslink** 等待 explicit confirm（"OK" / "拍" / "同意" / 或直接 "do X instead"）
4. 拿到 confirm 之前**不能** merge / 不能 push 到 main / 不能进入 implementation phase
5. 如果是已实施代码触发了这条规则（review 阶段发现），暂停 merge / revert + 走流程

**判断不清楚时的默认**: 倾向于"需要确认"，宁可多一次同步也别累 architecture-of-record 债。

**已观察到的 case**:

- v2.0 GA 多 process 直读 sqlite 文件 = 跨进程绕 AppService = 触发本规则的过去版本（2026-05-24 @oopslink firsthand 部署验证识别 + 直接驳回 "Option A 现状最小化改动"）
- 2026-06-06 @oopslink 立此规则，作为 v2.0 → v2.7 → v2.8 一系列架构整改经验沉淀

#### Enforce 机制

1. **Arch test**: `internal/cli/handlers_*.go` 出现 `persistence.Open` = CI fail（白名单：`handlers_migrate.go` schema 迁移工具 + `handlers_system.go` server 启动）
2. **Mock-as-default lint**: `NoopSender` / `NoopKillSender` / `nil Spawner` 等出现在 `internal/cli/app.go` / `handlers_system.go` 必须有 `// FIXME(prod-wiring):` 注释；release tag pre-flight 阻止任何带 `FIXME(prod-wiring)` 的 commit
3. **Doc-impl drift check**: ADR 写"X 通过 transport Y"，自动 grep 是否反例（如 ADR-0038 说"CLI 通过 admin endpoint"则 CLI handlers 不应有 `persistence.Open`）
4. **Deployed-binary smoke gate**: 每个 phase close 强制 1 个 fresh-binary deploy + drive 1 real user path；不准全 mock；smoke 数 = 0 的 phase 不许 close（也见 [§ 14](#-14-测试)）
5. **Self-audit 强制 deploy 步**：任何"GA / release / phase close / system health" 类 audit，必须 actually run binary + drive 真路径，不能只靠 grep + test 数字

### § 0.5 不接受的措辞

- "事件" → 必须区分 **Domain Event**（领域事件，进 events 表）vs **AgentTraceEvent**（agent JSONL 内的事件，不进 events 表，见 [ADR-0015](../design/decisions/0015-agent-trace-not-in-events-table.md)）
- "对象" / "数据" / "记录" → 模糊；明示 Entity / Value Object / Aggregate / Projection / Event
- "调用 / RPC / 接口" → 跨聚合或跨 BC 时明示是 **Customer-Supplier** / **Shared Kernel** / **ACL** 哪种模式
- "中央 / 大脑 / 总管" → Supervisor，**不是** Brain（[ADR-0003](../design/decisions/0003-supervisor-not-brain.md)）

### § 0.6 「没做」≠「不支持」≠「假设了反面」

描述系统现状时**止步于「未实现」**，不向「设计意图」越界推断。**「展示缺口」与「设计假设」必须分层陈述，不得合并推断**。

分析系统现状时，若某功能在前端 / UI 层缺失，**只能**表述为：

- ✅ 「该功能在 UI 层尚未实现」
- ✅ 「后端 / 模型层已支持，但 SPA 未暴露」

**不得**表述为：

- ❌ 「设计上假设了 X」
- ❌ 「当前架构不支持 X」

除非能提供以下至少一条**可定位的证据**：

1. **代码注释或文档明确写出该假设**（grep 得到的具体文件 + 行号）
2. **数据模型层有结构性约束**（schema unique constraint、`NOT NULL` 设计意图注释、AR 字段缺失）
3. **API 设计有明确的单数语义**（如 `/api/X` 返回 object 而非 array、CLI 命令无 `--all` flag 等）

写"为什么 SPA 没有 X 入口" / "为什么 v1/v2 不能做 Y" 这种回答前，先 grep 一次相应层。grep 不出来 → 只描述观察 + 能力两层，不补第三层（设计意图）。

**反例**（违反本节）：「v2 web console scope 是 *per-user single project context* 隐式假设，所以没有 Projects 入口」——这把「SPA 没暴露 Project」（观察）和「模型支持 Project」（能力）越界推断成「设计上假设单 project」（意图），没有 grep 出任何支撑。

**正确写法**：
> 「Project 是 Workforce BC 的独立 AR（`internal/workforce/project.go`），backend `/api/projects` + admin endpoint 全套接通；SPA 的侧栏没有 Projects 入口，仅 DeriveModal 在派生 issue / task 时调用 list。这是 SPA 暴露的缺口，模型层不限制。」

## § 1. 单一来源 / 无野任务

**Center 是任务的唯一权威**。Task 只能由 supervisor 或用户创建。Worker / Agent **不允许造任务**，它们想要新工作必须开 [Issue](../design/architecture/tactical/discussion/00-overview.md) 走讨论。

**自检：** 我引入的新动作 / 接口，会不会让某个 worker / agent 绕过中心创建任务？会的话，要么改为开 Issue，要么本设计有问题。

## § 2. 可观测性优先 (Observability First)

可观测性是一级需求。**任何新功能的设计阶段必须同时设计其可观测能力。** "先做功能、事后补观测"不被接受。

**自检（设计阶段必答）：**

| 问题 | 必须回答 |
|---|---|
| 这个功能产生哪些 **domain event**？字段是什么？ | 列清单 |
| 这些事件如何落到 `events` 表？是否会重放？ | 说明 |
| 是否引入新的 **trace 维度**（agent 视角 / supervisor 视角 / worker 视角）？ | 是 / 否 + 说明 |
| `inspect` / `ps` / `stats` 三个查询接口是否要扩展？ | 是 / 否 + 列变更 |
| 异常路径如何被发现？（fail 事件？timeout？外部告警？） | 说明 |
| 用户从飞书侧能问到这个功能的状态吗？ | 是 / 否 |

详情见 [架构层 / 可观测性](../design/architecture/tactical/observability/00-overview.md)。

### § 2.x 观测层是统一且 opinionated 的

Agent-center 的观测层（`events` 表 / fleet view / `inspect` 命令格式 / domain event 类型）是**统一**的，不允许 per-project 自定义。

- **项目特有的度量**应留在**项目自身**（项目仓库的 README / metric 文件 / 自有可观测工具链）
- **Agent-center 提供规范 / 扩展点**（如标准 event 类型、agent 可发的 metric event），项目按规范对接
- **不允许**为某个项目特殊需求侵入 agent-center 观测层代码

这条原则确保 fleet view 跨项目可比，inspect / stats 命令保持统一接口。

## § 3. AI Native

工具暴露给 agent 走 **skill + CLI**，**不引入 MCP** —— 见 [ADR-0001](../design/decisions/0001-no-mcp.md)。

新增 agent 可用能力的标准流程：

1. 在对应 skill 文件（`supervisor.md` / `worker-agent.md`）里描述：何时该用、参数语义、返回格式
2. 在 `agent-center` 二进制加 CLI 子命令
3. CLI 检测运行上下文（server 同机 / worker 内 / 远程）自动选 transport

**自检：** 新能力是否同时具备 skill 文档 + CLI 子命令？skill 是否清楚地讲明"agent 在什么场景应该调它"？

## § 4. 零 LLM SDK 依赖

**严禁 `import` 任何 LLM 厂商 SDK**（anthropic / openai / gemini 等）。LLM 能力一律通过 spawn agent CLI 实现 —— 见 [ADR-0002](../design/decisions/0002-no-llm-sdk-use-cli-agents.md)。

**自检：** 代码改动是否引入了任何 LLM SDK 依赖？是的话立即改。

## § 5. 文档先行 / 决策必有 ADR

任何设计 / 决策 / 接口提议 **先到文档**，再讨论；不在聊天 / Issue / PR 描述里口头确认设计。

架构 / 实现的"分叉路口"决定必须写 ADR。推翻先前决定 → 写新 ADR 并把旧 ADR 标 `Superseded by ADR-NNNN`，**不删旧 ADR**。

具体流程见 [文档管理规则](documentation.md)。

**自检：** 我做了哪些"多选一"决定？这些决定有 ADR 吗？

## § 6. 范围决策两分（出范围 vs 推迟）

**永远不在职责里** 的（边界决策）放 [`requirements/03-out-of-scope.md`](../design/requirements/03-out-of-scope.md)。
**v1 不做但早晚要做** 的（节奏决策）放 [`roadmap.md`](../design/roadmap.md) 或对应 architecture 文档的"未来扩展"小节。

混在一起是违反规约。详见 [文档管理规则 § 4](documentation.md#4-范围决策的两种区分)。

**自检：** 我新增的"不做"条目，归属边界还是节奏？写对位置了吗？

## § 7. 项目本地约定不进 agent-center

项目自有的 `CLAUDE.md` / `AGENTS.md` / `README.md` 等约定文件**留在项目仓库**，由 agent CLI 在 worktree cwd 自然加载。agent-center **不复制、不缓存、不注入** —— 见 [ADR-0005](../design/decisions/0005-project-charter-stays-in-project-repo.md)。

**自检：** 我引入的新功能是否在 agent-center 里维护了"项目级文档/规则"？如果是，是否能改为"读项目本地文件"或"通过 supervisor working memory 的 project scope"？

## § 8. 大文件走 BlobStore

DB **不存** markdown / 日志 / trace 这类大内容。统一通过 BlobStore 抽象，DB 只存**相对路径** —— 见 [ADR-0006](../design/decisions/0006-blob-store-for-large-content.md) 与 [implementation/01-blob-store.md](../design/implementation/01-blob-store.md)。

**自检：** 我新增的字段，内容会超过 ~10KB 吗？会的话走 BlobStore。

## § 9. 持久化层 dialect-agnostic

v1 默认嵌入式 **SQLite**（单文件、零运维）。未来可能切 **Postgres**（多 center / 高并发 / 强约束需求），写代码时**贴 SQL 共同子集**，不踩任何一家方言独占的特性。理由见 [NF3](../design/requirements/02-non-functional.md)。

> Worker 端不使用 SQLite，改用 per-execution 目录承载本机状态，见 [ADR-0018](../design/decisions/0018-detached-agent-via-per-execution-shim.md)。

### § 9.0 禁忌清单 + 替代方案

| 维度 | 禁用 | 替代 |
|---|---|---|
| 自增主键 | SQLite `AUTOINCREMENT` / PG `SERIAL` / `IDENTITY` | 应用层生成 **ULID / UUID** 字符串当主键 |
| Boolean | PG `BOOLEAN` | `INTEGER` 0/1 |
| 时间戳 | PG `TIMESTAMPTZ` / SQLite 内置 datetime | `TEXT` 存 ISO8601（UTC，`2026-05-17T10:00:00Z`） |
| JSON | PG `JSONB` + GIN 索引 / SQLite `json_extract` 索引 | `TEXT` 存 JSON 字符串，应用层 marshal；**不在 SQL 里查 JSON 内容** |
| 行级锁 | PG `SELECT ... FOR UPDATE` / SQLite `BEGIN IMMEDIATE` | **乐观锁**：version 列 + `UPDATE ... WHERE version=? `，CAS 失败重试 |
| 队列消费原语 | PG `SKIP LOCKED` | events 表 + 应用层游标 / 状态机 |
| 表特性 | SQLite `WITHOUT ROWID` / 动态类型 ; PG 数组 / enum / 范围类型 / `LATERAL` / 触发器 / 物化视图 | 普通表 + 普通列；enum 用 `TEXT` + 应用层校验 |
| Upsert | — | 两边都支持 `INSERT ... ON CONFLICT(...) DO UPDATE`，可用 |
| 全文搜索 | SQLite FTS5 / PG `tsvector` | v1 不上 FTS；需要时单独 ADR 评估 |

### § 9.1 Schema migration

- 只做 **additive migration**（加列 / 加表 / 加索引），不依赖 `ALTER TABLE` 复杂能力（SQLite 受限）
- 删列 / 改类型 / 改约束 → 新表 + copy + rename 的两步迁移
- 一份 migration SQL 必须**同时在 SQLite 和 PG 上跑通**；类型映射差异在 migration 工具层抽象，业务 query 不分叉

### § 9.2 CI 双引擎跑

测试套件（含 repository / migration / 任何写过 SQL 的代码）原则上**同时在 SQLite 和 PG 上跑**。CI 默认双 job 并行，否则方言泄漏会悄无声息地积累，到真要切 PG 那天爆炸。

> **v1 阶段例外**：落地 SQLite-only（[implementation/02-persistence-schema § 1.3](../design/implementation/02-persistence-schema.md)）；PG CI 在 v1 不强制，切 PG 按"重做"重新激活而非平滑迁移。本节其余规则（§ 9.0 禁忌 / § 9.1 additive migration）仍生效以保持大部分可移植性。

### § 9.3 不依赖 dialect 的存储抽象边界

dialect-agnostic 只承诺"**SQL 引擎之间**可切"，**不**承诺可换到 KV / 文件 / 文档库。需要换存储种类的场景（如 Memory file repo）请独立 ADR 论证，不要塞进 repository 层抽象。

**自检：**
- 我的 schema 改动在 SQLite 和 Postgres 上都能跑吗？
- CI 是不是真的两个引擎都跑了？
- 我有没有用 § 9.0 禁忌清单里的特性？用了的话替代方案是什么？

### § 9.4 Schema 变更是独立工作单位

一次 schema 变更是它**自己的工作单位**，绝不作为某个特性 ticket 的子步骤夹带。v2.7.1 两次践行：把 `reasoning_level` / `mode` / `provider`（`#229`）、work-item type / priority（`#231`）推迟到 v2.8，而非把四个 migration 拖进 `#228` 的 UI 重构；并把 org-sequence migration（`#245`）、worker-config 升级 migration（`#251`）拆成各自的 PR。

一个 schema-change PR **至少**包含：

1. migration up + down SQL 文件（遵 § 9.1 additive）。
2. **同步 5 个 `schema_version` 断言点**（全部手维护，必须 lockstep 一起改，漏一个即测试红 / 迁移断言不一致）：
   - `internal/persistence/migrator_test.go`
   - `internal/persistence/migration_round_trip_test.go`
   - `internal/cli/handlers_migrate_v1_to_v2.go` 的 `const targetSchemaVersion`
   - 上者的 `handlers_migrate_v1_to_v2_test.go`（含 `"target/new schema version: N"` 字串）
   - `tests/integration/integration_test.go`
3. **更新 `collectKnownKeys`（`internal/config/config.go`）** 若新增 config section / key——它是手维护的 YAML allowlist。漏加 → 启动报 `unknown YAML key`，看起来像配置损坏。`#200`（`server.bootstrap_public_url` 曾漏出 allowlist）即此教训；`#211`（`instance`）、`#249`（`worker:` section）后续都正确加入。
4. 新增 NOT NULL 列 → migration 时 **backfill**，并 seed watermark / max-id，使迁移后的新分配从 backfill 续号（`#245`：`pm_org_sequence.next_value = MAX(org_number) + 1`，老数据回填 1..N、新分配从 N+1 续）。
5. **真升级 smoke**：v_prev → v_new 在真实例 + 真数据上跑，不只空库 round-trip——空 fixture 上的 schema 正确性必要但永远不充分（尤其 backfill 正确性只有真旧数据才验得出）。

schema 变更还需 Tester 显式**升级路径验收**：fresh install 只验 schema 形状；只有真跨版本升级才验 migration 真在旧版本数据上跑过。

**自检：** 我的 schema 改动是不是夹在某特性 PR 里？5 个断言点都同步了吗？新 config key 加进 `collectKnownKeys` 了吗？新 NOT NULL 列 backfill + watermark seed 了吗？有没有真升级 smoke（非空库 round-trip）？

> 背景与完整复盘见 [v271-retrospective.md § 8](v271-retrospective.md)。

## § 9.x DB schema 尽量减少 JOIN 操作

简单的 list-of-id references → 用**主表上的 JSON 数组字段**（如 `issue.related_conversation_ids`）。

复杂 N:N（含富元数据 / 频繁双向查询 / 关系本身有领域含义）→ 才用**独立实体子聚合**（不是纯 join 表）。

避免：仅为关联两个实体而存在的"纯 join 表"（如 `issue_conversation_join (issue_id, conversation_id)` 无任何额外字段）。

**自检：** 我新增的关联是不是只是 list-of-id？是的话用 JSON 字段；不是的话有没有真正的领域含义？

## § 9.w Schema 不声明外键（FK）

DB schema **不使用 FOREIGN KEY** 约束；表间引用语义用列名约定（`<entity>_id` / `<entity>_ref`）表达，引用完整性**由应用层 Repository / Domain Service 负责**。

**为什么**：

- **Invariant 单一来源**：DDD 一致性由 aggregate root + Repository 保证；FK 是双层防御，跟应用层规则脱钩易漂移
- **跟 [§ 9.z BC 物理隔离](#-9z-bc-间不共享底层数据表)一致**：跨 BC 引用本来就走 events / API 不走 FK；同 BC 内 FK 多余
- **Migration 灵活性**：SQLite 加 FK 后改表必须新表 + copy + rename 两步走（[§ 9.1](#-91-schema-migration)）；无 FK 减少摩擦
- **数据维护**：admin / 运营脚本能直接修任意表，不被约束卡死
- **驱逐 "FK 兜底" 防御代码**："FK should prevent this; but tx safety" 这种 silently swallow 模式直接消失，违反 [§ 17 错误显式化](#-17-错误显式化处理-or-抛出不允许吞)
- **JOIN 不依赖 FK**：JOIN 用列匹配即可；SQLite / PG 查询优化器对 FK 无收益

**仍可用的约束**（同行 / 单表内）：

| 约束 | 用 | 不用 |
|---|---|---|
| `PRIMARY KEY` | ✅ | - |
| `NOT NULL` / `DEFAULT` | ✅ | - |
| `UNIQUE` / `INDEX` / partial index | ✅ | - |
| `CHECK`（值域 / 同行字段关系） | ✅ | - |
| `FOREIGN KEY` | - | ❌ |

引用列命名仍按 `<entity>_id` / `<entity>_ref`，**仅作语义提示，不加 FK 约束**。

**实施细节**：

- DSN 仍开 `_foreign_keys=ON`（[02-persistence § 1.2](../design/implementation/02-persistence-schema.md)）—— 没 FK 声明它就 no-op；保持配置一致防意外引入 FK 时立刻生效
- Repository 实现**禁止**靠 SQLite 抛 FK 错来判断 referenced row 不存在；应用层应该先查再写或同事务双写
- 既存代码里 `isForeignKeyViolation` / `IsForeignKeyConstraint` 等 helper 删除（FK 既然不声明，这个错误路径不会发生）

**自检**：

- 我新加的 migration / SQL 字符串里有 `FOREIGN KEY (...) REFERENCES ...` 吗？删掉
- 我引用完整性靠谁保证？显式说出 Repository / Service 在什么时机校验
- 我有没有 "FK should prevent X" 类防御代码？删掉或换 panic（[§ 17](#-17-错误显式化处理-or-抛出不允许吞)）

## § 9.z BC 间不共享底层数据表

跨 BC 的数据交换走 **API / 接口**（同 BC 内 Repository / Domain Event 订阅 / 应用层 RPC），**不能共享物理表**。"同表跨 BC ownership / column-level ownership / 各 BC 写各自列"等措辞一律视为违反 BC 边界。

**为什么**：

- 物理表跨 BC 共享意味着 migration / 索引 / 字段加减要协调多 BC，破坏 BC 独立演进
- 隐含的 partial-column UPDATE 是不成文契约，落代码时易出错
- 字面违反 hexagonal / BC 边界，给后续设计立坏样本
- "省一次 PK JOIN" 的收益通常不值这些代价（SQLite 单库 PK JOIN 可忽略）

**合法的跨 BC 数据通道**：

- **Domain Event**：BC-A emit，BC-B 订阅投影到**自己的**表
- **应用层 RPC / API**：BC-B 调 BC-A 暴露的查询方法
- **Repository 接口暴露的查询**：仅用于同 BC 内

**自检：** 我新增的表 / 列能指向**唯一**一个 owning BC 吗？两个以上 BC 要写同一行，要么拆表，要么改成 events 订阅投影到本 BC 独有表。

## § 9.y 领域模块零外部 vendor 依赖（v2 + v3+ 通用原则）

> v2 撤回所有 vendor 集成（per [ADR-0031](../design/decisions/0031-v2-drop-bridge-vendor-integration.md)）；v2 用户主入口 = Web Console + CLI。本节规则仍有效作为通用原则（领域模块零外部依赖）；具体外部接入 v3+ 才重新设计。

领域模块（Issue / Conversation / Task / 等）是**系统内部模块**，**不直接调用**任何外部 SDK 或 API。

未来若引入外部接入（v3+），走独立 ACL 程序实现：

- **Outbound（系统 → 外部）**：ACL 订阅领域事件 → 推到外部
- **Inbound（外部 → 系统）**：ACL 收回调 → 写到领域模块的 API
- 领域模块零外部依赖，可独立测试、可独立运行

**自检：** 我的领域模块代码里 import 了外部 vendor SDK 吗？v2 直接拒绝；v3+ 走 ACL。

## § 10. 单一二进制 / 多模式

所有功能聚合到 `agent-center` 一个 binary，按子命令区分模式（`server` / `supervisor` / `worker` / 各 CLI 命令）。**不引入额外可执行文件**。

**自检：** 我的功能是否需要新增一个独立 binary？能否改为新的子命令？

## § 11. Issue / InputRequest / 状态事件 三者分清

不同交互场景走不同渠道，不要混用：

| 场景 | 渠道 |
|---|---|
| 议题、要讨论、可能后续产生多个 Task | **Issue**（非阻塞，多轮）|
| 任务执行中卡住要拍板，等不动 | **InputRequest**（同步阻塞）|
| Worker / Task 状态机推进、心跳、产物上报 | **Domain Event**（异步事件流）|

**自检：** 我设计的新交互，走的是哪条渠道？为什么不是另两条？

## § 12. 命名一致

| 概念 | 用 | 不用 |
|---|---|---|
| 调度官 agent | **Supervisor** | ~~Brain~~（见 [ADR-0003](../design/decisions/0003-supervisor-not-brain.md)）|
| 待讨论的事 | **Issue** | ~~Suggestion / Proposal~~（见 [ADR-0004](../design/decisions/0004-issue-not-suggestion.md)）|
| 工作单元 | **Task** | ~~Job / Work~~ |
| 用户开发机的守护进程 | **Worker daemon** | ~~Agent / Worker（指 host 本身）~~ |
| Worker 上跑的 CLI agent 实例 | **Agent** 或 **Worker agent** | ~~Worker~~（避免歧义）|
| 持久脑 | **(supervisor) memory** | ~~knowledge / brain~~ |
| 任务隔离的目录 | **Worktree** | ~~workspace / sandbox~~ |
| 大文件存储抽象 | **BlobStore** | ~~ObjectStore / FileStore~~ |

代码、文档、CLI、表名、event_type 字符串等**一律遵守**。

**自检：** 我用的命名跟此表一致吗？

## § 12.x ID 分层与引用格式（member-id / ULID / identity-ref）

业务面与内部面用**两套 ID**，且 identity 引用与 entity 引用**格式不同**——混用是 v2.7.1 反复出现的一类 bug（`#240` / `#241` / `#244`）。

**ID 分层（业务面 vs 内部面）**

| 面 | 用什么 | 出现在 |
|---|---|---|
| 业务 / 用户可见 | **member-id**（`agent-<8hex>`，Phabricator 式 hash） | UI、REST API、`@mention` token、ref token |
| 内部 / 运维 | **ULID** | DB 主键列、worker 文件系统布局、跨 BC 引用 |

- member-id ↔ ULID 的映射存在 DB；**UI / REST API / `@mention` / ref token 一律不暴露 ULID**。
- operator-surface（`~/.agent-center` 文件系统布局）保留 ULID = **实现细节**，与 git 的 object SHA 是实现细节同理（owner 既定拍板，`#185`）。
- 判据：**终端用户**（非运维）会看到它吗？会 → member-id；只有运维读 `~/.agent-center` 才看到 → 任一 ID 皆可。

**引用格式：prefixed identity-ref vs bare entity-id（[ADR-0033](../design/decisions/0033-identity-model-refactor.md)）**

- **identity（agent / user / member）→ 引用必带 `kind:` 前缀**：`agent:<id>` / `user:<id>` / `system`，在**每一个校验 identity-ref 的消费端**（createDM members、invite、`assign_task` assignee、project-member-add、channel-member）。`pm.IdentityRef.Validate()` 拒裸 id。
- **entity（project / task / issue / channel / work-item）→ ID 裸用**：消费端不得要求前缀，生产端不得添加前缀。
- **产出 identity-ref 的 tool / DTO 必须产出「完整可消费」形态**，而非裸 id 寄望调用方自己拼 `agent:`——见 `#241`：`find_org_agent` 返 `{id, name, assignee_ref:"agent:<id>"}`，agent 把 `assignee_ref` 直接喂 `assign_task`，零拼接（§ 0.4 / 「无中间态」complete-consumable output）。
- 前端**每个**生产点都须拼前缀（identity-ref 的前端落地见 [ux-standards.md](ux-standards.md)）。当前现状是 per-component 各自拼（`DMStartModal` / `ProjectMemberAddModal` 本地 `refOf`、`AssignModal` / `AgentDetail` 内联 `agent:`），**易漏前缀——正是 `#240` 根因**（`AgentDetail` 新增 Message 按钮时重新内联、漏前缀 → 400）。**目标态**：收敛到单一共享 helper（如 `identityRef(kind, id)`），使任何 call site 都无法再忘前缀——把规则从「文档约定」变「代码强制」（v2.8 task #254）。
- 发现一例 → **扫整类**（所有校验 identity-ref 的写端点 + 所有返回 id 给 ref-消费端的 tool），不是只补那一个实例（`#240`→`#241`→`#244` 即整类 sweep）。
- 坏 ref 应在 handler **400**（清晰），不漏进 domain 变 500。

**自检：** 我返回的 id 会被喂给校验 identity-ref 的端点吗？是的话我产出带前缀的完整 ref 了吗？我这条引用是 identity（带前缀）还是 entity（裸）？这类的其它实例我一起扫了吗？

> 背景与完整复盘见 [v271-retrospective.md § 5 / § 7](v271-retrospective.md)。

## § 13. 安全 / 隐私（v1 简化）

| 项 | 当前要求 |
|---|---|
| Worker ↔ Center 认证 | bootstrap token + session token（v2: BootstrapToken Entity stateful，[ADR-0023](../design/decisions/0023-worker-enroll-lightweight.md)） |
| Agent ↔ Worker daemon 通信 | 本机 unix socket，同机进程信任 |
| Admin CLI | 本机 unix socket / 同机用户身份；远程访问留口未实现 |
| 用户数据（消息内容、agent 输出） | 仅在 agent-center 内流转，不外发；BlobStore 内容遵从存储后端的访问控制 |
| **用户密钥**（MCP env vars / 云凭据等） | v2: 中心化在 [SecretManagement BC](../design/architecture/tactical/secret-management/00-overview.md) 管理（[ADR-0026](../design/decisions/0026-user-secret-management-bc.md)）；AES-GCM 加密；master key 配置文件（不入 DB）；CLI 不打印明文；引用语法 `secret:<name>` |
| **系统内部凭证** | 仍按本节其他行处理（各自 BC 自管）；**不**迁入 SecretManagement |

**自检：** 我的功能是否引入新的外部通信？凭据怎么管？数据是否会泄露到不该到的地方？

## § 14. 测试

测试规约展开在独立文档 [`testing.md`](testing.md)。本节只保留**纲领**与**自检**，细则去 testing.md。

核心硬约束：

- **单元测试行覆盖率 ≥ 90%**（整体 + diff），CI 阻断 < 90% 的合并
- **每次测试必须有测试计划 + 测试报告**，条目编号 1:1 对齐，**逐条确认**通过 / 失败 / 跳过 + 理由
- **关键路径 e2e**：`worker enroll → dispatch → 任务结束`、`Web Console 入口 → supervisor → task → 回流`
- **BlobStore 契约测试**：local 实现 / S3 mock 实现走同一份契约
- 设计 / 实现侧的可测性硬约束见下面 [§ 14.x](#-14x-设计与实现必须可测)

**自检：** 我的改动是否同时附了测试计划 + 测试报告？单元行覆盖率（整体 + diff）是否 ≥ 90%？关键路径有 e2e 吗？

### § 14.x 设计与实现必须可测

测试不是事后补的工序，**设计阶段与实现阶段都必须把"可测试"作为硬约束**。任何让测试只能靠"真连外部服务 / sleep / 真实超时巧合"才能跑通的设计，都视为设计缺陷。

**设计阶段必答：**

| 问题 | 必须回答 |
|---|---|
| 这个功能的外部依赖（DB / blob store / 飞书 SDK / 时钟 / 随机数 / 子进程 / 网络）有哪些？ | 列清单 |
| 每个外部依赖能否在测试中**替换为可控实现**（interface / port / 注入）？ | 是 / 否 + 说明 |
| 每类**异常路径**（timeout / 5xx / 网络中断 / 状态不一致 / 部分失败）能否**显式 mock 注入**？ | 是 / 否 + 说明 |
| 时间 / 随机 / 并发等非确定性来源如何注入？ | 说明 |
| 关键事件 / 副作用如何被测试**断言**到（事件流 / 显式同步原语，不靠 sleep）？ | 说明 |

**实现要求：**

- 业务逻辑**不直接持有具体依赖**；通过构造函数 / 参数 / 接口注入
- 凡是有 IO / 时钟 / 随机的位置，必须留出注入点
- "只能在生产环境复现的异常"是反模式 —— 必须把触发条件抽象到接口，让测试能主动制造
- 不允许给测试加 `sleep` 之类的等待 —— 改用可注入时钟或显式同步

**自检：** 我的接口能不能在不连真实外部服务的情况下覆盖所有正常+异常路径？如果不能，先改设计再写实现。

## § 15. 新功能 / 新设计自检清单

提交设计文档 / PR 前，逐条过：

- [ ] 单一来源（§1）：没有引入"绕过 center 造任务"的路径
- [ ] 可观测性（§2）：列出新 events、说明 trace 影响、扩展 inspect / ps / stats 的项
- [ ] AI Native（§3）：新 agent 能力同时有 skill + CLI，不走 MCP
- [ ] 零 LLM SDK（§4）：没新增 LLM 厂商 SDK 依赖
- [ ] 文档先行（§5）：设计落到 `docs/design/` 对应层；分叉决定有 ADR
- [ ] DDD 统一语言（§0）：新引入的术语 / 概念已归类到 DDD 范畴（Aggregate / Entity / VO / Domain Event / ...）；用词跟 [01 § 1.1](../design/architecture/strategic/03-bounded-contexts.md#-1-通用语言ubiquitous-language) 一致，没造同义词
- [ ] **AppService 唯一入口（§0.4）**：BC 外部代码（CLI / worker daemon / Web API handler / 其它 BC）通过 AppService transport 访问领域状态，**不允许直读底层存储**；single-host 与 multi-host 仅 transport 不同（unix socket vs TCP），AppService 表面相同；mock-as-default（NoopSender 等）在 production wiring 路径必须有 `// FIXME(prod-wiring)` tag
- [ ] **架构整改起点（§0.4）**：任何架构整改方案的起点是"领域模型要求什么"（what does the domain model require），不是"现状最小补丁"（smallest patch on current code）；提议时附"必答铁律"答卷
- [ ] 范围决策（§6）：新增的"不做"分到出范围 / 推迟正确位置
- [ ] 项目本地约定（§7）：不在 agent-center 里管项目自有的规则文档
- [ ] BlobStore（§8）：大字段走 BlobStore
- [ ] dialect-agnostic（§9）：SQL 跨 SQLite / PG 兼容
- [ ] Schema 无 FK（§9.w）：新增 DDL 不声明 FOREIGN KEY；引用完整性走应用层 Repository / Service
- [ ] BC 物理隔离（§9.z）：新增表 / 列归属唯一 BC；跨 BC 数据流走 events / RPC 不共享物理表
- [ ] 单一二进制（§10）：没新增独立 binary
- [ ] 渠道选对（§11）：明确走 Issue / InputRequest / Event 哪一条
- [ ] 命名（§12）：遵循术语表
- [ ] 安全（§13）：凭据 / 数据流转无新风险
- [ ] 测试（§14）：单元行覆盖 ≥ 90%（整体 + diff）；测试计划 + 报告齐全且条目 1:1 对齐；关键路径 e2e；详情见 [testing.md](testing.md)
- [ ] 可测性（§14.x）：外部依赖可注入、异常路径可 mock、无 sleep / 真连外部服务
- [ ] **Deployed-binary smoke（§0.4）**：每个 phase close 都有至少 1 个用真 binary（不全 mock）端到端跑过的 user path；test report 把 unit / integration-mocked / deployed-smoke 分层标注，deployed-smoke = 0 的 phase 不许 close
- [ ] reason + message 双字段（§16）：所有带 `reason` 的事件 / 字段同时携带 `message`
- [ ] 错误不吞（§17）：每个 `if err != nil` 分支要么 emit event / 改状态 / 通知 caller，要么 return err；log 不算处理；未知协议字段必须显式 emit
- [ ] **CRUD 完备性（§15.x）**：新增「被管理实体」的 repository / service / API 一次立全 CRUD + 生命周期 + cascade，或在 release note 显式标 deferred + 目标版本
- [ ] **ID 分层 / 引用格式（§12.x）**：用户可见处用 member-id 不漏 ULID；identity-ref 带前缀、entity-id 裸用；产出给 ref-消费端的 id 给完整可消费形态
- [ ] **Schema 变更独立单位（§9.4）**：schema 改动不夹带进特性 PR；5 个 `schema_version` 断言点 lockstep；新 config key 进 `collectKnownKeys`；新 NOT NULL 列 backfill + watermark；真升级 smoke

任何一项 ❌ → 退回修改。

## § 15.x 新「被管理实体」的 CRUD / 生命周期一次立全

给一个新的「被管理实体」（agent / worker / member / channel / project / issue / task …）立 bounded context 时，自然的拉力是只做**首个特性需要的 create + read + list 路径**，把 Delete / Update + cascade 留「以后」——这正是 v2.7.1 反复撞墙的完备性盲区：agent 无删除（`#197` 才补）、project member 无移除 UI（`#207` 才补）、DM 无 dedup（`#215` 才补）、channel URL 不一致（`#247` 才补）。

backend 规约（§11 产品力 / CRUD enumeration 的 backend 对偶）：

- 新被管理实体 = **完整 CRUD 面一次立全**：repository（Delete / Update）+ service（cascade / 生命周期语义：谁能删、删了关联怎么办、状态进出对称）+ API endpoint。或在 release note **显式标 deferred + 目标版本**——不留隐式 gap 等 dogfood 撞。
- **cascade / 引用完整性是 Delete 的一等公民**（删 agent → 其 membership / assignment 行怎么处理）。§9.w 不声明 FK，引用完整性由应用层 Repository / Service 负责，所以 cascade 必须在加这个实体时就显式设计，不是补 Delete 时才想。
- **入口对称性**：能创建就应能删除 / 能从列表移除；能进某状态（start / archive）就应能出（stop / unarchive）。
- 三道网：设计-enumerate（产品 checklist）/ 实现-立全（本节 backend）/ 验收-catch（Tester T-9：缺失基本操作 = finding，按用户对基本管理的合理预期判）。

**自检：** 我这个新实体的增删改查 + 生命周期 + cascade 是不是一次立全？省了哪个？省的有没有在 release note 显式 deferred + 标版本？Delete 的 cascade 语义定了吗？入口对称吗（能进能出 / 能建能删）？

> 背景见 [v271-retrospective.md § 11](v271-retrospective.md)（产品面 + Tester T-9 验收面）。

## § 16. 错误 / 状态信息双字段（reason + message）

凡是携带 `reason` 枚举的**事件 payload / RPC 响应 / 状态字段**，必须**同时**携带 `message` 字段。

| 字段 | 用途 |
|---|---|
| `reason` | **机器可读**枚举值，supervisor / center 据此路由决策（如 `worker_lost` / `dispatch_no_ack` / `worktree_path_busy`）|
| `message` | **人类可读**详细信息，落 inspect / 飞书卡片 / log（如 `"last heartbeat 2026-05-16T10:23 UTC"`） |

**适用场景**：

- `task_execution.failed { reason, message }`
- `task_execution.killed { reason, message }`
- `task_execution.kill_requested { reason, message }`
- `input_request.timed_out { reason, message }`
- `input_request.canceled { reason, message }`
- `DispatchNack { reason, message }`
- `worker.offline { reason, message }`
- 任何带"为什么"的状态终结 / 异常

**不需要**：

- 纯动作事件（如 `task.dependency_added` —— 没有"为什么"）
- 正常状态机迁移（如 `task_execution.working`）

**自检：** 我新加的字段 / 事件里有 `reason` 吗？是的话配 `message` 了吗？

## § 17. 错误显式化：处理 or 抛出，**不允许吞**

每个错误必须明示一种**显式动作**：

1. **处理** —— 捕获 + 做出 explicit 决定：emit domain event / 改变状态 / 转化业务结果 / 降级 / 重试 / 通知 caller 之一。**决定本身可观测**（与 [§ 2 可观测性](#-2-可观测性优先-observability-first) 对齐）。
2. **抛出** —— `return err` / `panic` 上传到能做决定的层。

**禁止的反模式**：

- `_ = err` / 显式丢弃
- `if err != nil { return nil }` —— 把错误转成 nil 给上游
- `if err != nil { log.Println(err) }` 然后继续 —— **log 不算处理**（容易被忽略，不可发现）
- 默认值兜底掩盖错误（解析失败返回 zero value）
- 未知协议 / 字段 / 类型当 noop 不上报（如 JSONL 未知 event type 直接归 Unknown 不 emit observability event）
- catch-all 不区分已知 / 未知错误
- 超时 / cancel 后假装成功

**为什么**：吞错让业务带病运行**没人知道**，是最难排查的错误模式 —— 排查时只能看见结果偏差找不到原因。直接违反 § 2 可观测性。

**怎么算"处理"**：

| 动作 | 算处理吗 |
|---|---|
| emit domain event 进 events 表 | ✅ |
| 改变状态机（如 → `failed(reason=...)`）| ✅ |
| 转化业务结果 + 业务事件（如 `unauthorized` → 登录页 + emit）| ✅ |
| 通过 return err 让 caller 处理 | ✅（caller 必须接） |
| 仅 log | ❌ |
| 仅 metrics counter +1 | ❌（要 emit event）|
| 返回默认值 / nil | ❌ |

**例外白名单**（极少数）：

- **best-effort cleanup**（如 `defer Close()` 错误）：可仅 log 不 emit，但**必须 log**，且仅限 cleanup 路径
- 已确认 deduplicate 后的 ignorable repeat（同一 reason 在短时间内重复出现）：可去重，但**首次必须 emit**

**自检：**

- 我每个 `if err != nil` 分支做了什么？符合上表"算处理"哪一行？
- 我的解析 / 协议层遇到未知字段 / 未知类型 / 未知 reason 时上报了吗？
- 我有没有 silently 返回默认值掩盖错误？

## § 18. 版本号格式

**规约**（@oopslink 2026-06-08 拍）：

```
version = ${branch}-${git-hash}
```

例如 `v2.8.1-9908825`（当前 v2.8.1 分支 commit 9908825）/ `main-d92a211` / `fix-some-feature-abc1234`。

**默认 build** = 当前 git branch + 短 commit hash（自动派生，无需手动维护）。

**Release tag override**（仅在打 release tag 时显式）：
```bash
make build VERSION=v2.8.1      # 仅在 release ceremony 时用
```
override 也仍带 commit hash 是最佳实践（`v2.8.1-9908825`），仅在外部 ship/标识需要 clean tag 时省略.

**不可省略 branch**：单 `v2.8.1` 不算合规（缺 branch 上下文不知是哪个 commit / 哪个 fork）。

**为什么**：
- 部署到客户/test instance 时 `agent-center --version` 输出含 branch 自然反映 "这是哪条线 / 哪个 commit"
- v2.7.1 ship-gap 教训：仅靠 tag 名验证落地易出 evidence-binding 错（同 tag 不同 commit）
- branch-hash 是 unambiguous identity
- 与 [§ 19 worktree isolation](#-19-worktree-isolation) + [§ 20 content-delta verify](#-20-content-delta-verify) 同精神：identity 必须 unambiguous + verifiable

**落实**：

- `Makefile`：`VERSION ?= $(BRANCH)-$(COMMIT)`（自动），覆盖通过 env 传 `VERSION=tag-name`
- 所有打包/编译/install 脚本 inherit Makefile VERSION
- `agent-center --version` / banner / 安装包文件名 用同一 VERSION

**自检：**

- `make build` 产出的二进制 `--version` 显示 branch + hash 吗？
- Release tarball 文件名 含 branch + hash 吗？
- 我手动改 VERSION 时, 是否仍保留 commit hash 后缀？

## § 19. 测试 Ownership 按 scope 分（不按语言/目录/PR 系列）

**规约**（@oopslink 2026-06-08 拍 — PR7 e2e 误派事件后立）：

测试任务的 ownership 由**测试 scope** 决定, 不由**语言/目录/PR 系列名**决定。

| 测试 scope | Ownership | 例子 |
|---|---|---|
| 单模块单测（unit）| **Dev / Dev2**（写实现者）| `internal/agent/work_item_test.go` 测 WorkItem AR 行为 |
| 跨模块集成（integration）| **Tester** | `tests/integration/d_pull_flow_test.go` 串 D 全流程 |
| 端到端 acceptance（e2e）| **Tester** | "assign → queue → wake → start_work → complete → done" 全链 |
| Runtime / 真-LLM 行为（real-LLM behavioral）| **Tester2** | 真 claude agent 按 prompt 决策、必复 mention、真 pause |
| UI/UX run-real | **Tester2** | 浏览器渲染 + computed-truth + 多模态 / 时区 emulation |

**不决定 ownership 的因素**：

- **语言**：Go integration test 不归 Dev（PR7 e2e 是 Go 但 scope 是 integration → Tester）
- **目录**：`internal/...` 下的测试也可能是 integration scope（看断言跨模块还是单模块）
- **PR 系列名**：D rollout PR1-PR7 是 Dev 主导，但 PR7 = e2e 是 Tester scope，**series 不决定 ownership**

**为什么这条规则**：

- 之前误以为 "PR7 是 D 系列 → Dev 写" → @oopslink 立即指出错配
- 根因：默认 **by-series** 而非 **by-scope**（应 by-scope）
- 后果：Dev 揽过多 lane（impl + integration），Tester acceptance lane 被削弱
- Fix：每个 PR 的测试任务派人，先看 **scope**，再决定 ownership

**自检（PD 派任务时）：**

- 这个 PR 的测试是 **单模块单测** vs **跨模块集成** vs **e2e/acceptance** vs **真-LLM behavioral** vs **UI run-real**？
- 按 scope 派给对应 owner（Dev / Tester / Tester2），不按谁写实现 / 在什么目录 / 哪个 PR 系列。
- 如果一个 PR 包含多 scope（罕见），按 scope 拆成多 PR / 多任务派给各自 owner。

**与 § 14 测试规约 互补**：§ 14 讲测试**怎么写**（TDD、覆盖率、不 mock 等），§ 19 讲测试**谁来写**（ownership by scope）。

## § 20. API 无隐含状态（显式上下文进路径）

**API 的返回不能依赖隐含的请求上下文**（session cookie / query 参数推断的当前-org 等）。同一个路径在不同隐含上下文下返不同数据 = **隐含状态**，脆且易踩坑。**上下文要显式进资源路径**。

- **反例（隐含状态）**：`GET /api/members`（org-scoped、但「当前哪个 org」从 session 或 `?org_slug=` query 隐式推断）→ 同路径不同 org 返不同 members；nav 没带 org-slug → 隐式 org 上下文空 → members 空。
- **正例（显式）**：org-scoped 资源挂在 org 路径下：`GET /api/orgs/{slug}/members`、`/api/orgs/{slug}/...` —— org 是资源路径的一部分、不是隐藏的 query/session 状态；也和前端 URL 已经是 `/<org-slug>/...` 对齐。
- 红线 org-isolation 仍由路径里的 `{slug}` + membership 校验保证，不靠隐式上下文。

**为什么这条规则**：

- 2026-06-10 v2.8.1 ship review，@oopslink 看到 `/api/members` 指出「这种 api 不太好、有隐含状态」。根因：org 上下文隐式（query/session），导致同路径不同结果、并多次造成 run-real 验收「members 空 / mention 解析不到」的坑。
- @oopslink 指令：把「API 无隐含状态」加进规约 + 全站 org-scoped 路由显式化作为 **v2.9 架构项**（重构所有 org-scoped 路由为 `/api/orgs/{slug}/...`，非 v2.8.1 范围）。

**自检（设计 / review API 时）：**

- 这个 endpoint 的返回是否依赖隐含上下文（session / query 推断的 org 等）？同路径不同上下文会返不同数据吗？
- 若是 → 把上下文显式进路径（`/api/orgs/{slug}/...`），消除隐含状态。
- org-isolation 靠路径 `{slug}` + membership 校验，不靠隐式推断。
