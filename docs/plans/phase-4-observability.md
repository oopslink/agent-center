# Phase 4: Observability 投影 + 查询面

> DDD BC: Observability（Open Host）完整化 · 依赖 Phase 1-3 · 解锁 Phase 5 (Bridge ACL Outbound)
> 纪律：按里程碑顺序 / 模块完备不半成品 / 单测 ≥ 90% + 集成 + e2e + 测试报告

## § 0. 目标

Phase 1 已落地 `events` 表 + `EventSink`（同事务双写通道）；Phase 2 / 3 各 BC 已在状态机推进时 emit domain event。本 phase 把 Observability BC 从"事件总线"补成 **Open Host 完整查询面**：

1. **运维视角**：`agent-center ps` / `inspect` / `query` / `stats` / `logs` 5 个统一 CLI 动词成为唯一查询入口（user / supervisor / 将来 Web Console 共用，[observability/00-overview § 7.1](../design/architecture/tactical/observability/00-overview.md)）
2. **supervisor 视角**：本 phase 完成后，supervisor 拉取 `trigger_event_ids` + `query events --since=...` 已经能完整工作（Phase 6 的 supervisor 不再需要为查询自造 RPC）
3. **用户视角**：`peek-trace <execution>` 实时窥视当前 agent 行为（worker daemon RPC 透传）；execution 终态后 `logs <execution>` 走 BlobStore 归档
4. **DDD 意义**：Observability BC 从被动事件接收方升级为 **Open Host Service**（[strategic/03-bounded-contexts § 3](../design/architecture/strategic/03-bounded-contexts.md)）。所有其它 BC 不再各自实现查询接口，统一走本 BC；为 Phase 5 Bridge / Phase 6 Cognition 提供"查 = 调 Observability"的契约面

**关键约束**：

- **events 不承担状态重建**（[ADR-0014](../design/decisions/0014-event-sourcing-level.md)）— 投影服务**不能反向写**主状态表
- **trace 不进 events 表**（[ADR-0015](../design/decisions/0015-agent-trace-not-in-events-table.md)）— agent JSONL 走 TaskExecutionProjection（实时） + BlobStore（归档） + peek-trace RPC（live 深挖）
- **§ 9.z BC 物理隔离**（[conventions § 9.z](../rules/conventions.md)）— `task_execution_projections` 是 Observability **独占表**，TaskRuntime 不允许直接写。worker daemon 的 push 必须经 `TaskExecutionProjectionService`，绝不混进 TaskExecution 聚合的 UPDATE 通道

---

## § 1. DDD 工件清单

### 1.1 Aggregate Roots

无新增 AR。Event AR 已在 Phase 1 落地（append-only，不再扩列）。

### 1.2 Entities（子从属）

无。

### 1.3 Value Objects

| VO | 用途 | 备注 |
|---|---|---|
| `EventQueryFilter` | `query events` / supervisor 输入流过滤入参 | Phase 1 已有骨架；本 phase 补齐 `Refs` / `EventType` 前缀 / `Cursor` 字段 |
| `EventRefsFilter` | refs 多键合取过滤 | Phase 1 已有；本 phase 加 `task_id / execution_id / issue_id / worker_id / project_id` 子集合取 |
| `ProjectionUpdate` | worker daemon push 入参 | 新增；字段：`current_activity / current_activity_at / total_tool_calls / total_tokens_input / total_tokens_output / working_seconds_accumulated / last_push_at` |
| `FleetSnapshot` | `ps` 输出聚合 VO | 新增；含 executions / workers / open_input_requests / pending_issues 4 段 |
| `InspectResult<Kind>` | 10 种 kind 各自的组装结果 | 新增；按 kind 多态：`task / execution / worker / issue / supervisor / conversation / input_request / project / worktree / decision` |
| `StatsScope` | `stats` 入参 | 新增；枚举：`tasks / executions / workers / events / issues / proposals` |
| `TraceBlobRef` | trace.jsonl.gz 在 BlobStore 中的 path | 新增；落 `tasks/<task_id>/trace.jsonl.gz` |

### 1.4 Repositories

| Repository | 状态 | 说明 |
|---|---|---|
| `EventRepository` | Phase 1 已落 `Append` + `FindByID`；**本 phase 补 `Find(ctx, EventQueryFilter)`** | cursor 分页（ULID id ASC）；refs 合取过滤；前缀匹配 `event_type` |
| `TaskExecutionProjectionRepository` | **新增** | `FindByID / FindActive / UpdateProjection`（UPSERT）；物理表 `task_execution_projections`（[02-persistence-schema § 8.2](../design/implementation/02-persistence-schema.md)） |
| `TraceArchiveRepository` | **新增**（BlobStore 抽象，不在 DB） | `Upload / Download / Exists`；落 [01-blob-store](../design/implementation/01-blob-store.md) 抽象 |

> **§ 9.z 强调**：`task_execution_projections` 是 Observability 独占。TaskRuntime 的 TaskExecution 聚合**不持** projection 列；其聚合根 UPDATE 不触本表。worker daemon push → center → `TaskExecutionProjectionRepository.UpdateProjection` → UPSERT，**完全不经** TaskExecution 状态机通道。
>
> 历史包袱清理（参见 [observability/00-overview § 5.2 注](../design/architecture/tactical/observability/00-overview.md)）：原设计曾把 projection 列拼到 task_executions 表跨 BC 共写，已按 conventions § 9.z 拆分；本 phase 实装时**必须**按拆分后的 1:1 PK + 物理隔离落地，不允许"为省一次 round-trip 把 projection 列嵌回主表"。

### 1.5 Domain Services

