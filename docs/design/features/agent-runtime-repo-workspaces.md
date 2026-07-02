# Agent Runtime Repo Workspaces — executor 源码工作区设计

> 承接：[agent-concurrent-execution.md](agent-concurrent-execution.md) /
> [W4a live fork](agent-concurrent-execution-w4a-live-fork-trigger.md) /
> [Project CodeRepo](project-coderepo.md)。
>
> 范围：**当前单机 worker** 下，为并发 executor 提供 agent-local repo source
> 与 per-executor git worktree。目标是让 worker daemon 保持薄，只做控制面；
> repo materialization、workspace 准备和清理归到 agent runtime / harness 边界。
> 不设计 Kubernetes 调度，只让当前实现的边界以后容易迁移。

## 1. 当前实现事实

当前代码已经有并发 executor 的文件协议和进程模型，但 git 工作区还没有接上生产路径：

| 位置 | 当前行为 |
|---|---|
| `AgentController.startSession` | 为 supervisor/codex 创建 `<agent_home>/tasks`，CLI cwd 指向该目录 |
| `executor.Layout` | executor 目录固定为 `<agent_home>/executors/<executor_id>/workspace` |
| `executor.WorktreeProvisioner` | 已封装 `git worktree add` / `add -b` / `remove --force` / `prune` |
| `executor.Pool.provisionAndSpawn` | 如果 `PoolConfig.Worktrees + BaseRef` 同时存在，则创建 worktree；否则只 `MkdirAll(workspace)` |
| `buildExecutorEngine` | 生产 wiring 明确选择 plain isolated dir，注释说明尚无 per-agent source git repo |
| `forkOnWorkAvailable` | live 路径为 `get_task -> start_task -> launchExecutor` |
| `centerTaskDetail` / `buildWorkItem` | 只投影 title / description / status / model / org_ref，无 repo / base ref |
| CodeRepo 数据 | 已有 `code_repos` 与 `pm_code_repo_refs`，project 可标 primary repo |

这意味着：当前 dev2 机器上某个 agent 恰好有 `tasks/repo` checkout，只是历史遗留的本地状态；
它不是 worker 或 executor 的可依赖生产契约。其他 agent 没有同样目录时，plain workspace
里的 executor 会启动，但没有源代码可改。

## 2. 目标

1. **agent-local source repo**：每个 agent 在自己的 home 下维护 repo source，不能依赖某个
   executor 或 supervisor 的 cwd。
2. **per-executor workspace**：executor 继续只看自己的
   `<agent_home>/executors/<executor_id>/workspace`，该目录由 git worktree 派生。
3. **worker daemon 薄化**：daemon 负责 worker 连接、heartbeat、控制循环、agent 生命周期和
   work_available 分发；repo / worktree 细节封装进 agent runtime / harness。
4. **确定性流程**：workspace 准备由 runtime 执行，不让 supervisor LLM 直接跑 git，也不要求
   supervisor LLM 调工具来创建 executor。
5. **准入前准备**：repo source / worktree 准备必须发生在 `start_task` 之前，避免中心任务已
   running 后才发现本机没有代码目录。
6. **保留迁移余地**：当前仍是单机目录和 git CLI，但 repo materializer 是可替换端口，不把
   本地磁盘 cache 变成跨机器 correctness 状态。

## 3. 非目标

- 不设计 Kubernetes 调度、PVC、分布式 cache 或跨 worker repo 同步。
- 不让 supervisor agent 或 LLM 决定 executor spawn / git worktree 生命周期。
- 不把 merge-check bare mirror 复用为 executor source repo。merge-check cache 是校验用
  临时/内部 cache，不是 agent workspace 合约。
- 不改变 task 的通用类型模型。当前只需要从 task/project 解析 primary repo 与 base ref。
- 不在本设计里实现 push、PR、merge 或 branch shipping。

## 4. 目录布局

`tasks/` 继续是 supervisor/codex 的任务 scratch，不作为生产 repo 约定。新增 repo source
目录放在 agent home 根下：

