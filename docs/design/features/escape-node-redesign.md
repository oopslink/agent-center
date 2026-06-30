# 逃生节点重设计 —— escape 降为 Decision 终态 + 升级信号（A），穷尽处置策略化（C）

> **状态**：设计稿（已与 oopslink 对齐，采纳「A + C」+ 推荐决策）。
> **来源 issue**：issue-624bfb53。
> **基线**：v2.13.0 控制流引擎（B0/B1/B2/B3），原 spec `docs/releases/v2.13.0/control-flow-engine-spec.md`。
> **范围**：仅改控制流引擎对「Decision 回环耗尽」的建模与处置；不动 Dev/Review 语义、不动 Gate 之外的 seq 链语义。**本文为设计交付，不含代码改动。**

---

## 1. 背景与问题

cycle 控制流图当前为**每个 feature** 静态预铺一个 **Escape（逃生/人工兜底）节点**：它是 Decision 在 outcome `reject_exhausted` 上的 conditional 后继；owner 留空。Decision 的有界 loopback（→Dev，`MaxRounds` 默认 3）跑满后，引擎路由到它。它本身**不做任何事**——只是个等 PD 手动处理的停车位。

**三处不优雅（重构动机）**：

1. **把异常编码成图里的一等顶点**：N 个 feature 预铺 N 个几乎永不走到的死节点，污染 DAG，并逼着 `plan_view.go` 为「未走到的 escape 停在 settled / NodeFailed / 被 prune 收敛」做三态特判。异常路径不该是静态拓扑的一部分。
2. **被动、非兜底而是停摆**：owner 空，触发后无人指派，纯靠 PD 盯；无任何自动恢复/降级动作。
3. **策略被焊死**：穷尽后只有「人工」一条路，无法表达「放弃该 feature 让 Gate 降级放行 / 带标记强合」等合理处置。

### 1.1 现状精确地图（file:line）

| 关注点 | 位置 |
|---|---|
| Scaffold 预铺 escape 节点 + 条件边 | `internal/projectmanager/service/plan_scaffold.go:374-386`（节点+边）；常量 `cycleOutcomeRejectExhausted` `:62-69` |
| 耗尽路由（**引擎运行时**，A 的真正核心改点） | `internal/projectmanager/service/plan_controlflow.go` `applyLoopbacks:54-136`，耗尽分支 `:111-134`（拼 `outcome+"_exhausted"`、扫逃生边、`RecordDecisionOutcome`、未命中 `notifyLoopStuck`） |
| B3 裁决（**只产 pass/reject，从不产 reject_exhausted**） | `internal/projectmanager/decision_auto.go:54-60`（注释明确「engine's escape edge」由引擎写） |
| plan-view 逃生语义（无独立 if，集中在 T467 剪枝豁免**注释**） | `internal/projectmanager/plan_view.go:260-266`、`:336-341`；`pruneImmune` `:267` |
| 角色枚举 | `internal/projectmanager/plan_unmerged.go:35` `CycleRoleEscape`；`IsValid` `:47-49` |
| Decision 完成即终态 / block 约束 | `internal/projectmanager/task.go:56-62`（状态机）；`BlockTask` 仅对 `running` 有效（`assign_flow.go`） |
| Gate barrier seq 上游就绪判定 | `plan_view.go:348-353`（`seqUp` 要求 `taskIsDone`） |

---

## 2. 方案 A —— escape 从「顶点」降为「Decision 终态 + 升级信号」

**核心**：loopback 跑满时，不再路由到预铺的 Escape 顶点，而是：

1. 引擎 `RecordDecisionOutcome(decision, "reject_exhausted")`——保留该 outcome 值名（向后兼容历史 plan），它成为 Decision 的**终结 outcome 语义**，**不再对应任何条件出边**（Escape 没了）。
2. 触发 `escalateExhaustion(plan, decision)`：复用现有 `PostMention` 通知通道（同 `notifyLoopStuck`/`notifyCreatorOnFailure`）@mention PD/creator，告知「feature X 的 Decision 回环耗尽（escalated），需介入裁决」。
3. **不引入新的 task 生命周期状态**：Decision 仍是 `TaskCompleted`。可观测性由 `outcomeOf[decision] == reject_exhausted` 体现，不靠死节点。

**为什么删了 Escape 顶点视图仍正确**：cycle 正常流里 Decision 仅剩一条条件出边 `pass→Integrate`。当 outcome=`reject_exhausted` 时，Integrate 的条件 in-edge `When=pass` 不匹配 → Integrate 自动落 `NodeSkipped`（`plan_view.go` 死分支剪枝）——这正是「本 feature 不集成、悬停等裁」想要的，无需 Escape 顶点。

**关键约束（已落实到实现选择）**：耗尽时 Decision 已 `TaskCompleted`（终态），**不能** `BlockTask`（它要求 `running`）。Integrate 此刻是 `blocked` 也非 `running`。故 escalate **用 `PostMention` 通知 + 不对任何节点打 BlockTask**（见 §5 决策 Q6）。

