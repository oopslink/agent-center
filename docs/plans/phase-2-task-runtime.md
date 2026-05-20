# Phase 2: TaskRuntime Core

> DDD BC: **TaskRuntime**（Core / Supporting-Essential）· 依赖 Phase 1 · 解锁 Phase 3
> 纪律：按里程碑顺序 / 模块完备不半成品 / 单测 ≥ 90% + 集成 + e2e + 测试报告

> 本 phase 是 v1 工件最重的 phase ——Task / TaskExecution / InputRequest 三聚合 + per-execution shim + worker daemon dispatch loop + Agent CLI Adapter + 全套 CLI handler 全部落地。所有 ADR-0010 / 0011 / 0017 / 0018 / 0019 的协议在此 phase 兑现为代码。

---

## § 0. 目标

**用户视角**：Phase 2 完成后，用户能在 CLI 上：

1. 创建 task（`task create`）、把 task 绑到 Conversation（`task bind-conversation`/`unbind-conversation` 接口预留）
2. 派单（`dispatch`）—— center 选 worker、构造 DispatchEnvelope、worker 接 envelope spawn shim、shim spawn agent CLI 子进程、agent 实际跑起来
3. 看任务跑：从 `submitted → working` 状态机推进；agent 中途调 `request-input`/`report-progress`/`report-artifact` CLI；agent exit 0 → `completed`、非 0 → `failed`
4. 主动停（`kill-execution`）—— Center 发 `kill_requested` → worker SIGTERM → 5s grace → SIGKILL → emit `killed`
5. 异常路径：dispatch 30s no-ACK / shim 60s no-hello / shim 崩 / agent 崩 / worker 心跳超时 / execution 6h 总超时 / submitted 5min 超时 / InputRequest 24h 超时 —— 全部按 § 1.7 表的 reason+message 落地

**Supervisor 视角**：Phase 2 提供了 supervisor 决策可触发的 4 类动作（dispatch / kill-execution / abandon-task / suspend-task；后两者本 phase 仅暴露给 user，supervisor 的实际唤醒链路留 Phase 6，但 CLI 必须就位）。

**运维视角**：所有 task.* / task_execution.* / input_request.* / worktree.* / artifact.* domain event 走 Phase 1 EventSink 落 `events` 表（同事务双写，[ADR-0014 § 2](../design/decisions/0014-event-sourcing-level.md)）。Observability 投影留 Phase 4；本 phase 仅消费 Phase 1 已有的 `events` 表 / EventSink。

**DDD 意义**：BC TaskRuntime 从架构层（docs/design/architecture/tactical/task-runtime/）落成代码。3 个独立 AR + 1 个子 Entity + 1 套 Domain Service + 1 套 ACL Adapter（agent CLI）+ 1 套 Worker 端运行时（per-execution shim 模型 / ADR-0018），完整兑现 [ADR-0019](../design/decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md) BC 合并方案。

---

## § 1. DDD 工件清单

### 1.1 Aggregate Roots

| AR | Go 包路径 | 状态机 | 说明 |
|---|---|---|---|
| **Task** | `internal/taskruntime/task` | 4 态（open / suspended / done / abandoned） | 工作单元身份；4 态见 [01-task § 2](../design/architecture/tactical/task-runtime/01-task.md) |
| **TaskExecution** | `internal/taskruntime/execution` | 6 态（submitted / working / input_required / completed / failed / killed） | 独立 AR（[ADR-0019](../design/decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md)）；状态机见 [02-task-execution § 2](../design/architecture/tactical/task-runtime/02-task-execution.md) |
| **InputRequest** | `internal/taskruntime/inputrequest` | 4 态（pending / responded / timed_out / canceled） | 独立 AR；[03-input-request § 2](../design/architecture/tactical/task-runtime/03-input-request.md) |

### 1.2 Entities（子从属）

| Entity | 从属 | Go 包路径 | 说明 |
|---|---|---|---|
| **Artifact** | TaskExecution | `internal/taskruntime/execution/artifact.go` | append-only；独立表 `artifacts`；[02-task-execution § 12](../design/architecture/tactical/task-runtime/02-task-execution.md) |

### 1.3 Value Objects

| VO | Go 包路径 | 用途 |
|---|---|---|
| **DispatchEnvelope** | `internal/taskruntime/dispatch/envelope.go` | Center → Worker 派单载荷；详见 [02-task-execution § 5.2](../design/architecture/tactical/task-runtime/02-task-execution.md) |
| **DispatchAck / DispatchNack** | `internal/taskruntime/dispatch/ack.go` | Worker → Center ACK / NACK；[ADR-0011 § 2](../design/decisions/0011-dispatch-reliability-protocol.md) |
| **WorkspaceMode** | `internal/taskruntime/execution/workspace_mode.go` | `worktree` \| `direct` 枚举 |
| **Priority** | `internal/taskruntime/task/priority.go` | `high` \| `medium` \| `low` |
| **CompletedReason / FailedReason / KilledReason + Message** | `internal/taskruntime/execution/reason.go` | 闭集枚举 + 配 message（[conventions § 16](../rules/conventions.md)） |
| **IssueConcludeSpec** | `internal/taskruntime/dispatch/issue_conclude_spec.go` | Discussion BC 调入 batch spawn 入参 stub（Phase 3 由 Discussion 注入实际值） |
| **ReconcileRequest / ReconcileResponse** | `internal/taskruntime/reconcile/types.go` | Worker enroll / 重连对账三类分组 |
| **InputResponse** | `internal/taskruntime/inputrequest/response.go` | InputRequest 响应载荷（answer / decided_by / decided_at） |

### 1.4 Repositories

按 [00-overview § 5](../design/architecture/tactical/task-runtime/00-overview.md) Go 接口签名 + sentinel errors 落代码：

| Repository | 接口位置 | SQLite 实现位置 | 表 |
|---|---|---|---|
| `TaskRepository` | `internal/taskruntime/task/repository.go` | `internal/persistence/sqlite/task_repo.go` | `tasks` |
| `TaskExecutionRepository` | `internal/taskruntime/execution/repository.go` | `internal/persistence/sqlite/task_execution_repo.go` | `task_executions` |
| `InputRequestRepository` | `internal/taskruntime/inputrequest/repository.go` | `internal/persistence/sqlite/input_request_repo.go` | `input_requests` |
| `ArtifactRepository` | `internal/taskruntime/execution/artifact_repository.go` | `internal/persistence/sqlite/artifact_repo.go` | `artifacts` |

错误：每个 Repository 暴露各自 sentinel errors（[00-overview § 5](../design/architecture/tactical/task-runtime/00-overview.md)）—— `ErrTaskNotFound` / `ErrTaskExecutionNotFound` / `ErrSingleActiveViolation` / `ErrInputRequestNotFound` 等。

### 1.5 Domain Services

| Service | Go 包路径 | 职责 | 本 phase 实装? |
|---|---|---|---|
| **DispatchService** | `internal/taskruntime/dispatch/service.go` | Task → TaskExecution 创建 + Envelope 发送 + ACK/NACK 处理 + 单活校验 + max_executions_per_task 兜底 | ✅ 完整实装 |
| **ReconcileService** | `internal/taskruntime/reconcile/service.go` | Worker enroll / 重连对账；返回 active / stale / unknown | ✅ 完整实装 |
| **TimeoutScanner** | `internal/taskruntime/timeoutscan/scanner.go` | 30s 周期 ticker；扫 4 类 timeout（submitted / execution / input_request / worker_heartbeat）；worker_heartbeat 部分从 Workforce BC 订阅 worker.offline 事件 | ✅ 完整实装 |
| **KillCoordinator** | `internal/taskruntime/kill/coordinator.go` | 两阶段 kill 协调（emit kill_requested → worker SIGTERM → emit killed）；6 种 killed reason 区分 | ✅ 完整实装 |
| **IssueConcludeSpawn** | `internal/taskruntime/dispatch/issue_conclude_spawn.go` | Discussion BC 调入 batch spawn N tasks（all-or-nothing） | ⚠️ **接口 stub**（Phase 3 由 Discussion BC 接入真实 caller）；本 phase 仅保证接口形状 + 单元测试（mock IssueConcludeSpec 直接调） |

### 1.6 Application Services（CLI handler 层）

按 [03-cli § 8.1 + § 9.1](../design/implementation/03-cli-subcommands.md) 实装：

