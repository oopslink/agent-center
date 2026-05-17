# Supervisor 运行模型

回答"Supervisor 是什么、怎么被唤醒、怎么做决策、怎么管自己的记忆 / 决策审计"。

Supervisor 是中心的"调度官 agent"。它是 LLM 驱动的真 agent（spawn `claude` 子进程实现），不是规则路由器；跟 worker agent 是**同源**（都是 claude 实例），区别仅在工具集和上下文（[ADR-0003](../decisions/0003-supervisor-not-brain.md)）。

跟其它架构层文档的边界：
- 具体表 schema / CLI 签名归 [implementation/](../implementation/)
- "为什么这样定"归 [decisions/](../decisions/)（核心 ADR：[0012 Memory file-based](../decisions/0012-memory-file-based.md) / [0013 Invocation 并发模型](../decisions/0013-supervisor-invocation-concurrency.md)）
- Supervisor 在 BC 地图里的位置见 [01-bounded-contexts.md § BC5](01-bounded-contexts.md)

---

## § 1. 运行形态：C+D 合体

事件驱动外壳（C） + 工具调用做动作（D）：

- 每次唤醒事件触发：center spawn 一个**短生命周期**的 `claude` 子进程（带 `supervisor.md` skill + Memory ancestor walk 自动加载）
- Supervisor 通过 `Bash` 工具调 `agent-center <subcommand>` 完成读 / 写
- 进程结束后所有状态都落在外部存储：
  - `supervisor_invocations` 表（DB）
  - `decision_records` 表（DB）
  - `events` 表（DB）
  - `$AGENT_CENTER_MEMORY_DIR/` git 仓（文件系统）
- 下一个事件来就是又一个全新进程

**没有常驻 supervisor 进程**。`SupervisorInvocation` ≠ supervisor 本身 —— 每次唤醒是一次 invocation，跑完就消失。

参见 [ADR-0002 不用 LLM SDK 走 CLI agent](../decisions/0002-no-llm-sdk-use-cli-agents.md)。

---

## § 2. SupervisorInvocation

### 2.1 定义

`SupervisorInvocation` = 一次 `claude` 子进程从 spawn 到退出的**审计单元**。Cognition BC 的核心聚合根。

| 维度 | 内容 |
|---|---|
| 进程 | center 机器上一个 `claude` 子进程（秒到分钟级寿命） |
| 输入 | 唤醒事件（coalescing 后的 batch）+ Memory ancestor walk 注入 + `supervisor.md` skill |
| 工作 | 在 prompt 内推理 → 通过 `Bash` 工具调 `agent-center <cmd>` 实施动作 |
| 输出 | 该 invocation 内每次动作 CLI 落 `decision_records` + 触发其他 BC 的 domain events；claude 自身的 JSONL trace 通过 `--session-id=invocation-id` 关联 |
| 终态 | claude 进程退出后 invocation 行回填 `ended_at` + `status` + `token_usage` + `decisions_made_count` |

详细 ADR：[0013 Supervisor Invocation 并发模型](../decisions/0013-supervisor-invocation-concurrency.md)。

### 2.2 状态机

4 态，无 `pending` 状态（行在 spawn 时刻直接落 `running`）：

```
                  (created on spawn)
                       ↓
                  ┌──────────┐
                  │ running  │
                  └────┬─────┘
       claude exit 0   │   claude exit ≠ 0
                       │   或 OOM / center restart 等
       ┌───────────────┼───────────────┐
       │               │               │
       ↓               ↓ (hard timeout) ↓
  ┌─────────┐      ┌──────────┐    ┌────────┐
  │succeeded│      │ timed_out│    │ failed │
  └─────────┘      └──────────┘    └────────┘
       (终态)         (终态)          (终态)
```

