# 0011. Dispatch 可靠性协议：ACK + execution_id 幂等 + Reconcile

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-16 |

## Context

Center 派单（dispatch）TaskExecution 给 worker 涉及跨进程 / 跨网络的协作，必须明确"派单不丢、不重复执行"的保证机制。初稿设计未明确以下场景：

1. **Envelope 投递可靠性**：Center 发 DispatchEnvelope，worker 没收到 / 没 ACK，状态怎么走？
2. **Worker 崩溃恢复**：Worker daemon 进程崩溃重启后，之前正在跑 / 正要起的 execution 怎么处理？
3. **网络分区下的双跑**：Worker A 跟 Center 失联但 agent 仍在跑；Center 标 `worker_lost` 后派给 Worker B；A 重连后怎么办？
4. **Center 重启**：派出 envelope 期间崩溃，重启后是否丢失 / 是否重复派？

分布式系统不存在通用的 exactly-once；务实方案是**at-least-once 投递 + 幂等接收 + 双向对账**。

## Decision

### 1. Execution_id 即幂等 key + fencing key

- Center 创建 TaskExecution 时分配 uuid（`execution_id`）
- Worker 接收 envelope 用 `execution_id` 做幂等：重复收到同 id → 检查本地 ledger 决定 ACK / 忽略
- Worker 上报任何事件（trace / status / artifact）必须带 `execution_id`
- Center 端校验：worker_id 不匹配 / execution 已终态 → 拒收并通知 worker reconcile

**不引入独立的 `dispatch_token`**：execution_id 自带唯一性、不可变性、跟 worker 一一绑定（dispatch 时定死 `worker_id`），足够承担 fencing 职责。Retry 是创建新 execution_id（[ADR-0010](0010-task-execution-two-layer-model.md)），旧 id 永远停在终态，天然防双跑。

### 2. ACK 协议

```
Center → Worker: DispatchEnvelope { execution_id, ... }
Worker → Center: DispatchAck { execution_id, accepted=true, message?, acked_at }
              或 DispatchNack { execution_id, accepted=false, reason, message, acked_at }
```

- Center 发 envelope 后：`task_executions.dispatch_state = 'pending_ack'`
- 30s 没收到 ACK → execution 直接进 `failed(reason='dispatch_no_ack', message=...)`
- 收到 NACK → execution 直接进 `failed(reason='dispatch_nack:<sub_reason>', message=...)`
- 收到 ACK → `dispatch_state = 'acked'`；监听后续 worker emit 的状态事件

**Center 不周期性重发 envelope**：失败即 fail，supervisor 唤醒后决定重派（新 execution_id）。简单、避免双投递。

**NACK reason 标准枚举**（不穷举，可扩展）：

| reason | 含义 |
|---|---|
| `worker_at_capacity` | 并发上限（`per_agent_type` 满）|
| `agent_cli_unsupported` | Worker 不支持该 agent_cli adapter |
| `mapping_missing` | Worker 上无该 project 的 WorkerProjectMapping |
| `worktree_path_busy` | worktree 模式 + 目标路径已被占用 |
| `base_branch_missing` | worktree 模式 + base_branch 不在本地 repo |
| `envelope_version_unsupported` | 防未来升级；本版本不识别 |

所有 NACK 必须同时填 `reason` + `message` 双字段（见 [conventions § 16](../../rules/conventions.md)）。

### 3. Worker 本地 ledger（持久化）

Worker daemon 在本地维护一个轻量 sqlite ledger：

- 收到 DispatchEnvelope **先写 ledger** 再 ACK（写入失败不 ACK）
- ledger 字段：`execution_id` / `received_at` / `phase` ∈ {preparing / running / done}
- 重启后从 ledger 取回未完成派单（preparing / running）
- ledger 只是本地崩溃恢复用，**不是状态权威**（状态权威在 Center）

幂等查询：

| 本地 ledger | Worker 行为 |
|---|---|
| 未收过 | 落 ledger → ACK → 准备 workspace |
| 已收过 + phase=running | 重发 ACK；不重起 agent（防双跑） |
| 已收过 + phase=done | 重发 ACK；不做任何事 |

### 4. Reconcile 协议（Worker 上线时强制对账）

Worker enroll / 重连后**第一件事**：

```
Worker → Center: Reconcile { worker_id, local_active_executions: [E-7, E-9, ...] }
Center → Worker: ReconcileResponse {
  active:   [E-7, ...],          # center 也认为还 active, 继续跑
  stale:    [E-9, ...],          # center 已标 failed/killed/done, worker 杀本地进程
  unknown:  [E-99, ...]          # 不存在或归属其他 worker
}
```

