# W4a 实现说明 — live work_available → executor fork 触发

> 父设计：[agent-concurrent-execution.md](agent-concurrent-execution.md)（§3 唯一写入者 /
> §11 调用链）+ [W1](agent-concurrent-execution-w1-orchestrator-loop.md)（fork 入口 HandleWork）。
> 阶段二 issue-47fe2a78（I55）/ 任务 T608（cycle dev/v2.20.0）。
> 本文档记录 W4a：把 W1 留下、**生产无 producer** 的 executor fork 入口接上**真实 live 触发**——
> concurrency-enabled agent 收到 `agent.work_available` 时由 daemon **fork executor**，而非只 nudge 常驻 claude。

## 1. 背景 — 为什么并发此前没真生效

current main 逐行核实（见 issue-47fe2a78「证据」节）：

- **live 路径是 pull-nudge**：`agent.work_available` → `workAvailable()`（`agent_controller.go`）只
  relaunch 死会话或 `Inject(workAvailableNudge)` 唤醒**常驻 claude** 自己跑 MCP `start_task`。**无 fork**。
- **fork 入口无 producer**：`HandleWork`（`concurrent_exec.go`）唯一调用者＝`workViaExecutor`，其唯一调用者＝
  `work()`（处理 `cmdTypeAgentWork="agent.work"`）；而 `"agent.work"` 全代码库**无 producer**（F7 退役）→
  `HandleWork` 永不触发 → executor 槽恒 `active:0`。
- 区分：**中心任务层 ≤N**（W4c：常驻 claude 自管 N 个 running task）≠ **W1 executor 进程并发**（overlay 槽位，本特性目标）。

## 2. 范围（W4a）

- **live 触发**：`workAvailable()` 入口判 `exec != nil`（即 `concurrencyEnabled` agent，
  `maybeAttachExecutorEngine` 只为 opt-in、非 codex agent 挂 `executorEngine`）→ daemon 自己：
  1. 经 `get_task` agent-tool 拉 task 详情（title/description/model）；
  2. **代 `start_task`**（open→running）准入——中心在**同一事务**里执行 W4c ≤max_concurrent 槽位 cap +
     single-active 幂等；
  3. 准入成功才 **fork executor**（W1 `HandleWork` 链）。
- **与 nudge 路径互斥（防双跑，验收②）**：concurrency 分支 fork（或留队）后**短路 `return nil`**——
  **绝不再 `Inject` 那条 pull nudge**，常驻 claude 不会被叫去自跑同一 task，executor 与常驻会话不会双跑同一 task。
  `start_task` 是中心侧的准入闸：re-emit（task 已 running）或满 cap 的 agent 被干净拒绝→**不 fork**（task 留队，
  wake-sweep 再 emit，腾槽后下一拍准入）。
- **回写正确（验收③）**：`WorkItem.TaskRef` 带**规范 task_id**（非 T<n> org_ref），故 W2 `CenterWriteback`
  经 `Source.TaskRef` complete/block **正确的 task**。
- **默认（非并发）agent 零回归（验收④）**：`exec == nil` → 走原 nudge/relaunch 路径**字节级不变**。

## 3. 落点

| 文件 | 职责 |
|---|---|
| `internal/workerdaemon/concurrent_work_available.go` | **新增**：`forkOnWorkAvailable`（get_task→start_task 准入→fork，best-effort 非 wedge）+ `buildWorkItem`（task 详情→`WorkItem`，TaskRef=规范 task_id、TaskModel=task.model）+ `fetchCenterTask`/`startCenterTask`（get_task/start_task 经 `agentToolCaller`）+ `centerTaskDetail`/`goalTitle` |
| `internal/workerdaemon/agent_controller.go` | `workAvailable()`：取 `ma.exec`；`exec != nil` → `forkOnWorkAvailable` 后**短路 return**（在 nudge/relaunch 之前；互斥） |
| `internal/workerdaemon/concurrent_exec.go` | 抽出 `launchExecutor`（fork+drain+bookkeeping 共享尾）；`workViaExecutor` 改为构 `WorkItem`→`launchExecutor`（行为不变） |

## 4. 调用链

```
agent.work_available → workAvailable(pl)
  ma.exec == nil → 旧路径（sess==nil relaunch / 否则 coalesce+Inject(nudge)）  ← 默认 agent 零回归
  ma.exec != nil → forkOnWorkAvailable(agentID, taskID, ee); return nil       ← 互斥短路，不 nudge
       1. fetchCenterTask: CallAgentTool("get_task",{agent_id,task_id}) → centerTaskDetail
            status ∉ {open,reopened} → 跳过（廉价幂等预检；re-emit/已起不重复 fork）
       2. startCenterTask: CallAgentTool("start_task",{agent_id,task_id})   ← 准入闸（W4c ≤N cap + 幂等）
            err（满 cap / 已 running / 不可跑）→ 不 fork，留队
       3. launchExecutor(buildWorkItem(taskID, task)) → ee.engine.HandleWork
            ErrAtCapacity（reap-skew：中心已准入但本地槽未腾）→ 记日志；lease 回收 task→重派
            成功 → go drainExecutor（腾槽）+ hadWork/currentTaskID 记账
```