| 状态 | 含义 |
|---|---|
| `running` | claude 进程在跑 |
| `succeeded` | claude 进程 exit 0 |
| `failed` | claude 进程 exit ≠ 0；reason 字段表多样性 |
| `timed_out` | 命中 hard timeout，center SIGTERM → 5s grace → SIGKILL |

`failed_reason` 枚举（必配 `failed_message`，[conventions § 16](../../rules/conventions.md)）：

| reason | 含义 |
|---|---|
| `claude_nonzero` | claude 子进程 exit ≠ 0 |
| `cli_command_error` | 某个 supervisor 调的动作 CLI 异常退出（导致 claude 主程序也异常退出） |
| `oom` | 进程 OOM killed |
| `center_restart_orphan` | center 重启 reconcile 时发现 status='running' 但进程已不存在的孤儿行 |
| `killed_by_admin` | 管理员显式 SIGKILL（admin tool 罕用） |
| `unknown` | 兜底 |

### 2.3 Hard timeout（per scope_kind）

| scope_kind | timeout |
|---|---|
| `task` / `issue` / `conversation` / `worker` | 180s |
| `global`（周期 review 类）| 600s |

超时：center SIGTERM → 5s grace → SIGKILL → `status='timed_out'`、`timed_out_at` 填写。

### 2.4 失败 / 超时：alert + 人工 retrigger

`failed` / `timed_out` 不**自动重试**。emit `supervisor.invocation_failed_alert` 事件 → Bridge 推飞书 + Web Console flag。人工 `agent-center supervisor retrigger <invocation-id>` 用同样 `trigger_event_ids` 起一个新 invocation。

### 2.5 Center crash 恢复

Center 重启时：

1. 扫 `supervisor_invocations.status='running'` → 改 `failed(reason='center_restart_orphan')`、emit `supervisor.invocation_failed_alert`
2. 按 scope_kind/scope_key 扫 `events` 表：取 `occurred_at > last_succeeded_invocation.started_at AND event_id NOT IN any trigger_event_ids` 的事件
3. 这些是"未被任何成功 invocation 覆盖"的事件 → 重建 coalescing window 续跑

**语义区分**：
- `claude_nonzero` / `timed_out` / `cli_command_error` → invocation "试了" → 不重试
- `center_restart_orphan` → invocation "未尝试" → 自动 replay

### 2.6 字段

详细 schema 见 [implementation/02-persistence-schema.md](../implementation/)（TBD）。架构层字段语义：

| 字段 | 类型 | 含义 |
|---|---|---|
| `id` | uuid PK | 主身份；同时是 claude `--session-id` 实参 |
| `scope_kind` | enum | task / issue / conversation / worker / global |
| `scope_key` | string | "T-42" / "I-7" / "C-3" / "W-1" / "_global_" |
| `trigger_event_ids` | JSON array | ≥1 个事件 id（coalesced batch 用同一行表达） |
| `status` | enum | running / succeeded / failed / timed_out |
| `failed_reason` | string \| null | failed 时填，配 `failed_message` |
| `failed_message` | string \| null | 人读详情 |
| `timed_out_at` | timestamp \| null | 命中 hard timeout 时刻 |
| `hard_timeout_seconds` | int | 落座时按 scope_kind 决定，便于 inspect |
| `started_at` | timestamp | invocation 行落座时刻 |
| `ended_at` | timestamp \| null | 终态时刻 |
| `token_usage` | JSON \| null | `{input, output, cache_read, cache_create, ...}`，claude exit 后回填 |
| `decisions_made_count` | int | 该 invocation 内 decision_records 行数（便于 stats） |
| `prompt_blob_ref` | string \| null | 全 prompt > 10 KB 时走 BlobStore 引用（[conventions § 8](../../rules/conventions.md)） |

---

## § 3. 调度 / 唤醒 / 节流

详细 ADR：[0013 Supervisor Invocation 并发模型](../decisions/0013-supervisor-invocation-concurrency.md)。

### 3.1 唤醒事件白名单

