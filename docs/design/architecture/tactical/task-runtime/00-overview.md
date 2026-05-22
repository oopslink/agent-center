# TaskRuntime BC — DDD 战术设计 Overview

> **DDD 战术层** · BC: TaskRuntime
>
> 任务全生命周期：从 Issue spawn / 用户发起 → dispatch → 实际执行（worker 侧运行时）→ 终结。
>
> 本 BC 合并自原 BC1 Scheduling + BC4 Execution（[ADR-0019](../../../decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md)）。物理 split（状态权威在 center / 物理执行在 worker）≠ 概念 split —— 同一 BC 内分布式实现。

---

## § 0. BC 一览

### 0.1 职责

| 维度 | 内容 |
|---|---|
| **聚合管理** | Task（4 态状态机）/ TaskExecution（6 态状态机，独立 AR）/ InputRequest（独立 AR）/ Artifact（TaskExecution 子实体） |
| **派单协议** | DispatchEnvelope / ACK / NACK / 30s no-ack timeout / 单活校验 / retry = 新派 |
| **运行时实施** | Worker daemon 运行时、shim 模型、Workspace 物理创建、Agent CLI 子进程、JSONL 解析、per-execution 目录 |
| **生命周期协调** | Kill 两阶段、Abandon / Suspend / Resume、Reconcile worker 端 |
| **观测协议** | 状态机 + reason+message 双字段 + Artifact 收集 |

### 0.2 UL 切片

直接来自 [strategic/03-bounded-contexts § 1.1](../../strategic/03-bounded-contexts.md) 标 TaskRuntime 上下文的术语：`Task` / `TaskExecution` / `InputRequest` / `AgentInstance`（绑 dispatch；归属 Workforce BC，详见 [workforce/04-agent-instance.md](../workforce/04-agent-instance.md)） / `Worktree` / `Artifact`。详细 UL 定义见 strategic/03。

### 0.3 Context Map 位置

[strategic/03-bounded-contexts § 3](../../strategic/03-bounded-contexts.md)：

- **TaskRuntime ↔ Workforce**：Shared Kernel（Task / TaskExecution 引用 worker_id / project_id）
- **TaskRuntime ↔ Conversation**：Shared Kernel（`task.conversation_id` 1:1 强引用 `kind=task` Conversation；同事务双写，[ADR-0017](../../../decisions/0017-task-as-conversation.md)）
- **Discussion → TaskRuntime**：Customer-Supplier（Issue conclude 批量 spawn Tasks）
- **TaskRuntime → Discussion**：Customer-Supplier（worker 上的 agent 调 `open-issue` 入 Discussion）
- **Bridge → TaskRuntime**：Customer-Supplier（inbound 时 Bridge 调 `task bind-conversation` / `InputRequest.respond`）
- **Cognition → TaskRuntime**："User" via tools（Supervisor 调 `dispatch` / `kill-execution` / etc.）
- **Observability ← TaskRuntime**：Open Host（所有 task.* / task_execution.* / input_request.* / worktree.* / artifact.* 事件订阅）

---

## § 1. 聚合清单（X.1）

### 1.1 Aggregate Roots

| 聚合 | 文件 | 状态机 | 身份 / 不变性 |
|---|---|---|---|
| **Task** | [01-task.md](01-task.md) | 4 态（open / suspended / done / abandoned）| `id` uuid，不变；身份永远绑一个 project |
| **TaskExecution** | [02-task-execution.md](02-task-execution.md) | 6 态（submitted / working / input_required / completed / failed / killed）| `execution_id` = 主身份 + 幂等 + fencing key；不变；持 `task_id` 强引用 Task（[ADR-0019](../../../decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md)：独立 AR） |
| **InputRequest** | [03-input-request.md](03-input-request.md) | 4 态（pending / responded / timed_out / canceled）| 独立聚合；通过 `task_execution.pending_input_request_id` 关联 TaskExecution |

### 1.2 Entity（子从属）

| 实体 | 从属 | 位置 |
|---|---|---|
| **Artifact** | TaskExecution（独立表 `artifacts`，归属 execution；append-only） | [02-task-execution § 12](02-task-execution.md) |

