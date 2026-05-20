# Observability BC — DDD 战术设计 Overview

> **DDD 战术层** · BC: Observability
>
> 跨上下文事件总线 + 实时投影 + 查询接口：所有 BC 的 domain event 汇集到 `events` 表（append-only），各类读模型从 events 投影出来。
>
> **本 BC 是 Open Host / Subscribe-only** —— 不生新事件，订阅其他 BC 的事件做投影 / 归档 / 查询。

---

## § 0. BC 一览

### 0.1 职责

| 维度 | 内容 |
|---|---|
| **聚合管理** | Event（append-only AR）+ 多个 Projection 读模型（TaskExecutionProjection / TaskStatusProjection / FleetSnapshot / 等）|
| **事件总线** | 单一 `events` 表承载所有 BC 的 domain event |
| **实时投影** | worker daemon 边解析 agent JSONL 边维护 TaskExecution 投影摘要；center 端 fleet view 投影 |
| **查询接口** | 5 个统一 CLI 动词：`inspect / query / ps / stats / logs`（user / supervisor / Web Console 共用）|
| **trace 归档** | execution / supervisor invocation 结束 → `trace.jsonl.gz` 上传 BlobStore + 回填 blob_ref；**不入 events 表**（[ADR-0015](../../../decisions/0015-agent-trace-not-in-events-table.md)）|
| **不持有的** | 各 BC 的状态权威（events 表是审计流，不是状态权威；[ADR-0014](../../../decisions/0014-event-sourcing-level.md)）|

### 0.2 UL 切片

来自 [strategic/03-bounded-contexts § 1](../../strategic/03-bounded-contexts.md) 标 Observability 上下文的术语：

- `Event`（聚合根，append-only domain event 单条）
- `AgentTraceEvent`（agent JSONL 单行；**不入 events 表**，[ADR-0015](../../../decisions/0015-agent-trace-not-in-events-table.md)）
- `TaskExecutionProjection` / `TaskStatusProjection` / `FleetSnapshot`（读模型）
- 行为动词：`Emit`（发事件）/ `Project`（投影到读模型）/ `Inspect` / `Query`（CLI 查询）

### 0.3 Context Map 位置

[strategic/03-bounded-contexts § 3](../../strategic/03-bounded-contexts.md)：

- **Observability ← ALL**：**Open Host / Subscribe-only**（所有 BC emit domain event 到本 BC 的 events 表；Observability 不主动调任何 BC）
- **TaskRuntime worker → Observability**（间接）：worker daemon 边解析 agent JSONL 边维护 TaskExecution 投影；**不推 events 表**（[ADR-0015](../../../decisions/0015-agent-trace-not-in-events-table.md)）
- **Cognition → Observability**：Supervisor 用同样的 inspect / query CLI（不为 supervisor 单造 RPC，[cognition/00-overview § 7.3](../cognition/00-overview.md)）

---

## § 1. 聚合清单 + 详情（X.1）

> **单聚合 BC**：仅 Event 一个 AR + 多个读模型。按 [ddd-blueprint § 3.1](../../../ddd-blueprint.md) 规则，聚合详情合并进本 § 1，不另起 `01-event.md`。

### 1.1 Aggregate Root: Event

**append-only domain event 单条；事件流系统兼容（未来切 Kafka / NATS 无痛）**。

#### 字段（概念）

| 字段 | 类型 | 含义 |
|---|---|---|
| `id` | ULID/UUID | event_id；时序兼容（ULID 排序 ≈ occurred_at）|
| `occurred_at` | ISO8601 TEXT | 事件发生时刻（业务时间）|
| `seq` | INTEGER | 单调递增序列号（per partition 或全局）|
| `event_type` | TEXT | `task.created` / `task_execution.failed` / 等；BC 前缀 + 实体 + 动作 |
| `refs` | TEXT (JSON) | `{task_id, execution_id, input_request_id, issue_id, worker_id, ...}` 可选键集 |
| `actor` | TEXT | `user:hayang` / `supervisor:invocation-id` / `worker:W-1` / `system`（派生事件）|
| `payload` | TEXT (JSON) | 事件 specific data；含 reason+message 双字段（[conventions § 16](../../../../rules/conventions.md)）|
| `correlation_id` | TEXT, nullable | 跨事件关联（如 supervisor invocation_id 关联其触发的所有事件）|
| `decision_id` | ULID/UUID, nullable | 决策触发事件时填（JOIN decision_records 拿 rationale）|

