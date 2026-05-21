# Phase 6: Cognition Supervisor

> DDD BC: **Cognition**（Customer 角色）· 依赖 Phase 1-5 · 解锁 Phase 7（Bridge inbound + 部署收尾）
> 纪律：按里程碑顺序 / 模块完备不半成品 / 单测 ≥ 90% + 集成 + e2e + 测试报告

## § 0. 目标

让"调度官 agent"在 center 上跑起来：领域事件落 events 表后，center 内部按 [ADR-0013](../design/decisions/0013-supervisor-invocation-concurrency.md) 的 coalescing window + per-scope 串行 + 跨 scope 并行模型，把事件路由成 `SupervisorInvocation` 行，spawn 一个短命的 `agent-center supervisor` 子进程 —— 该子进程内部用真实 `claude` CLI（[ADR-0002](../design/decisions/0002-no-llm-sdk-use-cli-agents.md)）跑一次决策周期，通过 `Bash` 工具调 `agent-center <cmd>`（dispatch / kill-execution / issue conclude / record-decision / 等）落 `DecisionRecord` + emit 联动 domain events。下游 BC（TaskRuntime / Discussion / Conversation / Workforce）通过订阅事件自然推进；Bridge outbound 把 supervisor 的副作用推到飞书（Phase 5 已就绪）。

DDD 意义：Customer 角色就位 —— 此前 5 个 phase 全部是被调方（Shared Kernel / Core BC / Open Host / ACL outbound），Cognition 是它们的**唯一调用方**。Phase 6 把"事件 → 决策 → 动作 → 新事件"的反馈环闭合，center 从"被动等命令的状态机集合"升级为"自主调度的 agent 系统"。

**OUT of scope（明确推迟）**：

- ❌ Bridge inbound（飞书 → `conversation.message_added` → 唤醒 supervisor）—— 走 Phase 7
- ❌ Web Console / supervisor 训练 / 学习闭环 —— roadmap，v1 不做
- ❌ Cross-invocation 协调 / Memory advisory lock —— roadmap（[cognition/00-overview § 8](../design/architecture/tactical/cognition/00-overview.md)）

---

## § 1. DDD 工件清单

### 1.1 Aggregate Roots

| 聚合 | 角色 | 文件 | 状态机 |
|---|---|---|---|
| **SupervisorInvocation** | 一次 spawn → exit 的审计单元 | [cognition/01-supervisor-invocation § 2](../design/architecture/tactical/cognition/01-supervisor-invocation.md) | 4 态：`running` / `succeeded` / `failed` / `timed_out` |
| **Memory** | Supervisor 持久脑（file-based + git）| [cognition/02-memory](../design/architecture/tactical/cognition/02-memory.md) | 无状态机（git commit 历史承担演进）|

### 1.2 Entities（子从属）

| 实体 | 从属 | 物理表 | 备注 |
|---|---|---|---|
| **DecisionRecord** | SupervisorInvocation | `decision_records`（独立 sub-repo） | append-only；INSERT 后不可变；[01-supervisor-invocation § 4](../design/architecture/tactical/cognition/01-supervisor-invocation.md) |

### 1.3 Value Objects

| VO | 用在 | 描述 |
|---|---|---|
| **InvocationStatus** | Invocation.status | `running` / `succeeded` / `failed` / `timed_out` 枚举 |
| **InvocationScope** | Invocation.scope（scope_kind + scope_key）+ Memory CWD 映射 | scope_kind ∈ {task / issue / conversation / worker / global} (Invocation 5 种)；Memory 多 2 种 (project / supervisor) |
| **TriggerEventSet** | Invocation.trigger_event_ids | JSON array of event_id；coalesced batch 共用一行；≥1 |
| **InvocationOutcome** | Invocation 终态回填 | `{status, ended_at, failed_reason+message, timed_out_at, token_usage, decisions_made}`（即架构层 `InvocationTerminalUpdate`）|
| **InvocationFailedReason+Message** | failed 时附加 | reason ∈ {claude_nonzero / cli_command_error / oom / center_restart_orphan / killed_by_admin / unknown}（[conventions § 16](../rules/conventions.md)）|
| **TokenUsage** | Invocation.token_usage | `{input, output, cache_read, cache_create}` 来自 claude 子进程退出回填 |
| **DecisionKind** | DecisionRecord.kind | 12 种闭集枚举（[01-supervisor-invocation § 4.4](../design/architecture/tactical/cognition/01-supervisor-invocation.md)）|
| **DecisionOutcome** | DecisionRecord.outcome | `succeeded` / `failed`（CLI 同步结果）|
| **HardTimeout** | Invocation.hard_timeout_seconds | per scope_kind 派生（task/issue/conversation/worker = 180s；global = 600s）|

### 1.4 Repositories

| Repository | 接口位置 | 实现位置 | 备注 |
|---|---|---|---|
| **SupervisorInvocationRepository** | `internal/cognition/repository.go` | `internal/persistence/cognition/invocation_repo.go` | 接口签名见 [00-overview § 5.1](../design/architecture/tactical/cognition/00-overview.md) |
| **DecisionRecordRepository** | `internal/cognition/repository.go` | `internal/persistence/cognition/decision_repo.go` | append-only；接口签名见 [00-overview § 5.2](../design/architecture/tactical/cognition/00-overview.md) |
| **MemorySkeletonFactory** | `internal/cognition/memory/skeleton.go`（接口）| `internal/cognition/memory/skeleton_fs.go`（fs+git 实现） | [02-memory § 4](../design/architecture/tactical/cognition/02-memory.md) |
| **MemoryGitOpsService** | `internal/cognition/memory/gitops.go`（接口）| `internal/cognition/memory/gitops_exec.go`（shell out git）| [02-memory § 5.1](../design/architecture/tactical/cognition/02-memory.md) |

Sentinel errors（[00-overview § 5](../design/architecture/tactical/cognition/00-overview.md)）：
- `ErrInvocationNotFound` / `ErrInvocationAlreadyTerminal` / `ErrInvocationVersionConflict` / `ErrScopeKeyRunningExists`
- `ErrDecisionNotFound` / `ErrDecisionImmutable` / `ErrRationaleRequired`
- `ErrMemoryDirNotInitialized` / `ErrMemoryFileExists` / `ErrMemoryGitOpFailed`

### 1.5 Domain Services

| Service | 职责 | 引用 |
|---|---|---|
| **SupervisorTriggerCoalescer** | 订阅 EventRepository → 派生 scope_key → in-memory coalescing window（30s 滚动 / 5min 硬上限）→ FIFO 队列（全局 max=5）| [00-overview § 3.1 / 3.2](../design/architecture/tactical/cognition/00-overview.md), [ADR-0013 § 1-3](../design/decisions/0013-supervisor-invocation-concurrency.md) |
| **SupervisorSpawner** | 关窗后落 `supervisor_invocations` 行（status=running）+ fork+exec `agent-center supervisor` 子进程；HOME / CLAUDE_CONFIG_DIR 隔离；按 scope 设 CWD | [00-overview § 3.2 / § 4.1](../design/architecture/tactical/cognition/00-overview.md), [02-memory § 3 / § 3.2](../design/architecture/tactical/cognition/02-memory.md) |
| **SupervisorPromptAssembler** | `agent-center supervisor` 子进程内部组装：`supervisor.md` skill（binary embed）+ memory snapshot 临时文件 + trigger events 摘要 → final prompt 传给 claude adapter | [cognition/00-overview § 7.4](../design/architecture/tactical/cognition/00-overview.md), [agent-harness/01-prompt-assembly](../design/architecture/tactical/agent-harness/01-prompt-assembly.md)（注：worker-side 两套独立）|
| **DecisionRecorder**（即 DecisionWriter）| 动作 CLI 内部自动 INSERT decision_records + emit 联动 events 同 tx | [01-supervisor-invocation § 4.6-4.7](../design/architecture/tactical/cognition/01-supervisor-invocation.md), [ADR-0014](../design/decisions/0014-event-sourcing-level.md) |
| **InvocationTimeoutHandler** | 周期扫 running invocation，命中 hard timeout → SIGTERM → 5s grace → SIGKILL → status=timed_out | [00-overview § 3.3](../design/architecture/tactical/cognition/00-overview.md) |
| **InvocationCrashRecovery** | Center 重启时扫 status=running 孤儿行 → failed(reason=center_restart_orphan) + replay 未覆盖事件 | [00-overview § 3.4](../design/architecture/tactical/cognition/00-overview.md), [01-supervisor-invocation § 2.4](../design/architecture/tactical/cognition/01-supervisor-invocation.md) |

### 1.6 Application Services（CLI handler 层）