| Service | 职责 | 关键边界 |
|---|---|---|
| `TaskExecutionProjectionService` | 接受 worker daemon push，UPSERT `task_execution_projections` | 不走 version CAS（高频派生数据，多写一次旧值不损 invariant，[02-persistence-schema § 8.2.2](../design/implementation/02-persistence-schema.md)）。staleness 防护：input `last_push_at < 已存行 last_push_at` 时丢弃 + emit warning event |
| `TaskStatusProjectionService` | 状态推进时同事务更新 task / task_execution 主行的派生汇总（如 `task.last_execution_status`） | 走各 BC tx；Observability 提供 helper 但不持有这些表 |
| `FleetSnapshotService` | `ps` 实时聚合：扫 `task_executions` + `task_execution_projections` + `workers` + `input_requests` + `issues` | 不持久化；按需 multi-roundtrip 查 + 在应用层 join；不用 SQL JOIN（[conventions § 9.z](../rules/conventions.md) + [03-cli § 9.2](../design/implementation/03-cli-subcommands.md)） |
| `TraceArchiveService` | execution 终态 hook：打包本地 `events.jsonl` + `agent.log` 为 `trace.jsonl.gz` → 上传 BlobStore → 回填 `task.trace_blob_path` | worker daemon 端 hook 由 Phase 2 shim 触发本 service；center 端只回填路径 |
| `QueryService` | 5 个统一 CLI 动词的 application-service 入口 | 内部 dispatch 到 10 种 inspect kind / 8 种 query resource / fleet / stats / logs |
| `StatsService` | 聚合度量：吞吐 / 失败率 / 平均时长 / token 消耗 | SQL `COUNT / AVG / SUM` 直接打 state 表 + events 表；不引入 metric pipeline（v1 单 binary） |
| `UnknownEventEscalator` | 周期 query：`agent_adapter.unknown_event_seen` 24h ≥ 阈值 → emit `supervisor.invocation_failed_alert` 风格的 escalation event，供 Phase 6 supervisor 决策 | 周期由 `agent-center server` 启动时注册定时任务（默认 1h scan）；阈值默认 10（可 config，[05-agent-adapters § 3.1 步骤 5](../design/implementation/05-agent-adapters.md)） |

### 1.6 Application Services（CLI handler 层）

| Handler | 命令 | 调用栈 |
|---|---|---|
| `InspectHandler` | `agent-center inspect <kind> <id> [--format=...]` | `QueryService.Inspect(kind, id) → kind-specific repo fan-out` |
| `QueryHandler` | `agent-center query <resource> [--filter] [--since] [--limit] [--cursor]` | `QueryService.Query(resource, filter)` |
| `PsHandler` | `agent-center ps [--watch] [--project=...]` | `FleetSnapshotService.Snapshot(project?)` |
| `StatsHandler` | `agent-center stats [--scope=...] [--since=...]` | `StatsService.Aggregate(scope, since)` |
| `LogsHandler` | `agent-center logs <kind> <id> [--follow] [--since=...]` | `kind=task → task.log_blob_path → BlobStore.Get`；`kind=execution` 同理 |
| `PeekTraceHandler` | `agent-center peek-trace <execution_id> [--last=N] [--kind=...] [--follow]` | center → worker gRPC（复用 Phase 1 enroll 通道）→ daemon 读本地 `trace.jsonl` 流回 |

### 1.7 Domain Events（本 phase emit 给 events 表）

Observability BC 本身**几乎不 emit 业务事件**（[observability/00-overview § 7.3](../design/architecture/tactical/observability/00-overview.md)）。本 phase 仅新增：

| 事件 | 触发 | 字段（reason + message 必带） |
|---|---|---|
| `observability.projection_stale_drop` | UPSERT 时 `last_push_at` 早于已存行 | `{execution_id, dropped_at, existing_last_push_at, reason: "out_of_order_push", message}` |
| `observability.trace_archive_uploaded` | `TraceArchiveService` 上传成功 | `{execution_id, task_id, blob_ref, bytes, reason: "execution_terminal", message}` |
| `observability.trace_archive_failed` | 上传失败 | `{execution_id, task_id, attempt, reason ∈ {blob_store_unavailable / payload_too_large / checksum_mismatch}, message}` |
| `observability.unknown_event_escalated` | `UnknownEventEscalator` 触发 | `{adapter_name, cli_type_field, count_in_window, window_hours: 24, reason: "threshold_reached", message}` |

> Trace 本身**不 emit 事件**（[ADR-0015](../design/decisions/0015-agent-trace-not-in-events-table.md)）；上述事件是 **archive 这一动作**的元事件，不是 trace 内容。

### 1.8 Context Map 关系

| 方向 | 关系类型 | 说明 |
|---|---|---|
| Observability ← TaskRuntime (Phase 2) | Open Host / Subscribe-only | 订阅 `task_execution.*` 投影到 `task_execution_projections` |
| Observability ← Discussion (Phase 3) | Open Host / Subscribe-only | 订阅 `issue.*`；本 phase 在 inspect/query 路径支持 `kind=issue` |
| Observability ← Workforce (Phase 1) | Open Host / Subscribe-only | 订阅 `worker.*` / `worker_project_mapping.*`；ps / inspect worker 支持 |
| Observability ← Conversation (Phase 1) | Open Host / Subscribe-only | inspect conversation 支持 |
| **Observability → TaskRuntime worker daemon** | Customer (反向 RPC) | peek-trace 时 center 主动调 worker daemon（**单向反向调用**；非订阅）|
| Observability ← BlobStore | Conformist | `TraceArchiveRepository` 走 [§ 8 抽象](../rules/conventions.md) |

---

## § 2. 上游依赖

