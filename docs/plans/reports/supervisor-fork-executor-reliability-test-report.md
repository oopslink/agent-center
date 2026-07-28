# 测试报告 — Supervisor fork executor 可靠性修复

## 结论

✅ 通过。生产主故障已复现、定位并修复；同 task 幂等、不同 task 真并发、Pool 上限、并发 worktree 生命周期、center durable enqueue、Unix socket 控制边界和部署 binary 子进程链路均有自动化证据。最终全仓测试、race、lint、build、前端测试与 deployed smoke 全绿。

## 根因调查

### 生产证据

- 检查 worker 日志 `~/.agent-center/workers/worker-edb09a0c/logs/worker.log` 与 center SQLite `~/.agent-center/var/agent-center.db`：自 supervisor-driven 改动 `b27f5965` 后，共观察到 24 条 `agent.fork_executor` command，涉及 10 个 task。
- 最早 6 个失败 task 都是 Supervisor 先调用 `start_task`，在 15–23 秒后再调用 `fork_executor`；此时 task 已为 `running`。
- 旧 runtime 仍保留 auto-dispatch 时代的状态 allow-list，只允许 empty/open/reopened。它对 `running` 记录 “already running — not forking” 后返回 `nil`；worker 因此 ACK durable command，MCP 返回 accepted，但没有 executor、worktree 或执行结果。
- `task-005545d8` 被多个 supervisor 重复 enqueue 且每次都返回 accepted，但在 task 后续 reopen/redrive 前始终没有 executor；这排除了底层 child spawn 为主因，并锁定为 admission 状态机错配。

### 次生原因

1. `forkMu` 跨 `get_task → repo/worktree → start_task → process spawn` 全程持有，把同 agent 的不同 task 强制串行；慢 RPC/磁盘工作会造成 head-of-line blocking，并放大 5 秒 control delivery deadline 风险。
2. durable enqueue key 每次唯一，重复 supervisor 调用会产生多条 command；runtime 缺少按 task 的单飞/active-executor 去重，修主状态机后若不同时补幂等会转化为双 fork。
3. 直接移除全局锁会打开 worktree prune 竞态：task A 已 prepare、尚未进入 Pool 时，task B 可能把 A 当 orphan 删除。静态 live snapshot 仍会在等待 repo lock 时过期。

### 修复后的不变量

- `open/reopened`：`fork_executor` 自己 admission；`running` 且无 blocked reason：视为 Supervisor 已 admission，跳过重复 `start_task` 后继续 fork。
- blocked、terminal、unknown、`supervisor_inline`、foreign assignee：fail closed，并记录 `FORK-REJECTED`。
- 同 task：非阻塞 single-flight + task→live executor registry，重复 command 记录 `FORK-COALESCED`，不重复访问 center/prepare/spawn。
- 不同 task：不共享长临界区；并发上限由 `executor.Pool` 原子 reservation 唯一负责。
- worktree：preparing executor 在 Pool 可见前先注册；prune 使用动态 liveness callback，避免等待 repo lock 期间 snapshot 失效。
- enqueue：center 在返回 202 前验证 task 所有权；工具描述明确 open task 不应先手工 `start_task`，runtime 仍兼容旧 Supervisor 的 already-running 顺序。

## 计划项执行结果

| # | 用例 | 状态 | 证据 / 备注 |
|---|---|---|---|
| F1 | running、无 executor 时继续 fork | ✅ pass | `TestSpawnExecutor_AlreadyRunningWithoutExecutorForksWithoutRestart`：0 次 `start_task`、恰好 1 个 route/executor。 |
| F2 | blocked / terminal / unknown fail closed | ✅ pass | `TestSpawnExecutor_NonDispatchableStatesSkip` 的 5 个 subtest；另有 supervisor-inline gate 回归。 |
| F3 | 同 task 并发幂等 | ✅ pass | `TestSpawnExecutor_SameTaskConcurrentCallsCoalesce`、`SameTaskWithLiveExecutorCoalescesBeforeCenterRead`；channel barrier，无 sleep。 |
| F4 | 不同 task 真并发 | ✅ pass | `TestSpawnExecutor_DistinctTasksProceedConcurrently` 观测到 2 个同时 in-flight admission；50 轮压力通过。 |
| F5 | Pool cap 不突破 | ✅ pass | `TestPool_ConcurrentLaunchesRespectCap`：10 路争抢、max=3，恰好 3 成功/7 at-cap；与 F3–F4 一起 50 轮通过。 |
| F6 | Unix socket control 集成 | ✅ pass | `TestForkExecutorControl_AlreadyRunningTaskLaunchesThroughUnixSocket`：真实 server/client、真实 OS child；普通 20 轮 + `-race` 10 轮通过。 |
| F7 | 并发 worktree prune 安全 | ✅ pass | `TestSpawnExecutor_Repo_ConcurrentPruneKeepsPreparingSibling`：B 先捕获 callback，A 后注册 prepare，B 仍动态看见 A 为 live；50 轮通过。 |
| F8 | center enqueue 所有权/持久命令 | ✅ pass | 真 SQLite/AppService：自有 task 202 + 单 command；foreign task 403 + 0 command。 |
| F9 | MCP 契约 | ✅ pass | `TestForkExecutorTool_StatesAdmissionContract` 验证 admission、不要先 start、running compatibility 三段说明。 |
| F10 | 异常与清理 | ✅ pass | get/malformed/no caller/start decline/model reject/cap skew、repo prepare/source/launch failure均覆盖；prepared workspace 清理与结构化 block 回归通过。 |
| F11 | race | ✅ pass | `make test-race` = `go test -race -count=10 ./internal/agentruntime/...`，退出码 0；worker socket 集成另跑 `-race -count=10`。 |
| F12 | 全仓回归 | ✅ pass | `go test ./...`，99 packages，退出码 0；`web/pnpm test` 184 files / 1683 tests 全绿。 |
| F13 | 覆盖率 | ✅ pass | 当前 worktree diff statement coverage：199/219 = **90.9%**；profile `/tmp/agent-center-fork-cover-final.out`。核心函数见下表。 |
| F14 | deployed smoke | ✅ pass | 新增专项：fresh-built binary agent-runtime → 真 Unix socket → 同 binary executor child → real `true` runner → `output.json`；普通 10 轮、race 3 轮。仓库 `make smoke` 的 Playwright deployed pipeline 另有 1 条通过。 |

