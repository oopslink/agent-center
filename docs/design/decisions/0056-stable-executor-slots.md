# 0056. Stable Executor Slots Separate from Execution Run Identity

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-08-08 |

## Context

一个 Agent 可并行运行至多 `max_concurrent_tasks` 个 executor。当前运行时用
`exec-{ULID}` 同时承担 Run identity、目录/分支 key 与操作界面标签。它适合唯一审计，
但不适合回答“这个 Agent 的第几个并发单元正在做什么”，导致 Agent Detail 与 Activity
只能展示随机 hash，无法形成稳定的并发槽位心智。

直接把 Run 改名为 `exec-0` 会复用 identity，破坏目录隔离、历史事件分组、日志、恢复、
worktree 与 delivery audit。只在 Web 按数组顺序临时编号则会在 heartbeat、restart 和 map
遍历后跳号，把展示序号伪装成运行时事实。

## Decision

引入 **Executor Slot**：Agent 内的有限运行时资源，identity 为
`(agent_id, slot_index)`，编号从 `0` 到 `effective_concurrency_cap-1`。一次
**Execution Run** 仍由全局唯一 `execution_id=exec-{ULID}` 标识，并在其活跃生命周期内
独占一个 Slot。

- Slot 由 agent-runtime 的 `executor.Pool` 在 mutex 内原子分配；采用最小空闲编号。
- starting、running、finishing、adopted orphan 都占槽，只有最终 `Release` 释放。
- `slot_index` 在 spawn 前写入 `input.json`，并写入 `orchestrator.json`；restart/adopt 恢复
  原 Slot。旧记录缺字段时按稳定顺序补号并回写。
- `execution_id` 继续作为目录、worktree、branch、interaction ref、peek/log、恢复与审计主键。
- Worker heartbeat 的 live snapshot 是实时槽位状态唯一来源；Center 只保存 latest projection，
  不创建第二套 Slot 聚合或长期 Slot 历史表。
- Agent Activity 事件携带发生时的 `slot_index`，但历史分组继续按 `execution_id`。
- 运行中缩容采用 draining：不迁移、不终止活跃 Run；高位 Slot 释放后才完成收敛。

## Consequences

正面：

- UI 可稳定展示 `Executor #0` 与 `2/4 active`，restart 后仍不跳号。
- 唯一 Run 审计链完全兼容；Slot 复用不会串联不同历史 Run。
- 复用现有 concurrency heartbeat/API/Activity 链路，无第二权威来源。

代价：

- Pool 从计数 map 升级为双索引 Slot allocator，并需要处理 reservation、adopt 冲突和动态 cap。
- file protocol、heartbeat、API 与 Activity payload 增加兼容字段。
- mixed-version 环境无稳定 Slot 的旧 snapshot 只能明确标记 unavailable，不能伪造编号。

## Alternatives Considered

1. **把 execution id 改为 `exec-0`**：拒绝。编号复用破坏唯一身份与历史审计。
2. **仅 Web 按返回顺序编号**：拒绝。返回顺序不稳定，restart 后会错误换号。
3. **Center 持久化 Slot 聚合**：拒绝。Slot 是 worker/agent-runtime 本地运行资源，Center
   持久化会形成双重真相，并增加 heartbeat 与数据库状态协调。
4. **round-robin 分配**：拒绝。最小空闲编号更符合固定槽位心智，行为和测试更可预测。
