# 0046. Task 状态机精简：blocked 降级为注解 + 删除 verified（v2.9.1）

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-06-13 |
| Delivered | v2.9.1（实现 task `task-46138d5f`） |
| Supersedes | 现行 7-状态 Task 状态机（`internal/projectmanager/task.go` 的 `taskTransitions`）中 `blocked` / `verified` 两态及其转移 |
| Related | restart→死锁-blocked 事故（T16）+ P0 恢复口 `unblock_task`（task-76690aa1）+ 调度语义 dispatch-driven（task-5d0422cc）；Plan `node_status` 派生（`plan_view.go`，本 ADR 不改它） |

## Context

现行 Task 状态机（`internal/projectmanager/task.go`）有 **7 个状态**与转移邻接表：

```
open      → running, discarded
running   → blocked, completed, discarded
blocked   → running, discarded
completed → verified, reopened
verified  → reopened
discarded → ()                 // terminal
reopened  → open
```

v2.9.1 周期暴露出三个问题：

1. **`blocked` 作为「状态」+ 非法转移，制造了一类死锁。** restart / 30min stale `WorkItemReconciler` 释放 active WorkItem 后，projector 把 `running → blocked("agent execution failed")`；而 `blocked → completed` 是非法转移、又没有暴露的恢复入口，任务**死锁**（实例 T16、Tester2 的 P1/P2/P3 验收 task）。P0（task-76690aa1）补了 `unblock_task` 兜底，但根因是「把一个会自动进入、却没有自然出口的瞬态做成了一等状态」。
2. **`task_status: blocked` 与 Plan 派生的 `node_status: blocked` 重名、语义不同。** 后者表示「DAG 上游未完成」（派生、不存储，见 `plan_view.go`），前者表示「任务被 Block」。同名不同义，导致排障时「假 block（node=dispatched 但 task=blocked）vs 真 block（node=blocked）」的反复确认成本。
3. **`verified` 在实践中没被使用。** task 一律停在 `completed`；「独立验收」是靠 PD §-1 + Tester/Tester2 run-real 的证据与 IntegrationDev 合并体现的，没有走 `Verify` 转移。`verified` 成了不用的仪式。

## Decision

### 1. 删除 `blocked` 状态 → 降级为 `running` 上的 `blocked_reason` 注解

- 「卡住 + 原因」不再是状态，而是 `running` 任务上的 `blocked_reason` 注解（该列已存在，`Task.BlockedReason()`）。
- `block_task`：在 `running` 任务上写 `blocked_reason`（**状态不变，仍 running**）。
- `unblock_task`：清 `blocked_reason`（**状态不变**）。
- 效果：**不再有「进得去、出不来」的非法转移陷阱**（任务永远停在可推进的 running）；**消除与 Plan `node_status: blocked` 的重名**（后者保持派生不变）。
- "stuck 可见性"（v2.8.1 circuit-break/stale-release 的初衷）由注解 + UI 徽章保留，不丢信号。

### 2. 删除 `verified` 状态（连同 `Verify` 动作与 `ErrSelfVerify`）

- `completed` 即终态「已完成」（仍可 `Reopen`）。
- 多面验收纪律（「谁都不自验收」）**继续靠流程**：PD §-1（verify-not-trust）+ Tester（data/API）+ Tester2（run-real）+ 证据；不再做成 task 状态。

### 3. 目标状态机（7 → 5 个状态）

```
open      → running, discarded
running   → completed, discarded      // + 可选 blocked_reason 注解（不改状态、永远可恢复）
completed → reopened                  // 终态（可 reopen）
reopened  → open
discarded → ()                        // terminal
```

## Migration（既有行）

- `status = blocked` → `running`（**保留** `blocked_reason`）。
- `status = verified` → `completed`。
- 迁移时机：**Thread superset + P3 合并、板面干净后**执行 cutover（避免在大量 task in-flight 时迁移状态）；代码与测试可先行。

## Consequences

- **+** 死锁那一类从设计上消失（不存在「无出口的状态」）。
- **+** 状态机更小；`blocked` 重名歧义消除。
- **+** 「卡住 + 原因」信号保留（注解），且任务始终可推进。
- **−** 失去「独立验收通过」这个可查询状态 → 验收落在流程与证据上（这已是现状，可接受）。
- **触点：** `task.go`（状态枚举 / `taskTransitions` / `Block`-`Unblock`-`Verify` 方法）、迁移脚本、`sqlite` repo、projectors（`task_status_sync` + Plan `node_status` 派生不变）、MCP/admin（`block_task`/`unblock_task` 改注解语义、移除 `verify_task`）、SPA（状态 chip：running + "stuck" 徽章；移除 verified）、`state_machine_test` + class-guard（验证不存在能死锁的非法转移）、`docs/rules` 同步。

## Acceptance

- Tester：新转移表全覆盖；`blocked`/`verified` 既有行迁移 round-trip 正确；class-guard 证明无「可死锁的非法转移」。
- Tester2：UI 上 `running` 任务的 stuck 徽章、无 verified 状态、run-real。
