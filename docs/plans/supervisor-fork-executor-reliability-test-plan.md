# 测试计划 — Supervisor fork executor 可靠性修复

## 范围

- 被测对象：`fork_executor` MCP → center durable command → worker/agent-runtime control delivery → `LocalRuntime.SpawnExecutor` → executor pool/process/workspace。
- 改动摘要：修复 Supervisor 已先 `start_task` 时 `running` 被静默拒绝的问题；把全 agent 的 fork 全局串行锁收窄为按 task 去重；保持 pool 的原子并发上限；保护并发准备中的 worktree 不被 orphan prune；强化工具契约说明。
- 不在范围：更改 task/executor 持久化 schema、重做 worker 的全局有序 control stream、改变 executor 结果判定与回写协议。

## 前置条件

- 环境：本地 Go toolchain、SQLite、git、POSIX 子进程与 Unix socket；不访问外部服务。
- 数据：临时 SQLite、临时 agent home、可控 ToolCaller/RepoMaterializer、测试 runner；deployed smoke 使用本仓库实际构建 binary。
- 基线：`go test ./internal/agentruntime/... ./internal/workerdaemon/... ./internal/admin/api/...` 通过。

## 用例清单

| # | 类型 | 目标 | 输入 | 预期 | 异常 mock |
|---|---|---|---|---|---|
| F1 | unit | 复现并锁定主故障 | 自有 task=`running`、无 blocked reason、无 live executor | 跳过重复 `start_task`，仍创建且返回 executor | 无 |
| F2 | unit | 保持停止态 fail-closed | terminal / parked / legacy `running+blocked_reason` | 不 start、不 fork | 状态不一致 |
| F3 | unit | 同 task 并发调用幂等 | 两个同时到达的相同 task fork | 只有一个 admission/prepare/launch，另一个显式 coalesce | 用 channel 卡住 center 调用 |
| F4 | unit | 不同 task 真正并发 | 两个不同 task 同时 fork，pool cap=2 | 两条 admission/launch 可重叠，均成功；不再全局串行 | 用 barrier 同步 center 调用 |
| F5 | unit/stress | 本地 cap 不突破 | 多个不同 task 并发竞争 cap=N | active/launch 成功数始终 ≤N，无重复 executor | 同步释放 runner |
| F6 | integration | 真实 control transport 走通已 running 路径 | Unix socket agentcontrol server/client 投递 `agent.fork_executor` | Deliver 成功，runtime 创建真实子进程 executor | 测试 ToolCaller，仅 center RPC 为 double |
| F7 | integration | 并发 worktree 生命周期安全 | 同 repo 两个 task 并发 prepare/prune/launch | 准备中的 worktree 被视为 live，不被另一请求 prune；每 task 独立 branch/workspace | 可控 RepoMaterializer barrier |
| F8 | integration | center enqueue 所有权与命令契约 | 自有/非自有 task 调 `fork_executor` | 自有任务 202+单条 durable command；非自有任务拒绝且不入队 | 无，真 SQLite/AppService |
| F9 | contract | Supervisor 工具说明与状态机一致 | MCP tool metadata/system prompt | 明确 fork 自带 admission、无需先 `start_task`，且实际运行兼容已 start | 无 |
| F10 | failure | center/workspace/launch 异常不泄漏 | get/start/prepare/launch error | 无双 fork、无超 cap、prepared workspace 被清理、不可恢复失败显式 block | 分别注入各层错误 |
| F11 | race | 并发状态无 data race | F3–F7 在 `-race -count=10` 下重复 | 0 race、0 flaky | 无 sleep，全部 channel/barrier |
| F12 | regression | 全仓回归 | `go test ./...` | 0 failure | 无 |
| F13 | coverage | 新增/修改生产逻辑覆盖 | coverprofile + diff mapping | diff line coverage ≥90% | 无 |
| F14 | deployed-smoke | 真 binary/真 socket/真子进程最小链路 | 构建后运行现有或新增 smoke | 至少 1 条 fork user path 通过 | 不走 in-process shortcut |

## 异常路径覆盖

- [ ] get_task error / malformed response
- [ ] foreign assignee
- [ ] blocked/terminal/inconsistent running+blocked_reason
- [ ] start_task decline
- [ ] worktree prepare/prune failure与清理
- [ ] local pool capacity race
- [ ] executor launch failure
- [ ] duplicate control request

## 出口标准

- [ ] F1–F14 全部 pass，或报告中逐条给出不可执行的明确理由。
- [ ] 新增/修改生产逻辑 diff line coverage ≥90%。
- [ ] `go test ./...` 0 failure。
- [ ] `make test-race`（`-race -count=10`，直接看退出码）0 failure / 0 race。
- [ ] 至少 1 条 deployed-binary smoke 走真 binary + Unix socket + executor 子进程。
- [ ] conventions §15 自检通过，未改 schema、未引入跨 bounded-context 直连。