| CLI handler | Go 文件 | 调谁 |
|---|---|---|
| `task create` | `cmd/agent-center/task/create.go` | `TaskService.Create` → TaskRepository.Save + （a/e 路径同事务建 Conversation） |
| `task bind-conversation` | `cmd/agent-center/task/bind_conversation.go` | `TaskService.BindConversation` |
| `task unbind-conversation` | `cmd/agent-center/task/unbind_conversation.go` | 接口预留（v1 不实装；返回 not_implemented） |
| `dispatch` | `cmd/agent-center/dispatch.go` | `DispatchService.Dispatch` |
| `kill-execution` | `cmd/agent-center/kill_execution.go` | `KillCoordinator.RequestKill` |
| `request-input` | `cmd/agent-center/request_input.go` | `InputRequestService.Create`；阻塞等回应 |
| `report-progress` | `cmd/agent-center/report_progress.go` | （转 Conversation Message + 推 Phase 4 projection；本 phase 仅写 Message + emit event） |
| `report-artifact` | `cmd/agent-center/report_artifact.go` | `ArtifactService.Append` |
| `report-failure` | `cmd/agent-center/report_failure.go` | `TaskExecutionService.MarkFailed`（reason=agent_reported_failure） |
| `read-task-context` | `cmd/agent-center/read_task_context.go` | `TaskService.ReadContext`；返回 task + project + parent + deps + 最近 Messages |
| `worker shim` | `cmd/agent-center/worker/shim.go` | shim 子命令（detached）；audience=Sys |

### 1.7 Domain Events（emit 进 `events` 表）

按 [observability/00-overview](../design/architecture/tactical/observability/00-overview.md) 命名规范 `<bc>.<entity>.<action>`：

| Event | 触发 | reason+message? |
|---|---|---|
| `task.created` | TaskFactory（a/b/c/d/e 5 caller） | - |
| `task.suspended` / `task.resumed` / `task.abandoned` / `task.done` | Task 状态机 | abandoned 配 reason+message |
| `task.dependency_added` / `task.dependency_removed` | depends_on_task_ids 改 | - |
| `task.workspace_mode_changed` | requires_worktree 改 | - |
| `task.dispatch_limit_reached` | max_executions_per_task 超 | reason=`dispatch_limit_reached`+message |
| `task_execution.submitted` | DispatchService 创建 | - |
| `task_execution.dispatched` | Envelope 发出 / dispatch_state=pending_ack | - |
| `task_execution.acked` / `task_execution.nacked` | Worker ACK/NACK | nack 配 reason+message |
| `task_execution.working` | Worker spawn agent OK | - |
| `task_execution.input_required` | InputRequest 创建联动 | - |
| `task_execution.completed` | Agent exit 0 + 无 pending IR | reason=`agent_reported_success`（或 null）+message |
| `task_execution.failed` | 11 类 failed reason（见 [02-task-execution § 7.1](../design/architecture/tactical/task-runtime/02-task-execution.md)） | reason+message 必填 |
| `task_execution.kill_requested` | KillCoordinator stage 1 | reason+message |
| `task_execution.killed` | KillCoordinator stage 2 / Worker 上报 | reason+message |
| `input_request.requested` | InputRequestFactory | - |
| `input_request.responded` | 三响应路径 | - |
| `input_request.timed_out` | TimeoutScanner T2=24h | reason=`input_timeout_t2`+message |
| `input_request.canceled` | KillCoordinator 联动 | reason+message |
| `worktree.created` / `worktree.released` | worker daemon worktree mode | - |
| `artifact.uploaded` | report-artifact | - |
| `agent_adapter.unknown_event_seen` | adapter 遇 EventUnknown | reason+message+sample_raw（[05-agent-adapters § 3.1](../design/implementation/05-agent-adapters.md)） |

每条 event 走 Phase 1 提供的 EventSink（同事务双写），不直接 INSERT events 表。

### 1.8 Context Map 关系

| 关系 | 对方 BC | 类型 | 本 phase 行为 |
|---|---|---|---|
| TaskRuntime ↔ Workforce | Workforce | Shared Kernel（task / execution 引用 worker_id / project_id） | 本 phase 通过 Phase 1 已就绪的 `WorkerRepository` / `ProjectRepository` 查 worker / project 元数据 |
| TaskRuntime ↔ Conversation | Conversation | Shared Kernel（task.conversation_id 1:1） | 本 phase 通过 Phase 1 已就绪的 `ConversationService.AddMessage` / `Create` API 双写 |
| Discussion → TaskRuntime | Discussion | Customer-Supplier（IssueConcludeSpawn） | 本 phase 仅做 IssueConcludeSpawn 接口 stub；Phase 3 由 Discussion BC 接入 |
| TaskRuntime → Discussion | Discussion | Customer-Supplier（agent open-issue） | 本 phase 不实装；agent CLI 调 `open-issue` 在 Phase 3 实装 |
| Bridge → TaskRuntime | Bridge | Customer-Supplier | 本 phase 暴露 `InputRequest.Respond` API；Bridge 实装在 Phase 5/7 |
| Observability ← TaskRuntime | Observability | Open Host | 全部 event 走 Phase 1 EventSink |

---

## § 2. 上游依赖（来自 Phase 1）

Phase 1 必须交付以下工件作为本 phase 的输入：

| Phase 1 工件 | 本 phase 哪一步会用 |
|---|---|
| **Shared Kernel: ULID / time 工具 / clock.Clock interface** | TaskFactory / ExecutionFactory 生成 id；TimeoutScanner 注入 clock |
| **`events` 表 + migration（01-events.up.sql）** | 所有 emit 走 EventSink 落此表（[02-persistence § 8.2.1](../design/implementation/02-persistence-schema.md)） |
| **EventSink Domain Service（`internal/observability/eventsink`）** | DispatchService / KillCoordinator / TimeoutScanner / 各 CLI handler 调 `Emit(ctx, eventType, refs, payload)` |
| **EventRepository.Append（append-only INSERT）** | EventSink 内部用 |
| **tx-via-ctx 模板（`internal/persistence.WithTx / TxFromCtx`）** | 跨聚合同事务双写（task+conversation；task_execution+input_request；execution+artifact 等） |
| **SQLite 连接池 + golang-migrate** | 本 phase 加 migration `0002_taskruntime.up.sql`（tasks / task_executions / input_requests / artifacts 四表 + 索引） |
| **BlobStore Adapter + 路径约定**（[01-blob-store](../design/implementation/01-blob-store.md)） | task.description > 10KB / Artifact blob_ref / agent.log 归档 |
| **Conversation BC**（Phase 1 交付） | `ConversationService.Create(kind=task)` / `AddMessage(content_kind=agent_finding, input_request_ref=...)` |
| **Workforce BC `WorkerRepository` / `ProjectRepository` / Worker 心跳 + offline 事件**（Phase 1 交付） | DispatchService 查 worker / project；TimeoutScanner 订阅 `worker.offline` 事件触发 worker_lost 处理 |
| **`agent-center server` 启动框架 + admin unix socket / gRPC worker 端口**（Phase 1 交付） | dispatch loop / shim 接入；CLI handler 注册 |
| **Repository 模板 / 单测 harness（`:memory:` SQLite + WithTx）**（Phase 1 交付） | Phase 2 Repository 实装与单测套用 |

> **Phase 1 没做不做事的 fall-back**：若 Phase 1 某工件未落地，**本 phase 不能开工** —— 按 [README § 2.1](README.md) 严格顺序原则。

---

## § 3. 工作项分解（严格按 DDD + 依赖顺序）

> 顺序原则：AR → VO → Repository → Domain Service → ACL Adapter → Worker 运行时 → CLI handler → 端到端验证。前后依赖严格，不允许并行多项。

### 3.1 TaskRuntime AR 聚合 + Repository（Task / TaskExecution / InputRequest / Artifact）

**工件**：4 个聚合 Go struct + 4 个 Repository 接口 + 4 个 SQLite 实现 + migration `0002_taskruntime.up.sql`

**输入（依赖）**：Phase 1 ULID / clock / tx-via-ctx / SQLite 连接池 / migration 框架

**输出**：

- `internal/taskruntime/task/task.go` — Task struct + 状态机 method（`Suspend` / `Resume` / `Abandon` / `MarkDone`）+ Invariants 校验
- `internal/taskruntime/task/repository.go` — TaskRepository interface（[00-overview § 5.1](../design/architecture/tactical/task-runtime/00-overview.md)）+ sentinel errors
- `internal/taskruntime/execution/execution.go` — TaskExecution struct + 6 态状态机 method
- `internal/taskruntime/execution/repository.go` — TaskExecutionRepository interface + sentinel errors
- `internal/taskruntime/execution/artifact.go` — Artifact entity struct + ArtifactRepository interface（append-only）
- `internal/taskruntime/inputrequest/inputrequest.go` — InputRequest struct + 4 态状态机
- `internal/taskruntime/inputrequest/repository.go` — InputRequestRepository interface
- `internal/persistence/sqlite/task_repo.go` / `task_execution_repo.go` / `input_request_repo.go` / `artifact_repo.go` — SQLite 实现（CAS UPDATE，[02-persistence § 8.1.2](../design/implementation/02-persistence-schema.md) SQL 模板）
- `internal/persistence/migrations/0002_taskruntime.up.sql` + `.down.sql`

