# 可观测性 & Fleet View

> **DDD 战术层** · BC: Observability

可观测性是 agent-center 的一级关注点。

## 为什么 AI agent 场景的观测特别关键

对人类驱动的脚本，"日志看 stdout" 够用。对 LLM agent，观测对象多一个维度：**它思考过什么、调用了哪些工具、为什么做这个决定**。没有这层观测：

- Agent 跑飞，看不出原因
- Supervisor 决策不合理，不知道它"想了啥"
- 出 bug 没法复现（同样 prompt 跑两次结果不同）
- 成本 / token 用量是黑盒
- 跨任务模式分析做不了

所以观测不是"日志收集"，是 **结构化、可追溯、可重放、可聚合的 agent 行为档案**。

## 可观测对象分层

| 层 | 观测内容 |
|---|---|
| Task | 4 态状态变化（open/suspended/done/abandoned）、所有 TaskExecution 列表、依赖图、priority / ETA、累计 dispatch 次数、artifacts 集 |
| TaskExecution | A2A 6 态状态变化、所属 worker / agent_cli、workspace_mode + cwd、本次执行的 prompt、**逐条 tool call** 记录、token 用量、artifacts、failed_reason+message |
| Supervisor | 每次调用：触发事件、注入的 memory / 上下文、prompt 全文、LLM 输出、tool call 序列、最终决策、耗时 |
| Worker | 在线 / 离线、心跳、并发任务数、capacity、本机 active execution 数（`exec/*/` 目录）、reconcile 历史、host 资源（可选） |
| Issue | 开启 / 讨论 / 收敛全过程、参与方、产生的 Task batch |
| System | 任务吞吐率、TaskExecution 成败率、Issue → Task 转化率、Supervisor 唤醒频率、DB 大小、归档大小 |

## 设计承诺

### O1. 一切状态变化进 `events` 表，不丢

- 单一 append-only 事件流，所有上下文的 domain event 都落到这一张表
- 字段（概念）：`id, occurred_at, seq, event_type, refs (JSON), actor, payload (JSON), correlation_id, decision_id (nullable)`
- `decision_id` 让事件能反查具体 `decision_records` 行 + 拿 rationale；非决策触发的事件（如 `worker.online` / `task_execution.working` 等世界事件）该列为 null（详见 [06-supervisor-model.md § 5.10](../cognition/01-supervisor-model.md)）
- `refs` 是 JSON 对象，含 `task_id` / `execution_id` / `input_request_id` / `issue_id` / `worker_id` 等可选键；不同 event 关联的实体不同，平铺 nullable 列 vs JSON `refs` 由 implementation 层定（[conventions § 9](../../../../rules/conventions.md) dialect-agnostic）
- `actor` 标识动作触发者：`user:hayang` / `supervisor:invocation-id` / `worker:W-1` / `system`（派生事件）
- 与对应状态表在同事务内双写（state UPDATE + event INSERT 同一 tx）；**不是状态权威**，状态恢复走 DB 备份。用途：审计 / supervisor 输入 / 跨上下文 timeline / 新增投影。理由见 [ADR-0014](../../../decisions/0014-event-sourcing-level.md)
- 结构跟事件流系统兼容（未来切 Kafka / NATS 无痛）

### O1.x reason + message 双字段

凡是携带 `reason` 枚举的事件 payload / RPC 响应，必须同时携带 `message` 字段（人类可读详情）。详见 [conventions § 16](../../../../rules/conventions.md)。例：`task_execution.failed { reason: "worker_lost", message: "last heartbeat 2026-05-16T10:23 UTC" }`。

### O2. Agent 子进程必须开启结构化输出

- Claude code：`--output-format stream-json`，每行 JSONL（tool call / response / thinking 片段）
- Codex / OpenCode：各自的等价模式（adapter 层负责）
- **投递路径**：agent → shim 解析 JSONL → shim 把事件 append 到 `~/.agent-center-worker/exec/<id>/events.jsonl`（durable queue）+ 主动上报 daemon → daemon 实时更新 TaskExecution 投影摘要（`current_activity` / `recent_activities` / counts）+ 写 task 本地 `trace.jsonl` 文件；**不推 center events 表**（[ADR-0015](../../../decisions/0015-agent-trace-not-in-events-table.md)）。daemon 升级窗口期事件继续在 events.jsonl 累积，daemon 起来后 shim catchup 补齐（[ADR-0018 § 4](../../../decisions/0018-detached-agent-via-per-execution-shim.md)）；events.jsonl 是 worker 本机的事件 durable queue，**不替代** events 表（状态权威仍在 center）
- 任务结束时，本地 `trace.jsonl` 打包为 `tasks/<task_id>/trace.jsonl.gz` 上传 BlobStore，回填 `task.trace_blob_path`
- Execution 仍在跑时如需深挖 tool 参数 / thinking 文本：走 `agent-center peek-trace <execution>`（worker daemon 侧 RPC，center 转发）

