# Agent Runtime & Memory Management

## Unified CLI 进程模型

---

## 1. 概述

本文档定义 Agent 的运行时进程模型、上下文（Context）管理、记忆（Memory）管理三个方面的设计。

**核心设计决策**：

- 单一 Agent CLI 进程，线程池并发处理多个 Task
- 每个 Task 独立 LLM context window（内存隔离，非进程隔离）
- 双轨上下文恢复（快速路径：本地缓存；慢速路径：本地事件流回放）
- Agent 持久记忆（`memory/MEMORY.md`）存放经验、教训、技能、原则；业务实体 ground truth 在 Center

---

## 2. 术语

| 术语 | 所属 BC | 说明 |
|---|---|---|
| **Task** | ProjectManager | 工作单元；执行状态由 `block_reason` 注解 + `execution_lease` + `TaskActionLog` 承载 |
| **Agent** | Agent | 持久代理身份（Profile + Worker 绑定 + Skills + CapabilityTags） |
| **AgentLifecycle** | Agent | 7 态生命周期：stopped / running / stopping / resetting / error / failed / archived |
| **Availability** | Agent | 派生可用性信号（available / busy / unavailable），由 workerOnline + lifecycle + hasActiveTask 三因素计算 |
| **AgentActivityEvent** | Agent | Append-only LLM 交互事件流（SQLite `agent_activity_events` 表），按 `taskRef` 关联 Task |
| **Worker** | Workforce | 运行代理程序的机器守护进程 |
| **WorkerControlEvent** | Environment | Center → Worker 的有序可重放控制流命令（agent.reconcile / agent.work / agent.wake） |
| **ControlLog** | Environment | 控制流服务：per-Worker 单调 offset + 幂等去重 + SSE 实时下推 + 重连回放 |
| **AgentController** | Environment（Worker 端） | 命令执行器，驱动 SupervisorSession |
| **Memory** | Cognition | file-based + git 仓；单一 `MEMORY.md` 存放经验、教训、技能、原则。业务实体（Project/Task/Plan/Issue）ground truth 在 Center，不在本地镜像 |
| **Conversation** | Conversation | 消息线程，kind ∈ {dm, channel, task, issue} |
| **Plan** | ProjectManager | Task DAG 编排单元（4 态：draft / running / done / archived）；支持 Builtin（Assignment Pool）和 Structured（DAG）两类 |
| **NodeStatus** | ProjectManager | Plan 内 Task 节点的派生状态（blocked / ready / dispatched / running / paused / done / failed / skipped），纯函数计算，不持久化 |
| **DispatchRecord** | ProjectManager | 编排器唯一持久化状态：记录 ready node 的 @mention 已发出，防止重复派发 |
| **TaskRunGate** | Agent（端口） → ProjectManager（实现） | Task 可运行性门禁：Agent 开始执行前校验 Plan 依赖是否满足，防止抢跑 |
| **BlockReasonType** | ProjectManager | Task 运行中阻塞注解（`input_required` / `obstacle`） |
| **Center** | —（非 BC） | agent-center 服务器应用 |

---

## 3. 目录结构

每个 Agent 拥有独立的 Home 目录（`$AGENT_CENTER_DATA_DIR/{agent_id}/`），所有运行时数据均在此目录下管理，严禁路径逃逸。

