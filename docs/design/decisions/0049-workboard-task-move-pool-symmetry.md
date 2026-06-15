# 0049. Work Board 拖拽改所属 plan + 内置池 task-set 对称可编辑 + running/终态 plan 锁定 (T121)

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-06-15 |
| Delivered | v2.10.2（T121：Work Board 卡片跨列拖拽改所属 plan） |
| Related | ADR-0047 三段式 Work Board（Backlog / Assignment Pool / structured Plans，见 `docs/design/v2.10.1/`） / [v2.9 plan-orchestration §9.4 draft-only editing](../v2.9-plan-orchestration.md) |

## Context

owner 2026-06-15 需求：task 放入 plan 后无法改所属 plan，不符合预期。Work Board 上应支持**拖拽 task 卡片改所属 plan**，跨列移动：Backlog / Assignment Pool / 各 plan 之间互拖。

实现现状盘点（拖拽前）：

- **已可用**：Backlog → draft plan（SELECT）、Backlog → 内置池（SELECT）、draft plan → Backlog（REMOVE）、draft plan ↔ draft plan（MOVE = remove+add，A7）。
- **缺口**：内置 **Assignment Pool** 的卡片**不可拖出**（`PoolTaskCard` 无 `draggable`），且后端 `RemoveTaskFromPlan` 对内置池**不豁免** draft-only 闸门 —— 池子永远是 `running`，故"把 pool 任务拖去别处"在服务层报 `plan_conflict`（owner/PD 实测）。这是"无法改所属 plan"的池子分支根因。
- **缺口**：running / 终态（done/archived）plan **无显式锁定视觉**（仅靠"没有 remove 按钮 + 不可拖"隐式表达），不满足"列禁用 + 原因 tooltip / 卡片锁定态"的诉求。

关键约束（owner）：可拖范围 = **两端都非 running 的 structured plan**：Backlog / Assignment Pool / draft plan 之间自由互拖；**running plan 的 task 不可拖出、也不可拖入**（与既有"plan 仅 draft 可编辑 task-set/DAG"一致）；completed/archived plan 的 task 同样锁定（终态不可改）。**校验必须在服务层（非仅 UI 禁拖）**，越权 / 对 running plan 的直接调用 fail-closed 拒绝。

## Decision

### 1. 内置池 task-set 对称可编辑（后端，根因修复）

`SelectTaskIntoPlan` 早已对 `IsBuiltin()` 池子**豁免** draft-only 闸门（池子是 always-running 的扁平 claimable 桶，不是执行中的 DAG）。本次让 `RemoveTaskFromPlan` **镜像同一豁免**：

```go
// plan_flow.go RemoveTaskFromPlan
if !p.IsBuiltin() && p.Status() != pm.PlanDraft {
    return pm.ErrPlanNotDraft
}
```

即：**池子的 add / remove 对称**——两侧都豁免 draft-only。structured plan 的 running/终态仍然锁定（add+remove 都拒）。池子无 DAG 边，故 remove 内的 depends_on 清理对池子是 no-op。

> 语义边界：池子成员关系（plan_id）独立于 task 的 assignee/claim/status；把 task 移出池子只是改其归属，不影响其已分派/认领进度。终态 task（completed/discarded）本就被池子/Backlog 默认隐藏，不会被拖。

### 2. 拖拽 = 既有原子操作的组合（不新增端点）

跨列移动复用既有的 `add_task_to_plan` / `remove_task_from_plan`（webconsole + MCP）：

- SELECT = Backlog → plan/pool（POST `/plans/{id}/tasks`）
- REMOVE = plan/pool → Backlog（DELETE `/plans/{id}/tasks/{taskId}`）
- MOVE = plan/pool → 另一 plan/pool = **remove(源) 然后 add(目标)**

不引入原子 `move_task_plan` 端点：MOVE 复用两个已对称、各自服务层 fail-closed 校验的原语，与 v2.10.1 既有的 draft↔draft MOVE 实现保持一致，API 面最小。**fail-closed 由原语保证**：拖入 running structured plan → add 被 `ErrPlanNotDraft` 拒（→409 `plan_conflict`）；拖出 running structured plan → remove 被拒；非项目成员 → `requireProjectMember` 拒。

> 取舍记录（备选）：原子 `MoveTaskToPlan`（单事务校验两端）可消除"remove 成功 / add 失败 → task 落回 Backlog"的并发窄窗 stranding。当前与既有 MOVE 实现一致地接受该窄窗（仅在目标 plan 拖拽中途状态翻转时发生）；若后续 stranding 成为实际问题，再升级为原子端点。

### 3. running / 终态 plan 锁定视觉（前端）

- **列**：running/done 列 `data-locked="true"` + `title` 给原因 tooltip；header 加常驻锁形 glyph（`plan-locked-{id}`）；拖拽进行中对锁定列显示 no-drop 原因横幅（`plan-drop-blocked-{id}`）+ `cursor-no-drop`。archived plan 整列不在 Work Board 渲染（既有过滤）。
- **卡片**：锁定列的卡片不可拖（既有 `canRemove==isDraft` 闸门），并以锁形 glyph（`plan-task-locked-{id}`）+ `title` 原因取代 remove 按钮。
- **池子**：`PoolTaskCard` 现为 `draggable`（drag 即"移出"的唯一交互，池子扁平无 remove 按钮）；池子列接受任何非自身的在飞拖拽（Backlog SELECT 或他 plan MOVE-in）。

文案语言：Work Board 既有 UI 全英文，故锁定 tooltip / 横幅用英文（如 "This plan is running — its tasks can't be moved to or from another plan."），与周边一致。

## Consequences

- 池子任务可自由在 Backlog / Pool / draft plan 间拖拽；"无法改所属 plan"消除。
- MCP `remove_task_from_plan` 现也允许从内置池移除（与 add 对称）；工具描述已更新注明池子豁免。
- running/终态 plan 在服务层 + UI 双重 fail-closed 锁定，越权 / 对 running 的直接 API 调用被拒。
- `decideDrop` 纯逻辑天然泛化（select/remove/move/noop），池子作为 source/target 无需特判；触摸长按拖拽路径同源复用，自动覆盖池子。
