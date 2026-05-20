# Cognition BC — DDD 战术设计 Overview

> **DDD 战术层** · BC: Cognition
>
> Supervisor 运行模型 + 唤醒调度 + Memory + Decision 审计：回答"Supervisor 是什么 / 怎么被唤醒 / 怎么做决策 / 怎么管自己的记忆与决策审计"。
>
> Supervisor 是中心的"调度官 agent"。LLM 驱动的真 agent（spawn `claude` 子进程实现），不是规则路由器；跟 worker agent 是**同源**（都是 claude 实例），区别仅在工具集和上下文（[ADR-0003](../../../decisions/0003-supervisor-not-brain.md)）。

---

## § 0. BC 一览

### 0.1 职责

| 维度 | 内容 |
|---|---|
| **聚合管理** | SupervisorInvocation（AR + DecisionRecord 子从属）/ Memory（独立 AR，file-based）|
| **唤醒与调度** | 事件驱动唤醒 / coalescing window / per-scope 串行 + 跨 scope 并行 / global FIFO 队列 / 周期 review ticker |
| **决策审计** | 每次动作 CLI 自动 INSERT DecisionRecord（kind + target_refs + rationale + outcome）|
| **Memory 管理** | 7 种 scope（task / issue / conversation / worker / project / global / supervisor）；file-based + git 仓；ancestor walk 自动加载；零 SQL 表 |
| **Cross-BC 写路径** | 全走 CLI（同 user 同套）；不为 supervisor 单造 RPC |
| **进程形态** | 短命：每次唤醒 spawn 一个 claude 子进程，跑完即退；无常驻 supervisor |

### 0.2 UL 切片

来自 [strategic/03-bounded-contexts § 1](../../strategic/03-bounded-contexts.md) 标 Cognition 上下文的术语：

- `Supervisor`（角色，中心调度官 agent；不是聚合 —— invocation 才是聚合）
- `SupervisorInvocation`（聚合根，一次 spawn → exit 的审计单元）
- `DecisionRecord`（实体，子从属于 invocation；append-only）
- `Memory`（聚合根，独立；7 种 scope；file-based）
- 行为动词：`Wake`（唤醒）/ `Decide`（决策）/ `Record`（写 decision）/ `Reflect`（write to supervisor.md）
- 状态机词汇：Invocation `running` / `succeeded` / `failed` / `timed_out`

### 0.3 Context Map 位置

[strategic/03-bounded-contexts § 3](../../strategic/03-bounded-contexts.md)：

- **Cognition → ALL**："User" via tools（Supervisor 通过 CLI 调任何 BC 的动作命令 —— dispatch / kill-execution / issue conclude / conversation add-message / 等）
- **Cognition ← ALL**：Cognition BC **不主动订阅**事件；Center 的 wake scheduler 扫 events 表的 wake 白名单 → 触发 spawn
- **Observability ← Cognition**：Open Host（订阅 `supervisor.*` 事件 + DecisionRecord 投影）

---

## § 1. 聚合清单（X.1）

### 1.1 Aggregate Roots

| 聚合 | 文件 | 状态机 | 身份 / 不变性 |
|---|---|---|---|
| **SupervisorInvocation** | [01-supervisor-invocation.md](01-supervisor-invocation.md) | 4 态（running / succeeded / failed / timed_out） | ULID/UUID = `--session-id` 实参；身份不变 |
| **Memory** | [02-memory.md](02-memory.md) | 无状态机（CLAUDE.md 文件 + git commit 历史承担演进）| 由文件路径定位：`$MEMORY_DIR/{scope_path}/CLAUDE.md`；7 种 scope |

### 1.2 Entity（子从属）

| 实体 | 从属 | 位置 |
|---|---|---|
| **DecisionRecord** | SupervisorInvocation（独立表 `decision_records`，append-only） | [01-supervisor-invocation.md § 4](01-supervisor-invocation.md) |

### 1.3 Value Objects（按使用聚合分组）

