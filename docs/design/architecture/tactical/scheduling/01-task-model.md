# Task 模型 & 状态机

> **DDD 战术层** · BC: Scheduling

回答"Task / TaskExecution 是什么、怎么变迁、怎么协作"。

具体表 schema / CLI 签名归 [implementation/](../implementation/)；为什么这样定归 [decisions/](../decisions/)（核心 ADR：[0010 两层模型](../../../decisions/0010-task-execution-two-layer-model.md) / [0011 派单可靠性](../../../decisions/0011-dispatch-reliability-protocol.md)）。

---

## § 1. 两层模型

agent-center 用**两层概念**区分"任务"与"任务的一次执行"：

| 层 | 实体 | 角色 | 状态机 |
|---|---|---|---|
| 工作单元 | **Task** | entity，身份不变；语义 = "做这件事" | `open` / `suspended` / `done` / `abandoned`（4 态） |
| 一次执行 | **TaskExecution** | 一次 dispatch → 结束的运行痕迹 | `submitted` / `working` / `input_required` / `completed` / `failed` / `killed`（6 态，借鉴 A2A） |

**关键区别**：

- **失败属于 execution，不属于 task**：execution 跑挂了不代表 task 整体死，task 仍可重新派
- **重派不创建新 task**：retry = 同一 task 上创建新 execution；execution_id 唯一不变
- **Task 永远绑一个 project**（[conventions § 1](../../../../rules/conventions.md)：无散单）
- **任意时刻一个 task 最多 1 条 active execution**（单活约束）

> 这是 [ADR-0010](../../../decisions/0010-task-execution-two-layer-model.md) 的核心决策。AgentSession 概念在术语里下线 —— "一次执行"的唯一名词是 TaskExecution。

---

## § 2. Task

### 2.1 状态机

```
                  (created)
                      ↓
              ┌──────────────┐
              │     open     │ ──────── abandon ────────┐
              └──┬────────▲──┘                          │
        suspend  │        │ resume                      ↓
                 ↓        │                       ┌──────────────┐
              ┌──────────────┐  ── abandon ─────→ │  abandoned   │
              │  suspended   │                    │    (终态)    │
              └──────────────┘                    └──────────────┘

(open 或 suspended) ── task_execution.completed ──→ ┌─────────┐
                                                     │  done   │
                                                     │ (终态)  │
                                                     └─────────┘
```

### 2.2 状态语义

| 状态 | 含义 | 终态? | 能 dispatch 新 execution? |
|---|---|---|---|
| `open` | 等开干 / 可被 dispatch | 否 | ✅ |
| `suspended` | 暂停，可恢复 | 否 | ❌ |
| `done` | 干完了（任一 execution 成功） | **是** | ❌ |
| `abandoned` | 决定不做了 | **是** | ❌ |

> Task **没有 `failed` 状态**。某次 execution 失败 ≠ task 失败。Task 整体只有"完成 / 没完成 / 暂停 / 放弃"四种业务语义。

### 2.3 状态迁移

| 迁移 | 触发 | 触发者 | 备注 |
|---|---|---|---|
| → `open` | Task 创建（CLI / Issue conclude / supervisor spawn） | Center | 初始状态 |
| `open → suspended` | `suspend-task` 动作 | User / Supervisor | 前置：先 kill 当前 active execution（若有） |
| `suspended → open` | `resume-task` 动作 | User / Supervisor | 不续跑 —— 要继续干必须新建 execution |
| `open → abandoned` | `abandon-task` 动作 | User / Supervisor | 前置：先 kill 当前 active execution（若有） |
| `suspended → abandoned` | `abandon-task` 动作 | User / Supervisor | 同上 |
| 任何非终态 → `done` | `task_execution.completed` 事件 | Center（自动联动） | 自动派生事件 `task.done`，actor=`system` |

**`done` 与 `abandoned` 不可逆**。从 `suspended` 想再做 → resume 回 `open` → 新建 execution。

### 2.4 字段（架构层概念，schema 归实现层）

| 字段 | 类型 | 必填 | 含义 |
|---|---|---|---|
| `id` | uuid | ✅ | 主身份；不变 |
| `project_id` | string | ✅ | 归属 project（无散单） |
| `status` | enum | ✅ | open / suspended / done / abandoned |
| `title` | string | ✅ | 短标题 |
| `description` | string \| blob_ref | ✅ | 任务正文；≤10 KB 内联，超过用 BlobStore（[conventions § 8](../../../../rules/conventions.md)） |
| `priority` | enum | ✅ | high / medium / low；默认 medium |
| `eta_at` | timestamp | 可空 | 期望完成时刻（柔性，过期不影响系统行为） |
| `requires_worktree` | bool | ✅ | 默认 true；见 § 6 |
| `depends_on_task_ids` | JSON array | ✅ | 前置 task uuid 列表；默认 `[]`；见 § 7 |
| `parent_task_id` | uuid | 可空 | 父 task 血缘（不阻塞，仅记录） |
| `from_issue_id` | uuid | 可空 | 来源 Issue 血缘 |
| `current_execution_id` | uuid | 可空 | 指向当前 active execution（或最近一次）；submitted / 终态 + 等待重派时为 null |
| `conversation_id` | uuid | 可空 | 该 task 绑定的 `kind=task` Conversation；按来源决定创建时机（a/e 同步建 / b/c/d 懒创建 null）。详见 § 2.6 与 [ADR-0017](../../../decisions/0017-task-as-conversation.md) |
| `created_by` | string | ✅ | user:xxx / supervisor:xxx |
| `created_at` | timestamp | ✅ | |
| `updated_at` | timestamp | ✅ | |