### 1.3 Value Objects（按使用聚合分组）

| VO | 用在哪 | 描述 |
|---|---|---|
| **DispatchEnvelope** | TaskExecution 创建 | Center → Worker 派单载荷；含 execution_id / task_id / worker_id / project_id / conversation_id / agent_instance_id / workspace_mode / 等字段（详见 [02-task-execution § 5](02-task-execution.md)） |
| **DispatchAck** | TaskExecution dispatch_state 转移 | Worker → Center: `{ execution_id, accepted=true, message?, acked_at }` |
| **DispatchNack** | TaskExecution dispatch_state 转移 | Worker → Center: `{ execution_id, accepted=false, reason, message, acked_at }` |
| **WorkspaceMode** | TaskExecution.workspace_mode 字段 | `worktree` \| `direct` 枚举；详见 [02-task-execution § 8](02-task-execution.md) |
| **Priority** | Task.priority + TaskExecution.priority（透传）| `high` \| `medium` \| `low` 枚举 |
| **CompletedReason+Message** | TaskExecution 终态附加 | `(reason, message)` 对，`reason` 通常 null 或 "agent reported success" |
| **FailedReason+Message** | TaskExecution 终态附加 | `(reason, message)` 对；reason ∈ {agent_exit_nonzero / agent_reported_failure / worktree_setup_failed / submitted_timeout / execution_timeout / input_timeout / worker_lost / dispatch_no_ack / dispatch_nack:* / no_input_channel / shim_no_hello / shim_crashed}；详见 [02-task-execution § 7](02-task-execution.md) |
| **KilledReason+Message** | TaskExecution 终态附加 | `(reason, message)` 对；reason ∈ {user_request / supervisor_request / abandon_precondition / suspend_precondition / reconcile_stale / reconcile_unknown / timeout_kill} |
| **IssueConcludeSpec** | IssueConcludeSpawn 入参 | 来自 Discussion BC；batch spawn 描述（含 local_id / dep 引用解析） |
| **ReconcileRequest** / **ReconcileResponse** | ReconcileService | Worker 上报 local active executions + Center 回 active/stale/unknown 三类分组 |

> **VO 共用约定**：所有 `reason + message` 对遵循 [conventions § 16](../../../../rules/conventions.md) 双字段约定。

---

## § 2. Invariants 索引（X.2）

每个聚合自己维护 invariants 节，本 § 仅做索引：

- **Task Invariants** → [01-task.md § 10](01-task.md)
- **TaskExecution Invariants** → [02-task-execution.md § 13](02-task-execution.md)
- **InputRequest Invariants** → [03-input-request.md § 9](03-input-request.md)

**跨聚合的不变量**（不属于单一聚合）：

1. **同一 task 任意时刻最多 1 条 active execution**（单活约束；应用层在 dispatch tx 内校验）
2. **执行权威在 Center**：Worker / Agent 不能直接改 task / execution 状态（仅可 emit 事件）
3. **状态机终态不可逆**：done / abandoned / completed / failed / killed 均为终态

---

## § 3. Domain Services（X.3）

### 3.1 DispatchService

**职责**：Task → TaskExecution 创建 + Envelope 发送 + ACK/NACK 处理 + 单活校验 + AgentInstance 容量校验。

