# Control-Flow Engine Spec — v2.13.0 / B0

> **本文是什么。** B 轨地基设计：把 plan 从「纯 AND-DAG」升级为**控制流图**——
> 引入**决策/网关节点**、**条件边**（按节点产出路由，只走命中分支）、**有界回环边**
> （reject→回上游 Dev，带最大轮数 + 退出守卫）。redefine cycle 模型为
> `Dev → Review → Decision{ 过 → Integrate, 打回 → 回 Dev(有界) }`。
>
> **为什么。** F1 把 cycle 固化成线性 DAG，但「Review 打回 → 重做 Dev」这种**回路**
> 在纯 DAG（禁环）里表达不了，只能靠人手动重开节点（原 F5「重开 Dev 机制」）。
> 本设计用**结构化的有界回环**取代手动重开，把「评审不过就返工、过了才集成」编进引擎。
>
> **它驱动谁。** 本文是 B1/B2/B3 的实现依据：
> - **B1**（`b1-cf-engine`）：实现引擎——决策/网关节点 + 条件边 + 有界回环边。
> - **B2**（scaffold 升级）：`scaffold_cycle_plan` 生成控制流图（而非线性链）。
> - **B3**（决策自动化）：Decision 节点产出（pass/reject）由 §-1 gate 结果自动判定。
>
> 来源 issue：**I18 = issue-b10f3ca1**。前置：F1 规格（[cycle-node-graph-spec.md](./cycle-node-graph-spec.md)）已合主干。
> 状态：B0 设计交付。建议 owner=Dev、reviewer=B0-Review。

---

## 0. 设计原则（锁定）

1. **向后兼容是硬约束。** 现有纯 DAG plan（无决策节点、无条件/回环边）**行为逐字不变**。
   所有新语义都走「新增、默认关闭」的扩展位：不带新属性的边/节点 == 今天的语义。
2. **派生不落库的不变量延续。** node_status 仍是 `f(...)` 纯派生（§9.2），永不持久化。
   新增的「编排者拥有的存储状态」只允许是 **append-only / INSERT-OR-IGNORE 幂等记录**
   （对齐现有 `DispatchRecord`），不破坏 replay/并发安全。
3. **复用既有状态机，不发明新 task 生命周期。** 回环重做复用现成的
   `Completed → Reopened → Open → Running` 合法迁移（task.go），不加新状态。
4. **引擎通用、cycle 是其实例。** 引擎只懂「决策/条件边/有界回环」三件原语；
   `Dev→Review→Decision` 的具体形状是 B2 scaffold 按本原语拼出来的，引擎不硬编 cycle 角色。

---

## 1. 现状回顾（grounding，B1 在此之上改）

当前 plan 引擎（`internal/projectmanager`）：

- **边**：`Dependency{PlanID, FromTaskID, ToTaskID}` = `From depends_on To`（To 先 done，From 才可起）。
- **无环**：`ValidateNoCycle` / `WouldCreateCycle` 三色 DFS 拒绝任何环。
- **节点状态派生**（`plan_view.go::ComputePlanView` / `DeriveNodeStatus`）：
  `node_status = f(task.status, upstreamAllDone, dispatched, paused)`，其中
  **`upstreamAllDone` = 该节点所有上游都 terminal-done（AND）**。Ready 当且仅当全上游 done。
- **编排者存储状态**：仅 `DispatchRecord{plan_id, task_id, dispatched_at, msg_id}`（PK 幂等）。
- **自动推进**（`PlanOrchestratorProjector`）：消费 `pm.task.state_changed` + `pm.plan.started`
  → `dispatchReadyNodes` → `ComputePlanView` → 对 `ReadySet` 每个节点 PostMention +
  RecordDispatch + emit `pm.task.assigned`（WorkItemProjector 唤醒 agent）。`AllDone` 时 `MarkDone`。
- **失败**：discarded 节点令其下游永久 `blocked` + 通知 plan creator；plan 不自动终止。

**关键约束识别**（B0 要打破/扩展的三点）：
1. `upstreamAllDone` 是 **AND**——无法表达「只走命中分支」的 OR/路由语义。
2. 禁环——无法表达回环。
3. `AllDone` 要求**每个**节点 done——一旦有条件分支，没走的分支永远不 done，plan 永不完成。

---

## 2. 新概念总览