详细 schema 见 [implementation/02-persistence-schema.md](../../../implementation/) (TBD)。`refs` 平铺 nullable 列 vs JSON 由 implementation 层定（[conventions § 9](../../../../rules/conventions.md) dialect-agnostic）。

#### Append-only + 同事务双写

- 与对应状态表在**同事务**内双写（state UPDATE + event INSERT 同一 tx）；**不是状态权威**，状态恢复走 DB 备份（[ADR-0014](../../../decisions/0014-event-sourcing-level.md)）
- 用途：审计 / supervisor 输入 / 跨上下文 timeline / 新增投影
- **append-only**：INSERT 后永不 UPDATE / DELETE；事件无版本概念

### 1.2 Entity（子从属）

**无**。Event 是单条 immutable 行，不持子实体。

### 1.3 Value Objects（按使用聚合分组）

| VO | 用在哪 | 描述 |
|---|---|---|
| **EventType** | event_type 字段 | 命名规范：BC 前缀 + 实体 + 动作（如 `task_execution.failed`）；闭集枚举跨 BC 维护 |
| **EventRefs** | refs JSON | 关联实体 id 集合的 VO 雏形；不同 event 关联不同子集 |
| **ReasonMessage** | payload 内 reason+message 字段对 | 详见 [conventions § 16](../../../../rules/conventions.md) |
| **Actor** | actor 字段 | `user:<id>` / `supervisor:<inv-id>` / `worker:<id>` / `system` 形式化字符串 |

### 1.4 Read Models（projections）

非聚合（不持身份），是 events 流的实时 / 异步投影：

| Projection | 维护者 | 物理形态 | 用途 |
|---|---|---|---|
| **TaskExecutionProjection** | worker daemon 边解析 agent JSONL 边更新 + 推 center | DB 表行 `task_executions` 的 projection 列（`current_activity` / `current_activity_at` / `total_tool_calls` / `total_tokens` / `working_seconds_accumulated`） | Fleet View / `inspect execution` |
| **TaskStatusProjection** | 同 TaskExecution 状态机推进 + DB | task / task_execution 表本身 | `query tasks` / `inspect task` |
| **FleetSnapshot** | center 端按需聚合（`ps`）| 实时计算，不持久化 | `ps` / `agent-center ps --watch` |
| **AgentTraceEvent** | worker daemon 解析 JSONL 后写本地 `trace.jsonl` | 文件系统 + 任务结束打包 BlobStore | execution / invocation 结束归档查询 |

**关键约束**：`AgentTraceEvent` **不入 events 表**（[ADR-0015](../../../decisions/0015-agent-trace-not-in-events-table.md)）；通过本机 `events.jsonl` durable queue + worker daemon 实时投影承载。

---

## § 2. Invariants

### Event 不变量

1. **append-only**：INSERT 后永不修改 / 删除
2. **同事务双写**：state 改变 + event INSERT 必须同事务（[ADR-0014 § 2](../../../decisions/0014-event-sourcing-level.md)）
3. **reason + message 双字段**：带 `reason` 枚举的 payload 必带 `message`（[conventions § 16](../../../../rules/conventions.md)）
4. **actor 必填**：每条 event 必有 actor 字段标识触发者
5. **occurred_at 单调（per partition）**：seq 严格递增；occurred_at 可能略乱（NTP 抖动）
6. **decision_id nullable**：只有 supervisor 动作 CLI emit 的事件才有；世界事件（worker.online / task_execution.working / 等）为 null

### 投影不变量