| CLI 命令 | audience | handler 位置 | 备注 |
|---|---|---|---|
| `agent-center supervisor --scope=<scope> [--invocation-id=...] [--trigger-events=...]` | Sys（center 内部 spawn）| `internal/cli/supervisor_run.go` | 短生命周期；**不读 config file**，CLI flag 注入；[03-cli § 8.8](../design/implementation/03-cli-subcommands.md), [04-config § 7.3 表头注](../design/implementation/04-configuration.md) |
| `agent-center supervisor retrigger <invocation_id>` | U（人工）| `internal/cli/supervisor_retrigger.go` | 复用同 trigger_event_ids 起新 invocation；[03-cli § 8.5](../design/implementation/03-cli-subcommands.md) |
| `agent-center record-decision --invocation=<id> --kind=no_op --target=... --rationale=...` | S（supervisor）| `internal/cli/record_decision.go` | "沉默思考"留痕；非 no_op 由各动作 CLI 内部自动写 |
| `agent-center escalate-input-request <input_request_id>` | S | `internal/cli/escalate_input_request.go` | [cognition/00-overview § 7.3](../design/architecture/tactical/cognition/00-overview.md) |

### 1.7 Domain Events（emit 给 events 表）

| event_type | 触发 | refs | payload 关键字段 |
|---|---|---|---|
| `supervisor.invocation_started` | SupervisorSpawner 落 invocation 行后 | `{invocation_id, scope_kind, scope_key}` | `{trigger_event_ids[], hard_timeout_seconds, started_at}` |
| `supervisor.invocation_succeeded` | claude exit 0 后回填终态 | 同上 | `{token_usage, decisions_made, ended_at}` |
| `supervisor.invocation_failed_alert` | claude exit ≠ 0 / center crash orphan / cli_command_error / oom / killed_by_admin | 同上 | `{failed_reason, failed_message, ended_at}` |
| `supervisor.invocation_timed_out` | InvocationTimeoutHandler kill | 同上 | `{timed_out_at, hard_timeout_seconds}` |
| `supervisor.periodic_review_ticker` | Center cron-ish ticker 合成 | `{}` | `{}` —— 路由到 `global` scope |
| `supervisor.retriggered` | `supervisor retrigger` CLI | `{prev_invocation_id, new_invocation_id, scope_kind, scope_key}` | `{operator, trigger_event_ids[]}` |
| `input_request.escalated` | `escalate-input-request` CLI | `{input_request_id, task_id}` | `{notification_channel}` |

**反循环不变量**：`supervisor.*` 事件全部**不在** SupervisorTriggerCoalescer 的 wake 白名单（[00-overview § 3.1](../design/architecture/tactical/cognition/00-overview.md)），仅供 Bridge / observability 消费。`input_request.escalated` 是跨 BC 命令的 emit，进 wake 白名单（属 `input_request.*`）。

`record-decision --kind=no_op` 走 DecisionRecorder 通用路径，**不**单独 emit `decision.*` 事件 —— events 表上下游通过 `events.decision_id` JOIN `decision_records` 取 rationale（[01-supervisor-invocation § 4.10](../design/architecture/tactical/cognition/01-supervisor-invocation.md)）。

### 1.8 Context Map 关系

| 关系方向 | 对端 | 关系类型 | 说明 |
|---|---|---|---|
| Cognition → TaskRuntime / Discussion / Conversation / Workforce / Bridge | 全部 | Customer-Supplier（**Customer 角色**）via CLI | Supervisor 通过 `Bash` 工具调 `agent-center <cmd>` 写状态；CLI handler 内部跑领域逻辑 + DecisionRecorder |
| Cognition ← TaskRuntime / Discussion / Conversation / Workforce | 全部 | **不订阅事件**；扫 EventRepository | SupervisorTriggerCoalescer 扫 events 表的 wake 白名单 → 派生 scope_key；不主动订阅，避免反向依赖（[00-overview § 0.3](../design/architecture/tactical/cognition/00-overview.md)）|
| Observability ← Cognition | Observability | Open Host | Phase 4 已建的 events 表 + inspect / query 直接复用；不为 supervisor 单造接口 |
| Cognition → Bridge outbound | Bridge | 间接（通过 emit `supervisor.invocation_failed_alert` 等事件）| Phase 5 Bridge 已订阅；Phase 6 仅 emit |
| Cognition ↔ Memory | 同 BC 内 | Aggregate-Aggregate（独立 AR）| Memory 物理形态 = 文件 + git；不走标准 Repository 抽象 |

---

## § 2. 上游依赖（来自 Phase 1-5 的工件）

| 上游 phase | 工件 | Phase 6 哪一步用 |
|---|---|---|
| Phase 1 | **EventRepository**（events 表 append-only + tx-via-ctx）| 3.1 写 invocation 行 + emit `supervisor.*` 事件；3.5 Coalescer 扫 events 表（按 occurred_at + event_type 白名单 + cursor）；3.7 DecisionRecorder 同 tx INSERT decision_records + Append event |
| Phase 1 | **EventSink** domain service | DecisionRecorder 内部走它；动作 CLI handler 也通过它 emit 联动事件 |
| Phase 1 | **Workforce / Conversation shared kernel**（User / Worker / Conversation VO）| Memory 路径派生：`projects/<X>/CLAUDE.md` / `workers/<W>/CLAUDE.md` / `conversations/<C>/CLAUDE.md` —— 需要稳定 ID 格式 |
| Phase 1 | **clock.Clock interface**（测试可注入）| Coalescer 30s / 5min 窗口判断；TimeoutHandler 超时判断；CrashRecovery `last_succeeded_invocation.started_at` 比较 |
| Phase 2 | **TaskRepository** | DecisionRecorder 同 tx 跑 task 状态机（如 `dispatch` 路径里），属于动作 CLI handler 内部行为；Phase 6 仅复用，不改 |
| Phase 2 | **TaskExecutionRepository** | 同上：`kill-execution` / `abandon-task` / `suspend-task` / `resume-task` 路径 |
| Phase 2 | **Agent CLI Adapter（claude-code）** | **关键复用**：`agent-center supervisor` 子进程内部 SupervisorSpawner 调 `agentadapter.claudecode.Adapter` spawn 真实 `claude` 子进程；走 BuildCommand / ParseEvent 等接口；**无 LLM SDK**（[ADR-0002](../design/decisions/0002-no-llm-sdk-use-cli-agents.md)）|
| Phase 2 | **per-execution shim 复用** | **不复用** —— 那是 worker daemon 路径；supervisor 路径是 center 同机直接 fork+exec，无 shim 层 |
| Phase 3 | **IssueRepository** | 动作 CLI handler：`issue open / comment / conclude / close / bind-conversation / link-conversation` 内部使用 |
| Phase 3 | **ConversationRepository / MessageRepository** | `conversation add-message` 路径 |
| Phase 3 | **InputRequestRepository** | `escalate-input-request` / `respond-to-input-request` 路径 |
| Phase 4 | **五动词 `inspect` / `query` / `ps` / `stats` / `logs`** | Supervisor 跑 prompt 时通过 `Bash` 工具调它们读 fleet 状态；**Phase 6 不重新实现**，只确认五动词 audience 含 `S` 标记，且 `kind=supervisor` / `kind=decision` 已就绪（[03-cli § 8.7](../design/implementation/03-cli-subcommands.md)）|
| Phase 4 | **peek-trace `<execution_id>`** | 同上 —— supervisor 决策时按需深挖 worker agent JSONL trace |
| Phase 4 | **EventsRepository.QueryByTimeWindow + cursor** | SupervisorTriggerCoalescer 扫 events 入口；InvocationCrashRecovery replay 逻辑 |
| Phase 5 | **Bridge outbound（飞书）** | Phase 6 emit `supervisor.invocation_failed_alert` 后由 Phase 5 Bridge 订阅 → 推飞书提醒人工 retrigger；Phase 6 **不引** vendor SDK，仅 emit 事件 |
| Phase 5 | **ChannelBinding / Identity** | `escalate-input-request` 路径需要查询 user 的 notification channel；Phase 5 工件已就绪 |

---

## § 3. 工作项分解

每个工件**完备交付**才能进下一步（[plans § 2.2](README.md) 半成品红线）；不允许"先 stub 后补"。

### 3.1 SupervisorInvocation AR + DecisionRecord 子从属 + Repository

**类型**：Aggregate Root + Entity + Repository

**输入**：Phase 1 EventRepository / EventSink / clock.Clock / tx-via-ctx / migration 框架

**输出**：
- `internal/cognition/aggregate/invocation.go` — `SupervisorInvocation` struct + 状态机方法（`Spawn` / `MarkSucceeded` / `MarkFailed` / `MarkTimedOut`）+ invariants 校验
- `internal/cognition/aggregate/decision.go` — `DecisionRecord` struct + `NewDecisionRecord` factory（rationale 必填校验）
- `internal/cognition/vo.go` — `InvocationStatus` / `InvocationScope` / `TriggerEventSet` / `InvocationOutcome` / `InvocationFailedReason` / `DecisionKind` / `DecisionOutcome` / `HardTimeout` / `TokenUsage` 类型
- `internal/cognition/repository.go` — `SupervisorInvocationRepository` / `DecisionRecordRepository` 接口 + sentinel errors（exported）
- `internal/persistence/cognition/invocation_repo.go` — SQLite 实现：CAS UPDATE（`version` 列）/ `UpdateStatusToTerminal` / `FindRunningByScope` / `Find` cursor 分页
- `internal/persistence/cognition/decision_repo.go` — append-only INSERT；`FindByInvocationID` / `Find` cursor 分页；Update/Delete 方法**不存在**（不实现 = 编译保证 immutable）
- `internal/persistence/migrations/000N_cognition.up.sql` / `.down.sql` — `supervisor_invocations`（13 字段 + version + created_at + updated_at）+ `decision_records`（append-only，无 version 无 updated_at）+ 索引