```
{agent_id}/                          # Agent Home（containment root）
│
├── session.instance                 # Session CLI 单实例租约文件
│                                    #   内容：session_id, generation, pid, prev_pid, prev_crash_at
│
├── agent.sock                       # Session CLI 监听的 Unix Domain Socket
│                                    #   Worker ↔ Session CLI 通信
│
├── memory/                          # Agent 持久记忆（git 仓库）
│   ├── .git/
│   └── MEMORY.md                    # 经验、教训、技能、原则
│
├── plans/                           # Plan 级公共信息（agent 执行多个 Task 时的小抄）
│   └── {plan_id}/
│       └── plan.json                # Plan 级经验教训、失败记录等公共信息
│                                    #   Plan 结构的 ground truth 在 Center
│
└── tasks/                           # 所有 TaskExecution 根目录
    │
    ├── {task_id}/                   # 标准执行目录（pending/running/paused/failed/done）
    │   ├── task.json                # TaskExecution 元数据
    │   │                            #   内容：task_id, status, plan_id（可选）, created_at, updated_at
    │   ├── execution.json           # 本次执行上下文（fork 参数、重试计数等）
    │   │
    │   ├── events.current.jsonl     # 当前写入的事件流（状态恢复唯一可信源）
    │   ├── events.{seq}.jsonl.gz    # 已压缩归档的历史事件段（如 events.000001.jsonl.gz）
    │   ├── events.offset            # 当前 Center 消费位置
    │   │                            #   内容：segment, byte_offset, last_event_id
    │   │
    │   └── task.log                 # Task CLI 进程 stdout/stderr 日志
    │
    ├── {task_id}__aborted_{ts}/     # Aborted 归档目录（原子 rename，不参与执行语义）
    │   └── ...                      # 结构同上，仅供排障；由 Worker GC 清理
    │
    └── {task_id}__aborted_{ts}__gc_deleting/
                                     # GC 删除临时态（rename 后递归删除；失败下次重试）
```

### 3.1 目录分类与 Reset 边界

| 目录/文件 | 含义 | Reset memory | Reset workspace | Reset all |
|---|---|:---:|:---:|:---:|
| `memory/` | Agent 记忆 | 清除 | — | 清除 |
| `plans/` | Plan 级公共信息 | — | 清除 | 清除 |
| `tasks/` | Task 执行目录 | — | 清除 | 清除 |
| `session.instance` | 单实例租约 | — | — | 清除 |
| `agent.sock` | UDS 文件 | — | — | 清除 |

Task 执行期间如需临时文件，由 Agent 实现自行决定写入位置，框架层不预设 `workspace/` 目录。

Reset 前置条件：Agent 处于 `stopped` / `error` / `failed` 状态。

路径 containment guard：仅允许删除 Agent Home 下的子目录，拒绝路径逃逸。

### 3.2 tasks/ 扫描规则（Agent CLI Start 阶段）

Agent CLI 启动时扫描 `tasks/`，**只识别标准执行目录**：

- 识别：`tasks/{task_id}/`（纯 task_id 格式，无后缀）
- 忽略：`tasks/{task_id}__aborted_{ts}/`
- 忽略：`tasks/{task_id}__aborted_{ts}__gc_deleting/`

对每个标准执行目录：
- 读取 `tasks/{task_id}/task.json`
- 通过 Center API 对账确认 Task 仍分配给本 Agent
- 对账通过 → 恢复执行
- 对账不通过（Task 已重新分配 / 已终态）→ 执行 Abort 流程（§11）

---

## 4. 进程模型

### 4.1 进程架构

```
Worker Daemon（workerdaemon）
  └─ AgentController（per-Agent 控制器）
       └─ agent.sock（Unix Domain Socket）
            └─ Agent CLI（单一常驻进程）
                 ├─ Task Worker 线程池（并发处理多个 Task）
                 ├─ 活动事件投递线程（AgentActivityEvent → Center SQLite）
                 └─ Memory 同步线程（定期 git commit + push）
```

**关键架构约束**：

- AgentController 响应 ControlLog 中的 `agent.reconcile` / `agent.work` / `agent.wake` 命令
- Worker 通过 `agent.sock`（Unix Domain Socket）与 Agent CLI 通信
- `session.instance` 租约文件确保 Agent CLI 单实例运行（generation 递增防止脑裂）
- 每个 Task 独立 LLM context window（内存隔离）

### 4.2 通信方式

| 方向 | 机制 | 说明 |
|---|---|---|
| Center → Worker | **WorkerControlEvent**（ControlLog） | 控制命令流，Worker 累积 ack offset；SSE 实时下推 + 重连回放 |
| Worker → Agent CLI | `agent.sock`（Unix Domain Socket） | AgentController 通过 socket 向 Agent CLI 注入工作指令 |
| Agent CLI → Center | HTTP API | AgentActivityEvent 投递到 Center 的 SQLite 表 |
| Agent 内部 Task 间 | 内存共享 queue | 消除文件 I/O 瓶颈 |

---

## 5. Agent 生命周期

### 5.1 状态机