| VO | 用在哪 | 描述 |
|---|---|---|
| **InvocationStatus** | Invocation.status | `running` / `succeeded` / `failed` / `timed_out` 枚举 |
| **InvocationFailedReason+Message** | Invocation 终态附加 | reason ∈ {claude_nonzero / cli_command_error / oom / center_restart_orphan / killed_by_admin / unknown}；配 message（[conventions § 16](../../../../rules/conventions.md)）|
| **TokenUsage** | Invocation.token_usage | `{input, output, cache_read, cache_create, ...}` 来自 claude 进程退出后回填 |
| **ScopeKey** | Invocation.scope + Memory 寻址 | `(scope_kind, scope_key)` 二元组；scope_kind ∈ task / issue / conversation / worker / global（Invocation）+ project / supervisor（Memory 多 2 种）|
| **DecisionKind** | DecisionRecord.kind | 12 种闭集枚举（详见 [01-supervisor-invocation § 4.4](01-supervisor-invocation.md)）|
| **TriggerEventIds** | Invocation.trigger_event_ids | JSON array of event_id；coalesced batch 共用一行 |
| **HardTimeout** | Invocation.hard_timeout_seconds | per scope_kind：task/issue/conversation/worker = 180s；global = 600s |

---

## § 2. Invariants 索引（X.2）

每个聚合自己维护 invariants 节，本 § 仅做索引：

- **SupervisorInvocation Invariants** → [01-supervisor-invocation.md § 5](01-supervisor-invocation.md)
- **DecisionRecord Invariants** → [01-supervisor-invocation.md § 4.5](01-supervisor-invocation.md)
- **Memory Invariants** → [02-memory.md § 5](02-memory.md)

**跨聚合的不变量**：

1. **Supervisor 没有常驻进程**：每次 invocation 是 spawn → exit 全生命周期
2. **决策权威在 supervisor**：center 不做硬编码调度，所有派单 / 升级 / 关闭决策由 supervisor 跑出
3. **状态权威在 center**：supervisor 通过 CLI 写状态（同 worker / user 同套 CLI），不写本地副本
4. **同 scope_key 任意时刻最多 1 个 running invocation**（[ADR-0013](../../../decisions/0013-supervisor-invocation-concurrency.md)）
5. **`supervisor.*` 事件不进 wake 白名单**（反循环）
6. **HOME 隔离**：spawn claude 必显式 `HOME` / `CLAUDE_CONFIG_DIR`，禁污染用户私人配置

---

## § 3. Domain Services（X.3）

### 3.1 WakeScheduler

**职责**：扫 `events` 表的 wake 白名单 → 派生 scope_key → coalescing → 进 FIFO → spawn invocation。详细 ADR：[0013](../../../decisions/0013-supervisor-invocation-concurrency.md)。

| 维度 | 内容 |
|---|---|
| 输入 | events 表新行（domain event）|
| 路由 | event_type + refs → scope_key（deterministic、side-effect-free 纯函数；详见 § 3.2）|
| Coalescing | per scope_key，in-memory window：滚动 30s since last event + 硬上限 5 min since first event |
| 并发 | 同 scope_key 至多 1 running；全局上限 `max_concurrent_invocations=5`（v1 默认，可配）；超过进 in-memory FIFO 队列 |
| 反循环 | `supervisor.*` 事件不在白名单，避免自唤醒 |
| 周期 review | Center cron-ish ticker emit 合成事件 `supervisor.periodic_review_ticker` → 路由到 `global` scope → 正常路径，不绕节流 |

#### 3.2 唤醒事件白名单（v1）

```
Task / Execution / InputRequest:
  task.created / task.priority_changed / task.eta_changed /
  task.dependency_added / task.dependency_removed /
  task.done / task.dispatch_limit_reached
  task_execution.completed / task_execution.failed / task_execution.killed
  input_request.requested / input_request.timed_out

Issue / Conversation:
  issue.opened / issue.discussion_started / issue.concluded / issue.withdrawn
  conversation.message_added

Worker:
  worker.online / worker.offline
  worker_project_proposal.proposed

Global:
  supervisor.periodic_review_ticker      ← center cron-ish ticker emit 的合成事件
```