| 上游 phase | 上游工件 | 本 phase 哪一步使用 |
|---|---|---|
| Phase 1 | `events` 表 schema + `EventSink` + `EventRepository.Append/FindByID` | § 3.5 QueryService 在此基础上加 `Find(filter)`；§ 3.8 UnknownEventEscalator 通过 `Find` 查 24h 窗口 |
| Phase 1 | `Event` AR + `EventRefs` VO 骨架 | § 3.5 cursor 分页基于 ULID id；refs 合取过滤实现 |
| Phase 1 | Workforce BC 的 worker / mapping 表 | § 3.3 FleetSnapshot 扫 workers；inspect worker handler |
| Phase 1 | Conversation BC 的 conversation 表 | inspect conversation handler；inspect task 时 JOIN conversation_id |
| Phase 2 | `task_executions` 表 + 状态机（submitted / working / input_required / completed / failed / killed） | § 3.1 projection PK 1:1 引用；§ 3.4 终态 hook 触发 TraceArchive |
| Phase 2 | TaskExecution Aggregate 的状态查询 Repository | § 3.3 FleetSnapshot 实时聚合；§ 3.5 inspect execution 主体字段 |
| Phase 2 | worker daemon 端 events.jsonl 本地 durable queue + agent.log（[ADR-0018](../design/decisions/0018-detached-agent-via-per-execution-shim.md)） | § 3.4 TraceArchive 打包源；§ 3.7 peek-trace 读取源 |
| Phase 2 | `report-progress` CLI（worker → center push 通道） | § 3.1 TaskExecutionProjectionService 是其中心端 handler |
| Phase 2 | center ↔ worker daemon gRPC 长连接（enroll / heartbeat） | § 3.7 peek-trace 复用该通道 |
| Phase 3 | Issue 聚合 + Discussion BC 表 | inspect issue / query issues handler |
| Phase 3 | InputRequest 聚合 | inspect input_request / fleet view "open input requests" 段 |

**冻结契约**：Phase 1-3 已交付的接口本 phase **不允许改语义**。新查询能力只能扩列、不能反向要求上游改动。

---

## § 3. 工作项分解（严格按依赖顺序）

### 3.1 TaskExecutionProjectionService（UPSERT，BC 独占表）

- **工件类型**：Repository 实现 + Domain Service
- **依赖**：Phase 2 `task_executions` 表存在（FK 约束）；Phase 2 `report-progress` CLI 已定义 push 协议
- **产出**：
  - `internal/observability/projection/task_execution_projection.go` — VO `ProjectionUpdate` + `TaskExecutionProjectionService`
  - `internal/observability/persistence/task_execution_projection_repo.go` — Repository 实现（UPSERT）
  - migration：`internal/persistence/migrations/0004_task_execution_projections.up.sql` / `.down.sql`
- **实现步骤**：
  1. 落 DDL（[02-persistence-schema § 8.2.1](../design/implementation/02-persistence-schema.md)）。`task_execution_id` PK + FK → `task_executions(id)`；无 version 列
  2. `UpdateProjection(ctx, id, ProjectionUpdate)` 实现 UPSERT（`INSERT ... ON CONFLICT DO UPDATE`）
  3. **staleness 防护**：UPSERT 前 SELECT 已有行的 `last_push_at`；若 push 早于已存，跳过 UPDATE + emit `observability.projection_stale_drop` event（同 tx）
  4. center 端 gRPC handler `ReportProgress(req)` → 调本 service
  5. `FindByID` / `FindActive`（status filter 走 join 到 `task_executions.status`，但用 multi-roundtrip：先查 active executions ids，再 batch SELECT projections）
- **§ 9.z 强制**：本 service 是写 `task_execution_projections` 的**唯一**入口；TaskExecution Aggregate / TaskRuntime 任何代码路径**禁止** 直接 SQL 写本表。lint 规则：包路径 `internal/task_runtime/` 不允许 import `internal/observability/persistence/task_execution_projection_repo`（v1 走 code review；后续可加 `gomodguard` config）
- **对位**：[02-persistence-schema § 8.2.1 / § 8.2.2](../design/implementation/02-persistence-schema.md) + [observability/00-overview § 5.2](../design/architecture/tactical/observability/00-overview.md)
- **DoD**：
  - [ ] UPSERT SQL 与 § 8.2.2 模板字字对应
  - [ ] staleness 路径有单测（push out-of-order → 丢弃 + emit event）
  - [ ] 100 个 push 高频回归测试：projection 行始终是最新 `last_push_at`，no row leak
  - [ ] FK 约束验证：删 task_execution 时 projection 行残留（v1 不级联 GC，[02-persistence-schema § 8.2.3](../design/implementation/02-persistence-schema.md)）有断言

### 3.2 TaskStatusProjection（同事务双写 helper）

- **工件类型**：Domain Service（helper）
- **依赖**：3.1 完成（同 tx 写两边）；Phase 1 `EventSink` 在 tx ctx 内可用
- **产出**：
  - `internal/observability/projection/task_status_projection.go`
- **实现步骤**：
  1. 提供 `TaskStatusProjectionService.OnExecutionTerminal(ctx, executionID, terminalStatus)` helper，**在 caller BC 的 tx 内**调用
  2. 内部行为：UPDATE 派生汇总列（如 `tasks.last_execution_status` / `tasks.last_execution_at`，**若存在**；不存在则 noop）；同 tx INSERT `task.status_summary_updated` event（如果定义了）
  3. 本 service **不写 projection 表** —— 是 TaskRuntime 的同事务双写助手（写 TaskRuntime 自家的 task / task_execution 表）
- **澄清**：这是 [observability/00-overview § 1.4](../design/architecture/tactical/observability/00-overview.md) "TaskStatusProjection 物理形态 = task / task_execution 主表本身" 的 helper 化表达，**不**新建独立表
- **DoD**：
  - [ ] 双写在 caller tx 失败时同步回滚（集成测试用 SQLite tx rollback 验证）
  - [ ] caller 在 tx 外调用本 helper 抛 explicit error（不允许 fire-and-forget）

### 3.3 FleetSnapshotService（实时聚合）

- **工件类型**：Domain Service + 应用组装
- **依赖**：3.1 完成；Phase 1 Workforce 表 / Phase 2 task_executions / Phase 3 issues + input_requests
- **产出**：
  - `internal/observability/query/fleet_snapshot.go` — VO `FleetSnapshot` + service