### 2.1 节点类型（node kind）
| kind | 含义 | 完成语义 |
|---|---|---|
| `task`（默认） | 普通工作节点（Dev/Integrate/...） | 完成即 `completed`，无产出标签 |
| `decision` | 决策/网关节点 | 完成时带一个 **outcome 标签**（如 `pass`/`reject`），用于路由出边 |

- 未标 kind 的节点 == `task`（向后兼容）。
- `decision` 节点本身仍是一个 task（有 assignee、会话、work item）；区别只在**完成时必须带 outcome**，
  且其**出边按 outcome 路由**。Review 可以**就是**一个 decision 节点（Review 产出 pass/reject，融合形态），
  也可以 Review(task) → Decision(gateway) 分开；选择权在 B2 scaffold，引擎两者都支持。

### 2.2 边类型（edge kind）—— `Dependency` 扩展
扩展 `Dependency` 增加两个**可选**字段（默认值 == 今天语义）：

```
Dependency{
  PlanID, FromTaskID, ToTaskID
  Kind  EdgeKind   // "seq"(默认) | "conditional" | "loopback"
  When  string     // 仅 conditional/loopback：命中的 outcome 标签（如 "pass"/"reject"）；seq 恒空
}
```

| Kind | 语义 | 方向 |
|---|---|---|
| `seq`（默认） | 硬 AND 依赖，**与今天逐字相同**：To done → From 可起 | 前向（无环） |
| `conditional` | 条件依赖：To 是 decision，**仅当 To.outcome == When 时本边激活**；否则本边「死」 | 前向（无环） |
| `loopback` | 有界回环：From 是 decision，To 是其**前向祖先**（如 Dev）；当 From.outcome == When（如 reject）时**重激活** To 子图，受最大轮数约束 | 后向（成环，单独校验） |

> **语义保持**：`Kind=seq` 且 `When=""` 的边集 ≡ 当前 `Dependency` 集合 ⇒ ComputePlanView 输出不变。

### 2.3 outcome 标签
- 由 decision 节点完成时给出（自由小写串；cycle 约定用 `pass` / `reject`）。
- **存储**：新增编排者拥有的幂等记录 `DecisionOutcome{plan_id, task_id, outcome, round, decided_at}`
  （PK `(plan_id, task_id, round)`，INSERT-OR-IGNORE），对齐 `DispatchRecord` 的存储纪律。
  `round` 见 §4。
- **谁给**：
  - **B1 起**：手动——complete decision 节点时带 `outcome`（MCP/AppService 增一个可选入参；见 §6）。
  - **B3 起**：自动——§-1 gate 全绿且无阻塞 comment ⇒ `pass`，否则 `reject`；边界（无 gate / 有 comment）
    回落人工。B3 只是 outcome 的**自动产生器**，不改本引擎契约。

---

## 3. 条件边语义（路由 + 死分支剪枝）

设节点 N 的入边按 kind 分组。N 的前向就绪判定（取代旧 `upstreamAllDone`）：

1. **seq 入边**：每条的 To 必须 `done`（与今天相同的 AND）。
2. **conditional 入边**：按其 To（同一个 decision）分组。对每个 decision To：
   - To 未 done → **pending**（N 还不能判定，保持 blocked）。
   - To done 且存在一条 `When == To.outcome` 的入边 → 该 decision 对 N **满足**。
   - To done 但**没有**入边的 When 命中 outcome → 该 decision 对 N 走的是**别的分支** ⇒ N 在本路径上**死**（见下「剪枝」）。
3. N **Ready** 当且仅当：所有 seq 入边 done **且** 每个 conditional-decision 分组都「满足」**且** N 未被剪枝。

### 3.1 死分支剪枝与新状态 `NodeSkipped`
- 当一个 decision 的 outcome 选定后，**未命中**的 conditional 出边的整条下游子图（直到汇合点之前）
  不会执行 ⇒ 这些节点必须进入一个**终态** `skipped`，否则 §1 的「AllDone 要求每节点 done」会让 plan 永不完成。
- 新增派生状态 **`NodeSkipped`**（terminal，类似 done 计入「已结算」）：
  一个节点若其**所有**到达路径都经过「已选了别的分支」的 decision，则为 `NodeSkipped`。
- `NodeSkipped` 是**派生**的（由 outcome 记录 + 图可达性算出），不落 task.status（task 仍 `open`，只是永不被 dispatch）。
- **AllDone 重定义**：`AllDone = 每个节点 ∈ {done, skipped}`（而非全 done）。`MarkDone` 据此触发。
  → 向后兼容：无 conditional 边时无 skipped 节点，退化为「全 done」== 今天。