### 2.5 可变性

| 字段 | 创建后可改? | 修改前置条件 |
|---|---|---|
| `priority` | ✅ | 非终态 |
| `eta_at` | ✅ | 非终态 |
| `depends_on_task_ids` | ✅ | 非终态 + 无活动 execution |
| `requires_worktree` | ✅ | 非终态 + 无活动 execution |
| `title` / `description` | ✅ | 非终态 |
| `id` / `project_id` / `parent_task_id` / `from_issue_id` | ❌ | 创建后不可改 |
| `status` | 系统驱动 | 仅通过 § 2.3 列的动作触发 |
| `current_execution_id` | 系统驱动 | dispatch / execution 状态变化时自动更新 |
| `conversation_id` | 系统驱动 | 仅 null → 非 null（绑定）：a/e 路径 task.created 同事务、b/c/d 路径懒创建（CLI `task bind-card` / 飞书 slash `/track` / supervisor 解析自由文本 / center fallback）；v1 不支持 unbind（非 null → null）—— 见 [roadmap](../../../roadmap.md) |

### 2.6 Task ↔ Conversation 绑定（飞书可见性）

[ADR-0017](../../../decisions/0017-task-as-conversation.md)：**Task ↔ Conversation 1:1**。所有 task 相关可见 IO（supervisor 分析 / worker 进展 / agent 请示 / 用户回应）走同一个 `kind=task` Conversation，复用既有 Message 模型，不引入"进度卡"独立概念。

**按来源决定创建时机：**

| Task 来源 | conversation 创建时机 |
|---|---|
| a. 飞书用户 @bot 直接发起 | Task.created 同事务建 Conversation + emit `conversation.opened` + Bridge 发 Task root card 回写 `primary_channel_thread_key` |
| b. Issue concluded spawn | **不**建；`task.conversation_id=null`；用户后续触发懒创建 |
| c. Supervisor 自主开（v1 暂无） | 同 b |
| d. CLI 直接 `task create` | 同 b |
| e. Web Console 新建 | 同 a（Web Console 是 Conversation 的另一个 channel binding） |

**懒创建触发：**

| 路径 | 触发 |
|---|---|
| 用户 CLI | `agent-center task bind-card <task_id> --channel=feishu --auto` 或 `--to=<conv_id>` |
| 飞书 slash 命令 | `/track <task_id>` —— Bridge 直接转 bind-card（不经 supervisor） |
| 飞书 @bot 自由文本 | "盯一下 T-42" → supervisor 解析意图 → bind-card |
| Center 硬规则 fallback | agent 调 `request-input` 且 task.conversation_id=null → 自动 bind 到 `notification.default_channel`；未配置 → InputRequest 创建失败、execution → `failed(reason=no_input_channel)` |

详见 [ADR-0017 § 10](../../../decisions/0017-task-as-conversation.md)。

**CLI：**

```
agent-center task bind-card <task_id> --channel=feishu --auto         # 新建 conversation + 推到 default channel
agent-center task bind-card <task_id> --channel=feishu --to=<conv_id> # 绑到既有 conversation
agent-center task unbind-card <task_id>                                # v1 不支持，留接口给 v2
```

**不引入：** `task.bound_card_json` 字段 / `task.bound_card_requested` 事件 / `task.progress_milestone_reached` 事件 / `task_progress` content_kind（[ADR-0016](../../../decisions/0016-task-progress-via-bound-thread.md) 期间的规划，被 ADR-0017 全部撤回）。

**事件：** Conversation 创建走既有 `conversation.opened`，不引入 Task 侧专用绑定事件。`task.conversation_id` 写入由所在事务的状态变更（task.created 或后续 bind 动作）携带 —— 不需要额外 audit event；`task.created` payload 含 `conversation_id` 字段已足够审计（[ADR-0014 § 3](../../../decisions/0014-event-sourcing-level.md)）。

---

## § 3. TaskExecution

### 3.1 状态机

```
            ┌──────────────┐
            │  submitted   │ ←──── (created by Center)
            └──────┬───────┘
                   │ (worker spawn agent OK → task_execution.working)
                   ↓
            ┌──────────────┐  ──── input_request.requested ────→ ┌─────────────────┐
            │   working    │                                       │ input_required  │
            │              │ ←──── input_request.responded ─────── │                 │
            └─┬────┬────┬──┘                                       └──┬──────────┬───┘
              │    │    │                                             │          │
       completed  failed  kill                                input_timeout      kill
              │    │    │                                             │          │
              ↓    ↓    ↓                                             ↓          ↓
       ┌──────────┐ ┌────────┐ ┌────────┐                       ┌────────┐ ┌────────┐
       │completed │ │ failed │ │ killed │ (终态)                │ failed │ │ killed │
       └──────────┘ └────────┘ └────────┘                       └────────┘ └────────┘
```

> `kill` 流程实际由 `task_execution.kill_requested`（Center 发起）触发 worker SIGTERM → 5s grace → SIGKILL → emit `task_execution.killed`。状态机视角下 `* → killed` 是单一逻辑迁移。

### 3.2 状态语义

| 状态 | 含义 | 终态? |
|---|---|---|
| `submitted` | 已创建，envelope 发出 / pending ACK / 等 worker spawn agent | 否 |
| `working` | Agent 子进程在跑 | 否 |
| `input_required` | Agent 卡在 InputRequest，等用户 / supervisor 回答 | 否 |
| `completed` | 成功结束（agent exit 0 + 无未 resolve input_request） | **是** |
| `failed` | 失败结束（含 timeout / exit ≠ 0 / worker_lost 等） | **是** |
| `killed` | 被显式 kill（user / supervisor 触发；或 abandon-task / suspend-task 前置触发） | **是** |