唤醒事件清单的权威来源 = [02-task-model.md § 12.1](02-task-model.md)（task 模型层）+ 本节补充。**`supervisor.*` 事件不在白名单**，避免自唤醒循环。

唤醒事件完整列表：

```
Task / Execution / InputRequest:
  task.created / task.priority_changed / task.eta_changed /
  task.dependency_added / task.dependency_removed /
  task.done / task.dispatch_limit_reached
  task_execution.completed / task_execution.failed /
  task_execution.killed
  input_request.requested / input_request.timed_out

Issue / Comment:
  issue.opened / issue.commented / issue.concluded / issue.withdrawn

Conversation:
  conversation.message_added

Worker:
  worker.online / worker.offline
  worker_project_proposal.proposed

Global:
  supervisor.periodic_review_ticker      ← center cron-ish ticker emit 的合成事件
```

### 3.2 Scope key 派生（路由表）

事件 → scope_key 是 deterministic、side-effect-free 的纯函数。Center 端硬编码：

| event_type 前缀 | refs 取值 | 派生 scope_key |
|---|---|---|
| `task.*` / `task_execution.*` / `input_request.*` | `refs.task_id=T` | `task:T` |
| `issue.*` | `refs.issue_id=I` | `issue:I` |
| `conversation.message_added` | `refs.conversation_id=C` | `conversation:C` |
| `worker.*` / `worker_project_proposal.*` | `refs.worker_id=W` | `worker:W` |
| `supervisor.periodic_review_ticker` | —（合成事件无 refs） | `global` |

事件落 events 表 → wake scheduler 扫到白名单事件 → 派生 scope_key → 进 coalescing window。

### 3.3 并发约束

同 scope_key 至多 1 个 running invocation；不同 scope_key 可并行。全局上限 `max_concurrent_invocations=5`（v1 默认，可配）；超过则进 in-memory FIFO 队列，任一 invocation 结束 → 出队一个 spawn。

跨 scope 决策冲突（如两个并行 invocation 互不知情下都决定派任务给 W-2）**不在 Cognition BC 内协调**，由 [ADR-0010 单活约束](../decisions/0010-task-execution-two-layer-model.md) + [ADR-0011 ACK 协议](../decisions/0011-dispatch-reliability-protocol.md) 兜底。

### 3.4 Coalescing window（per scope_key, in-memory）

关窗规则：

- **滚动 30s since last event** —— 同 scope 30s 内连续来事件 merge 进同一 batch
- **硬上限 5 min since first event** —— 防 burst 拖死，强制关窗

关窗后 → 检查全局并发 < 5 → spawn invocation；否则进全局 FIFO 队。

### 3.5 周期 review 触发

Center 内一个 cron-ish ticker（schedule 由 `server.yaml` 配，默认每日 9 AM），到点 emit 合成事件 `supervisor.periodic_review_ticker` → 路由到 `global` scope → 走正常 coalescing + queueing 路径，不绕过节流。

### 3.6 反循环

- `supervisor.*` 事件**不**在 wake 白名单
- `supervisor.invocation_failed_alert` 推 Bridge / Web Console，不会自唤醒 supervisor

### 3.7 In-memory state + crash 恢复

Coalescing window 和 FIFO 队列都是 in-memory。Center crash 即丢；重启走 § 2.5 的自动 replay 逻辑重建。

### 3.8 配置项（server.yaml 汇总）

```yaml
supervisor:
  coalescing_window_seconds: 30           # 滚动
  coalescing_hard_cap_seconds: 300        # since first event
  max_concurrent_invocations: 5           # 全局
  periodic_review_schedule: "0 9 * * *"   # cron
  invocation_timeout_seconds:
    task: 180
    issue: 180
    conversation: 180
    worker: 180
    global: 600
  memory_dir: /var/lib/agent-center/memory
```

---

## § 4. Memory

