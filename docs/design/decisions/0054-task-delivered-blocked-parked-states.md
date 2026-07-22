# ADR-0054：Task 增加 `delivered` / `blocked` 两个非终态（park），修正 ADR-0046

- **状态**：Superseded
- **日期**：2026-07-17
- **来源**：issue I107（executor 生命周期错配）
- **取代说明**：2026-07-22 快修移除了 `delivered`、`deliver_task`、`rework_task`。工作完成统一通过
  `complete_task`（可携带结构化 delivery / review 信息）表达；外部验收由 DAG 下游 Review 节点和既有
  Decision/loopback 返工机制表达。`blocked` 保留为唯一 parked 状态。
- **关系**：**Amends ADR-0046**（Task 状态机 7→5 简化）。ADR-0046 无独立文件，其决定记录在
  `internal/projectmanager/task.go` 的 `TaskStatus` 注释与 `state_machine_adr46_test.go`（本 ADR 落地后
  更名为 `state_machine_adr54_test.go`）中。

## 1. 背景：一个错配，长出三个症状

**编排侧的现实是多段的**（交付 → 评审 → 验收 → 合并），**实现侧的模型却假设「executor 一跑完就该出终态」**。
这个错配本身是根因，下面三条是它的侧面：

### ① 缺「已交付、等外部验收」这个状态

任务交付完成但未经外部验收时，三个可选状态**没有一个是真的**：

| 可选 | 为什么是假的 |
|---|---|
| `running` | 不准——活干完了，没有任何 executor 在跑 |
| `completed` | **假绿**——没人验收过，但所有 complete 下游（issue rollup / plan advance / stage barrier）全部触发 |
| `blocked` | **假故障**——读起来像「卡住了，快来救火」，但其实什么问题都没有 |

**人只能拿最接近的错状态去近似一个不存在的对状态。** 这不是使用者的疏忽，是模型缺格。

### ② `block_task` 是打标记，不是熔断

ADR-0046 为了消除 T16 死锁类（「自动进入但无合法出口」），把 `blocked` 从状态降级为
**`running` 任务上的 `blocked_reason` 注解**。死锁是消除了，**熔断也一起没了**：

- **不停派发**：`block_task` 写 `blocked_reason`，但 `status` 仍是 `running`，于是它在所有
  「哪些状态可派发」的判据里依然合格。一旦被 re-drive，会 fork 一个**全新空上下文 executor**，
  而它的 frozen prompt 写着「从 main 切分支、从头实现」——**它不知道活儿已经在 origin 上，
  会重做一遍、建重复分支甚至覆盖**（历史事故：block → 立即自动重派 → 反复空烧）。
- **催判永不停**：催判器认 `status`，block 改不了 status ⇒ 见 ③。

### ③ 催判罐头理由预设了「判不了」

催判升级时，系统递给 agent 一句现成的、**只需点头**的理由：

> `block_task(reason="executor finished but delivery could not be judged — needs attention")`

**但 agent 判了、逐条跑了**——这句是**假话**。系统之所以只能给出这句，正是因为 ①：
没有「判过了、等外部验收」这一格，唯一提供的出口只能断言一个并未发生的失败。
**给一句假话当默认选项，还配上重复催促加压 = 批量生产假状态**——阻力最小的路径就是接受系统已经替你写好的那句话。

## 2. 决定

**恢复 7 态**（ADR-0046 的 5 态 + `delivered` + `blocked`），两个新态都是**非终态**：

```
open → running → delivered → completed
running/delivered → blocked → running
open/running/delivered/blocked → discarded（终态）
completed → reopened → open | running
```

| 状态 | 含义 | 终态？ | 可派发？ | 占 run slot？ |
|---|---|---|---|---|
| `delivered` | 活干完并交付，等**外部**验收（评审/验证/合并） | 否 | 否 | 否 |
| `blocked` | park 在一个 reason 上，需要人介入 | 否 | 否 | 否 |

### 2.1 `IsParked()` —— 唯一的「停」判据