### 3.3 状态迁移

| 迁移 | 触发事件 | 谁 emit | 备注 |
|---|---|---|---|
| `submitted → working` | `task_execution.working` | Worker daemon | **agent 子进程 spawn 成功**后才转（worktree 建好 + agent PID 拿到）；不是 ACK 即转 |
| `working → input_required` | `input_request.requested` | Worker daemon（代 agent） | 详见 [04-input-required.md](02-input-required.md) |
| `input_required → working` | `input_request.responded` | Center | 写入 response 那一刻立刻转；worker 侧 agent 异步解阻塞 |
| `working → completed` | `task_execution.completed` | Worker daemon | agent exit 0 + 无未 resolve input_request；payload 含 artifact 引用 |
| `working → failed` | `task_execution.failed` | Worker daemon | reason ∈ § 3.6 |
| `* → killed`（含 input_required） | `task_execution.killed` | Worker daemon | 由 `task_execution.kill_requested`（Center）触发 |

> **Center 端两阶段 cancel**：先 emit `task_execution.kill_requested`（仅落 `cancel_requested_at` 时间戳），worker 收事件 → SIGTERM → 5s grace → SIGKILL → emit `task_execution.killed`。状态对外仍呈现为当时态 + `cancel_requested_at` 提示等待中；不引入新状态。

### 3.4 字段

| 字段 | 类型 | 含义 |
|---|---|---|
| `id` (`execution_id`) | uuid | 主身份；幂等 + fencing key |
| `task_id` | uuid | 所属 task |
| `worker_id` | uuid | 派给哪个 worker（确定后不变） |
| `status` | enum | submitted / working / input_required / completed / failed / killed |
| `dispatch_state` | enum | pending_ack / acked / null（acked 之后或终态时为 null） |
| `agent_cli` | string | 启 agent 用哪个 adapter（claude-code / codex / opencode） |
| `workspace_mode` | enum | worktree / direct（透自 task.requires_worktree） |
| `cwd` | string | 实际 CWD（worktree 路径 或 base_path） |
| `branch_name` | string \| null | worktree 模式下的新 branch（默认 `task/<execution_id>`）；direct 模式 null |
| `base_branch` | string \| null | worktree 模式下 base branch（默认 main） |
| `priority` | enum | 透自 task |
| `eta_at` | timestamp \| null | 透自 task |
| `execution_timeout_override` | duration \| null | 仅向上覆盖默认 6h |
| `working_seconds_accumulated` | int | accumulated working 时间（不计 input_required 期间） |
| `pending_input_request_id` | uuid \| null | input_required 时指向当前 IR |
| `started_at` | timestamp | submitted 创建时刻 |
| `working_started_at` | timestamp \| null | 第一次进 working 的时刻 |
| `cancel_requested_at` | timestamp \| null | 收到 kill 请求的时刻；nullable，不引入状态 |
| `ended_at` | timestamp \| null | 终态时刻 |
| `completed_reason` / `completed_message` | string | completed 时填（一般 null / "agent reported success"） |
| `failed_reason` / `failed_message` | string | failed 时填（reason ∈ § 3.6） |
| `killed_reason` / `killed_message` | string | killed 时填（reason ∈ {user_request / supervisor_request / abandon_precondition / suspend_precondition / reconcile_stale / reconcile_unknown / timeout_kill}） |

> 所有 reason 字段必须配 message 字段（[conventions § 16](../../../../rules/conventions.md)）。

### 3.5 不可变 / append-only

- TaskExecution 一旦创建，`id` / `task_id` / `worker_id` / `agent_cli` / `workspace_mode` 等"派单契约"字段不可改
- 状态机只能按 § 3.3 列的迁移路径走
- 终态后不可变

### 3.6 Failed reason 枚举

failed_reason 是 supervisor 重派决策的关键信号：

| reason | 含义 |
|---|---|
| `agent_exit_nonzero` | Agent 子进程退出码 ≠ 0 |
| `agent_reported_failure` | Agent 调 `agent-center report-failure` 显式上报 |
| `worktree_setup_failed` | git worktree add 出错 / 磁盘 / 权限等 |
| `submitted_timeout` | 在 submitted 状态超 5 min（worker 没起来 agent） |
| `execution_timeout` | 在 working 累计超 `execution_timeout`（默认 6h） |
| `input_timeout` | InputRequest 超时（默认 T2=24h） |
| `worker_lost` | Worker 心跳超 `worker_heartbeat_timeout`（默认 60s） |
| `dispatch_no_ack` | Envelope 发出后 30s 没 ACK |
| `dispatch_nack:<sub_reason>` | Worker 显式 NACK（含 worker_at_capacity / mapping_missing / 等） |
| `no_input_channel` | agent 调 `request-input` 时 task.conversation_id=null 且 `notification.default_channel` 未配 → § 2.6 fallback 失败、InputRequest 无法创建（[ADR-0017 § 10.4](../../../decisions/0017-task-as-conversation.md)） |
| `shim_no_hello` | Daemon spawn shim 后 60s 内未收到 ShimHello（[ADR-0018 § 7](../../../decisions/0018-detached-agent-via-per-execution-shim.md)） |
| `shim_crashed` | Shim 进程崩溃（PID 死 / start_time mismatch）；agent 若还活则连带 SIGTERM（同上 ADR） |

reason 之外必须填 message（人类可读详情）。

### 3.7 进度上报到 Conversation（worker daemon 行为）

