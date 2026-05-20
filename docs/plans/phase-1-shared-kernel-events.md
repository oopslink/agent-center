# Phase 1: Shared Kernel + Events 总线

> DDD Shared Kernel (Workforce / Conversation) + Open Host (Observability events 表) · 依赖 Phase — · 解锁 Phase 2 (TaskRuntime) / Phase 3 (Discussion) / Phase 4 (Observability 查询面)
> 纪律：按里程碑顺序 / 模块完备不半成品 / 单测 ≥ 90% + 集成 + e2e + 测试报告

## § 0. 目标

本 phase 落地 **Shared Kernel BC 全部战术工件** + **Open Host events 总线第一半（emit 侧）**。交付能力：

- **运维视角**：能起 `agent-center server`、跑 migration、加 Project、enroll Worker；events 表持续记录所有动作的领域事件，可作后续审计 / supervisor 输入流的素材
- **用户视角**：可通过 CLI 完成"注册 worker → 看 worker 自动发现的 proposal → 接受 / 忽略 → 加 project → 写 conversation message"完整管理面闭环；不依赖任何 vendor（飞书）
- **DDD 意义**：本 phase 落 [Shared Kernel BC](../design/architecture/strategic/03-bounded-contexts.md#-3-上下文映射context-map)（Workforce / Conversation）全部战术工件 + Open Host 总线 emit 侧（[ADR-0014 § 2](../design/decisions/0014-event-sourcing-level.md) 同事务双写机制 + EventSink 注入点）；订阅 / 投影 / 五动词查询面留 [Phase 4](phase-4-observability.md)

本 phase **不涉及** TaskRuntime / Discussion / Cognition / Bridge / TraceArchive / Observability 查询面 / Projection（见 § 0 之外 OUT 清单）。

---

## § 1. DDD 工件清单

### 1.1 Aggregate Roots

| BC | AR | 来源文档 |
|---|---|---|
| Workforce | **Worker** | [workforce/01-worker.md](../design/architecture/tactical/workforce/01-worker.md) |
| Workforce | **Project** | [workforce/02-project.md](../design/architecture/tactical/workforce/02-project.md) |
| Workforce | **WorkerProjectProposal** | [workforce/03-worker-project-proposal.md](../design/architecture/tactical/workforce/03-worker-project-proposal.md) |
| Conversation | **Conversation** | [conversation/01-conversation.md](../design/architecture/tactical/conversation/01-conversation.md) |
| Observability | **Event** | [observability/00-overview.md § 1.1](../design/architecture/tactical/observability/00-overview.md) |

> Identity AR 留 Phase 5（Bridge）；v1 单用户简化，Conversation 写 Message 时 sender_identity_id 用形式化字符串（`user:hayang`/`system`/`agent:<sid>`），暂不校验 Identity 行存在（落 Phase 5 时补回）。

### 1.2 Entities（子从属）

| Entity | 从属 AR | 物理表 |
|---|---|---|
| **WorkerProjectMapping** | Worker | `worker_project_mappings`（独立表，归属 Worker 聚合） |
| **Message** | Conversation | `messages`（独立表，归属 Conversation 聚合） |

### 1.3 Value Objects

| VO | 用在 | 说明 |
|---|---|---|
| `WorkerID` / `ProjectID` / `MappingID` / `ProposalID` / `ConversationID` / `MessageID` / `EventID` | 所有 AR | typed string alias（不裸 string） |
| `WorkerStatus` (online / offline) | Worker | [workforce/01-worker § 1](../design/architecture/tactical/workforce/01-worker.md) |
| `ProposalStatus` (pending / accepted / ignored / superseded) | Proposal | [workforce/03 § 1](../design/architecture/tactical/workforce/03-worker-project-proposal.md) |
| `ProjectKind` (coding / writing / investing / null) | Project / Proposal | enum + 应用层校验 |
| `InvalidateReason` (path_missing / not_git_repo / manual_remove) | Mapping invalidate | reason+message 双字段（[conventions § 16](../rules/conventions.md)） |
| `ConversationKind` (dm / group_thread / adhoc / notification / task / issue) | Conversation | [conversation/01 § 2](../design/architecture/tactical/conversation/01-conversation.md) |
| `ConversationStatus` (open / closed) | Conversation | 2 态状态机 |
| `MessageContentKind` (text / system / agent_finding / supervisor_summary / conclusion_draft / task_proposal) | Message | 6 种闭集 |
| `MessageDirection` (inbound / outbound / internal) | Message | 不可变 |
| `EventType` | Event | `<bc>.<entity>.<action>` 命名规范 |
| `EventRefs` | Event | JSON `{task_id?, worker_id?, ...}` |
| `Actor` | Event / Message | 形式化字符串 |
| `ReasonMessage` | 各终态 | [conventions § 16](../rules/conventions.md) reason + message 双字段对 |
| `CandidateMetadata` | Proposal | JSON `{git_remote_url, commit_count, recent_activity_at, detected_language}` |
| `BootstrapToken` / `SessionToken` | Worker enroll | 短期一次性 / 长期 |

### 1.4 Repositories

| BC | Repository | 接口出处 |
|---|---|---|
| Workforce | `WorkerRepository` | [workforce/00 § 5.1](../design/architecture/tactical/workforce/00-overview.md) |
| Workforce | `WorkerProjectMappingRepository` | [workforce/00 § 5.2](../design/architecture/tactical/workforce/00-overview.md) |
| Workforce | `WorkerProjectProposalRepository` | [workforce/00 § 5.3](../design/architecture/tactical/workforce/00-overview.md) |
| Workforce | `ProjectRepository` | [workforce/00 § 5.4](../design/architecture/tactical/workforce/00-overview.md) |
| Conversation | `ConversationRepository` | [conversation/00 § 5.1](../design/architecture/tactical/conversation/00-overview.md) |
| Conversation | `MessageRepository` | [conversation/00 § 5.2](../design/architecture/tactical/conversation/00-overview.md) |
| Observability | `EventRepository` | [observability/00 § 5.1](../design/architecture/tactical/observability/00-overview.md) |

全部 Domain errors（sentinel pattern）按 § 5.x 各小节落 Go `var Err* = errors.New(...)`。

### 1.5 Domain Services

| BC | Service | 职责 |
|---|---|---|
| Workforce | `WorkerEnrollService` | bootstrap → session 兑换 + Worker 入库 + emit `worker.enrolled` |
| Workforce | `ProjectDiscoveryService` | accept Proposal 时复用 / 建 Project（无 v1 worker-side scan loop；scan loop 留 Phase 2/5）|
| Workforce | `ProposalAcceptanceService` | Proposal accept / ignore / unignore 状态推进 + 单事务建 Project + Mapping（[ADR-0008](../design/decisions/0008-worker-project-mapping-via-discovery-proposal.md)） |
| Conversation | `MessageWriter` | `AddMessageCommand` → 同事务写 Message + emit `conversation.message_added`（Phase 1 不接 Bridge outbound） |
| Observability | `EventSink` | 各 BC 同事务双写注入点（state UPDATE + events INSERT 同 tx）|

### 1.6 Application Services（CLI handler 层）

每个 CLI 命令对应一个 handler；handler 负责 ctx / tx 边界、参数解析、调 Domain Service、输出格式。具体命令列表见 § 3.5-3.7（按 BC 分）。

### 1.7 Domain Events（emit 给 events 表）

Phase 1 emit 的事件清单（[observability § 7.5](../design/architecture/tactical/observability/00-overview.md)）：

**Workforce**：
- `worker.enrolled` — Worker 入库（payload: worker_id, capabilities）
- `worker_project_proposal.proposed` — Proposal 入库（CLI 模拟提交，因 Phase 1 无 worker scan loop）
- `worker_project_proposal.accepted` — accept 终态
- `worker_project_proposal.ignored` — ignore 终态
- `worker_project_proposal.unignored` — ignored → pending
- `worker_project_mapping.added` — mapping 建（accept 同事务）
- `project.created` / `project.updated` / `project.removed` — Project CRUD

**Conversation**：
- `conversation.opened` — Conversation 创建（kind ∈ {dm/group_thread/adhoc/notification}，kind=task/issue 留下一 phase）
- `conversation.message_added` — 写一条 Message
- `conversation.closed` — Conversation 终态

**跨 BC 集成验证**：worker enroll CLI → emit `worker.enrolled` event；assert events 表确有此行（§ 5.3 e2e）。

### 1.8 Context Map 关系

| 关系 | 类型 | 本 phase 体现 |
|---|---|---|
| Workforce ↔ TaskRuntime | Shared Kernel | TaskRuntime 留 Phase 2；本 phase 仅完成 Workforce 自身 |
| Workforce ↔ Discussion | Shared Kernel | Discussion 留 Phase 3；本 phase 仅 Workforce 自身 |
| Conversation ↔ TaskRuntime / Discussion | Shared Kernel / 1:1 | 留 Phase 2/3；本 phase 不涉及 kind=task/issue Conversation 创建 |
| **Observability ← ALL** | **Open Host** | 本 phase 落 emit 侧：EventSink + events 表 INSERT 链路；订阅 / 投影 / 查询面留 Phase 4 |

---

## § 2. 上游依赖

无（Phase 1 是起点）。

| 上游工件 | 来源 | 本 phase 哪一步用 |
|---|---|---|
| — | — | — |

---

## § 3. 工作项分解（严格按依赖顺序）

> **依赖序**：基础设施 → events 总线 → Workforce → Workforce CLI → Conversation → Conversation CLI → 跨 BC 集成验证。

### 3.1 基础设施 — persistence / migration / config / binary 骨架

#### 3.1.1 SQLite 连接 + tx helper

- **工件**：`internal/persistence/db.go` + `internal/persistence/tx.go`
- **类型**：infrastructure package
- **输入**：依赖 [02-persistence § 1.2 / § 5](../design/implementation/02-persistence-schema.md) DSN + WithTx/TxFromCtx 设计
- **输出**：
  - `Open(dsn string) (*sql.DB, error)` — DSN 必带 `_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON&_synchronous=NORMAL`
  - `WithTx(ctx, tx) context.Context` / `TxFromCtx(ctx) (*sql.Tx, bool)`
  - `sqlExecutor` interface + helper：Repository 实现共用，根据 ctx 选 Tx / DB
- **实现步骤**：
  1. 引入 `modernc.org/sqlite` (纯 Go port，[02-persistence § 1.1](../design/implementation/02-persistence-schema.md))
  2. Open 时 set `db.SetMaxOpenConns(1)`（SQLite 写串行）+ `SetMaxIdleConns(1)`
  3. tx.go：`txKey` 私有类型 + `WithTx` / `TxFromCtx` + `sqlExecutor` interface
- **对位 P8b**：[02-persistence § 1 / § 5](../design/implementation/02-persistence-schema.md)
- **DoD**：`go test ./internal/persistence/...` pass；e2e tx 跨 Repository 测试（§ 5.2-INT-1）通过

#### 3.1.2 Migration framework（golang-migrate + embed FS）

- **工件**：`internal/persistence/migrations/` + `internal/persistence/migrator.go`
- **类型**：infrastructure
- **输入**：[02-persistence § 6](../design/implementation/02-persistence-schema.md)
- **输出**：
  - `0001_init.up.sql` / `0001_init.down.sql` — 建本 phase 全部表（events + workers + worker_project_mappings + worker_project_proposals + projects + conversations + messages）
  - `Migrator.Up(ctx) error` / `Migrator.Down(ctx) error` 包装 golang-migrate API
- **实现步骤**：
  1. 引入 **手写 migrator**（`internal/persistence/migrator.go`，约 200 行，embed FS + `schema_migrations` 表）—— 取代 `github.com/golang-migrate/migrate/v4`：后者拖一组自有驱动注册路径，与 `modernc.org/sqlite` 同时注册容易冲突；本 phase 只 1 条 migration，自实现更小可测可控
  2. `//go:embed migrations/*.sql` 注入
  3. DDL 按各 BC 切片严格遵守 [02-persistence § 3 / § 4 / § 7](../design/implementation/02-persistence-schema.md)：TEXT 时间戳 / INTEGER 0/1 boolean / TEXT JSON / ULID PK / version 乐观锁 / `_reason` + `_message` 平铺
  4. events 表 DDL 完全按 [02-persistence § 8.2.1](../design/implementation/02-persistence-schema.md)（无 version 列、index 含 `correlation_id` / `decision_id` partial）
- **对位 P8b**：[02-persistence § 6 / § 7 / § 8.2](../design/implementation/02-persistence-schema.md)
- **DoD**：`Up` + `Down` 全部 idempotent；空库 `Up` 后所有表存在；`Down` 后所有表清空（验证脚本 § 5.2-INT-2）

#### 3.1.3 ULID 生成 + 时间封装

- **工件**：`internal/idgen/ulid.go` + `internal/clock/clock.go`
- **类型**：infrastructure
- **输入**：[02-persistence § 2](../design/implementation/02-persistence-schema.md)
- **输出**：
  - `NewULID() string` — `oklog/ulid/v2` MonotonicEntropy；线程安全
  - `Clock` interface + `SystemClock` / `FakeClock`（[conventions § 14.x](../rules/conventions.md) 时间可注入）
- **实现步骤**：
  1. 引入 `github.com/oklog/ulid/v2`
  2. 单例 monotonic source + `sync.Mutex`
  3. `Clock.Now() time.Time` interface，所有业务代码注入获取时间
- **DoD**：`go test ./internal/idgen/...` + `go test ./internal/clock/...` pass；ULID 同毫秒严格递增（table-driven test）

#### 3.1.4 单 binary + mode + config loader

- **工件**：`cmd/agent-center/main.go` + `internal/config/loader.go`
- **类型**：CLI 入口 + infrastructure
- **输入**：[04-configuration § 1-5 / § 7.1-7.2 / § 7.7](../design/implementation/04-configuration.md)
- **输出**：
  - `agent-center` 单 binary（[conventions § 10](../rules/conventions.md)）
  - 顶层子命令：`server` / `supervisor` / `worker` / `migrate` / `admin blob-migrate` / `version` + § 3.5-3.7 各 BC 子命令
  - Config loader：YAML → struct，env override（`AGENT_CENTER_*`），CLI flag override，fail-fast 验证（[04 § 2 / § 4](../design/implementation/04-configuration.md)）
- **实现步骤**：
  1. 选 CLI 框架 —— **本 phase 选 stdlib `flag` + 手写 sub-command router**（不引 cobra/viper）。R8 spike 决断：cobra 表面 OK 但额外依赖体积大，对 Phase 1 命令面足够时反而拖累；手写 router 约 150 行，完全可测，后续 phase 命令增加再评估。配置走 § 3.1.4 手写 loader（YAML + env override + unknown-key fail-fast）
  2. `cmd/agent-center/main.go` 注册顶层 verbs
  3. config: 解析顺序 file → env → flag；schema 用 struct + `mapstructure` / 等价 tag；fail-fast 时按 [04 § 4](../design/implementation/04-configuration.md) 输出诊断到 stderr
  4. `version` 子命令打印 `runtime/debug.BuildInfo` 内容
  5. `migrate` 子命令调 § 3.1.2 Migrator
  6. `admin blob-migrate`：Phase 1 仅注册 stub + 报 `not implemented in phase 1`（exit 64）—— **不算半成品**，因为 BlobStore 自己留 Phase 2+；CLI surface 一次性注册避免后期 mode 入口变更
  7. `supervisor` mode：Phase 1 同样 stub（Cognition 留 Phase 6）；`worker` mode（启动 daemon）也 stub（worker 端运行时是 Phase 2 TaskRuntime 内容）
- **对位**：[03-cli § 8.8](../design/implementation/03-cli-subcommands.md) + [04-configuration § 6](../design/implementation/04-configuration.md)
- **DoD**：
  - `agent-center version` 输出 build info
  - `agent-center server --config=tests/fixtures/server.yaml` 起服务后停（健康路径 + 自动 migrate.Up）
  - `agent-center migrate --target=0` / `agent-center migrate` 双向跑通
  - 未知 YAML 字段 → exit 2 + 准确诊断（§ 5.1-CFG-1..4）
  - `supervisor` / `worker` / `admin blob-migrate` 子命令存在但报 not_implemented（不影响其他 mode）

### 3.2 Observability — events 表 + EventRepository + EventSink

#### 3.2.1 Event AR + VO

- **工件**：`internal/observability/domain/event.go`
- **类型**：Aggregate Root + VO（[observability § 1.1](../design/architecture/tactical/observability/00-overview.md)）
- **输入**：[observability § 1.1 / § 1.3 / § 2](../design/architecture/tactical/observability/00-overview.md)
- **输出**：
  - `Event` struct（append-only；含 id / occurred_at / seq / event_type / refs / actor / payload / correlation_id / decision_id）
  - `EventType` typed string alias
  - `EventRefs` struct（typed fields + Marshal/Unmarshal JSON）
  - `Actor` typed alias + parse 验证 prefix（`user:` / `supervisor:` / `worker:` / `agent:` / `system`）
  - Validate() — invariant 校验（reason+message 配对 / actor 必填 / occurred_at 非零）
- **实现步骤**：
  1. immutable 字段（无 setter，仅 constructor）
  2. `NewEvent(type, refs, actor, payload, ...)` factory：clock 注入；ULID 生成；validate
- **DoD**：单测覆盖 ≥ 90%（§ 5.1-EV-1..6）

#### 3.2.2 EventRepository（SQLite 实现）

- **工件**：`internal/observability/persistence/event_repo.go` + `event_repo_test.go`
- **类型**：Repository 实现（[observability § 5.1](../design/architecture/tactical/observability/00-overview.md)）
- **输入**：`EventRepository` interface（架构层契约）+ events 表 DDL
- **输出**：
  - `Append(ctx, *Event) error` — INSERT only；走 `sqlExecutor(ctx)`（[02-persistence § 5](../design/implementation/02-persistence-schema.md) Tx via ctx）
  - `FindByID(ctx, EventID) (*Event, error)`
  - `Find(ctx, EventQueryFilter) ([]*Event, error)` — cursor 分页（ULID 字典序兼容）
  - Domain errors：`ErrEventNotFound` / `ErrEventImmutable`
  - seq 监 ed 实现：应用层 `atomic.Int64`；启动时 `SELECT MAX(seq) FROM events` 初始化
- **实现步骤**：
  1. `Append`：构造 INSERT；`refs` / `payload` marshal JSON；外部不允许传 id 已存在（ULID 冲突应跑 retry，单测覆盖）
  2. `Find`：dynamic WHERE 拼接（按 filter）；ULID cursor `id > ?`
  3. 试图 UPDATE / DELETE → 不暴露这些方法（编译期保护）
- **对位 P8b**：[02-persistence § 8.2](../design/implementation/02-persistence-schema.md)
- **DoD**：§ 5.1-ER-1..8 + § 5.2-INT-3 通过

#### 3.2.3 EventSink Domain Service

- **工件**：`internal/observability/service/event_sink.go`
- **类型**：Domain Service（[observability § 3.1](../design/architecture/tactical/observability/00-overview.md)）
- **输入**：`EventRepository`
- **输出**：
  - `EventSink.Emit(ctx, EmitCommand) (EventID, error)`，`EmitCommand{ EventType, Refs, Actor, Payload, CorrelationID?, DecisionID? }`
  - ctx 内必须有 tx（由 caller BeginTx + WithTx 注入）；不允许 sink 自己 BeginTx —— **tx 边界归 application service**（[02-persistence § 5](../design/implementation/02-persistence-schema.md) 约束）
  - clock + idgen 注入
- **实现步骤**：
  1. 校验 `EmitCommand` 字段（event_type 非空、actor 非空、refs JSON 合法）
  2. 构造 Event AR → 调 `repo.Append(ctx, e)`
  3. seq 由内部 atomic counter 分配；启动时 `SELECT MAX(seq)` 初始化
  4. 校验：若 ctx 内无 tx → 单元测试场景可降级（直接走 db），但 production 路径必须有 tx；通过 `Emit` 文档 + 测试约定
- **对位**：[ADR-0014 § 2](../design/decisions/0014-event-sourcing-level.md) 同事务双写
- **DoD**：§ 5.1-ES-1..5 + § 5.2-INT-4（同事务 state UPDATE + events INSERT）通过

### 3.3 Workforce — 4 AR + Repository（无 Service / CLI）

> 落 AR + Repository SQLite 实现；Service / CLI 留 § 3.4-3.5。

#### 3.3.1 Worker AR + WorkerProjectMapping Entity

- **工件**：`internal/workforce/domain/worker.go` + `worker_project_mapping.go`
- **类型**：AR + Entity（[workforce/01](../design/architecture/tactical/workforce/01-worker.md)）
- **输出**：
  - `Worker` struct（id / status / capabilities JSON / last_heartbeat_at / version）+ 状态机 method（`Enroll`/`MarkOnline`/`MarkOffline`/`Heartbeat`）
  - `WorkerProjectMapping` struct + `Invalidate(reason, message)` method
  - Invariants 实现：[workforce/01 § 7](../design/architecture/tactical/workforce/01-worker.md) + § 4.5
- **DoD**：§ 5.1-W-1..8 通过

#### 3.3.2 Project AR

- **工件**：`internal/workforce/domain/project.go`
- **类型**：AR（[workforce/02](../design/architecture/tactical/workforce/02-project.md)）
- **输出**：`Project` struct + slug 验证（lowercase / hyphenated）+ Invariants（§ 5 Project Invariants）
- **DoD**：§ 5.1-P-1..5 通过

#### 3.3.3 WorkerProjectProposal AR

- **工件**：`internal/workforce/domain/proposal.go`
- **类型**：AR（[workforce/03](../design/architecture/tactical/workforce/03-worker-project-proposal.md)）
- **输出**：4 态状态机 + 状态转移 method（`Accept(resultingMappingID)` / `Ignore` / `Unignore` / `Supersede`）+ Invariants
- **DoD**：§ 5.1-PR-1..7 通过

#### 3.3.4 4 个 Repository SQLite 实现

- **工件**：`internal/workforce/persistence/{worker_repo,mapping_repo,proposal_repo,project_repo}.go`
- **类型**：Repository 实现（[workforce/00 § 5.1-5.4](../design/architecture/tactical/workforce/00-overview.md)）
- **输入**：各 Repository interface + DDL（§ 3.1.2 migration 已建表）
- **输出**：
  - `WorkerRepository` 实现：`FindByID` / `FindByStatus` / `FindAll` / `Save` / `UpdateStatus`(CAS) / `UpdateLastHeartbeatAt`
  - `WorkerProjectMappingRepository`：`FindByWorkerID` / `FindByProjectID` / `FindByWorkerAndProject` / `Save` / `Invalidate`
  - `WorkerProjectProposalRepository`：`FindByID` / `FindByWorkerID(status...)` / `FindPending` / `FindByCandidatePath` / `Save` / `UpdateStatus`(CAS)
  - `ProjectRepository`：`FindByID` / `FindAll` / `Save` / `Update`(CAS) / `Delete`（带 `ErrProjectHasActiveDeps` 检查 → 查 mappings 表 active 行；Phase 1 task 表不存在，仅查 mapping）
  - 全部 sentinel errors（[workforce/00 § 5.1-5.4](../design/architecture/tactical/workforce/00-overview.md)）
  - 全部走 `sqlExecutor(ctx)` 支持 tx
- **对位**：DDL 落代码时按 [02-persistence § 1-7](../design/implementation/02-persistence-schema.md) 元层规则
- **DoD**：§ 5.1-WR/PR-1..10 + § 5.2-INT-5..7 通过

### 3.4 Workforce — Domain Services

#### 3.4.1 WorkerEnrollService

- **工件**：`internal/workforce/service/enroll.go`
- **类型**：Domain Service（[workforce/00 § 3.1](../design/architecture/tactical/workforce/00-overview.md)）
- **输入**：`WorkerRepository` + `EventSink` + `Clock` + `IDGen`
- **输出**：
  - `Enroll(ctx, EnrollCommand) (EnrollResult, error)`：BeginTx → Save Worker → emit `worker.enrolled` → Commit
  - **Phase 1 简化**：不做真实 bootstrap token / session token 兑换（mock 接口；token 生成留 Phase 6 Cognition 或 Phase 7 Bridge inbound）；CLI 直接接受 `--worker-id` + `--capabilities`
  - 返回 `WorkerID` + emit 的 `EventID`
- **错误处理**：worker_id 重复 → `ErrWorkerAlreadyExists`（[conventions § 17](../rules/conventions.md) 不吞）
- **DoD**：§ 5.1-WES-1..4 + § 5.2-INT-8 通过

#### 3.4.2 ProjectDiscoveryService（精简版）

- **工件**：`internal/workforce/service/project_discovery.go`
- **类型**：Domain Service
- **职责（Phase 1 范围）**：辅助函数 `EnsureProject(ctx, projectID, name, kind)`：若 Project 存在 → 返回；否则同事务建 + emit `project.created`
- **不含**：worker 端 scan loop / candidate metadata 抓取（留 Phase 2+ 或合并到 Cognition phase）
- **DoD**：§ 5.1-PDS-1..3 通过

#### 3.4.3 ProposalAcceptanceService

- **工件**：`internal/workforce/service/proposal_acceptance.go`
- **类型**：Domain Service（合并 [workforce/00 § 3.3](../design/architecture/tactical/workforce/00-overview.md) ProposalReviewService 职责，重命名贴需求）
- **输入**：`WorkerProjectProposalRepository` / `ProjectRepository` / `WorkerProjectMappingRepository` / `EventSink`
- **输出**：
  - `Accept(ctx, AcceptCommand) (AcceptResult, error)`：BeginTx → CAS update proposal → ensureProject → Save Mapping → emit `worker_project_proposal.accepted` + `worker_project_mapping.added` + `project.created`（如新建） → Commit
  - `Ignore(ctx, IgnoreCommand) error`
  - `Unignore(ctx, IgnoreID) error`
  - `Propose(ctx, ProposeCommand) (ProposalID, error)` — Phase 1 由 CLI（模拟 worker scan）触发；emit `worker_project_proposal.proposed`
- **错误处理**：proposal not pending → `ErrProposalAlreadyTerminated`；project_id 冲突且不同 worker_id 已映射 → `ErrMappingAlreadyActive`
- **跨聚合一致性**：单 tx 内写 Proposal + Project + Mapping + 3-4 个 events ([ADR-0014 § 2](../design/decisions/0014-event-sourcing-level.md))
- **DoD**：§ 5.1-PAS-1..9 + § 5.2-INT-9 通过

### 3.5 Workforce — CLI handlers

按 [03-cli § 8.3](../design/implementation/03-cli-subcommands.md)：

| 命令 | handler 文件 | 调用 |
|---|---|---|
| `worker enroll --worker-id=<id> [--capabilities=...]` | `cmd/agent-center/worker_enroll.go` | WorkerEnrollService.Enroll |
| `worker list [--status=...]` | `cmd/agent-center/worker_list.go` | WorkerRepository.FindByStatus / FindAll |
| `worker status <worker_id>` | `cmd/agent-center/worker_status.go` | WorkerRepository.FindByID |
| `worker proposal list [--worker-id=...] [--status=...]` | `cmd/agent-center/proposal_list.go` | ProposalRepository.FindByWorkerID / FindPending |
| `worker proposal show <id>` | `cmd/agent-center/proposal_show.go` | ProposalRepository.FindByID |
| `worker proposal propose --worker-id=<id> --candidate-path=<path> [--suggested-project-id=...]` | `cmd/agent-center/proposal_propose.go` | ProposalAcceptanceService.Propose（Phase 1 CLI 直接触发，模拟 worker scan）|
| `worker proposal accept <id> [--project-id=<slug>] [--kind=...]` | `cmd/agent-center/proposal_accept.go` | ProposalAcceptanceService.Accept |
| `worker proposal ignore <id>` | `cmd/agent-center/proposal_ignore.go` | ProposalAcceptanceService.Ignore |
| `worker proposal unignore <id>` | `cmd/agent-center/proposal_unignore.go` | ProposalAcceptanceService.Unignore |
| `project add <slug> --name=... [--kind=...] [--default-agent-cli=...]` | `cmd/agent-center/project_add.go` | ProjectRepository.Save + EventSink |
| `project list [--kind=...]` | `cmd/agent-center/project_list.go` | ProjectRepository.FindAll |
| `project show <slug>` | `cmd/agent-center/project_show.go` | ProjectRepository.FindByID |
| `project update <slug> [--name=...] [--kind=...] [--default-agent-cli=...]` | `cmd/agent-center/project_update.go` | ProjectRepository.Update（CAS）+ EventSink |
| `project remove <slug>` | `cmd/agent-center/project_remove.go` | ProjectRepository.Delete + EventSink |

**统一规约**（所有 handler）：
- 走 [03-cli § 4 / § 5 / § 6](../design/implementation/03-cli-subcommands.md)：`--format=human|json|yaml` / exit code 0/1/2/16/17/18/19 / `{error: {reason, message}}` JSON
- 每个 handler 单元测试覆盖 happy / error / format 三类（§ 5.1-CLI-*）
- worker run / worker shim Phase 1 stub（worker daemon 留 Phase 2）

**DoD**：§ 5.1-CLI-W*..P* + § 5.3-E2E-1..3 通过

### 3.6 Conversation — AR + Repository + Domain Service

#### 3.6.1 Conversation AR + Message Entity

- **工件**：`internal/conversation/domain/conversation.go` + `message.go`
- **类型**：AR + Entity（[conversation/01](../design/architecture/tactical/conversation/01-conversation.md)）
- **输出**：
  - `Conversation` struct + `Open` / `Close(reason, message)` + Invariants（§ 6）
  - `Message` struct + immutability enforcement（只允许 vendor_msg_ref 一次回填）
  - VO：`ConversationKind` (Phase 1 仅 dm/group_thread/adhoc/notification 是合法的"自由创建"kind；task/issue 留 Phase 2/3 跨 BC 路径)
  - validate：`closed` 不接 add-message；message direction 不可变；sender_identity_id 非空校验
- **DoD**：§ 5.1-CV-1..7 + § 5.1-MS-1..6 通过

#### 3.6.2 ConversationRepository + MessageRepository SQLite 实现

- **工件**：`internal/conversation/persistence/conversation_repo.go` + `message_repo.go`
- **类型**：Repository 实现（[conversation/00 § 5.1 / § 5.2](../design/architecture/tactical/conversation/00-overview.md)）
- **输出**：
  - `ConversationRepository`：`FindByID` / `Find(filter)` / `FindByChannelAndThreadKey` / `Save` / `UpdateStatus` / `UpdatePrimaryChannel`
  - `MessageRepository`：`FindByConversationID(filter)` / `FindByVendorMsgRef` / `FindRecent` / `Append`（append-only）/ `UpdateVendorMsgRef`（唯一允许的 mutate）
  - 全部 sentinel errors
  - `Append` 后非 `vendor_msg_ref` 字段 UPDATE → `ErrMessageImmutable`
- **DoD**：§ 5.1-CR/MR-1..10 + § 5.2-INT-10 通过

#### 3.6.3 MessageWriter Domain Service

- **工件**：`internal/conversation/service/message_writer.go`
- **类型**：Domain Service（[conversation/00 § 3.1](../design/architecture/tactical/conversation/00-overview.md) ConversationLifecycleService 中 add-message 那一半 + 同事务双写 hook）
- **输入**：`ConversationRepository` / `MessageRepository` / `EventSink`
- **输出**：
  - `OpenConversation(ctx, OpenCommand) (ConversationID, error)`：BeginTx → Save Conversation → emit `conversation.opened` → Commit
  - `AddMessage(ctx, AddCommand) (MessageID, error)`：BeginTx → 校验 conversation 状态 → Append Message → emit `conversation.message_added` → Commit
  - `Close(ctx, CloseCommand) error`：BeginTx → CAS status → emit `conversation.closed` → Commit
  - **同事务双写 hook**：所有 mutate 都通过 EventSink，符合 [ADR-0014 § 2](../design/decisions/0014-event-sourcing-level.md)
- **错误处理**：closed conv add-message → `ErrConversationClosed`；vendor_msg_ref 重复 → `ErrMessageDuplicate`
- **DoD**：§ 5.1-MW-1..8 + § 5.2-INT-11 通过

### 3.7 Conversation — CLI handlers

按 [03-cli § 8.4](../design/implementation/03-cli-subcommands.md)：

| 命令 | handler 文件 | 调用 |
|---|---|---|
| `conversation add-message <conversation_id> --kind=<content_kind> --content=... [--actor=...] [--direction=...] [--input-request-ref=<id>]` | `cmd/agent-center/conv_add_message.go` | MessageWriter.AddMessage |
| `conversation list [--kind=...] [--status=...]` | `cmd/agent-center/conv_list.go` | ConversationRepository.Find |
| `conversation read <id> [--tail=N] [--since=...]` | `cmd/agent-center/conv_read.go` | MessageRepository.FindRecent / FindByConversationID |

> **Phase 1 创建路径**：仅 `conversation list` / `read` / `add-message`；Conversation 创建用 admin-only `conversation open --kind=dm --title=...`（hidden subcommand，方便手动测试 + e2e；不进 § 8.4 暴露表）。生产路径上 Conversation 创建由各 BC 跨 BC 工件触发（Phase 2/3+）。

**DoD**：§ 5.1-CLI-C* + § 5.3-E2E-4 通过

### 3.8 跨 BC 集成验证

#### 3.8.1 worker enroll → events 表写入

- **工件**：`tests/e2e/worker_enroll_emits_event_test.go`
- **场景**：`agent-center worker enroll --worker-id=W-1` → 验证 events 表确有一行 `event_type=worker.enrolled` + `refs.worker_id=W-1` + `actor=user:<configured>`
- **意义**：验证 § 3.2 EventSink + § 3.4.1 WorkerEnrollService 同事务双写链路（[ADR-0014 § 2](../design/decisions/0014-event-sourcing-level.md)）
- **DoD**：§ 5.3-E2E-5 pass

#### 3.8.2 proposal accept → 多事件原子性

- **工件**：`tests/e2e/proposal_accept_emits_events_test.go`
- **场景**：先 propose（写 1 个 event）→ accept 新 project_id → 验证 events 表新增 3 行（`proposal.accepted` + `project.created` + `mapping.added`）；故意注入 mapping save 失败 → 验证全部 rollback（无半成品 event）
- **意义**：验证 § 3.4.3 ProposalAcceptanceService 跨聚合同事务双写 + 错误回滚
- **DoD**：§ 5.3-E2E-6 pass

#### 3.8.3 conversation add-message → event 写入

- **工件**：`tests/e2e/conversation_emits_event_test.go`
- **场景**：open conversation → add-message → 验证 events 表新增 2 行（`conversation.opened` + `conversation.message_added`），refs 含 conversation_id + message_id
- **DoD**：§ 5.3-E2E-7 pass

---

## § 4. Definition of Done（整体）

- [ ] § 1.1-1.5 所有工件（5 AR + 2 Entity + 13+ VO + 7 Repository + 5 Domain Service）实现并通过单元测试
- [ ] § 5 全部测试场景（unit + 集成 + e2e）通过
- [ ] 单测行覆盖率 ≥ 90%（整体 + diff；以 `go test -cover ./...` 为准；diff 用 git base = phase-1 起点 commit）
- [ ] 测试报告归档到 `docs/plans/reports/phase-1-test-report.md`，按 [README § 4 模板](README.md#-4-测试报告模板每-phase-完成后填) 填写；§ 5 计划项 1:1 对位
- [ ] § 1.7 列举的 11 类 domain event 实际在 e2e 测试中进 events 表（§ 5.3-E2E-5..7 验证）
- [ ] § 3.5 / § 3.7 CLI 命令 `--help` 输出跟 [03-cli § 8.3 / § 8.4 / § 8.8](../design/implementation/03-cli-subcommands.md) 对齐（每条命令有 `--format` / exit code / error.reason+message 双字段）
- [ ] migration `0001_init.up.sql` 在空库执行 + 全部本 phase 表存在；`down.sql` 完整反操作
- [ ] `go vet ./...` / `go build ./...` / `go test ./...` 全过；`agent-center version` / `agent-center migrate` / `agent-center server` 三条 mode 入口均可起
- [ ] § 7 风险项要么已处置要么显式 defer 到指定后续 phase（不允许"待定"）
- [ ] 凭据 / secret 处理符合 [04-configuration § 3](../design/implementation/04-configuration.md)（v1 simplification：worker bootstrap token Phase 1 mock，留 Phase 5 真实化）

---

## § 5. 测试计划

### 5.1 单测场景（按工件分类）

#### Event AR + EventSink

| # | 工件 | 场景 | 关键断言 |
|---|---|---|---|
| EV-1 | Event.NewEvent | happy path | id 是 ULID / occurred_at = clock.Now / fields 正确 |
| EV-2 | Event.Validate | event_type 空 | error |
| EV-3 | Event.Validate | actor 非合法 prefix（如 `foo:bar`） | error |
| EV-4 | Event.Validate | refs JSON 不合法 | error |
| EV-5 | Event.Validate | reason 非空但 message 空 | error（[conventions § 16](../rules/conventions.md)） |
| EV-6 | Event 字段不可变 | 反射检测无 exported setter | pass |
| ER-1 | EventRepo.Append | happy | row 写入 + 返回 nil |
| ER-2 | EventRepo.Append | id 冲突 | 错误（应用层 retry） |
| ER-3 | EventRepo.Append | 通过 tx ctx 传入 | row 在 tx 内可见，外部不可见 |
| ER-4 | EventRepo.Append | ctx 无 tx | 直接 db 写入 |
| ER-5 | EventRepo.FindByID | 存在 | 返回完整 Event |
| ER-6 | EventRepo.FindByID | 不存在 | `ErrEventNotFound` |
| ER-7 | EventRepo.Find | 按 event_type filter | 仅匹配项 |
| ER-8 | EventRepo.Find | cursor 分页 | 第二页不含第一页 |
| ES-1 | EventSink.Emit | happy | event 写入 + 返回 EventID |
| ES-2 | EventSink.Emit | EmitCommand event_type 空 | usage error |
| ES-3 | EventSink.Emit | seq 严格递增（并发） | atomic counter 无丢失 |
| ES-4 | EventSink.Emit | tx 内 emit + rollback | events 表无该行 |
| ES-5 | EventSink.Emit | clock 注入返回固定时间 | event.occurred_at 等于该时间 |

#### Worker AR + Repository

| # | 工件 | 场景 | 关键断言 |
|---|---|---|---|
| W-1 | Worker.Enroll | happy | status=offline / version=1 |
| W-2 | Worker.MarkOnline | offline → online | status=online / version+1 |
| W-3 | Worker.MarkOnline | 已 online | no-op / version 不动 |
| W-4 | Worker.MarkOffline | online → offline | status=offline |
| W-5 | Worker.Heartbeat | 更新 last_heartbeat_at + working_seconds | 字段正确 |
| W-6 | Worker invariants | worker_id 不可变 | 反射 / API 不允许改 |
| W-7 | WorkerProjectMapping.Invalidate | reason+message 必填 | error |
| W-8 | WorkerProjectMapping.Invalidate | already invalidated | `ErrMappingNotActive` |
| WR-1 | WorkerRepo.Save | 新建 | row + version=1 |
| WR-2 | WorkerRepo.Save | 同 worker_id | `ErrWorkerAlreadyExists` |
| WR-3 | WorkerRepo.UpdateStatus | CAS 成功 | version+1 |
| WR-4 | WorkerRepo.UpdateStatus | from 不匹配 | `ErrWorkerVersionConflict` |
| WR-5 | WorkerRepo.UpdateStatus | version 不匹配 | `ErrWorkerVersionConflict` |
| WR-6 | WorkerRepo.FindByStatus | status=online | 返回该状态全部 |
| WR-7 | WorkerRepo.UpdateLastHeartbeatAt | happy | row.last_heartbeat_at 更新 |
| WR-8 | MappingRepo.Save | 新建 | row + status=active |
| WR-9 | MappingRepo.FindByWorkerAndProject | active | 返回；invalidated 不返回 |
| WR-10 | MappingRepo.Invalidate | 含 reason+message | row.status=invalidated + invalidated_at 非空 |

#### Project AR + Repository

| # | 工件 | 场景 | 关键断言 |
|---|---|---|---|
| P-1 | Project.Create | happy slug | id / name / kind 正确 |
| P-2 | Project.Create | slug 非合法（大写 / 含空格） | usage error |
| P-3 | Project.Update | 改 name / kind | version+1 |
| P-4 | Project invariants | id 不可变 | API 无 setter |
| P-5 | Project.Delete | 有 active mapping | `ErrProjectHasActiveDeps` |
| PR-1 | ProjectRepo.Save | 新建 | row + version=1 |
| PR-2 | ProjectRepo.Save | id 重复 | `ErrProjectAlreadyExists` |
| PR-3 | ProjectRepo.Update | CAS 成功 | version+1 / 字段更新 |
| PR-4 | ProjectRepo.Update | version 不匹配 | `ErrProjectVersionConflict` |
| PR-5 | ProjectRepo.Delete | no active deps | row 消失 |
| PR-6 | ProjectRepo.Delete | 有 active mapping | `ErrProjectHasActiveDeps` |
| PR-7 | ProjectRepo.FindAll | --kind=coding filter | 仅返回 kind=coding |

#### Proposal AR + Repository

| # | 工件 | 场景 | 关键断言 |
|---|---|---|---|
| PR-1 | Proposal.Propose | happy | status=pending |
| PR-2 | Proposal.Accept | from=pending | status=accepted / reviewed_at / resulting_mapping_id 填值 |
| PR-3 | Proposal.Accept | from=accepted | `ErrProposalAlreadyTerminated` |
| PR-4 | Proposal.Ignore | from=pending | status=ignored |
| PR-5 | Proposal.Unignore | from=ignored | status=pending |
| PR-6 | Proposal.Unignore | from=pending | `ErrProposalInvalidTransition` |
| PR-7 | Proposal.Supersede | from=pending | status=superseded |
| PRR-1 | ProposalRepo.Save | 新建 | row + status=pending |
| PRR-2 | ProposalRepo.FindByCandidatePath | exists | 返回单条 |
| PRR-3 | ProposalRepo.FindByCandidatePath | 不存在 | nil + `ErrProposalNotFound` |
| PRR-4 | ProposalRepo.FindPending | mixed status | 仅 pending |
| PRR-5 | ProposalRepo.UpdateStatus | CAS 成功 | version+1 |
| PRR-6 | ProposalRepo.UpdateStatus | 非法 transition | error 由 AR validate 拦截 |

#### Workforce Domain Services

| # | 工件 | 场景 | 关键断言 |
|---|---|---|---|
| WES-1 | WorkerEnrollService.Enroll | happy | Worker 入库 + 1 个 event |
| WES-2 | WorkerEnrollService.Enroll | worker_id 重复 | `ErrWorkerAlreadyExists` + 无 event 写入 |
| WES-3 | WorkerEnrollService.Enroll | 注入 EventSink 失败 mock | tx rollback / 无 worker 行 |
| WES-4 | WorkerEnrollService.Enroll | 验证 emit payload | `worker.enrolled` + refs.worker_id |
| PDS-1 | EnsureProject | project 存在 | 直接返回不建 |
| PDS-2 | EnsureProject | project 不存在 | 建 Project + emit `project.created` |
| PDS-3 | EnsureProject | 在 tx 外调用 | 报错（tx 必需） |
| PAS-1 | Propose | happy | Proposal 入库 + emit `worker_project_proposal.proposed` |
| PAS-2 | Propose | 同 (worker, candidate_path) pending 已存在 | 返回既有；no double event |
| PAS-3 | Accept | new project | 4 个 events 同 tx（accepted + project.created + mapping.added + 触发 EventSink 4 次） |
| PAS-4 | Accept | existing project | 3 个 events（no project.created） |
| PAS-5 | Accept | proposal 已 accepted | `ErrProposalAlreadyTerminated` + 无 event |
| PAS-6 | Accept | mapping.Save 失败（mock 注入） | 全部 rollback / events / proposal status 不变 |
| PAS-7 | Ignore | happy | status=ignored + 1 event |
| PAS-8 | Unignore | happy | status=pending + 1 event |
| PAS-9 | Ignore / Unignore 错误路径 | non-pending / non-ignored | sentinel error |

#### Conversation AR + Message + Repository

| # | 工件 | 场景 | 关键断言 |
|---|---|---|---|
| CV-1 | Conversation.Open | happy | status=open |
| CV-2 | Conversation.Close | open → closed | closed_at 非空 |
| CV-3 | Conversation.Close | already closed | `ErrConversationClosed` |
| CV-4 | Conversation.AddMessage | closed | 拒绝 |
| CV-5 | Conversation.Kind 不可变 | API 无 setter | pass |
| CV-6 | Conversation invariants | task/issue kind 在 Phase 1 拒绝 open（保留扩展） | 该 kind 由跨 BC 路径建，CLI 不允许直接 open |
| CV-7 | Conversation.UpdatePrimaryChannel | open / closed | 都允许（Bridge 回写） |
| MS-1 | Message.New | happy | id ULID / direction 不可变 |
| MS-2 | Message.UpdateVendorMsgRef | nil → 设值 | OK |
| MS-3 | Message.UpdateVendorMsgRef | 已设值 → 再设 | `ErrMessageImmutable` |
| MS-4 | Message.Validate | sender_identity_id 空 | error |
| MS-5 | Message.Validate | content_kind 非合法 | error |
| MS-6 | Message.Validate | input_request_ref 设置（Phase 1 仍允许字段存在但不校验跨 BC，由 Phase 2 完整化） | OK |
| CR-1 | ConvRepo.Save | 新建 | row + status=open |
| CR-2 | ConvRepo.UpdateStatus | open → closed | row 更新 |
| CR-3 | ConvRepo.FindByChannelAndThreadKey | 唯一性 | 仅一条 |
| CR-4 | ConvRepo.Find | kind filter | 匹配 |
| CR-5 | ConvRepo.Find | cursor 分页 | 不重复 |
| MR-1 | MsgRepo.Append | 新建 | row + version 列不存在（append-only） |
| MR-2 | MsgRepo.Append | 同 vendor_msg_ref | `ErrMessageDuplicate` |
| MR-3 | MsgRepo.UpdateVendorMsgRef | null → set | OK |
| MR-4 | MsgRepo.UpdateVendorMsgRef | set → set | `ErrMessageImmutable` |
| MR-5 | MsgRepo.FindRecent | 最新 N 条 | 按 posted_at desc |

#### Conversation Domain Service

| # | 工件 | 场景 | 关键断言 |
|---|---|---|---|
| MW-1 | MessageWriter.OpenConversation | happy | 1 event |
| MW-2 | MessageWriter.AddMessage | happy | message 入库 + 1 event |
| MW-3 | MessageWriter.AddMessage | closed | `ErrConversationClosed` + no event |
| MW-4 | MessageWriter.AddMessage | vendor_msg_ref dup | `ErrMessageDuplicate` |
| MW-5 | MessageWriter.Close | open → closed | 1 event |
| MW-6 | MessageWriter.AddMessage | EventSink 注入失败 mock | tx rollback / message 不入库 |
| MW-7 | MessageWriter.AddMessage | 验证 emit payload | `conversation.message_added` + refs.conversation_id+message_id+direction |
| MW-8 | MessageWriter.OpenConversation | task/issue kind | 拒绝（Phase 1 不允许） |

#### CLI handlers

| # | 命令 | 场景 | 关键断言 |
|---|---|---|---|
| CLI-W1 | `worker enroll` happy | happy + `--format=json` | exit 0 + JSON 含 worker_id |
| CLI-W2 | `worker enroll` 缺 `--worker-id` | usage error | exit 2 |
| CLI-W3 | `worker enroll` 重复 id | error JSON | exit 1 + reason=`worker_already_exists` |
| CLI-W4 | `worker list` empty | exit 0 + 空 list |
| CLI-W5 | `worker status <id>` not found | exit 17 + reason=`worker_not_found` |
| CLI-P1 | `worker proposal accept` happy | exit 0 + JSON 含 mapping_id |
| CLI-P2 | `worker proposal accept` 已 accepted | exit 18 + reason=`proposal_already_terminated` |
| CLI-P3 | `worker proposal ignore` non-pending | exit 18 |
| CLI-PR1 | `project add` happy | exit 0 + JSON |
| CLI-PR2 | `project add` slug 重复 | exit 1 + reason=`project_already_exists` |
| CLI-PR3 | `project remove` 有 active mapping | exit 19 + reason=`project_has_active_deps` |
| CLI-PR4 | `project update` version 冲突（race 模拟） | exit 16 + reason=`project_version_conflict` |
| CLI-C1 | `conversation add-message` happy | exit 0 |
| CLI-C2 | `conversation add-message` to closed | exit 1 + reason=`conversation_closed` |
| CLI-C3 | `conversation list --kind=task` | exit 0 + 空（Phase 1 无 task kind） |
| CLI-C4 | `conversation read --tail=5` | 最近 5 条；倒序 |
| CFG-1 | config: 未知 YAML key | exit 2 + 含 "did you mean" |
| CFG-2 | config: 必填字段缺失 | exit 2 |
| CFG-3 | config: 枚举非法 | exit 2 + 列出合法值 |
| CFG-4 | config: env override > file | env 值生效 |

### 5.2 集成测试场景

| # | 场景 | 涉及工件 | 关键断言 |
|---|---|---|---|
| INT-1 | tx 跨 Repository 双写 | WithTx + WorkerRepo + EventRepo | tx commit 后两表都有；rollback 后两表都无 |
| INT-2 | migration up / down idempotent | Migrator | up 后表存在；再 up 不报错；down 后表清 |
| INT-3 | events 表 append-only invariant | EventRepo + 直接 SQL UPDATE | UPDATE 不报错但通过 Repository 接口无法触达；DB 层无触发器（v1）；通过约束 + 测试覆盖 |
| INT-4 | ADR-0014 同事务双写 | EventSink + WorkerRepo | UPDATE workers + INSERT events 同 tx；mock state UPDATE 失败 → events 无新行 |
| INT-5 | WorkerRepo + 真 SQLite + tx via ctx | WorkerRepo + 真 db | CAS race 注入 → `ErrWorkerVersionConflict` |
| INT-6 | MappingRepo + ProjectRepo 跨 Repository active deps | ProjectRepo.Delete | 真有 active mapping 时拒绝 |
| INT-7 | ProposalRepo dedupe by (worker_id, candidate_path) | ProposalRepo | unique active 约束生效 |
| INT-8 | WorkerEnrollService + EventSink + WorkerRepo | 真 SQLite | enroll 后 SELECT events / workers 都有行 |
| INT-9 | ProposalAcceptanceService.Accept 跨聚合 | 真 SQLite | 4 行 events + Project + Mapping 同事务可见 |
| INT-10 | MsgRepo append-only DB 层 | 真 SQLite | INSERT 后 UPDATE message.content（通过原生 SQL）虽然 DB 不阻止，但 Repository 接口不暴露该路径；用 lint / Repository API 单一通道保 invariant；test 用 raw SQL 验证写完后查到旧值 + Repository 不允许该方法 |
| INT-11 | MessageWriter.AddMessage 同事务双写 | MsgRepo + EventSink | rollback 时两表无 |

### 5.3 e2e 测试场景

| # | 场景 | 入口 CLI | 关键断言 |
|---|---|---|---|
| E2E-1 | worker enroll → list → status | `worker enroll` + `worker list` + `worker status` | enroll exit 0；list 含该 worker；status 正确 |
| E2E-2 | proposal propose → accept 新 project → 查 project | `worker proposal propose` + `worker proposal accept` + `project show` + `worker proposal list` + `worker proposal show` | accept 后 project 存在 + proposal status=accepted + mapping 存在 |
| E2E-3 | proposal ignore → unignore → accept | 同上 | 状态机走全；最终 accepted |
| E2E-4 | conversation open（admin）→ add-message → read | `conversation open --kind=dm` + `conversation add-message` + `conversation read` | read 返回该 message |
| E2E-5 | worker enroll → events 表有 `worker.enrolled` | + raw SQL query events | event 存在 + refs.worker_id 正确 |
| E2E-6 | proposal accept → events 表 3-4 行 | + raw SQL | 全部 4 个事件按预期；模拟 mapping 失败 → rollback 干净 |
| E2E-7 | conversation 流 → events 2 行 | + raw SQL | opened + message_added |
| E2E-8 | `agent-center migrate` + `agent-center server` + 关闭 | mode 入口 | server 启动 / 接到 SIGTERM 正常退出 |
| E2E-9 | `agent-center supervisor` / `agent-center worker run` stub | mode 入口 | exit 64 + reason=`not_implemented_in_phase_1` |
| E2E-10 | config fail-fast | server with malformed yaml | exit 2 + stderr 含诊断 |

---

## § 6. 风险 / Spike 项

| ID | 风险 | 缓解 / 处置 |
|---|---|---|
| R1 | **SQLite 写锁竞态** — events 表是热写表（每个状态变更都 INSERT）；与 WAL `_busy_timeout=5000` 配合是否够 | Spike：§ 5.2-INT-1 / INT-4 跑高并发 emit（100 goroutines × 100 emit），观察是否有 SQLITE_BUSY；落 e2e 测试 |
| R2 | **seq 单调性失守** — atomic counter 在多进程下不可靠（v1 单 center 单进程没问题；未来 HA 时要改） | v1 单 center 进程；ADR 已隐含；本 phase **不做** 分布式 seq；测试只覆盖单进程并发 |
| R3 | **append-only DB 层兜底缺失** — Repository 接口屏蔽 UPDATE/DELETE，但直接 SQL 仍可改 events 行 | v1 接受；通过 Repository API + lint 校验 + INT-3 测试覆盖；DB trigger 留 Phase 4 BlobStore 边落实边补 |
| R4 | **Conversation Identity 行未存在** — Phase 1 单用户简化跳过 Identity 真实校验 | Phase 1 文档明示：CLI handler validate sender_identity_id 仅做格式校验（前缀正确），Identity AR 留 Phase 5；定 `internal/conversation/domain/identity_ref.go` 临时承担 typed alias + parse；Phase 5 改 strict 时回归测试覆盖 |
| R5 | **WorkerEnrollService bootstrap/session token 简化** — v1 Phase 1 mock | 设计明示：token 字段保留接口但不强校验；Phase 5 Bridge 接入后真实化；本 phase e2e 不测 token 流程 |
| R6 | **worker scan loop 不在本 phase** — `worker_project_proposal.proposed` 由 CLI 触发（`worker proposal propose`）；非生产路径 | 接受；CLI subcommand 明示为 v1 admin 工具；scan loop 是 Phase 2 内容（worker daemon 长连接） |
| R7 | **golang-migrate down 在 SQLite 上的复杂度** — SQLite ALTER 限制；本 phase down.sql 仅 DROP TABLE 全表 | 接受；v1 down 仅用于本地开发 `0001` 全量重置；upgrade-only 是 v1 立场（[02-persistence § 6](../design/implementation/02-persistence-schema.md)） |
| R8 | **CLI router 选型** | **已处置**：Phase 1 选 stdlib `flag` + 手写 sub-command router（约 150 行，完全可测）；不引 cobra/viper。后续 phase 命令面扩张时再评估是否切换 |

---

## § 7. 下游解锁

本 phase 完成后解锁：

| 下游 phase | 依赖本 phase 哪些工件 |
|---|---|
| **Phase 2 (TaskRuntime)** | `persistence` / `idgen` / `clock` / `EventSink` + `Project` / `Worker` Repositories + `Conversation` / `MessageWriter`（task ↔ conversation 1:1 路径需用）+ migration framework + binary 多 mode 骨架 |
| **Phase 3 (Discussion)** | 同上 + issue ↔ conversation 1:1 路径需 MessageWriter + Conversation Repository |
| **Phase 4 (Observability)** | `EventRepository` + events 表 schema + EventSink 触发的真实事件流 → 给投影 / 五动词 / 查询面真实数据 |
| **Phase 5 (Bridge ACL Outbound)** | `MessageWriter` + Conversation Repository / 订阅 `conversation.message_added` |
| **Phase 6 (Cognition Supervisor)** | events 表 (`Event.Find(filter)` 拉触发事件流) + Worker / Project 元数据 + Conversation read |
| **Phase 7 (Bridge Inbound + 部署)** | Identity AR（v1 简化）真实化 / worker bootstrap token / `admin blob-migrate` |

**对外暴露的接口 surface（冻结契约）**：
- `internal/persistence` 包：`Open` / `WithTx` / `TxFromCtx` / `sqlExecutor`
- `internal/observability/service.EventSink.Emit(ctx, EmitCommand) (EventID, error)`
- `internal/observability/persistence.EventRepository`（全部方法签名）
- `internal/workforce/persistence.{Worker,Project,Mapping,Proposal}Repository`（全部方法签名）
- `internal/conversation/persistence.{Conversation,Message}Repository`（全部方法签名）
- `internal/conversation/service.MessageWriter`（全部方法签名）
- CLI 已注册命令的 flag / arg / exit code / `--format=json` schema

**Phase 完成后不允许**改本 phase 工件接口；仅允许后续 phase 扩列（[README § 2.1](README.md#21-顺序)）。

---

## § 8. References

### 设计文档
- [strategic/03-bounded-contexts](../design/architecture/strategic/03-bounded-contexts.md) — BC 总览
- [workforce/00-overview](../design/architecture/tactical/workforce/00-overview.md) / [01-worker](../design/architecture/tactical/workforce/01-worker.md) / [02-project](../design/architecture/tactical/workforce/02-project.md) / [03-worker-project-proposal](../design/architecture/tactical/workforce/03-worker-project-proposal.md)
- [conversation/00-overview](../design/architecture/tactical/conversation/00-overview.md) / [01-conversation](../design/architecture/tactical/conversation/01-conversation.md)
- [observability/00-overview](../design/architecture/tactical/observability/00-overview.md)

### 实现层
- [02-persistence-schema](../design/implementation/02-persistence-schema.md) — events 表 DDL + WithTx / golang-migrate / ULID / version CAS
- [03-cli-subcommands](../design/implementation/03-cli-subcommands.md) — § 8.3 / § 8.4 / § 8.8 CLI 命令
- [04-configuration](../design/implementation/04-configuration.md) — § 1-5 元层规则 + § 7.1 / 7.2 / 7.7 字段表

### ADR
- [ADR-0008 WorkerProjectMapping discovery proposal](../design/decisions/0008-worker-project-mapping-via-discovery-proposal.md)
- [ADR-0014 事件溯源走 L1](../design/decisions/0014-event-sourcing-level.md) — 同事务双写
- [ADR-0019 BC 合并](../design/decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md)

### 横切规约
- [conventions § 0 DDD](../rules/conventions.md) / § 9 dialect-agnostic / § 9.z BC 物理隔离 / § 14 测试 / § 16 reason+message / § 17 错误不吞
- [testing.md](../rules/testing.md) — 测试规约
- [plans/README.md](README.md) — 顺序 / 纪律 / 模板 / 测试报告模板
