# W1 实现说明 — 监工主循环接线 + 真 fork executor

> 父设计：[agent-concurrent-execution.md](agent-concurrent-execution.md)（§4 拓扑 / §5 模型
> 路由 / §8 一致性路由 / §11.1 主循环）。阶段二 issue-b8687f2a「让并发真正在生产跑」。
> 本文档记录 W1（cycle v2.18.0 plan-4751e59e / 任务 T536）的实现层落点：把 v2.17.0 的
> 地基（F1 进程模型 / F2 文件协议 / F3 模型路由 / F4 一致性路由）接到**生产监工主循环**，
> 收到工作时**真 fork executor**。

## 1. 范围（W1，PD 裁定）

- **真 fork**：daemon 的 per-agent work 路径串起 F4 routing → F3 modelrouter → F2 input →
  F1 Pool.Launch，真起 ≤N 个 executor 独立进程组。
- **并发上限**：`max_concurrent_tasks`（profile，默认 3）生效；超额 `ErrAtCapacity` 走重试式排队，不硬起。
- **opt-in 且可回退**（决策 2 = A）：仅当 agent profile `MaxConcurrentTasks>0 且 AllowedModels 非空`
  才走 executor 路径；否则保持既有单 claude inject，默认行为字节不变。
- **真 runner**（决策 3）：executor 跑**真 claude**（no-mcp + F3 选中模型），不占位。

**不在 W1**（下游 feature）：W2 Writeback 连中心回写（发 chat / 写记忆 / 更新中心 task）；
W3 adopt 孤儿看门狗 + 崩溃恢复产线接线。两者的 seam 已在 W1 留好（见 §5）。

## 2. worktree 可选（PD 裁定 B，F1 Pool 契约更新）

产线 agent **没有 per-agent 源码 git 仓库**概念（cwd = `<home>/tasks` 状态目录）。故把 F1
设计里「每 executor 一个 git worktree」**收敛为**：

> **executor workspace 默认 = 普通隔离目录**（进程组 + env allowlist + 路径 containment 即真
> 隔离）；**仅当配置了「源仓库 + base ref」时才上 git worktree**。

实现：`executor.PoolConfig.Worktrees` / `BaseRef` 改为**可选**（二者必须同设或同空，半配置报错）。
`provisionAndSpawn`：配了 → `AddNewBranch`；否则 → `os.MkdirAll(workspace)`。选项 A（profile/中心
加 repo+base 字段、跨 BC）留作**后续**——等真有「编辑代码仓库的 executor」需求再加。W1 产线走普通目录。

## 3. 落点

| 文件 | 职责 |
|---|---|
| `internal/workerdaemon/executor/pool.go` | F1 Pool worktree 步骤**可选**（§2）；新增 plain-dir 路径 + 校验（同设/同空）|
| `internal/workerdaemon/orchestrator/runner.go` | `RunnerCmdBuilder` 端口 + `ClaudeRunnerBuilder`：一次性 no-mcp claude argv（`-p <goal> --model <m> --append-system-prompt <executor framing>` + auth/bypass flags，**无** --mcp-config / 无 stream-json）|
| `internal/workerdaemon/orchestrator/engine.go` | `Engine.HandleWork`：F4 Route/Register → F3 ResolveExecutorModel → 建 runner+Input → F1 Pool.Launch → F4 Merge |
| `internal/workerdaemon/orchestrator/idminter.go` | 产线 `IDMinter`（idgen `<prefix>-<8hex>`，path-safe）|
| `internal/workerdaemon/concurrent_exec.go` | daemon 接线：`concurrencyEnabled` 闸门、`buildExecutorEngine`（plain-dir Pool + nil-judge Router + nil-Writeback Monitor）、`workViaExecutor`、`drainExecutor`（reap+释放槽）|
| `internal/workerdaemon/agent_controller.go` | `managedAgent.exec` 字段；`reconcileRunning` → `maybeAttachExecutorEngine`（opt-in 时建/挂引擎）；`work()` → exec 非空走 `workViaExecutor`，否则既有 inject |

## 4. 调用链（§11.1 step a–d 落地）