```go
const (
    LifecycleStopped   = "stopped"     // 初始 / 已停止
    LifecycleRunning   = "running"     // 运行中
    LifecycleStopping  = "stopping"    // 停止中（等待 graceful shutdown）
    LifecycleResetting = "resetting"   // 重置中
    LifecycleError     = "error"       // 瞬态异常（Worker 自动重试）
    LifecycleFailed    = "failed"      // 终态：crash-loop circuit breaker 触发
    LifecycleArchived  = "archived"    // 终态：软删除
)
```

### 5.2 状态转换

```
stopped ──Start──→ running
error   ──Start──→ running          （手动恢复）
failed  ──Start──→ running          （手动恢复，解除 circuit breaker）

running ──Stop───→ stopping ──MarkStopped──→ stopped
running ──Restart─→ running          （version bump，触发 reconcile）

any ─────Reset────→ resetting ──MarkStopped──→ stopped

running/error ──MarkFailed──→ failed （crash-loop 耗尽）
error ─────MarkRecovered───→ running （自动恢复成功）

stopped/error/failed ──Archive──→ archived（终态，不可逆）
```

### 5.3 核心操作语义

**Create**
- 绑定 Worker（immutable）、Profile（Name/CLI/Model/Reasoning/Mode/Provider）、Skills、CapabilityTags
- 初始态：`stopped`
- CLI 仅 `claude-code` 和 `codex` 受支持（`cli.go` 白名单）

**Start**
- AgentControlProjector 消费 `agent.lifecycle_changed` outbox 事件 → 写 `agent.reconcile` 到 ControlLog
- Worker Daemon ControlLoop 拉取 → AgentController 启动 Agent CLI
- Agent CLI 写 `session.instance`（session_id、generation、pid）并监听 `agent.sock`
- 扫描 `tasks/`，对账后恢复或执行 Abort 流程

**Stop**
- AgentController 通过 `agent.sock` 发送 stop 信号
- 各 Task Worker 线程 graceful shutdown（超时 30s）
- 被停下的 Task：LLM context 缓存保留，`task.json` status 改 `paused`；PM Task 状态不变（仍 `running`）
- Agent CLI 退出 → `MarkStopped` → `stopped`

**Restart**
- version bump 触发 ControlLog 新 reconcile 命令
- AgentController 先 Stop 再 Start
- `tasks/` 中 paused 的 Task 在 Start 阶段对账后恢复

**Reset**
- 需要 scope 参数：
  - `memory`：清除 `memory/` 目录
  - `workspace`：清除 `tasks/` 和 `plans/` 目录
  - `all`：清除 Agent Home 下所有运行时数据
- 完成后 → `MarkStopped` → `stopped`

### 5.4 Error / Failed / Recovered

| 方法 | 触发条件 | 目标状态 | 说明 |
|---|---|---|---|
| `MarkError(msg)` | Agent CLI 异常退出（单次） | `error` | 瞬态，Worker 自动重试 |
| `MarkFailed(msg)` | 重试耗尽 | `failed`（终态） | 需手动 Start/Reset |
| `MarkRecovered` | Session 自动恢复成功 | `running` | 仅从 `error` → `running` |

### 5.5 Archive

- 仅从 `stopped` / `error` / `failed` 可达
- 清除 workerID 绑定（Worker 释放）
- 保留 Agent 行（历史 Task 关联 / UI "(archived)" 标签）
- 终态，不可逆

---

## 6. Task 执行上下文

### 6.1 Task 生命周期（PM 域）

```
open → running → completed
open/running → discarded（终态）
completed → reopened → open | running
```

**执行扩展字段**（Task 聚合内）：

| 字段 | 类型 | 说明 |
|---|---|---|
| `blockedReasonType` | `input_required` / `obstacle` / "" | 运行中阻塞分类 |
| `blockedReason` | string | 阻塞原因描述 |
| `blockedComment` | string | Unblock 时用户回复（agent 恢复时读取） |
| `executionLeaseExpiresAt` | *time.Time | 心跳租约截止；RenewLease 延期，Block 清除 |
| `actionLogs` | []TaskActionLog | 生命周期历史（assigned / reassigned / agent_started / blocked / unblocked / lease_expired / lease_nudge / completed） |
| `planID` | PlanID | Task 所属 Plan（"" 表示纯 backlog，不在任何 Plan 中） |