**实现步骤**：

1. 写 4 个 AR struct（field 表对位 [01-task § 5](../design/architecture/tactical/task-runtime/01-task.md) / [02-task-execution § 5.1](../design/architecture/tactical/task-runtime/02-task-execution.md) / [03-input-request § 3](../design/architecture/tactical/task-runtime/03-input-request.md) / [02-task-execution § 12.2](../design/architecture/tactical/task-runtime/02-task-execution.md)）
2. 每个 AR 写状态机迁移方法 + Invariants 校验函数（return domain error）
3. 写 Repository interface + sentinel errors（用 `errors.New`；调用方走 `errors.Is`）
4. 写 SQLite migration `0002_taskruntime.up.sql`：四表 DDL（[02-persistence § 8.1.1](../design/implementation/02-persistence-schema.md)）+ down migration
5. 写 SQLite 实现：`Save` / `FindByID` / `FindByXxx` / `Update*` —— 全部用 tx-via-ctx；`Update*` 用 CAS UPDATE（`version=?` + `RETURNING version`），RowsAffected==0 → 返回 `Err*VersionConflict`
6. 写 Repository 单测（用 `:memory:` SQLite + Phase 1 harness）：覆盖 Save / 全 Find / 全 Update / CAS 冲突 / 状态机非法跃迁 / not_found / version conflict / append-only 拒绝 UPDATE 等路径

**DoD**：

- [ ] 4 个 AR struct + 4 个 Repository interface 编译通过
- [ ] migration up / down 在 `:memory:` SQLite 上跑通
- [ ] Repository 单测覆盖率 ≥ 90%（含 happy + 全异常路径）
- [ ] CAS 冲突路径有单测显式断言 `errors.Is(err, ErrXxxVersionConflict)`
- [ ] Invariants 校验路径全有用例（如 single-active 单 task 多 active 执行 → `ErrSingleActiveViolation`）

### 3.2 VO 集（DispatchEnvelope / Reason+Message / WorkspaceMode / Priority / Reconcile types）

**工件**：内部 VO struct，跨包共用

**输入（依赖）**：3.1（VO 要引用 TaskID / TaskExecutionID 类型）

**输出**：

- `internal/taskruntime/dispatch/envelope.go` — DispatchEnvelope struct + JSON marshal/unmarshal + envelope_version="v1" 常量
- `internal/taskruntime/dispatch/ack.go` — DispatchAck / DispatchNack + NackReason 闭集枚举（[ADR-0011 § 2](../design/decisions/0011-dispatch-reliability-protocol.md) 6 种）
- `internal/taskruntime/execution/workspace_mode.go` — WorkspaceMode enum + `Parse` / `String`
- `internal/taskruntime/task/priority.go` — Priority enum
- `internal/taskruntime/execution/reason.go` — CompletedReason / FailedReason（11 种）/ KilledReason（7 种）枚举 + `Reason.Validate`（防野字符串）
- `internal/taskruntime/reconcile/types.go` — ReconcileRequest / ReconcileResponse
- `internal/taskruntime/dispatch/issue_conclude_spec.go` — IssueConcludeSpec struct（Phase 3 由 Discussion 填值）
- `internal/taskruntime/inputrequest/response.go` — InputResponse struct

**实现步骤**：

1. 每个 VO 写 immutable struct（无 setter；构造走 factory func）
2. 写 JSON marshal/unmarshal（envelope 跨进程通信）
3. 每个 reason 枚举写 `Validate()` —— 未知字符串拒绝 → 返回 `ErrUnknownReason`
4. VO 单测：marshal/unmarshal round-trip / Validate 拒绝野字符串 / 各 enum 全值覆盖

**DoD**：

- [ ] 每个 VO struct 不可变（无 exported setter）
- [ ] DispatchEnvelope JSON round-trip 单测过
- [ ] 每个 reason 枚举全值在测试中至少出现一次（包含未知字符串拒绝路径）
- [ ] 覆盖率 ≥ 90%

### 3.3 Domain Services（DispatchService / ReconcileService / TimeoutScanner / KillCoordinator + IssueConcludeSpawn stub）

**工件**：4 个完整实装 + 1 个 stub

**输入（依赖）**：3.1 / 3.2 / Phase 1 EventSink / Phase 1 Workforce BC（WorkerRepository / 心跳 / project.* 事件订阅）/ Phase 1 Conversation API / Phase 1 clock.Clock

**输出**：

- `internal/taskruntime/dispatch/service.go`：
  - `DispatchService.Dispatch(ctx, taskID, pick) (TaskExecution, error)` —— 同事务：单活校验 → 创建 TaskExecution(submitted) → 写 task.current_execution_id → emit `task_execution.submitted` + `task_execution.dispatched` → 调 worker 接发 envelope（异步、不在 tx）→ 监听 ACK
  - `DispatchService.HandleAck(ctx, ack)` —— 写 dispatch_state=acked + emit `task_execution.acked`
  - `DispatchService.HandleNack(ctx, nack)` —— execution → failed(reason=`dispatch_nack:<sub>`)
  - `DispatchService.scanPendingAck(ctx)` —— 30s 无 ACK → execution → failed(reason=`dispatch_no_ack`)
  - max_executions_per_task 兜底（默认 3，配置 `execution.max_executions_per_task`）；超 → emit `task.dispatch_limit_reached`
- `internal/taskruntime/reconcile/service.go`：
  - `ReconcileService.Handle(ctx, req) (resp, error)` —— 查 worker 上的 active execution + center 视角的 active；分 active / stale / unknown 三类返回
- `internal/taskruntime/timeoutscan/scanner.go`：
  - `TimeoutScanner.Run(ctx)` —— 30s ticker（clock 注入）；每 tick 扫 4 类
  - submitted_timeout（5min default）→ execution → failed(`submitted_timeout`)
  - execution_timeout（6h default，基于 working_seconds_accumulated 不计 IR 时段）→ emit kill_requested(reason=`timeout_kill`) 给 KillCoordinator
  - input_request T1=4h ping（emit supervisor ping）/ T2=24h fail（IR → timed_out + execution → failed(`input_timeout`)）
  - worker_heartbeat 由 Phase 1 Workforce BC 负责检测；本 scanner 仅订阅 `worker.offline` 事件 → 上面所有 active execution → failed(`worker_lost`)
- `internal/taskruntime/kill/coordinator.go`：
  - `KillCoordinator.RequestKill(ctx, executionID, reason, message)` —— 写 cancel_requested_at + emit `task_execution.kill_requested`；下发 worker daemon
  - `KillCoordinator.HandleKilled(ctx, executionID, reason, message)` —— worker 上报 killed 后 execution → killed；若是 abandon/suspend precondition 联动 task 状态变更
  - kill submitted 状态时直接转 killed（不 SIGTERM；[02-task-execution § 4](../design/architecture/tactical/task-runtime/02-task-execution.md) 边界）
- `internal/taskruntime/dispatch/issue_conclude_spawn.go`：
  - `IssueConcludeSpawn.Spawn(ctx, spec) ([]TaskID, error)` —— **接口 stub**：实装 batch all-or-nothing 单事务、dep 图解析（既有 uuid + batch 内 local_id 混用）+ DFS 无环检测 + 各 task spec 校验；Phase 3 由 Discussion BC 实际接入

**实现步骤**：

1. 每个 Service 写构造函数（依赖通过 interface 注入：Repository / EventSink / clock.Clock / config）
2. 实装核心方法 + 错误路径全部 emit event（[conventions § 17](../rules/conventions.md) 不许吞）
3. 写 Service 单测：
   - DispatchService：happy / 单活冲突 / max_executions 超限 / NACK 各 sub_reason / 30s no-ACK
   - ReconcileService：active+stale+unknown 三分组
   - TimeoutScanner：4 类 timeout 用 mock clock 注入推时；worker_offline 事件订阅触发
   - KillCoordinator：kill submitted / working / input_required；abandon_precondition / suspend_precondition / timeout_kill 联动
   - IssueConcludeSpawn：batch=1/N、dep 图（local_id + 既有 uuid）/ 环检测 / 校验失败回滚