| 维度 | 内容 |
|---|---|
| 入参 | `task_id`（必）+ `agent_instance_id`（必，[ADR-0024](../../../decisions/drafts/0024-agent-instance-first-class.md)）+ workspace_mode + 等；worker_id 通过 AgentInstance 拿 |
| 出参 | 新建 TaskExecution + DispatchEnvelope 已下发 |
| 协议 | Center → Worker `DispatchEnvelope`，Worker → Center `DispatchAck` 或 `DispatchNack`；30s 无 ACK → execution → `failed(reason='dispatch_no_ack')` |
| Agent 校验 | dispatch 前查 AgentInstance：`state ∈ {idle, active}` ∧ `agent_cli ∈ worker.capabilities[detected ∧ enabled]` ∧ `count(active executions) < min(worker.concurrency.per_agent_type, agent.max_concurrent ?? ∞)`；不满足 → NACK reason=`agent_unavailable / capability_missing / agent_at_capacity` |
| 单活校验 | Center dispatch 前查 task：若 `current_execution_id` 不为 null 且对应 execution 在非终态 → 拒绝（应用层校验，DB tx 内） |
| Retry 语义 | 不内置；同 task 再 dispatch = 创建新 execution_id；新 / 老 execution 走同一代码路径 |
| 跨聚合 | 写 Task（`current_execution_id` 更新）+ 写 TaskExecution（新建）+ 可能触发 AgentInstance.state idle → active（通过 AgentInstanceLifecycleService）；单事务对 Task + TaskExecution，AgentInstance state 通过事件订阅异步 |
| 兜底 | `max_executions_per_task=3`（v1 全局默认）；超限 emit `task.dispatch_limit_reached` |

详见 [02-task-execution § 5-6 DispatchEnvelope + worker 端运行时](02-task-execution.md) 与 [ADR-0011](../../../decisions/0011-dispatch-reliability-protocol.md) / [ADR-0018](../../../decisions/0018-detached-agent-via-per-execution-shim.md)。

### 3.2 ReconcileService

**职责**：Worker enroll / 重连时对账 local active executions。

| 维度 | 内容 |
|---|---|
| 入参 | `ReconcileRequest { worker_id, local_active_executions: [...] }` |
| 出参 | `ReconcileResponse { active: [...], stale: [...], unknown: [...] }` |
| 不变量 | Worker reconcile 完成前不接新派单 |
| Worker 端响应 | active → 继续上报；stale / unknown → SIGTERM 本地 agent + emit `task_execution.killed { reason=reconcile_stale \| reconcile_unknown }`（仅审计，Center 不再改状态） |
| 跨聚合 | 读 TaskExecution 列表（按 worker_id）；不改 Center 状态（worker 端的 emit 是 audit-only） |

详见 [02-task-execution § 11 reconcile worker 端](02-task-execution.md)。

### 3.3 TimeoutScanner

**职责**：Center 端独立 background ticker（30s 周期）扫所有 active executions / pending InputRequests，触发 4 类 timeout。

| timeout 类型 | 看对象 | v1 默认 | 触发后 |
|---|---|---|---|
| `submitted_timeout` | TaskExecution 在 submitted | 5 min | execution → `failed(submitted_timeout)` |
| `execution_timeout` | TaskExecution working 累计（用 `working_seconds_accumulated`） | 6h | Center emit `task_execution.kill_requested` → 走 KillCoordinator |
| `input_request_timeout` | InputRequest pending | T1=4h ping / T2=24h fail | InputRequest → `timed_out`；execution → `failed(input_timeout)` |
| `worker_heartbeat_timeout` | Worker 心跳静默 | 60s | worker → offline；上面所有 active executions → `failed(worker_lost)` |

关键：**execution_timeout 不计 input_required 时间**。Worker daemon 自上报 `working_seconds_accumulated`；input_required 期间时钟暂停。

详见 [02-task-execution.md timeout 章节](02-task-execution.md)。

### 3.4 IssueConcludeSpawn

**职责**：Discussion BC 调入；Issue conclude 时按 `IssueConcludeSpec` 批量事务性创建 N 个 Tasks（all-or-nothing），支持 batch 内 task 间引用依赖（含跨 batch 既有 uuid 和 batch 内 local_id 混用）。

| 维度 | 内容 |
|---|---|
| 入参 | `IssueConcludeSpec`（含 resolution / tasks[] / closing_comment）|
| 出参 | N 个新 Task uuid 集合 |
| 跨 BC | Discussion BC 是 caller |
| 跨聚合 | 写 N 个 Task + 1 个 IssueComment + Issue 状态更新；**all-or-nothing 单事务**；emit `issue.concluded` + `issue.tasks_spawned` |
| 校验 | resolution 合法；各 task spec 合法；depends_on 引用的既有 uuid 存在；batch 内 local_id 全部 resolve；dep 图无环 |