### 6.2 Plan 编排与 Task 可运行性

Plan 是 Project 级的并行编排单元（Task DAG），由 ProjectManager BC 管理。Agent 不直接感知 Plan 的内部结构，但 Plan 通过**可运行性门禁**（TaskRunGate）控制 Agent 何时可以开始执行一个 Task。

#### 6.2.1 Plan 状态机

```
draft → running（Start：编排器激活，开始派发 ready node）
running → draft（Stop：暂停编排，允许编辑 DAG）
running → done（MarkDone：所有 node 到达终态）
draft/done → archived（终态，不可逆；cascade 归档所属 Task）
```

Plan 分两类：
- **Builtin（Assignment Pool）**：每个 Project 自动创建、始终 running 的扁平池，无依赖边，不可 stop / archive / delete
- **Structured（DAG Plan）**：用户创建的有向无环图，支持 seq / conditional / loopback 三种边类型

#### 6.2.2 DAG 边类型（EdgeKind）

| EdgeKind | 语义 | 说明 |
|---|---|---|
| `seq` | 硬 AND 依赖 | From 必须等 To 完成后才可开始（默认，零值） |
| `conditional` | 条件路由 | 仅当 To（决策节点）的 outcome == When 时边才激活；否则 From 被 skip |
| `loopback` | 有界回边 | 决策节点的 outcome == When 时回退重跑上游子图，受 MaxRounds 上限约束 |

#### 6.2.3 Node Status（派生，不持久化）

Node status 由 `DeriveNodeStatus(taskStatus, upstreamAllDone, dispatched, paused)` 纯函数计算，**不存储**：

| NodeStatus | 派生条件 |
|---|---|
| `done` | Task completed |
| `failed` | Task discarded |
| `running` | Task running 且未 paused |
| `paused` | Task running 但 agent 暂停了执行 |
| `blocked` | Task 非终态/非 running，且有上游未完成 |
| `ready` | 所有上游 done，尚未 dispatch |
| `dispatched` | 所有上游 done，dispatch @mention 已发出，agent 尚未开始 |
| `skipped` | 条件分支未被选中，或仅通过被剪枝节点可达 |

#### 6.2.4 TaskRunGate（可运行性门禁）

Agent 开始执行 Task（`open → running`）前必须通过 TaskRunGate 校验。Agent BC 不持有 Plan 知识，通过 `agentsvc.TaskRunGate` 端口委托给 PM Service：

**校验规则**（`EnsureTaskRunnable`）：

1. Task 已 `running` → 放行（resume，不是首次启动）
2. Task 无 `planID` → 拒绝（纯 backlog，未被纳入任何 Plan）
3. Builtin Plan → Task 必须是 `NodeDispatched`（已被选入 pool）才放行
4. Structured Plan → Plan 必须是 `running` 状态，且 Task 的 node status 为 `ready` 或 `dispatched`（所有 blockedBy 依赖已完成）

这个门禁防止 **抢跑**（run-ahead）：Agent 不能在上游 Task 完成前开始执行下游 Task。

#### 6.2.5 Plan Dispatch 流程

Plan `running` 状态下，编排器自动推进：

```
Task completed/discarded → 编排器 advance
  → 计算下游 node status（DeriveNodeStatus）
  → 发现 ready node → 创建 DispatchRecord + 在 Plan Conversation 中 @mention assignee
  → DispatchWakeProjector 消费 pm.task.assigned → 写 agent.work 到 ControlLog
  → Agent 收到工作指令 → TaskRunGate 校验通过 → 开始执行
```

**DispatchRecord** 是编排器唯一持久化的状态（`{plan_id, task_id, dispatched_at, dispatch_message_id}`），保证 @mention 不重复发送。

### 6.3 Agent 本地执行上下文

Agent CLI 为每个正在执行的 Task 维护本地执行目录（`tasks/{task_id}/`）：

```json
// tasks/{task_id}/task.json
{
  "task_id": "01JXYZ...",
  "status": "running",
  "plan_id": "01JABC...",
  "created_at": "2026-06-26T10:00:00Z",
  "updated_at": "2026-06-26T10:05:00Z"
}
```