**实现步骤**：

1. 写 migration SQL（[02-persistence § 1 dialect 子集 / § 9.0 禁忌](../design/implementation/02-persistence-schema.md)：ULID TEXT PK / 时间 ISO8601 TEXT / version CAS / JSON TEXT 不查内容 / 无 SERIAL / 无 BOOLEAN / 无 FOR UPDATE）
2. 定义 VO 类型（VO 不可变 = 字段全小写不导出 + 构造函数返回 value type）
3. 定义 AR struct + invariants 校验（[01-supervisor-invocation § 5](../design/architecture/tactical/cognition/01-supervisor-invocation.md) 9 条）：
   - 无 pending 状态；行落座即 running
   - 终态字段一致：status != running 必须有 ended_at
   - `failed_reason` 必带 `failed_message`（[conventions § 16](../rules/conventions.md)）
   - `scope_kind` + `scope_key` 创建后不可变
   - `trigger_event_ids` ≥ 1
4. Repository 接口签名严格按 [cognition/00-overview § 5.1 / 5.2](../design/architecture/tactical/cognition/00-overview.md)；ctx 必带；filter struct + cursor 分页
5. SQLite 实现：CAS UPDATE 返回 `ErrInvocationVersionConflict`；INSERT 同 scope_key+status=running 已存在时返回 `ErrScopeKeyRunningExists`（通过 `UNIQUE INDEX (scope_kind, scope_key) WHERE status='running'` partial index 实现；SQLite 支持 partial index，[02-persistence § 1.3](../design/implementation/02-persistence-schema.md) v1 SQLite-only 例外）
6. DecisionRecord Repository 不暴露 Update / Delete；Append 内部 INSERT 后立即 `errors.Is(err, sqlite.UniqueViolation)` 转 `ErrDecisionImmutable`（PK 重复极端场景）

**与 [02-persistence § 8 BC 实现切片] 的对位**：
- 当前文档 § 8 仅展开 TaskRuntime + Observability；Cognition 留 § 8.X 占位（[02-persistence § 8 注](../design/implementation/02-persistence-schema.md)：其余 BC 落代码时按 § 1-7 套用）。Phase 6 落代码时把 Cognition 切片补到 `02-persistence-schema.md` § 8.3（含 DDL / Repository SQL 映射 / 关键实现要点）。
- `events.decision_id` 列已在 Observability § 8.2.1 就绪（Phase 1），Phase 6 仅消费。

**DoD**：
- [ ] migration up/down 在真实 SQLite 跑通（建表 / 删表 / 索引 / partial index 全过）
- [ ] AR 状态机所有合法跃迁 + 所有非法跃迁拒绝 都有单测（4 × 3 = 至少 12 case）
- [ ] Repository 单测：CAS 冲突 / ErrScopeKeyRunningExists / cursor 分页 / append-only 拒改
- [ ] sentinel errors 用 `errors.Is` 可判（不裸 string 比较）
- [ ] `go vet ./internal/cognition/...` 干净

---

### 3.2 Value Objects 集

**类型**：Value Objects（已合并到 § 3.1 文件内，独立小节强调"值语义 + 不可变 + 等值比较"）

**输入**：DDD 统一语言表（[03-bounded-contexts § 1](../design/architecture/strategic/03-bounded-contexts.md)）

**输出**：
- `internal/cognition/vo.go`（同 § 3.1）
- `internal/cognition/vo_test.go` — 等值比较 / 不可变 / 构造函数边界 / String() 表示

**关键设计**：

| VO | 设计要点 |
|---|---|
| `InvocationScope` | `type InvocationScope struct { Kind ScopeKind; Key string }`；Memory 路径派生用 `(scope) CWDPath() string` 方法；`scope_kind=global` 时 key 强制为 `"_global_"`，构造函数校验 |
| `TriggerEventSet` | `type TriggerEventSet []EventID`；构造校验 len ≥ 1；JSON marshal 按数组；查询时不在 SQL 里展开（[conventions § 9.0](../rules/conventions.md) 不查 JSON 内容） |
| `InvocationOutcome` | 终态聚合 struct，sealed —— `MarkSucceeded(tokens, decisions)` / `MarkFailed(reason, message)` / `MarkTimedOut(at)` 三个 builder；调用方拿不到裸字段构造 |
| `DecisionKind` | 12 种 enum；string 表示与 CLI 命令 1:1（`dispatch` / `kill_execution` / ... / `no_op`）；Parse 函数遇未知值返回 typed error，不兜底 `unknown`（[conventions § 17](../rules/conventions.md) 错误显式化）|
| `HardTimeout` | `type HardTimeout time.Duration`；`HardTimeoutFor(scope_kind)` 工厂返回 180s 或 600s |

**DoD**：
- [ ] 每个 VO 有 String() / JSON marshal / equal / 构造函数边界 case 单测
- [ ] DecisionKind 12 种全列 + parse 未知拒绝
- [ ] HardTimeoutFor 5 个 scope_kind 全覆盖
- [ ] vo_test.go 覆盖 ≥ 95%（VO 是简单纯逻辑，应近满覆盖）

---

### 3.3 Memory 包：file-based + git ops + skeleton factory

**类型**：Aggregate（独立路径，不走 SQL Repository）+ Domain Service

**输入**：Phase 1 EventSink；事件订阅注册点；`$AGENT_CENTER_MEMORY_DIR` 配置（[01-supervisor-invocation § 7](../design/architecture/tactical/cognition/01-supervisor-invocation.md)）

**输出**：
- `internal/cognition/memory/skeleton.go` — `MemorySkeletonFactory` 接口 + sentinel errors
- `internal/cognition/memory/skeleton_fs.go` — fs 实现：mkdir -p + 写 `CLAUDE.md`（H1 + 注释）+ `git add` + `git commit --author="system:bootstrap <bootstrap@agent-center.local>"`
- `internal/cognition/memory/gitops.go` — `MemoryGitOpsService` 接口
- `internal/cognition/memory/gitops_exec.go` — `os/exec` shell out `git -C $MEMORY_DIR ...`；`AutoCommitDirty` 检测 `git status --porcelain` → 有变化则 `git add -A` + `git commit --author="supervisor:<inv-id> <supervisor:<inv-id>@agent-center.local>"`
- `internal/cognition/memory/subscriber.go` — 订阅 `project.created` / `task.created` / `issue.opened` / `worker.enrolled` / `conversation.opened` 事件 → 调 Factory.CreateSkeleton
- `internal/cognition/memory/path.go` — `ScopeToFSPath(scope) string` 纯函数 + path traversal 防护（拒绝 `..`、绝对路径、null byte）
- `internal/cognition/memory/init.go` — center 启动时调用：检查 `$MEMORY_DIR` 是否已是 git 仓 → 否则 `git init` + 写 root `CLAUDE.md` + 初始 commit

**实现步骤**：

1. 定义接口（[00-overview § 5.3](../design/architecture/tactical/cognition/00-overview.md)）
2. `ScopeToFSPath` 纯函数：
   - `task:T-42` (in project X) → `projects/X/tasks/T-42/CLAUDE.md`（**project_id 怎么取**：从 task 行的 `project_id` 列；Skeleton subscriber 订阅 `task.created` 事件 payload 自带 `project_id`，无需反查）
   - `issue:I-7` (in project X) → `projects/X/issues/I-7/CLAUDE.md`
   - `conversation:C-3` → `conversations/C-3/CLAUDE.md`
   - `worker:W-1` → `workers/W-1/CLAUDE.md`
   - `global` → `CLAUDE.md`
   - `project:X` → `projects/X/CLAUDE.md`（Memory-only scope，Invocation 不用）
   - `supervisor` → `supervisor.md`（注意文件名不是 CLAUDE.md，[02-memory § 3.1](../design/architecture/tactical/cognition/02-memory.md)）
3. Skeleton subscriber 注册到 Phase 1 的 EventBus（实现层暂以 polling EventRepository.QueryByTimeWindow 模拟订阅；Phase 1 已就绪此 pattern）；`ErrMemoryFileExists` 时**幂等返回 nil**（多 worker 重放可能）
4. `gitops_exec.go` 用 `exec.CommandContext` + 测试时注入 `GitRunner` interface（mock 出命令拼装而不真跑 git）；真实测试用临时 dir + 真 git binary
5. **HOME 隔离**（[02-memory § 3.2](../design/architecture/tactical/cognition/02-memory.md)）：所有 git 命令带 `env GIT_AUTHOR_NAME / GIT_AUTHOR_EMAIL / HOME=<隔离 dir>`，避免吃用户全局 `.gitconfig` 的 sign / hook
6. **并发**：v1 不加锁（[02-memory § 5.2](../design/architecture/tactical/cognition/02-memory.md)）—— Coalescer per-scope 串行已经避免了同 scope 内 race；跨 scope 写 `global/CLAUDE.md` 的偶发 race 走 roadmap