- **T467（issue-f5067ad2）剪枝豁免修正**：剪枝只对 **DONE（completed）** 节点豁免——一个真正完成了自身工作的节点永不被重分类。一个 **discarded** 节点**不**豁免：未命中的逃生/兜底节点（如某 feature 走 pass，其 `reject_exhausted` 节点）常被当 cleanup 弃置（discarded），这种「死分支上的 discard」必须收敛为 `NodeSkipped`、**不计入 `has_failed`**。反之，**活分支**（被选中的命中分支 / 无 conditional 的普通路径）上的 discarded 节点剪枝永远触达不到它 ⇒ 仍是 `NodeFailed`（真实失败：dev 真挂、或逃生分支真被走后弃置）。派生分类的优先级：`done > running > skipped > failed`——`running` 永远如实显示（有界回环可能重判 decision 并重激活该节点），`skipped`（死分支）优先于 `failed`（discarded），保证未走分支的弃置不污染 `has_failed`。

### 3.2 汇合（join）
- 多条分支汇合到同一下游节点（如 reject 回环 vs pass 前进最终都到 Ship 之前的某节点）时，
  该汇合节点的入边可混用 seq/conditional；按 §3 规则求值（seq 全 done + 命中的 conditional 满足）。
- cycle 里典型无复杂 join（reject 直接回 Dev），但引擎按通用规则实现，B2 可拼更复杂图。

---

## 4. 有界回环语义（reject → 回 Dev，受控重做）

回环是本设计的核心新能力。形态：`Decision --loopback,when=reject--> Dev`。

### 4.1 轮数与守卫
- 每个 **loopback 边**带一个上界 `MaxRounds`（建议默认 3；存于边的元数据，见 §7 数据模型）。
- 编排者维护 **轮计数**：`LoopRound{plan_id, loopback_edge_id, round}`（落库，编排者拥有）。
  `round` 从 1 起（首轮正常执行不算回环）。
- Decision 完成且 `outcome == 当 (reject)`：
  - 若 `round < MaxRounds`：**重激活回环目标子图**（§4.2），`round++`。
  - 若 `round == MaxRounds`：**回环禁用**（退出守卫）。此时 decision 不得再走 reject 分支——
    它必须有一条**逃生 conditional 出边**（约定 `when=reject_exhausted`，scaffold 默认连到「升级/人工/标失败」节点），
    否则引擎判为 `plan stuck` 并通知 creator（复用现有失败通知通道）。**禁止无界回环**。

### 4.2 重激活子图（reopen 重做）
当一轮 reject 触发重做，引擎对**回环目标到该 decision 之间的前向链**（如 `Dev→Review→Decision`）执行：
1. 复用既有合法迁移把这些节点 task.status 从 `completed` **Reopen → open**（`Completed→Reopened→Open`，见 task.go 状态机），令其重新非终态。
2. **清这些节点本轮之前的 `DispatchRecord`**（使其重新进入 ready-set 可被再次 dispatch）。
   清除是 round-scoped：DispatchRecord PK 建议加 `round`（见 §7），避免「上一轮已 dispatch」永久压制重做。
3. 下一次 `dispatchReadyNodes` 自然把重激活的 Dev 当 Ready 再次派发（PostMention + 新 work item）。
4. decision 节点自身也 Reopen（它要在新一轮 Review 后再次决策），其 `DecisionOutcome` 按新 `round` 记录（PK 含 round，不覆盖历史）。

> **为什么用 Reopen 而非删节点重建**：节点身份（task id / 会话 / 历史）稳定，回环只是同一节点的多轮执行；
> 复用 `Completed→Reopened→Open` 既有迁移，零新状态、可审计每轮。

### 4.3 失败（discarded）与回环的关系
- Dev/Review 在某轮被 `discarded`（真失败，非 reject）：沿用今天语义——下游 blocked + 通知 creator，**不**自动回环。
  回环只由 decision 的 **reject outcome** 触发，与 task 失败正交。

---

## 5. 节点状态派生：新 `ComputePlanView` 算法（B1 实现）

把 `DeriveNodeStatus` / `ComputePlanView` 升级为控制流求值。伪代码（保持纯函数、无 I/O）：