- **实现步骤**：
  1. 4 段并行查询（goroutine + errgroup）：
     - `executions` — `SELECT FROM task_executions WHERE status IN (working, input_required, submitted)` + 对每条 `LEFT batch SELECT FROM task_execution_projections WHERE task_execution_id IN (...)`
     - `workers` — `SELECT FROM worker_enrollments WHERE status IN (online, offline)` + `mappings` 子查询
     - `open_input_requests` — `SELECT FROM input_requests WHERE status = open ORDER BY opened_at`
     - `pending_issues` — `SELECT FROM issues WHERE status = open ORDER BY opened_at`
  2. 应用层组装 `FleetSnapshot` VO（**不写 SQL JOIN**，[conventions § 9.z](../rules/conventions.md)）
  3. `--project=<slug>` filter：4 段各自加 WHERE
  4. `--watch` 由 CLI handler 层 sleep + 重调（v1 简化，不做长连接 push）
- **对位**：[observability/00-overview § 7.2](../design/architecture/tactical/observability/00-overview.md) 表格
- **DoD**：
  - [ ] 4 段查询并行（errgroup）任一失败回 partial 结果 + warning to stderr（[conventions § 17](../rules/conventions.md)：错误不吞）
  - [ ] `--project` 过滤所有 4 段都生效
  - [ ] 集成测试：100 executions + 10 workers 场景 < 200ms（SQLite `:memory:`）

### 3.4 TraceArchiveService（execution 终态 hook）

- **工件类型**：Domain Service + BlobStore Repository
- **依赖**：Phase 2 shim 端 events.jsonl / agent.log；Phase 1 BlobStore 抽象（[01-blob-store](../design/implementation/01-blob-store.md)）
- **产出**：
  - `internal/observability/trace/archive_service.go` — `TraceArchiveService`
  - `internal/observability/persistence/trace_archive_repo.go` — `TraceArchiveRepository`
- **实现步骤**：
  1. worker daemon 端：execution shim 终态退出时（`task_execution.completed/failed/killed`）触发 `archiveExecution(executionID)`
  2. 打包：`tar + gzip { events.jsonl, agent.log }` → 流到 `bytes.Buffer`
  3. `TraceArchiveRepository.Upload(ctx, blobRef, reader)`，blobRef = `tasks/<task_id>/trace.jsonl.gz`（[ADR-0015 § 3](../design/decisions/0015-agent-trace-not-in-events-table.md)）
  4. 成功 → center RPC `ReportTraceArchived(task_id, blob_ref, bytes)` → center 回填 `tasks.trace_blob_path` + emit `observability.trace_archive_uploaded`
  5. 失败 → emit `observability.trace_archive_failed { reason }`；本地 jsonl **不删**（下一次 retry / 用户手动 `logs <execution>`）
  6. 重试策略：worker daemon 启动时扫本地 unfinished archives → retry
- **§ 17 错误显式化**：上传失败必 emit；不允许静默丢弃。`reason` 闭集：`blob_store_unavailable / payload_too_large / checksum_mismatch`
- **DoD**：
  - [ ] 终态触发链：execution.failed event → daemon hook → upload → center 回填 `trace_blob_path` 全程集成测试
  - [ ] 上传失败重试：人为打断 BlobStore，重启 daemon，自动重试成功
  - [ ] 超过 100MB payload 走 `payload_too_large` 早 fail（v1 上限可 config）

### 3.5 QueryService — 5 动词统一接口

- **工件类型**：Domain Service（dispatch）
- **依赖**：3.1 / 3.3 / 3.4 完成；Phase 1 `EventRepository`；Phase 1-3 各 BC Repository
- **产出**：
  - `internal/observability/query/query_service.go`
  - `internal/observability/query/inspect_*.go`（10 个 kind 各一文件）
  - `internal/observability/query/query_resource_*.go`（8 个 resource 各一文件）
- **实现步骤**：
  1. `EventRepository.Find(ctx, EventQueryFilter)` 实装（Phase 1 留的 stub）：
     - cursor 分页：ULID `id` ASC，`WHERE id > cursor LIMIT N`
     - `event_type` 支持前缀匹配（`type LIKE 'task.%'`，[02-persistence-schema § 8.2.2](../design/implementation/02-persistence-schema.md)）
     - `refs` 多键合取：v1 拉到内存过滤（events 表大量行时退化；postpone 索引列设计）
     - `since` / `until` 走 `occurred_at` 范围
     - 默认 `limit=100`，上限 `1000`
  2. `Inspect(kind, id)` dispatch 到 10 个 kind 各自的组装器；每个组装器**只调本 BC 接口 / cross-BC repo 查询**，不直接打表
  3. `Query(resource, filter)` dispatch 到 8 个 resource 各自的查询函数
- **inspect kind 完整列表**（[03-cli-subcommands § 8.7](../design/implementation/03-cli-subcommands.md) + [observability/00-overview § 7.1](../design/architecture/tactical/observability/00-overview.md)）：
  | kind | 数据源 |
  |---|---|
  | `task` | TaskRepository + 关联 executions / conversation / events |
  | `execution` | TaskExecutionRepository + ProjectionRepository（PK JOIN）+ ArtifactRepository + events |
  | `worker` | WorkerRepository + MappingRepository + 当前 executions + heartbeat events |
  | `issue` | IssueRepository + Conversation messages + spawned tasks + events |
  | `supervisor` | SupervisorInvocationRepo + DecisionRepo + events.correlation_id |
  | `conversation` | ConversationRepo + messages |
  | `input_request` | InputRequestRepo + 关联 execution + events |
  | `project` | ProjectRepo + mappings + tasks |
  | `worktree` | execution_id → workspace 元数据 |
  | `decision` | DecisionRepo + events.decision_id 反查触发事件 |