7. **投影不持权威**：所有 projection 都从 state / events 重算的（崩了 → DB 备份恢复）；**不能反向更新 state**
8. **TaskExecutionProjection 由 worker daemon 维护**：实时投影；center 端只接受 daemon 上报，不自己解析 JSONL
9. **AgentTraceEvent 不入 events 表**：[ADR-0015](../../../decisions/0015-agent-trace-not-in-events-table.md) 决定不 emit 到中心

---

## § 3. Domain Services（X.3）

### 3.1 EventSink

**职责**：接受各 BC 同事务双写的 event INSERT 调用。

| 维度 | 内容 |
|---|---|
| 入参 | EventRow{event_type, refs, actor, payload, correlation_id?, decision_id?} |
| 出参 | event_id |
| 跨聚合 | 单条 INSERT；状态改变在 caller BC tx 内 |

### 3.2 ProjectionService

**职责**：实时 / 异步从 events 维护读模型。

| 投影 | 同步性 | 实现 |
|---|---|---|
| TaskExecutionProjection | 实时（worker daemon push） | DB UPDATE on task_executions.projection_columns |
| TaskStatusProjection | 实时（同事务） | DB UPDATE on tasks / task_executions 主行 |
| FleetSnapshot | 按需 | `ps` 命令查多张表实时聚合，不持久化 |

### 3.3 QueryService

**职责**：5 个统一 CLI 动词，统一接口。

| 动词 | 用途 |
|---|---|
| `inspect <kind> <id>` | 看单个实体完整状态 + timeline |
| `query <kind> [filters]` | 列多个实体 |
| `ps` | 当前活动资源实时快照 |
| `stats [filter]` | 聚合度量 |
| `logs <ref>` | 拉原始事件日志 |

详见 § 7.1 查询接口。

### 3.4 TraceArchiveService

**职责**：execution / supervisor invocation 结束 → 本地 `trace.jsonl` 打包 → 上传 BlobStore → 回填 blob_ref。

| 触发 | 行为 |
|---|---|
| `task_execution.completed` / `task_execution.failed` / `task_execution.killed` | worker daemon 端打包 `tasks/<task_id>/trace.jsonl.gz` → 上传 BlobStore → 回填 `task.trace_blob_path` |
| Supervisor invocation 结束 | center 端打包 invocation `trace.jsonl.gz` → 上传 BlobStore |

详见 [ADR-0015 § 4](../../../decisions/0015-agent-trace-not-in-events-table.md)。

---

## § 4. Factories（X.4）

**无独立 Factory** —— Event 由各 BC 在自己的事务内 INSERT，没有"Event Factory"模式（每个 BC 自己 emit，跟自己的 state 改 1:1）。

`ProjectionFactory`：cold start 时从 state 表 + events 重建投影（罕见，仅 v1 没设计 cold-rebuild 路径；待 implementation 决定）。

---

## § 5. Repositories（X.5）

接口签名（Go-style，含 `ctx context.Context` 参数；架构层契约，跟实现解耦）：

### 5.1 EventRepository（append-only 严格）

```go
type EventRepository interface {
    Append(ctx context.Context, e *Event) error                                              // INSERT only；从不 UPDATE/DELETE
    FindByID(ctx context.Context, id EventID) (*Event, error)
    FindByType(ctx context.Context, eventType string, since time.Time, limit int) ([]*Event, error)
    FindByRefs(ctx context.Context, refs EventRefsFilter, since time.Time, limit int) ([]*Event, error)
    FindSince(ctx context.Context, since time.Time, limit int) ([]*Event, error)
    FindByCorrelationID(ctx context.Context, correlationID string) ([]*Event, error)
    FindByDecisionID(ctx context.Context, decisionID DecisionID) ([]*Event, error)
}

// Domain errors
var (
    ErrEventNotFound  = errors.New("observability: event not found")
    ErrEventImmutable = errors.New("observability: events table is append-only, cannot modify/delete")
)
```

### 5.2 TaskExecutionProjectionRepository

