# W3 实现说明 — adopt 孤儿看门狗 + 崩溃恢复产线接线

> 父设计：[agent-concurrent-execution.md](agent-concurrent-execution.md)（§9 完成信号/watchdog、§12 崩溃恢复）。
> 阶段二 issue-b8687f2a（让并发真正在生产跑）。本文档记录 W3（cycle v2.18.0 plan-4751e59e /
> 任务 T546）把 F5 的崩溃恢复 / watchdog **地基接到生产监工生命周期**的落点。
> 依赖 W1（监工主循环 + 真 fork，已集成 dev/v2.18.0）。W2（writeback 连中心）并行推进——
> 本接线沿用 Monitor 的 `Writeback` port（W1/W3 暂 nil），W2 注入真实回写即生效，无需改本代码。

## 1. 起点与缺口

phase-1 已交付 F5 **库**（`executor/{watchdog,recovery,monitor,completion}.go`：`Watchdog`/
`Reconciler`/`Tracker`/`Monitor.{Sweep,Recover,Finalize}`/`Classify`），但 **W1 的生产接线
（`concurrent_exec.go`）只用了 reap-to-free-slot**：Monitor 建时**无** Watchdog / Reconciler /
Tracker，**无**周期 Sweep，**无** Recover 调用。三个生产缺口：

1. **崩溃恢复未接**：监工（daemon）重启后没有扫 `executors/` 重建在管 executor。
2. **watchdog 未跑**：`Sweep` 已实现但无人周期调用，stalled 不会被 kill。
3. **孤儿无人看**：`Sweep` 只遍历 `pool.Handles()`（本进程 spawn 的 handle）；adopt 回来的孤儿是
   **无 handle 的占位**（`Pool.Adopt`），`Sweep` 跳过它，且**无法 `Wait`**（重启后已被 reparent，
   非本进程子进程）——它的 stall 与完成都没人观测。

## 2. 交付

### A. executor 包补强（可单测的单元）

| 改动 | 作用 |
|---|---|
| `Pool.Tracker`（可选）| `Launch` 成功 fork 后写 `orchestrator.json`（pid + runner_cmd），供重启后探活/重领（§12）。写失败 → kill 刚起的进程并 fail launch（不泄漏不可恢复的孤儿）。nil = 不写（保持 W1 行为）|
| `Monitor.Liveness`（可选，默认 `SignalLiveness`）+ `Monitor.CheckOrphan(ctx,id,pid)` | **孤儿一次 watchdog+完成 tick**：探活→gone 则 harvest+`Classify`+`Finalize`（done）；alive+stalled 则按 pid graceful-kill 并 finalize 为 **Failed/非重试**（§9「按失败处理」，避免把证明会 hang 的活儿自动重排）；alive+fresh 则 Running（not done）|
| `recoveredHandle(id,pid,sig)` + `Monitor.killSig`（注入式 group signaler）| 给孤儿建 **pid-only handle** 供 GracefulKill；signaler 注入使「杀孤儿」可测而不真发信号 |

`Sweep`（本进程 handle 的 stall）已存在；`CheckOrphan` 补上**孤儿**这条 `Sweep` 覆盖不到的线。

### B. 产线接线（`workerdaemon`）

1. **`buildExecutorEngine`**：给 Pool 接 `Tracker`，给 Monitor 接 `Watchdog`（默认 5m stall / 10s grace）
   + `Reconciler`（`SignalLiveness`）。Writeback 仍 nil（W2 注入）。
2. **`executorEngine.orphans`**（`id→pid`，带锁）：adopt 回来的孤儿集合，watchdog tick 轮询至终态。
3. **`recoverExecutors`**：驱动 `Monitor.Recover`（纯文件、不 re-spawn → 不重复拉起）——终态/崩溃孤儿
   `Finalize`（结果不丢），存活孤儿 re-adopt 进 pool 并登记 `orphans` 供轮询。
4. **`maybeRunExecutorWatchdog(ctx,now)`**：仿 `maybeRunGC` 的 **tick 驱动 + 节流（30s）+ 锁内快照**
   模型（无后台 goroutine），由 `OnTick` 调用。每 agent：①`Sweep` 杀 stalled 的本进程 handle；
   ②`CheckOrphan` 轮询每个孤儿，done 则出列。
5. **恢复时机**：`maybeAttachExecutorEngine` **每进程每 agent 仅首次** attach 跑 `recoverExecutors`
   （`recoveredExec` guard）。这是关键正确性点——见 §3。

## 3. 关键正确性：恢复为何「每进程每 agent 一次」

executor 进程是 **daemon 的子进程**。两种「重建引擎」要区分：