```text
<agent_home>/
  tasks/                         # supervisor/codex cwd，非生产 repo 合约
  repos/
    <repo_key>/
      source/                    # canonical non-bare checkout，worktree 从这里派生
      meta.json                  # repo_id/url/provider/default_branch/last_fetch_at
  executors/
    <executor_id>/
      input.json
      workspace/                 # git worktree；executor 唯一可见工作区
      progress.jsonl
      output.json
      status
```

`repo_key` 使用稳定 hash，例如 `sha256(normalized_repo_url)`。`meta.json` 存 repo identity；
如果现有 `source` 的 remote URL 与 task repo hint 不一致，runtime 必须 fail closed，不复用错仓。

v1 使用 non-bare `source` checkout，因为 `git worktree add` 的语义直接、便于人工排查。
以后可以把 `RepoMaterializer` 改成 bare mirror 或 remote artifact，调用方不感知。

## 5. 边界

当前可以仍在同一个 worker binary / process 内实现，但代码边界按下面切开：

```go
type AgentRuntime interface {
	StartSupervisor(ctx context.Context, spec AgentSpec) error
	StopSupervisor(ctx context.Context, agentID string) error
	HandleWorkAvailable(ctx context.Context, agentID, taskID string) error
	SnapshotConcurrency(agentID string) RuntimeSnapshot
}
```

worker daemon 只调用 `AgentRuntime.HandleWorkAvailable`。runtime 内部再做：

- task detail 获取与可运行状态预检；
- repo hint 解析；
- repo source ensure/fetch；
- executor worktree 创建；
- `start_task` 准入；
- executor spawn、watchdog、writeback、cleanup。

换句话说，daemon 不直接知道 `repos/<repo_key>/source`、branch name、`git worktree`
命令和清理策略。这是“当前单机实现考虑后续迁移”的关键：未来 runtime 可以搬到 sidecar、
job runner 或远端执行服务，而 daemon 控制面不用重新理解 git。

## 6. 数据输入

`get_task` agent-tool 响应需要扩展 repo hint。第一版只解析项目主仓：

```json
{
  "id": "task-...",
  "title": "...",
  "description": "...",
  "status": "open",
  "model": "claude-sonnet-4",
  "org_ref": "T123",
  "repo": {
    "repo_id": "repo-...",
    "url": "git@github.com:oopslink/agent-center.git",
    "provider": "github",
    "default_branch": "main",
    "is_primary": true
  },
  "base_ref": "main"
}
```

解析规则：

1. task 显式 `base_ref` 优先；
2. 没有 task base 时使用 primary CodeRepo 的 `default_branch`；
3. `repo` 缺失或 feature flag 关闭时，保持现有 plain workspace fallback；
4. 私有凭据不返回给 supervisor/executor。v1 可依赖 worker 机器已有 SSH agent / deploy key；
   后续若需要中心凭据，只给 runtime/materializer 一次性 materialization credential，禁止进
   `input.json`、executor env、日志或 chat。

## 7. 调用顺序

当前 W4a 是：

```text
work_available
  -> get_task
  -> start_task
  -> launchExecutor
```

接 repo workspaces 后改为：

```text
work_available
  -> runtime.HandleWorkAvailable(agent_id, task_id)
     -> get_task
     -> status precheck
     -> resolve repo/base_ref
     -> ensure repo source + fetch
     -> prepare executor dir + git worktree
     -> write input.json
     -> start_task
     -> spawn executor
```

重点是 `start_task` 前必须完成本机可判定的失败项：repo 不存在、credential 不可用、
base ref 不存在、worktree 创建失败。这样失败任务仍是 open/reopened，下一次配置修好后可重派；
不会进入 running 等 lease 回收。

现有 `executor.Pool.Launch` 把 reserve、provision、write input、spawn 放在一个方法里。
需要拆成两段，避免在 `start_task` 后才做 git：

```go
type PreparedExecutor struct {
	ExecutorID string
	Input      executor.Input
	Workspace string
	Cleanup    func(context.Context) error
}

Prepare(ctx, spec) (PreparedExecutor, error) // reserve + dir + worktree + input
SpawnPrepared(ctx, prepared, runnerCmd) (*executor.Handle, error)
```

如果 `SpawnPrepared` 失败，任务已经 running，仍按现有 lease reclaim 兜底；但 repo/worktree
类错误已经前移，显著缩小准入后失败窗口。

## 8. RepoMaterializer