新增 `TaskStatus.IsParked()`（= delivered ∨ blocked）与 `IsDispatchable()`（= ¬terminal ∧ ¬parked）。
**所有派发 / re-drive / 恢复路径问这一个具名判据**，不各自重新推导「哪些状态该停」的字面集合。
理由是历史教训：`taskReassigned` 漏看命名空间、point-recovery 漏看 `blocked_reason`——
**每个 P0 都是某一处判据漏了一个维度**。收敛到一个 seam，新增状态就不会被某个 gate 悄悄忘记。

### 2.2 `blocked_reason` 注解**保留**

`block_task` 照旧写 `blocked_reason` / `blocked_reason_type`，**外加**改 status。于是：

- 所有 **reason-keyed** 的消费者（overdue 提醒、`taskCancelEvidence`、UI）**一字未改**仍然工作；
- **status 才是真正停派发的那一半**。

这也是**向后兼容**的支点：升级前按 ADR-0046 形状 park 的存量行（`status=running` + reason）
**不做数据迁移**，`Unblock` 的「是否 blocked」判据仍然认 **reason 而非 status**，因此存量行照样能被恢复。
所有需要「找出 blocked 任务」的扫描都列 `{blocked, running}` 再按 reason 过滤，两种形状走同一个循环。

### 2.3 `delivered` 的两个出口

`complete_task`（验收通过）与 `rework_task`（验收驳回 → 回到 running，附驳回意见）。
两者共同保证 `delivered` 不是死胡同。

### 2.4 催判提示：**不给默认理由**

罐头理由删除。新提示列出三个出口（complete / deliver / block），**明确要求 agent 自己写 reason**，
并把 `deliver_task` 作为「判过了、等外部验收」这一先前无处可去的情形的诚实出口。

## 3. 命门（回归锁，都有测试钉住）

1. **两个新态绝不能是终态**——否则 `delivered` 就是假绿、parked 任务会被当成已结束的活儿 reap 掉，
   从所有 active 视图和恢复扫描中消失。钉：`TestTaskStates_ParkedIsNotTerminal`。
2. **不能重新引入 T16 死锁**——每个非终态必须有前向出口。钉：`TestTaskStates_NoDeadlockableState`
   （改为遍历整个 enum，而非 ADR-0046 那份手写清单）。
3. **恢复链必须同步**（「别只改停、不改别复活」）——`taskCancelEvidence`（point-recovery +
   boot self-reconcile 共用的 should-continue seam）必须认这两个新状态。**只停不改恢复 = 停了个寂寞。**
4. **`Start` 必须显式拒绝 parked**——`blocked` 的邻接表里合法地有一条 → running 边（那是 Unblock 的出口，
   也是无死锁的保证），**光靠邻接表拦不住 Start 走这条边**。钉：`TestADR54_StartRejectsParked`。
5. **默认路径不变**——普通任务的 complete / discard / start 行为未动。

## 4. 不做什么

- **不迁移存量行**（见 2.2）。方向上也已被证明有损：`0058_v291_task_state_simplify` 当年
  `UPDATE pm_tasks SET status='running' WHERE status='blocked'`，**pre-0058 的区分已被销毁，谁也捞不回来**。
  本 ADR 不假装能恢复。
- **不加 `delivery_summary` 列**：summary 记在 append-only 的 task action log（`TaskActionDelivered`）上，
  零 schema 变更。（与 I86 的 `delivery` 列正交：那个存 executor 的 git 证据，是旁路 sidecar。）
- **不治 frozen prompt / 死引用 / 采样进度日志**（I107 的另三个侧面，本 ADR 出范围）。

## 5. 影响面

- **DDL**：`pm_tasks.status` 是无 CHECK 的自由 TEXT 列，新状态值**不需要 DDL**。
  唯一需要改的是 0070 的两个**部分索引**（WHERE 子句字面写死了状态集），由 `0111` 重建。
- **node_status 派生**：`blocked` → `NodePaused`（**不是** `NodeBlocked`——那个词已被「上游依赖未满足」占用，
  复用会在派生层重演 ADR-0046 当年担心的命名冲突）；`delivered` → `NodeRunning`（DAG 视角下是未 settle 的在途工作）。
- **新 agent 工具**：`deliver_task` / `rework_task`。