Worker daemon 边解析 agent JSONL 边判断 milestone，命中后调 `conversation add-message` 写一条 `content_kind=agent_finding` 的 Message 到 `task.conversation_id`。task.conversation_id=null 时跳过（普通 progress milestone **不**触发 § 2.6 fallback；只有 InputRequest 触发）。

**5 条 milestone**（沿用 [ADR-0016 § 3](../../../decisions/0016-task-progress-via-bound-thread.md)，执行位置改为 worker daemon）：

| Milestone | 触发条件 |
|---|---|
| **status 变化** | TaskExecution status 转换（dispatched → working → input_required → completed / failed / killed） |
| **activity 类型变化** | `current_activity` 大类切换（如 "editing files" → "running tests"）；不按具体文件名变 |
| **长 tool call** | 单次 tool call 持续 > 30s（开始 + 结束各发一次） |
| **累计摘要** | 每累计 N 个 tool call 发一条摘要（N 默认 20，可配） |
| **用户在 task conversation 内询问** | 用户在 task conversation 里说话 → 立即 push 一条当前快照（不烧 supervisor） |

阈值（30s / N=20）走配置。

> **不 emit `task.progress_milestone_reached` 事件** —— 进度通过 `conversation.message_added` 走 outbound 链路；不需要专门 progress 事件类。详见 [ADR-0017 § 4](../../../decisions/0017-task-as-conversation.md)。

---

## § 4. Dispatch 流程 + 派单可靠性

详细决策见 [ADR-0011](../../../decisions/0011-dispatch-reliability-protocol.md)。本节给架构层视图。

### 4.1 DispatchEnvelope schema

```yaml
DispatchEnvelope:
  envelope_version: "v1"

  # Identity
  execution_id:    uuid               # 主身份 + 幂等 + fencing
  task_id:         uuid
  worker_id:       uuid
  project_id:      string
  conversation_id: uuid?              # task.conversation_id 透传；worker daemon 据此写进度 message / InputRequest 载体 message。null → 跳过 progress 写入；InputRequest 触发 § 2.6 fallback

  # Workspace
  agent_cli:       string             # claude-code | codex | opencode
  workspace_mode:  string             # worktree | direct
  base_branch:     string?            # worktree 模式默认 main

  # Task content (worker 组装 prompt)
  task_title:                  string
  task_description:            string?       # ≤10KB 内联
  task_description_blob_ref:   string?       # 或 blob 引用

  # Context refs (worker 按需 fetch)
  from_issue_id:           uuid?
  parent_task_id:          uuid?
  depends_on_task_ids:     [uuid]            # 已满足；仅上下文用

  # Signals
  priority:    string                 # high | medium | low
  eta_at:      timestamp?

  # Overrides
  execution_timeout_override:  duration?
  extra_skill_files:           [string]?
```

### 4.2 ACK / NACK 协议

```
Center → Worker: DispatchEnvelope
Worker → Center: DispatchAck   { execution_id, accepted=true, message?, acked_at }
              or DispatchNack { execution_id, accepted=false, reason, message, acked_at }
```

| 结果 | Center 行为 |
|---|---|
| ACK accepted=true | `dispatch_state='acked'`；监听后续 worker 事件 |
| NACK | `task_execution → failed(reason='dispatch_nack:<sub>', message=...)`；触发 supervisor wake |
| 30s 没 ACK | `task_execution → failed(reason='dispatch_no_ack')`；触发 supervisor wake |

NACK reason 标准枚举见 [ADR-0011](../../../decisions/0011-dispatch-reliability-protocol.md) § Decision 2。

### 4.3 Worker 端处理时序

```
1. 收到 DispatchEnvelope
2. 查 ~/.agent-center-worker/exec/<execution_id>/ 目录幂等:
   - 无目录 → 建目录 → 写 envelope.json → 继续
   - 目录存在 + status.json.phase=running → 重发 ACK；不重起 shim
   - 目录存在 + status.json.phase=done → 重发 ACK；不做事
3. 校验 envelope (agent_cli / mapping / base_branch / version)
4. 失败 → NACK + reason + message；目录回滚
5. 通过 → ACK
6. 准备 workspace:
   - worktree 模式: git worktree add -b task/<execution_id>
   - direct 模式: CWD = base_path
   - emit task_execution.working { workspace_mode, cwd, ... }
   - worktree 模式额外 emit worktree.created
7. 组装 prompt (详见 [08-prompt-assembly.md](../_cross-cutting/01-prompt-assembly.md))
8. 生成 shim_token (一次性 nonce)
9. Spawn shim 子命令 (detached, setsid)，env 注入（§ 4.4 + AGENT_CENTER_SHIM_TOKEN）：
   agent-center worker shim --execution-id=... --shim-token=... --cmd=... -- <args>
10. 等 ShimHello (60s 超时 → failed reason='shim_no_hello')；
    shim 起 agent CLI 子进程、写 status.json、连 daemon 上报
11. 进入 normal monitoring loop (接 shim 主动上报的事件 + 转发 agent RPC)
```

> Shim 模型细节（per-execution 目录 / RPC 协议 / fencing / 异常 timeout）详见 [07-worker-model.md § Shim 模型与 per-execution 目录](../workforce/01-worker-model.md) 与 [ADR-0018](../../../decisions/0018-detached-agent-via-per-execution-shim.md)。

### 4.4 Env 注入（daemon → shim → agent）

**daemon → shim**（spawn `agent-center worker shim` 子命令时）：