**与实现层 schema 对位**：
- 不在 [02-persistence § 8](../design/implementation/02-persistence-schema.md) 切 —— Memory **零 SQL 表**（[02-memory § 7 invariant 2](../design/architecture/tactical/cognition/02-memory.md)）
- 配置项 `supervisor.memory_dir`（[01-supervisor-invocation § 7](../design/architecture/tactical/cognition/01-supervisor-invocation.md)） —— Phase 6 落代码时加到 `04-configuration § 7.3`

**DoD**：
- [ ] 7 种 scope 路径映射 + 5 种 Skeleton 触发事件全测
- [ ] git init 幂等 / commit author 注入 / dirty 检测 / clean 跳过 commit 都有用例
- [ ] path traversal 防护：`..` / 绝对路径 / null byte / 超长路径全部被 path.go 拒绝（fuzz 友好）
- [ ] HOME 隔离：测试启动时设临时 HOME，不污染 dev 机 `~/.gitconfig`
- [ ] GitRunner mock 走单测 / 真实 git 走集成测（标 `//go:build integration`）

---

### 3.4 SupervisorPromptAssembler

**类型**：Domain Service（属 `agent-center supervisor` 子进程内部）

**输入**：embed 的 `supervisor.md` skill / Memory ancestor walk（由 claude code 原生承担，PromptAssembler 只设 CWD）/ trigger_event_ids 摘要（从 EventRepository 反查）

**输出**：
- `assets/skills/supervisor.md` — Skill 文档源码（binary embed，[agent-harness/02 § skill-files](../design/architecture/tactical/agent-harness/02-skill-cli-tooling.md)）；内容包含：角色说明 / `Always Read supervisor.md first` 硬指令 / 12 种 decision kind 的何时调 / `Bash` 工具使用约定 / CLI 命令清单
- `internal/cli/supervisor/skills.go` — Go `embed.FS` 嵌入 supervisor.md
- `internal/cli/supervisor/prompt.go` — `SupervisorPromptAssembler` 类型：
  - 输入 `(scope InvocationScope, triggerEvents []Event)` → 输出 `final_prompt string` + `claude_cwd string`
  - 内部：把 skill 文档作为 system block；trigger events 摘要追加（events 数组按 occurred_at 倒序，每条一行：`[ULID] event_type refs payload-excerpt`）
  - CWD 按 scope 映射（复用 § 3.3 ScopeToFSPath）；claude code 启动后自动 ancestor walk 加载链上 CLAUDE.md
  - **supervisor.md 自反 memory** 不在 ancestor walk 路径上 —— skill 文档顶部硬指令 `Always Read $AGENT_CENTER_MEMORY_DIR/supervisor.md first` 处理
  - prompt 全文 > 10 KB 时走 BlobStore（[01-supervisor-invocation § 3 prompt_blob_ref](../design/architecture/tactical/cognition/01-supervisor-invocation.md), [conventions § 8](../rules/conventions.md)）—— 实现：超过阈值则 Put 到 BlobStore + invocation 行 `prompt_blob_ref` 字段填路径，CmdSpec 内仍传 ≤10KB 摘要 + ref

**实现步骤**：

1. 写 `assets/skills/supervisor.md`：参考 [01-supervisor-invocation § 4.4 12 种 decision kind](../design/architecture/tactical/cognition/01-supervisor-invocation.md) + [00-overview § 7.1 CLI 表](../design/architecture/tactical/cognition/00-overview.md) + [02-memory § 3.1 / § 5](../design/architecture/tactical/cognition/02-memory.md)（"Memory 是你的私事，自决压缩"）
2. embed 在 binary：`//go:embed supervisor.md`
3. PromptAssembler 是**纯函数**（除 EventRepository.FindByIDs 反查 trigger events）：输入 scope + events → 输出 string + CWD path；无 IO 侧效应（侧效应在 Spawner 内）
4. 输出格式由测试黄金文件（`testdata/prompt_*.golden.txt`）固化；prompt schema 变化 = golden 文件更新 + 测试通过

**DoD**：
- [ ] 5 种 scope × 1 / 5 / 50 trigger events 的 golden 测试全 pass
- [ ] prompt > 10 KB 时 blob_ref 路径生效（mock BlobStore）
- [ ] supervisor.md skill 渲染后包含 12 种 decision kind 全列 + Memory 自决说明 + CLI 用法（grep golden 即可）
- [ ] embed.FS 在测试中读到 supervisor.md 非空

---

### 3.5 SupervisorTriggerCoalescer

**类型**：Domain Service（center 进程内常驻 goroutine）

**输入**：Phase 1 EventRepository.QueryByTimeWindow + cursor / clock.Clock / config（max_concurrent / window / cap）

**输出**：
- `internal/cognition/scheduler/coalescer.go` — `SupervisorTriggerCoalescer` struct：
  - `Run(ctx)` 启动 goroutine 循环：cursor 推进 → 拉 events → 白名单过滤 → 派生 scope_key → 入 per-scope window
  - per-scope state map：`map[ScopeKey]*window`；window struct = `{first_at, last_at, event_ids []EventID, mu sync.Mutex}`
  - 关窗条件（OR）：`clock.Now() - last_at >= 30s`（滚动）/ `clock.Now() - first_at >= 5min`（硬上限）
  - 关窗 → 调 SpawnQueue（§ 3.6）；不直接 spawn
  - 同 scope 已有 running invocation（`FindRunningByScope` 命中）→ 不关窗，等当前 invocation 结束（通过订阅 `supervisor.invocation_*` 终态事件触发关窗判定）
- `internal/cognition/scheduler/whitelist.go` — `IsWakeEvent(event_type) bool` + `RouteToScope(event_type, refs) (scope, ok)`：白名单严格按 [00-overview § 3.2 v1 白名单](../design/architecture/tactical/cognition/00-overview.md) 闭集；遇未知事件返回 `(scope, false)` 跳过（**不上报 Unknown event**：未知事件不在 Coalescer 职责，由 Phase 4 adapter unknown 路径负责，[conventions § 17 例外白名单](../rules/conventions.md)）
- `internal/cognition/scheduler/whitelist_test.go` — 表驱动：每个白名单 event_type 路由到正确 scope；非白名单全部跳过；refs 缺字段（如 `task.*` 缺 `task_id`）→ `ok=false`
- `internal/cognition/scheduler/queue.go` — 全局 FIFO SpawnQueue + `max_concurrent_invocations=5` 上限；in-memory channel/list

**实现步骤**：

1. 白名单 + 路由函数（纯函数，最易测）
2. coalescer 主循环：
   ```
   for ctx not done {
     events := EventRepo.QueryByTimeWindow(cursor, batch_size)
     for e in events:
       if !IsWakeEvent(e.Type): cursor = e.ID; continue
       scope, ok := RouteToScope(e.Type, e.Refs); if !ok: emit Unknown_route_event_warning; continue
       windows[scope].push(e, clock.Now())
       cursor = e.ID
     for scope, w in windows:
       if w.ready(clock.Now()):
         SpawnQueue.Enqueue(InvocationRequest{scope, w.event_ids})
         delete(windows, scope)
     <-clock.After(check_interval)  // 1s 默认；可注入
   }
   ```
3. 订阅 `supervisor.invocation_succeeded` / `supervisor.invocation_failed_alert` / `supervisor.invocation_timed_out` 终态事件 → 同 scope 的下一批可以出队 spawn
4. Crash recovery（§ 3.9 InvocationCrashRecovery）在 Run 之前一次性跑完：扫 status=running 孤儿改 failed + replay 未覆盖事件重建 window

**与 [ADR-0013 § 3 § 4 § 7 § 9] 对位**：滚动 30s / 硬上限 5min / 跨 scope 冲突由底层兜底 / `supervisor.*` 反循环。

**DoD**：
- [ ] 白名单路由表驱动单测：18 个 wake event_type × refs 全覆盖 + 拒绝非白名单
- [ ] window 边界测试（注入 clock）：
  - 单事件 30s 后关窗
  - 5 个事件 6s 间隔 → 滚动延期 → 30s after 最后一个关窗
  - 高频流（1s 一个）→ 5min 硬上限关窗
  - 同 scope 第二批在第一批 running 时不关窗 → 第一批终态事件后关窗
- [ ] 并发：5 个 scope 并行入 window + 全部关窗 → 全部进 queue，但 max=5 时 queue 容纳；max=2 时只 2 个出队
- [ ] cursor 推进幂等：crash → restart 不会双倍处理（依赖 InvocationCrashRecovery 的 replay 边界）
- [ ] reflective panic 不会 kill 整个 goroutine：每个 event 处理 panic 隔离 + emit `supervisor.scheduler_panic_alert`

---

### 3.6 SupervisorSpawner（fork+exec `agent-center supervisor` 子进程）

**类型**：Domain Service + Application Service

**输入**：SpawnQueue 出队的 InvocationRequest / agentadapter（Phase 2，**仅用于子进程内部**；Spawner 自己只 spawn `agent-center supervisor`） / config

