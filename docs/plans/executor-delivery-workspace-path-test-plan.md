# Executor 真实工作区交付探测测试计划

> 关联事故：`exec-a9ae5c15`（2026-08-05）
>
> 关联设计：`docs/design/features/agent-runtime-repo-workspaces.md` §10.1

## 1. 风险与目标

共享 RepoCacheManager 将 executor worktree 放在 runtime repo cache 下，路径不再等于 exchange
目录的 `executors/<id>/workspace`。若终态流程仍使用 exchange 派生路径，会把真实提交误判为
“无 git worktree”，并跳过远端交付发现、证据落盘和 eager-push。

本次测试要证明：

1. `Record.WorkspacePath` 是终态 git 操作和恢复规划的首选路径，空值时才兼容旧布局；
2. executor 已推送的实际分支，只有允许交付且 `origin ref == HEAD SHA` 时才算 durable delivery；
3. `main` / `master` / task base / repo default branch 即使精确存在于 origin 也不能算交付；
4. runtime 自己代推时仍只允许唯一 `ac-exec/<task>/<executor>` 分支，并可从 mirror-backed
   RepoCache 以单分支非 force refspec 推送；
5. succeeded / failed / crashed 都清除本地 remote-tracking hint 并执行远端证明，但只有
   succeeded 可以主动 push；
6. evidence-only artifact 也写入并提交到真实 worktree；
7. 错误与清理失败路径保持可观测，不能误报成功或静默泄漏。

## 2. 测试范围

### 2.1 编号测试项

| ID | 层级 | 场景与断言 |
|---|---|---|
| TC-01 | unit/component | exchange workspace 与 `Record.WorkspacePath` 不同；finalize 从记录路径读取 branch、HEAD、base advancement |
| TC-02 | unit | RecoveryPlanner 优先探测 `Record.WorkspacePath`，旧记录才回退 layout workspace |
| TC-03 | component | mirror 无 remote-tracking ref；custom branch 仅在 origin ref 精确等于 HEAD 时被发现 |
| TC-04 | component/unit | origin 上精确的 main/base/default ref 仍被 Monitor 与 writeback 拒绝 |
| TC-05 | component | origin 同名 ref 停留旧 SHA 或本地 hint stale；`Pushed=false` 且 unexpected branch 不被代推 |
| TC-06 | component | 普通 repo 中未推送的预期 `ac-exec` branch 被非 force eager-push 并二次精确验证 |
| TC-07 | integration | 真实 RepoCacheManager→PreparedWorkspace→Pool Record→Monitor.Finalize；split path 与 `remote.origin.mirror=true` 下仍交付精确 branch/SHA |
| TC-08 | component | failed/crashed 已有 partial origin delivery 被发现；未推送内容不产生 runtime push，stale hint 被清除 |
| TC-09 | component | evidence-only 使用真实记录路径；artifact、commit 与 origin ref 来自同一 worktree |
| TC-10 | unit | origin 查询有 deadline；Record 损坏、网络/鉴权、cleanup 失败均 fail closed 且有日志/PushError |
| TC-11 | regression | 旧 exchange-layout plain/pool fixture 保持兼容，全量测试无回归 |

### 2.2 回归与全量测试

- 定向：`go test ./internal/agentruntime/executor`
- 相关运行时：`go test ./internal/agentruntime/...`
- 全量：`go test ./...`

本次不修改 goroutine 或共享状态；不触发 `make test-race` 的并发改动条件。

## 3. 验收标准

- 事故同构路径不再出现 `no git worktree ... eager-push UNREACHABLE`；
- writeback 中包含实际 branch、精确 HEAD SHA、`pushed=true`；
- 远端 ref 与 HEAD 不一致时结果必须为 retryable `non_delivery`；
- 所有定向、相关运行时与全量测试 0 failures；
- 测试报告记录命令、退出码与变更覆盖。
