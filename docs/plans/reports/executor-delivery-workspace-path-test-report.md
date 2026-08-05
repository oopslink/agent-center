# Executor 真实工作区交付误判调查与测试报告

> 事故：`exec-a9ae5c15` / `task-e12e4fb9`
>
> 调查日期：2026-08-05
>
> 结论：代码交付存在；`non_delivery` 是交付发现 false negative，但原执行的测试命令证据已不可恢复。

## 1. 事故事实

### 1.1 执行时间线

- `2026-08-05T11:58:13Z`：runtime 为 `exec-a9ae5c15` 创建隔离执行；持久记录包含：
  - base：`50874b5fbf44bf315343bc8b37926d7dc5416df7`
  - 预配 branch：`ac-exec/task-e12e4fb9/exec-a9ae5c15`
  - 实际 worktree：worker runtime 的 `var/runtime/worktrees/exec-a9ae5c15`
- `2026-08-05T12:17:04Z`：executor 在实际 worktree 产出提交
  `6784094a299d288cf688939f43ba80a9157bce07`。
- `2026-08-05T12:17:46Z`：进程终态 `outcome=succeeded, output_present=true`。
- finalize 随即记录：`no git worktree ... eager-push UNREACHABLE`；Supervisor 只从 task audit
  看到执行启动和终态，没有收到 branch/SHA，因而在 `12:18:07Z` block 为 `non_delivery`。

### 1.2 实际交付

远端现在可独立核验：

```text
6784094a299d288cf688939f43ba80a9157bce07  refs/heads/hotfix/deployed-binary-smoke-version-assertions
```

预配的 `ac-exec/task-e12e4fb9/exec-a9ae5c15` 不在 origin；executor 切换到上述 hotfix
分支并自行推送。提交父节点正是 spawn base `50874b5f...`，变更为 5 个文件、
`+685/-15`，内容包括 worker system-info 投影、deployed-version helper 及其 Go/Playwright 回归。

因此应区分两个结论：

- **代码交付存在**：有独立 origin ref、精确 HEAD SHA 和实质 diff；原机械判定漏报。
- **原执行验收证据不完整**：executor exchange 已被回收，原始定向/全量测试命令与 exit code
  未进入 task audit，无法声称“当时的测试已经通过”。

对精确 SHA 的事后独立复核结果：

- 新增 Go 定向测试：通过；
- `deployed-version-helper.spec.ts`：3 passed；
- `go test ./...`：业务包均通过，但全量并发时
  `TestExecGitRunner_ContextCancelKillsGitProcessGroup` 未等到 fake SSH 启动而失败；该文件不在
  事故提交 diff 中，单独 `-count=20` 为 20/20 通过，判定为已有的负载时序 flake。本报告不把
  这次事后复核包装成一份全绿的原执行测试证据。

## 2. 根因

事故直接根因是两个叠加的拓扑假设失效；真实组合回归又发现一个会让 runtime 代推失败的
相邻生产缺口。

### 2.1 终态流程探测了错误目录

RepoCacheManager 实际在共享 runtime 根下创建 worktree，并把路径持久化到
`Record.WorkspacePath`。executor 也确实从该路径运行。但以下终态消费者仍各自调用
`Layout.WorkspaceDir(executorID)`，得到 exchange 目录下并不存在的旧路径：

- `finalizeGitStatus`；
- `eagerSupervisorPush`；
- `materializeEvidence`。

结果是实际 worktree 明明存在，git probe 却返回 nil，后续 delivery status、eager-push、
evidence artifact 全部不可达。cleanup 路径早已正确使用 `Record.WorkspacePath`，但这个事实没有
成为所有终态 git 操作的统一语言/单一来源。

### 2.2 mirror cache 没有 remote-tracking ref

共享 mirror 的 refspec 是 `+refs/*:refs/*`；origin heads 被保存为本地 `refs/heads/*`，
`git branch -r --contains HEAD` 为空。即使只修复工作区路径，旧探测仍会把 executor 已自行
push 的 custom branch 读成 `pushed=false`，随后因它不是预期 `ac-exec/...` 分支而拒绝代推。

durable delivery 必须直接验证 `refs/heads/<actual-branch>` 在 origin 上的 SHA 是否精确等于
本地 HEAD，而不能依赖 mirror 中不存在的 remote-tracking ref。

### 2.3 mirror source 的 push mode 会让 runtime 代推必失败

真实 RepoCache 集成回归进一步发现，共享 source 是 `git clone --mirror`，其配置带有
`remote.origin.mirror=true`。旧 eager-push 使用 `git push origin <branch>`；Git 会把该 remote
当作 mirror push，同时又看到显式 refspec，直接失败：

```text
fatal: --mirror can't be combined with refspecs
```

这不是 `exec-a9ae5c15` 自推 custom branch 被漏报的直接原因，但它解释了另一类反复出现的
“executor 已提交、runtime 声称会代推、最终仍无 origin ref”：历史测试都使用普通 clone，
从未执行真实 RepoCacheManager→Pool→Finalize 组合，因此没有触发 mirror remote 配置。