## 覆盖率

- 覆盖命令：`go test ./internal/agentruntime ./internal/admin/api ./internal/mcphost ./internal/workerdaemon -coverprofile=/tmp/agent-center-fork-cover-final.out -count=1`
- 当前 worktree diff statement coverage：**90.9%（199/219）**。计算范围包含当前 worktree 中同 package 的既有未提交改动，因此不是只挑本任务有利行的分母。
- 关键生产函数：`beginTaskFork` 100%、`endTaskFork` 100%、`trackPreparingExecutor` 90.9%、`launchExecutorNow` 92.0%、`executorIDLive` 100%、`SpawnExecutor` 87.5%。
- Go 原生 coverprofile 不输出数值 branch coverage；本次用显式分支用例代替：open、running、blocked、legacy running+blocked、completed、discarded、unknown、foreign assignee、same-task duplicate、live duplicate、at-cap、prepare/start/spawn failure 均有断言。

## 测试分层 (Layered Test Inventory)

| 层 | 计数 | 入口 |
|---|---:|---|
| Unit (in-package) | 3 packages / 49 top-level cases | `executor_runtime_test.go`（29）、`executor/pool_test.go`（9）、`mcphost/server_test.go`（11） |
| Integration with mocks | 3 packages / 20 top-level cases | `repo_workspace_test.go`（15）、`agent_tools_fork_executor_test.go`（4）、`fork_executor_control_integration_test.go`（1）；center/repo 为 controllable boundary，其余走真 SQLite/socket/process |
| **Deployed-binary smoke** | **2 cases** | `TestForkExecutorDeployedBinary_AlreadyRunningTask`（fork 专项）+ `make smoke` / `v22-deployed-pipeline.spec.ts`（既有全链路） |

补充全量回归：Go 99 packages；Web 184 files / 1683 tests。

## 异常路径

- [x] get_task error / malformed response
- [x] foreign assignee（center enqueue + runtime defense in depth）
- [x] blocked / terminal / inconsistent running+blocked_reason
- [x] start_task decline
- [x] worktree prepare/prune failure与清理
- [x] local Pool capacity race / center-local cap skew
- [x] executor launch failure
- [x] duplicate control request / live executor duplicate

## 发布门禁

- [x] `go test ./...`
- [x] `cd web && pnpm test`（184/184 files，1683/1683 tests）
- [x] `make test-race`（直接读取退出码，无管道）
- [x] `make lint`
- [x] `make build`
- [x] `make smoke`（1 Playwright deployed-binary case）
- [x] fork 专项 deployed binary smoke（10 次普通 + 3 次 race）
- [x] `git diff --check`

门禁过程中同时修复了 main 上已有的三类确定性 lint drift：Go 1.25.11 gofmt、`docs/release/`→`docs/releases/` allowlist 路径，以及两个 raw amber token / 一个 checkbox multi-pick。后者复用现有 `EntityMultiSelect` 并同步更新行为测试；均经过完整 Web test、lint 与 build。

## 出口标准核对

- [x] F1–F14 逐项通过，无 skip。
- [x] diff coverage ≥ 90%。
- [x] 全仓 Go / Web tests 0 failure。
- [x] race 0 finding。
- [x] deployed-smoke 计数 2（≥1）。
- [x] 无 schema/config/跨 BC 直连改动；Pool 仍是并发 cap 的单一来源。

## 结论

✅ 无保留通过。