**plan-view 简化**：T467 的 `pruneImmune`（discarded 不豁免）规则**逻辑保留**（其它正交场景仍可能产生死分支上的 discard），但 `:260-266`、`:336-341` 处**以 escape 为例的注释改写**，不再提逃生节点。净效果：plan-view 里没有以 escape 命名的分支被删（它本就没有），删的是 scaffold 的顶点 + 引擎的边路由，plan-view 仅做注释去 escape 化，逻辑保持纯通用条件流求值。

---

## 3. 方案 C —— 穷尽处置策略化（正交叠加于 A）

把「回环耗尽后怎么办」做成 **per-feature 策略**，scaffold 时选定，引擎按策略分发：

```
type ExhaustionPolicy string
const (
    ExhaustionEscalate       ExhaustionPolicy = "escalate"        // 默认（= A 行为）
    ExhaustionAbandon        ExhaustionPolicy = "abandon"
    ExhaustionForceIntegrate ExhaustionPolicy = "force-integrate"
)
```

**存储**：放在 **Decision 节点的 cycle metadata**（per-feature）。`CycleNodeMeta`（`plan_unmerged.go`）+ Task AR（`task.go`，随 `SetCycleMeta` 持久化）新增 `ExhaustionPolicy` 字段；新增 schema 列 `exhaustion_policy TEXT NOT NULL DEFAULT 'escalate'`（DEFAULT 保证旧行 = escalate，向后兼容）。

引擎耗尽分支（`plan_controlflow.go applyLoopbacks` 现 `:111-134`）重写为：先 `RecordDecisionOutcome(decision, "reject_exhausted")`（让 plan-view 知道 Decision 非 pass 终结），再 `switch policy`：

### 3.1 `escalate`（默认，= A）
升级给 PD（`PostMention`），Integrate→`NodeSkipped`，feature 悬停等人裁（手动 reopen / 重裁 / 改判）。语义等价今天「Escape 顶点变 ready 等人接」，但少一个顶点。

### 3.2 `abandon`（放弃本 feature，Gate 降级放行其余）
本 feature 不集成，但**不阻塞整个 plan**——其它 feature 照常 ship。

链路：
1. outcome=`reject_exhausted` → Integrate 落 `NodeSkipped`（自动）。
2. **Gate barrier 需要「degraded/partial」概念**。今天 Gate `blocked_by 每个 feature 的 terminal`（**seq 边**），seq 上游必须 `Done` 才解锁（`plan_view.go:348-353`）。被 abandon 的 feature 其 Integrate 是 `NodeSkipped` ≠ `Done` → Gate 永久卡死。
   **修法**：Gate 这类 barrier 的 seq 上游就绪判定由 `taskIsDone` 放宽为 `settled = Done || Skipped`，**仅对 `Role==gate` 的节点放宽**（精准，不波及普通 seq 链——见决策 Q2）。
3. `PlanView` 暴露 `degraded bool`（任一 feature 被 abandon），进入 degraded 时 @mention PD「N 个 feature 被 abandon，Gate 降级放行，请确认」并在 board / Accept 列明被 abandon 的 feature 清单（决策 Q4）。
4. Accept / Ship 照常。

### 3.3 `force-integrate`（耗尽仍合入，带标记）
1. 引擎写新 outcome `pass_forced`（与 `reject_exhausted` 并列记录），并让 scaffold 的 Integrate 条件 in-edge `When` 同时接受 `pass | pass_forced`（多值 When 在 plan-view 已支持——`condUp[...][To]` 是 append 列表）。→ Integrate 命中、可完成。
2. Integrate 节点打 `force_integrated` 标记（task tag / cycle metadata flag），board / Accept / 通知显式标注「⚠️ 未通过评审，强制集成」。
3. **不绕过 F3 merge guard、不绕过 §-1 gate 绿**（决策 Q3）——只放宽「review reject」这一逻辑门，不放宽真实 git ancestry 与构建红。

---

## 4. 已采纳的决策（oopslink「按推荐的做」）

| # | 问题 | **决策（= 推荐）** |
|---|---|---|
| Q1 | escalate 下是否置 `has_failed`？ | **否**。等人裁 ≠ 失败，对齐今天 Escape 是 open（非 failed）。 |
| Q2 | abandon 的「seq 上游 Done→Done‖Skipped」放宽范围 | **只对 `Role==gate`**。普通 seq 链语义不变。要求 `ComputePlanView` 能读节点 role（Task 上已有 `Role()`，签名可不变）。 |
| Q3 | force-integrate 是否仍强制 gate 绿 | **是**。只放宽 review reject，不放宽构建红 / 不绕 merge guard。 |
| Q4 | abandon 是否在 board/Accept 显式标 ABANDONED | **是**。`PlanView.degraded` + Gate 通知列明被 abandon 的 feature 清单，避免 PD 漏看。 |
| Q5 | 耗尽**发生后** PD 能否手动改 policy 重裁 | **暂不**。F1/F2 先支持 scaffold-time 固定；post-hoc 改 policy（reopen Decision + 重写 outcome）留作后续增量。 |
| Q6 | escalate 的载体 | **`PostMention` 通知 + 不对 Decision/Integrate 打 BlockTask**（二者非 `running`，BlockTask 不适用）。若产品上要「Integrate 显式 blocked 高亮」，需一个能对非 running 节点记 obstacle 的新机制——超出「复用现有 block/unblock」承诺，留待后续单独评估。 |
| Q7 | 历史 plan 双轨保留多久 | 引擎长期保留 `hasLegacyEscapeEdge` 兜底分支；在某大版本设「历史 Escape plan 已全部 ship」检查点后再删（见 §6）。 |

