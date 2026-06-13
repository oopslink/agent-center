# 0047. claimable 派生 + 内置指派池 + 三段看板(v2.9.1）

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-06-13 |
| Delivered | v2.9.1（实现 task `task-5d0422cc`，收尾 plan `plan-f1f04088`） |
| Related | [ADR-0046 Task 状态机精简](0046-task-state-machine-simplify.md)（依赖：本 ADR 用其 status 集 + dispatch 记录）；Plan Orchestration / `node_status` 派生（`plan_view.go` §9.2，本 ADR 沿用其「derive-not-store」原则）；restart→死锁 P0（`task-76690aa1`） |

## Context

dispatch-driven 模型下，需要清晰区分**指派(assign)**与**领取(claim)**：把 task 指派给某 agent ≠ 该 agent 现在就能执行 —— 它可能还在 backlog、或所在 Plan 还没推进到该节点。当前缺一个明确的「这个 task 现在能不能被领取执行」的概念，导致：

- agent 误把"被指派"当"该开工"（self-drain backlog），与编排意图冲突。
- 没有统一的「可领」判定供 `get_my_work` / backlog 视图过滤。

同时需要一个**自由领取池**：不是所有活都该排进 DAG，有些是"随时可抓"的即时任务。

## Decision

### 1. `claimable` 是派生谓词，不存字段（守 §9.2 derive-not-store）

```
claimable(t) :=
      t.archivedAt == nil
    ∧ t.status == open          // 还没开始（claim = open→running）
    ∧ t.assignee != ""          // 已指派
    ∧ t.planID != ""            // 已进某 Plan（在 backlog = 不可领）
    ∧ nodeStatus(t) == dispatched   // plan 已 start + 上游全 done + 该节点已派发
```

- 存"原因"（Plan per-node **dispatch 记录**，已有），派生"结论"（claimable）。**不加 `claimable` 存储位** —— 存它会与上述条件 drift，正是 ADR-0046 力图消除的 T16 那类「存了的派生态对不上现实」。
- DTO 出**计算字段** `claimable: bool`；agent `get_my_work` / 可领队列只返 `claimable==true`。
- agent 领取开工 → `open→running`，`nodeStatus→running`，自然不再 claimable（=已领）。

### 2. assign ≠ claim

- **Assign**：设 `assignee`（归属 / lane），任何非终态可设，**不触发执行**。
- **Claim**：`open→running`，仅当 `claimable`。触发来源 = Plan 推进到该节点的 dispatch（结构化 Plan = push 唤醒；内置池 = pull，见下）。

### 3. 三段看板 + 两段 staging

| 桶 | planID | 可领? | 视图 | 派发语义 |
|---|---|---|---|---|
| **真 backlog** | `""` | 否（捕获、未就绪） | 扁平，**completed/discarded 隐藏** | —（**新建 task 默认落这**） |
| **内置 plan / 指派池** | `<builtin>` | 是（扁平→全 dispatched） | 扁平，**completed/discarded 隐藏**（像 backlog） | **pull**：变 claimable 但**不主动唤醒**，agent 空了自己拉 |
| **结构化 Plan** | `<plan>` | 推进到才可领 | **DAG，保留 done 历史** | **push**：推进到节点→编排器在会话 @ 唤醒 assignee |

- **两段 staging**：新 task 默认进**真 backlog（不可领）**；ready 了再拖进内置池或结构化 Plan（只改 `planID`）。
- completed/discarded 的**视图隐藏**只对 backlog + 内置池（待办性质）；**结构化 Plan 的 DAG 保留 done 节点**（历史）。

### 4. 内置 plan（指派池）形态

- **每项目一个**，建项目时自动创建（存量项目迁移 backfill）。
- Plan 上加 **`builtin`/`is_default` 标记 + 每项目唯一**；**常驻 started**；**禁依赖边**（是"池"不是 DAG，保证里面每个 task 都立即 dispatched→可领）；**随项目归档**（不可单独删/停）。
- 池里是**指派池**：task 仍要 `assignee`，被指派的 agent 随时可领（claimable 公式不变 `assignee!=""`）。自由/队列池（任一 agent self-assign 认领）留作后续可选。

## Consequences

- **+** assign/claim 清晰解耦；agent 不再误抢 backlog；`get_my_work` / backlog 视图过滤有统一判据。
- **+** claimable 派生、零 drift；与 ADR-0046 / `node_status` 同一「derive-not-store」原则。
- **+** 内置池给"随时可抓"的活一个家，又不破坏「claimable 只经 Plan」不变量。
- **−** 每项目多一个常驻 Plan（builtin）；需 backfill 存量项目。
- **触点：** projectmanager（Plan 加 builtin 标记 + 每项目唯一 + 禁边 + 自动建/backfill 迁移；claimable 派生 + DTO 计算字段）、内置 plan 的 pull 派发（不唤醒）vs 结构化 push、mcphost（`get_my_work` 按 claimable 过滤）、SPA（看板三段:backlog | 内置池 | 结构化 Plan;完成隐藏只对前两者）。依赖 ADR-0046 的 status 集（open/running/completed/discarded/reopened）。

## Acceptance

- Tester：claimable 派生在各组合下正确（backlog 不可领 / 内置池可领 / 结构化未推进不可领、推进后可领 / 终态/归档不可领）；新建默认落 backlog；内置池禁边 + 每项目唯一 + backfill;org-isolation。
- Tester2 run-real：看板三段呈现 + 完成隐藏（backlog/池）vs DAG 保留 done;内置池 pull(不唤醒)、结构化 push(唤醒)行为区分;both-mode AA。
