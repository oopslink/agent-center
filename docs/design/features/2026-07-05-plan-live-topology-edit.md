# Plan 运行中追加式动态拓扑编辑（Live Topology Edit）

- **Status**: Proposed（设计基线）
- **Issue**: issue-00995bf5
- **Date**: 2026-07-05
- **Author**: PD
- **Related**: [`2026-07-02-orchestration-engine-design.md`](./2026-07-02-orchestration-engine-design.md)（编排引擎 / plan→graph 编译）

---

## 1. 背景与目标

- **现状**：running plan 的拓扑编辑（`add_task_to_plan` / `add_plan_dependency` / `remove_task_from_plan` / `remove_plan_dependency`）要求 plan 处于 draft（`ErrPlanNotDraft`），必须 `stop_plan → 改 → start_plan`。完成的节点在 stop→start 后保留（task 状态是 source-of-truth，`start_plan` 的 `syncGraphToTasks` 重新同步），但要暂停全局。
- **问题**：对「追加节点 / 插入后续工作 / 调整未跑节点的顺序」太死板，不符合 AI 驱动的自适应编排——orchestrator 半路发现要加活，不该被迫暂停整个 plan。
- **目标**：running plan 支持**安全的追加式拓扑编辑**——已完成的活不丢、不打断在飞节点、并发安全。

## 2. 核心概念

### 2.1 节点可变性（mutable）

> `node mutable ⟺ 无 dispatch record 且 task 非 terminal / 非 running ⟺ node_status ∈ {blocked, ready}`

依据现有 `DeriveNodeStatus(taskStatus, upstreamAllDone, dispatched, paused)`（`plan_view.go`）——`dispatched` 即 dispatch record 存在（task 此时仍 `open`，是 derived 态）。`dispatched / running / done / discarded` → **immutable**（结构不可 live 改）。可变性判据是**已有的**，无需新增状态。

### 2.2 编辑 = 带版本的提交（commit）

- `pm_plans.version` 单调递增（**已有字段**）。
- 编辑基于读自 `get_plan` 的 `base_version`，提交时做 compare-and-swap。

## 3. 接口：单一批量原子接口

```
edit_plan_topology(plan_id, base_version, ops[])
```

**唯一**的拓扑编辑入口，draft 与 running 都走它；退役 per-op 工具。

- `ops`：`add_node{task_id}` | `remove_node{task_id}` | `add_edge{from,to,kind,when?,max_rounds?}` | `remove_edge{from,to}`（可选：`add_node` 内联 `create_task`）。

## 4. 事务语义（TOCTOU / 一致性）—— 方案命门

一个 `RunInTx` 写事务，原子 all-or-nothing：

1. **CAS**：`plan.version == base_version` 否则 `ErrPlanVersionConflict`（并发编辑 → 调用方 rebase 重试）。
2. 在内存副本上应用**整批** ops。
3. **只校验终态（不校验中间步）**：acyclic、resolvable assignee；running 时每个**结构被动的节点**都 mutable 否则 `ErrPlanNodeInFlight`（点名冲突节点）。
4. 持久化 + `version++` + 写 audit ledger（`topology_commit` diff）+ 重算 ready set + 派发新就绪节点。

**为什么没有 TOCTOU 窗口**：auto-advance（`dispatchReadyNodes`）也是 `RunInTx` 写事务；**SQLite 单写者串行化** → edit 与 advance 不可能交错：

- edit 先拿写锁：查（未派发）→改→commit；advance 之后看到新拓扑按新结构派发。
- advance 先拿写锁：派发节点 X（写 dispatch record）→commit；edit 事务内**重查**看到 X 已有 record → 谓词失败 → 拒绝。

即 **CAS 管「编辑 vs 编辑」，mutable 检查管「编辑 vs 派发」**，两者在**同一提交事务内**。handler 层 `get_plan` 看到的状态仅供参考，**提交时以事务内重校验为准**（乐观并发）。`dispatchReadyNodes` 本身幂等（dispatch record INSERT-OR-IGNORE），进一步保证不会重复派发。