**Scope key 派生**：

| event_type 前缀 | refs 取值 | 派生 scope_key |
|---|---|---|
| `task.*` / `task_execution.*` / `input_request.*` | `refs.task_id=T` | `task:T` |
| `issue.*` | `refs.issue_id=I` | `issue:I` |
| `conversation.message_added` | `refs.conversation_id=C` | `conversation:C` |
| `worker.*` / `worker_project_proposal.*` | `refs.worker_id=W` | `worker:W` |
| `supervisor.periodic_review_ticker` | — | `global` |

### 3.2 InvocationFactory

**唯一 caller**：WakeScheduler。spawn claude 子进程 + 落 invocation 行（status=running）。

| 维度 | 内容 |
|---|---|
| 入参 | scope（scope_kind + scope_key）+ trigger_event_ids + hard_timeout_seconds |
| 出参 | InvocationRow{id} + spawned claude PID |
| Spawn 配置 | `HOME` / `CLAUDE_CONFIG_DIR` 隔离目录；`--session-id=<invocation_id>` 关联 trace；CWD 按 scope 设置（详见 [02-memory § 3 加载机制](02-memory.md)） |

### 3.3 InvocationTimeoutHandler

**职责**：扫所有 status=running 的 invocation，命中 `hard_timeout_seconds` → SIGTERM → 5s grace → SIGKILL → status=timed_out。

### 3.4 InvocationCrashRecovery

**职责**：Center 重启时扫 status=running 的"孤儿"行 → 改 failed(reason=center_restart_orphan) → 按 scope_kind/scope_key 扫"未被任何成功 invocation 覆盖"的事件 → 重建 coalescing window 续跑。

详见 [01-supervisor-invocation § 2.5](01-supervisor-invocation.md)。

### 3.5 DecisionWriter

**职责**：动作 CLI（dispatch / kill-execution / issue conclude / conversation add-message / 等）内部自动 INSERT decision_records 行 + emit 联动 domain events（带 `decision_id` / `correlation_id=invocation_id`）。Actor 推断从 env `AGENT_CENTER_INVOCATION_ID` 取（有 → supervisor；无 → user，不写 decision_record）。

详见 [01-supervisor-invocation § 4.6](01-supervisor-invocation.md)。

---

## § 4. Factories（X.4）

### 4.1 InvocationFactory

见 § 3.2。

### 4.2 MemorySkeletonFactory

**职责**：订阅 lifecycle 事件，立即创建 scope 对应的 `CLAUDE.md` 空骨架 + git commit（author=`system:bootstrap`）。

| 触发事件 | 创建文件 |
|---|---|
| `project.created` | `projects/<id>/CLAUDE.md` |
| `task.created` | `projects/<X>/tasks/<id>/CLAUDE.md` |
| `issue.opened` | `projects/<X>/issues/<id>/CLAUDE.md` |
| `worker.enrolled` | `workers/<id>/CLAUDE.md` |
| `conversation.opened` | `conversations/<id>/CLAUDE.md` |

详见 [02-memory § 4 冷启动骨架](02-memory.md)。

---

## § 5. Repositories（X.5）

接口签名（Go-style，含 `ctx context.Context` 参数；架构层契约，跟实现解耦）：

### 5.1 SupervisorInvocationRepository

