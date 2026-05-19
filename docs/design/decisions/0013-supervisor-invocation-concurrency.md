# 0013. Supervisor Invocation 并发模型：per-scope 串行 + 跨 scope 并行

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-17 |

## Context

Supervisor 是事件驱动的：每个唤醒事件（task / execution / issue / conversation / worker 事件 + 周期 review）触发一次 `SupervisorInvocation` —— spawn 一个短命 `claude` 子进程做一次决策周期。

[task-runtime/00-overview § 7.1 Supervisor 唤醒事件白名单](../architecture/tactical/task-runtime/00-overview.md) 和 [cognition/01-supervisor-model.md](../architecture/tactical/cognition/01-supervisor-model.md) 都提到了"每个聚合根维持 30 秒 coalescing window，同源事件合并成一个 batch 喂给 supervisor"，但没明确：**当两个不同聚合根的窗口同时就绪时，supervisor 进程是串行跑还是并行 spawn？**

这个看似细节的开关决定了：

- 短任务被长任务阻塞的延迟敏感度
- center VPS 上的并发资源占用（claude 进程 / API rate limit / token cost）
- 跨 invocation 决策的冲突可能性
- 节流策略、Memory 写并发、崩溃恢复、retry 等下游一整套设计的形状

不先拍这个，下游各项设计无锚。

## Decision

### 1. 并发模型：per-scope 串行 + 跨 scope 并行

| | |
|---|---|
| 同一 `scope_key`（如都 `task:T-42`）| **至多 1 个** running invocation；后到的事件累入"下一批 coalescing window" |
| 不同 `scope_key`（如 `task:T-42` vs `issue:I-7`）| **可并行** spawn |

全局总并发上限：`max_concurrent_invocations=5`（v1 默认，可配）。超过则进 in-memory FIFO 队列，任一 invocation 结束 → 出队一个 spawn。

### 2. 5 种 scope_kind

事件按 `scope_kind` + `scope_key` 二元路由到串行桶：

| scope_kind | scope_key | 收哪些 wake 事件 |
|---|---|---|
| `task` | `<task_id>` | `task.*` / `task_execution.*` / `input_request.*` |
| `issue` | `<issue_id>` | `issue.*` / `issue.comment.*` |
| `conversation` | `<conversation_id>` | `conversation.message_added`（飞书 DM / group thread / 等） |
| `worker` | `<worker_id>` | `worker.online` / `worker.offline` / `worker_project_proposal.*` |
| `global` | `_global_`（单例）| 周期 review、未来的全局级合成事件 |

路由函数 `route(event_type, refs) → (scope_kind, scope_key)` 在 center 端硬编码（不暴露给 supervisor / worker / user）。这是个确定性、无副作用的纯函数。

### 3. Coalescing window 关窗规则

per scope_key，in-memory：

- **滚动 30s since last event** —— 同 scope 30s 内连续来事件，merge 进同一 batch
- **硬上限 5 min since first event** —— burst 场景下避免无限拖延，强制关窗 spawn

### 4. 失败兜底依赖底层单活 / ACK 协议

跨 scope 并行带来"两个 invocation 互不知情下都决定派任务给 W-2"等冲突场景。**不在 Cognition BC 内做协调** —— 由底层 BC 的现有约束兜底：

- [ADR-0010 单活约束](0010-task-execution-two-layer-model.md)：同一 task 同时刻最多 1 条 active execution，第二派被拒
- [ADR-0011 ACK 协议](0011-dispatch-reliability-protocol.md)：worker NACK reason 包含 `worker_at_capacity` 等冲突情形
- 失败的 dispatch 通过 events 表 + correlation_id 回流到对应 scope 唤醒下次 invocation 重决策

### 5. State machine

`SupervisorInvocation` 4 态：

```
running → succeeded            (claude exit 0)
running → failed(reason)       (claude exit ≠ 0；含 OOM / cli_command_error / center_restart_orphan / 等)
running → timed_out            (超出 per-scope_kind hard timeout)
```

终态。无自动重试 —— 失败 / 超时 emit `supervisor.invocation_failed_alert` 推飞书 + Web Console；人工 `agent-center supervisor retrigger <id>` 才重发。

### 6. Hard timeout per scope_kind

| scope_kind | timeout |
|---|---|
| task / issue / conversation / worker | 180s |
| global | 600s（周期 review 类长任务） |

超时：center SIGTERM → 5s grace → SIGKILL → status='timed_out'。

### 7. Center crash 后的窗口 / 队列恢复

Window state 和 FIFO 队列都是 in-memory，center crash 即丢。重启时：

1. 扫 `supervisor_invocations.status='running'` → 改 `failed(reason='center_restart_orphan')`、emit alert
2. 按 scope_kind/key 扫 events 表：
   - `WHERE occurred_at > last_succeeded_invocation.started_at AND event_id NOT IN any trigger_event_ids`
   - 这些是"未被任何成功 invocation 覆盖"的事件
3. 重建 coalescing window，按正常流程继续