> **「只校验终态」是批量的核心价值**：reorder = 数个 remove+add，中间会临时成环 / 孤儿，只要**结果**合法即可；per-op 工具做不到（每步都得自洽）。

## 5. 版本化（三层，按需取）

| 层 | 内容 | 存储 |
|---|---|---|
| **1 · 版本号（必备·CAS）** | `pm_plans.version` 单调递增，每次拓扑 commit +1 | 已有 `pm_plans.version` |
| **2 · 历史（复用审计模块·零新存储）** | 每次 commit 写 `topology_commit{from_version,to_version,ops/diff,actor,at}` 进 plan 审计 ledger；可查/可重建版本 N | audit ledger（审计模块 issue-74df441a） |
| **3 · 快照 + rollback（可选）** | 存每版（或每 K 版）全量快照，支持 O(1) 历史读 + forward-only rollback | 新表 `pm_plan_revisions` |

- **rollback = forward-only（git-revert 语义）**：不倒退版本号，而是「以 v_N 的拓扑生成一个新 commit v_{cur+1}」，避免版本号回退带来的 CAS / 审计歧义。rollback 亦受 mutable 约束——**已执行节点不可被拓扑 rollback 抹掉**（撤已跑的活走 reopen / loopback，不是拓扑编辑）。
- **边界（语义纯净）**：版本化的是**编辑意图**；执行进度（running / done / dispatch）是 derived，**不进版本号**。两者正交 → 「版本」= 纯编辑史，「进度」= 实时视图。

## 6. 允许 / 禁止矩阵（running plan）

| 操作 | 允许条件 |
|---|---|
| `add_node` | 总允许（新节点、无入边、无 dispatch） |
| `add_edge(from,to)` | `from` 可变 + 终态不成环；`to` 任意态（可依赖已 done 节点） |
| `remove_edge(from,to)` | `from` 可变 |
| `remove_node(t)` | `t` 可变 且所有依赖 `t` 的下游也可变 |

**禁止**（→ `ErrPlanNodeInFlight`，需 `stop_plan` / reopen）：改在飞/已完成节点的入边、删在飞节点、终态成环。**撤销已执行的活 → reopen / loopback，非拓扑编辑。**

## 7. 迁移

- 退役 `add_task_to_plan` / `remove_task_from_plan` / `add_plan_dependency` / `remove_plan_dependency`（或降级为 deprecated 的单-op 薄壳，内部走 `edit_plan_topology`）。
- plan authoring 流：`create_plan → create_task ×N → edit_plan_topology([add_node…, add_edge…]) → start_plan`（若把建 task 折进批量 → `create_plan → edit_plan_topology → start_plan` 三步）。
- cycle 流程模版随之再改一版（继「graph 工具 → plan API」之后）。

## 8. 分期

- **MVP**：`edit_plan_topology`（4 类 op）+ CAS + mutable 谓词 + 终态校验 + audit `topology_commit`（层 1+2）；per-op 工具降级。
- **扩展**：层 3 快照 + rollback；staging（propose → review → commit）；`add_node` 内联建 task。

## 9. 测试（重点）

- **并发**：edit × auto-advance 的 tx 隔离（edit 抢先 / advance 抢先两向都一致）；`base_version` 冲突 → `ErrPlanVersionConflict`；提交前节点被派发 → `ErrPlanNodeInFlight`。
- **终态校验**：批量 reorder 中间态成环但终态合法 → 通过；终态成环 → 整批拒。
- **边界**：改在飞节点入边拒；删在飞节点拒；done 节点跨编辑保留。
- **回归**：draft 走 `edit_plan_topology` 与旧 per-op 等价；全套 cycle plan 不破。

## 10. 风险 / 范围

主风险 = 并发 tx（靠 SQLite 单写串行 + CAS + 幂等 dispatch 化解）+ 迁移动到所有建 plan 的路径与模版（需回归全套 cycle plan）。**范围 ~1 cycle（MVP）。**