详细 ADR：[0012 Supervisor Memory 走 file-based + git](../decisions/0012-memory-file-based.md)。

### 4.1 存储形态：file-based + git 仓

`$AGENT_CENTER_MEMORY_DIR`（默认 `/var/lib/agent-center/memory/`）整个目录是一个 git 仓，每个 scope 一个 `CLAUDE.md`：

```
$AGENT_CENTER_MEMORY_DIR/         ← git 仓根
├── .git/
├── CLAUDE.md                     # global scope
├── supervisor.md                 # supervisor 自反（不在 ancestor walk 链）
├── projects/<project_id>/
│   ├── CLAUDE.md                 # project scope
│   ├── tasks/<task_id>/CLAUDE.md         # task scope
│   └── issues/<issue_id>/CLAUDE.md       # issue scope
├── workers/<worker_id>/CLAUDE.md
└── conversations/<conv_id>/CLAUDE.md
```

不引入 `agent_memory` SQL 表。无 `expires_at` / `pinned` / `tags` 等管理字段。

### 4.2 7 种 scope_kind

| scope_kind | scope_key | 例子 |
|---|---|---|
| `task` | `<task_id>` | "T-42 必须在邻迁后重跳" |
| `issue` | `<issue_id>` | "I-7 用户要求 ASCII art 风格" |
| `conversation` | `<conversation_id>` | "C-3 用户偏好简答" |
| `worker` | `<worker_id>` | "W-1 偶尔丢心跳但主机正常" |
| `project` | `<project_id>` | "X-billing 一律 worktree 起 PR" |
| `global` | `_global_` | "用户在太平洋时区" |
| `supervisor` | `_supervisor_` | "我偶尔过于急于派单，决定多等 30s" |

注意：Memory 的 7 种 scope **多于** invocation 的 5 种 scope_kind（多 `project` 和 `supervisor`）—— invocation 是唤醒驱动的，没有 "project 事件" 这类；Memory 是经验维度的，可按 project 累积。

### 4.3 加载机制：claude code 原生 ancestor walk

Invocation 启动按 scope_kind 设 CWD：

| Invocation scope | CWD | Ancestor walk 自动加载 |
|---|---|---|
| `task:T-42`（project X）| `projects/X/tasks/T-42/` | T-42 + project X + global ✅ |
| `issue:I-7`（project X）| `projects/X/issues/I-7/` | I-7 + project X + global ✅ |
| `conversation:C-3` | `conversations/C-3/` | C-3 + global ✅ |
| `worker:W-1` | `workers/W-1/` | W-1 + global ✅ |
| `global` | `$MEMORY_DIR/` | global ✅ |

把 task / issue 嵌在 project 目录下 → ancestor walk 自动覆盖 task + project + global 三层，**不需要 `@import` 头**。

### 4.4 `supervisor.md` 自反 memory

文件名不是 `CLAUDE.md`，**不在 ancestor walk 路径上**。`supervisor.md` skill 顶部硬指令：

```
Always Read $AGENT_CENTER_MEMORY_DIR/supervisor.md first to recall your meta-cognitive notes.
```

### 4.5 HOME 隔离

Spawn `claude` 时显式设 `HOME` / `CLAUDE_CONFIG_DIR` 到隔离目录，避免吃到 user 私人 `~/.claude/CLAUDE.md` 污染。

### 4.6 冷启动骨架

Center 订阅事件，立即创建对应 `CLAUDE.md` 空骨架（H1 + 空白）并 git commit（author=`system:bootstrap`）：

| 事件 | 创建文件 |
|---|---|
| `project.created` | `projects/<id>/CLAUDE.md` |
| `task.created` | `projects/<X>/tasks/<id>/CLAUDE.md` |
| `issue.opened` | `projects/<X>/issues/<id>/CLAUDE.md` |
| `worker.enrolled` | `workers/<id>/CLAUDE.md` |
| `conversation.opened` | `conversations/<id>/CLAUDE.md` |

