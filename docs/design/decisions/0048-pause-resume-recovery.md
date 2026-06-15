# 0048. Work-item pause vs plan-node resume — one recovery entry (T101)

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-06-15 |
| Delivered | v2.10.1（fix T101：UI「Resume」generic 报错） |
| Related | [ADR-0019 BC scheduling/execution merged to task-runtime](0019-bc-scheduling-execution-merged-to-task-runtime.md) / T53（paused-node 显示 + operator resume）/ [v2.9 plan-orchestration §9](../v2.9-plan-orchestration.md) |

## Context

There are **two** "pause" notions and they were being conflated by operators (owner 实测 T101: 点 Plan「Resume」报 generic `Couldn't resume this node`)。

1. **Work-item pause（agent 侧，scheduling autonomy — v2.8.1 #278 D）**
   - `pause_work(work_item_id)` 把 agent 当前 active work item 置 `active→paused`，**释放唯一 active 槽**，好让 agent `start_work` / `resume_paused_work` 另一个任务。
   - 恢复入口：agent 侧 `resume_paused_work`（`ResumeWork`）—— 受 ownership 守卫 + **单 active 槽守卫**（`HasActiveWorkItem` → `ErrAgentHasActiveWork`）。agent 必须先 pause/finish 当前 active 项才能 resume。

2. **Plan-node `paused` 显示（T53，派生态）**
   - 不是独立状态：`node_status` 由 `DeriveNodeStatus(task.status, …, work-item-paused?)` 派生。一个 `running` task 的 live work item 处于 `paused` 时，节点显示 `paused`（plan_view.go）。即 **node `paused` ⟺ task running 且其 work item 被 agent pause**。
   - operator 恢复入口：Plan UI「Resume」按钮 → `POST …/plans/{id}/nodes/{task}/resume` → `PMService.ResumePausedNode`（authz: 项目成员 + plan running + task∈plan）→ `NodeResumer` 端口 → `ResumeWorkByOperator`。

**关键：两者是同一个 work item 的两个恢复入口，不是两套独立机制。** `ResumeWorkByOperator` 跳过 ownership 守卫（caller 授权改由 pm 项目成员校验），但**保留**单 active 槽不变量。

### 根因（T101）

node 显示 `paused` 通常是因为 **agent `pause_work` 了它去切换到另一个任务**——切换后 agent 在**另一个** work item 上 active。此时 operator 点「Resume」：`ResumePausedNode → ResumeWorkByOperator → HasActiveWorkItem=true → ErrAgentHasActiveWork`（后端正确返回 409 `agent_busy`，单 active 不变量不允许双激活）。但前端把**所有**错误压成一句 generic「Please try again」，operator 看不到真因 → 误以为坏了。

经排除：`paused` node ⟹ task running ⟹ plan running，故 `ErrPlanNotRunning`/`ErrNodeNotPaused` 对一个真正 paused 的 node 不可达；NodeResumer 已 wire。**唯一现实失败 = `agent_busy`。** 后端行为正确，bug 在前端 error masking。

## Decision

1. **单一恢复语义，两个入口**：work item 的 paused→active 恢复只有一条真实路径（`Resume`/`ResumeWorkByOperator`），都受单 active 槽不变量约束。Plan UI 的「Resume」= operator 入口；agent 的 `resume_paused_work` = agent 入口。**不放宽单 active 不变量**（强行 resume 会静默 pause agent 另一个 active 项，语义错误）。

2. **准确提示而非 generic 报错（T101 fix）**：
   - **前端**（`PlanDetail.tsx`）：「Resume」失败按后端 `ApiError.code` 渲染准确文案：
     - `agent_busy` → “Its agent is busy on another work item — it'll resume this one when that finishes, or the agent can resume it from its side.”
     - `node_not_paused` → “Nothing to resume (it may have already resumed).”
     - `plan_not_running` → “The plan isn't running.”
     - 其它 → 原 generic 兜底。
   - **后端**（webconsole `pmResumePausedNodeHandler`）：补 `ErrPlanNotRunning → 409 plan_not_running`，与 agent-tools（MCP）`writeResumePausedNodeError` 对齐（原来落到 generic `plan_conflict`）。

3. **运维指引**：node 卡在 `paused` 且 UI 提示 `agent_busy` —— 表示其 agent 正忙于另一项，会在那项结束后（或 agent 侧 `resume_paused_work`）自然恢复；operator 无需也不应强行双激活。

## Consequences

- operator 能看到 paused node 无法立即恢复的**真实原因**，不再误报为故障。
- 两路 resume 错误码一致（webconsole == MCP）。
- 单 active 槽不变量保持不变；恢复语义单一、可预期。