**输出**：
- `internal/cognition/scheduler/spawner.go` — `SupervisorSpawner`：
  - `Spawn(ctx, req) (InvocationID, error)`：
    1. CAS-style insert `supervisor_invocations` 行（status=running，id 用 ULID）；遇 `ErrScopeKeyRunningExists` → 返回 nil（已被另一路径起；理论上 Coalescer 不会双起，但保护层）
    2. emit `supervisor.invocation_started` event（同 tx）
    3. fork+exec `agent-center supervisor --scope=task:T-42 --invocation-id=<id> --trigger-events=<csv>`；env 注入 `AGENT_CENTER_INVOCATION_ID=<id>`、`HOME=<隔离 dir>`、`CLAUDE_CONFIG_DIR=<隔离 dir>`、`GIT_AUTHOR_NAME=supervisor`、`GIT_AUTHOR_EMAIL=supervisor:<id>@agent-center.local`、`AGENT_CENTER_MEMORY_DIR=<root>`
    4. 不等子进程退出 —— 异步 goroutine `cmd.Wait()` 拿 exit code → 写终态：
       - exit 0 → `MarkSucceeded` + emit `supervisor.invocation_succeeded`
       - exit ≠ 0 → `MarkFailed(reason=claude_nonzero, message="exit_code=N stderr=...")` + emit `supervisor.invocation_failed_alert`
       - context cancelled by TimeoutHandler → `MarkTimedOut` + emit `supervisor.invocation_timed_out`
    5. 终态写入后通知 Coalescer 同 scope 可出队（如 § 3.5 描述）
  - 子进程退出后 token_usage 怎么回填：子进程退出前最后一步把 claude 子进程的 `usage` JSONL 行汇总写到 `~/.agent-center/invocations/<id>.usage.json` 临时文件 → Spawner 在 Wait 后读取 → MarkSucceeded 时填入
- `internal/cli/supervisor/run.go` — `supervisor run` CLI handler（即 `agent-center supervisor --scope=... --invocation-id=...`）：
  1. 解析 CLI flag → scope / invocation_id / trigger_event_ids
  2. 拉 Memory CWD（§ 3.3 ScopeToFSPath；不存在则拒绝退出，emit cli_command_error）
  3. PromptAssembler 拼 prompt（§ 3.4）
  4. 调 `agentadapter.claudecode.Adapter.BuildCommand(SpawnRequest{...})` 拿 CmdSpec → 直接 `exec.Command` 跑 claude，stdout 走 JSONL pipe 解析 → 写 trace.jsonl + 累计 token usage
  5. claude exit → 写 token_usage 文件 → 子进程退出（exit code 透传 claude 的 exit code；动作 CLI 内部失败已通过 DecisionRecorder 反映到 events 表，这里仅决定 invocation 终态）

**关键区分**：
- `SupervisorSpawner`（center 主进程内）只 spawn `agent-center supervisor`（自家 binary 子命令），**不**直接 spawn `claude`
- `supervisor run` CLI handler（在子进程内）才 spawn 真实 `claude`（通过 Phase 2 的 claudecode adapter）
- 这两层分离让 SupervisorSpawner 单测**完全不用 claude binary** —— 用 mock CLI（`internal/agentadapter/testing/fake_claude.sh`）替换 `agent_cli.claude_code.binary` 配置即可
- per-execution shim（[ADR-0018](../design/decisions/0018-detached-agent-via-per-execution-shim.md)）**不复用** —— shim 是 worker daemon 路径，supervisor 是 center 同机短命路径，直接 fork+exec 即可

**与实现层 [05-agent-adapters § 6 Prompt 拼装职责]** 对位：worker-side prompt 由 worker daemon 拼；supervisor-side prompt 由 SupervisorPromptAssembler 拼；两套独立但都走同一个 `agentadapter.Adapter` 接口下沉到 BuildCommand → CmdSpec → exec。

**DoD**：
- [ ] Spawner 用 mock CLI（fake_claude.sh exit 0 / exit 1 / exit 137 / 永不退出）覆盖 4 种终态
- [ ] token_usage 回填路径：fake CLI 写 usage 文件 → Spawner 读到 → 落库
- [ ] HOME / CLAUDE_CONFIG_DIR / GIT_AUTHOR_* env 真实注入（用 fake_claude.sh `env | grep` 断言）
- [ ] CWD 按 scope 正确设置（fake_claude.sh `pwd` 断言）
- [ ] subprocess panic / OOM 注入（mock 返回 exit 137） → reason=oom 分类（reason 推断规则：exit -1/SIGKILL 且 stderr 含 "out of memory" / cgroup oom_score → oom；其它 ≠ 0 → claude_nonzero）

---

### 3.7 DecisionRecorder（同事务双写 + emit）

**类型**：Domain Service（动作 CLI handler 内部调用）

**输入**：Phase 1 EventSink + tx-via-ctx；Phase 2-5 各动作 CLI handler

**输出**：
- `internal/cognition/decision/recorder.go` — `DecisionRecorder` 类型：
  - `Record(ctx, RecordRequest) error` 在调用方已经 BEGIN TRANSACTION 的 ctx 内执行：
    1. 校验 `RecordRequest`：rationale ≠ ""；invocation_id 必填且对应行 status=running（**不**校验是否 actor 一致，权威由 env `AGENT_CENTER_INVOCATION_ID` 推断）
    2. 校验失败 → 返回 `ErrRationaleRequired` 等 sentinel；调用方 ROLLBACK
    3. INSERT decision_records（outcome 由调用方传入：成功路径 `outcome=succeeded`；catch err 路径 `outcome=failed` + outcome_message）
    4. 关联 events：调用方 emit 联动 domain event 时把返回的 `decision_id` 填入 `events.decision_id` 列（同 tx）
  - `RecordRequest` struct: `{InvocationID, Kind, TargetRefs(JSON), Rationale, Outcome, OutcomeMessage}`
- Actor 推断 helper `internal/cli/actor.go` — `InferActor(env) Actor`：env `AGENT_CENTER_INVOCATION_ID` 有 → `supervisor:<id>` + 写 decision_records；无 → `user:<id>`（按 socket 当前 user）+ **不写** decision_records；events.actor 字段双方都填（[01-supervisor-invocation § 4.8](../design/architecture/tactical/cognition/01-supervisor-invocation.md)）

**改动到的现有 CLI handler**（Phase 2-5 已有，Phase 6 在内部插入 DecisionRecorder 调用）：

> 注：v1 没有独立的 `abandon-task` / `suspend-task` / `resume-task` 顶层命令；这三个 DecisionKind 通过 `kill-execution --reason=abandon_precondition|suspend_precondition` 派生（CLI handler 按 reason 选择 DecisionKind）。`resume_task` 没有对应 CLI，由 supervisor 通过 `record-decision --kind=no_op` 附 rationale 留痕（v2 可以单独建命令）。`issue close` 在代码里写作 `issue withdraw`（命名差异但语义对应 `close_issue` kind）。

| CLI 命令 | 改动位置 | 插入 DecisionRecord.kind |
|---|---|---|
| `dispatch` | Phase 2 handler | `dispatch` |
| `kill-execution` | Phase 2 handler | `kill_execution`（默认）/ `abandon_task`（reason=abandon_precondition）/ `suspend_task`（reason=suspend_precondition）|
| `issue open` / `issue comment` / `issue conclude` / `issue withdraw` | Phase 3 handler | `open_issue` / `issue_comment` / `conclude_issue`（withdrawn 走 `close_issue`）/ `close_issue` |
| `conversation add-message` | Phase 3 handler | `conversation_message` |
| `escalate-input-request` | Phase 6 新增（§ 3.8）| `escalate_input_request` |
| `record-decision` | Phase 6 新增（§ 3.8）| `no_op` |

**实现机制**（DoD 收口补交）：CLI handler 用 `runSupervisorActionTx(ctx, app, actionFn, kind, refsJSON, rationale)` 统一封装；该 helper 开 outer tx，调 actionFn（action service 内部 `persistence.RunInTx` 因 tx-reentrant 自动 join outer tx），同 tx 写 DecisionRecord。任一失败回滚整笔。`--rationale` 在 supervisor caller path 必填，user caller path 不要求（user 不写 decision_records）。

**关键不变量**（[01-supervisor-invocation § 4.5 / § 4.7](../design/architecture/tactical/cognition/01-supervisor-invocation.md)）：
- append-only；INSERT 后 outcome 字段也不变（包括失败路径的 outcome_message）
- rationale 必填 —— actor=supervisor 时 CLI handler 没传 `--rationale` flag 直接拒绝执行（exit code 4，[03-cli § 5 exit code](../design/implementation/03-cli-subcommands.md)）；actor=user 时不要求（user 路径不写 decision_records）
- outcome 只反映**CLI 同步结果**；下游异步状态走 `events.correlation_id=invocation_id` JOIN

**与 [ADR-0014 同事务原则](../design/decisions/0014-event-sourcing-level.md) 对位**：状态表 UPDATE + decision_records INSERT + events INSERT 三表同 tx；任一失败回滚整笔。

**DoD**：
- [ ] DecisionRecorder 单测：
  - succeeded 路径：状态表 + decision_records + events 三方都写
  - failed 路径：状态表 UPDATE 不发生，但 decision_records `outcome=failed` 写入 + **不** emit 联动事件
  - rationale 缺失：返回 ErrRationaleRequired，tx rollback，全无副作用
  - actor=user：不写 decision_records，events.actor=user:<id>，events.decision_id=NULL