骨架内容示例（task scope）：

```markdown
# Task T-42 memory

<!-- supervisor 的笔记写在这里 -->
```

### 4.7 写入 / 维护

Supervisor 用原生 `Edit` / `Write` 工具直接改对应文件。**压缩 / rewrite / TTL 全由 supervisor 自决，center 不掺和**。`supervisor.md` skill 给一般性建议（"保持条目简练，过期可摘要"），不设硬 cap。

### 4.8 Git commit 策略（混合）

- Supervisor 在 invocation 内用 `Bash` 调 `git -C $MEMORY_DIR commit -am "..."` 写**语义 commit**（message 说"为什么这样改"）
- Invocation 退出时 center 兜底：仓内有 dirty 变化 → `git add -A && git commit -m "auto: invocation <id> (scope=...) - remaining"`
- Commit author 通过 env 注入：`GIT_AUTHOR_NAME="supervisor"`、`GIT_AUTHOR_EMAIL="supervisor:<invocation-id>@agent-center.local"`
- User 自己 vim + `git commit` 也走同一仓，author=`user:<id>`

### 4.9 并发写：不加锁

共享 scope 文件（`global/CLAUDE.md` / `supervisor.md` / `projects/X/CLAUDE.md`）在跨 scope 并行 invocation 下可能被并发 Edit；接受偶发 race（v1 单用户低频）；v2+ 视场景加 advisory lock（[roadmap](../roadmap.md)）。

### 4.10 GC：终态实体永远保留

Task done/abandoned、Issue closed/withdrawn、Worker un-enrolled、Conversation closed 后，其 `CLAUDE.md` **不归档不删除**。markdown 体积极小，留供复盘 + supervisor 跨实体类比。

### 4.11 Observability：复用现有渠道

不引入 `memory.*` 事件类。变更已有两条审计：

- `agent_trace.event`（worker-side 解出）—— claude `Edit` / `Write` 工具调用本身记录 `payload.tool` + `payload.file_path`，filter 即得
- `git log` —— commit message + diff + author，原生 audit

---

## § 5. DecisionRecord

### 5.1 定义

`DecisionRecord` = supervisor 在一次 invocation 内做的一次"对外决策"的结构化条目。

- "对外" = 影响其他 BC 状态的动作（dispatch / kill / open issue / 给用户发消息 / 升级 input request 等）
- "决策" = 一次完整 action 单位，不是底层 tool call 颗粒
- "结构化" = 字段化行（kind / target_refs / rationale / outcome），不是自由文本
- "Memory 编辑不算 decision"（[ADR-0012](../decisions/0012-memory-file-based.md) 后 memory 是 agent 私事，已被 agent_trace + git log 双渠道审计）

1 个 `SupervisorInvocation` : N 个 `DecisionRecord`（0/N，可能"无 decision"如 no_op invocation）。

### 5.2 跟邻近概念的区分

| 概念 | 性质 | 关系 |
|---|---|---|
| **SupervisorInvocation** | 启动→退出容器 | 1:N → DecisionRecords |
| **Event**（events 表） | 状态变化原子记录 | 1 decision → 0/N events |
| **Memory**（CLAUDE.md 文件） | 私事笔记 | 平行；memory 写不入 decision_records |
| **agent_trace.event** | Tool call 颗粒（worker-side 解出 / supervisor 用同一链路） | DecisionRecord 是它的"聚合视图" |

### 5.3 字段

详细 schema 见 [implementation/02-persistence-schema.md](../implementation/)（TBD）。架构层字段语义（**append-only，INSERT 后不可变**）：