详见 [01-task.md § 9 创建来源](01-task.md) 与 [discussion/00-overview.md](../discussion/00-overview.md)。

### 3.5 KillCoordinator

**职责**：协调 kill / abandon-precondition-kill / suspend-precondition-kill 三个动作的两阶段决策。

**协议两阶段**：

```
1. Center emit task_execution.kill_requested { execution_id, reason, message }
   写 task_executions.cancel_requested_at = now
2. Worker 收事件 → SIGTERM agent → 5s grace → SIGKILL → 进程死后清理 → emit task_execution.killed
3. Center: execution.status=killed
4. 若是 abandon 前置 → 继续走 abandon 流程；suspend 前置 → 继续走 suspend
```

**Reason 语义化**：

| reason | 上下文 |
|---|---|
| `user_request` | 用户直接 kill-execution |
| `supervisor_request` | Supervisor 决策 kill |
| `abandon_precondition` | abandon-task 自动前置 kill |
| `suspend_precondition` | suspend-task 自动前置 kill |
| `reconcile_stale` / `reconcile_unknown` | Worker reconcile 端 audit-only emit |
| `timeout_kill` | execution_timeout 触发 |

**权限**：User / Supervisor 可 kill；Worker / Agent 不能（agent 想中止 → `report-failure` → execution → failed，不是 killed）。

**跨聚合**：可能联动 Task 状态变更（abandon/suspend 前置完成后改 Task）。

详见 [02-task-execution § 10 kill 进程级机制](02-task-execution.md)。

---

## § 4. Factories（X.4）

### 4.1 TaskFactory

**5 个 caller**（创建 Task 入口）：

| Caller | 触发 | 备注 |
|---|---|---|
| **CLI** | 用户 `agent-center task create` | conversation_id=null（懒创建） |
| **IssueConcludeSpawn**（Domain Service § 3.4）| Issue conclude with `closed_with_tasks` | batch 创建；conversation_id=null（懒创建） |
| **Supervisor** | v1 暂无（roadmap） | conversation_id=null（懒创建） |
| **Web Console** | 用户 Web UI 新建 | task.created 同事务建 Conversation + Bridge 发 Task root card |
| **Bridge**（飞书 @bot）| 用户飞书 @bot 直接发起 | task.created 同事务建 Conversation + Bridge 发 Task root card 回写 primary_channel_thread_key |

详见 [01-task.md § 7 Conversation 绑定（含懒创建）](01-task.md)。

### 4.2 TaskExecutionFactory

**唯一 caller**：DispatchService（§ 3.1）。

入参 envelope 构造 + 单活校验在 DB tx 内完成。详见 § 3.1。

### 4.3 InputRequestFactory

**Caller**：Worker daemon（代 agent）。

| 流程步骤 | 描述 |
|---|---|
| 1 | Worker 收到 agent 的 `request-input` RPC（unix socket） |
| 2 | Center 单事务执行：写 input_request + 更新 task_execution.pending_input_request_id + status='input_required' + INSERT events |
| 3 | 解析 task.conversation_id：非 null → 写 Message (kind=agent_finding, input_request_ref=*) 到该 conversation；null → 触发 fallback（详见 [03-input-request.md § 8 fallback](03-input-request.md)） |

跨聚合：写 InputRequest + 写 TaskExecution + 可能写 Conversation Message + 同事务。

---

## § 5. Repositories（X.5）

接口签名（Go-style，含 `ctx context.Context` 参数；架构层契约，跟实现解耦）：

### 5.1 TaskRepository

