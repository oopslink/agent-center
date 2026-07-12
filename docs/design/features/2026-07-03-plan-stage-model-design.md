# Plan Stage 模型设计（Spark 式 stage + task）

> **状态**：设计已确认（oopslink，2026-07-03），待实现。
> **硬前置**：阶段二（issue-6a253094 / v2.32.0）ship —— orchestration graph 成为唯一权威控制流模型 + 统一建图工具（`add_plan_dependency` kind/when/max_rounds）+ ③ 的引擎有界 reopen（`ResolveCondition`/`ApplyConditionResult`/`countReopens`）。Stage 直接复用这三样。
> **来源**：issue-d07007e5 讨论。

## 1. 动机

PD 在大 plan 里反复用「分批验收」：一批节点做完 → 验收 → 下一批 →……（如 v2.32.0 那种 16 节点的 plan，每个 batch barrier 都是手工用节点依赖 + 一个验收节点拼出来的）。这个模式**好用但不是一等公民**——barrier 边一条条连、stage 级进度/重试无把手。参考 Apache Spark 的 stage + task 模型，把它**固化成 plan 的一等概念**：plan 由 Stage 组成，每个 Stage 是一个子 DAG，Stage 边界是一个 barrier + 一个可选 gate（验收）。

## 2. 决策（已确认）

1. **Stage 可选**——加法特性、完全向后兼容。不声明 stage → 就是现在的纯节点 DAG，零影响。
2. **Stages 组成 DAG**（非串行）——Stage 之间可有依赖，可并行。
3. **显式声明**——PM/agent 显式声明 stage（不像 Spark 从 shuffle 边派生；我们没有天然边界信号，"一批"是人的判断）。
4. **Barrier = 「stage 内业务节点全完成 且 gate 通过」**——gate 是 barrier 携带的验收/校验节点，是「分批验收」的价值；不声明 gate 则退化成纯 barrier（只等全完成）。
5. **不支持跨 stage 回退**——gate reject 只重跑**本 stage**（stage-local 有界重试），不回退上游 stage（v1 明确 out of scope：跨 stage 回退语义复杂——下游作废/幂等）。
6. **Stage 轻量一等**——落一张 `pm_stages` 记录（一等、可寻址可查），但 **Stage 自己不驱动执行、不存独立状态机**：状态是成员节点的**投影**，执行**全托付** graph 引擎。

## 3. 心智模型

一个 Plan =
- **节点的 DAG**（无 stage，= 现状），或
- **Stage 的 DAG**（每个 Stage 内又是一个节点子 DAG）。

一个模型覆盖两种。外层 = stage DAG，内层 = 每个 stage 的节点子 DAG。

```
Plan
 ├─ Stage A ─┐          Stage A: [Dev → Review → Decision] + Gate_A
 │           ├─→ Stage C   （Stage C depends_on A,B：A、B 都「全完成+gate通过」才起）
 └─ Stage B ─┘
```

## 4. 领域模型

### 4.1 Stage（轻量一等聚合）

`pm_stages` 表：

```
Stage {
  id:                 StageID
  plan_id:            PlanID
  name:               string
  depends_on_stages:  []StageID    // 外层 stage DAG 的边
  gate_node_id:       NodeID?      // 该 stage 的出口 gate（一个 condition/验收节点），可空
  max_rounds:         int          // stage-local 有界重试上限（gate reject 重跑本 stage 的轮次）
  created_at / updated_at
}
```

- **节点归属**：每个 task / graph 节点带 `stage_id`（可空 = 不属任何 stage）。
- **status 不落库、是投影**：`stage.status` 由成员节点状态推导——
  - 成员全 completed 且 gate 通过 → `done`
  - 有成员 running / gate 未决 → `running`
  - gate reject 触发 reopen → `reopen`
  - 全未起 → `open`
  - 无第二套被独立推进的状态机。

### 4.2 落在 orchestration graph 上（不另起引擎）

- **Stage = graph 节点按 `stage_id` 的分组**。
- **Stage 内子 DAG** = 带该 `stage_id` 的节点 + 它们之间的边（复用现有 business/control 节点 + Start/End）。
- **Stage Gate = 一个 condition 节点**：上游 = 该 stage 所有业务节点，`success` → 放行下游 stage 入口，`reject` → reopen 本 stage 子 DAG（有界 `max_rounds`）。复用阶段二 ③ 建的引擎有界 reopen。
- **外层 stage DAG** = 下游 stage 入口节点 `depends_on` 上游 stage 的 gate 节点（stage 级 barrier 落成节点级依赖）。