```
AGENT_CENTER_EXECUTION_ID       = <uuid>
AGENT_CENTER_TASK_ID            = <uuid>
AGENT_CENTER_PROJECT_ID         = <string>
AGENT_CENTER_CONVERSATION_ID    = <uuid> or ""    # task.conversation_id；空字符串表示未绑定
AGENT_CENTER_WORKSPACE_MODE     = "worktree" | "direct"
AGENT_CENTER_CWD                = <resolved path>
AGENT_CENTER_PRIORITY           = "high" | "medium" | "low"
AGENT_CENTER_ETA_AT             = <ISO 8601> or ""
AGENT_CENTER_SHIM_TOKEN         = <crypto random nonce>   ★ 仅 daemon → shim
```

**shim → agent**（除 SHIM_TOKEN 外原样透传 + 改写 WORKER_SOCK 指向 shim）：

```
AGENT_CENTER_WORKER_SOCK        = ~/.agent-center-worker/exec/<execution_id>/shim.sock
                                  ★ 指向 shim 的本地 socket，不是 daemon 主 socket
                                  (daemon 升级窗口期 agent 调 RPC 不受影响)
```

详见 [07-worker-model.md § Worker 内 Agent CLI 中转](../workforce/01-worker-model.md) 与 [ADR-0018](../../../decisions/0018-detached-agent-via-per-execution-shim.md)。

### 4.5 Worker reconcile（重连对账）

Worker enroll / 重连后第一件事：

```
Worker → Center: Reconcile { worker_id, local_active_executions: [...] }
Center → Worker: ReconcileResponse {
  active:  [...],     # 继续跑
  stale:   [...],     # center 已标 failed/killed/done; worker 杀本地进程
  unknown: [...],     # 不存在或归属其他 worker
}
```

Worker 后续：

- `active` → 继续上报事件
- `stale` / `unknown` → SIGTERM 本地 agent；emit `task_execution.killed { reason='reconcile_stale' / 'reconcile_unknown' }`（仅审计；Center 不改状态）

Worker reconcile 完成前不接新 dispatch。

### 4.6 单活约束实现

应用层校验（不用 partial unique index，[conventions § 9](../../../../rules/conventions.md) dialect-agnostic）：

- Center dispatch 前查 task：若 `current_execution_id` 不为 null 且对应 execution 在非终态 → 拒绝（避免双 active execution）
- 校验在 DB transaction 内完成，保证一致性

---

## § 5. Kill / Abandon / Suspend

跟 cancel 相关的三个动作严格区分，**不互相隐式联动**。

### 5.1 动作定义

| 动作 | 直接效果 | Task 状态 | Execution 状态 |
|---|---|---|---|
| `kill-execution` | 杀当前 execution | `open`（不变） | `killed` |
| `suspend-task` | task 整体暂停 | `open → suspended` | （前置：有 active 则先 kill） |
| `abandon-task` | task 整体废弃 | `* → abandoned` | （前置：有 active 则先 kill） |

### 5.2 kill 进程级机制

```
1. Center 收 kill 请求 (user/supervisor)
2. Center 写 task_executions.cancel_requested_at = now
   emit task_execution.kill_requested { execution_id, reason, message }
3. Worker 收事件:
   a. SIGTERM 给 agent 子进程
   b. 等 5s grace（worker.yaml 可配置）
   c. 还活着 → SIGKILL
   d. 进程死后:
        - 关 unix socket (若 agent 阻塞在 request-input)
        - 取消未完 input_request (emit input_request.canceled)
        - 已 emit 的 trace events / agent 显式 artifacts 保留
        - 不主动扫 worktree 挖产物 (24h GC 期人工查)
        - emit task_execution.killed { reason, message }
4. Center: execution.status=killed
   若是 abandon-task 前置: 继续走 abandon 流程
   若是 suspend-task 前置: 继续走 suspend 流程
```

### 5.3 abandon-task / suspend-task 前置 kill

abandon / suspend 不"隐式触发" kill。CLI / Bridge 实现成**显式两条事件链**：

```
t0  abandon-task T-42 命令进来
t0  Center 查 T-42 当前 active execution = E-7
t0  Center emit task_execution.kill_requested { execution_id=E-7, reason='abandon_precondition' }
    (走 § 5.2 全流程)
t5+ task_execution.killed 落地
t5+ Center 查 T-42 现在无 active execution → 继续 abandon
t5+ Center emit task.abandoned { task_id=T-42 }
```

reason 字段标明上下文（`user_request` / `supervisor_request` / `abandon_precondition` / `suspend_precondition`），inspect 看得清来龙去脉。

### 5.4 权限

| 动作 | User | Supervisor | Worker | Agent |
|---|---|---|---|---|
| kill-execution | ✅ | ✅ | ❌ | ❌ |
| abandon-task | ✅ | ✅ | ❌ | ❌ |
| suspend-task | ✅ | ✅ | ❌ | ❌ |
| resume-task | ✅ | ✅ | ❌ | ❌ |

Worker / Agent 不能直接 cancel（[conventions § 1](../../../../rules/conventions.md)：center 是状态权威）。Agent 想中止 → `report-failure` → execution → `failed`，不是 killed。

### 5.5 边界情况

| 场景 | 行为 |
|---|---|
| Cancel `submitted` 状态的 execution（agent 还没 spawn） | 不 SIGTERM；直接转 killed；emit `task_execution.killed` |
| Cancel `input_required` 状态的 execution | 走 § 5.2；同时 input_request.canceled |
| Cancel 已终态的 execution | 报错（不可对终态操作） |
| abandon 已 abandoned 的 task | 报错（终态不可逆） |
| 重复 kill（grace 期再点） | 幂等 no-op（`cancel_requested_at` 已存在） |
| abandon 前置 kill 超时（worker 离线 / SIGKILL 也不死） | CLI 报错；task 不进 abandoned；v1 不做 `--force-abandon`（推 [roadmap](../../../roadmap.md)） |
| Worker 离线时发 kill | Center 记录 `cancel_requested_at`，事件留在 worker 派单队列；上线后处理；最终通过 reconcile 或 timeout 触发 |