### O3. Supervisor 调用全留档

- 每次 supervisor 启动：先在 DB 写 `supervisor_invocations(id, scope_kind, scope_key, trigger_event_ids, started_at, status='running', hard_timeout_seconds)` 占位（`trigger_event_ids` JSON 数组，coalesced batch 用同一行表达；详见 [06-supervisor-model.md § 2](../cognition/01-supervisor-model.md)）
- 调 `claude` 时把 `--session-id <invocation-id>` 传进去，让 claude 自己产的 JSONL trace 跟 invocation 关联
- Supervisor 子进程结束：回填 `ended_at, status, failed_reason / failed_message / timed_out_at, token_usage, decisions_made_count`
- 4 态终态：`succeeded` / `failed`（含 `reason ∈ {claude_nonzero / cli_command_error / oom / center_restart_orphan / killed_by_admin}`，配 `message`，[conventions § 16](../../../../rules/conventions.md)）/ `timed_out`
- Decisions 是 supervisor 通过 CLI 命令显式 emit 的：动作 CLI（`dispatch` / `kill-execution` / `issue *` / `conversation add-message` / `escalate-input-request` 等）内部自动 INSERT `decision_records` 行；`no_op` 决策走 `record-decision` CLI；详见 [06-supervisor-model.md § 5](../cognition/01-supervisor-model.md)
- 失败 / 超时 invocation **不自动重试**，emit `supervisor.invocation_failed_alert` → Bridge 推飞书 + Web Console flag；人工 `agent-center supervisor retrigger <id>` 才重发
- Center crash 后 `status='running'` 的孤儿行 → `failed(reason='center_restart_orphan')`，未被任何成功 invocation 覆盖的 wake 事件 → 自动 replay（[ADR-0013](../../../decisions/0013-supervisor-invocation-concurrency.md)）
- `events.decision_id` (uuid, nullable) 让事件能反查具体 decision；JOIN `decision_records` 拿 rationale

### O4. 日志的双写

- 实时通道走结构化事件 + agent JSONL trace（量小、价值高）
- 原始 stdout / stderr 任务结束打包一次性上传到 **BlobStore**（见 [implementation/01-blob-store.md](../../../implementation/01-blob-store.md)），DB 只存相对路径（`task.log_blob_path`）

### O5. 查询接口

统一的 5 个 CLI 动词（user / supervisor / web console 共用，无 supervisor 专属 RPC）：

| 动词 | 用途 |
|---|---|
| `inspect <kind> <id>` | 看单个实体完整状态 + timeline |
| `query <kind> [filters]` | 列多个实体 |
| `ps` | 当前活动资源实时快照 |
| `stats [filter]` | 聚合度量 |
| `logs <ref>` | 拉原始事件日志 |

**inspect 支持的 kind**：

| kind | 输出 |
|---|---|
| `task <id>` | 元数据 + 当前 status + dep 状态 + 所有 executions 列表 + 时间线 |
| `execution <id>` | execution 状态 / workspace / artifacts / agent trace 摘要 / 全 events |
| `worker <id>` | enroll 信息 / 当前 capacity / 正在跑的 executions / 心跳历史 |
| `issue <id>` | issue 状态 / comments / spawned tasks / 关联 conversations |
| `supervisor <invocation_id>` | 该次 invocation 的触发事件 / prompt / 决策记录 / 落地动作 |
| `conversation <id>` | message 时间线 |
| `input_request <id>` | 请示来龙去脉 |
| `project <id>` | project 元数据 / 所有相关 task / mapping 状态 |
| `worktree <execution_id>` | 给定 execution 的 workspace 详情 |

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

完整 CLI 签名归 [implementation/03-cli-subcommands.md](../implementation/)（TBD）。Supervisor 通过 CLI 用同一套查询（[02-task-model.md § 12.2](../scheduling/01-task-model.md)）。

飞书：任务完成卡片附 [查看 trace] [查看 supervisor 思考] 按钮，点击拉详细页（v1 回 Markdown 摘要文本）。