- **query resource 完整列表**（[observability/00-overview § 7.1](../design/architecture/tactical/observability/00-overview.md) 表格 + [03-cli § 8.7](../design/implementation/03-cli-subcommands.md)）：
  | resource | filter 集 |
  |---|---|
  | `tasks` | `--status / --project / --priority / --blocked-by / --not-dispatchable / --since` |
  | `executions` | `--status / --task-id / --worker / --since / --failed-reason` |
  | `workers` | `--status / --has-mapping` |
  | `issues` | `--status / --project / --opener` |
  | `input_requests` | `--status / --task-id` |
  | `proposals` | `--status` |
  | `events` | `--type` (前缀) `/ --task-id / --execution-id / --since / --actor / --correlation-id / --decision-id` |
  | `decisions` | `--invocation-id / --kind / --outcome / --since` |
- **cursor 语义**：所有 `query` 返回末页带 `next_cursor`（最后一行 id）；空 → 末页。客户端持有 cursor 调下一页直到 empty。
- **DoD**：
  - [ ] 10 个 kind 各有单测（fixture 数据 + 输出 JSON snapshot）
  - [ ] 8 个 resource 各有单测（filter / cursor / limit / since 矩阵）
  - [ ] cursor 分页正确：1000 行 events，每页 100，10 页拼回原序无重无漏
  - [ ] `events --type=task.` 前缀匹配 / `--task-id=T-42` refs 合取 都过单测
  - [ ] `limit` 超过上限 1000 → 报 explicit error（不静默截断）

### 3.6 CLI handlers（6 个 verb）

- **工件类型**：Application Service（CLI command）
- **依赖**：3.5 完成（QueryService API 稳定）+ 3.7（peek-trace）
- **产出**：
  - `cmd/agent-center/cli/inspect.go`
  - `cmd/agent-center/cli/query.go`
  - `cmd/agent-center/cli/ps.go`
  - `cmd/agent-center/cli/stats.go`
  - `cmd/agent-center/cli/logs.go`
  - `cmd/agent-center/cli/peek_trace.go`
- **实现步骤**：
  1. 每个 verb 注册到 root command（cobra）
  2. `--format=json|table` 双轨输出（[03-cli-subcommands § 4](../design/implementation/03-cli-subcommands.md)）
  3. exit code 遵循 § 5：0=ok / 2=usage / 4=not_found / 16=cas_conflict / etc
  4. JSON 输出 schema 稳定（[03-cli-subcommands § 4.2](../design/implementation/03-cli-subcommands.md)）；table 输出宽度自适应终端
  5. `ps --watch`：循环 sleep 2s + 重画（v1 简化，无 ANSI cursor 重定位，直接 clear screen + print）
  6. `logs --follow`：BlobStore 不支持 stream，`--follow` 仅对当前 execution 有意义（走 peek-trace 路径 + agent.log tail）；归档 logs `--follow` 报 explicit error
- **DoD**：
  - [ ] 6 个 handler `--help` 文案与 [03-cli-subcommands § 8.7](../design/implementation/03-cli-subcommands.md) 表格 1:1 对齐
  - [ ] JSON / table 双轨输出在 e2e 测试中各跑一遍
  - [ ] exit code 全部 case 有单测

### 3.7 peek-trace RPC（center → worker daemon → shim）

- **工件类型**：跨进程 RPC
- **依赖**：Phase 2 center↔worker daemon gRPC 长连接已建
- **产出**：
  - `proto/peek_trace.proto`（PeekTraceRequest / PeekTraceResponse stream）
  - center 侧 client：`internal/observability/peek/center_client.go`
  - worker daemon 侧 server：`internal/worker/peek/daemon_server.go`
- **实现步骤**：
  1. proto：`rpc PeekTrace(PeekTraceRequest) returns (stream PeekTraceResponse)`
  2. center 收 CLI 请求 → 找 execution 对应 worker_id → 查 mapping 拿 worker grpc addr → 发起 stream
  3. worker daemon 收请求 → 找当前 execution shim 工作目录 → 读 `events.jsonl`（不是 trace.jsonl.gz；这是 live 路径）→ 按 `--kind` filter / `--last=N` tail / `--follow` `inotify`-style 监视
  4. 流式回传；中断 / worker 死 → CLI 端收到 stream 关闭 + explicit error reason
  5. **不依赖归档**：execution 进行中即可（[ADR-0015 § 4](../design/decisions/0015-agent-trace-not-in-events-table.md)）
- **错误 reason 闭集**（[conventions § 16](../rules/conventions.md)）：`execution_not_found / worker_offline / worker_not_owner / trace_file_missing / stream_canceled`
- **DoD**：
  - [ ] e2e：dispatch task → execution running → `peek-trace E-x --last=10` 拉到最近 10 条 JSONL（worker daemon 真起）
  - [ ] `--follow` 流式：写入新一行 JSONL → CLI 端 ≤ 500ms 内收到
  - [ ] worker 不在线 → 立即报 `worker_offline` exit code，不挂起

### 3.7.x 与 § 2 可观测性自检（中间小节）

按 [conventions § 2](../rules/conventions.md) / plans README § 2.5，本 phase 新增工件的"被观测"清单：

| 工件 | 它 emit 哪些 event | inspect/query 是否扩列 | 失败路径如何 emit |
|---|---|---|---|
| TaskExecutionProjectionService | `observability.projection_stale_drop`（staleness） | `inspect execution --include-projection` 扩列 projection 字段 | UPSERT 失败 → 返回 explicit error；不静默；caller 决定 retry / 上报 |
| TraceArchiveService | `observability.trace_archive_uploaded / _failed` | `inspect task / execution` 增 `trace_blob_path` 字段 | failed event 必带 `reason ∈ {blob_store_unavailable / payload_too_large / checksum_mismatch} + message` |
| FleetSnapshotService | 不 emit（按需聚合） | `ps` 输出本身 | 4 段任一失败 → partial 输出 + stderr warning（exit 0；用户看得见） |
| QueryService | 不 emit（只读） | 自身就是查询面 | filter 拼写错 → exit 2 (usage)；超出 limit 上限 → exit 2 + 明确 message |
| UnknownEventEscalator | `observability.unknown_event_escalated` | `query events --type=observability.` 可拉到 | 自身 scan 失败 → emit `observability.escalator_scan_failed`（同模式）；不静默 |
| peek-trace | 不 emit（透传 RPC） | 自身是查询动作 | reason 闭集 `execution_not_found / worker_offline / worker_not_owner / trace_file_missing / stream_canceled` |