---

## § 6. Workspace 模式

每个 execution 必有 CWD；两种模式独立设计、不互相硬塞。

### 6.1 两种模式

| 模式 | `requires_worktree` | CWD | 隔离 | 典型用途 |
|---|---|---|---|---|
| **worktree** | `true`（默认） | `base_path + ".wt/task-<execution_id>"`（per-execution git worktree） | git worktree 隔离 | 改代码 / 新 branch / 起 PR / 并发执行 |
| **direct** | `false` | `base_path` 自身 | 无 | 读项目文件 / 想在用户当前 branch in-place / 想看用户工作树上下文 |

### 6.2 决策维度（不是"是不是 code 工作"）

判断要不要 worktree 看 4 个问题：

1. 需要跟用户当前工作树**隔离**吗？
2. 需要**干净 base branch** 起手（不继承 uncommitted）吗？
3. 这个 task 可能跟其他 task 在同 project **并发**跑（互踩风险）吗？
4. 是否必须 push 到 base_path 当前 branch（in-place hot fix）？

任一 yes → worktree。Q4 单独 yes → direct。都 no → direct 更轻。

写进 supervisor.md skill：

```
Default: worktree (requires_worktree=true).

Use worktree when ANY:
  - Need isolation from user's current working tree
  - Need fresh main-branch start (don't inherit uncommitted state)
  - Task may run concurrently with other tasks on same project
  - Will create a PR or new branch as artifact

Use direct when ANY:
  - User explicitly wants modifications in-place on current branch
  - Task is pure reading / synthesis with no git interaction
  - Want agent's working output visible in user's current dir

Default to worktree on uncertainty — isolation is safer.
```

### 6.3 Direct 模式约束

- CWD = `base_path`，agent 能读 CLAUDE.md / AGENTS.md / 项目其他文件（[ADR-0005](../../../decisions/0005-project-charter-stays-in-project-repo.md)）
- **按约定**不修改项目文件；不强制 enforcement（v1 不做 readonly mount，推 [roadmap](../../../roadmap.md)）
- 多 direct 模式 execution 共享 base_path → 接受无锁并发（假定 agent 只读、副作用最小）
- direct 模式仍**必须**有 WorkerProjectMapping（要 base_path）；worker 选择策略与 worktree 模式一致

### 6.4 Workspace 资源生命周期事件

| Event | 触发 |
|---|---|
| `task_execution.working` | execution 进入 working；payload 含 `workspace_mode` + `cwd` |
| `worktree.created` | **仅** worktree 模式 emit |
| `worktree.released` | **仅** worktree 模式 24h GC 时 emit |

Direct 模式不引入 ephemeral 资源 / 不 emit `worktree.*`。

### 6.5 修改 workspace_mode

`requires_worktree` 字段**可改**（修正 v1 设计），前置条件：

- Task 在非终态（`open` 或 `suspended`）
- Task **无活动 execution**（先 kill 当前 execution 或等结束）

事件：`task.workspace_mode_changed { task_id, old_mode, new_mode, by, at }`。

---

## § 7. Task 依赖（depends_on_task_ids）

### 7.1 语义

```
T-B.depends_on_task_ids = [T-A1, T-A2]
=>  T-B 可 dispatch 的前提:
        T-A1.status == 'done' AND T-A2.status == 'done'
```

- 用 JSON 数组字段（[conventions § 9.x](../../../../rules/conventions.md)：简单 list-of-id 用 JSON 不做 join 表）
- **不引入新 Task 状态**（如 `blocked`）；"是否可派"是**计算属性**（查 deps 状态）

### 7.2 Dep 状态对应可派性

| Dep 状态组合 | T-B 可派? | 怎么处理 |
|---|---|---|
| 所有 dep = `done` | ✅ | supervisor 决定何时派 |
| 任一 dep ∈ `open` / `suspended` | ❌（暂时） | dep 完成事件唤醒 supervisor 重评估 |
| 任一 dep = `abandoned` | ❌（永久） | supervisor 决定 abandon T-B 或人工介入 |

> dep 进 abandoned 不自动 cascade abandon 依赖者；自动 cascade 推 [roadmap](../../../roadmap.md)。

### 7.3 创建时校验

- 所有 dep task_id 存在（不存在则拒绝）
- 不能 self-dep
- 整个 dep 图无环（DFS 检测，深度上限 32）
- 允许跨 project 依赖（v1 不限制；后续按需收紧）

### 7.4 运行时修改 deps

允许，前置：

| 条件 | 必须 |
|---|---|
| Task 在非终态 | `status ∈ {open, suspended}` |
| Task **无活动 execution** | `current_execution_id IS NULL` |

**Add dep 额外校验**：

- ✅ dep task_id 存在
- ✅ 不能 self-dep
- ✅ 加完无环（DFS 更新图）
- ❌ **不能 add 一个 `abandoned` 状态的 dep**（明示性强；想 add 必须该 dep 还活着）

**Remove dep**：dep 必须在当前 list 中（不允许静默 no-op）。

事件：

- `task.dependency_added { task_id, dep_task_id, by, at }`
- `task.dependency_removed { task_id, dep_task_id, by, at }`

权限：User / Supervisor 可改；Worker / Agent 不行。