## 3. 历史修复为何反复失效

| 时间/提交 | 修复内容 | 未覆盖的缺口 |
|---|---|---|
| 2026-07-13 `88efae07` | 隔离 worktree + non-delivery gate | 当时仍假设终态能从 layout 找到 worktree |
| 2026-07-14 `53d8b616` / `0e009eb6` / `0c5224f9` | 传递 git status、要求 pushed、runtime eager-push | producer/门禁加强了，但 workspace identity 仍从旧布局反推 |
| 2026-07-16 `7c97f4f7` | 把 BaseRef 写入 Record | 只修了 base producer，没有让消费者读取同一 Record 的 WorkspacePath |
| 2026-07-17 `5c58643f` | eager-push 三种 skip fail-loud | 让错误可见，却把真实 prepared worktree 误报成“flag off / unmanaged” |
| 2026-07-24 `4f64d856` | executor 间共享 repository mirror | 同时引入“真实 worktree != exchange workspace”和 `remote.origin.mirror=true` 两个生产差异；既让终态读错路径，也让普通 eager-push refspec 必失败 |
| 2026-08-05 `b33117eb` / `41ff981c` / `3b086857` | evidence-only artifact 与真实命令事件 | artifact 仍写旧 layout 路径；事故运行版本 `main-50874b5f` 已包含这些修复，排除“仅部署未生效” |

测试遗漏位于组件接缝：executor finalize/evidence 测试都把真实 git repo 建在
`Layout.WorkspaceDir`；RepoCacheManager 测试只验证 prepare/cleanup，没有把
`PreparedWorkspace.Path != Layout.WorkspaceDir` 的生产拓扑贯穿到 Monitor.Finalize。
因此每个局部单测都绿，组合路径却长期失效。

## 4. 修复

1. 新增统一 workspace resolver：优先读取 `Record.WorkspacePath`，仅对旧 plain/pool record
   回退 `Layout.WorkspaceDir`；git probe、evidence、eager-push、prepared cleanup 共用它；
   RecoveryPlanner 也按同一规则选择 resume/rerun workspace。
2. delivery gate 前使用 authenticated `git ls-remote --heads origin refs/heads/<actual-branch>`
   校验 origin ref 与本地 HEAD 精确相等，并为网络调用设置独立 deadline。
3. executor 已自行推送 custom branch 时只采信/报告实际 branch+SHA，不改推；runtime 主动 push
   仍只允许唯一 `ac-exec/<task>/<executor>` 分支，不放宽写权限边界。
4. origin 精确匹配仍受分支权限约束：main/master/task base/repo default branch 一律拒绝；
   writeback 做第二层相同防御。
5. succeeded/failed/crashed 都把 remote-tracking ref 仅作为本地 hint 并做 origin 精确复核；
   failed/crashed 只发现 partial delivery，绝不主动产生新 push。
6. mirror-backed runtime push 临时覆盖 `remote.origin.mirror=false`，使用显式单分支
   `refs/heads/<branch>:refs/heads/<branch>` 非 force refspec，并在 push 后再次验证 SHA。
7. workspace record 损坏、origin SHA 不匹配、网络/鉴权、cleanup 失败均 fail closed，并在
   `PushError` / `non_delivery` / runtime log 中保留原因。

## 5. 本次修复验证

| 命令 | 结果 |
|---|---|
| `go test ./internal/agentruntime/executor ./internal/agentruntime/orchestrator ./internal/agentruntime -count=1` | PASS |
| `go test -cover ./internal/agentruntime/executor -count=1` | PASS，90.4% statements |
| `go test ./internal/agentruntime/... -count=1` | PASS |
| `go test ./...` | PASS（含 e2e / integration） |
| `git diff --check` | PASS |

计划逐项结果：

| ID | 状态 | 证据 |
|---|---|---|
| TC-01 | PASS | executor recorded-workspace finalize/evidence tests |
| TC-02 | PASS | RecoveryPlanner recorded runtime workspace + legacy fallback tests |
| TC-03 | PASS | actual custom branch / exact origin SHA discovery test |
| TC-04 | PASS | exact origin main refusal + writeback protected/base/default branch tests |
| TC-05 | PASS | different origin SHA + failed stale remote-tracking hint tests |
| TC-06 | PASS | expected branch eager-push positive tests |
| TC-07 | PASS | `TestRepoCacheDelivery_ActualWorkspaceFlowsThroughPoolRecordAndFinalize`，真实回归曾先红于 mirror push mode，修复后转绿 |
| TC-08 | PASS | failed partial delivery discovery / stale hint rejection tests |
| TC-09 | PASS | evidence-only recorded-workspace artifact tests |
| TC-10 | PASS | bounded origin context、corrupt Record、network 与 cleanup logging tests |
| TC-11 | PASS | legacy fixtures 与全量回归命令见上表 |

本次未修改 goroutine 或共享状态，不触发 `make test-race` 的并发改动要求。