这是 **at-least-once 的事件处理**：center crash 不丢工作。语义上 `center_restart_orphan` 跟 `claude_nonzero` / `timed_out` 不同 —— 前者是"未尝试"，故自动 replay；后者是"试了不行"，故无自动重试。

### 8. Trigger 事件用 JSON array 记录

`supervisor_invocations.trigger_event_ids` 是 JSON 数组（≥1 个事件 id），统一表达单事件和 coalesced batch。不引入 join 表（[conventions § 9.x](../../rules/conventions.md) "简单 list-of-id 用 JSON 不做 join 表"）。

### 9. 反循环

`supervisor.*` 事件**不**在 wake 白名单。`supervisor.invocation_failed_alert` 走 Bridge / Web Console，不会自唤醒 supervisor。

## Consequences

正面：

- **延迟独立**：长 invocation 不阻塞别 scope 的紧急事件（如长周期 review 跑着不影响新 feishu 消息处理）
- **冲突可控**：跨 scope 冲突场景由底层单活 / ACK 协议兜底，不在 Cognition BC 内引入分布式锁
- **崩溃恢复明确**：center restart 自动 replay 未覆盖事件，user 无需介入；常规失败 emit alert 等人工
- **节流可调**：单一参数 `max_concurrent_invocations=5` 调全局压力；coalescing 30s/5min 可配
- **审计完备**：每个 invocation 独立 status + trigger_event_ids + token_usage，inspect 一目了然
- **跟 ADR-0010 / 0011 单活 / ACK 协议自然嵌合**：cross-scope 冲突场景"用现有兜底"，零新机制

负面 / 待跟进：

- **跨 scope 决策冲突**：两个并行 invocation 互不知情下做出竞争决策（如都判 W-2 空闲都派任务），靠底层约束驳回，会产生一次 NACK + 一次 supervisor wake；用 token 多绕一圈。v1 单用户低频，可接受
- **In-memory window/queue 复杂度**：center 进程需要维护 per-scope coalescing state 和全局 FIFO 队列 + 重启 replay 逻辑；实现层有一定复杂度
- **token cost 上限"靠运维"**：5 并发 + 各自 600s = 短时高峰可能跑出小账单；监控告警留在 [roadmap](../roadmap.md)（Token cost 折算成钱）
- **`center_restart_orphan` 自动 replay 跟"无自动重试"语义边界**：两条原则一致（前者是"未尝试"、后者是"试了"），实现要区分 reason 字段，否则容易混

## Alternatives Considered

### A. 全局串行（max 1 invocation 全场）

- Pro: 决策上下文最完整；零冲突可能；实现最简
- Con: 长任务（周期 review、慢决策）阻塞所有紧急事件 30-60s；用户视角"机器卡住"

### B. 全并发（每事件一个 invocation，不串行不防抖）

- Pro: 延迟最低；无防抖逻辑
- Con: 同 scope 内多次 invocation 互相竞争决策（两次 task scope invocation 在毫秒级先后都决定派 T-42）；token 浪费、决策不一致
- 不选

### C. 当前方案：per-scope 串行 + 跨 scope 并行 + 全局上限 5

- Pro: 延迟好（紧急事件不被无关任务阻塞）；同 scope 决策一致；并发可控
- Con: 跨 scope 冲突靠底层兜底；in-memory 状态需 crash recovery 逻辑；都可接受

### D. Per-scope_kind 串行（同 kind 串行，跨 kind 并行）

- Pro: 比 A 好；比 C 简单（不需要按 scope_key 细分）
- Con: 同 task 50 个事件来跟跨 50 个 task 来同等对待 —— 实际上后者更需要并行；模型粗
- 不选

## 影响范围

- 重写 [architecture/06-supervisor-model.md](../architecture/tactical/cognition/01-supervisor-model.md) § 2 Invocation 生命周期 + § 3 调度 / 节流（TBD-partial → Draft）
- 更新 [03-bounded-contexts § Cognition BC](../architecture/strategic/03-bounded-contexts.md)：核心事件加 `supervisor.invocation_failed_alert`（注：[ADR-0019](0019-bc-scheduling-execution-merged-to-task-runtime.md) 后 Cognition 编号 BC5 → BC4）
- 更新 [task-runtime/00-overview § 7.1 Supervisor 唤醒事件白名单](../architecture/tactical/task-runtime/00-overview.md)：跟 cognition/01-supervisor-model § 3 引用对齐
- 更新 [architecture/05-observability.md § O3](../architecture/tactical/observability/01-observability.md)：Supervisor 调用全留档章节补充并发 / 重试 / crash 字段说明
- 更新 [requirements/02-non-functional.md](../requirements/02-non-functional.md)：明确 supervisor 并发 SLA / 资源上限
- 更新 [roadmap.md](../roadmap.md)：列"Cross-invocation 协调机制 / 多 supervisor 调度"等推迟项
- 实现层 schema：新增 `supervisor_invocations` 表（13 字段）+ events 表加 `decision_id` 列
- 实现层：center wake scheduler 模块（路由 + coalescing + 并发上限 + 队列 + 重启 replay）