## 5. 语义

- **Stage barrier**：一个 stage 的子 DAG 派发 ⟺ 其所有上游 stage 都「子 DAG 全完成 且 gate 通过」。复用引擎 `ReadyNodes` / `IsAutoDone`（stage 子 DAG auto-done + gate 就绪）。
- **Stage gate**：stage 内业务节点全完成 → gate condition 就绪；`pass` → 放行下游；`reject` → reopen 本 stage 子 DAG（清 dispatch/outcome、有界 `max_rounds`，复用引擎 `ResolveCondition`/`ApplyConditionResult`/`countReopens`）。
- **Stage-local 有界重试**：gate reject 只重跑本 stage 的成员节点集；**不回退上游 stage**；耗尽 → escalate / 记 exhausted（复用阶段二的耗尽语义）。
- **内层控制流正交**：stage 内子 DAG 可含普通节点级 Decision/loopback（Dev↔Review），与 stage-level gate 互不干扰。
- **Stage 结构不变量（建图校验）**：stage 内节点只能有 stage 内的边（+ 自动 barrier）；手工连一条**绕过 gate 的跨 stage 边**（下游 stage 节点直接 `depends_on` 上游 stage 的非-gate 节点）= **建图时报错**——否则 barrier 被绕过、gate 形同虚设。stage 入口节点 = stage 内无前驱者，自动挂「上一 stage gate pass」的 barrier 边。（2026-07-12 grill 补）
- **无 stage**：plan = 现在的节点 DAG，行为零变化。

## 6. Agent 建图工具

- `create_stage(plan_id, name, depends_on_stages[])` → `stage_id`
- 节点归入：`add_task_to_plan` / `add_graph_node` 带 `stage` 参数
- Stage gate：声明该 stage 的 gate 节点（一个验收/校验 task），或 `create_stage` 自动生成一个 gate condition
- Stage 内控制流：复用阶段二统一工具 `add_plan_dependency(kind/when/max_rounds)`

## 7. 监控

- 详情页 / 进度：**stage 级** —「Stage 2/3：③④ 行为不变 · running」+ 每 stage 内子 DAG 进度 / 重试轮次。
- Stage 一等 → 可 `get_stage` / 列 stage / 查 stage 状态（投影）/「重开某 stage」有把手。

## 8. 向后兼容

- Stage 纯加法：`pm_stages` 空 + 节点 `stage_id` 全空 = 现状纯节点 DAG，零影响。
- 现有 plan 不受影响。

## 9. v1 范围 & Out of scope

- **v1**：§2–§7 全部（Stage 聚合 + 落图 + barrier/gate/stage-local 重试 + create_stage 等工具 + stage 级监控）。
- **Out of scope（v1 不做）**：跨 stage 回退（gate reject → 退回上游 stage）。

## 10. 开发 plan 骨架（阶段二 ship 后建）

1. `Stage` 聚合 + `pm_stages` 持久化 + 节点 `stage_id`（迁移）。
2. 落图：`stage_id` 分组 + stage gate condition 生成 + 外层 stage DAG 依赖。
3. barrier / gate / stage-local 有界重试驱动（复用引擎 ReadyNodes/IsAutoDone/ResolveCondition/countReopens，Stage 不重写执行）。
4. `create_stage` 等 agent 工具 + `add_task_to_plan` stage 参数。
5. 详情页 stage 级渲染 + 进度 + `get_stage` API。
6. 部署级真跑验收（stage barrier + gate pass/reject/stage 重试 + 无 stage 零回归）。

## 11. 参考

- Apache Spark：job → stage（shuffle 边界）→ task。
- 本仓 orchestration engine 设计：`docs/design/features/2026-07-02-orchestration-engine-design.md`（business/control 节点 + Start/End/Condition）。
- 阶段二（issue-6a253094 / v2.32.0）：graph 唯一权威 + 统一建图工具 + 引擎有界 reopen（Stage 的硬前置与复用来源）。