```go
type SupervisorInvocationRepository interface {
    FindByID(ctx context.Context, id InvocationID) (*SupervisorInvocation, error)
    FindByScope(ctx context.Context, scopeKind ScopeKind, scopeKey string) ([]*SupervisorInvocation, error)
    FindRunning(ctx context.Context) ([]*SupervisorInvocation, error)
    FindRunningByScope(ctx context.Context, scopeKind ScopeKind, scopeKey string) (*SupervisorInvocation, error) // 单活校验（命名对齐 FindByScope）
    Save(ctx context.Context, inv *SupervisorInvocation) error
    UpdateStatusToTerminal(ctx context.Context, id InvocationID, update InvocationTerminalUpdate, version int) error
}

// InvocationTerminalUpdate 聚合终态回填字段
type InvocationTerminalUpdate struct {
    Status         InvocationStatus     // succeeded / failed / timed_out
    EndedAt        time.Time
    FailedReason   InvocationFailedReason  // typed enum (claude_nonzero / oom / etc.)；succeeded 时为零值
    FailedMessage  string                  // reason+message 双字段，[conventions § 16]
    TimedOutAt     *time.Time              // 仅 timed_out 时填
    TokenUsage     TokenUsage
    DecisionsMade  int
}

// Domain errors
var (
    ErrInvocationNotFound          = errors.New("cognition: invocation not found")
    ErrInvocationAlreadyTerminal   = errors.New("cognition: invocation already in terminal state")
    ErrInvocationVersionConflict   = errors.New("cognition: invocation version conflict (optimistic lock)")
    ErrScopeKeyRunningExists       = errors.New("cognition: another invocation running for same scope_key (single-active)")
)
```

### 5.2 DecisionRecordRepository（sub-repo of Invocation）

```go
type DecisionRecordRepository interface {
    FindByInvocationID(ctx context.Context, invocationID InvocationID) ([]*DecisionRecord, error)
    FindByKind(ctx context.Context, kind DecisionKind, since time.Time) ([]*DecisionRecord, error)
    FindByOutcome(ctx context.Context, outcome DecisionOutcome, since time.Time) ([]*DecisionRecord, error)
    Append(ctx context.Context, d *DecisionRecord) error           // immutable append-only；不允许 Update/Delete
}

// Domain errors
var (
    ErrDecisionNotFound  = errors.New("cognition: decision record not found")
    ErrDecisionImmutable = errors.New("cognition: decision record is append-only, cannot modify")
    ErrRationaleRequired = errors.New("cognition: rationale field required for all decisions")
)
```

### 5.3 MemoryRepository（特殊：文件系统 + git 仓）

**不走标准 Repository 抽象** —— Memory 物理形态是 `$AGENT_CENTER_MEMORY_DIR/` git 仓，由 supervisor 用 claude 原生 `Edit` / `Write` 工具直接读写（[ADR-0012](../../../decisions/0012-memory-file-based.md)）。

但有 **MemorySkeletonFactory** 接口（cold-start 时建空骨架）+ **MemoryGitOpsService** 接口（invocation 退出时 center 兜底 commit）：

```go
type MemorySkeletonFactory interface {
    CreateSkeleton(ctx context.Context, scopeKind ScopeKind, scopeKey string) error      // 建空 CLAUDE.md + git commit
}

type MemoryGitOpsService interface {
    AutoCommitDirty(ctx context.Context, invocationID InvocationID, scopeKind ScopeKind, scopeKey string) error
    // invocation 退出时检查 dirty + auto commit (author=supervisor:<inv-id>)
}

// Domain errors
var (
    ErrMemoryDirNotInitialized = errors.New("cognition: memory dir not initialized as git repo")
    ErrMemoryFileExists        = errors.New("cognition: memory file already exists (skeleton)")
    ErrMemoryGitOpFailed       = errors.New("cognition: memory git operation failed")
)
```

### 5.4 约定

- 外部只通过 Invocation.id 引用 invocation AR（[conventions § 0.3](../../../../rules/conventions.md) AR 守门）
- DecisionRecord 通过 `invocation_id` 关联到 Invocation 聚合
- Memory **不**走传统 Repository 抽象 —— 直接走 file ops；但 SkeletonFactory + GitOpsService 是架构层契约
- DecisionRecord append-only；INSERT 后不可变（含 outcome 字段）
- Repository 是**领域层抽象接口**；实现层落到 [implementation/02-persistence-schema.md](../../../implementation/) (TBD)（Memory 物理形态见 [ADR-0012](../../../decisions/0012-memory-file-based.md)）
- Domain errors 用 sentinel error pattern；调用方用 `errors.Is` 判定

