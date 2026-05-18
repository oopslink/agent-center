# 0010. Task / TaskExecution 两层模型；AgentSession 合并

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-16 |

## Context

[01-bounded-contexts.md § 1.1](../architecture/strategic/03-bounded-contexts.md) 初稿同时存在两个执行相关的概念：

- **AgentSession**：一次 agent 子进程 spawn → 退出的运行周期
- **TaskExecution**：worker 侧任务运行时聚合，关联一个 Task + 一个 Worktree + 一组 AgentSession

定义里"TaskExecution 关联**一组** AgentSession"暗示 1:N，但 v1 实际场景下"一次执行"和"一个 agent 进程"是 1:1（无重启同 session、`input_required` 通过 unix socket 同进程阻塞 / 解阻塞，见 [04-input-required.md](../architecture/tactical/scheduling/02-input-required.md)）。两个概念硬拆两张表，inspect / events / persistence 都翻倍复杂。

更关键的问题：初稿把 A2A 6 状态（`submitted` / `working` / `input_required` / `completed` / `failed` / `canceled`）当作 **Task 自己的状态**。讨论中发现这跟语义不符：

- **Task 是工作单元身份**（不变 id；"做这个 PR" / "总结这个" 是单元定义）
- **失败是某次执行的状态，不是 Task 的状态**：execution 跑挂了不代表 task 整体死，task 仍可被重新派
- **重派不应创建新 task id**：保留血缘 + 不破坏"任务即工作单元"语义

因此把 A2A 状态机挂 Task 上是建模错位。

## Decision

### 1. 两层模型

| 层 | 实体 | 状态机 |
|---|---|---|
| 工作单元 | **Task**（entity，身份不变） | `open` / `suspended` / `done` / `abandoned`（4 态） |
| 一次执行 | **TaskExecution** | `submitted` / `working` / `input_required` / `completed` / `failed` / `killed`（A2A 借鉴的 6 态） |

- Task ↔ TaskExecution 是 **1:N**（一个 task 可被多次 dispatch）
- **单活约束**：任意时刻一个 task 最多一条 active execution（应用层约束；non-terminal status: `submitted` / `working` / `input_required`）

具体每态语义 / 迁移条件见 [02-task-model.md](../architecture/tactical/scheduling/01-task-model.md)。

### 2. 失败属于 execution，不属于 task

- Task 没有 `failed` 状态
- 一次 execution 失败 → execution 终态 `failed` / `killed`；task 仍在 `open`
- 想重新做 → 对同一 task 再 dispatch 创建新 execution；旧 execution 永远停留在自己的终态

### 3. Retry 不创建新 task

- 同一 task 多次 dispatch ↔ 多条 TaskExecution（独立 `execution_id`）
- 不存在 `failed → submitted` 的状态迁移（task 没 failed；execution 一旦失败永久停在那）
- 上限：`max_executions_per_task=3`（v1 全局默认）兜底，防 supervisor 死循环

### 4. AgentSession 概念合并到 TaskExecution

- **AgentSession 在术语 / events / 表名 / event_type 字符串里全部下线**
- "一次执行"的承载唯一名词 = **TaskExecution**
- worker 侧"agent 子进程在跑"的运行时投影属性（pid / started_at / 等）作为 TaskExecution 的字段
- 表名 `task_executions`（v1 不再有 `agent_sessions` 表）
- event_type 前缀 `task_execution.*`（不再有 `agent_session.*`）

### 5. Task 状态行为约束

| Task 状态 | 能 dispatch 新 execution? | 终态? |
|---|---|---|
| `open` | ✅ | 否 |
| `suspended` | ❌（要先 resume 回 open） | 否 |
| `done` | ❌ | **是**（由 `task_execution.completed` 自动联动） |
| `abandoned` | ❌ | **是**（由 `abandon-task` 动作触发） |

- `kill-execution` ≠ `abandon-task`：前者只杀当前 execution，task 仍 `open`；后者把 task 整体废弃（前置条件：先 kill 当前 active execution，两条独立事件链）
- `suspend-task` / `abandon-task` 前若有活动 execution：CLI / Bridge 自动**先发 kill 命令**走完 kill 流程，等 `killed` 确认后再发对应 task 动作

## Consequences

正面：

- **建模清晰**：身份层（Task）与运行层（TaskExecution）各管各的；命名上不再有"task failed"的语义错位
- **血缘自然**：retry 历史保留为同 task 的多条 execution，inspect 一键看完整重派链
- **Observability 简洁**：events / inspect 不需要在 TaskExecution + AgentSession 两套概念间切换
- **跟"center 是状态权威"对齐**：失败 / 重派全在 center 侧 task / execution 表里推进，无 worker 侧的"我的 session 还活着"心智
- **跟 retry 策略统一**：retry = 创建新 execution，跟首次 dispatch 走同一代码路径

负面 / 待跟进：

- **AgentSession 名词遗留**：[01-bounded-contexts.md § 1.1](../architecture/strategic/03-bounded-contexts.md) / [04-input-required.md](../architecture/tactical/scheduling/02-input-required.md) / [05-observability.md](../architecture/tactical/observability/01-observability.md) / [07-worker-model.md](../architecture/tactical/workforce/01-worker-model.md) 都有引用，要批量同步换名
- **Task 表加 `current_execution_id` 字段**：nullable，指向当前 active execution（或最近一次）；查询时不需要 join 全 executions 表
- **"单活约束"是应用层约束**（不是 partial unique index，因 [conventions § 9](../../rules/conventions.md) dialect-agnostic）；要在 dispatch 路径上显式校验

## Alternatives Considered

### A. TaskExecution = 一次执行；AgentSession 保留为子细节

- Pro: 兼容现有 BC 模型（execution / session 各管各）
- Con: v1 一次执行 ↔ 一个 agent 进程是 1:1，硬拆两张表过度建模；inspect / events 复杂

### B. AgentSession = 一次执行；废弃 TaskExecution

- Pro: 已有概念可复用
- Con: "AgentSession"名字偏 worker 进程视角；spawn 失败时 agent 还没起来 "session" 概念不成立，但"执行"成立 —— 语义不准

### C. 当前方案：TaskExecution 唯一承载，AgentSession 合并下线

- Pro: 概念最少、命名最准、retry 路径自然
- Con: 现有文档批量迁移；名称变动需要 sync 8 处

## 影响范围

- 重写 [architecture/02-task-model.md](../architecture/tactical/scheduling/01-task-model.md)（TBD → Draft）
- 更新 [architecture/01-bounded-contexts.md](../architecture/strategic/03-bounded-contexts.md)：UL § 1.1 / BC4 Execution / § 1.3 状态机 / § 5 命名对照表
- 更新 [architecture/04-input-required.md](../architecture/tactical/scheduling/02-input-required.md)：`agent_session_id` → `execution_id`；状态机映射
- 更新 [architecture/05-observability.md](../architecture/tactical/observability/01-observability.md)：event_type `agent_session.*` → `task_execution.*`
- 更新 [architecture/07-worker-model.md](../architecture/tactical/workforce/01-worker-model.md)：worker 侧"agent 子进程"作为 execution 字段；时序图 rename
- 更新 [architecture/08-prompt-assembly.md](../architecture/tactical/_cross-cutting/01-prompt-assembly.md)：dispatch envelope 字段名同步
- 实现层 [02-persistence-schema.md](../implementation/) (TBD)：表名 `task_executions`，无 `agent_sessions`