---

## 5. 向后兼容 / 迁移

**结论：不迁移历史 plan；新行为只对新 scaffold 的 plan 生效；引擎对历史 Escape 边保留旧路由。**

理由：
1. 历史 plan 可能正在运行（Decision 处于某轮回环），改图 = 改运行态，违反「向后兼容硬约束」+「派生不落库、append-only」。
2. Escape 顶点是真实 task（有 id/会话/历史），删它要处理 archived/discard/会话回收，风险远高于收益。
3. 引擎兜底成本极低。

**Cutover 动作**：
- `applyLoopbacks` 耗尽分支：先 `hasLegacyEscapeEdge(edges, decision, outcome)`（即原 `:114-124` 扫边逻辑封装）。命中 → 走旧路由（写 `reject_exhausted`、路由到 Escape 顶点）；否则 → 新策略分发（§3）。
- `CycleRoleEscape`（`plan_unmerged.go:35`）+ `IsValid()`：**保留**，标 `// deprecated: only on pre-vX plans`（历史 board 仍要认）。
- 新 scaffold **不再产出** Escape 顶点 + `reject_exhausted` 条件边。
- 新增列 `exhaustion_policy DEFAULT 'escalate'`：历史 Decision 行读出 `escalate`，但因其有 Escape 边，`hasLegacyEscapeEdge` 先命中走旧路由，该字段对历史 plan 实际不生效——双保险无冲突。

---

## 6. 分阶段实施

| 阶段 | 范围 | 验收（硬性：含部署级真跑） |
|---|---|---|
| **F1** A 核心（demote escape → Decision 终态 + escalate）+ 历史兜底 | 删 scaffold 的 Escape 顶点/边；`applyLoopbacks` 耗尽分支改为「写 `reject_exhausted` + `escalateExhaustion`」+ `hasLegacyEscapeEdge` 兜底；plan-view 注释去 escape 化；`CycleRoleEscape` deprecated | 单测：新 scaffold 无 `escape` 角色节点 / 无 `reject_exhausted` 边；耗尽时写 outcome + 调 escalate 通知；历史带 Escape 边的 plan 仍走旧路由（回归 T272 用例）；耗尽后 Integrate=`NodeSkipped`、`has_failed` 不置位。**deployed e2e**：`install test-instance` 起隔离实例，scaffold MaxRounds=1 cycle，驱动至耗尽，断言 PD 会话收到 escalation @mention、Integrate skipped、plan 不卡死 |
| **F2** policy 字段 + `abandon`（含 Gate 降级） | `ExhaustionPolicy` 枚举 + schema 列 + Task/CycleNodeMeta 字段 + scaffold 入参；abandon 的 Gate seq 上游 `Done‖Skipped` 放宽（仅 `Role==gate`）+ `PlanView.degraded` | 单测：scaffold 写 policy、默认 escalate；abandon 下 Gate 在「被 abandon 的 feature skipped + 其它 done」时 ready、plan 可至 Ship、`degraded==true`；只对 gate 放宽（防回归）。**deployed e2e**：两 feature，A abandon+MaxRounds=1 驱动耗尽、B 正常 pass，断言走到 Ship、Gate 通知标 degraded、A 的 Integrate skipped |
| **F3** `force-integrate` | `pass_forced` outcome + Integrate 条件边接受多值 When + force 标记 + 通知；仍要求 gate 绿、不绕 merge guard | 单测：耗尽后 Integrate 命中可完成、带 force 标记、gate 仍要求绿、不绕 merge guard。**deployed e2e**：force feature 实际走完 Integrate→Gate→Ship、board/通知体现 force flag |
| **F4** 文档 + 清理 | deprecate 记录、`mcphost` 工具描述更新（去「escape/人工兜底」，加 `exhaustion_policy` 入参）、原 spec §4.1/§9 注脚更新 | doc-only |

> PR 顺序 F1→F2→F3→F4。F1 可独立上线（escalate 即覆盖今天 Escape 的核心价值）；F2/F3 增量叠加，互不阻塞。

---

## 7. 影响面与风险

- 动到 plan 引擎核心（`ComputePlanView` / decision routing / scaffold / cycle role），**有回归面**——靠 §5 双轨兜底 + 每阶段 deployed e2e 收口。
- `ComputePlanView` 需读节点 role 做 Gate 放宽（Q2）；Task 上已有 `Role()`，预期签名不变。
- `pass_forced` 多值 When 依赖 plan-view 现有 `condUp` append 语义——实现前需复核该路径确实支持一个 To 多个 When。
- 长期保留 `hasLegacyEscapeEdge` 双轨分支（Q7），在历史 Escape plan 全 ship 后清退。
