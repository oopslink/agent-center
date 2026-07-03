# 变更记录 / 审计日志（Change-Log / Audit-Trail）设计

> **状态**：设计草案，待 oopslink 拍板（2026-07-03）。
> **来源**：issue-74df441a——"Issue/Task/Plan 的变更需要有 log 记录，可以考虑增加通用升级模块，并在 issue/plan/task 详情页侧边栏展示变更记录"。
> **前置**：无硬前置（纯加法特性）。排在 v2.32.0（删旧引擎）ship 之后实现。

## 1. 动机

issue / task / plan 这些**领域对象的状态迁移目前没有可追溯的账本**：一个 task 从 open→running→completed、被 reassign、加了依赖边、gate 判了 reject——发生了、但事后**查不到「谁、什么时候、把什么从 X 改成了 Y」**。运维/复盘/追责都缺一条**对象级语义变更时间线**。

本特性做一个**通用的、append-only 的变更记录模块**（一张表 + 一个统一写入点 + 一套读 API），并在三个详情页侧边栏用**人话时间线**展示。

## 2. 决策（待拍板）

1. **对象级语义账本，不是字段全 diff**——只记**高价值语义变更**（状态迁移、归属、依赖、决策/gate 结果、close/reopen、block/unblock），不追每个字段的 diff（title/desc 这类文本编辑记一条粗粒度「edited metadata」即可，不存全文 diff）。
2. **通用一张表 `pm_audit_log`**——`object_type ∈ {issue,task,plan}` 复用同一张表 + 同一套读写，不是三张分表。
3. **同事务同步写入（in-tx `RecordChange`），不是异步投影**——审计行**和它记录的那次变更在同一个 `runInTx` 里一起提交**，账本无最终一致性延迟、无「事件没 emit 就漏记」的洞。沿用已有 `flushActionLogs` 的成例。
4. **与已有 `agent_activity_events` 正交、不复用**——那是 agent 运行时轨迹（thinking/tool_use/executor lifecycle，高频、按 agent/task、会 GC）；本账本是对象级语义账本（低频、按 issue/task/plan、**永久保留**）。两者互补，不合并。
5. **纯加法、零回归**——新表空 = 现状零影响；不动现有 `pm_task_action_logs`（task-only、喂 overdue reminder 的那条路）。

## 3. 与现有三条流的边界（关键，避免重复造）

代码里已有三条"历史/事件"流，本账本是**第四条、正交的**：

| 流 | 表 | 粒度 / 用途 | 保留 |
|---|---|---|---|
| 领域事件总线 | `outbox_events` (0040) | 事务性 outbox，at-least-once 中继给 projector（喂会话/参与者）。**基础设施、非用户可见** | 中继后清 |
| agent 活动流 | `agent_activity_events` (0043) | **agent 运行时轨迹**：thinking / tool_use / tool_result / executor lifecycle（v2.31.0 per-executor start/stop/progress）。按 `agent_id`/`task_ref`，高频 | GC |
| task 动作日志 | `pm_task_action_logs` (0070) | append-only per-task 动作日志，**但仅 task、无 object_type、无读 API/前端**，只内部喂 overdue-block reminder | 永久 |
| **本特性·变更账本** | **`pm_audit_log` (新)** | **对象级语义变更**：状态迁移/归属/依赖/gate/close-reopen，按 `object_type+object_id`，低频、人话 | **永久** |

- **为什么不复用 `agent_activity_events`**：它是"agent 干了啥"（执行轨迹），本账本是"对象被改成了啥"（业务状态迁移）——键、语义、保留策略都不同。
- **为什么不直接推广 `pm_task_action_logs`**：它 task-only、无 `object_type`、无读端、且正被 reminder 消费——推广它会动到工作中的 reminder 路。v1 新建 `pm_audit_log`、不碰它；**未来是否把 action_logs 并进 audit_log 列为 out-of-scope**。
- **为什么不用纯 outbox projector（异步）**：省服务层改动，但——① 账本是权威记录，异步最终一致对"刚改完就看变更记录"体验差；② 依赖边增删（`addPlanEdge`）、纯 metadata 编辑（`UpdateIssue`）**现在根本不 emit 事件**，projector 会漏记这些。故选同步 in-tx 写入。

## 4. 领域模型

### 4.1 `pm_audit_log` 表（迁移 `0097_vXXX_pm_audit_log`）

形状对齐 append-only 成例 `pm_task_action_logs`（ULID PK、`occurred_at TEXT`、**无 version、无 updated_at、无 FK**，FK-free/app-managed，见 0070 注释），泛化字段：