**DoD**：

- [ ] 4 个 Service 完整实装；IssueConcludeSpawn 接口 stub + 单测能用 mock spec 调通
- [ ] 每个错误路径 emit event（单测断言 EventSink 收到对应 event）
- [ ] TimeoutScanner 100% 用 mock clock；测试无 sleep（[testing.md § 4](../rules/testing.md)）
- [ ] 覆盖率 ≥ 90%（含 § 5 测试计划全部场景）

### 3.4 Agent CLI Adapter 包（接口 + claude-code 实装 + codex/opencode 预留）

**工件**：Adapter interface + register table + claude-code 主力 + EventUnknown observable degrade + 2 个预留 subpackage

**输入（依赖）**：3.1（AgentTraceEvent 内部 schema 引用 execution_id）/ Phase 1 EventSink（emit `agent_adapter.unknown_event_seen`）

**输出**：

- `internal/agentadapter/adapter.go` — Adapter interface（4 方法 Name / BuildCommand / ParseEvent / SupportsSession）+ Register / Get table（[05-agent-adapters § 2](../design/implementation/05-agent-adapters.md)）
- `internal/agentadapter/event.go` — AgentTraceEvent struct + EventType enum（7 个：thinking / tool_call / tool_result / tokens_report / turn_end / error / unknown）
- `internal/agentadapter/claudecode/adapter.go` + `init.go`（自注册）：
  - BuildCommand: `claude --output-format stream-json --session-id=<execution_id> -p "<prompt>"`
  - ParseEvent: 5 类映射 + 兜底 EventUnknown
  - SupportsSession: true
- `internal/agentadapter/codex/`（仅 skeleton）：Adapter struct + Name="codex"；BuildCommand / ParseEvent 返回 ErrNotImplemented；不自注册（避免误用）
- `internal/agentadapter/opencode/`（同上）
- `internal/agentadapter/unknown_event_reporter.go` —— EventUnknown 去重 + emit `agent_adapter.unknown_event_seen` + per-execution 阈值告警（[05-agent-adapters § 3.1](../design/implementation/05-agent-adapters.md)）

**实现步骤**：

1. 写 Adapter interface + Register table（基于 `init()` 自注册）
2. 写 AgentTraceEvent + 7 个 EventType const
3. 写 claude-code adapter：BuildCommand 拼 args / env，ParseEvent 用 `switch raw["type"]` 映射 + 5 类已知 → AgentTraceEvent；未知 → EventUnknown + Raw 留全
4. 写 UnknownEventReporter：per-execution 内同 `(adapter_name, cli_type)` 只 emit 一次 → 走 EventSink；超 N 条/execution → emit `task_execution.warning(reason=adapter_significantly_behind)`
5. 写 codex / opencode skeleton（接口形状对齐；BuildCommand / ParseEvent 直接 return ErrNotImplemented）
6. 单测：
   - claude-code BuildCommand 断言 args 顺序（[05 § 9.1](../design/implementation/05-agent-adapters.md)）
   - ParseEvent 喂 testdata JSONL 行（5 类各喂一行 + 1 行未知 type）断言映射
   - UnknownEventReporter 去重 + 阈值告警
   - 集成测：spawn `testdata/mock-agents/thinking_then_tool_call.sh` 模拟 agent CLI；断言完整 event 流（[05 § 9.1](../design/implementation/05-agent-adapters.md)）

**DoD**：

- [ ] Adapter interface 编译通过 + claude-code 自注册可 Get("claude-code")
- [ ] codex / opencode 显式 not_implemented（**不算违反 § 2.2 半成品** —— ADR-0019 / Phase 2 显式仅 v1 主力 claude-code；接口预留是契约义务）
- [ ] EventUnknown observable degrade 全单测覆盖
- [ ] 覆盖率 ≥ 90%（含 mock-agent 集成测）

### 3.5 Per-execution shim 子命令（`agent-center worker shim`）

**工件**：`worker shim` 子命令；shim ↔ daemon RPC；per-execution 目录管理

**输入（依赖）**：3.4 Agent CLI Adapter（shim 内部用 Adapter.ParseEvent 解 JSONL）

**输出**：

- `cmd/agent-center/worker/shim.go` — shim 子命令 entry
- `internal/shim/shim.go` — Shim main loop：detached / setsid / fork+exec agent / 监 unix socket / append events.jsonl / 接 daemon RPC
- `internal/shim/rpc.go` — shim ↔ daemon RPC：ShimHello / PhaseChanged / Event / RPCForward / ShimGoodbye（[ADR-0018 § 4](../design/decisions/0018-detached-agent-via-per-execution-shim.md)）
- `internal/shim/dir.go` — per-execution 目录管理（envelope.json / status.json / events.jsonl / agent.log / stderr.log / shim.sock；temp+rename 原子写）
- `internal/shim/fencing.go` — shim_token + PID start_time 校验（macOS `ps -o lstart=` / Linux `/proc/<pid>/stat`）
- `internal/shim/kill.go` — Kill RPC：SIGTERM → 5s grace → SIGKILL（grace 走 config `execution.kill_grace_seconds`）

**实现步骤**：

1. shim 子命令 entry：解析 `--execution-id` / `--shim-token` / `--cmd` / `--` 后 args
2. `setsid` 脱离 daemon process group（Linux/macOS via `syscall.SysProcAttr{Setsid: true}`）
3. 写 envelope.json（temp+rename）→ fork+exec agent CLI（stdout/stderr 重定向到本机 agent.log / stderr.log）
4. 开本地 unix socket `shim.sock`（agent 通过该 socket 调 `request-input` / `report-progress` 等 RPC；shim 转发给 daemon）
5. 连 daemon `daemon.sock` 发 ShimHello（带 shim_token / shim_pid / shim_start_time / agent_pid / agent_start_time）
6. 监听 daemon RPC：Kill / Reconnect / Catchup
7. agent stdout 读一行 → Adapter.ParseEvent → append events.jsonl（O_APPEND，seq 递增）→ 发 daemon
8. agent exit → status.json.phase=done + exit_code → 发 ShimGoodbye → 等 GoodbyeAck → exit
9. Reconnect：daemon 重启后 ShimHello 重发 last_acked_seq；从 events.jsonl 该位置重发追平

**异常路径**：

- daemon 没起来 → shim 不阻塞 agent，继续 append events.jsonl 等 daemon 回来
- agent crash → shim wait 拿到非 0 exit code → emit failed(reason=`agent_crashed`)
- SIGTERM / SIGKILL 后 agent 退出 → emit killed(reason=透传)

**DoD**：

- [ ] shim 子命令能独立运行（`agent-center worker shim --help` 显示用法）
- [ ] per-execution 目录 5 个文件按规范写入 + 原子 rename
- [ ] daemon 重启场景 shim 能 reconnect + catchup（集成测）
- [ ] Kill 5s grace 用 mock clock 测（不真 sleep）
- [ ] fencing：daemon 假冒 PID 用 start_time 拦截（单测）
- [ ] 覆盖率 ≥ 90%

### 3.6 Worker daemon dispatch loop + Reconcile worker 端

**工件**：Worker daemon 收 DispatchEnvelope → spawn shim → ACK / NACK / 终态上报；Worker enroll / 重连后 Reconcile

**输入（依赖）**：3.3 DispatchService（worker 端的 ACK / NACK 协议契约）/ 3.4 Adapter（worker daemon 检查 envelope.agent_cli 是否可用）/ 3.5 shim 子命令（worker 通过 os/exec spawn）

**输出**：