```
inputs: tasks, edges(含 Kind/When), dispatch(含 round), outcomes(DecisionOutcome), loopRounds, paused
for each node N:
  # 优先级（T467）：done > running > skipped > failed。pruned 只对 DONE 豁免，
  # 故 discarded 的死分支节点也会被 pruned → Skipped；活分支的 discarded 才落 Failed。
  if N.task done:        status = Done;  continue
  if N.task running:     status = Running/Paused; continue
  # 前向就绪 / 剪枝求值（§3）：pruned 只跳过 DONE 节点，discarded 节点参与死分支剪枝。
  seqOK   = all seq-incoming.To are Done
  condOK  = for each conditional-decision group D of N:
               D done AND exists incoming(When == outcome(D, currentRound))
  pruned  = exists conditional-decision group D of N: D done AND no incoming When matches outcome
            OR N only reachable through pruned nodes        # 传递剪枝
  if pruned:             status = Skipped; continue          # 死分支终态（§3.1，含被弃置的未走逃生节点）
  if N.task discarded:   status = Failed; continue           # 活分支上的真实失败
  if not (seqOK and condOK): status = Blocked; continue
  status = dispatched(N, currentRound) ? Dispatched : Ready
# loopback 边不参与上面的前向就绪（它们是后向重激活触发器，§4），从前向图中排除
AllDone = every node ∈ {Done, Skipped}
```

要点：
- **loopback 边在前向就绪里被忽略**（只在 decision 完成事件处理时驱动 §4 的重激活）。
- `dispatched` / `outcome` 查询带 **currentRound**（回环多轮的关键）。
- `Skipped` 是新增终态，计入 `AllDone`。
- 无新属性时（全 seq、无 decision、无 loopback）：condOK 恒真、pruned 恒假、currentRound 恒 1
  ⇒ 退化为旧 `upstreamAllDone` 与旧 AllDone，**逐字兼容**。

---

## 6. 执行器/分发改动清单（给 B1）

1. **边模型**：`Dependency` 加 `Kind EdgeKind` + `When string`；新增 `EdgeKind` 常量（seq/conditional/loopback）。
   loopback 边的 `MaxRounds` 随边元数据（§7）。
2. **decision 完成带 outcome**：complete 路径（AppService `CompleteTask` / 对应 MCP 工具）加**可选** `outcome` 入参；
   仅 decision 节点接受/要求 outcome；写 `DecisionOutcome` 记录（PK 含 round）。普通 task 完成不变。
3. **ComputePlanView 重写**：按 §5 算法；新增 `NodeSkipped`；就绪/AllDone 改造；查询带 round。
4. **回环驱动**：在 `PlanOrchestratorProjector.advance`（或 `dispatchReadyNodes` 前）增「decision→reject→重激活」步骤：
   读 loopback 边 + LoopRound，按 §4 Reopen 子图 + round++ + round-scoped 清 DispatchRecord；
   达上限走逃生边 / stuck 通知。
5. **存储状态**：新增 `DecisionOutcome`、`LoopRound`；`DispatchRecord` PK 加 `round`（§7）。全部 append/幂等。
6. **环校验**：`ValidateNoCycle` / `WouldCreateCycle` **仅对 seq+conditional 边**求值（前向必须无环）；
   loopback 边单独校验：From 必须是 decision、To 必须是 From 的前向祖先、必须带 `When` 且 `MaxRounds≥1`。
7. **dispatch 文案**：重做轮的 @mention 注明「第 N 轮（评审打回）」，便于 agent 理解上下文（复用 findings block 机制）。

---

## 7. 数据模型 / 迁移增量（给 B1，细节 B1 定）

| 落点 | 增量 | 兼容 |
|---|---|---|
| 边表（plan dependencies） | `kind TEXT NOT NULL DEFAULT 'seq'` + `when TEXT NOT NULL DEFAULT ''` + `max_rounds INTEGER NOT NULL DEFAULT 0`（仅 loopback 用） | 旧行默认 seq/''/0 == 今天 |
| 新表 `pm_plan_decision_outcomes` | `(plan_id, task_id, round, outcome, decided_at)` PK(plan_id,task_id,round) | 新表，旧 plan 无行 |
| 新表 `pm_plan_loop_rounds` | `(plan_id, loopback_edge_id, round)` | 新表 |
| DispatchRecord 表 | PK 加 `round`（默认 1）；旧行 round=1 | round=1 == 今天单轮 |