---

## § 6. 跨聚合引用出方向（X.6）

| 引用方 → 被引方 | 强弱 | 一致性窗口 | 触发场景 |
|---|---|---|---|
| **DecisionRecord → SupervisorInvocation**（`decision.invocation_id`）| 强 / 不可变 | tx 同步 | DecisionWriter（动作 CLI 内部）|
| **Event → DecisionRecord**（`events.decision_id`，Observability BC 内）| 弱 / nullable | tx 同步（emit 时填 decision_id） | DecisionWriter 联动 emit |
| **跨 BC 写**：Supervisor 通过 CLI → 任何 BC 的动作 | 弱 / via CLI | CLI 内部事务 | Supervisor 调动作 CLI |
| **跨 BC 读**：Supervisor 通过 inspect / query / ps CLI 查任何 BC 状态 | 无引用 | 无（只读）| Supervisor 进入 prompt 时通过工具调用 |

---

## § 7. 跨 BC 交互

### 7.1 Cognition → 其他 BC（写路径，全走 CLI）

Supervisor 通过 `Bash` 工具调 `agent-center <cmd>` 触动作。CLI 内部跑领域逻辑 + INSERT `decision_records` + emit 联动 events：

| 目标 BC | CLI 子命令 | 对应 DecisionRecord kind |
|---|---|---|
| TaskRuntime | `dispatch` | `dispatch` |
| TaskRuntime | `kill-execution` | `kill_execution` |
| TaskRuntime | `abandon-task` / `suspend-task` / `resume-task` | 同名 |
| TaskRuntime | `respond-to-input-request` | （走 `conversation_message` 或 `escalate_input_request`，看路径） |
| Discussion | `issue open` / `issue comment` (facade) / `issue conclude` / `issue close` / `issue bind-conversation` | `open_issue` / `issue_comment` / `conclude_issue` / `close_issue` |
| Conversation | `conversation add-message` | `conversation_message` |
| 跨 BC | `escalate-input-request` | `escalate_input_request` |
| Cognition 自身 | `record-decision` | `no_op` |

### 7.2 Cognition ← 其他 BC（订阅事件触发 wake）

Cognition BC **不主动订阅**事件。Center 的 wake scheduler（§ 3）扫 events 表的 wake 白名单 → 派生 scope_key → coalescing window → 触发 spawn。

### 7.3 查询 / inspect：跟 user 共用同一套

Supervisor 用同样的 `inspect` / `query` / `ps` CLI 查 task / execution / issue / worker / conversation / 等。**不为 supervisor 单造 RPC**。详见 [observability § O5](../observability/01-observability.md)。

### 7.4 Prompt 组装边界

**Supervisor prompt 三块构成**：

```
(a) supervisor.md skill                    ← bundled with binary
(b) ancestor walk 自动加载的 CLAUDE.md 链  ← § scope → CWD 映射，详见 02-memory § 3
(c) wake event payload                     ← trigger_event_ids 内容 + 上下文
```

跟 [worker-side prompt 组装](../agent-harness/01-prompt-assembly.md) 是**两套独立机制**，不复用。

### 7.5 跟 worker-side agent 的对比

| 维度 | Supervisor agent | Worker agent |
|---|---|---|
| 触发 | 事件驱动（唤醒事件 → coalescing → spawn）| dispatch 驱动（envelope 到达 → spawn）|
| 短命/长命 | 短命，每次一个新进程 | 短命，每次一个新进程 |
| Skill 文件 | `supervisor.md` | `worker-agent.md` |
| 上下文注入 | Memory ancestor walk（自动）+ wake event payload | Task envelope content + worktree-local `CLAUDE.md`（项目仓库管，[ADR-0005](../../../decisions/0005-project-charter-stays-in-project-repo.md)）|
| Memory 来源 | `$AGENT_CENTER_MEMORY_DIR/` git 仓（agent 自管）| 工作树内项目自带的 `CLAUDE.md` / `AGENTS.md` |