- `internal/workerdaemon/daemon.go` — daemon main loop
- `internal/workerdaemon/dispatch_loop.go` — 收 envelope → 校验 → ACK/NACK → spawn shim
- `internal/workerdaemon/shim_supervisor.go` — 监控 active shims（kill -0 + start_time 探活）；shim 崩 → emit failed(`shim_crashed`）
- `internal/workerdaemon/reconcile.go` — enroll / 重连后调 ReconcileService.Handle；处理 active/stale/unknown
- `internal/workerdaemon/env_inject.go` — daemon → shim env 注入（AGENT_CENTER_EXECUTION_ID / TASK_ID / PROJECT_ID / CONVERSATION_ID / WORKSPACE_MODE / CWD / PRIORITY / ETA_AT / SHIM_TOKEN）
- `internal/workerdaemon/workspace.go` — workspace 物理创建（worktree mode: `git worktree add -b task/<execution_id>`；direct mode: 用 base_path）+ 24h GC
- `internal/workerdaemon/prompt_assembly.go` — worker-agent.md skill + constraints_extra + task_description 拼装（[agent-harness/01-prompt-assembly](../design/architecture/tactical/agent-harness/01-prompt-assembly.md)）

**实现步骤**：

1. daemon main loop：注册到 center（enroll/重连）→ Reconcile → 接 envelope
2. 收 envelope：
   - 查 `exec/<execution_id>/` 幂等（[02-task-execution § 9.2](../design/architecture/tactical/task-runtime/02-task-execution.md) 11 步时序）
   - 校验：agent_cli 是否支持 / WorkerProjectMapping 是否存在 / base_branch 是否在本地 repo
   - 失败 → NACK (reason)；通过 → ACK
3. workspace 准备：worktree mode `git worktree add` / direct mode no-op → emit `task_execution.working` + 可选 `worktree.created`
4. 拼 prompt（read worker-agent.md skill + envelope.task_description + constraints_extra）
5. 生成 shim_token nonce → spawn shim 子命令（detached）
6. 等 ShimHello（60s timeout，[config execution.shim_hello_timeout_seconds](../design/implementation/04-configuration.md)）→ failed(`shim_no_hello`)
7. 后续 shim 主动推 PhaseChanged / Event → daemon 转 center；RPCForward（request-input 等）→ daemon 转 center
8. Kill 流程：center 下发 kill_requested → daemon 调 shim.Kill RPC
9. 24h GC：扫 `exec/<id>/` 修改时间 > 24h → 整目录删 + 配套 worktree `git worktree remove` + emit `worktree.released`

**DoD**：

- [ ] daemon 能完整跑 Phase 1 已有的 enroll/heartbeat + 本 phase 的 dispatch + reconcile
- [ ] 11 步 dispatch 时序在集成测全覆盖（含幂等重发）
- [ ] 异常路径：shim 60s no-hello / shim crashed / agent crashed / kill 5s grace 全集成测
- [ ] worktree 模式与 direct 模式 cwd 切换正确（集成测在真实 git repo 上）
- [ ] 24h GC 用 mock clock 单测
- [ ] 覆盖率 ≥ 90%

### 3.7 CLI handlers（task * / dispatch / kill-execution / request-input / report-*）

**工件**：[03-cli § 8.1](../design/implementation/03-cli-subcommands.md) 列出的 11 条命令

**输入（依赖）**：3.3 Domain Services（CLI handler 是 application service，调 domain service）/ 3.6 Worker daemon（agent CLI handler 通过 worker daemon RPC 中转）

**输出**：

- `cmd/agent-center/task/create.go` — `task create`（含 `--no-conversation` flag；a/e 路径同事务建 Conversation；[03-cli § 9.1](../design/implementation/03-cli-subcommands.md)）
- `cmd/agent-center/task/bind_conversation.go` — `task bind-conversation` 两种模式 `--auto` / `--to=<conv_id>`
- `cmd/agent-center/task/unbind_conversation.go` — 接口预留（return error code 20=`not_implemented_v1`；保留 surface 给 v2）
- `cmd/agent-center/dispatch.go` — `dispatch <task_id> --worker=<worker_id>`
- `cmd/agent-center/kill_execution.go` — `kill-execution <execution_id> --reason --message`
- `cmd/agent-center/request_input.go` — `request-input <execution_id> --reason --message [--options]`（agent 阻塞调用；worker daemon socket transport）
- `cmd/agent-center/report_progress.go` — 写 Conversation Message（content_kind=agent_finding）+ 无 input_request_ref；本 phase 不写 projection（留 Phase 4）
- `cmd/agent-center/report_artifact.go` — append Artifact 行 + emit `artifact.uploaded`
- `cmd/agent-center/report_failure.go` — execution → failed(reason=`agent_reported_failure`)
- `cmd/agent-center/read_task_context.go` — 返回 task + project + parent + deps 元数据 + 最近 N 条 Message（JSON 输出）
- `cmd/agent-center/worker/shim.go` — 已在 3.5 列出

**实现步骤**：

1. 每个 handler 走"解析 flag → 决定 transport（admin socket / worker daemon socket / env-driven autoadaptive，[agent-harness/02-skill-cli-tooling](../design/architecture/tactical/agent-harness/02-skill-cli-tooling.md)）→ 调 application service → 输出 JSON"
2. 错误 → 非 0 exit code + JSON `{error_code, reason, message}`（exit code 表见 [03-cli § 9](../design/implementation/03-cli-subcommands.md)）
3. agent 视角 CLI（request-input / report-progress / report-artifact / report-failure / read-task-context）—— 通过 env `AGENT_CENTER_WORKER_SOCK` 找 shim.sock → shim 转 daemon → daemon 转 center
4. user / supervisor 视角 CLI（task * / dispatch / kill-execution）—— 通过 `--admin-socket` flag 或默认 `/run/agent-center/admin.sock`
5. Skill 文件同步：本 phase 涉及的 agent CLI 命令都写进 `assets/skills/worker-agent.md`；user / supervisor 视角命令进 `assets/skills/supervisor.md`（[agent-harness/02-skill-cli-tooling](../design/architecture/tactical/agent-harness/02-skill-cli-tooling.md)）

**DoD**：

- [ ] 11 条命令 `--help` 输出与 [03-cli § 8.1](../design/implementation/03-cli-subcommands.md) 一致
- [ ] 每条命令至少 1 个集成测（用 `:memory:` SQLite + fake worker daemon）
- [ ] request-input 阻塞行为：单测用 fake daemon + 响应注入；不真 sleep
- [ ] unbind-conversation 返回固定 not_implemented 错误（单测断言）
- [ ] skill 文档同步（CI 校验命令清单 ⊆ skill 提及）
- [ ] 覆盖率 ≥ 90%

### 3.8 端到端验证（task create → dispatch → shim spawn → agent run → 完成）

**工件**：e2e harness + 关键路径用例

**输入（依赖）**：3.1 ~ 3.7 全部完成

**输出**：

- `tests/e2e/taskruntime/happy_path_test.go` — `task create → dispatch → worker接 envelope → shim spawn → mock agent CLI run → completed → events 表全链路验证`
- `tests/e2e/taskruntime/kill_test.go` — `dispatch → working → kill-execution → SIGTERM → 5s grace → SIGKILL → killed`
- `tests/e2e/taskruntime/input_request_test.go` — `dispatch → working → agent 调 request-input → input_required → respond → working → completed`
- `tests/e2e/taskruntime/timeout_test.go` — submitted_timeout / execution_timeout / input_timeout / worker_lost 4 类
- `tests/e2e/taskruntime/dispatch_failure_test.go` — dispatch_no_ack / dispatch_nack:* / shim_no_hello / shim_crashed
- `tests/e2e/taskruntime/reconcile_test.go` — worker 重连 reconcile 三分组
- `testdata/mock-agents/` — 6+ 个 shell 脚本模拟不同 agent 行为（happy / fail / crash / blocks-on-input / long-running / unknown-event）
- `tests/e2e/harness/` — 启动 in-process center + worker daemon + shim spawn（真 fork exec mock-agent shell）的 harness

**实现步骤**：

1. e2e harness：单进程内启 center server + worker daemon（共 SQLite `:memory:`；真实 unix socket）；shim 真 fork+exec mock-agent.sh
2. 每个 e2e 用例：通过 CLI binary 调命令 → 断言事件 / 状态 / Artifact / Conversation Message 完整出现
3. 时间穿越用 clock.Clock 注入；no sleep
4. mock agent 通过 stdout 输出 JSONL 模拟 thinking / tool_call / tool_result / end_turn / unknown
5. 文档化 testdata/e2e/ 目录布局 + 各场景预期事件 trace（[README § 2.3](README.md) 关键路径要求）

**DoD**：

- [ ] § 5.3 列出的 全部 e2e 场景都有用例
- [ ] e2e 不真 sleep / 不真连外部网络
- [ ] 跑 `make test-e2e` 在 ≤ 5min 内全过
- [ ] 测试报告归档 `docs/plans/reports/phase-2-test-report.md`

---

## § 4. Definition of Done（整体）

- [ ] § 1 所有工件实现并通过单元测试（4 AR / 4 Repository / 9 VO / 5 Domain Service [其中 IssueConcludeSpawn stub] / 11 CLI handler / Worker daemon / Shim / Adapter）
- [ ] § 5 所有测试场景通过（unit + 集成 + e2e）
- [ ] **单测行覆盖率 ≥ 90%**（diff + 整体；`go test -cover ./internal/taskruntime/... ./internal/agentadapter/... ./internal/shim/... ./internal/workerdaemon/...`）
- [ ] 测试报告归档：`docs/plans/reports/phase-2-test-report.md`
- [ ] 触发的 domain event 全部进 `events` 表（[§ 1.7](#17-domain-eventsemit-进-events-表) 全清单；集成测验证）
- [ ] CLI 命令 `--help` 与 [03-cli § 8.1](../design/implementation/03-cli-subcommands.md) 对齐
- [ ] migration `0002_taskruntime.up.sql` 在 SQLite `:memory:` + 本地文件双跑
- [ ] `go vet ./... && go build ./... && go test ./...` 全绿
- [ ] skill 文档 `assets/skills/worker-agent.md` 同步本 phase 新增 agent CLI 命令
- [ ] § 7 风险项要么处理要么显式 defer

---

## § 5. 测试计划

### 5.1 单测场景（按工件分类）

| # | 工件 | 测试场景 | 关键断言 |
|---|---|---|---|
| U-1 | Task | `open → suspended` happy path | 状态机方法 emit `task.suspended`；version+1 |
| U-2 | Task | `open → abandoned` 含 reason+message | abandoned 字段填；reason 非空校验 |
| U-3 | Task | `suspended → open` resume | 状态变 + emit `task.resumed` |
| U-4 | Task | 终态再操作（`done → suspended`） | 拒绝；返回 `ErrTaskInvalidTransition` |
| U-5 | Task | parent_task_id / from_issue_id 创建后改 | 拒绝（Invariants） |
| U-6 | Task | conversation_id null→非null | 允许；非null→null 拒绝（v1 不 unbind） |
| U-7 | Task | depends_on：self-dep / 环 / 不存在 / abandoned | 全拒绝 |
| U-8 | Task | depends_on：active execution 期间改 | 拒绝（Invariants） |
| U-9 | TaskExecution | `submitted → working` happy | dispatch_state=acked、状态机迁移 |
| U-10 | TaskExecution | `working → completed`（reason+message） | completed_reason/message 写入；终态不可改 |
| U-11 | TaskExecution | `working → failed` 11 种 failed reason 各一用例 | reason 枚举 Validate 通过；emit event |
| U-12 | TaskExecution | `working → killed` 经 kill_requested | cancel_requested_at 写；killed reason 填 |
| U-13 | TaskExecution | `* → killed` 含 submitted 直接 killed（不 SIGTERM） | submitted 直接转 killed |
| U-14 | TaskExecution | CAS 冲突（kill vs complete 竞态） | 后者返回 `ErrTaskExecutionVersionConflict` |
| U-15 | TaskExecution | 终态再 update | 拒绝；`ErrTaskExecutionAlreadyTerminated` |
| U-16 | InputRequest | `pending → responded`（三响应路径 mock） | response 字段填；execution → working 联动 |
| U-17 | InputRequest | `pending → timed_out` T2=24h | ended_reason=`input_timeout`；execution → failed |
| U-18 | InputRequest | `pending → canceled`（kill precondition 联动） | 同 tx execution → killed |
| U-19 | Artifact | append-only：UPDATE 拒绝 | 返回 `ErrArtifactImmutable` |
| U-20 | Artifact | append concurrently | 无冲突；append seq 单调 |
| U-21 | DispatchEnvelope VO | JSON round-trip | marshal/unmarshal 等值 |
| U-22 | DispatchEnvelope VO | envelope_version="v1" 强制 | 字段缺失拒绝 |
| U-23 | Reason 枚举 | 全枚举值 Validate 通过 + 未知字符串拒绝 | `ErrUnknownReason` |
| U-24 | WorkspaceMode | Parse "worktree"/"direct" + 未知拒绝 | 同上 |
| U-25 | DispatchService | happy（单活校验 + tx 同事务） | task.current_execution_id 写、emit submitted/dispatched |
| U-26 | DispatchService | 单活违反 | `ErrSingleActiveViolation`；不创建 execution |
| U-27 | DispatchService | max_executions_per_task 超 | emit `task.dispatch_limit_reached`；拒绝 |
| U-28 | DispatchService | NACK 6 种 sub_reason 各一用例 | execution → failed(reason=`dispatch_nack:<sub>`) |
| U-29 | DispatchService | 30s no-ACK | mock clock 推 30s → execution → failed(`dispatch_no_ack`) |
| U-30 | ReconcileService | active 三分组 | 三 slice 内容正确 |
| U-31 | ReconcileService | unknown execution_id | 进 unknown 列；不改 center 状态 |
| U-32 | TimeoutScanner | submitted_timeout 5min | mock clock 推 5min → execution → failed |
| U-33 | TimeoutScanner | execution_timeout 6h（不计 input_required） | working_seconds_accumulated 累计正确；input_required 时段不计 |
| U-34 | TimeoutScanner | input_request T1=4h ping | emit supervisor ping（[ADR-0013](../design/decisions/0013-supervisor-invocation-concurrency.md) 留 Phase 6） |
| U-35 | TimeoutScanner | input_request T2=24h fail | IR → timed_out + execution → failed |
| U-36 | TimeoutScanner | worker_offline 事件触发 worker_lost | execution → failed(`worker_lost`) |
| U-37 | KillCoordinator | user_request / supervisor_request kill | 两阶段：kill_requested → killed |
| U-38 | KillCoordinator | abandon_precondition / suspend_precondition | killed 后 task 联动 abandoned/suspended |
| U-39 | KillCoordinator | timeout_kill 路径 | TimeoutScanner 调入 KillCoordinator |
| U-40 | KillCoordinator | kill 已终态 | 拒绝；幂等 no-op |
| U-41 | KillCoordinator | 重复 kill 在 grace 期 | `cancel_requested_at` 已存在 → no-op |
| U-42 | IssueConcludeSpawn stub | batch=N + 全 dep 既有 uuid | all-or-nothing 成功 |
| U-43 | IssueConcludeSpawn stub | batch 内 local_id 引用 | resolve 后存 dep；不存在 local_id 拒绝 |
| U-44 | IssueConcludeSpawn stub | dep 图有环 | 拒绝 + 整事务回滚 |
| U-45 | Adapter | claude-code BuildCommand args 顺序 | args = `[--output-format stream-json --session-id <id> -p <prompt>]` |
| U-46 | Adapter | claude-code ParseEvent 5 类 + unknown | 5 类映射对；unknown 走 Raw |
| U-47 | Adapter | EventUnknown 去重（同 type 仅 emit 一次） | EventSink 收到 1 条 `agent_adapter.unknown_event_seen` |
| U-48 | Adapter | per-execution 阈值 N 条 → warning | emit `task_execution.warning(reason=adapter_significantly_behind)` |
| U-49 | Adapter | codex/opencode skeleton 拒绝调用 | `ErrNotImplemented` |
| U-50 | Adapter | jsonl_parse_error 累计阈值 | emit failed(`jsonl_parse_error`) |
| U-51 | Shim | per-execution 目录 5 文件原子 rename | 写过程中 status.json 不可见半写态 |
| U-52 | Shim | shim_token 校验失败 | daemon 拒绝连接 |
| U-53 | Shim | PID start_time 校验（PID 复用） | 不匹配 → 拒绝 |
| U-54 | Shim | Kill：SIGTERM → 5s grace → SIGKILL | mock process + clock；不真 sleep |
| U-55 | Shim | Reconnect after daemon restart | last_acked_seq 续传 |
| U-56 | Shim | ShimGoodbye 后 24h ACK timeout | mock clock 推 24h → shim fence-and-forget |
| U-57 | Worker daemon | dispatch 11 步幂等（重复 envelope） | 第二次 envelope 收到 → 重发 ACK 不重 spawn |
| U-58 | Worker daemon | NACK 6 种 reason 路径 | envelope 校验失败 → 对应 reason NACK |
| U-59 | Worker daemon | shim_no_hello 60s | mock clock → emit failed(`shim_no_hello`) |
| U-60 | Worker daemon | shim_crashed 探活 | kill -0 失败 + start_time 不匹配 → emit failed(`shim_crashed`） |
| U-61 | Worker daemon | worktree mode `git worktree add` | 真 git repo 单测；branch=`task/<exec_id>` |
| U-62 | Worker daemon | direct mode cwd=base_path | cwd 字段正确 |
| U-63 | Worker daemon | 24h GC | mock clock → 目录 + worktree 一起删 + emit `worktree.released` |
| U-64 | Worker daemon | env 注入完整性（10 个 env） | 10 个 env 全 set；SHIM_TOKEN 仅 daemon→shim |
| U-65 | Worker daemon | Reconcile worker 端：stale → SIGTERM 本地 + audit emit | local agent SIGTERM；emit killed(reason=reconcile_stale)；center 不改 |
| U-66 | CLI: task create | a/e 路径同事务建 Conversation | conversation_id 写入；emit conversation.opened + task.created |
| U-67 | CLI: task create | --no-conversation 路径 | conversation_id=null；不建 Conversation |
| U-68 | CLI: task bind-conversation | --auto 建新 conversation 到 default_channel | conversation_id 写、emit conversation.opened |
| U-69 | CLI: task bind-conversation | --to=<conv_id> 既有 | conversation_id 写既有；不重建 |
| U-70 | CLI: task unbind-conversation | 拒绝 | exit code 20 `not_implemented_v1` |
| U-71 | CLI: dispatch | happy | 走 DispatchService.Dispatch |
| U-72 | CLI: kill-execution | reason+message 必填 | 缺失拒绝 |
| U-73 | CLI: request-input | 阻塞 + 响应返回 | fake daemon 注入响应；stdout JSON 含 answer |
| U-74 | CLI: request-input | conversation_id=null + default_channel 配置 | 自动 bind；走 fallback |
| U-75 | CLI: request-input | conversation_id=null + default_channel 未配 | 整事务回滚；execution → failed(`no_input_channel`） |
| U-76 | CLI: report-progress | 写 Message(agent_finding) 到 task.conversation_id | Message append + emit conversation.message_added |
| U-77 | CLI: report-progress | task.conversation_id=null | 跳过；不 fallback（只 IR 才 fallback） |
| U-78 | CLI: report-artifact | append Artifact + emit `artifact.uploaded` | 表行新增；event 进 events 表 |
| U-79 | CLI: report-failure | execution → failed(`agent_reported_failure`) | 状态机 + emit |
| U-80 | CLI: read-task-context | 返回完整上下文 JSON | task + project + parent + deps + 最近 N Message |

### 5.2 集成测试场景

| # | 场景 | 涉及工件 | 关键断言 |
|---|---|---|---|
| I-1 | Repository ↔ 真实 SQLite + migration | 4 Repository + migration | up/down 跑通；CAS 真正阻断并发 |
| I-2 | 跨聚合 tx：task + conversation 同事务建 | TaskRepository + ConversationRepository（Phase 1 提供）+ WithTx | 失败一方 tx 整体回滚 |
| I-3 | 跨聚合 tx：input_request + task_execution + conversation message | InputRequestRepository + TaskExecutionRepository + ConversationRepository + EventSink | 4 行 INSERT/UPDATE 同 tx；其中一行失败全回滚 |
| I-4 | 跨聚合 tx：execution + artifact append | ArtifactRepository + TaskExecutionRepository | append + execution updated_at 同 tx |
| I-5 | DispatchService 全路径（happy + 单活 + max_executions + NACK + 30s no-ACK） | DispatchService + EventSink + 真 SQLite | events 表行数正确 |
| I-6 | TimeoutScanner 4 类 + worker_offline 订阅 | TimeoutScanner + 真 SQLite + EventSink + clock mock | 推时后所有该 fail 的 execution 都 fail |
| I-7 | KillCoordinator 两阶段 + 6 种 reason | KillCoordinator + EventSink + worker daemon stub | kill_requested → killed 链路完整 |
| I-8 | Adapter EventUnknown 链路 | Adapter + UnknownEventReporter + EventSink + 真 SQLite | events 表收到 `agent_adapter.unknown_event_seen`；去重生效 |
| I-9 | Shim ↔ daemon reconnect + catchup | Shim + daemon RPC + per-execution dir | last_acked_seq 续传正确 |
| I-10 | Worker daemon dispatch 11 步幂等 | Worker daemon + Shim + Adapter + DispatchService | 幂等场景 ACK 重发 / shim 不重 spawn |
| I-11 | Worker daemon 24h GC + worktree.released | Worker daemon + clock mock | per-exec dir 删 + worktree remove + emit |
| I-12 | ReconcileService active/stale/unknown 三分组 | ReconcileService + 真 SQLite + worker stub | 三 slice 准确 |
| I-13 | Task ↔ Conversation 1:1 a 路径 | TaskService.Create + ConversationService.Create | task.conversation_id 写、emit 两条 event |

### 5.3 e2e 测试场景

| # | 场景 | 用户视角 / 入口 CLI | 关键断言 |
|---|---|---|---|
| E-1 | Happy path 全链路 | `task create` → `dispatch` → mock-agent.sh thinking+tool_call+end_turn → completed | events 表完整：task.created / task_execution.submitted / acked / working / completed；artifact 可选；Conversation Message 多条 |
| E-2 | User 主动 kill | E-1 中跑到 working → `kill-execution` → 等 killed | SIGTERM → 5s grace → SIGKILL；events 表 kill_requested + killed；reason=user_request |
| E-3 | Agent 调 request-input → user respond | mock-agent.sh blocks-on-input → `respond-to-input-request` → continue | execution: working → input_required → working → completed；Message(agent_finding,input_request_ref) 写到 conversation |
| E-4 | InputRequest T2=24h 超时 | E-3 但不 respond，mock clock 推 24h | IR → timed_out；execution → failed(`input_timeout`） |
| E-5 | submitted_timeout 5min | dispatch 后 worker 不 ACK，mock clock 推 5min | execution → failed(`submitted_timeout`） |
| E-6 | execution_timeout 6h | mock-agent 长跑，mock clock 推 6h | execution → failed(`execution_timeout`） via KillCoordinator |
| E-7 | dispatch_no_ack 30s | worker daemon 故意不 ACK，mock clock 推 30s | execution → failed(`dispatch_no_ack`） |
| E-8 | DispatchNack 各 sub_reason（worker_at_capacity / mapping_missing / agent_cli_unsupported / worktree_path_busy / base_branch_missing / envelope_version_unsupported） | worker daemon 注入 NACK | execution → failed(`dispatch_nack:<sub>`) |
| E-9 | shim_no_hello 60s | daemon spawn shim 但 shim 不发 ShimHello，mock clock 推 60s | execution → failed(`shim_no_hello`） + shim PID kill |
| E-10 | shim_crashed | daemon spawn shim → kill shim 进程 → daemon 探活 | execution → failed(`shim_crashed`） + agent SIGTERM |
| E-11 | agent_crashed (非 0 exit) | mock-agent.sh exit 17 | execution → failed(`agent_exit_nonzero`） |
| E-12 | agent_reported_failure | mock-agent.sh 调 `report-failure` | execution → failed(`agent_reported_failure`） |
| E-13 | Worker reconnect + reconcile（stale） | worker daemon 杀掉重启，center 已标 stale | worker 收 ReconcileResponse.stale → SIGTERM 本地 agent + audit emit killed(`reconcile_stale`) |
| E-14 | Worker_lost 触发 worker_offline → execution failed | worker 心跳停 > 60s | worker.offline 事件订阅 → execution → failed(`worker_lost`） |
| E-15 | Daemon 重启不打断 agent | daemon kill -9 → 重启 → shim 重连 catchup | agent 全程不死；events 表 seq 续传 |
| E-16 | abandon-task 触发 abandon_precondition kill | execution working 中 `abandon-task` | execution → killed(`abandon_precondition`） → task → abandoned |
| E-17 | suspend-task 触发 suspend_precondition kill | execution working 中 `suspend-task` | execution → killed(`suspend_precondition`） → task → suspended |
| E-18 | unknown agent event 链路 | mock-agent.sh 输出 `{"type":"future_thing"}` | events 表收到 `agent_adapter.unknown_event_seen` |
| E-19 | 工作流：task create → bind-conversation --auto → request-input fallback | b 路径 task + 配 default_channel | 自动建 Conversation + IR 写入 + Message 写入 |
| E-20 | IR fallback 失败：default_channel 未配 | b 路径 task + 不配 default_channel | IR 创建失败、execution → failed(`no_input_channel`） |
| E-21 | report-artifact happy | agent 调 report-artifact --kind=pr_url | Artifact 行 + emit artifact.uploaded；inspect task 能看 |
| E-22 | dispatch_limit_reached | 同 task 重派 3 次失败后再派 | emit task.dispatch_limit_reached；拒绝第 4 次 |

### 5.4 异常路径覆盖矩阵

| 异常 | 单测 | 集成 | e2e |
|---|---|---|---|
| Repository CAS 冲突 | U-14 | I-1 | - |
| 状态机非法跃迁 | U-4 / U-15 | - | - |
| 跨聚合 tx 回滚 | - | I-2/3/4 | E-20 |
| dispatch_no_ack | U-29 | I-5 | E-7 |
| dispatch_nack:* (6 sub) | U-28 | I-5 | E-8 |
| submitted_timeout | U-32 | I-6 | E-5 |
| execution_timeout | U-33 | I-6 | E-6 |
| input_timeout | U-35 | I-6 | E-4 |
| worker_lost | U-36 | I-6 | E-14 |
| shim_no_hello | U-59 | I-10 | E-9 |
| shim_crashed | U-60 | I-10 | E-10 |
| agent_exit_nonzero / agent_crashed | - | - | E-11 |
| agent_reported_failure | U-79 | - | E-12 |
| no_input_channel | U-75 | - | E-20 |
| reconcile stale/unknown | U-30/31, U-65 | I-12 | E-13 |
| daemon 升级不打断 agent | U-55 | I-9 | E-15 |
| dispatch_limit_reached | U-27 | I-5 | E-22 |
| unknown agent event | U-47/48 | I-8 | E-18 |
| abandon/suspend precondition | U-38 | I-7 | E-16/17 |
| kill 已终态 / 重复 kill / kill 在 grace 期 | U-40/41 | - | - |

---

## § 6. 风险 / Spike 项

### 6.1 Spike：claude-code stream-json 实际输出 schema

**风险**：[05-agent-adapters § 8.1](../design/implementation/05-agent-adapters.md) 列的 claude code JSONL 字段是设计假设，需用真实 binary 校验。

**缓解**：

- 落代码前先 spike：在隔离环境跑一次 `claude --output-format stream-json --session-id=x -p "hello"`，记录真实输出 → 写进 `testdata/agent-cli/claude-code/sample.jsonl`
- ParseEvent 与该 sample 对位
- 若字段名 / 嵌套结构与设计不同 → 调整 adapter 而**非**改 AgentTraceEvent schema（保 schema 稳定）

**Spike 出口**：sample.jsonl 入库 + adapter 测试通过即结束。

### 6.2 Spike：setsid 跨平台行为

**风险**：[ADR-0018 § 2](../design/decisions/0018-detached-agent-via-per-execution-shim.md) 仰赖 `syscall.SysProcAttr{Setsid: true}` 让 shim 脱离 daemon process group。macOS / Linux 行为是否一致 / kill daemon process group 不波及 shim？

**缓解**：

- 落 shim 代码前先在两平台各跑一次：daemon spawn shim → kill -9 daemon → 验证 shim + agent 仍活
- 若 macOS 行为不同 → 用 `process group` + `Setpgid` 显式分离

**Spike 出口**：两平台手动验证脚本 `tests/e2e/scripts/setsid-isolation.sh` 入库。

### 6.3 Spike：PID start_time 跨平台读取

**风险**：[ADR-0018 § 6](../design/decisions/0018-detached-agent-via-per-execution-shim.md) 用 PID start_time 防 PID 复用。macOS `ps -o lstart=` 输出格式与 Linux `/proc/<pid>/stat` 第 22 字段不同。

**缓解**：在 `internal/shim/fencing.go` 写 platform adapter（`fencing_darwin.go` / `fencing_linux.go`）；接口统一 `GetProcessStartTime(pid int) (time.Time, error)`。

### 6.4 SQLite single-writer 与 worker daemon 并发

**风险**：v1 SQLite single-writer，高并发 dispatch / event append 可能锁竞争。

**缓解**：

- v1 单 center 单 worker 场景下并发度低（≤ 几个 active execution），可接受
- 真撞性能问题 → 走 ADR 升 PG（[conventions § 9.2](../rules/conventions.md)），非本 phase 范围

**推迟到**：[roadmap](../design/roadmap.md) 性能压测后决定。

### 6.5 IssueConcludeSpawn stub 与 Phase 3 接入

**风险**：本 phase 仅写接口 + 单测 mock 调；Phase 3 由 Discussion BC 接入时可能发现 spec 字段不全。

**缓解**：

- 接口签名严格遵守 [00-overview § 3.4](../design/architecture/tactical/task-runtime/00-overview.md)：`Spawn(ctx, spec IssueConcludeSpec) ([]TaskID, error)`
- IssueConcludeSpec 字段表（tasks[] + closing_comment + resolution + ...）按 [01-task § 9](../design/architecture/tactical/task-runtime/01-task.md) IssueConcludeSpawn caller 描述抓全
- Phase 3 接入时若需扩字段 → 走 additive 扩列（[README § 2.1](README.md) 不破坏 Phase 2 接口语义）

### 6.6 worker daemon ↔ shim ↔ center 协议演进

**风险**：本 phase 定 ShimHello / PhaseChanged / Event / RPCForward 等 RPC schema；后续 Phase 改协议会破坏向后兼容。

**缓解**：

- RPC schema 加 `protocol_version` 字段（v1=1）
- daemon / shim 不同 version 时拒绝连接 → 降级路径走 dispatch fail / reconcile fail，让 supervisor 重派
- 推荐 Phase 7 部署收尾时 review 协议演进规则

---

## § 7. 下游解锁

本 phase 完成后，下游 phase 可依赖以下接口 surface：

| 下游 Phase | 依赖本 phase 哪些工件 |
|---|---|
| **Phase 3 Discussion** | IssueConcludeSpawn Domain Service（本 phase stub，Phase 3 实装真 caller）；TaskFactory（Issue concluded 创建 task 入口）；`open-issue` CLI 由 Phase 3 自己写但走相同 Conversation 模式 |
| **Phase 4 Observability 投影** | `task_execution_projections` 表（本 phase 不写，Phase 4 落 schema）；本 phase 已 emit 的 task.* / task_execution.* / input_request.* events 是 Phase 4 投影的输入 |
| **Phase 5 Bridge Outbound（飞书）** | `input_request.requested` / `conversation.message_added`（含 input_request_ref） / `task_execution.*` 事件订阅 → 飞书卡片渲染 |
| **Phase 6 Cognition Supervisor** | DispatchService / KillCoordinator（supervisor 决策入口）；`task.created` / `task_execution.failed` / `input_request.requested` events 唤醒 supervisor |
| **Phase 7 Bridge Inbound + 部署** | `task bind-conversation` / `InputRequest.Respond` API 是 inbound 翻译目标 |

**接口冻结**：按 [README § 2.1](README.md)，Phase 2 完成后所有 Repository 接口 / Domain Service 接口 / VO schema / CLI 命令 surface 冻结；下游 phase 只能扩列，不能改语义。

---

## § 8. References

### 必读架构文档

- [task-runtime/00-overview](../design/architecture/tactical/task-runtime/00-overview.md) — BC wrap（聚合清单 / Domain Service / Factory / Repository / 跨聚合）
- [task-runtime/01-task](../design/architecture/tactical/task-runtime/01-task.md) — Task 聚合
- [task-runtime/02-task-execution](../design/architecture/tactical/task-runtime/02-task-execution.md) — TaskExecution + worker 运行时 + Artifact
- [task-runtime/03-input-request](../design/architecture/tactical/task-runtime/03-input-request.md) — InputRequest 聚合
- [agent-harness/01-prompt-assembly](../design/architecture/tactical/agent-harness/01-prompt-assembly.md)
- [agent-harness/02-skill-cli-tooling](../design/architecture/tactical/agent-harness/02-skill-cli-tooling.md)

### 必读 ADR

- [ADR-0010 两层模型](../design/decisions/0010-task-execution-two-layer-model.md)
- [ADR-0011 派单可靠性协议](../design/decisions/0011-dispatch-reliability-protocol.md)
- [ADR-0017 Task ↔ Conversation 1:1](../design/decisions/0017-task-as-conversation.md)
- [ADR-0018 Per-execution shim 模型](../design/decisions/0018-detached-agent-via-per-execution-shim.md)
- [ADR-0019 BC 合并](../design/decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md)

### 必读实现层

- [implementation/02-persistence-schema § 8.1](../design/implementation/02-persistence-schema.md) — TaskRuntime DDL
- [implementation/03-cli-subcommands § 8.1 / § 9.1-9.2](../design/implementation/03-cli-subcommands.md) — CLI 签名
- [implementation/04-configuration § 7.6](../design/implementation/04-configuration.md) — `execution.*` 配置项
- [implementation/05-agent-adapters](../design/implementation/05-agent-adapters.md) — Adapter 全文

### 规约 / 蓝图

- [conventions § 0 / § 1 / § 2 / § 9.z / § 14 / § 16 / § 17](../rules/conventions.md)
- [testing](../rules/testing.md)
- [ddd-blueprint](../design/ddd-blueprint.md)
- [plans/README](README.md) — § 2 纪律 / § 3 模板 / § 4 测试报告