```go
type TaskExecutionProjectionRepository interface {
    FindByID(ctx context.Context, id TaskExecutionID) (*TaskExecutionProjection, error)
    FindActive(ctx context.Context) ([]*TaskExecutionProjection, error)              // status ∈ {submitted/working/input_required}
    UpdateProjection(ctx context.Context, id TaskExecutionID, p ProjectionUpdate) error  // worker daemon push
}

// Domain errors
var (
    ErrProjectionNotFound      = errors.New("observability: projection not found")
    ErrProjectionStale         = errors.New("observability: projection update arrived out of order (stale)")
)
```

### 5.3 TraceArchiveRepository（BlobStore，不在 DB）

```go
type TraceArchiveRepository interface {
    Upload(ctx context.Context, blobRef string, content io.Reader) error           // gzip 压缩后的 trace.jsonl.gz
    Download(ctx context.Context, blobRef string) (io.ReadCloser, error)
    Exists(ctx context.Context, blobRef string) (bool, error)
}

// Domain errors
var (
    ErrBlobNotFound = errors.New("observability: blob not found in store")
    ErrBlobUpload   = errors.New("observability: blob upload failed")
)
```

### 5.4 约定

- `events` 表 INSERT only；不允许 UPDATE / DELETE（应用层强制 + 实现层可加 DB 触发器兜底）
- Projection 列允许 UPDATE（但只能由相应的 daemon / domain service 改；外部 BC 不直接改 projection）
- BlobStore 通过 [conventions § 8](../../../../rules/conventions.md) 抽象访问；实现层落 LocalDir / S3 选型
- Repository 是**领域层抽象接口**；实现层落到 [implementation/02-persistence-schema.md](../../../implementation/) (TBD) + [implementation/01-blob-store.md](../../../implementation/01-blob-store.md)
- Domain errors 用 sentinel error pattern；调用方用 `errors.Is` 判定

---

## § 6. 跨聚合引用出方向

| 引用方 → 被引方 | 强弱 | 一致性窗口 | 触发场景 |
|---|---|---|---|
| **Event → DecisionRecord**（`events.decision_id`） | 弱 / nullable / 反向血缘 | tx 同步（emit 时填）| supervisor 动作 CLI 内部 emit |
| **Event → 各 BC 实体**（`events.refs.{task_id / issue_id / ...}`） | 弱 / 反向血缘 | tx 同步 | 各 BC emit |
| **TaskExecutionProjection → TaskExecution**（同表 projection 列） | 强 / 同行 | 实时同步 | worker daemon push |

---

## § 7. 跨 BC 交互

### 7.1 查询接口（CLI / Supervisor / Web Console 共用）

**inspect 支持的 kind**：

| kind | 输出 |
|---|---|
| `task <id>` | 元数据 + 当前 status + dep 状态 + 所有 executions 列表 + 时间线 |
| `execution <id>` | execution 状态 / workspace / artifacts / agent trace 摘要 / 全 events |
| `worker <id>` | enroll 信息 / 当前 capacity / 正在跑的 executions / 心跳历史 |
| `issue <id>` | issue 状态 / Conversation 消息时间线 / spawned tasks / 关联 conversations |
| `supervisor <invocation_id>` | 该次 invocation 的触发事件 / prompt / 决策记录 / 落地动作 |
| `conversation <id>` | message 时间线 |
| `input_request <id>` | 请示来龙去脉 |
| `project <id>` | project 元数据 / 所有相关 task / mapping 状态 |
| `worktree <execution_id>` | 给定 execution 的 workspace 详情 |
| `decision <id>` | 单条决策详情 + rationale + 触发事件链 |

**query 支持的 kind**：

| kind | 常用 filters |
|---|---|
| `tasks` | `--status` / `--project` / `--priority` / `--blocked-by` / `--not-dispatchable` / `--since` |
| `executions` | `--status` / `--task-id` / `--worker` / `--since` / `--failed-reason` |
| `workers` | `--status` / `--has-mapping` |
| `issues` | `--status` / `--project` / `--opener` |
| `input_requests` | `--status` / `--task-id` |
| `proposals` | `--status` |
| `events` | `--type` / `--task-id` / `--execution-id` / `--since` / `--actor` |
| `decisions` | `--invocation-id` / `--kind` / `--outcome` / `--since` |

