# T83 · Claimability 规则收窄 + Assignment Pool 开放认领

- **任务**：task-2c899f57（T83，v2.10.1 plan-9674fa13 节点）
- **owner 拍板**：2026-06-15（含「限制每 agent 同时最多持有 N 个已领池任务」，见 §3.6）
- **实现**：dev1（agent-6cdfa49f）
- **验收**：ACC 节点 + Tester1 authz 硬门

---

## 1. 现状（代码事实，ADR-0047 「derive-not-store」）

`claimable` 是**派生谓词**、从不落库。`internal/projectmanager/plan_view.go:97`：

```go
Claimable = !archived && status==open && assignee!="" && planID!="" && nodeStatus==dispatched
```

- **backlog（planID==""）已经永不 claimable** ✅ —— 要求「backlog 不可领」现状即满足，无需新增。
- **Assignment Pool** = 内建 pool plan；任务选入池 = 记一次 dispatch（无 wake）→ nodeStatus=dispatched → 任务可领。
- `ListClaimableTasks` 当前**按 assignee** 列（`internal/projectmanager/service/reads.go:88`）。
- 派生点：`get_my_work` 的 `claimable_tasks`（agent_tools.go）、`get_task` 的 `claimable`（TaskClaimableByID, reads.go:141）、plan 节点 DTO 的 `claimable`（agent_tools_plans.go:512）。

## 2. 差距

owner 要「**Assignment Pool 内 task 开放认领**」。当前谓词要求 `assignee!=""` —— 即必须**先指派**给某 agent 才可领。开放认领 = 池任务**无需预指派**，项目内任何合格 agent 都能领。

## 3. 规格（最小改动）

1. **backlog 不可领**：保持原样（planID=="" → false）。
2. **池开放认领**：去掉 **pool 任务**的 `assignee!=""` 要求：
   - `pool 任务 claimable = !archived && status==open && planID==<built-in POOL> && nodeStatus==dispatched`
   - **结构化 plan 节点保持原样**（含 `assignee!=""`：指派给特定 agent、仅该 agent 领）。
   - 按 planID 区分两类：built-in pool vs 结构化 plan。
3. **认领流（claim = open→running + 落 assignee，原子）**：
   - 合格 agent 在 `get_my_work` 看到开放池任务 → `start_work`（=认领）。
   - 认领**原子地**：`assignee = 认领者` + `status open→running`。
   - 并发双领用 **CAS / 乐观锁**（version 校验）；第二个返回 `already_claimed`（不报错刷屏，幂等可读）。
4. **类守护（authz 硬门，fail-closed）**：
   - 只有任务所属 **project 的成员 agent** 能领该池任务；非成员**不可见**且直接 `start_work` 被拒（403/404 opaque，不泄露存在性）。
   - 守护在服务层而非仅 UI；越权红线。
5. **可见性**：
   - `get_my_work.claimable_tasks` = (自己被指派的 dispatched 任务) ∪ (自己所在 project 内的开放池任务)。
   - backlog 永不出现在任何人的 claimable。
   - `get_task.claimable` 派生同步上述规则。
6. **持有上限（owner 2026-06-15 拍：要限）**：每 agent 同时最多持有 **N** 个**已领池任务**（= 状态 running 且 assignee 为该 agent、来自 built-in pool 的任务）。
   - 达上限时，该 agent 对**新池任务**的 `start_work`/认领被拒：`pool_claim_limit_reached`（不影响领取结构化 plan 指派给它的节点）。
   - 计数口径：仅统计 **pool 来源**、状态 **running** 的任务；任务 complete/fail/release 后腾出名额。
   - **N 可配置**（org/系统级配置项，便于调参），**默认 N=3**（owner 2026-06-15 拍定）。
   - 仅约束**开放池认领**；不约束结构化 plan 节点的指派执行。

## 4. 验收（Tester1 authz 硬门，逐条带证据）

| # | 用例 | 期望 |
|---|------|------|
| 4.1 | backlog 任务 | 不在任何人 claimable；直接 start_work 被拒 |
| 4.2 | 池内未指派任务 + project 成员 agent | 可见、可领；领后 assignee=该 agent、status=running |
| 4.3 | 池内未指派任务 + 非成员 agent | 不可见；start_work 403/404 opaque（不泄露存在性） |
| 4.4 | 两 agent 并发领同一池任务 | 仅一个成功，另一个 already_claimed；无双 assignee |
| 4.5 | 结构化 plan 节点 | 仍只被其指派 agent 领；他人不可领 |
| 4.6 | agent 已持有 N 个已领池任务，再领第 N+1 个 | 被拒 `pool_claim_limit_reached`；完成在手一个后可再领；结构化 plan 节点不受此限 |

授权用例（4.1/4.3/4.4）不过 **不 promote**。

## 5. 注

- 与 task-f628b23d（Plan P&lt;number&gt;/org_ref 显示）、task-60bc3a1a（#id-tail bug）同属 dev1 的 org_ref/claimability 域，串着做。
- 实现走 PD §-1 → IntegrationDev 合 → Tester1 authz 硬门 + run-real（截图证据）。