- **daemon 重启**：旧进程的 executor 被 reparent 成真·孤儿 → 必须扫 `executors/` re-adopt。
- **进程内 reconcile**（版本变更 / claude 会话重启）：executor 仍是**本进程子进程**，由
  `drainExecutor` 在 `Wait`。此时若再扫目录把它们当孤儿 re-adopt + 轮询，会与 `drainExecutor`
  **双重 finalize**（双回写 / 双 release）。

`maybeAttachExecutorEngine` 在**首次** attach（即本进程第一次接管该 agent 并发 = 恰好重启后）才跑恢复，
`recoveredExec` guard 保证只此一次；之后的进程内重建跳过恢复。daemon 重启 → 新进程 guard 空 → 恢复；
进程内 reconcile → guard 已置 → 跳过。两路天然分开，杜绝双重 finalize。

## 4. stalled → kill → 回写（两条线一致）

- **本进程 handle**：`Sweep` 检出 stalled → `GracefulKill`（SIGTERM→grace→SIGKILL）→ 进程退出 →
  `drainExecutor` 的 `AwaitCompletion` `Wait` 到信号错误 → `Classify` 非零退出 → **Failed** → `Finalize` 回写。
- **孤儿**（无 handle 不能 Wait）：`CheckOrphan` 检出 stalled → 按 pid `GracefulKill` → **直接** finalize 为
  **Failed/stalled**（不等下一 tick 把 kill 误判成 retryable crash 而自动重排一个已证明会 hang 的活儿）。

两条线最终都走 `Monitor.Finalize` —— W2 的 `Writeback` 一接，stalled 即按失败回写中心 task + 发 chat。

## 5. 规约自检（conventions §15）

- §3 单点写：恢复/watchdog/finalize 全在监工单协调者内；`Recover` 纯文件、**不 re-spawn**（不重复拉起）。
- §12 命名：orchestrator / executor / orphan 一致；`orchestrator.json` 为监工私有记录（executor 不读写）。
- §16 reason+message：stalled 完成带 `ErrorDetail{Kind:"stalled",Message:…}`；各 err 分支带上下文。
- §17 错误不吞：Tracker 写失败 → kill+fail launch（不泄漏不可恢复孤儿）；恢复 best-effort 但
  无 pid 记录的存活孤儿**显式 log** 出来（不静默丢）。
- §4 零 LLM SDK / §10 单二进制：无新依赖、无新 binary。

未触发：§8 BlobStore（仅小 JSON）、§9.4 schema 变更（无 SQL）。

## 6. 测试 / 报告

### executor 包（`orphan_watchdog_test.go` / `pool_tracker_test.go`）
| 用例 | 结果 |
|---|---|
| CheckOrphan alive+fresh → Running，零信号零回写 | PASS |
| CheckOrphan alive+stalled → SIGTERM+SIGKILL、Failed/非重试/stalled、回写、拆目录 | PASS |
| CheckOrphan gone+success → Succeeded、拆目录 | PASS |
| CheckOrphan gone+running 无 output → Crashed/retryable、**保留**目录 | PASS |
| CheckOrphan alive+无 status → 不判 stalled（无时间戳不可证）| PASS |
| CheckOrphan pid<=0 → 报错 | PASS |
| Pool.Launch 写可读 Record（pid + runner_cmd + spawned_at）| PASS |

### workerdaemon 接线（`recovery_watchdog_test.go`，真子进程 + 真 SignalLiveness）
| 用例 | 结果 |
|---|---|
| recoverExecutors：存活孤儿 re-adopt（进 orphans + 占 pool 槽）、终态孤儿 finalize（拆目录）、不 re-spawn | PASS |
| watchdog tick 轮询孤儿至完成：alive 留、进程退出后 finalize 并出列、释放槽 | PASS |
| 恢复每进程每 agent 仅首次：二次 attach 不再扫（不双重 finalize）| PASS |

## 7. 出口标准核对

- [x] 杀监工重启后在管 executor 状态重建正确（recover adopt/finalize 用例）
- [x] adopt 孤儿被 watchdog 监控（CheckOrphan + tick 轮询用例）
- [x] stalled 被 kill 并回写（两条线均 finalize；W2 注入 Writeback 即真回写）
- [x] 重启恢复不丢、不重复拉起（Recover 纯文件不 re-spawn；首次恢复 guard 防双重 finalize）
- [x] `go build ./...` / `go vet` / `gofmt`(go1.25.11) 干净；full `go test ./...` 绿

## 8. 结论 & follow-up

W3 验收达成：崩溃恢复 + 孤儿 watchdog + stalled 处理已接进生产监工生命周期（tick 驱动，零后台 goroutine）。
**Follow-up（不在本范围）**：真实中心回写由 W2 注入 `Writeback`；crash 自动重试的重排策略；
孤儿轮询粒度/节流随生产负载调优。