Worker 后续动作：

- `active` → 继续上报事件；不做任何重置
- `stale` + `unknown` → SIGTERM 本地仍 alive 的 agent；emit `task_execution.killed { reason='reconcile_stale' / 'reconcile_unknown' }` 仅作审计（Center 状态不改）

Worker reconcile 完成前**不接收新 dispatch**。

### 5. Center 端持久化要求

- Dispatch envelope 发出前必须 DB 持久化 `task_executions.dispatch_state`
- Center 启动时扫描 `dispatch_state='pending_ack'` 的 execution：
  - `dispatched_at` 距今 > 30s → 标 `failed(reason='dispatch_no_ack')` → emit 事件 → supervisor wake
  - 否则等 ACK / 超时
- 任何状态变迁必须在 DB transaction 内完成；不依赖内存状态

### 6. Worker_lost 触发的双跑兜底

场景：Worker A 心跳超 60s（[Q4 timeout](../architecture/02-task-model.md) `worker_heartbeat_timeout`）→ Center 标 W-A.status=offline + 上面所有 active execution → `failed(reason='worker_lost')` → supervisor 派给 W-B（新 execution_id）。

A 重连后：

1. Reconcile 协议：Center 回 `stale: [E-7, ...]`（已 fail 的旧 execution）
2. A 杀本地 agent，emit `task_execution.killed { reason='reconcile_stale' }`
3. 旧 execution_id 在 events 表保留审计；新 execution_id 在 W-B 上正常跑

不引入复杂 fencing token：execution_id + worker_id 绑定 + reconcile 已足够。

## Consequences

正面：

- **At-least-once + 幂等 = 不丢且不重复执行**（同 execution_id 不会被两个 worker 跑）
- **崩溃恢复明确**：worker ledger + center DB transaction 让两侧都能从异常恢复
- **网络分区下 graceful**：A 与 B 各自 execution_id 独立；A 重连 reconcile 自动清理；不破坏 artifacts
- **复杂度可控**：单 token (execution_id) + 单协议（ACK + Reconcile），不引入分布式锁 / 复杂 fencing 序列号
- **可观测性**：每次 fail / reconcile 都有 reason + message 落 events，inspect 看得清

负面 / 待跟进：

- **Worker 引入本地 sqlite ledger**：增加 worker daemon 复杂度（一个本地 db 文件）；接受 —— 避免"daemon 崩溃丢派单"风险
- **网络分区下短窗口双 agent 进程**：A、B 两个 worker 各跑自己 execution_id 的 agent，A 重连前都浪费算力。v1 接受 —— 各自 worktree 互不干扰，无数据冲突
- **不追求 exactly-once 投递**：业务无强约束（agent 工作幂等性由 prompt + workspace 隔离保证）；接受 at-least-once

## Alternatives Considered

### A. Center 周期性重发 envelope（不超时即 fail）

- Pro: 网络抖动不损失派单
- Con: 重发频率难选；worker 端要更严格幂等（场景增加）；不知道"已发了几次"
- 不选

### B. 完整 fencing token 协议（单调递增 sequence number）

- Pro: 多 supervisor / 多 center 并发场景安全
- Con: v1 仅单 center / 单 supervisor，过度设计；token 管理代码量大
- 推迟到 [roadmap](../roadmap.md)（多 supervisor 之前）

### C. Center 端调度 worker fail-over（A 失联自动迁移 execution 到 B）

- Pro: 用户视角更顺滑
- Con: "迁移"语义复杂（worktree 状态怎么搬？events 怎么续？）；v1 让 supervisor 决定重派更简单
- 推迟到 [roadmap](../roadmap.md)

### D. 当前方案（本 ADR）

- Pro: 协议简单、单 token、双向对账、有明确兜底
- Con: 网络分区下双 agent 短窗浪费（可接受）

## 影响范围

- 新增 [architecture/02-task-model.md](../architecture/02-task-model.md)：dispatch ACK + Reconcile + ledger 章节
- 更新 [architecture/07-worker-model.md](../architecture/07-worker-model.md)：增 dispatch 时序图（含 ACK / 本地 ledger / reconcile）；env 注入清单
- 更新 [architecture/05-observability.md](../architecture/05-observability.md)：events `task_execution.dispatch_*` / `reconcile.*`
- 更新 [rules/conventions.md](../../rules/conventions.md)：§ 16 `reason + message` 双字段约定
- 实现层 [02-persistence-schema.md](../implementation/) (TBD)：`task_executions.dispatch_state` 字段
- 实现层 worker daemon：本地 sqlite ledger schema
