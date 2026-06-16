# 规则：Backlog task 惰性（backlog-task-inert）

> T190。固化「未入 plan/pool 的 backlog task 不可动」为一条**显式不变量**，并统一所有 agent MCP 工具在该场景的错误。

## 不变量（Invariant）

**一个没有归属 plan 的 task（`planID == ""`）是 BACKLOG —— 已捕获、但尚未被安排执行，因而是 INERT（惰性的）：**

- **不可领取**（`claim_task`）
- **不可 start**（`start_work`）
- **不可改状态 / 推进**（`complete_task` / `block_task`）

要让它变得可动作，必须二选一：

1. `add_task_to_plan` —— 把它放进一个**真实 plan**（structured plan 节点），或
2. 把它**派发进内建 Assignment Pool**（builtin pool 的 dispatched 成员）。

**唯一例外：discard / 删除允许直接对 backlog task 执行**（清理误建任务）。这两条路径不需要 plan / pool / WorkItem —— 见下「例外」。

> 注：内建 pool 里**尚未 dispatched** 的 task 也是 backlog 语义，但那属于 `EnsureTaskRunnable`（runnability）的范畴；本不变量的判定式刻意只取最纯粹的 `planID==""`（见 `IsBacklogInert`）。

## 落点（Where it lives）

| 关注点 | 位置 |
| --- | --- |
| 命名判定式（pure predicate） | `pm.IsBacklogInert(planID) bool` — `internal/projectmanager/plan_view.go` |
| 统一 sentinel | `pm.ErrTaskBacklogNotActionable` — `internal/projectmanager/types.go` |
| 统一文案（单一来源） | `pm.BacklogNotActionableHint` — `internal/projectmanager/types.go` |
| 统一错误码 | `task_backlog_not_actionable`（HTTP 409） |
| agent 工具共享门 | `Server.rejectIfBacklog` / `writeBacklogNotActionable` — `internal/admin/api/agent_tools_write.go` |

## 统一错误（Unified error）

backlog 场景下，`claim_task` / `start_work` / `complete_task` / `block_task` **都**返回同一条 409 错误：

- **code**：`task_backlog_not_actionable`
- **message**：`task is in backlog — add it to a plan (add_task_to_plan) or dispatch it into the assignment pool before <action>`

这取代了此前分散的错误：`not_claimable`（claim）、`task_not_runnable`（start）、`not_agents_task`（complete/block —— backlog 无 WorkItem，旧路径会误报「不是你的 task」而非指引入池）。

`rejectIfBacklog` 是 handler 侧共享判定门：先 `GetTask` 拿到 `planID`，命中 `IsBacklogInert` 即写统一错误。**非 backlog**（structured-plan 节点、已 running、已被他人 claim 等）仍落到各自原有的门（`ClaimPoolTask` 的 `not_claimable` / `already_claimed`、`requireOwnTask` 的 `not_agents_task`），行为不回归。`start_work` 走 WorkItem，故由 `writeWorkStateError` 把 `ErrWorkItemTaskNotRunnable` 收敛到同一 envelope。

## 例外：discard / 删除（Exemption）

`discard_task` 走 `requireTaskAccess`（T183）：授权基于「**任务创建者 / 项目成员**」而非 WorkItem，因此**对 backlog task（无 WorkItem）也能直接 discard**，把它打到 `discarded` 终态。删除路径同理。这是有意为之 —— 让误建的 backlog task 能被清理，而不必先入 plan/pool 再废弃。

## 本期 Out-of-scope

- owner / Web-UI / admin **直接改 backlog task 状态**这条路径**暂不**绑定该不变量（owner 选轻量方案）。如需一并挡，后续另开任务。