| 字段 | 类型 | 含义 |
|---|---|---|
| `id` | uuid PK | 主身份 |
| `invocation_id` | uuid FK → `supervisor_invocations.id` | 归属 invocation |
| `kind` | enum | 12 种之一（§ 5.4） |
| `target_refs` | JSON | 决策影响的实体引用（如 `{task_id, worker_id}`） |
| `rationale` | text NOT NULL | supervisor 解释"为什么这样决定"（必填） |
| `outcome` | enum | `succeeded` / `failed`（CLI 同步返回结果） |
| `outcome_message` | text \| null | 失败时人读详情 |
| `created_at` | timestamp | 写入时刻 |

### 5.4 Kind 闭集（12 种，跟 CLI 命令 1:1）

```
dispatch
kill_execution
abandon_task / suspend_task / resume_task
open_issue / issue_comment / conclude_issue / close_issue
conversation_message
escalate_input_request
no_op
```

新增动作 CLI 命令必须**显式新增 kind enum** + schema migration。

### 5.5 创建时机

- **动作 CLI 内部自动 INSERT**：`dispatch` / `kill-execution` / `abandon-task` / `suspend-task` / `resume-task` / `issue open|comment|conclude|close` / `conversation add-message` / `escalate-input-request` 在 CLI 内部 INSERT decision_records 行
- **显式 `record-decision`** 仅供 `no_op` 决策使用 —— supervisor 决定"不动某实体"时调一次：

```
agent-center record-decision \
  --kind=no_op \
  --target=task:T-39 \
  --rationale="T-39 在 input_required 状态，等用户回复"
```

不调任何 CLI 的"沉默思考"是不被记录的；想留痕就 `record-decision`。

### 5.6 处理流程（动作 CLI 内部事务）

```
BEGIN TRANSACTION;
TRY:
  domain_logic()       -- 校验 + 写 task_executions / issues / etc.
  INSERT decision_records (..., outcome='succeeded');
  emit domain events WITH decision_id=<this_id>, correlation_id=invocation_id;
CATCH err:
  INSERT decision_records (..., outcome='failed', outcome_message=str(err));
  -- 不 emit 联动 domain 事件（动作未真正发生）
COMMIT;
```

`outcome` 字段**仅捕捉同步 CLI 返回结果**，下游异步状态（worker ACK / NACK / task_execution.completed / 等）通过 `events` 表 + `correlation_id=invocation_id` 反查得到，不在 decision_records 行更新。

### 5.7 Rationale 必填

所有动作 CLI 和 `record-decision` 都要求 `--rationale`，缺则 CLI 拒绝执行（跟 [conventions § 16](../../rules/conventions.md) "reason + message 双字段" 哲学对齐 —— rationale 担当 message 的角色）。

supervisor.md skill 顶部硬指令："每次动作都要交代为什么，哪怕一句话"。

### 5.8 Actor 推断

动作 CLI 通过 env `AGENT_CENTER_INVOCATION_ID` 推断 actor：

| env 值 | actor | 写 decision_records? |
|---|---|---|
| 有 | `supervisor:<invocation-id>` | ✅ |
| 无 | `user:<user_id>` | ❌（user 视角的"决定"已经在 events.actor 里有了） |

User 用同一套 CLI 调 `dispatch` 等命令，不写 decision_record；events 表保留 actor 区分。

### 5.9 反查能力

- `inspect supervisor <inv-id>`：列 invocation 元数据 + decisions 表（按 `created_at` 序）+ 每个 decision 的触发 events（JOIN by `decision_id`）
- `inspect decision <id>`：单条决策详情 + rationale + 触发事件链
- `query decisions`：`--invocation-id` / `--kind` / `--outcome` / `--since`

### 5.10 Events 表加 `decision_id` 列

`events.decision_id` (uuid, nullable) —— 非决策触发的事件（如 `worker.online`、worker-emit 的 trace 等）该列为 null。带 `decision_id` 的事件能直接 JOIN 回 `decision_records` 看到 rationale。

---

## § 6. Cross-BC 交互

### 6.1 Cognition → 其他 BC（写路径，全走 CLI）