- [ ] 集成测试：跨 Phase 2 dispatch handler 跑一遍，断言三表同步
- [ ] events.decision_id JOIN decision_records 查到 rationale（query 单测）

---

### 3.8 CLI handlers（supervisor retrigger / record-decision / escalate-input-request）

**类型**：Application Service

**输入**：Phase 1 EventRepository / Phase 2 InputRequestRepository / § 3.1 SupervisorInvocationRepository / § 3.7 DecisionRecorder

**输出**：3 个 CLI handler + 测试 + 注册到 cobra（或当前用的 CLI 框架）+ 命令 `--help` 文本对齐 [03-cli § 8.5](../design/implementation/03-cli-subcommands.md)

#### 3.8.1 `agent-center supervisor retrigger <invocation_id>`

audience=U（人工）；不写 decision_records（不在 invocation 内）。流程：

1. `FindByID(invocation_id)` → 必须 status ∈ {failed, timed_out}；其它状态拒（exit code 4 + "only failed/timed_out invocations can be retriggered"）
2. 复制其 `scope` + `trigger_event_ids` → 创建新 `InvocationRequest` → 直接调 `SupervisorSpawner.Spawn`（绕过 Coalescer）
3. emit `supervisor.retriggered` event（refs={prev_invocation_id, new_invocation_id}）

#### 3.8.2 `agent-center record-decision --invocation=<id> --kind=no_op --target=task:T-39 --rationale=...`

audience=S（supervisor agent 唯一调用方）。流程：

1. 校验 env `AGENT_CENTER_INVOCATION_ID` == `--invocation` flag（不一致拒，防止 supervisor agent 写错 invocation）
2. 校验 `--kind` 必须 `no_op`（其它 kind 由各动作 CLI 自动写，不允许 supervisor 显式调 record-decision 写 `dispatch` 等 kind —— 防止"假装动作发生但没真发生"）
3. 调 DecisionRecorder.Record（kind=no_op, outcome=succeeded）
4. 不 emit 任何 domain event（no_op 没有联动事件）

#### 3.8.3 `agent-center escalate-input-request <input_request_id>`

audience=S。跨 BC 命令（属 Cognition，写 TaskRuntime InputRequest 状态）。流程：

1. `InputRequestRepository.FindByID` → 必须 status=pending
2. 推断目标 user → 查 ChannelBinding（Phase 5 工件）拿 notification channel
3. 同 tx：
   - UPDATE input_requests SET `escalated_to_channel=<channel>`（新加列，Phase 6 migration 拆出来；或 Phase 3 已留位 —— 落代码时 reconcile）
   - DecisionRecorder.Record(kind=escalate_input_request, target={input_request_id, task_id, channel}, rationale=（来自 --rationale flag）)
   - emit `input_request.escalated`
4. Phase 5 Bridge 订阅 `input_request.escalated` → 推飞书

**DoD**：
- [ ] 3 个 handler 各自 success / fail / 边界（已终态 / actor 不匹配 / 不存在）case 全测
- [ ] `--help` 文本含 audience tag（U / S）+ 例子；CI 校验 skill 文档（supervisor.md）里 S 类命令列表跟实际 CLI 一致

---

### 3.9 端到端验证（事件触发 → coalesce → spawn → 决策 → 派单 → worker 接到）

**类型**：e2e 测试 + InvocationTimeoutHandler / InvocationCrashRecovery 实现

**输入**：所有上面的工件 + Phase 1-5 完整栈

**输出**：
- `internal/cognition/scheduler/timeout.go` — `InvocationTimeoutHandler.Run(ctx)`：每秒扫 running 行 → `clock.Now() - started_at >= hard_timeout_seconds` → 给对应子进程 SIGTERM → 5s grace（注入 clock）→ SIGKILL → MarkTimedOut + emit
- `internal/cognition/scheduler/crash_recovery.go` — `InvocationCrashRecovery.Recover(ctx)`：center 启动时一次性调用
  1. 扫 status=running → MarkFailed(reason=center_restart_orphan, message="center crashed at <last_known_alive>") + emit alert
  2. 按 scope_kind/scope_key 扫 events 表：`occurred_at > last_succeeded_invocation.started_at AND id NOT IN any trigger_event_ids` 的事件
  3. 这些是"未被任何成功 invocation 覆盖"的事件 → 写回 SupervisorTriggerCoalescer 的初始 window → 正常路径接管
- `tests/e2e/cognition/supervisor_e2e_test.go` — 真实 SQLite + fake_claude.sh + 真 git binary 的端到端测试

**e2e 场景**：
1. 用户通过 CLI `agent-center dispatch <task>` 不可能 —— 因为没 task。从 fixture 起步：
   - SetUp：seed 一个 task (T-1) + 一个 worker (W-1)；触发 `task.created` 事件
   - MemorySkeletonFactory subscriber tick → 建 `projects/X/tasks/T-1/CLAUDE.md`
   - SupervisorTriggerCoalescer tick → window for `task:T-1` → 30s 后（注入 clock 跳跃）关窗 → SpawnQueue
   - SupervisorSpawner spawn `agent-center supervisor --scope=task:T-1 --invocation-id=I-1 --trigger-events=E-1`
   - 子进程内部：PromptAssembler 拼 prompt（注入 fake claude config，CWD=projects/X/tasks/T-1） → 调 `agentadapter.claudecode.Adapter`（binary 配置成 fake_claude.sh） → fake_claude.sh 模拟 claude，stdout 输出预设 JSONL：
     ```
     {"type":"thinking","text":"T-1 should be dispatched to W-1"}
     {"type":"tool_use","name":"Bash","input":{"command":"agent-center dispatch T-1 --worker=W-1 --rationale='W-1 idle, T-1 high prio'"}}
     {"type":"tool_result","tool_use_id":"...","content":"OK execution_id=E-1"}
     {"type":"usage","input_tokens":100,"output_tokens":50}
     {"type":"end_turn"}
     ```
     fake_claude.sh 内部确实跑 `agent-center dispatch ...`（同一 binary 子命令）—— 走 Phase 2 dispatch handler → DecisionRecorder.Record + emit `task.dispatched`
   - fake_claude.sh exit 0 → 子进程退出 0 → SupervisorSpawner.MarkSucceeded + emit `supervisor.invocation_succeeded`
2. **断言**：
   - `supervisor_invocations` 表有 1 行 status=succeeded，token_usage 填了 input=100/output=50
   - `decision_records` 表有 1 行 kind=dispatch, rationale="W-1 idle, T-1 high prio", outcome=succeeded
   - `events` 表有 `task.created` / `task.dispatched`(decision_id=D-1) / `supervisor.invocation_started` / `supervisor.invocation_succeeded`
   - Memory: `projects/X/tasks/T-1/CLAUDE.md` 存在，git log 有 system:bootstrap 的 init commit + (如果 fake_claude.sh 模拟 Edit 工具的话) 一条 supervisor commit
   - TaskRuntime: `task_executions` 表有新行 status=submitted（等 worker ACK）
   - 模拟 worker daemon ACK → status=working → 完成路径走 Phase 2 已测
3. **额外路径**：
   - Timeout：fake_claude.sh 跑 sleep 999 → 注入 clock 跳到 181s → TimeoutHandler SIGTERM → 5s grace（注入跳到 186s）→ SIGKILL → status=timed_out + emit alert
   - Crash recovery：杀掉 center 进程 → invocation 行 status=running → 重启 center → CrashRecovery 把它改 failed(center_restart_orphan) + alert + replay 未覆盖事件
   - Failed alert → Phase 5 Bridge 订阅推飞书（用 mock vendor server 断言）—— **Phase 5 工件复用**

**DoD**：
- [ ] e2e happy path 跑通 + 所有断言 pass
- [ ] timeout / crash recovery / failed alert 三条异常路径 e2e pass
- [ ] 全部用 mock CLI / mock vendor server，**零真 claude binary 调用 + 零真飞书 API**
- [ ] 时间穿越用 clock.Clock；测试时长 ≤ 30s（不实际 sleep）

---

## § 4. Definition of Done（整体）

- [ ] § 1 所有工件实现并通过单元测试
- [ ] § 5 所有测试场景通过（单测 + 集成 + e2e）
- [ ] 单测行覆盖率 ≥ 90%（整体 + diff；`go test -cover` 报告归档）
- [ ] 测试报告归档到 `docs/plans/reports/phase-6-test-report.md`
- [ ] 触发的所有 domain event（7 类，§ 1.7）实际进 events 表，集成测试验证 schema + actor + decision_id 关联
- [ ] CLI 命令 `--help` 跟 [03-cli § 8.5 / § 8.8](../design/implementation/03-cli-subcommands.md) 对齐：`supervisor` / `supervisor retrigger` / `record-decision` / `escalate-input-request` 四条
- [ ] `assets/skills/supervisor.md` 跟 [01-supervisor-invocation § 4.4 12 decision kind](../design/architecture/tactical/cognition/01-supervisor-invocation.md) + [00-overview § 7.1 跨 BC CLI 映射表](../design/architecture/tactical/cognition/00-overview.md) 一致；CI 校验 skill ↔ CLI 一致性（[03-cli § 7](../design/implementation/03-cli-subcommands.md)）
- [ ] 配置 `supervisor.*`（[04-config § 7.3](../design/implementation/04-configuration.md)）+ `supervisor.memory_dir` 全部接通；env override 也通；`agent-center supervisor` 子命令明确**不读 config**（[04-config § 1 表第 2 行](../design/implementation/04-configuration.md)）
- [ ] `internal/cognition/...` + `internal/persistence/cognition/...` + `internal/cli/supervisor/...` + `assets/skills/supervisor.md` 通过 `golangci-lint` + `go vet` + `go test ./... -race`
- [ ] **零 LLM SDK 依赖** 验证：`go mod why github.com/anthropics/... github.com/openai/...` 全部 "not used"（[conventions § 4](../rules/conventions.md)）；CI 加 grep guard
- [ ] § 6 风险项每条已处置或显式 defer 到具体后续 phase