## Fleet View — 跨任务实时全景

**现在有哪些 execution 活着、各自在干嘛**。跟单任务下钻并列的关键视角。

### 数据基础

每个 worker daemon 边解析 agent 子进程 JSONL，边维护 `TaskExecution` 实时投影。schema 见 [implementation/02-persistence-schema.md](../../../implementation/02-persistence-schema.md) TBD。投影字段含义：

- `current_activity` 自由文本: `"edit src/x.go"` / `"bash: go test"` / `"thinking"`
- `current_activity_at` 当前活动开始时间
- `status` submitted / working / input_required / completed / failed / killed（A2A 6 态）
- `workspace_mode` worktree / direct
- `cwd` 实际 CWD（worktree path 或 base_path）
- `total_tool_calls` / `total_tokens` 累计
- `working_seconds_accumulated` 不计 input_required 期间的累计 working 时长

这张表是 worker daemon 维护的**实时投影**（边解析 JSONL 边更新本地状态 + 推 center）；崩了从 DB 备份恢复，不靠 events 表重算（[ADR-0014](../../../decisions/0014-event-sourcing-level.md)）。

### CLI: `agent-center ps`（仿 `docker ps` / `kubectl get pods`）

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

### 飞书侧

用户问"现在在跑啥" → supervisor 调 `query executions --status=working,input_required` → 组装卡片回复。周期 review 卡片附此刻活跃 execution 小卡。

## 事件总览（按 BC 分组）

| BC | 主要事件类型 |
|---|---|
| Scheduling (Task) | `task.created` / `task.priority_changed` / `task.eta_changed` / `task.workspace_mode_changed` / `task.dependency_added` / `task.dependency_removed` / `task.suspended` / `task.resumed` / `task.done` / `task.abandoned` / `task.dispatch_limit_reached` |
| Scheduling (TaskExecution) | `task_execution.created` / `task_execution.dispatched` / `task_execution.working` / `task_execution.input_required` / `task_execution.completed` / `task_execution.failed` / `task_execution.kill_requested` / `task_execution.killed` |
| Scheduling (InputRequest) | `input_request.requested` / `input_request.responded` / `input_request.timed_out` / `input_request.canceled` |
| Discussion | `issue.opened` / `issue.commented` / `issue.discussion_started` / `issue.concluded` / `issue.withdrawn` / `issue.tasks_spawned` |
| Workforce | `worker.enrolled` / `worker.online` / `worker.offline` / `worker.heartbeat` / `worker_project_proposal.*` / `worker_project_mapping.*` / `project.*` |
| Execution (worker 侧) | `worktree.created` / `worktree.released` / `artifact.uploaded` / `task_log.archived` / `task_trace.archived`（agent_trace 不再作为事件流入 events 表，见 [ADR-0015](../../../decisions/0015-agent-trace-not-in-events-table.md)） |
| Cognition | `supervisor.invocation_started` / `supervisor.invocation_ended` / `supervisor.decision_made` / `supervisor.invocation_failed_alert`（Memory 变更不 emit `memory.*` 事件，由 supervisor invocation 的 `trace.jsonl.gz`（含 `Edit`/`Write` tool 调用）+ `git log` 双渠道审计，[ADR-0012](../../../decisions/0012-memory-file-based.md) / [ADR-0015](../../../decisions/0015-agent-trace-not-in-events-table.md)） |
| Conversation | `conversation.opened` / `conversation.message_added` / `conversation.closed` / `identity.registered` / `channel_binding.added` |
| Bridge | `channel.delivered` / `channel.delivery_failed` / `bridge.parse_failed` |

所有失败 / 取消 / 超时事件必须带 `reason + message` 双字段（[conventions § 16](../../../../rules/conventions.md)）。

## 关键 Aggregate / Event 摘要

- `Event` —— 跨上下文唯一事件流
- `SupervisorInvocation` —— 一次 supervisor 调用的审计单元
- `TaskExecution` —— 一次任务执行的状态承载（一并承载 worker 侧的运行时投影；AgentSession 概念已合并，见 [ADR-0010](../../../decisions/0010-task-execution-two-layer-model.md)）
- `AgentTraceEvent` —— agent JSONL 单行事件（worker daemon 运行时概念；实时投影到 TaskExecution 摘要 + 归档至 BlobStore；**不入 events 表**，[ADR-0015](../../../decisions/0015-agent-trace-not-in-events-table.md)）