runtime 内部新增可替换端口：

```go
type RepoMaterializer interface {
	EnsureSource(ctx context.Context, target RepoTarget) (SourceRepo, error)
	PrepareWorktree(ctx context.Context, source SourceRepo, req WorktreeRequest) (PreparedWorktree, error)
	RemoveWorktree(ctx context.Context, wt PreparedWorktree) error
}
```

`RepoTarget` 包含 `repo_id/url/provider/default_branch/base_ref`。`WorktreeRequest` 包含
`executor_id/task_id/branch_name/workspace_path`。

单机 v1 行为：

- 对每个 `repo_key` 使用进程内 mutex，clone/fetch/worktree add/remove/prune 期间串行；
  executor 运行期间不持锁。
- `EnsureSource`：不存在则 `git clone <url> source`；存在则校验 `.git` 与 `origin.url`；
  然后 `git fetch --prune origin`。
- `PrepareWorktree`：从解析后的 `origin/<base>` 或 pinned SHA 创建唯一 executor branch，
  例如 `ac-exec/<task_id>/<executor_id>`。
- `RemoveWorktree`：终态 cleanup 调 `git worktree remove --force <workspace>`，随后 `git worktree prune`。

所有错误日志只打 repo id / repo key / task id / executor id，不打 credential。

## 9. Executor 可见性

executor 只拿到自己的 `workspace`，不拿：

- `<agent_home>/repos/<repo_key>/source`；
- repo credential；
- worker/admin token；
- mcp config。

文件上传/下载和 path containment 继续以 executor workspace 为 root。`input.json` 可以包含
repo metadata 的非敏感摘要用于上下文说明，但不包含 source path 和 secret。

## 10. 清理与恢复

需要把 worktree metadata 写进 monitor 私有记录或 executor record：

```json
{
  "executor_id": "...",
  "repo_key": "...",
  "source_path": "...",
  "workspace_path": "...",
  "branch": "ac-exec/task/executor",
  "base_ref": "main"
}
```

恢复规则：

- worker/runtime 启动时扫描 `executors/`，按现有 recovery/watchdog 重新分类 executor；
- 对终态 executor，先完成 writeback，再 remove worktree；
- 对 retryable crash，保留目录与 worktree 供排查或重试策略使用；
- 永远不因 executor cleanup 删除 `repos/<repo_key>/source`；
- reset workspace 只清 `tasks/` 与 `executors/`；清 `repos/` 应是单独的 repo-cache reset。

## 11. Rollout

1. 加 feature flag，例如 `AC_EXECUTOR_GIT_WORKTREE=1`，默认关闭。
2. 扩展 `get_task` 投影 repo/base_ref，但对旧 worker 兼容：未知字段忽略。
3. 新增 materializer 单元测试，先在 `agent-center` primary repo 上灰度。
4. 打开 flag 后，缺 repo hint 的任务仍走 plain workspace；有 repo hint 的任务走 worktree。
5. 观察 `repo_materialize_failed`、`worktree_prepare_failed`、`spawn_prepared_failed`
   三类日志/指标，再扩大范围。

## 12. 测试计划

- `RepoMaterializer`：clone/fetch、remote mismatch fail closed、base ref 不存在、并发同 repo
  mutex、错误不含 credential。
- `forkOnWorkAvailable` / runtime：验证顺序为 `get_task -> prepare worktree -> start_task -> spawn`；
  prepare 失败不调用 `start_task`；at-cap 不做 repo work；旧 plain fallback 不回归。
- `executor.Pool`：拆分 prepare/spawn 后仍精确守并发上限，prepare 失败释放 reservation，
  duplicate executor id 不泄漏 worktree。
- `get_task` 投影：project primary CodeRepo 解析 repo hint；无 primary 时旧行为兼容。
- recovery/cleanup：终态后 remove worktree；writeback 失败不删 durable 状态；repo source 不被误删。

## 13. 待定问题

1. 私有仓凭据 v1 是否只支持机器 SSH key，还是同时接中心加密凭据的一次性 materialization token。
2. executor branch 终态是否默认删除，还是保留 failed/crashed 分支用于人工排查。
3. `base_ref` 的最终来源：task 字段、plan/cycle 字段，还是只用 project primary repo default branch。