**status 语义**（Agent 本地 TaskExecution 状态）：

| 状态 | 含义 |
|---|---|
| `pending` | 已创建执行目录，等待 Agent CLI fork 新上下文 |
| `running` | Agent CLI 内部执行中（LLM 对话运行中） |
| `paused` | Agent Stop 后暂停，LLM context 缓存保留 |
| `failed` | 异常重试超出上限，需人工干预或重新指派 |
| `done` | 正常完成 |

**执行上下文文件**（`tasks/{task_id}/execution.json`）：fork 参数、LLM 配置、重试计数等运行时信息。

**事件流文件**（`tasks/{task_id}/events.current.jsonl`）：状态恢复的唯一可信源（详见 §10）。

### 6.4 Task 分配与通知

1. PM Service `AssignTask` → 写 `pm.task.assigned` outbox 事件
2. Environment BC `DispatchWakeProjector` 消费事件 → 写 `agent.work` 到 ControlLog
3. Worker Daemon ControlLoop 拉取 → AgentController `Handle("agent.work")` → 通过 `agent.sock` 注入工作指令
4. Agent CLI 收到指令 → 创建 `tasks/{task_id}/` → 拉取 Task 详情 → TaskRunGate 校验 → 开始 LLM 执行

兜底：60s WakeReconcileLoop 定时扫描（避免信号丢失）。

**Plan 驱动的自动派发**：Structured Plan 中上游 Task 完成后，编排器自动 advance 下游 ready node → 触发 AssignTask → 进入上述通知流程。

### 6.5 Block / Unblock 流程

```
Agent 遇到阻塞 → 调 Block API（blocked_reason_type = input_required）
  → PM Task 标记 blocked_reason → executionLease 清除
  → UI 展示阻塞状态 → 用户在 Conversation 中回复
  → PM Service UnblockTask → 清除 blocked_reason，写 blockedComment
  → DispatchWakeProjector 发 agent.work → Agent 恢复，读 blockedComment 继续
```

---

## 7. 信号机制与对账

### 7.1 正常路径（WorkerControlEvent 控制流）

```
Center → ControlLog.AppendCommand → WorkerControlEvent(offset, commandType, payload)
  → SSE 实时下推 / Worker 轮询（重连按 lastAckedOffset 回放）
  → AgentController.Handle(command)
  → 执行后累积 ack offset
```

**命令类型**：

| commandType | 含义 | 触发源 |
|---|---|---|
| `agent.reconcile` | 生命周期对齐（start/stop/reset） | AgentControlProjector（消费 `agent.lifecycle_changed`） |
| `agent.work` | 注入工作指令（新 Task 分配） | DispatchWakeProjector（消费 `pm.task.assigned/reassigned`） |
| `agent.wake` | 注入 Conversation 消息（唤醒） | WakeProjector（消费 `conversation.message_added`） |

**幂等性**：
- ControlLog 按 `UNIQUE(worker_id, idempotency_key)` 去重
- `agent.reconcile` 键：`agent_id + version`
- `agent.work` 键：`agent_id + task_id + event_id`

### 7.2 兜底路径（WakeReconcileLoop）

Worker Daemon 60s 定时扫描：

- 查询 Agent 负责的 runnable Tasks
- 与当前 active `tasks/` 对比
- 发现遗漏 → 注入 wake 信号

### 7.3 两路径关系

| | 正常路径（ControlLog） | 兜底路径（WakeReconcileLoop） |
|---|---|---|
| 触发方式 | outbox 事件驱动 + SSE 推送 | 定时轮询（60s） |
| 实时性 | 秒级 | 分钟级 |
| 覆盖场景 | Agent 正常运行期间 | 信号丢失 / session 异常恢复 |

---

## 8. AgentActivityEvent 事件流

### 8.1 存储

**本地（Agent 端）**：per-Task 事件流文件

```
tasks/{task_id}/
  events.current.jsonl      # 当前写入段（状态恢复唯一可信源）
  events.000001.jsonl.gz    # 已压缩归档段
  events.offset             # 当前 Center 消费位置（segment, byte_offset, last_event_id）
```

**远程（Center 端）**：SQLite `agent_activity_events` 表（`ActivityEventRepository.Append`）

