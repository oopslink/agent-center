# F5 实现说明 — 完成信号 / watchdog / 崩溃恢复（监工侧编排）

> 父设计：[agent-concurrent-execution.md](agent-concurrent-execution.md)（§9 完成信号与
> watchdog / §11.2 主流程 / §12 崩溃恢复）。
> 本文档记录 F5（cycle v2.17.0 plan-9c606650 / 任务 T525）的**实现层**落点，对应包
> `internal/workerdaemon/executor`。F5 是**监工侧的生命周期编排**：它 *驱动* F1 的 `Pool`、
> *消费* F2 的文件协议，把「检测完成 / 看门狗 / 崩溃重建」串成监工主循环的事件 (3)
> 「executor 退出 / status 变更」处理与重启兜底。

## 1. 范围

F5 只做**监工侧的判定与编排逻辑**，全部可纯测：

- **双完成信号判定**（§9）：把进程退出码、output.json、status 三个事实合并成唯一 Outcome。
- **watchdog**（§9）：`last_progress_at` 超时 → 判 stalled → graceful kill（SIGTERM→宽限→SIGKILL）。
- **崩溃恢复**（§12）：监工重启后扫 `executors/` 重建在管 executor，**不丢、不重复拉起**。
- **统一回写编排**（§3「唯一写入者」/ §11.2 step g/h）：决定*何时*回写、回写后拆 worktree、
  释放并发槽。

**不在本范围**：真正连中心的回写实现（发 chat / 写记忆 / 更新 task）—— 本包按设计**不连任何
中心 DB / AppService**，故回写是一个 `Writeback` **端口**，由连中心的监工实现（F1 主循环接线处）。
模型路由（F3）、一致性路由 routing.json（F4）亦不在此。

## 2. 双完成信号（`completion.go`，design §9）

监工**绝不信任单一信号**。`Classify(CompletionFacts) Completion` 是一个**全函数**纯判定，
覆盖两条取证路径——live（观察到退出）与 recovery（孤儿 + 探活）：

| 事实 | 判定 | Outcome |
|---|---|---|
| exit==0 且 output.json 存在(success) | 成功 | `Succeeded` |
| exit==0 且 output.json 显式 success=false | 信任显式失败 | `Failed` |
| exit==0 但无 output.json | 干净退出却没交结果 = 异常 | `Crashed`（可重试）|
| exit!=0 | 明确失败，详情取 status.error→output.error→exit err | `Failed` |
| 孤儿仍活（探 pid）| 不是完成，待**重新接管** | `Running` |
| 孤儿已没、output 成功 | 在停机期间已完成 | `Succeeded` |
| 孤儿已没、status=failed / output 失败 | 失败 | `Failed` |
| 孤儿已没、status=done 但无 output | 撕裂的终态写 | `Crashed`（可重试）|
| **孤儿已没、status 仍 running** | **§9 核心崩溃情形** | `Crashed`（可重试）|

`Completion.Retryable == (Kind==Crashed)`。错误详情经 `resolveError` 取最具体者，且永远带
human-readable message（规约 §16）。

## 3. watchdog（`watchdog.go`，design §9）

executor 每次流式 progress 会刷 `status.last_progress_at`（run.go）。`Watchdog`：

- `Check(status, now) StallVerdict`：**仅** `state=running` 可判 stalled；`now-last_progress_at >
  StallTimeout`（默认 5min）即 stalled。终态 status 永不被判 stalled（幂等）。
- `GracefulKill(ctx, handle)`：SIGTERM 进程组 → 宽限窗（默认 10s，可被 ctx 取消提前结束）→
  SIGKILL。最终 SIGKILL **容忍 ESRCH**（宽限期内自己退了正是期望终态）。

watchdog **不**决定 Outcome——它只结束卡死进程；被 kill 后进程退出，再走正常完成路径
（`Monitor.AwaitCompletion`）判为失败（§9「按失败处理」）。判定单一来源仍在 `completion.go`。

## 4. 崩溃恢复（`recovery.go`，design §12）