---

## § 8. Out-of-Scope / Future Work

| 项 | 归属 |
|---|---|
| Supervisor 自动重试（claude_nonzero / timed_out 自动重发同 trigger）| [roadmap](../../../roadmap.md)（未来视失败率统计决定）|
| Task-level "long-no-update" auto-ping（每 task 加 idle timer 主动唤醒 supervisor）| [roadmap](../../../roadmap.md)（跟 [task-runtime/00-overview § 8 OOS](../task-runtime/00-overview.md) "ETA 过期触发 supervisor 唤醒" 同类）|
| Cross-invocation 协调机制（多 invocation 间互锁）| [roadmap](../../../roadmap.md)（多 supervisor 之前）|
| Memory 并发写 advisory lock | [roadmap](../../../roadmap.md) |
| Memory 跨 BC 聚合查询（grep 工具化、Web Console 可视）| [roadmap](../../../roadmap.md) |
| 显式 `pending` invocation 状态（队列可见性）| [roadmap](../../../roadmap.md)（队列 metric / UI 时再加）|
| Memory file 体积监控告警（自动提醒 supervisor 压缩）| [roadmap](../../../roadmap.md) |
| Token cost 折算金钱 + 告警 | [roadmap](../../../roadmap.md)（已列）|
| 多 supervisor / 跨机器 | [roadmap](../../../roadmap.md)（多用户 / SaaS 之前）|

---

## § 9. References

### 相关 ADR

- [ADR-0002 不用 LLM SDK 走 CLI agent](../../../decisions/0002-no-llm-sdk-use-cli-agents.md)
- [ADR-0003 Supervisor 而非 Brain](../../../decisions/0003-supervisor-not-brain.md)
- [ADR-0012 Memory file-based + git](../../../decisions/0012-memory-file-based.md)
- [ADR-0013 Supervisor Invocation 并发模型](../../../decisions/0013-supervisor-invocation-concurrency.md)
- [ADR-0015 agent_trace 不进 events 表](../../../decisions/0015-agent-trace-not-in-events-table.md)

### 战略层

- [strategic/03-bounded-contexts § 1 UL](../../strategic/03-bounded-contexts.md)（Cognition 上下文术语）
- [strategic/03-bounded-contexts § 2 BC4 Cognition](../../strategic/03-bounded-contexts.md)
- [strategic/03-bounded-contexts § 3 Context Map](../../strategic/03-bounded-contexts.md)

### 同 BC 内聚合详情

- [01-supervisor-invocation.md](01-supervisor-invocation.md) — SupervisorInvocation AR + DecisionRecord 子从属
- [02-memory.md](02-memory.md) — Memory AR（file-based + git 仓）

### 跨 BC 协作文档

- [task-runtime/00-overview.md § 7.1](../task-runtime/00-overview.md) — Supervisor 唤醒事件白名单（task / execution / input_request 部分权威）
- [discussion/00-overview.md § 7.1](../discussion/00-overview.md) — Issue 相关唤醒事件
- [workforce/00-overview.md § 7.1](../workforce/00-overview.md) — Worker / Proposal 相关唤醒事件
- [conversation/00-overview.md](../conversation/00-overview.md) — conversation.message_added 唤醒事件
- [observability/00-overview.md](../observability/00-overview.md) — events 表 + `inspect supervisor` 查询接口
- [agent-harness/01-prompt-assembly.md](../agent-harness/01-prompt-assembly.md) — Worker-side prompt 组装（跟本 BC 独立）
- [bridge/01-feishu-integration.md](../bridge/01-feishu-integration.md) — 失败 invocation 推飞书提醒人工 retrigger

### 横切方法论

- [conventions](../../../../rules/conventions.md) § 0 DDD / § 1 无野任务 / § 2 可观测性 / § 8 BlobStore（prompt_blob_ref）/ § 16 reason+message