**投递流程**：Agent CLI 写入 `events.current.jsonl` → 异步投递到 Center SQLite → Center ack 后更新 `events.offset` → 整段 ack 后允许压缩归档

### 8.2 事件结构

```go
type AgentActivityEvent struct {
    id             string       // ULID
    agentID        AgentID      // 归属 Agent
    taskRef        string       // 可选，"pm://tasks/{id}" 关联 Task
    interactionRef string       // 可选，逻辑 turn 分组
    eventType      string       // 事件类型
    payload        string       // JSON
    occurredAt     time.Time
}
```

### 8.3 事件类型

| EventType | Payload 示例 | 说明 |
|---|---|---|
| `system_init` | `{model, session_id, mcp_servers}` | 会话初始化 |
| `assistant_text` | `{text}` | LLM 输出文本 |
| `thinking` | `{text}` | LLM 思考过程 |
| `tool_use` | `{tool_name, args, tool_use_id}` | 工具调用 |
| `tool_result` | `{tool_name, ok, tool_use_id, tool_result}` | 工具结果 |
| `result` | `{subtype, is_error, result, stop_reason, cost_usd, tokens_in, tokens_out}` | 回合结束 |
| `lifecycle` | `{event, ...}` | 生命周期事件 |
| `rate_limit` | `{...}` | 速率限制 |

### 8.4 查询接口

| 方法 | 说明 |
|---|---|
| `Append(event)` | 写入单条事件 |
| `ListByAgent(agentID, limit, before)` | Agent 维度分页（newest-first，cursor pagination） |
| `ListByTask(taskRef)` | Task 维度查询（oldest-first），用于上下文回放 |
| `LatestByAgents(agentIDs)` | 批量获取各 Agent 最新事件（window function，无 N+1） |

---

## 9. Memory 管理

### 9.1 定位

`memory/MEMORY.md` 是 Agent 的**持久记忆**——存放经验、教训、技能、原则。

它与 `tasks/`、`plans/` 的职责边界清晰：

| 目录 | 存放内容 | Ground Truth |
|---|---|---|
| `memory/` | 经验、教训、技能、原则 | Agent 本地（git 管理） |
| `tasks/` | Task 执行上下文（事件流、状态、日志） | Center（PM Task 聚合） |
| `plans/` | Plan 级公共信息（经验教训、失败记录等小抄） | Agent 本地；Plan 结构 ground truth 在 Center |

业务实体（Project / Task / Plan / Issue）的 ground truth 在 Center，Agent 不在本地镜像其目录结构。Memory 只记录 Agent 自身的认知产出。

### 9.2 存储形态

```
memory/
  .git/
  MEMORY.md              # 经验、教训、技能、原则
```

- 单一文件，git 管理版本历史，支持回溯与审计
- Reset memory 清除整个 `memory/` 目录（含 git 历史）

### 9.3 更新与同步

**写入方**：Agent CLI（在 Task 执行期间自主归纳写入）

**同步流程**：
1. Agent CLI 修改 `MEMORY.md`
2. GitOps 执行 `git add + git commit`
3. 后台线程异步 `git push` 到 Center Memory 服务

**一致性保证**：
- 未 commit 的修改是最新写入（进程内可见）
- 已 commit 的版本是持久化版本（进程崩溃可恢复）

### 9.4 路径安全

- 仅允许在 `memory/` 目录内操作
- 路径 containment guard：拒绝任何逃逸 `memory/` 目录的操作

---

## 10. 双轨上下文恢复

核心原理：Agent CLI 内存中的 LLM context window 是最完整的运行状态。为保证健壮性与成本平衡，采用快速 + 慢速双轨恢复。

### 10.1 快速路径（Fast Path）

- **适用**：Agent 本地 Stop/Start（`stopping → stopped → running`），LLM context 缓存完好
- **动作**：Agent CLI 拉起 Task 时检测到本地缓存仍有效，直接基于已有上下文继续，零 Token 消耗
- **优势**：毫秒级恢复

### 10.2 慢速路径（Slow Path）