```go
type TaskRepository interface {
    FindByID(ctx context.Context, id TaskID) (*Task, error)
    FindByProject(ctx context.Context, projectID ProjectID, filter TaskFilter) ([]*Task, error)
    FindByStatus(ctx context.Context, status TaskStatus, filter TaskFilter) ([]*Task, error)
    FindBlockedBy(ctx context.Context, blockerTaskID TaskID) ([]*Task, error)
    Save(ctx context.Context, t *Task) error               // 新建 + 全量更新（含乐观锁 version 列）
    UpdateStatus(ctx context.Context, id TaskID, from, to TaskStatus, version int) error
    UpdateConversationID(ctx context.Context, id TaskID, conversationID ConversationID) error
    UpdateCurrentExecutionID(ctx context.Context, id TaskID, executionID TaskExecutionID) error
}

// Domain errors
var (
    ErrTaskNotFound          = errors.New("taskruntime: task not found")
    ErrTaskAlreadyExists     = errors.New("taskruntime: task already exists")
    ErrTaskInvalidTransition = errors.New("taskruntime: invalid task status transition")
    ErrTaskVersionConflict   = errors.New("taskruntime: task version conflict (optimistic lock)")
)
```

### 5.2 TaskExecutionRepository

```go
type TaskExecutionRepository interface {
    FindByID(ctx context.Context, id TaskExecutionID) (*TaskExecution, error)
    FindByTaskID(ctx context.Context, taskID TaskID) ([]*TaskExecution, error)
    FindByWorkerID(ctx context.Context, workerID WorkerID, status ...TaskExecutionStatus) ([]*TaskExecution, error)
    FindActive(ctx context.Context) ([]*TaskExecution, error)              // status ∈ {submitted, working, input_required}
    Save(ctx context.Context, e *TaskExecution) error
    UpdateStatus(ctx context.Context, id TaskExecutionID, from, to TaskExecutionStatus, version int) error
    UpdateDispatchState(ctx context.Context, id TaskExecutionID, state DispatchState) error
    UpdateCancelRequestedAt(ctx context.Context, id TaskExecutionID, requestedAt time.Time, reason CancelReason, message string, version int) error
    UpdateCompleted(ctx context.Context, id TaskExecutionID, reason CompletedReason, message string, version int) error
    UpdateFailed(ctx context.Context, id TaskExecutionID, reason FailedReason, message string, version int) error
    UpdateKilled(ctx context.Context, id TaskExecutionID, reason KilledReason, message string, version int) error
}

// Domain errors
var (
    ErrTaskExecutionNotFound          = errors.New("taskruntime: task execution not found")
    ErrTaskExecutionAlreadyTerminated = errors.New("taskruntime: task execution already in terminal state")
    ErrTaskExecutionVersionConflict   = errors.New("taskruntime: task execution version conflict")
    ErrSingleActiveViolation          = errors.New("taskruntime: task already has active execution (single-active invariant)")
)
```

> **终态 reason 是 typed enum**（§ 1.3 VO）：`CompletedReason` / `FailedReason` / `KilledReason` 各自闭集枚举，配 `message` 字段（[conventions § 16](../../../../rules/conventions.md)）；CAS via `version int` 防终态被并发覆盖（如 kill 跟 normal complete 竞态）。
>
> **BC 物理隔离**（[conventions § 9.z](../../../../rules/conventions.md)）：本 Repository 独占 `task_executions` 表，仅承载业务状态列（status / dispatch_state / cancel_requested_at / terminal reason+message / version 等）。execution 的实时 projection（current_activity / total_tool_calls / working_seconds_accumulated 等）归 [Observability TaskExecutionProjectionRepository](../observability/00-overview.md) 写入**它自己**的 `task_execution_projections` 表（PK = task_execution_id，与本表 1:1）。**两表物理独立，不共享行 / 不共享列**；`inspect execution` 等读路径通过 PK JOIN 拼合。
>
> **历史包袱清理（2026-05-20）**：原设计曾把 projection 列拼到本表跨 BC 共写，已按 conventions § 9.z 拆分。

### 5.3 InputRequestRepository

```go
type InputRequestRepository interface {
    FindByID(ctx context.Context, id InputRequestID) (*InputRequest, error)
    FindByTaskExecutionID(ctx context.Context, executionID TaskExecutionID) (*InputRequest, error)
    FindPending(ctx context.Context, olderThan time.Time) ([]*InputRequest, error)
    Save(ctx context.Context, r *InputRequest) error
    UpdateStatus(ctx context.Context, id InputRequestID, from, to InputRequestStatus, version int) error
    UpdateResponse(ctx context.Context, id InputRequestID, response InputResponse) error
}

// Domain errors
var (
    ErrInputRequestNotFound          = errors.New("taskruntime: input request not found")
    ErrInputRequestAlreadyResolved   = errors.New("taskruntime: input request already resolved")
    ErrInputRequestVersionConflict   = errors.New("taskruntime: input request version conflict")
)
```