完整 CLI 签名归 [implementation/03-cli-subcommands.md](../../../implementation/) (TBD)。

### 7.2 Fleet View — 跨任务实时全景

CLI `agent-center ps`（仿 `docker ps` / `kubectl get pods`）：

```
$ agent-center ps

EXECUTION   TASK   PROJECT        WORKER        STATUS          AGENT         MODE       ACTIVITY                  DURATION
E-7         T-42   agent-center   mac-mini-1    working         claude-code   worktree   edit src/worker/main.go   0:03:21
E-8         T-43   agent-center   mac-mini-1    working         codex         worktree   bash: go test ./...       0:00:08
E-9         T-44   personal-site  home-server   input_required  claude-code   direct     request_human_input       0:00:45 ⏸

WORKER        STATUS    CAPACITY  PROJECTS_MAPPED
mac-mini-1    online    2/2       agent-center, foo
home-server   online    1/2       personal-site
nas-jp        offline   -         -

OPEN_INPUT_REQ   TASK   EXECUTION  AGE   QUESTION
IR-3             T-44   E-9        8m    "用 REST 还是 GraphQL?"

PENDING_ISSUES   PROJECT       OPENER          AGE
I-12             agent-center  user:hayang     2h
```

变体：`ps --watch`、`ps --by-worker | --by-project | --by-agent-type`。

数据基础：worker daemon 维护的 TaskExecutionProjection 实时投影 + center 端聚合查询。

### 7.3 Bridge 渲染（subscribe-only 不渲染）

Observability BC 自己不渲染。Bridge 订阅 BC 业务事件（[bridge/01-feishu-integration § 5](../bridge/01-feishu-integration.md)），不订阅 Observability 自己的事件（Observability 不 emit 业务事件）。

### 7.4 飞书侧

- 用户问"现在在跑啥" → supervisor 调 `query executions --status=working,input_required` → 组装卡片回复
- 周期 review 卡片附此刻活跃 execution 小卡
- 任务完成卡片附 [查看 trace] [查看 supervisor 思考] 按钮，点击拉详细页（v1 回 Markdown 摘要文本）

### 7.5 事件总览（按 BC 分组）

| BC | 主要事件类型 |
|---|---|
| TaskRuntime (Task) | `task.created` / `task.priority_changed` / `task.eta_changed` / `task.workspace_mode_changed` / `task.dependency_added` / `task.dependency_removed` / `task.suspended` / `task.resumed` / `task.done` / `task.abandoned` / `task.dispatch_limit_reached` |
| TaskRuntime (TaskExecution) | `task_execution.created` / `task_execution.dispatched` / `task_execution.working` / `task_execution.input_required` / `task_execution.completed` / `task_execution.failed` / `task_execution.kill_requested` / `task_execution.killed` |
| TaskRuntime (InputRequest) | `input_request.requested` / `input_request.responded` / `input_request.timed_out` / `input_request.canceled` |
| TaskRuntime (worker 侧) | `worktree.created` / `worktree.released` / `artifact.uploaded` / `task_log.archived` / `task_trace.archived`（agent_trace 不再作为事件流入 events 表，[ADR-0015](../../../decisions/0015-agent-trace-not-in-events-table.md)） |
| Discussion | `issue.opened` / `issue.discussion_started` / `issue.concluded` / `issue.withdrawn` / `issue.tasks_spawned`（议事消息走 `conversation.message_added` (kind=issue)，不再 emit `issue.commented`；[ADR-0021](../../../decisions/0021-issue-as-conversation.md)） |
| Workforce | `worker.enrolled` / `worker.online` / `worker.offline` / `worker.heartbeat` / `worker_project_proposal.*` / `worker_project_mapping.*` / `project.*` |
| Cognition | `supervisor.invocation_started` / `supervisor.invocation_ended` / `supervisor.decision_made` / `supervisor.invocation_failed_alert`（Memory 变更不 emit `memory.*` 事件，由 supervisor invocation 的 `trace.jsonl.gz`（含 `Edit`/`Write` tool 调用）+ `git log` 双渠道审计，[ADR-0012](../../../decisions/0012-memory-file-based.md) / [ADR-0015](../../../decisions/0015-agent-trace-not-in-events-table.md)） |
| Conversation | `conversation.opened` / `conversation.message_added` / `conversation.closed` / `identity.registered` / `channel_binding.added` |
| Bridge | `channel.delivered` / `channel.delivery_failed` / `bridge.parse_failed` |