```
pm_audit_log {
  id            TEXT PRIMARY KEY      -- ULID（repo 层 idgen.NewULID 铸）
  project_id    TEXT NOT NULL         -- 便于按项目查/GC 边界
  object_type   TEXT NOT NULL         -- 'issue' | 'task' | 'plan'
  object_id     TEXT NOT NULL         -- 逻辑引用，无 FK
  change_type   TEXT NOT NULL         -- 枚举，见 §4.2
  field         TEXT                  -- 可空，变更的字段名（status/assignee/...）
  from_value    TEXT                  -- 可空，旧值（人话或原值）
  to_value      TEXT                  -- 可空，新值
  actor_ref     TEXT NOT NULL         -- 谁改的：agent_ref / user / 'system:<reconciler>'
  detail        TEXT NOT NULL DEFAULT '{}'  -- JSON，结构化补充（依赖边的 kind/when、gate round、note 等）
  occurred_at   TEXT NOT NULL
}
CREATE INDEX idx_pm_audit_log_object ON pm_audit_log(object_type, object_id, occurred_at);
CREATE INDEX idx_pm_audit_log_project ON pm_audit_log(project_id, occurred_at);
```

### 4.2 `change_type` 枚举（v1 覆盖面）

只覆盖高价值语义变更，逐条对应 §5 的写入点：

- **task**：`created` / `status_changed`（含 open→running→completed/discarded/blocked/unblocked/reopened，`from/to` 记状态）/ `assigned` / `reassigned` / `unassigned` / `claimed`（从 pool）/ `auto_assigned` / `review_verdict`（gate，`detail` 记 verdict）。
- **plan**：`created` / `started` / `dependency_added` / `dependency_removed`（`detail` 记 kind/when/max_rounds）/ `decision_outcome`（`detail` 记 node/outcome/round）/ `loopback`（`detail` 记 reopen 的节点集）。
- **issue**：`created` / `status_changed`（含 close/reopen，`from/to` 记状态）/ `metadata_edited`（title/desc 粗粒度一条，不存全文 diff）/ `auto_closed`（resolved 自动关）。

### 4.3 Go 侧

- 领域聚合 `pm.AuditEntry` + 端口 `AuditLogRepository`（`Append(ctx, entry)` / `ListByObject(ctx, objType, objID, cursor)`），落 `internal/projectmanager/repository.go`（对齐 `TaskActionLogRepository`）。
- Repo `internal/projectmanager/sqlite/audit_log_repo.go`，**tx-aware**：`exec, _ := persistence.ExecutorFromCtx(ctx, r.db)` 拿事务上下文，`Append` 在同 tx 里 insert（对齐 `task_action_log_repo.go`）；ID 在 repo 层 `idgen.NewULID()` 铸；时间 `ts(t)` TEXT。

## 5. 写入点（in-tx `RecordChange` 统一收口）

服务层加一个 helper `s.recordChange(ctx, objType, objID, changeType, field, from, to, actor, detail)`——追加到 per-tx 缓冲、在 `runInTx` 末尾随 `flushActionLogs` 一起 flush（同一事务、原子）。在下列**语义写入点**各插一条调用（紧挨已有的 `s.emit`）：

| change_type | 写入点函数 | file:line |
|---|---|---|
| task `status_changed` | `taskStateOp`（SetStatus/Discard/Block/Complete 共享闸）| `assign_flow.go:728` |
| task `status_changed`（批量）| `BatchUpdateTask`（内联 tx、不走 taskStateOp，需单独插）| `assign_flow.go:806` |
| task `status_changed`（override）| `SetTaskStatus` | `assign_flow.go:776` |
| task `assigned/reassigned/unassigned` | `AssignTask` / `UnassignTask` | `assign_flow.go:22` / `:657` |
| task `claimed` / `auto_assigned` | `ClaimPoolTask` / `autoAssignOne` | `claim_flow.go:41` / `auto_assign_reconciler.go:170` |
| task `review_verdict` | `RecordReviewVerdict` | `decision_auto.go:148` |
| plan `dependency_added/removed` | `addPlanEdge` / `RemovePlanDependency` | `plan_flow.go:308` / `:~348` |
| plan `decision_outcome` / `loopback` | `SetDecisionOutcome` / `applyLoopbacks` | `plan_controlflow.go:31` / `:~55` |
| plan `started` | `StartPlan` | `plan_lifecycle.go:48` |
| issue `status_changed` | `TransitionIssue` / `SetIssueStatus` | `issue_flow.go:15` / `:48` |
| issue `metadata_edited` | `UpdateIssue` | `metadata_flow.go:95` |
| issue `auto_closed` | `CloseResolvedIssues` | `resolved_issue_closer.go:24` |