### 5.4 ArtifactRepository（sub-repo of TaskExecution）

```go
type ArtifactRepository interface {
    FindByExecutionID(ctx context.Context, executionID TaskExecutionID) ([]*Artifact, error)
    FindByTaskID(ctx context.Context, taskID TaskID) ([]*Artifact, error)
    Append(ctx context.Context, a *Artifact) error          // immutable append-only；不允许 Update/Delete
}

// Domain errors
var (
    ErrArtifactNotFound = errors.New("taskruntime: artifact not found")
    ErrArtifactImmutable = errors.New("taskruntime: artifact is append-only, cannot modify")
)
```

### 5.5 约定

- 外部只通过 Root.id 引用各 AR，不持内部 Entity 句柄（[conventions § 0.3](../../../../rules/conventions.md) AR 守门）
- Artifact 作为 TaskExecution 的子实体，通过 TaskExecution 主键关联（`execution_id`）
- Repository 是**领域层抽象接口**；实现层（SQL schema / dialect / tx context 传递）落到 [implementation/02-persistence-schema.md](../../../implementation/)，跟接口解耦
- 乐观锁：`UpdateStatus` / `Update*` 带 `version int` 参数，CAS 失败返回 `Err*VersionConflict`（[conventions § 9.0](../../../../rules/conventions.md) 禁用行级锁）
- Domain errors 用 sentinel error pattern（`var Err* = errors.New(...)`）；调用方用 `errors.Is(err, ErrTaskNotFound)` 判定

---

## § 6. 跨聚合引用出方向（X.6）

| 引用方 → 被引方 | 强弱 | 一致性窗口 | 触发场景 | ADR |
|---|---|---|---|---|
| **Task → Conversation**（`task.conversation_id`） | 强 / 1:1 | tx 同步（a/e 路径同事务建；b/c/d 路径懒创建另事务） | task 创建（a/e）/ task bind-conversation 命令（b/c/d） | [ADR-0017](../../../decisions/0017-task-as-conversation.md) |
| **Task → Issue**（`task.from_issue_id`） | 弱 / 血缘 | 无（创建时填，从不变） | IssueConcludeSpawn 创建 task | - |
| **Task → parent_task**（`task.parent_task_id`） | 弱 / 不阻塞 | 无 | Sub-task 创建时填 | - |
| **Task → Worker**（间接 via execution）| 弱 / 间接 | 无（通过 current_execution_id → worker_id） | dispatch | - |
| **TaskExecution → Task**（`task_execution.task_id`） | 强 / 不可变 | tx 同步（创建时即定） | DispatchService 创建 execution | [ADR-0010](../../../decisions/0010-task-execution-two-layer-model.md) |
| **TaskExecution → Worker**（`task_execution.worker_id`） | 强 / 不可变 | tx 同步 | DispatchService 选 worker 后定 | [ADR-0010](../../../decisions/0010-task-execution-two-layer-model.md) / [ADR-0011](../../../decisions/0011-dispatch-reliability-protocol.md) |
| **TaskExecution → InputRequest**（pending；`task_execution.pending_input_request_id`） | 强 / 临时 | tx 同步（InputRequest pending 时填；resolved 时改 null） | InputRequestFactory | - |
| **InputRequest → TaskExecution**（`input_request.task_execution_id`） | 强 / 不可变 | tx 同步 | InputRequestFactory | - |
| **Artifact → TaskExecution**（`artifact.execution_id`）+ **Artifact → Task**（`artifact.task_id` 冗余） | 强 / 不可变 | tx 同步（append-only） | Worker daemon report-artifact | - |

**跨聚合一致性策略汇总**：