来源口径：`get_task` 投影（`agentTaskMap`）只含 title/description/model/org_ref，**无 chat 会话 id**——
而 task 本身就是回写目标（`complete_task`/`block_task` 原子发到 task 会话），故 `WorkItem.ChatID` 留空、
`TaskRef`＝规范 task_id 即足以正确收尾。`get_task` 在 `start_task` **之前**调用（precheck + 构 `WorkItem`），
但 `start_task` 才是权威闸（裸 status 读有 TOCTOU，故仅作廉价短路，真正幂等/cap 靠中心事务）。

## 5. 规约自检（conventions §15）

- **§0.4 AppService 唯一入口**：get_task/start_task 全经 `agentToolCaller`→`*AdminClient.CallAgentTool`
  （agent-tools transport，worker bearer + agent_id，中心 re-check requireAgentOnWorker），daemon **不直碰中心存储**。
- **§3 唯一写入者**：fork 后结果回写仍由 W2 `CenterWriteback`（互斥串行）写——W4a 不新增写入方。
- **§17 错误不吞**：`forkOnWorkAvailable` 每个失败分支都 `c.log` 显式记录（get_task/start_task/fork 失败、
  无 ToolCaller、空 task_id、已 running、准入后 fork 失败），且**永不 wedge**——caller 始终 ack work_available
  使单游标控制环不被堵。
- **§4 零 LLM SDK / §10 单二进制 / §9 无 SQL·FK**：无新依赖、无新 binary、无 schema 变更。

## 6. 已知边界 / deferred（W4a 诚实披露）

- **常驻 claude 自 pull 仍在系统提示里**：W4a 只接 work_available 触发点。concurrency agent 的常驻 claude
  系统提示仍含 list_my_tasks-on-boot pull loop；本特性靠 `start_task` 准入闸做中心侧互斥（谁先 start 谁拿，
  另一方 start 必失败），从触发点根除「nudge→双跑」。彻底让常驻 claude 退成纯监工（不自 pull）是后续接线。
- **reap-skew 准入后 fork 失败**：中心 cap==pool Max==N，但已完成 executor 的本地槽 reap 略滞后于中心
  complete 的极小窗口里，`start_task` 准入而本地 `HandleWork` 返回 `ErrAtCapacity`：**不会双跑**（无 executor 起），
  task 由执行 lease（5h）回收→open→wake-sweep 重派。已显式记日志。真正的「准入后立即重试/回滚」留后续。
- **TaskModel plumb 已接，judge 仍 nil**：`buildWorkItem` 带 `task.model` 硬覆盖进 fork 路径（§5 链第一档生效）；
  「LLM 判难度→allowed_models」的 DifficultyJudge 仍 nil（W1/W4 既有 deferred，非本特性）。

## 7. 覆盖率（testing.md §1，新代码 diff ≥90% 硬门）

`go test ./internal/workerdaemon/ -run 'ForkOnWorkAvailable|WorkAvailable|BuildWorkItem' -coverprofile` →
`go tool cover -func`，新增/改动生产函数逐个：

| 函数 | 文件 | 覆盖 |
|---|---|---|
| `forkOnWorkAvailable` | concurrent_work_available.go | **100.0%** |
| `buildWorkItem` | concurrent_work_available.go | **100.0%** |
| `goalTitle` | concurrent_work_available.go | **100.0%** |
| `fetchCenterTask` | concurrent_work_available.go | **100.0%** |
| `startCenterTask` | concurrent_work_available.go | **100.0%** |
| `launchExecutor`（抽出共享尾） | concurrent_exec.go | **100.0%** |
| `workAvailable` 新并发分支 | agent_controller.go | 端到端 `TestWorkAvailable_ConcurrencyAgentForksAndDoesNotNudge` 覆盖 |

→ **新代码 diff 覆盖 ≈100%**（远超 90% 门）。测试（`concurrent_work_available_test.go`）覆盖：准入即 fork（get_task
先于 start_task、routing 绑规范 task_id、currentTaskID 记账）、start_task 拒绝不 fork、已 running 跳过（不调 start_task）、
get_task 错误/响应畸形跳过、无 ToolCaller 留队、空 task_id 短路、准入后 fork 失败（reap-skew）不 panic、
**端到端互斥**（concurrency agent 不 nudge 常驻会话）。默认 agent 零回归由既有
`TestAgentController_WorkAvailable_NudgesOnceCoalesced` 守门。