- **actor 归属**：从 ctx 取操作者（agent_ref / user）；系统驱动的变更（auto-assign / auto-close / loopback / lease-reclaim）→ `actor_ref = 'system:<reconciler名>'`，别记成空或误记成对象 owner。
- **收口原则**：优先在**共享闸**（`taskStateOp`）记一条覆盖大多数 task 状态变更；只有绕开闸的（`BatchUpdateTask`、`SetTaskStatus`）才单独补，避免散记漏记。

## 6. 读 API

- `GET /api/orgs/{slug}/{issues|tasks|plans}/{id}/audit`（游标分页），镜像已有 activity 路由 `internal/webconsole/api/server.go:345` 的注册与 handler 形态。
- DTO 把 `change_type`/`field`/`from`/`to`/`actor`/`detail`/`occurred_at` 渲染成前端好消费的结构（人话文案在前端拼，后端给结构化字段）。

## 7. 前端展示（三详情页侧边栏「变更记录」时间线）

复用 `web/src/components/AgentActivityRow.tsx` 的时间线结构（左侧 rail + category dot + 彩色 label + 可折叠 detail）+ `agentActivityGrouping.ts` 的按时间分组，新建 `useObjectAudit(objType,id)` query 打 §6 端点。人话渲染示例：「**@alice** 把状态 `running → completed`」「**@bob** 认领了任务」「加了依赖 `Dev → Review`（loopback, max_rounds=2）」「gate 判定 `reject`，重开第 2 轮」。

- **Task 详情**（`TaskDetail.tsx` → `TaskDetailSidebar.tsx`）：在 read-only 段（`:279`）后追加一个 `<section>`「变更记录」。
- **Issue 详情**（`IssueDetail.tsx` → `IssueDetailSidebar.tsx`）：在 `IssueRelatedPlansBlock`（`:245`）下方追加一个 stacked block。
- **Plan 详情**（`PlanDetail.tsx`，tab 式、无持久右侧栏）：在 `plan-tabs`（`:184`）加一个「变更记录」tab，与 chat/DAG 并列。

## 8. 向后兼容

纯加法：`pm_audit_log` 空 + 无任何写入点被调用 = 现状零影响。现有对象/流不受影响；`pm_task_action_logs` 与 reminder 路不动。

## 9. v1 范围 & Out of scope

- **v1**：§4–§7 全部（表 + repo + in-tx RecordChange 全写入点 + 读 API + 三详情页时间线）+ 部署级真跑验收（真触发各类变更→查账本→侧边栏渲染）。
- **Out of scope（v1 不做）**：字段级全文 diff（title/desc 只记粗粒度一条）；把 `pm_task_action_logs` 并进 audit_log；审计的导出/检索/按 actor 过滤（v1 只按对象查）；变更的撤销/回放。

## 10. 开发 plan 骨架（v2.32.0 ship 后建）

1. `pm_audit_log` 迁移（0097）+ `pm.AuditEntry` 聚合 + tx-aware repo（对齐 `task_action_log_repo.go`）+ 单测。
2. `s.recordChange` helper + per-tx 缓冲/flush（随 `flushActionLogs`）+ §5 全写入点接入 + actor 归属；行为单测覆盖各 change_type。
3. 读 API（镜像 `server.go:345`）+ DTO + handler 测试。
4. 前端三详情页「变更记录」时间线（复用 AgentActivityRow 结构）+ 人话渲染 + query。
5. 部署级真跑验收：真起服务→真触发状态迁移/reassign/加依赖/gate reject/close-reopen→查读 API 有对应条目、actor 正确→三详情页侧边栏真渲染出时间线；无变更时零影响回归。

## 11. 参考

- 现有时间线渲染成例：`web/src/components/AgentActivityRow.tsx` + `agentActivityGrouping.ts`。
- append-only 表成例（形状/无 FK/tx-aware repo）：`pm_task_action_logs`（迁移 0070）+ `internal/projectmanager/sqlite/task_action_log_repo.go`。
- 事件/活动流边界：`outbox_events`(0040) / `agent_activity_events`(0043，v2.31.0 executor lifecycle)。
- 迁移约定：`internal/persistence/migrations/`，最新 `0096`，新表 `0097_vXXX_pm_audit_log`。