- 大量使用"tx 内同时改两聚合"（[ADR-0014 § 2](../../../decisions/0014-event-sourcing-level.md)）—— Task / Conversation 同建；Task / TaskExecution dispatch；TaskExecution / InputRequest pending；TaskExecution / Artifact append
- 单 BC 内多聚合 tx 是合法工程模式（不引入跨 BC 跨表的复杂分布式事务）

---

## § 7. 跨 BC 交互

### 7.1 Supervisor 唤醒事件白名单

TaskRuntime emit 的事件中触发 supervisor 唤醒的子集：

| 事件 | 典型 supervisor 决策 |
|---|---|
| `task.created` | 评估派单 |
| `task.priority_changed` / `task.eta_changed` | 重评估派单顺序 |
| `task.dependency_added` / `task.dependency_removed` | 重评估派单可行性 |
| `task_execution.completed` | 是否派 deps 后续 task / 通知用户 |
| `task_execution.failed` | 重派 / 升级 Issue / 通知用户 |
| `task_execution.killed` | 通常不动作（已是显式 kill） |
| `task.done` | 检查 dep 链路解锁的 task；评估派单 |
| `task.dispatch_limit_reached` | 升级到用户 |
| `input_request.requested` | 推飞书卡片 / 提议答案 |
| `input_request.timed_out` | 决定重派 / 升级 |
| `worker.online` | 重评估堆积 open task（来自 Workforce BC） |
| `worker.offline` | 处理 worker_lost 的 active executions（来自 Workforce BC） |

详见 [cognition/00-overview.md](../cognition/00-overview.md)。

### 7.2 Bridge 渲染（outbound）

| 事件 | Bridge 渲染动作 |
|---|---|
| `task.created`（a/e 路径，含 conversation_id）| 发 Task root card 到当前 channel → thread root → 回写 `conversation.primary_channel_thread_key` |
| `input_request.requested` 触发的 `conversation.message_added`（kind=agent_finding + input_request_ref）| 渲染为 interactive card with buttons（选项 / 取消 / 自己写） |
| `input_request.responded` / `input_request.timed_out` / `input_request.canceled` | `update_card` 置灰 + 显示终态文案 |
| `conversation.message_added`（agent_finding milestone，无 input_request_ref）| 普通 text message |

详见 [bridge/01-feishu-integration.md](../bridge/01-feishu-integration.md)。

### 7.3 Observability 订阅

Observability BC 订阅 TaskRuntime 全部事件做投影 / 查询 / inspect。详见 [observability/00-overview.md § 7.5 事件总览](../observability/00-overview.md)。

### 7.4 Customer-Supplier 上下游汇总

| 上游 → 下游 | 模式 | 内容 |
|---|---|---|
| Discussion → TaskRuntime | Customer-Supplier | Issue conclude 后批量创建 Tasks（IssueConcludeSpawn） |
| TaskRuntime → Discussion | Customer-Supplier | Worker 上 agent 调 `open-issue` 命令 Discussion 创建 Issue |
| TaskRuntime ↔ Workforce | Shared Kernel | Task / TaskExecution 引用 worker_id / project_id |
| TaskRuntime ↔ Conversation | Shared Kernel | task.conversation_id 1:1（[ADR-0017](../../../decisions/0017-task-as-conversation.md)） |
| Bridge → TaskRuntime | Customer-Supplier | inbound 时 Bridge 调 `task bind-conversation` / `InputRequest.respond` |
| Cognition → TaskRuntime | "User" via tools | Supervisor 调 `dispatch` / `kill-execution` / `abandon-task` / `respond-to-input-request` |
| Observability ← TaskRuntime | Open Host | 全部 task.* / task_execution.* / input_request.* / worktree.* / artifact.* 事件订阅 |

完整 context map 见 [strategic/03-bounded-contexts § 3](../../strategic/03-bounded-contexts.md)。

---

## § 8. Out-of-Scope / Future Work