- **适用**：缓存损坏 / 异常退出后
- **唯一可信源**：`tasks/{task_id}/events.current.jsonl`（本地事件流文件）
- **流程**：
  1. Agent CLI 清理损坏的本地缓存
  2. 自动裁剪（truncate）末尾未闭合的脏数据，保留至最后一个完整 Event
  3. 逐条回放 Event，重构 LLM 历史对话
  4. 被丢弃的损坏 Event 中的工具调用允许重试（要求工具幂等性）
  5. 重试上限 3 次，超限后 Task 本地 status 改 `failed`

---

## 11. 执行上下文清理与 GC

### 11.1 正常完成

Task 完成（`completed` / `discarded`）后，Agent CLI 将 `task.json` status 写为 `done`。执行目录保留用于排障与审计，默认 3 天后由 Worker GC 自动清理。

### 11.2 Abort 流程

Task 被 Center 取消或重新指派时，Agent CLI 执行 Abort：

1. 停止当前 Task 执行
2. 写 abort 终态事件到 `events.current.jsonl`
3. 上报 Center
4. 原子 rename：`tasks/{task_id}/` → `tasks/{task_id}__aborted_{ts}/`

rename 破坏标准目录名，防止后续扫描误读。重新指派时创建全新 `tasks/{task_id}/`，不复用 aborted 目录。

### 11.3 Aborted 归档 GC

**责任主体**：Worker Daemon（Agent CLI 不负责删除）

**触发时机**：
- Worker 启动时扫描老 aborted 目录
- 定时 GC（建议 1h 间隔）

**清理条件**：
- 目录名匹配 `tasks/{task_id}__aborted_{ts}/`
- 距当前时间超过保留期（默认 7d）

**删除步骤**：
1. Rename 为 `tasks/{task_id}__aborted_{ts}__gc_deleting/`
2. 递归删除该目录
3. 删除失败保留现场，下次重试

---

## 12. 验收标准

| 编号 | 验收项 | 关键点 |
|---|---|---|
| P1 | Agent Stop 后 Agent CLI 进程退出，PM Task 状态不变（仍 `running`），Agent → `stopped` | Task 状态与 Agent 生命周期解耦 |
| P2 | Agent Start 后 `tasks/` 中 paused Task 对账通过后恢复执行 | Center API 对账 + LLM context 恢复 |
| P3 | Agent Start 前对账：已重新分配 / 终态的 Task 执行 Abort 归档 | 遍历 tasks/，逐个查 Center |
| P4 | Restart = version bump → ControlLog reconcile → Stop + Start | suspended → active 转移 |
| P5 | ControlLog 命令（agent.work）到达后，AgentController 注入工作指令 | SSE 推送 + 幂等 ack |
| P6 | Agent 离线期间 ControlLog 命令积累；恢复后按 offset 回放补偿 | 重连回放机制 |
| P7 | 双轨恢复：优先本地 context 缓存（快速）；缓存失效回退到 events.current.jsonl 回放 | 缓存检测 + 自动降级 |
| P8 | Block（input_required）→ 用户回复 → Unblock → Agent 读 blockedComment 恢复 | PM Task block/unblock 流程 |
| P9 | TaskRunGate 拒绝依赖未满足的 Task 启动（抢跑防护） | Plan DAG 依赖校验 |
| P10 | Plan advance：上游 Task 完成后下游 ready node 自动派发 | DispatchRecord 幂等 + ControlLog 通知 |
| P11 | Reset 前验证 Agent 处于 stopped/error/failed | lifecycle 校验 |
| P12 | 非法 Reset scope 直接拒绝 | memory / workspace / all 枚举白名单；workspace 清 tasks/ + plans/ |
| P13 | Abort 后 `tasks/{task_id}/` 原子 rename 为 aborted 归档；Start 扫描不会误恢复 | 目录后缀隔离 |
| P14 | Worker GC 独立清理 aborted 归档（7d）和 done 执行目录（3d），Agent stopped 下仍可执行 | GC 不依赖 Agent 状态 |
| P15 | Memory git commit + push 正常同步到 Center | git 操作验证 |
| P16 | AgentActivityEvent 本地事件流投递到 Center SQLite，断线后恢复补投 | events.offset 消费位点 |
| P17 | `session.instance` 确保 Agent CLI 单实例运行，旧实例停止消费 | generation 校验 |