**自检通过**：所有失败 / 异常路径要么 emit event、要么 explicit error 返回 caller，**无吞**（[conventions § 17](../rules/conventions.md)）。

### 3.8 UnknownEventEscalator（周期 query）

- **工件类型**：Domain Service + 定时任务
- **依赖**：3.5 EventRepository.Find 完成；Phase 2 worker daemon 已 emit `agent_adapter.unknown_event_seen`（[05-agent-adapters § 3.1](../design/implementation/05-agent-adapters.md) Phase 2 应已实现）
- **产出**：
  - `internal/observability/escalator/unknown_event_escalator.go`
- **实现步骤**：
  1. `agent-center server` 启动时注册定时任务（默认 1h interval，config 可调）
  2. `EventRepository.Find { event_type=agent_adapter.unknown_event_seen, since=now-24h }`
  3. 按 `(adapter_name, cli_type_field)` group + count
  4. 任一 group `count ≥ threshold`（默认 10） → emit `observability.unknown_event_escalated`
  5. 已 escalate 过的 `(adapter_name, cli_type_field, day_bucket)` 24h 内去重（避免每小时刷屏）
- **去重存储**：内存 map（center 重启 = 重新评估，可接受）；不持久化
- **DoD**：
  - [ ] 阈值触发 / 不触发 / 去重 单测
  - [ ] 集成测试：注入 11 条 unknown_event_seen → 触发 1 条 escalated；再注入 5 条同 group → 不触发（去重）
  - [ ] 升 supervisor 决策路径留给 Phase 6（本 phase 只到 emit）

---

## § 4. Definition of Done（整体）

- [ ] § 1 所有工件实现并通过单元测试
- [ ] § 5 所有测试场景通过（unit + 集成 + e2e）
- [ ] 单测行覆盖率 ≥ 90%（diff + 整体；以 `go test -cover` 为准）
- [ ] 测试报告归档到 `docs/plans/reports/phase-4-test-report.md`
- [ ] 本 phase emit 的 4 个 event 实际进 events 表（集成测试验证）
- [ ] 6 个 CLI verb `--help` 与 [03-cli-subcommands § 8.7](../design/implementation/03-cli-subcommands.md) 1:1 对齐
- [ ] migration `0004_task_execution_projections.up.sql` 跑通 + down 可回滚
- [ ] **§ 9.z 边界验证**：grep `internal/task_runtime/` 不出现 `task_execution_projections` 表名 / 不出现 `TaskExecutionProjectionRepository` import
- [ ] 项目本地 `go vet ./... && golangci-lint run && go test ./...` 全过
- [ ] § 6 风险项要么处理要么显式 defer 到具体后续 phase

---

## § 5. 测试计划

### 5.1 单测场景

| 工件 | 测试场景 | 关键断言 |
|---|---|---|
| TaskExecutionProjectionService | UPSERT 新行 | 行成功 INSERT；`last_push_at` 等于 input |
| TaskExecutionProjectionService | UPSERT 已存行 | UPDATE 路径；所有列覆盖；version 列不存在 |
| TaskExecutionProjectionService | staleness（push out-of-order） | 不 UPDATE + emit `observability.projection_stale_drop` |
| TaskExecutionProjectionService | FK 违反（execution_id 不存在） | 返回 explicit error，不创建孤儿行 |
| TaskExecutionProjectionRepository | FindActive | 仅返回 status ∈ {submitted/working/input_required} 的 projection |
| TaskStatusProjectionService | OnExecutionTerminal in tx | 同 tx UPDATE + INSERT event；tx rollback 时双双回滚 |
| TaskStatusProjectionService | 调用未在 tx 内 | explicit error |
| FleetSnapshotService | 4 段并行 | 全部成功时返回完整 snapshot |
| FleetSnapshotService | 其中 1 段失败 | partial snapshot + warning event |
| FleetSnapshotService | `--project` 过滤 | 4 段 WHERE 都生效 |
| TraceArchiveService | 终态触发成功 | upload + 回填 trace_blob_path + emit uploaded event |
| TraceArchiveService | upload 失败 | emit failed event w/ reason；本地 jsonl 保留 |
| TraceArchiveService | retry on restart | daemon 重启扫 unfinished archives，retry 成功 |
| QueryService.EventRepo.Find | cursor 分页 | 100 条 events，10 页拼回原序，无重无漏 |
| QueryService.EventRepo.Find | type 前缀匹配 | `task.` 命中 `task.created` / `task_execution.failed` 不命中 |
| QueryService.EventRepo.Find | refs 合取过滤 | `task_id=T-42 AND worker_id=W-1` 二者都满足 |
| QueryService.Inspect × 10 kinds | 每 kind 一条 fixture | 输出 JSON snapshot 稳定 |
| QueryService.Query × 8 resources | 每 resource 一条 fixture | filter / since / limit / cursor 全 case |
| StatsService | scope=executions since=24h | COUNT / AVG 数学正确 |
| InspectHandler | json / table 双格式 | 输出 schema 稳定；table 不报 panic |
| QueryHandler | exit code 4 (not_found) | resource 拼写错误 → exit 2（usage） |
| PsHandler | `--watch` 单次循环 | 第二次调 snapshot 数据更新可见 |
| LogsHandler | `--follow` on archived | explicit error（不支持），exit code 非 0 |
| PeekTraceHandler | worker_offline | exit code + stderr message 明确 |
| UnknownEventEscalator | threshold 触发 | emit `observability.unknown_event_escalated` |
| UnknownEventEscalator | 去重 | 同 (adapter, type, day) 24h 内只 emit 一次 |
| EventRepository.Find | since + until 窗口 | 返回行 occurred_at ∈ [since, until) 严格 |
| EventRepository.Find | empty filter | 默认 ORDER BY id ASC，limit=100 |
| EventRepository.Find | actor 过滤 | 精确匹配 `supervisor:I-3` |
| EventRepository.Find | correlation_id 过滤 | 索引命中（idx_events_correlation） |
| EventRepository.Find | decision_id 过滤 | 索引命中（idx_events_decision） |
| FleetSnapshot VO | json 序列化稳定 | 字段顺序 / 命名与 JSON schema snapshot 文件 1:1 |
| InspectResult VO | 多 kind 反序列化 | 客户端解析 kind=task / kind=execution 时字段不冲突 |