| 项 | 归属 |
|---|---|
| 父子状态联动（parent done 触发 sub-task done） | [out-of-scope](../../../requirements/03-out-of-scope.md) 或 [roadmap](../../../roadmap.md) 待定 |
| 自动 cascade abandon（dep abandoned 自动连带依赖者 abandoned） | [roadmap](../../../roadmap.md) |
| Per-project timeout / max_executions override | [roadmap](../../../roadmap.md) |
| ETA 触发 supervisor 唤醒（ETA 过期自动 ping） | [roadmap](../../../roadmap.md) |
| Cross-worker dispatch fail-over（迁移 active execution） | [roadmap](../../../roadmap.md) |
| `--force-abandon`（kill 超时也强 abandon） | [roadmap](../../../roadmap.md) |
| 完整 fencing token（单调递增 sequence） | [roadmap](../../../roadmap.md)（多 supervisor 之前） |
| Workspace 模式 readonly mount enforcement | [roadmap](../../../roadmap.md) |
| Artifact 版本化 / 标签 / 自定义维度 | [roadmap](../../../roadmap.md) |
| 实时 stream timeline（CLI tail -f） | [roadmap](../../../roadmap.md) |
| Issue reopen 后 amend 已 spawn task 集 | [roadmap](../../../roadmap.md) |
| Task unbind conversation（非 null → null）| [roadmap](../../../roadmap.md)（v1 conversation 跟 task 同生命周期；切渠道场景 v2 再做）|
| Task ↔ Conversation 多 tracker（1:N，多用户多渠道镜像）| [roadmap](../../../roadmap.md)（[ADR-0017 § 9](../../../decisions/0017-task-as-conversation.md) 显式驳回 v1）|

---

## § 9. References

### 相关 ADR

- [ADR-0010 两层模型 + AgentSession 合并](../../../decisions/0010-task-execution-two-layer-model.md)
- [ADR-0011 派单可靠性协议](../../../decisions/0011-dispatch-reliability-protocol.md)
- [ADR-0014 事件溯源走 L1](../../../decisions/0014-event-sourcing-level.md)
- [ADR-0015 agent_trace 不进 events 表](../../../decisions/0015-agent-trace-not-in-events-table.md)
- [ADR-0017 Task ↔ Conversation 1:1](../../../decisions/0017-task-as-conversation.md)
- [ADR-0018 Detached agent + per-execution shim 模型](../../../decisions/0018-detached-agent-via-per-execution-shim.md)
- [ADR-0019 BC1 + BC4 合并为 TaskRuntime](../../../decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md)

### 战略层

- [strategic/03-bounded-contexts § 1.1 UL](../../strategic/03-bounded-contexts.md)（TaskRuntime 上下文术语）
- [strategic/03-bounded-contexts § 2 BC](../../strategic/03-bounded-contexts.md)（BC1 TaskRuntime 总览）
- [strategic/03-bounded-contexts § 3 Context Map](../../strategic/03-bounded-contexts.md)（上下游表）

### 同 BC 内聚合详情

- [01-task.md](01-task.md) — Task 聚合
- [02-task-execution.md](02-task-execution.md) — TaskExecution 聚合（含 worker 端运行时 + Artifact）
- [03-input-request.md](03-input-request.md) — InputRequest 聚合

### 跨 BC 协作文档

- [cognition/00-overview.md](../cognition/00-overview.md) — Supervisor 模型
- [workforce/00-overview.md](../workforce/00-overview.md) — Workforce BC（Worker / Project / Mapping / Proposal）
- [discussion/00-overview.md](../discussion/00-overview.md) — Issue 模型 + conclude 协议
- [conversation/00-overview.md](../conversation/00-overview.md) — Conversation 统一会话层
- [observability/00-overview.md](../observability/00-overview.md) — 事件总览 + 投影读模型
- [bridge/01-feishu-integration.md](../bridge/01-feishu-integration.md) — FeishuBridge 渲染
- [agent-harness/01-prompt-assembly.md](../agent-harness/01-prompt-assembly.md) — Prompt 组装

### 横切方法论

- [conventions](../../../../rules/conventions.md) § 0 DDD / § 1 无野任务 / § 8 BlobStore / § 9 mutator / § 16 reason+message