Supervisor 通过 `Bash` 工具调 `agent-center <cmd>` 触动作。CLI 内部跑领域逻辑 + INSERT `decision_records` + emit 联动 events：

| 目标 BC | CLI 子命令 | 对应 DecisionRecord kind |
|---|---|---|
| Scheduling | `dispatch` | `dispatch` |
| Scheduling | `kill-execution` | `kill_execution` |
| Scheduling | `abandon-task` / `suspend-task` / `resume-task` | 同名 |
| Scheduling | `respond-to-input-request` | （走 `conversation_message` 或 `escalate_input_request`，看路径） |
| Discussion | `issue open` / `issue comment` / `issue conclude` / `issue close` | `open_issue` / `issue_comment` / `conclude_issue` / `close_issue` |
| Conversation | `conversation add-message` | `conversation_message` |
| 跨 BC | `escalate-input-request` | `escalate_input_request` |
| Cognition 自身 | `record-decision` | `no_op` |

### 6.2 Cognition ← 其他 BC（订阅事件触发 wake）

Cognition BC **不主动订阅**事件。Center 的 wake scheduler（§ 3）扫 events 表的 wake 白名单 → 派生 scope_key → coalescing window → 触发 spawn。

### 6.3 查询 / inspect：跟 user 共用同一套

Supervisor 用同样的 `inspect` / `query` / `ps` CLI 查 task / execution / issue / worker / conversation / 等。**不为 supervisor 单造 RPC**（[02-task-model § 12.2](02-task-model.md) / [05-observability § O5](05-observability.md)）。

### 6.4 Prompt 组装边界（06 / 08）

**06 contract（本文档）**：supervisor 的 prompt 由三块构成：

```
(a) supervisor.md skill                    ← bundled with binary
(b) ancestor walk 自动加载的 CLAUDE.md 链   ← § 4.3 决定的 scope → CWD 映射
(c) wake event payload                     ← trigger_event_ids 内容 + 上下文
```

`HOME` 隔离防 user `~/.claude/` 污染（§ 4.5）；`--session-id=<invocation_id>` 让 claude 自己的 trace 跟 invocation 关联。

**08 contract**（[08-prompt-assembly.md](08-prompt-assembly.md)）：worker-side prompt 组装（task envelope → worker daemon 拼最终 prompt），跟 supervisor 自己的 prompt 组装**两套独立机制**。08 的设计**不**复用到 supervisor 这边，反之亦然。

### 6.5 跟 worker-side agent 的对比

| 维度 | Supervisor agent | Worker agent |
|---|---|---|
| 触发 | 事件驱动（唤醒事件 → coalescing → spawn） | dispatch 驱动（envelope 到达 → spawn） |
| 短命/长命 | 短命，每次一个新进程 | 短命，每次一个新进程 |
| Skill 文件 | `supervisor.md` | `worker-agent.md` |
| 上下文注入 | Memory ancestor walk（自动）+ wake event payload | Task envelope content + worktree-local `CLAUDE.md` (project local) |
| Memory 来源 | `$AGENT_CENTER_MEMORY_DIR/` git 仓（agent 自管） | 工作树内项目自带的 `CLAUDE.md` / `AGENTS.md`（项目仓库管，[ADR-0005](../decisions/0005-project-charter-stays-in-project-repo.md)） |
| 调度可见性 | 通过 CLI 调 supervisor 写的动作命令 | 通过 unix socket 调 worker-agent 命令（`request-input` / `report-progress` / `open-issue` / 等） |

共性：都是 spawn `claude` / `codex` / 等 CLI 子进程，都吃 skill + 上下文。差异：supervisor 是事件驱动 + 跨 invocation 持久 memory；worker 是单任务执行 + 跟项目仓共生。

---

## § 7. 部署位置 / 资源契约