```
中心 agent.work → ControlLoop → AgentController.work(pl)
  ma.exec == nil → 既有 sess.Inject(brief)（默认/未开并发）
  ma.exec != nil → workViaExecutor:
     Engine.HandleWork(WorkItem{TaskRef, Goal{Title=brief首行, Desc=brief}}):
       1. F4  routing.Route(signal) → 命中 problem / 新建 Register
       2. F3  router.ResolveExecutorModel(task.model, goal, cfg) → 选中模型（§5 优先级链）
       3. 建 runner argv（真 claude no-mcp + 选中模型）+ F2 Input(model, goal, problem, source)
       4. F1  Pool.Launch → 真 fork `worker executor`（Setpgid 独立进程组，env 无 mcp/凭据）
              满 → ErrAtCapacity → workViaExecutor 返回可重试错误（控制环下个 tick 重拉=排队不硬起）
       5. F4  routing.Merge(problem, signal, executorID)
     go drainExecutor(monitor, handle)  // executor 退出 → 释放并发槽（W1 用 nil-Writeback Monitor）
```

## 5. 下游 seam（W2/W3 接这里）

- **W2 Writeback**：`buildExecutorEngine` 现给 Monitor 传 **nil Writeback**——`AwaitCompletion` 仍
  reap + 释放槽 + 清目录，但不回写中心。W2 只需传入一个真 `executor.Writeback` 实现（连中心：发结果
  到来源 chat / 写记忆 / 更新 task complete·block），`Monitor.Finalize` 的 Report→Release→Teardown
  次序保证回写在拆目录前发生、回写失败保留目录不丢。
- **W3 adopt 孤儿看门狗 / 崩溃恢复**：`buildExecutorEngine` 现未挂 `Watchdog` / `Reconciler`。W3 接
  `executor.NewReconciler` + `Watchdog` + 启动时 `Monitor.Recover`（扫 executors/ + routing.json 重建、
  Adopt 活孤儿）+ 定时 `Monitor.Sweep`（stalled kill）。F5 的 Pool.Adopt / Reconcile 已就绪。

## 6. 已知边界 / deferred（W1 诚实披露）

- **LLM 难度判断未接**：`Router` 用 **nil judge**，§5 链当前解析为 `task.model → default_executor_model`。
  「按难度从 allowed_models 选模」需把监工的 claude 接成 `DifficultyJudge`（经 supervisor socket 问一次）
  ——留作后续；`allowed_models` 当前仅作 opt-in 信号。优先级链本身（硬覆盖 + 兜底）已生效。
- **task.model / 结构化 goal / chat ref 未全程接通**：`workPayload` 仅有 `Brief`，故 Goal.Title 取 brief
  首行、Desc 取 brief，TaskModel 空（走兜底），ChatID/IssueRef 空（routing 以 TaskRef 为键，每 task 独立
  problem）。从中心结构化下发 goal+task.model 是后续 plumbing，不阻塞 W1 fork。
- **排队是重试式**：满载时 work 命令返回错误 → 控制环 cumulative-ack 下个 tick 重拉，槽空即起。满足
  「不硬起」；更精细的内存队列留后续。
- **停机未取消 drain / executor**：opt-out/停机时已 fork 的 executor 成孤儿（W3 崩溃恢复覆盖）。

## 7. 测试

| 范围 | 文件 | 结果 |
|---|---|---|
| F1 Pool worktree 可选（plain-dir launch、校验同设/同空）| executor/pool_test.go | PASS |
| 真 runner argv（无 mcp / 无 stream-json / 有 --model / 安全 flags / 系统提示禁中心）| orchestrator/runner_test.go | PASS |
| Engine 串链（F4 路由+合并、F3 兜底/硬覆盖、F2 input 落盘、F1 真 fork、ErrAtCapacity 冒泡、同 chat 归并）| orchestrator/engine_test.go | PASS |
| daemon 接线（concurrencyEnabled 闸门、buildExecutorEngine 真 fork→drain 释放槽、workViaExecutor 落 routing+state、at-capacity 可重试）| workerdaemon/concurrent_exec_test.go | PASS |
| 既有 workerdaemon 全量回归（agent_controller / reconcile / work）| 既有 *_test.go | PASS（无回归）|

集成/真 fork 用例用真 `true` 二进制 harness（缺则 t.Skip），真 claude 端到端属 Accept。

## 8. 验收对应（W1）

- ✅ 一个 agent 收到多个可并行 task 能真 fork ≤N 个 executor 独立进程组：`workViaExecutor` →
  `Engine.HandleWork` → `Pool.Launch`（Setpgid），并发上限 + ErrAtCapacity 排队（测试覆盖）。
- ✅ executor 环境无 mcp / 凭据：F1 `BuildExecutorEnv`（default-deny + 中心凭据 scrub）+ runner **无**
  `--mcp-config`，系统提示亦显式禁止访问中心。
- ✅ 优先级链选模生效：F3 `ResolveExecutorModel`（task.model 硬覆盖 → default 兜底；LLM 判断 deferred §6）。
- ✅ opt-in 可回退：未开并发的 agent 字节不变走原 inject。