---

## § 5. 测试计划

### 5.1 单测场景（按工件分类）

| 工件 | 测试场景 | 关键断言 |
|---|---|---|
| SupervisorInvocation AR | 4 状态机合法跃迁（running → succeeded / failed / timed_out / 自跃迁 reject）| 状态字段 + ended_at + reason+message 一致；非法跃迁 panic |
| SupervisorInvocation AR | invariants（trigger_event_ids ≥ 1 / failed_reason 必带 message / scope 不可变 / version CAS）| 违反 invariant 构造拒绝；CAS 冲突返回 sentinel |
| DecisionRecord Entity | rationale 必填 / kind 闭集 / append-only（Update 不存在 = 编译保证） | NewDecisionRecord("") 拒绝；ParseDecisionKind("xxx") 返回 typed error |
| VO（9 种）| 等值 / 不可变 / JSON marshal / 边界 | golden 值固化；DecisionKind 12 种 parse 全过 |
| SupervisorInvocationRepository | CAS UPDATE 冲突 / ErrScopeKeyRunningExists（partial unique index）/ cursor 分页 / FindRunningByScope 命中 + miss | 错误用 sentinel `errors.Is` 可判 |
| DecisionRecordRepository | Append → FindByInvocationID 返回；同 ID 重复 INSERT 返回 ErrDecisionImmutable；filter + cursor | 排序按 created_at ASC |
| MemorySkeletonFactory | 7 种 scope 路径映射 / 5 种 lifecycle 事件触发建文件 / 幂等（文件已存在返回 nil）/ path traversal 拒绝 | git commit author=`system:bootstrap`；存在性可 stat |
| MemoryGitOpsService | AutoCommitDirty：clean 不 commit / dirty add+commit / author env 注入正确 | mock GitRunner 拼装命令断言；真实 git 集成测 |
| ScopeToFSPath（path.go）| 7 scope × 含/不含 project_id 全表 + path traversal fuzz | 输出与 [02-memory § 1 目录结构](../design/architecture/tactical/cognition/02-memory.md) 一致 |
| SupervisorPromptAssembler | 5 scope × 1/5/50 events golden 测；> 10 KB 走 BlobStore | golden 文件比对；blob_ref 字段填写 |
| Skill embed | supervisor.md 嵌入非空；含 12 decision kind / Memory 自决说明 | grep 关键字 |
| SupervisorTriggerCoalescer | 白名单路由（18 type × 5 scope）；window 边界（30s 滚动 / 5min 硬上限 / 同 scope 单活）；FIFO 队列上限（max=2 / max=5）；cursor 推进；panic 隔离 | 注入 clock；断言 SpawnQueue 内容 |
| RouteToScope | 非白名单事件 / refs 缺字段 / refs 异常类型 | `(scope, false)` 全部 |
| SupervisorSpawner | mock CLI 4 种 exit（0 / 1 / 137 / context cancel）→ 4 种终态；token_usage 文件回填；HOME/CWD/env 正确注入；同 scope 已 running 拒绝 spawn | fake_claude.sh 断言 + DB 查询 |
| `supervisor run` handler | scope 不存在 / prompt > 10 KB / fake claude crash / Memory CWD missing | reason 分类正确（cli_command_error / oom / claude_nonzero）|
| DecisionRecorder | succeeded 三表同写 / failed 仅写 decision_records / rationale 缺 reject / actor 推断（env 有 = supervisor，无 = user）| 同 tx 校验；events.decision_id JOIN 反查 rationale 通 |
| `supervisor retrigger` handler | running invocation 拒 / succeeded invocation 拒 / failed invocation 起新 / emit `supervisor.retriggered` | exit code + 新 invocation_id 输出 |
| `record-decision` handler | env 不匹配拒 / kind != no_op 拒 / rationale 缺拒 / 成功路径 | 仅写 decision_records，无 emit |
| `escalate-input-request` handler | pending → escalated（DB + decision_record + event 同 tx）；非 pending 拒 | Phase 5 Bridge 订阅可见 event |
| InvocationTimeoutHandler | running 行命中 timeout → SIGTERM 5s grace → SIGKILL → MarkTimedOut；未命中不动；context cancel 干净退出 | 注入 clock；SIGTERM 后子进程未退则 SIGKILL |
| InvocationCrashRecovery | running 行 → failed(center_restart_orphan) + emit；未覆盖事件 replay 进 Coalescer window；重复 recover 幂等 | events 表 replay 后窗口确实关窗了 |

**单测异常路径覆盖**（[testing.md § 2.1](../rules/testing.md)）：
- DB tx 中途失败 → 全表回滚
- SQLite busy / locked → 重试上限 / 报错
- git command nonzero exit → ErrMemoryGitOpFailed
- fake_claude.sh stdout 损坏 JSON → adapter EventUnknown，trace 仍归档
- EventRepository 拉空 → coalescer 不卡死
- 时钟回退（clock skew）→ window 不死锁
- subprocess SIGKILL 后 Wait 收到 ProcessState != nil

### 5.2 集成测试场景

| 场景 | 涉及工件 | 关键断言 |
|---|---|---|
| 跨 phase tx：dispatch CLI → tasks UPDATE + decision_records INSERT + events INSERT 三表同 tx | Phase 2 dispatch handler + DecisionRecorder + Phase 1 EventSink | 任一失败回滚整笔；events.decision_id JOIN 通 |
| 跨 phase tx：issue conclude → issues UPDATE + N task INSERT + N decision_records 子条 + events | Phase 3 issue handler + DecisionRecorder | 多 task 同事务；rationale 落每个 decision_record |
| Memory 真实 git：init + skeleton + supervisor AutoCommitDirty | MemoryGitOpsService 真实 git binary | `git log` 三条提交 + author 区分 |
| Memory subscriber 真实事件循环：emit `task.created` → 100ms 内文件存在 | MemorySkeletonFactory subscriber + Phase 1 EventBus | 文件 stat + git log 验证 |
| 状态机全路径：spawn → running → 各终态 → emit 联动 → Coalescer 同 scope 可出队下一批 | 整 Cognition BC | 状态机 4 态全覆盖；events 顺序正确 |
| Crash recovery：注入 status=running 行 + 后续未覆盖事件 → Recover → events 重新进 window 关窗 spawn | InvocationCrashRecovery + Coalescer + Spawner | 不丢事件 + 不重复处理（at-least-once 边界） |
| 配置加载：yaml + env override → SupervisorTriggerCoalescer 用对的 window / max | [04-config § 7.3](../design/implementation/04-configuration.md) loader + Phase 6 工件 | 30s 改 5s 测试快 |

### 5.3 e2e 测试场景

| 场景 | 用户/触发视角 / 入口 CLI | 关键断言 |
|---|---|---|
| **happy path**：task.created → coalesce → supervisor spawn → fake claude → dispatch → execution submitted | seed task + worker；clock 跳 30s；外部观察 events 表 + worker daemon mock 收到 envelope | events 7 条按顺序；decision_records 1 条；TaskExecution 1 行 status=submitted |
| **失败 alert**：fake_claude exit 1 → invocation_failed_alert → Bridge mock 推飞书 | seed + 让 fake_claude.sh 早退 | events 含 `supervisor.invocation_failed_alert`；mock 飞书 server 收到 outbound 请求（Phase 5 路径） |
| **timeout**：fake_claude sleep 999 → 181s（注入）→ SIGTERM → 5s grace → SIGKILL → timed_out | seed + clock 跳跃 | status=timed_out；ended_at 填写；alert emit |
| **retrigger**：失败 invocation → `supervisor retrigger I-1` → 新 I-2 起 | 上一场景结束后人工 CLI | 新 invocation 用同 trigger_event_ids；emit `supervisor.retriggered` |
| **escalate input request**：fake_claude → `escalate-input-request IR-1 --rationale=...` → Phase 5 Bridge 推飞书 | seed input_request + Channel binding | input_requests 状态变更 + decision_record(kind=escalate_input_request) + event 同 tx |
| **center crash recovery**：杀 center → 重启 → status=running 孤儿改 failed → 未覆盖事件 replay 关窗 → 新 invocation spawn | 测试 harness 模拟 SIGKILL center | 不丢事件；不双处理 |
| **跨 scope 并行**：seed 2 task + 2 issue → 4 个 scope window → max_concurrent=2 → 2 个并行 spawn + 2 个排队 | 并发触发 | 任一时刻 ≤ 2 个 running invocation；FIFO 顺序 |

