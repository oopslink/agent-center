# SupervisorInvocation 聚合（+ DecisionRecord 子从属）

> **DDD 战术层** · BC: Cognition · 聚合: SupervisorInvocation（AR）+ DecisionRecord（Entity，子从属）

`SupervisorInvocation` 是一次 `claude` 子进程从 spawn 到退出的**审计单元**。

| 维度 | 内容 |
|---|---|
| 进程 | center 机器上一个 `claude` 子进程（秒到分钟级寿命） |
| 输入 | 唤醒事件（coalescing 后的 batch）+ Memory ancestor walk 自动加载 + `supervisor.md` skill |
| 工作 | 在 prompt 内推理 → 通过 `Bash` 工具调 `agent-center <cmd>` 实施动作 |
| 输出 | 该 invocation 内每次动作 CLI 落 `decision_records` + 触发其他 BC 的 domain events；claude 自身的 JSONL trace 通过 `--session-id=invocation-id` 关联 |
| 终态 | claude 进程退出后 invocation 行回填 `ended_at` + `status` + `token_usage` + `decisions_made_count` |

详细 ADR：[0013 Supervisor Invocation 并发模型](../../../decisions/0013-supervisor-invocation-concurrency.md)。

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

**没有常驻 supervisor 进程**。参见 [ADR-0002 不用 LLM SDK 走 CLI agent](../../../decisions/0002-no-llm-sdk-use-cli-agents.md)。

---

## § 2. Invocation 状态机

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

### 2.1 `failed_reason` 枚举（必配 `failed_message`）

| reason | 含义 |
|---|---|
| `claude_nonzero` | claude 子进程 exit ≠ 0 |
| `cli_command_error` | 某个 supervisor 调的动作 CLI 异常退出（导致 claude 主程序也异常退出） |
| `oom` | 进程 OOM killed |
| `center_restart_orphan` | center 重启 reconcile 时发现 status='running' 但进程已不存在的孤儿行 |
| `killed_by_admin` | 管理员显式 SIGKILL（admin tool 罕用） |
| `unknown` | 兜底 |

遵循 [conventions § 16](../../../../rules/conventions.md) 双字段约定。

### 2.2 Hard timeout（per scope_kind）

| scope_kind | timeout |
|---|---|
| `task` / `issue` / `conversation` / `worker` | 180s |
| `global`（周期 review 类）| 600s |

超时：center SIGTERM → 5s grace → SIGKILL → `status='timed_out'`、`timed_out_at` 填写。

### 2.3 失败 / 超时：alert + 人工 retrigger

`failed` / `timed_out` 不**自动重试**。emit `supervisor.invocation_failed_alert` 事件 → Bridge 推飞书 + Web Console flag。人工 `agent-center supervisor retrigger <invocation-id>` 用同样 `trigger_event_ids` 起一个新 invocation。

### 2.4 Center crash 恢复

Center 重启时（详见 [00-overview § 3.4 InvocationCrashRecovery](00-overview.md)）：

1. 扫 `supervisor_invocations.status='running'` → 改 `failed(reason='center_restart_orphan')`、emit `supervisor.invocation_failed_alert`
2. 按 scope_kind/scope_key 扫 `events` 表：取 `occurred_at > last_succeeded_invocation.started_at AND event_id NOT IN any trigger_event_ids` 的事件
3. 这些是"未被任何成功 invocation 覆盖"的事件 → 重建 coalescing window 续跑

**语义区分**：

- `claude_nonzero` / `timed_out` / `cli_command_error` → invocation "试了" → 不重试
- `center_restart_orphan` → invocation "未尝试" → 自动 replay

### 2.5 In-memory state + crash 恢复

Coalescing window 和 FIFO 队列都是 in-memory。Center crash 即丢；重启走 § 2.4 的自动 replay 逻辑重建。

---

## § 3. 字段

详细 schema 见 [implementation/02-persistence-schema.md](../../../implementation/) (TBD)。架构层字段语义：