### 5.2 集成测试场景

| 场景 | 涉及工件 | 关键断言 |
|---|---|---|
| events 表 cursor 分页 | EventRepository（真实 SQLite file） | 1000 行 INSERT，每页 100 拉，10 页拼回完整 |
| projection 高频更新不串味 | TaskExecutionProjectionService + SQLite WAL | 100 个 push / 秒，跑 30 秒，最后行 = 最新 push；无 lock timeout |
| fleet 聚合多表 join | FleetSnapshotService + 4 张真实表 | 50 executions × 10 workers fixture，snapshot 字段全对得上 |
| terminal hook trace archive | shim mock + center | execution.failed event → 自动触发 archive → trace_blob_path 落库 |
| events 同事务双写 | EventSink + state tx rollback | tx 内 emit 后 rollback → events 表 + state 表均无该行 |
| § 9.z 隔离回归 | source scan | `internal/task_runtime/**/*.go` 不引用 task_execution_projections 表 / Repository |
| migration up + down | 0004 SQL | up 后表存在 + 索引存在；down 后表删除；data 重新 up 不冲突 |
| 跨 BC tx 失败回滚 | TaskRuntime tx + Observability EventSink 在同 tx | execution.failed UPDATE 后 INSERT events 失败 → tx rollback → 主行 + event 均无记录 |
| TraceArchive 大文件回归 | shim mock 生成 50MB events.jsonl + 10MB agent.log | gzip 压缩比合理；upload 成功；download 字节对得上原文件 |
| peek-trace 跨网络 | center + worker 跑在不同 binary（同机器） | gRPC stream 完整跑通，不挂起 |
| UnknownEvent 去重存储重启 | center 重启后内存 dedup 表丢失 | 重启后立即再扫窗口 → 已 escalate 过的 group 会再 emit 一次 → 接受（v1 内存去重）+ 用集成测试明示行为 |
| stats 数学正确性 | 10 executions（3 success / 5 failed / 2 working） + `stats --scope=executions` | counts 与手动 SQL `COUNT GROUP BY status` 一致 |

### 5.3 e2e 测试场景

| 场景 | 用户视角 / 入口 CLI | 关键断言 |
|---|---|---|
| Dispatch → ps 可见 | `task create` → `task dispatch` → `agent-center ps` | execution 行出现，status=submitted；shortly 后 → working；projection 列有 current_activity |
| Inspect execution | execution 进行中 → `inspect execution E-x --include-projection` | 同时返回 task_executions 字段 + projection 字段（多 roundtrip） |
| Query events 跨 task | 跑多个 task → `query events --type=task. --since=10m` | 拉到正确 type / refs filter 命中 |
| Peek-trace live | execution running → `peek-trace E-x --last=5 --follow` | 实时收到最近 5 行 + 新写入行 ≤ 500ms |
| Terminal trace archive | execution 跑完 → `logs execution E-x` | 走 BlobStore download，内容是 events.jsonl + agent.log 合包 |
| Logs follow on archive 报错 | `logs execution E-x --follow` 在 execution 已 archived 后 | explicit error + exit code |
| UnknownEvent escalation | 注入 11 条 `agent_adapter.unknown_event_seen` → 触发 escalator | events 表新增 `observability.unknown_event_escalated` |
| Worker offline peek-trace | worker 杀掉 → `peek-trace E-x` | exit + reason=worker_offline |
| Stats happy path | 跑多个 task → `stats --scope=executions --since=1h` | 数字与手动 COUNT 一致 |
| Inspect 10 kinds smoke | fixture 全 10 kind 各跑一次 inspect | 不 panic + JSON schema 稳定 |
| ps `--watch` 实时刷新 | 起 server → 跑 task → 在另一 terminal `ps --watch` | 状态 submitted→working→completed 都在屏幕上能依次看到 |
| ps `--project=<slug>` 过滤 | 多 project fixture | 只显示该 project 的 executions / pending issues / mappings |
| Inspect decision → 反查事件 | supervisor 跑过一次 invocation → `inspect decision <id>` | 决策 + 触发事件链（events.decision_id JOIN）完整 |
| Query events cursor 全跑通 | 1000 events → 客户端循环拉直至 empty | 所有行不重不漏；总数 = 1000 |
| Logs archived task | task 跑完归档 → `logs task <id>` | 走 BlobStore download；输出含 events.jsonl + agent.log 合包 |
| Trace archive failure → retry | 强制 BlobStore 不可用跑 archive → 修复后重启 daemon | 失败 emit + 重启后自动 retry 成功 emit |
| § 9.z 违规扫描（lint 阶段） | CI 跑 source scan + `gomodguard` (若启用) | task_runtime 包不引 projection repo / 表名 |

---

## § 6. 风险 / Spike 项

### 6.1 events 表 refs JSON 过滤 v1 性能