**测试基础设施**：
- `tests/e2e/cognition/harness.go` — 起 center + mock worker daemon + mock vendor server（Phase 5 已建）+ fake_claude.sh 配置
- `tests/e2e/cognition/fake_claude.sh` — 接 `--session-id` / `--output-format` / `-p` flag；按 `AGENT_CENTER_FAKE_CLAUDE_SCRIPT` env 选脚本走（dispatch / fail / timeout / escalate / 等）
- 真实 SQLite 临时文件 / 真实 git binary / 真实 fork+exec；**零** claude binary / 飞书 API
- 时间穿越用 `clock.Clock` interface；测试用 `clock.NewFake()`

---

## § 6. 风险 / Spike 项

| 风险 | 缓解 / 处置 |
|---|---|
| **fake_claude.sh 模拟 stream-json 行格式跟真 claude 不一致** —— Phase 6 跑通了，Phase 7 接真 claude 时 ParseEvent 翻车 | **Spike**：Phase 6 早期（§ 3.4 之前）跑一次真 claude 拿样本 JSONL，fake_claude.sh 按真样本 fixture 回放；样本归档 `tests/e2e/cognition/testdata/real_claude_samples/`；对应 [05-agent-adapters § 8.1](../design/implementation/05-agent-adapters.md) 提到的"落代码时按 `claude --output-format stream-json` 实际输出对齐" |
| **Memory git ops 在 CI 没装 git** | CI image 装 git；本地开发标准 dev container 已含 |
| **SQLite partial index `WHERE status='running'`** 实测不支持？ | Spike：3.1 写 migration 时先跑通 partial unique index；不支持则降级方案：普通 unique index `(scope_kind, scope_key, status_running_marker)` + trigger / 应用层校验 |
| **Coalescer + Spawner + TimeoutHandler 三个 goroutine 协调死锁** | 严格定义所有 channel buffer / select 拓扑；测试加 `-race`；状态机推进通过 events 表（已持久化）不靠 goroutine 间共享内存 |
| **跨 scope 并行下 decision_records / events 写并发** | SQLite 单 writer 串行已天然解决；用 `_busy_timeout=5000ms` 容忍瞬时锁等待（[02-persistence § 1.2](../design/implementation/02-persistence-schema.md) 连接参数已配） |
| **supervisor.md skill 内容过长 → prompt 持续逼近 BlobStore 阈值** | 3.4 加体积监控单测（skill 渲染后 ≤ 8 KB）；超过阈值 build 时 lint 拒 |
| **token_usage 文件回填竞争**（claude 子进程未写完文件就 exit）| 子进程内 fsync + rename 原子写；Spawner 读时如文件不存在则 token_usage 字段为 null（不阻断 MarkSucceeded） |
| **Memory subscriber 漏掉事件**（center crash 时 task.created 已进 events 表但还没建 skeleton）| subscriber 启动时 backfill：扫 events 表所有 lifecycle 事件 → 调 CreateSkeleton（幂等）；§ 3.3 实现里包含 |
| **fake_claude.sh 内部调 `agent-center dispatch ...` 会真写 DB** —— 跟单元测试 isolate 冲突 | e2e 测试设计如此（就是要看真链路）；单测层 SupervisorSpawner 不走 fake_claude 真跑 CLI 而是用 mock subprocess 接口 |
| **per-execution shim 是否复用** | **明确不复用**（§ 3.6 关键区分）；shim 是 worker daemon 路径，supervisor 在 center 同机直接 fork+exec |
| **MemoryDir 多用户隔离** | 单用户单 center v1 不做；roadmap |
| **token cost 监控告警** | roadmap（[01-supervisor-invocation § 6](../design/architecture/tactical/cognition/01-supervisor-invocation.md)）|

**Spike 全部消化在 Phase 6 内**；未消化的明确 defer：
- "Cross-invocation 协调机制" → roadmap
- "Memory 并发写 advisory lock" → roadmap
- "supervisor 训练 / 学习闭环" → roadmap
- "Web Console" → roadmap

---

## § 7. 下游解锁

Phase 6 完成后**Phase 7（Bridge inbound + 部署收尾）可开始**。

提供给下游的接口 surface：

| 接口 | 用法 |
|---|---|
| **events 表 wake 白名单已开放** `conversation.message_added` 路由到 `conversation:<C>` scope | Phase 7 Bridge inbound 收飞书消息后调 `agent-center conversation add-message ...` → 自动 emit `conversation.message_added` → SupervisorTriggerCoalescer 唤醒 supervisor → supervisor 决策回复 |
| **`supervisor.invocation_failed_alert` 事件** | Phase 7 部署收尾时 ops dashboard / 飞书机器人订阅 |
| **`agent-center supervisor` CLI 子命令** | Phase 7 部署文档 systemd unit / docker 命令引用；明确：不是用户直接调用，仅由 center 主进程内部 SupervisorSpawner spawn |
| **`agent-center supervisor retrigger`** | Phase 7 ops 手册：失败 invocation 的人工处置 |
| **DecisionRecord / SupervisorInvocation 通过 `inspect`+ `query`** | Phase 4 通用查询面已就绪；Phase 7 文档示例用例 |
| **Memory git 仓 backup 方案** | Phase 7 部署文档：rsync 或 git remote push schedule |

**冻结接口**（Phase 6 完后不允许语义改动；只能扩列）：
- `SupervisorInvocationRepository` / `DecisionRecordRepository` 接口签名
- `supervisor_invocations` / `decision_records` 表 column 语义
- `supervisor.*` 7 类 domain event 的 refs / payload schema
- 12 种 `DecisionKind` 闭集
- `assets/skills/supervisor.md` skill 文档（语义稳定；表达可改）

---

## § 8. References

### ADR

- [ADR-0001 不引入 MCP](../design/decisions/0001-no-mcp.md) — skill + CLI 方式
- [ADR-0002 不引入 LLM SDK](../design/decisions/0002-no-llm-sdk-use-cli-agents.md) — 跑真实 claude CLI
- [ADR-0003 Supervisor 非 Brain](../design/decisions/0003-supervisor-not-brain.md) — 角色 + 命名
- [ADR-0012 Memory file-based + git](../design/decisions/0012-memory-file-based.md) — Memory 存储形态
- [ADR-0013 Supervisor Invocation 并发模型](../design/decisions/0013-supervisor-invocation-concurrency.md) — coalescing / 串行 / 并行 / FIFO / timeout / crash recovery
- [ADR-0014 同事务原则](../design/decisions/0014-event-sourcing-level.md) — DecisionRecorder 同 tx 双写
- [ADR-0015 agent_trace 不进 events 表](../design/decisions/0015-agent-trace-not-in-events-table.md) — Memory 审计 / DecisionRecord 与 trace 区分
- [ADR-0018 per-execution shim](../design/decisions/0018-detached-agent-via-per-execution-shim.md) — 明确 supervisor 不复用 shim 路径

### 战术层（Cognition BC）

- [cognition/00-overview](../design/architecture/tactical/cognition/00-overview.md) — BC 入口
- [cognition/01-supervisor-invocation](../design/architecture/tactical/cognition/01-supervisor-invocation.md) — Invocation AR + DecisionRecord
- [cognition/02-memory](../design/architecture/tactical/cognition/02-memory.md) — Memory AR

### 战术层（被调 BC，Phase 1-5 工件）

- [agent-harness/01-prompt-assembly](../design/architecture/tactical/agent-harness/01-prompt-assembly.md) — Worker-side prompt（跟 supervisor-side 独立，不复用）
- [agent-harness/02-skill-cli-tooling](../design/architecture/tactical/agent-harness/02-skill-cli-tooling.md) — Skill 文档 + CLI 工具机制
- [observability/00-overview § 7.1](../design/architecture/tactical/observability/00-overview.md) — 五动词查询接口
- [task-runtime/00-overview § 7.1](../design/architecture/tactical/task-runtime/00-overview.md) — 唤醒事件白名单（task 部分权威）

### 实现层

- [02-persistence-schema § 8](../design/implementation/02-persistence-schema.md) — Cognition 切片落代码时补 § 8.3
- [03-cli-subcommands § 8.5 / § 8.8](../design/implementation/03-cli-subcommands.md) — supervisor 命令 + 子命令
- [04-configuration § 7.3](../design/implementation/04-configuration.md) — `supervisor.*` 配置项
- [05-agent-adapters § 8.1](../design/implementation/05-agent-adapters.md) — claude-code adapter（Phase 2 工件，Phase 6 复用）

### 横切

- [conventions](../rules/conventions.md) — § 0 DDD / § 1 无野任务 / § 2 可观测 / § 3 AI native / § 4 零 LLM SDK / § 8 BlobStore / § 11 Issue/InputRequest/Event 三分 / § 12 命名 / § 14 测试 / § 16 reason+message / § 17 错误不吞
- [testing](../rules/testing.md) — 覆盖率 ≥ 90% + 测试计划/报告 + 可测性
- [plans/README](README.md) — Phase 顺序纪律 + 模板