| 字段 | 类型 | 含义 |
|---|---|---|
| `id` | ULID/UUID PK | 主身份；同时是 claude `--session-id` 实参 |
| `scope_kind` | enum | task / issue / conversation / worker / global |
| `scope_key` | string | "T-42" / "I-7" / "C-3" / "W-1" / "_global_" |
| `trigger_event_ids` | JSON array | ≥1 个 event_id（coalesced batch 用同一行表达） |
| `status` | enum | running / succeeded / failed / timed_out |
| `failed_reason` | string \| null | failed 时填，配 `failed_message` |
| `failed_message` | string \| null | 人读详情 |
| `timed_out_at` | timestamp \| null | 命中 hard timeout 时刻 |
| `hard_timeout_seconds` | int | 落座时按 scope_kind 决定，便于 inspect |
| `started_at` | timestamp | invocation 行落座时刻 |
| `ended_at` | timestamp \| null | 终态时刻 |
| `token_usage` | JSON \| null | `{input, output, cache_read, cache_create, ...}`，claude exit 后回填 |
| `decisions_made_count` | int | 该 invocation 内 decision_records 行数（便于 stats） |
| `prompt_blob_ref` | string \| null | 全 prompt > 10 KB 时走 BlobStore 引用（[conventions § 8](../../../../rules/conventions.md)） |

---

## § 4. DecisionRecord（Entity，子从属）

### 4.1 定义

`DecisionRecord` = supervisor 在一次 invocation 内做的一次"对外决策"的结构化条目。

- "对外" = 影响其他 BC 状态的动作（dispatch / kill / open issue / 给用户发消息 / 升级 input request 等）
- "决策" = 一次完整 action 单位，不是底层 tool call 颗粒
- "结构化" = 字段化行（kind / target_refs / rationale / outcome），不是自由文本
- "Memory 编辑不算 decision"（[ADR-0012](../../../decisions/0012-memory-file-based.md) 后 memory 是 agent 私事，由 invocation `trace.jsonl.gz` + `git log` 双渠道审计，详见 [ADR-0015](../../../decisions/0015-agent-trace-not-in-events-table.md)）

1 个 `SupervisorInvocation` : N 个 `DecisionRecord`（0/N，可能"无 decision"如 no_op invocation）。

### 4.2 跟邻近概念的区分

| 概念 | 性质 | 关系 |
|---|---|---|
| **SupervisorInvocation** | 启动→退出容器 | 1:N → DecisionRecords |
| **Event**（events 表，Observability BC） | 状态变化原子记录 | 1 decision → 0/N events（through `events.decision_id`）|
| **Memory**（CLAUDE.md 文件） | 私事笔记 | 平行；memory 写不入 decision_records |
| **AgentTraceEvent**（JSONL 行） | Tool call 颗粒；**不入 events 表**（[ADR-0015](../../../decisions/0015-agent-trace-not-in-events-table.md)）| DecisionRecord 是它的"聚合视图"；想看细节走 `peek-trace` / `logs <invocation>` |

### 4.3 字段（append-only，INSERT 后不可变）

详细 schema 见 [implementation/02-persistence-schema.md](../../../implementation/) (TBD)。架构层字段语义：

| 字段 | 类型 | 含义 |
|---|---|---|
| `id` | ULID/UUID PK | 主身份 |
| `invocation_id` | ULID/UUID FK → `supervisor_invocations.id` | 归属 invocation（强引用，不可变）|
| `kind` | enum | 12 种之一（§ 4.4）|
| `target_refs` | JSON | 决策影响的实体引用（如 `{task_id, worker_id}`）|
| `rationale` | text NOT NULL | supervisor 解释"为什么这样决定"（必填）|
| `outcome` | enum | `succeeded` / `failed`（CLI 同步返回结果）|
| `outcome_message` | text \| null | 失败时人读详情 |
| `created_at` | timestamp | 写入时刻 |

### 4.4 Kind 闭集（12 种，跟 CLI 命令 1:1）

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

### 4.5 DecisionRecord Invariants

1. **append-only**：INSERT 后不可变（含 outcome 字段）
2. **必带 invocation_id**：跟 invocation 强引用，不可变
3. **rationale 必填**：缺则 CLI 拒绝执行（类比 [conventions § 16](../../../../rules/conventions.md)）
4. **outcome 只反映 CLI 同步结果**：下游异步状态（worker ACK / NACK / task_execution.completed / 等）通过 `events.correlation_id=invocation_id` 反查得到，不在 decision_records 行更新
5. **kind 闭集**：新增动作必须改 schema + migration

### 4.6 创建时机（DecisionWriter）