所有失败 / 取消 / 超时事件必须带 `reason + message` 双字段（[conventions § 16](../../../../rules/conventions.md)）。

---

## § 8. Out-of-Scope / Future Work

| 项 | 归属 |
|---|---|
| 事件流系统切 Kafka / NATS | [roadmap](../../../roadmap.md)（v1 SQL 表足够；schema 兼容设计已就位）|
| 跨 task 模式分析 / 异常检测 | [roadmap](../../../roadmap.md)（agent-center 本身不做；项目自有的 metric 工具链[conventions § 2.x](../../../../rules/conventions.md)）|
| Per-project metric / 自定义 dashboard | [requirements/03-out-of-scope.md](../../../requirements/03-out-of-scope.md)（observability 层 opinionated；项目特有度量留项目）|
| Token cost 折算金钱 + 告警 | [roadmap](../../../roadmap.md) |
| AgentTraceEvent 跨 worker 实时聚合 | [out-of-scope](../../../requirements/03-out-of-scope.md)（trace 不进 events 表，跨 worker 聚合走 BlobStore + peek-trace）|
| 实时 stream timeline（CLI tail -f）| [roadmap](../../../roadmap.md) |
| Web Console 时间线可视 | [roadmap](../../../roadmap.md)（v1 仅 fleet view + 单实体 trace 概览）|

---

## § 9. References

### 相关 ADR

- [ADR-0014 事件溯源走 L1：状态表为权威，事件表是审计流](../../../decisions/0014-event-sourcing-level.md)
- [ADR-0015 agent_trace 不进 events 表](../../../decisions/0015-agent-trace-not-in-events-table.md)
- [ADR-0006 大文件走 BlobStore](../../../decisions/0006-blob-store-for-large-content.md)（trace.jsonl.gz / log archive）
- [ADR-0018 Detached agent + per-execution shim](../../../decisions/0018-detached-agent-via-per-execution-shim.md)（events.jsonl durable queue）

### 战略层

- [strategic/03-bounded-contexts § 1 UL](../../strategic/03-bounded-contexts.md)（Event / AgentTraceEvent 术语）
- [strategic/03-bounded-contexts § 2 BC5 Observability](../../strategic/03-bounded-contexts.md)
- [strategic/03-bounded-contexts § 3 Context Map](../../strategic/03-bounded-contexts.md)

### 跨 BC 协作文档

- [task-runtime/00-overview.md](../task-runtime/00-overview.md) — Task / Execution / InputRequest 事件 emit
- [discussion/00-overview.md](../discussion/00-overview.md) — Issue 事件 emit
- [workforce/00-overview.md](../workforce/00-overview.md) — Worker / Proposal / Mapping / Project 事件 emit
- [cognition/00-overview.md](../cognition/00-overview.md) — Supervisor 事件 emit
- [cognition/01-supervisor-invocation.md § 4](../cognition/01-supervisor-invocation.md) — DecisionRecord + events.decision_id 关联
- [conversation/00-overview.md](../conversation/00-overview.md) — Conversation 事件 emit
- [bridge/01-feishu-integration.md](../bridge/01-feishu-integration.md) — Bridge 事件 emit + 查询接口在飞书侧的暴露

### 实现层

- [implementation/01-blob-store.md](../../../implementation/01-blob-store.md) — BlobStore 抽象
- [implementation/02-persistence-schema.md](../../../implementation/) (TBD) — events 表 / projection 表 schema
- [implementation/03-cli-subcommands.md](../../../implementation/) (TBD) — CLI 完整签名

### 横切方法论

- [conventions § 0 DDD](../../../../rules/conventions.md) / § 2 可观测性优先 / § 8 BlobStore / § 9 dialect-agnostic / § 16 reason+message