- **风险**：refs 是 TEXT JSON，`task_id=T-42` 过滤拉到内存做。events 表 100k 行时 `query events --task-id=T-42` 退化为全表扫
- **缓解**：v1 默认 limit=100 + ORDER BY id DESC 拿最新一批 → 大多数 inspect / supervisor 场景是"近期 X 条"不是"全历史"。**postpone 到 Phase 7**：加 `task_id_indexed TEXT GENERATED ALWAYS AS (json_extract(refs,'$.task_id')) STORED` 反范式列 + index（SQLite-only 特性，[02-persistence-schema § 8.2.3](../design/implementation/02-persistence-schema.md)）。**v1 显式标注，不悄悄退化**

### 6.2 peek-trace 在 worker 离线时的 UX

- **风险**：用户调 peek-trace 时 worker 刚好掉线 → 拿不到任何信息；execution 仍在 events 表显示 "working"
- **缓解**：reason=`worker_offline` 显式提示用户：execution 状态可能 stale，建议 `query workers --status=offline` 复查。**不**做"自动 fallback 到归档"（归档此时未生成）。v1 接受此 UX

### 6.3 § 9.z BC 物理隔离的工程强制

- **风险**：人为约定靠 code review；新人提 PR 误改容易翻车
- **缓解**：v1 走 code review + 集成测试中的 source scan 兜底（§ 5.2 的 "§ 9.z 隔离回归"）。**Spike**：调研 `gomodguard` 配置加 disallow-import 规则；若可行，本 phase 内加；否则 defer to Phase 5 收尾

### 6.4 ProjectionFactory cold-rebuild

- **out-of-scope**：[observability/00-overview § 4](../design/architecture/tactical/observability/00-overview.md) 已显式留白 "罕见，仅 v1 没设计 cold-rebuild 路径；待 implementation 决定"。本 phase 不做。projection 表损坏 → DB 备份恢复（[ADR-0014 § 4](../design/decisions/0014-event-sourcing-level.md)）

### 6.5 stats 在大数据量下的查询成本

- **风险**：`stats --scope=executions --since=30d` 全表扫 COUNT / AVG
- **缓解**：v1 单用户单 VPS 量级（万行级）可接受；**postpone 到 roadmap**：加预聚合表 / 物化视图

### 6.6 `--watch` 体验

- **风险**：v1 clear-screen 方式在小窗口闪屏；大型 fleet 看不全
- **缓解**：v1 文档明示"小屏看 `--by-worker` 切片"；后续上 TUI（[observability/00-overview § 8 roadmap](../design/architecture/tactical/observability/00-overview.md) "实时 stream timeline"）

### 6.7 trace.jsonl.gz 大文件可能撑爆 BlobStore quota

- **风险**：长任务 trace 几十 MB，多 task 并跑 1 天可能数 GB；LocalDir BlobStore 单盘写满
- **缓解**：v1 单 VPS 容量足；**postpone**：BlobStore 用量 metric + 老 trace GC 策略（保留 90 天）留 roadmap。本 phase 在 `observability.trace_archive_uploaded` event 带 `bytes` 字段，给将来量化分析留底
- **现在做**：`payload_too_large` 阈值（默认 100MB）作为 hard guard，避免单 trace 撑爆

### 6.8 inspect 多 roundtrip 在慢盘下的延迟

- **风险**：`inspect execution --include-projection --include-artifacts` 串行 3 次 SELECT；高 IOPS 慢盘下 ~30ms 起步
- **缓解**：v1 SQLite WAL + `:memory:` 测试基本 ms 级；生产单盘可接受。**用 errgroup 并行** 3 个 SELECT（无依赖）；不引入 SQL JOIN（守 § 9.z）

### 6.9 周期 escalator 与 server 生命周期耦合

- **风险**：`agent-center server` 重启会重置 escalator 内存去重 → 短时间内可能重复 emit
- **缓解**：明示行为（§ 5.2 已 cover）；将"去重持久化"标 spike，**defer to Phase 6**（supervisor 真正开始消费 escalation event 时再处理）

---

## § 7. 下游解锁

本 phase 完成后：

### 7.1 解锁 Phase 5（Bridge ACL Outbound）

Bridge 给飞书渲染卡片时的数据源即是本 phase 的 QueryService。Phase 5 实现"卡片渲染器"调本 phase API（[observability/00-overview § 7.4](../design/architecture/tactical/observability/00-overview.md)），**不**自造查询路径。

### 7.2 解锁 Phase 6（Cognition Supervisor）

Supervisor invocation 拉 `trigger_event_ids` + 跨上下文 timeline 查询 100% 走 `EventRepository.Find` + `QueryService.Inspect`（[cognition/00-overview § 7.3](../design/architecture/tactical/observability/00-overview.md)）。Phase 6 不为 supervisor 单造 RPC / 查询接口。

### 7.3 解锁 Phase 7（部署收尾）

`agent-center ps` / `stats` 成为运维标准入口；部署 runbook 引用本 phase 命令做 smoke check。

### 7.4 本 phase 提供的接口 surface（冻结）

| Surface | Consumer |
|---|---|
| `EventRepository.Find(EventQueryFilter) → []*Event` | Phase 5 Bridge 渲染 / Phase 6 supervisor |
| `QueryService.Inspect(kind, id) → InspectResult` | Phase 5 / 6 |
| `QueryService.Query(resource, filter) → []` | Phase 5 / 6 |
| `FleetSnapshotService.Snapshot(projectFilter) → FleetSnapshot` | Phase 5 / 6 / 部署 smoke |
| `TaskExecutionProjectionService.UpdateProjection` | Phase 2 worker daemon push 路径（已对接） |
| `TraceArchiveService` 终态 hook 协议 | Phase 2 shim（已对接） |
| `agent-center inspect / query / ps / stats / logs / peek-trace` CLI | 所有用户 / supervisor / Web Console（v2） |

**冻结约束**：上述接口签名后续 phase **不允许改语义**，只能扩列。新 inspect kind / query resource 加列是兼容的；删除 / 改字段语义不兼容。