### 7.5 跟 parent_task_id 的区分

| 概念 | 含义 | 阻塞? |
|---|---|---|
| `parent_task_id` | 父子血缘（"我从哪个 task spawn 出来"） | ❌ 不阻塞 |
| `depends_on_task_ids` | 前置依赖（"我等谁 done"） | ✅ 阻塞 |

如果想"父完成才能子派" → **显式**把 `parent_task_id` 加到子 task 的 `depends_on_task_ids`。

### 7.6 查询能力

- `agent-center query tasks --blocked-by=<task_id>` —— 反查"我 done 后能解锁谁"
- `inspect task <id>` 输出含 "blocked by: T-3 (open), T-7 (suspended)"

---

## § 8. Retry 策略

详细决策见 [ADR-0010](../../../decisions/0010-task-execution-two-layer-model.md)。本节给架构层概要。

### 8.1 不自动 retry —— Supervisor 决定

- TaskExecution 进 `failed` → emit 事件 → wake supervisor
- Supervisor 看 `failed_reason` + `failed_message` + 上下文 → 决定：
  - 重派同一 task（创建新 execution）
  - 开 Issue 升级用户
  - 通知用户后放置
  - 啥也不做

> Center 端零内置重试逻辑。retry = 普通 dispatch。

### 8.2 兜底：max_executions_per_task

- v1 全局默认 `max_executions_per_task=3`
- 同一 task 的 task_executions 行数 ≥ 3 时，center 拒绝新 dispatch
- 触发 emit `task.dispatch_limit_reached`，supervisor wake → 多半走 Issue 升级
- 推迟到 [roadmap](../../../roadmap.md)：per-project override

### 8.3 retry 与首次 dispatch 共代码路径

`agent-center dispatch <task_id>` 一个命令；任何 task `status=open` 都能派；状态机自然区分首次 / retry。

---

## § 9. Timeout 策略

### 9.1 4 类独立 timeout

| # | 类型 | 看的对象 | v1 默认 | 触发后 |
|---|---|---|---|---|
| 1 | `submitted_timeout` | TaskExecution 在 submitted | 5 min | execution → failed(`submitted_timeout`) |
| 2 | `execution_timeout` | TaskExecution working 累计 | 6 h | Center 发 kill_requested → SIGTERM → execution → failed(`execution_timeout`) |
| 3 | `input_request_timeout` | InputRequest 在 pending | T1=4h ping / T2=24h fail（见 [04-input-required.md](02-input-required.md)） | InputRequest → timed_out → execution → failed(`input_timeout`) |
| 4 | `worker_heartbeat_timeout` | Worker 心跳静默 | 60 s | worker → offline；上面所有 active execution → failed(`worker_lost`) |

### 9.2 关键细节

- **execution_timeout 不计 input_required 时间**：用 `working_seconds_accumulated` 字段，worker heartbeat 自上报；input_required 期间时钟暂停
- **per-task override**：dispatch envelope 可带 `execution_timeout_override`（仅向上覆盖，避免恶意短 timeout）；其他 timeout 不允许 task 级 override
- **TimeoutScanner**：Center 端独立 background ticker（30s 周期）扫所有 active executions，触发 timeout 检查
- **不引入新事件**：timeout 触发的 fail 复用 `task_execution.failed`，reason 字段表达细分

### 9.3 worker_lost 后的恢复

- Worker 重连 → 走 reconcile 协议（§ 4.5） → 拿到 `stale` 列表 → kill 本地僵尸 agent
- 旧 execution_id 永远停在 failed；新 execution_id 在新 worker 上跑

---

## § 10. Artifacts

详见 [05-observability.md](../observability/01-observability.md) 与实现层 schema。

### 10.1 模型

- **独立表 `artifacts`**（不是 task JSON 字段）—— 跨 task 查询友好；增量上报并发安全
- **归属 execution**（不是 task）—— 每次执行各产物
- `kind` 是 **free-form 字符串**（v1 推荐 `pr_url` / `file` / `report` / `diff` / `test_run` / `note`，不强校验）
- 大内容（>10KB）走 BlobStore，`blob_ref` 字段引用（[conventions § 8](../../../../rules/conventions.md)）

### 10.2 字段

| 字段 | 含义 |
|---|---|
| `id` | uuid |
| `task_id` | FK（冗余但便于 query） |
| `execution_id` | FK（artifact 归属一次执行） |
| `kind` | TEXT free-form |
| `title` | 短标签 |
| `blob_ref` | BlobStore 相对路径（nullable） |
| `url` | 外部 URL（nullable） |
| `metadata_json` | type-specific 字段（JSON 字符串） |
| `created_at` | timestamp |
| `created_by` | agent:execution-id / supervisor:invocation-id |

### 10.3 上报方式

```
agent-center report-artifact --kind=pr_url --title="..." --url=https://...
agent-center report-artifact --kind=file --title="..." --blob-ref=$(agent-center blob put design.md)
agent-center report-artifact --kind=test_run --title="..." --metadata='{"passed":10,...}'
```

CLI → worker daemon → forward to center → insert + emit `artifact.uploaded`。

### 10.4 不可变 / append-only

- 一旦 insert 不可修改
- 要"更新" → insert 新一条（inspect 按 created_at desc 显示，最新覆盖视觉）
- v1 不允许删除（DB 行保留；BlobStore lifecycle 独立）

---

## § 11. Issue conclude → spawn tasks

详细 conclude 流程归 [03-issue-discussion.md](../discussion/01-issue-discussion.md)。本节只列对 Task 的影响。

### 11.1 批量事务性 spawn