「文件即 durable 状态」。重建需要一个 F2 executor 侧文件**不带**的事实——pid，用来探活
（§12「进程是否活」）。pid 是**监工侧**关心的（它 spawn、它知道 pid），故 F5 持久化到一个
**监工私有**记录 `orchestrator.json`，与 executor 写的协议文件并列但区分（executor 从不读写它）。

- `Record{executor_id, pid, spawned_at, base_ref?, runner_cmd?}` + `Tracker`（原子写/读，
  复用本包 `writeJSONAtomic`/`readJSON`）。`base_ref`/`runner_cmd` 留够信息可**重拉**可重试崩溃。
- `LivenessProbe`（默认 `SignalLiveness`：signal 0，nil/EPERM=活、ESRCH=死）。注入可测。
- `Reconciler.Reconcile()`：`FileExchange.Scan()` 列每个 executor 目录 →（按 Record 探活）→
  `Classify` → 每个目录**恰好一条** `Reconciled`。**无任何副作用**（不 spawn / 不 kill / 不回写），
  故**不可能重复拉起**；监工据返回列表驱动（终态/崩溃 → finalize，活着 → 重新接管）。

**不丢**：`Scan` 列全部目录（含损坏的也上报，§17）→ 每目录一条。**不重复**：纯重建，按 id 唯一。

## 5. 监工生命周期引擎（`monitor.go`）

`Monitor` 把上面三块 + `Pool` + `Writeback` 端口接成监工的 executor 生命周期大脑：

| 方法 | 对应主循环 | 行为 |
|---|---|---|
| `AwaitCompletion(ctx, h)` | 事件(3) 本进程 spawn 的 executor 退出 | `Wait` 收割 → harvest output/status → `Classify`（live）→ `Finalize` |
| `Sweep(ctx)` | watchdog tick | 遍历 pool 在管 handle，读 status，stalled 的 `GracefulKill`；返回被 kill 的 id |
| `Recover(ctx)` | 重启兜底 | `Reconcile` → 终态/崩溃 `Finalize`（结果不随重启丢失），活着 `Pool.Adopt` 重新接管（恢复并发计数）|
| `Finalize(ctx, c)` | step g/h | **唯一回写**：`Writeback.Report`（唯一中心写）→ 释放并发槽 → 终态拆 worktree+删目录；**可重试崩溃保留**目录供重拉（§7「清理或保留」）|

**回写失败不丢**：`Finalize` 先 `Report` 成功才丢 durable 状态——回写失败则保留目录、不释放槽，
留给重试。`Running` 是 no-op（不是完成）。

`Pool.Adopt(id)`（加在 `pool.go`）：为重启后仍活的 reparent executor **不 spawn** 占一个并发槽
（handle-less reservation，`Handles()` 跳过、`Release` 正常释放），让 `max_concurrent_tasks`
重启后仍准确。

**已知边界**（留作后续）：reparent 的孤儿没有可 `Wait` 的 handle，`Sweep` 的看门狗只覆盖本进程
spawn 的在管 handle；对 adopt 的孤儿改用轮询 status/output + 按 Record.pid 信号——本期未做。

## 6. 验收对应

| 验收项 | 实现 | 测试 |
|---|---|---|
| 三种完成判定正确 | `completion.go` `Classify` | `completion_test.go`（表驱动覆盖全部 §9 分支）+ `monitor_test.go` `AwaitCompletion_{Success,Failure,Crash}`（真实进程退出码）|
| stalled 被 kill | `watchdog.go` + `Monitor.Sweep` | `watchdog_test.go`（Check/GracefulKill 信号序列）+ `monitor_test.go` `Sweep_KillsOnlyStalled` |
| 重启重建不丢不重复 | `recovery.go` + `Monitor.Recover` | `recovery_test.go` `RebuildsEveryDirOnce` + `monitor_test.go` `Recover_FinalizesAndAdopts` |

包级单测行覆盖率 ≥ 90%（本包整体 93.1%）。