- **动作 CLI 内部自动 INSERT**：`dispatch` / `kill-execution` / `abandon-task` / `suspend-task` / `resume-task` / `issue open|comment|conclude|close` / `conversation add-message` / `escalate-input-request` 在 CLI 内部 INSERT decision_records 行
- **显式 `record-decision`** 仅供 `no_op` 决策使用 —— supervisor 决定"不动某实体"时调一次：

```
agent-center record-decision \
  --kind=no_op \
  --target=task:T-39 \
  --rationale="T-39 在 input_required 状态，等用户回复"
```

不调任何 CLI 的"沉默思考"是不被记录的；想留痕就 `record-decision`。

### 4.7 处理流程（动作 CLI 内部事务）

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

### 4.8 Actor 推断

动作 CLI 通过 env `AGENT_CENTER_INVOCATION_ID` 推断 actor：

| env 值 | actor | 写 decision_records? |
|---|---|---|
| 有 | `supervisor:<invocation-id>` | ✅ |
| 无 | `user:<user_id>` | ❌（user 视角的"决定"已经在 events.actor 里有了） |

User 用同一套 CLI 调 `dispatch` 等命令，不写 decision_record；events 表保留 actor 区分。

### 4.9 反查能力

- `inspect supervisor <inv-id>`：列 invocation 元数据 + decisions 表（按 `created_at` 序）+ 每个 decision 的触发 events（JOIN by `decision_id`）
- `inspect decision <id>`：单条决策详情 + rationale + 触发事件链
- `query decisions`：`--invocation-id` / `--kind` / `--outcome` / `--since`

### 4.10 Events 表加 `decision_id` 列

`events.decision_id` (uuid, nullable) —— 非决策触发的事件（如 `worker.online`、worker-emit 的 trace 等）该列为 null。带 `decision_id` 的事件能直接 JOIN 回 `decision_records` 看到 rationale。

---

## § 5. Invocation Invariants

1. **无 `pending` 状态**：行在 spawn 时刻直接落 `running`；没有"已排队等 spawn"的物理 invocation 行
2. **同 scope_key 至多 1 running**：[ADR-0013](../../../decisions/0013-supervisor-invocation-concurrency.md)
3. **`succeeded` / `failed` / `timed_out` 不可逆**：终态固定
4. **终态时 `ended_at` 必填**：跟 status != running 共填
5. **`failed_reason` 必带 `failed_message`**（[conventions § 16](../../../../rules/conventions.md)）
6. **`scope_kind` + `scope_key` 不可变**：创建时填，永不改
7. **`trigger_event_ids` ≥ 1**：至少一个事件触发
8. **`token_usage` claude exit 后回填**：是 invocation 终态时的快照，不增量更新
9. **不 emit `supervisor.*` 给自己的 wake**：反循环（[00-overview § 3.1 反循环](00-overview.md)）

---

## § 6. 部署位置 / 资源契约

- Supervisor 进程跑在 **center 同机**（VPS）
- 假设 VPS 上 `claude` CLI 已可用（OAuth 已完成 / API key 已配；本项目不管登录流程）
- `max_concurrent_invocations=5` 时高峰短时占用：5 个 claude 子进程 + 5 个并发 Anthropic API 调用
- VPS 不足以承担高并发时（用户视感觉到延迟 / OOM），调小 `max_concurrent_invocations` 或提升 VPS 规格；目前 v1 保守默认

---

## § 7. 配置项（server.yaml 汇总）

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

## § 8. References

- [ADR-0013 Supervisor Invocation 并发模型](../../../decisions/0013-supervisor-invocation-concurrency.md)
- [ADR-0002 不用 LLM SDK 走 CLI agent](../../../decisions/0002-no-llm-sdk-use-cli-agents.md)
- [ADR-0015 agent_trace 不进 events 表](../../../decisions/0015-agent-trace-not-in-events-table.md)
- [00-overview.md](00-overview.md) — BC 入口（WakeScheduler / DecisionWriter / 跨 BC 交互）
- [02-memory.md](02-memory.md) — Memory 聚合（invocation prompt 的另一个组成部分）
- [observability/00-overview.md](../observability/00-overview.md) — events / `inspect supervisor` / `query decisions`
- [conventions § 16](../../../../rules/conventions.md) — reason+message 双字段