User concludes Issue with `closed_with_tasks` → 一次性批量创建 N 个 tasks（all-or-nothing），支持 batch 内 task 间引用依赖。

### 11.2 Conclude spec 形态

```yaml
issue_conclude_spec:
  resolution: "closed_with_tasks"
  tasks:
    - local_id: "t1"                # batch 临时占位符
      title: "..."
      project_id: ...
      requires_worktree: true
      priority: high
      eta_at: "2026-05-20T18:00:00Z"
      depends_on: []                # 引用既有 task uuid 或 local_id
    - local_id: "t2"
      depends_on: ["t1"]            # batch 内 local_id
    - local_id: "t3"
      depends_on: ["t1", "T-39"]    # 混用 local_id + uuid
  closing_comment: "..."
```

### 11.3 处理流程

```
1. Center 收 conclude RPC
2. 校验:
   - resolution 合法
   - 各 task spec 字段合法
   - depends_on 引用的既有 uuid 都存在
   - batch 内的 local_id 引用全部 resolve
   - dep 图无环 (含跨 batch + batch 内)
3. 单 DB 事务:
   - 为每个 task spec 分配 uuid
   - 构造 local_id → uuid 映射
   - 把 depends_on 中的 local_id 替换为 uuid
   - INSERT N 条 tasks (status=open, from_issue_id=this_issue)
   - INSERT 1 条 IssueComment (closing_comment)
   - UPDATE issue.status = 'closed_with_tasks'
4. emit issue.concluded { issue_id, resolution, task_ids }
5. emit issue.tasks_spawned { issue_id, task_ids }
6. 全部成功才 commit
```

---

## § 12. Cross-BC 交互

### 12.1 Supervisor 唤醒事件白名单

Task 模型层产生的事件中，触发 supervisor 唤醒的子集：

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
| `worker.online` | 重评估堆积 open task |
| `worker.offline` | 处理 worker_lost 的 active executions |

详细 supervisor 模型见 [06-supervisor-model.md](../cognition/01-supervisor-model.md)。

### 12.2 Supervisor 查询用 Observability CLI

Supervisor 是 Observability 的客户，跟 user 一样用 CLI 查 task / execution 状态。**不**为 supervisor 单造 RPC。

常用：

```
agent-center query tasks --status=open --project=X
agent-center inspect task T-42
agent-center inspect execution E-7
agent-center query executions --status=failed --since=24h
agent-center query tasks --not-dispatchable
agent-center ps
```

详见 [05-observability.md](../observability/01-observability.md) + supervisor.md skill。

### 12.3 任务时间线还原

events 表带 `refs` 字段（JSON）含 `task_id` / `execution_id` / `input_request_id` 等。`inspect task X --timeline` 查 `refs->>'task_id'=X OR refs->>'execution_id' IN (...)` 按 `occurred_at, seq` 排序。

派生事件（如 `task.done`）也落 events，actor=`system`，便于一眼看完整时间线。

---

## § 13. 不变量（Invariants）

设计 / 实现都要保持：

1. **Task 永远绑一个 project**（[conventions § 1](../../../../rules/conventions.md)）
2. **TaskExecution 永远绑一个 task 和一个 worker**（创建后不变）
3. **同一 task 任意时刻最多 1 条 active execution**（单活约束）
4. **执行权威在 Center**：Worker / Agent 不能直接改 task / execution 状态
5. **状态机迁移单向**：终态不可逆（done / abandoned / completed / failed / killed）
6. **execution_id 唯一**：retry 创建新 id；旧 id 永远停在终态
7. **每个 reason 字段配 message**（[conventions § 16](../../../../rules/conventions.md)）
8. **大字段走 BlobStore**（[conventions § 8](../../../../rules/conventions.md)）

---

## § 14. 不做的事（Out-of-Scope / Future Work）

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
| 实时 stream timeline（CLI tail -f）| [roadmap](../../../roadmap.md) |
| Issue reopen 后 amend 已 spawn task 集 | [roadmap](../../../roadmap.md) |
| Task unbind conversation（非 null → null）| [roadmap](../../../roadmap.md)（v1 conversation 跟 task 同生命周期；切渠道场景 v2 再做）|
| Task ↔ Conversation 多 tracker（1:N，多用户多渠道镜像）| [roadmap](../../../roadmap.md)（[ADR-0017 § 9](../../../decisions/0017-task-as-conversation.md) 显式驳回 v1）|

---

## § 15. 引用文档

- [ADR-0010 两层模型 + AgentSession 合并](../../../decisions/0010-task-execution-two-layer-model.md)
- [ADR-0011 派单可靠性协议](../../../decisions/0011-dispatch-reliability-protocol.md)
- [ADR-0014 事件溯源走 L1](../../../decisions/0014-event-sourcing-level.md)
- [ADR-0015 agent_trace 不进 events 表](../../../decisions/0015-agent-trace-not-in-events-table.md)
- [ADR-0017 Task ↔ Conversation 1:1](../../../decisions/0017-task-as-conversation.md)
- [01 限界上下文 & UL](../../strategic/03-bounded-contexts.md)
- [04 InputRequired 协议](02-input-required.md)
- [05 可观测性](../observability/01-observability.md)
- [07 Worker 执行模型](../workforce/01-worker-model.md)
- [08 Prompt 组装](../_cross-cutting/01-prompt-assembly.md)
- [09 飞书集成](../bridge/01-feishu-integration.md)
- [12 Conversation 统一会话层](../conversation/01-conversation.md)
- [conventions](../../../../rules/conventions.md)（§ 1 / § 2 / § 8 / § 9 / § 16）