- 迁移编号接最新（B1 落地前 `grep targetSchemaVersion` 核对、避免撞号——见 cycle 团队纪律
  [schema bump 连带改版本断言] 教训）。
- 所有新列/表**可加可空/有默认**，旧 plan 读出来等价于纯 DAG。

---

## 8. 向后兼容性分析（逐条对账）

| 维度 | 旧 plan（无新属性）表现 | 保证机制 |
|---|---|---|
| 边语义 | 全 `seq` | Kind 默认 seq；conditional/loopback 分支不触达 |
| 就绪 | `upstreamAllDone`(AND) | condOK 恒真、pruned 恒假 ⇒ 退化 |
| 完成 | 全 done 才 AllDone | 无 skipped 节点 ⇒ `{done,skipped}` == `{done}` |
| 环校验 | 禁任何环 | 无 loopback 边 ⇒ 校验集 == 全边集 == 今天 |
| 多轮 | 单轮 | round 恒 1；DispatchRecord/outcome 查询 round=1 |
| 失败 | 下游 blocked + 通知 | 不变（reject 与 discarded 正交，§4.3） |

→ **结论**：纯 DAG plan 在新引擎下行为逐字不变；新语义全部是「opt-in 扩展位」。B1 必须带「旧 plan 回归」测试证明。

---

## 9. redefine：cycle 控制流图（B2 按此 scaffold）

每个 feature 链由线性 `Dev→Review→Integrate` 升级为：

```
        ┌──────────────── loopback(when=reject, max=3) ───────────────┐
        ▼                                                             │
S0 → Dev ──seq──▶ Review ──seq──▶ Decision ──conditional(when=pass)──▶ Integrate ─▶ (集成闸)
                                     │
                                     └─conditional(when=reject_exhausted)──▶ 升级/人工兜底
```

- **Review** 跑 code review + §-1 gate（沿用 F1 §2.3 DoD）。
- **Decision**（可与 Review 融合）：产出 `pass` / `reject` / 上限后 `reject_exhausted`。
- `pass` → Integrate（F1 §2.4 的 origin `--contains` 合并校验护栏，即已合主干的 F3）。
- `reject`（round<max）→ loopback 重激活 `Dev`（§4.2），开新一轮。
- `reject_exhausted`（round==max）→ 逃生分支（人工/升级），不无限返工。
- doc-only feature 仍可省到单 `Dev`（F1 §4.2），无 Review/Decision/loopback。

**吸收的旧内容**：
- 原 **F1「Review 双结论 / 可重跑」** → 由 `decision` 节点的 `pass/reject` outcome + 多轮 Reopen 落地。
- 原 **F5「重开 Dev 机制」** → 由**有界 loopback**结构化取代（不再手动重开节点）。

---

## 10. 决策锁定 & 留给下游

**锁定**：
- 三原语：`decision` 节点 + `conditional` 边 + 有界 `loopback` 边；其余皆其组合。
- 回环必须有界（MaxRounds + 逃生边）；禁无界。
- 复用 `Completed→Reopened→Open` 做重做，零新 task 状态。
- 新增**派生**终态 `NodeSkipped`；AllDone = 全节点 ∈ {done, skipped}。
- 向后兼容是硬门：B1 带旧-plan 回归测试。

**交接**：
- **B1**：按 §5–§8 实现引擎 + 迁移 + 旧-plan 回归 + 新（条件路由/有界回环/skipped/AllDone）测试。
- **B2**：`scaffold_cycle_plan` 生成 §9 控制流图（Decision 节点 + conditional/loopback 边 + MaxRounds），
  非 doc-only feature 用回环链；owner 仍留空待 PD。
- **B3**：Decision outcome 自动判定（§-1 gate 绿+无 comment→pass，否则 reject；边界回落人工）；
  只产 outcome，不改本契约。B3-Review 重点：自动判定正确、边界走人工、不误放。

**留待 B1 细化的开放点**（非阻塞）：
- loopback 边 id 的稳定标识方式（用于 LoopRound 关联）。
- 「逃生边缺失」时是判 stuck-通知 还是 scaffold 强制必带——建议 scaffold 默认必连逃生边 + 引擎兜底 stuck 通知，双保险。
- `reject` 重做范围（仅 Dev 还是 Dev→Review→Decision 整链）默认整链；是否允许 scaffold 配置部分重做，B1 评估成本后定。