- Supervisor 进程跑在 **center 同机**（VPS）
- 假设 VPS 上 `claude` CLI 已可用（OAuth 已完成 / API key 已配；本项目不管登录流程）
- `$AGENT_CENTER_MEMORY_DIR` 在 center 机器本地文件系统（备份方案 = rsync 或推 git remote）
- `max_concurrent_invocations=5` 时高峰短时占用：5 个 claude 子进程 + 5 个并发 Anthropic API 调用

VPS 不足以承担高并发时（用户视感觉到延迟 / OOM），调小 `max_concurrent_invocations` 或提升 VPS 规格；目前 v1 保守默认。

---

## § 8. 不变量（Invariants）

1. **Supervisor 没有常驻进程**：每次 invocation 是 spawn → exit 全生命周期
2. **决策权威在 supervisor**：center 不做硬编码调度，所有派单 / 升级 / 关闭决策由 supervisor 跑出
3. **状态权威在 center**：supervisor 通过 CLI 写状态（同 worker / user 同套 CLI），不写本地副本
4. **同 scope_key 任意时刻最多 1 个 running invocation**（[ADR-0013](../decisions/0013-supervisor-invocation-concurrency.md)）
5. **DecisionRecord append-only**：INSERT 后不可变（含 outcome 字段）
6. **Memory 是 agent 私事**：写入 / 压缩 / TTL 全由 agent 自决，center 不掺和
7. **HOME 隔离**：spawn claude 必显式 `HOME` / `CLAUDE_CONFIG_DIR`，禁污染用户私人配置
8. **每个 decision 必带 rationale**（rationale 字段必填，类比 [conventions § 16](../../rules/conventions.md) 的 reason+message）
9. **`supervisor.*` 事件不进 wake 白名单**（反循环）

---

## § 9. 不做的事（Out-of-Scope / 推迟 Roadmap）

| 项 | 归属 |
|---|---|
| Supervisor 自动重试（claude_nonzero / timed_out 自动重发同 trigger）| [roadmap](../roadmap.md)（未来视失败率统计决定） |
| Task-level "long-no-update" auto-ping（每 task 加 idle timer 主动唤醒 supervisor）| [roadmap](../roadmap.md)（跟 [02-task-model § 14](02-task-model.md) "ETA 过期触发 supervisor 唤醒" 同类） |
| Cross-invocation 协调机制（多 invocation 间互锁）| [roadmap](../roadmap.md)（多 supervisor 之前） |
| Memory 并发写 advisory lock | [roadmap](../roadmap.md) |
| Memory 跨 BC 聚合查询（grep 工具化、Web Console 可视）| [roadmap](../roadmap.md) |
| 显式 `pending` invocation 状态（队列可见性）| [roadmap](../roadmap.md)（队列 metric / UI 时再加） |
| Memory file 体积监控告警（自动提醒 supervisor 压缩）| [roadmap](../roadmap.md) |
| Token cost 折算金钱 + 告警 | [roadmap](../roadmap.md)（已列） |
| 多 supervisor / 跨机器 | [roadmap](../roadmap.md)（多用户 / SaaS 之前） |

---

## § 10. 引用文档

- [ADR-0002 不用 LLM SDK 走 CLI agent](../decisions/0002-no-llm-sdk-use-cli-agents.md)
- [ADR-0003 Supervisor 而非 Brain](../decisions/0003-supervisor-not-brain.md)
- [ADR-0012 Memory file-based + git](../decisions/0012-memory-file-based.md)
- [ADR-0013 Supervisor Invocation 并发模型](../decisions/0013-supervisor-invocation-concurrency.md)
- [01 限界上下文 § BC5 Cognition](01-bounded-contexts.md)
- [02 Task 模型 § 12.1 唤醒事件白名单](02-task-model.md)
- [05 可观测性 § O3 Supervisor 调用全留档](05-observability.md)
- [08 Prompt 组装（worker-side）](08-prompt-assembly.md)
- [conventions](../../rules/conventions.md)（§ 1 / § 2 / § 8 / § 16）
