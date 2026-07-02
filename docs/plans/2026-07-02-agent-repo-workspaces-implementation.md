# Agent Repo Workspaces — 技术方案与开发计划

> 承接设计文档：[agent-runtime-repo-workspaces.md](../design/features/agent-runtime-repo-workspaces.md)
>
> 日期：2026-07-02

## 1. 目标效果

当 supervisor LLM 决定 fork executor 做某个 task 时，runtime 自动：

1. 检查前置条件（并发上限、duplicate executor）
2. 从 task 的 repo hint 解析 project primary CodeRepo
3. 在 `<agent_home>/repos/<repo_key>/source` ensure/fetch canonical checkout
4. 从 source 创建 per-executor git worktree 到 `<agent_home>/executors/<executor_id>/workspace`
5. 调 `start_task` 准入
6. spawn executor 进程，cwd = 隔离的代码 checkout
7. executor 结束后 worktree 自动清理

### 关键约束

- 没有 repo hint 或 feature flag 关闭时 → plain workspace fallback
- repo/worktree 准备失败 → 返回错误给 supervisor，supervisor 决定下一步
- executor 看不到 source repo path、credential、worker token
- feature flag `AC_EXECUTOR_GIT_WORKTREE=1`，默认关闭

## 2. Agent 并发模型

### 2.1 职责分工

| 角色 | 职责 | 不做 |
|---|---|---|
| **Supervisor LLM** | 决策：看任务，决定自己做还是 fork executor；日常对话 | 不做 workspace 准备、不跑 git |
| **Runtime** | 执行：持有 supervisor session + executor 全生命周期 | 不做 fork 决策（at capacity 返回错误，supervisor 自行决定） |
| **Daemon** | 信号路由：command 解码 → 转发给正确 runtime 实例 | 不持有 session、不做 fork、不做 workspace 准备 |

### 2.2 调用链

```
[工作通知]
center → daemon: work_available(agentID, taskID)
  → daemon 路由到 runtime
  → runtime.NotifyWorkAvailable(taskID)
  → runtime 内部：inject nudge 到 supervisor session
  → supervisor LLM 通过 MCP 查看任务、决策
  → supervisor 调 MCP fork_executor(task_id)
  → runtime.SpawnExecutor(req)
    ├ 前置检查（并发上限 / duplicate）
    ├ repo ensure/fetch + worktree prepare
    ├ start_task（center 准入）
    └ spawn executor 进程

[日常对话]
center → daemon: converse(agentID, conversationID, message)
  → daemon 路由到 runtime
  → runtime.NotifyConverse(req)
  → runtime 内部：inject converse brief 到 supervisor session
  → supervisor LLM 处理对话，通过 MCP 回复

[生命周期]
center → daemon: reconcile(agentID, desired=running)
  → daemon: runtime = NewLocalRuntime(cfg) 或 runtime.Reconfigure(cfg)
center → daemon: reconcile(agentID, desired=stopped)
  → daemon: runtime.Stop()
```

### 2.3 k8s 部署模型

```
当前（单机）：                          未来（k8s）：
┌──────────────────────┐              ┌────────────────────┐
│ daemon               │              │ daemon (per cluster)│
│  ├ websocket←center  │              │  ├ websocket←center│
│  ├ command decode     │              │  ├ command decode  │
│  ├ route → runtime    │              │  └ RPC → pod      │
│  │    │              │              └────────┬───────────┘
│  │    ▼              │                       │
│  │ Runtime (同进程)  │              ┌────────▼───────────┐
│  │  ├ supervisor sess│              │ Pod (per agent)    │
│  │  ├ SpawnExecutor  │              │  = Runtime         │
│  │  ├ Materializer   │              │  ├ supervisor sess │
│  │  └ Pool/Monitor   │              │  ├ SpawnExecutor   │
│  │                   │              │  ├ Materializer    │
│  ├ heartbeat/snapshot│              │  └ Pool/Monitor    │
│  └ reconcile         │              └────────────────────┘
└──────────────────────┘
```

k8s 下 supervisor session 在 pod 里，daemon 不可能直接 `sess.Inject()`。所以 **converse / work_available / wake 等所有需要触达 session 的操作都必须经过 Runtime 接口**。

## 3. DDD 边界分析

### 3.1 涉及的限界上下文

| BC | 角色 | 改动范围 |
|---|---|---|
| **ProjectManager** | 拥有 Task + CodeRepoRef（`is_primary`） | `agentTaskMap` 投影补 repo hint |
| **CodeRepo** | 拥有 Repo 实体（url/provider/default_branch/加密凭据） | 只读消费，不改动 |

### 3.2 应用层组件（非 BC）

| 组件 | 角色 | 改动范围 |
|---|---|---|
| **Worker Daemon** (`internal/workerdaemon`) | 薄信号路由 | 提取 Runtime；退化为 command → runtime 转发 |
| **AgentRuntime** (新，`internal/workerdaemon/agentruntime`) | per-agent 执行面 | 承接 supervisor session + executor 全生命周期。放在 workerdaemon 子包：import 方向天然正确（子包不 import 父包）。后续有需要可 promote 到 `internal/agentruntime` |
| **executor 包** | Pool/Monitor/Worktree/Materializer | 新增 RepoMaterializer |
| **orchestrator 包** | Engine | 不改 |

### 3.3 DDD 概念归类

| 新概念 | DDD 范畴 | 说明 |
|---|---|---|
| `AgentRuntime` | 应用服务接口 | daemon→runtime 边界；未来 k8s RPC 合约。注：设计文档 §5 使用 `AgentRuntime` + `StartSupervisor/StopSupervisor/HandleWorkAvailable`；本方案细化为 `Runtime` + `Start/Stop/NotifyWorkAvailable/SpawnExecutor` 分离通知与执行。**实现时以本方案为准，同步更新设计文档。** |
| `SpawnExecutor` | 应用服务方法 | supervisor（MCP tool）→ runtime 的 fork 入口 |
| `RepoHint` | 值对象 | get_task 投影中的 repo 摘要 |
| `RepoTarget` / `SourceRepo` / `PreparedWorktree` | 值对象 | RepoMaterializer 的入参/返回 |
| `RepoMaterializer` | 端口 (Port) | 可替换的 repo 物化接口 |
| `LocalGitMaterializer` | 适配器 (Adapter) | 单机 v1 实现 |

## 4. 技术方案

### Phase 0：提取 AgentRuntime 边界

**目标**：把 `AgentController` 拆成两层——daemon（信号路由）+ Runtime（per-agent 执行面）。Runtime 持有 supervisor session + executor engine，daemon 不再直接接触 session。

这是一个大型纯结构重构，行为不变。

#### 4.0.1 Runtime 接口

```go
// Package agentruntime — per-agent 执行面。
// 包路径：internal/workerdaemon/agentruntime
// 约束：不 import internal/workerdaemon（方向：daemon → runtime，不反向）。
//       共享类型（如 StreamEvent）通过 interface 或独立包传递。
package agentruntime

// Runtime is the per-agent execution runtime. Daemon holds one per agent.
// 当前实现：同进程 LocalRuntime。未来 k8s：RPC stub → pod。
type Runtime interface {
    // === 信号投递（daemon → runtime → supervisor session） ===

    // NotifyWorkAvailable: 有任务来了。
    // FIXME(phase-6): 过渡期直接调 SpawnExecutor（W4a 行为）。
    // Phase 6（supervisor 接入 fork_executor MCP tool）完成后，必须改为
    // inject nudge 到 supervisor session，并删除直接 SpawnExecutor 调用。
    NotifyWorkAvailable(ctx context.Context, taskID string) error

    // NotifyConverse: 人类发来日常对话消息。
    NotifyConverse(ctx context.Context, req ConverseRequest) error

    // NotifyWork: legacy agent.work brief（注入 supervisor session）。
    NotifyWork(ctx context.Context, req WorkRequest) error

    // NotifyWake: wake nudge（注入 supervisor session）。
    NotifyWake(ctx context.Context, req WakeRequest) error

    // === Supervisor session 生命周期 ===

    // Start: 启动 supervisor session（cli=claude-code 或 codex）。
    Start(ctx context.Context, spec StartSpec) error

    // Stop: 停止 supervisor session。
    Stop(ctx context.Context) error

    // IsRunning: session 是否存活。
    IsRunning() bool

    // === Executor 管理（supervisor MCP tool → runtime） ===

    // SpawnExecutor: supervisor 决定 fork 时调用。
    // 前置检查 → repo/worktree → start_task → spawn。
    //
    // 并发约束：SpawnExecutor 内部使用 per-runtime mutex 串行化整个
    // get_task → prepare → start_task → launch 序列。原因：
    // (a) Pool.Launch 内部 mutex 只保护 cap 计数，不保护前序步骤；
    // (b) 调用方可能来自不同 goroutine（MCP tool handler vs 过渡期
    //     NotifyWorkAvailable），必须防止同 agent 的并发 double-fork。
    SpawnExecutor(ctx context.Context, req SpawnRequest) (*SpawnResult, error)

    // === 周期性运维 ===

    // Tick: daemon OnTick 驱动的周期性维护入口。内部做：
    // self-heal 重试、rate-limit/API-error resume、executor watchdog sweep。
    Tick(ctx context.Context, now time.Time) error

    // Recover: daemon 重启后重建 in-flight executor 状态。
    Recover(ctx context.Context) error

    // SnapshotConcurrency: heartbeat 上报的实时 executor 视图。
    SnapshotConcurrency() []concurrency.ExecutorSnapshot
}

// SpawnRequest is the input for SpawnExecutor.
type SpawnRequest struct {
    TaskID string
    // === optional hints（supervisor 已知信息，避免 runtime 重复 RPC）===
    Model   string     // supervisor 指定的 model override（空 = runtime 自行解析）
    Context string     // supervisor 聚合的上下文（空 = runtime 从 task detail 构建）
    // 后续可扩展：priority、resource hints 等
}

type SpawnResult struct {
    ExecutorID string
    Model      string
    CLI        string
}
```

#### 4.0.2 搬迁原则：什么留在 daemon、什么搬到 runtime

**留在 daemon（`AgentController`）的**：

| 职责 | 方法 | 理由 |
|---|---|---|
| command 解码路由 | `Handle` | 纯路由，不碰 session |
| reconcile 决策 | `reconcile` / `reconcileRunning` / `reconcileStop` / `reconcileReset` | 创建/销毁 runtime 实例是 daemon 职责 |
| runtime 实例管理 | `maybeAttachRuntime` (原 `maybeAttachExecutorEngine`) | daemon 持有 `map[agentID]Runtime` |
| heartbeat / snapshot 聚合 | `SnapshotConcurrency` (遍历 runtimes) | 跨 agent 聚合是 daemon 职责 |
| boot reconcile | `ReconcileOnBoot` / `enumerateLocalAgents` | daemon 启动时的全局初始化。新流程：枚举本地 agents → 对每个 agent 创建 runtime（`NewLocalRuntime`）→ 调 `runtime.Recover()`（重建 executor 状态）→ 根据 center desired state 调 `runtime.Start()` 或 `runtime.Stop()` |
| 全局 GC / lease renew | `maybeRunGC` / `drainLeaseRenewals` | 跨 agent 周期性维护 |
| shutdown | `Shutdown` / `Stop` | 遍历 runtime.Stop() |

**搬到 runtime（`LocalRuntime`）的**：

| 职责 | 来源方法 | runtime 方法 |
|---|---|---|
| supervisor session start/stop | `startSession` / `startCodexSession` / `stopSession` | `Runtime.Start` / `Stop` |
| session 事件处理 | `onEvent` / `onExit` | `LocalRuntime` 内部 callback |
| work inject | `work` | `Runtime.NotifyWork` |
| wake inject | `wake` | `Runtime.NotifyWake` |
| converse inject | `converse` | `Runtime.NotifyConverse` |
| work_available 通知 | `workAvailable` (并发 agent 分支) | `Runtime.NotifyWorkAvailable` |
| executor fork / drain | `forkOnWorkAvailable` / `launchExecutor` / `drainExecutor` / `workViaExecutor` | `Runtime.SpawnExecutor` 内部 |
| executor engine 构建 | `buildExecutorEngine` | `NewLocalRuntime` 工厂 |
| executor recovery / watchdog | `recoverExecutors` / `maybeRunExecutorWatchdog` / `checkOrphanOnce` | `Runtime.Recover` / `Tick`（内部 sweep） |
| center RPC (fork 准入) | `fetchCenterTask` / `startCenterTask` / `blockTaskOnModelNotAllowed` | 通过 `CenterClient` 端口 |
| self-heal | `recordCrashAndSchedule` / `selfHealRelaunch` | `LocalRuntime` 内部 |
| rate limit resume | `maybeScheduleRateLimitResume` / `drainRateLimitResumes` | `LocalRuntime` 内部 |
| API error resume | `maybeScheduleAPIErrorResume` / `resetAPIErrorRetries` | `LocalRuntime` 内部 |
| turn failure surface | `surfaceTurnFailure` / `surfaceConverseFailure` | `LocalRuntime` 内部 |
| task events | `recordTaskEvent` / `sealTaskSegment` | `LocalRuntime` 内部 |
| usage report | `maybeReportUsage` | `LocalRuntime` 内部 |
| reply nudge | `maybeReplyNudge` | `LocalRuntime` 内部 |
| dedup sets | `recordWake` / `recordWorkAvail` | `LocalRuntime` 内部 |

#### 4.0.3 managedAgent 状态迁移

`managedAgent` 的大部分字段搬到 `LocalRuntime` 内部。daemon 侧只保留薄 wrapper：

```go
// daemon 侧（AgentController）
type managedAgent struct {
    agentID        string
    runtime        agentruntime.Runtime // 持有一切
    appliedVersion int                  // reconcile 版本幂等
}

// runtime 侧（LocalRuntime 内部状态，不暴露）
// session、hadWork、currentTaskID、currentConversationID、
// wakeSeen、workAvailSeen、toolNames、taskLog、
// rateLimitResumeAt、apiErrorRetries、selfHeal、exec engine...
// 全部在 LocalRuntime 内部
```

#### 4.0.4 端口

**AdmissionClient**（runtime → center，fork 前准入）：

```go
// AdmissionClient is the runtime's port to the center for pre-fork admission.
// 与 orchestrator.WritebackClient（fork 后回写）职责不同，独立演进。
type AdmissionClient interface {
    GetTask(ctx context.Context, agentID, taskID string) (*TaskDetail, error)
    StartTask(ctx context.Context, agentID, taskID string) error
    BlockTask(ctx context.Context, agentID, taskID, reason, reasonType string) error
}
```

注意：orchestrator 已有 `CenterClient`（CompleteTask/BlockTask/PostMessage，用于 writeback）。为避免混淆，runtime 侧命名为 `AdmissionClient`（准入面），orchestrator 侧重命名为 `WritebackClient`（回写面，follow-up 统一）。两者职责不同，保持独立。

**Reporter**（runtime → center 活动上报）：复用现有 `AgentControllerConfig.Reporter` 接口，注入 runtime。

**SessionStarter**（runtime 内部启动 supervisor 进程）：复用现有 `starter` / `codexStarter` 函数签名，注入 runtime。

#### 4.0.5 daemon 的 Handle 退化

```go
func (c *AgentController) Handle(ctx context.Context, cmd ControlCommand) error {
    switch cmd.CommandType {
    case cmdTypeAgentReconcile:
        // 解码 → reconcile 决策 → 创建/销毁 runtime
        return c.reconcile(ctx, pl)
    case cmdTypeAgentWork:
        // 路由到 runtime
        rt := c.runtimeFor(pl.AgentID)
        if rt == nil { return ... }
        return rt.NotifyWork(ctx, ...)
    case cmdTypeAgentWake:
        rt := c.runtimeFor(pl.AgentID)
        return rt.NotifyWake(ctx, ...)
    case cmdTypeAgentConverse:
        rt := c.runtimeFor(pl.AgentID)
        return rt.NotifyConverse(ctx, ...)
    case cmdTypeWorkAvailable:
        rt := c.runtimeFor(pl.AgentID)
        return rt.NotifyWorkAvailable(ctx, pl.TaskID)
    ...
    }
}
```

#### 4.0.6 OnTick 退化

```go
func (c *AgentController) OnTick(ctx context.Context) {
    now := c.now()

    // 全局维护（留在 daemon）
    c.maybeRunGC(now)
    c.drainLeaseRenewals(ctx, now)

    // per-runtime 维护（转发到 Runtime.Tick）
    for _, rt := range c.allRuntimes() {
        // Runtime.Tick 内部做：
        // - self-heal 重试（crash backoff + relaunch）
        // - rate-limit / API-error resume（inject resume nudge）
        // - executor watchdog sweep（stall detection + orphan poll）
        if err := rt.Tick(ctx, now); err != nil {
            c.log("runtime tick: %v", err)
        }
    }
}
```

#### 4.0.7 约束

- **行为不变**：Phase 0 是纯结构重构
- `agentruntime` 包不 import `workerdaemon` 包（方向：daemon → runtime）
- orchestrator / executor 包不改
- 测试随代码搬迁

#### 4.0.8 分步执行

Phase 0 体量大，建议分 3 个子步骤独立合并：

| 子步骤 | 内容 | 风险 |
|---|---|---|
| **0a** | 定义 `Runtime` 接口 + `LocalRuntime` 骨架 + daemon 侧 `managedAgent` 改为持有 `Runtime`。NotifyConverse/NotifyWork/NotifyWake 先实现为直接转发到内部 session（行为不变）。 | 低：接口定义 + 薄 wrapper |
| **0b** | 搬迁 session 生命周期（Start/Stop）+ onEvent/onExit + self-heal + rate-limit/API-error resume + task events 到 `LocalRuntime`。daemon 的 `reconcileRunning` 改为调 `runtime.Start`。 | 中：搬迁最多代码，但行为不变 |
| **0c** | 搬迁 executor 面（executorEngine / forkOnWorkAvailable / launchExecutor / drainExecutor / recovery / watchdog）到 `LocalRuntime`。暴露 `SpawnExecutor` + `Recover`；watchdog sweep 归入 `Tick`。 | 中：依赖 0b 的 session 集成 |

### Phase 1：数据面 — get_task 投影补 repo hint

（可与 Phase 0 并行）

**改动点**：

1. **`agentTaskMap`** (`internal/admin/api/agent_tools_passthrough.go`)
   - task → `task.ProjectID()` → `PMService.ResolveProjectRepoForMember(projectID, "", actor)` 解析 primary CodeRepoRef
   - 有 primary ref 且 ref.RepoID() 非空 → `CodeRepoSvc.GetRepo(ref.RepoID())` 拿 url/provider/default_branch
   - 拼 `repo` 子对象：`{repo_id, url, provider, default_branch, is_primary}`
   - `base_ref` 取值优先级：task.Base() > repo.DefaultBranch()
   - 无 primary ref 或解析失败 → 不 emit `repo` 字段（向后兼容）

2. **`TaskDetail`** (`agentruntime` 包内类型)
   - `Repo *RepoHint` + `BaseRef string`

**测试**：有 primary → emit；无 primary → 无 repo；跨 org → 不泄漏；JSON 兼容旧格式

### Phase 2：RepoMaterializer — 本地 git 物化

（可与 Phase 0/1 并行）

**新增文件**：`internal/workerdaemon/executor/materializer.go` + `materializer_test.go`

```go
type RepoMaterializer interface {
    EnsureSource(ctx context.Context, target RepoTarget) (SourceRepo, error)
    PrepareWorktree(ctx context.Context, source SourceRepo, req WorktreeRequest) (PreparedWorktree, error)
    RemoveWorktree(ctx context.Context, wt PreparedWorktree) error
}
```

**LocalGitMaterializer**：per-repo_key mutex；EnsureSource（clone/fetch/validate remote）；PrepareWorktree（复用 WorktreeProvisioner.AddNewBranch）；RemoveWorktree（复用 WorktreeProvisioner.Remove）。错误日志不含 credential。

### Phase 3：SpawnExecutor 接入 repo workspace

**前置**：Phase 0c + Phase 1 + Phase 2。

`LocalRuntime.SpawnExecutor` 内部加 repo/worktree 准备步骤：

```go
func (r *LocalRuntime) SpawnExecutor(ctx context.Context, req SpawnRequest) (*SpawnResult, error) {
    // 1. 前置检查
    if r.pool.Available() == 0 { return nil, ErrAtCapacity }

    // 2. 获取任务详情
    task, err := r.center.GetTask(ctx, r.agentID, req.TaskID)

    // 3. repo/worktree 准备
    var prepared *preparedWorkspace
    if r.materializer != nil && task.Repo != nil {
        source, err := r.materializer.EnsureSource(ctx, resolveRepoTarget(task))
        if err != nil { return nil, err }
        wt, err := r.materializer.PrepareWorktree(ctx, source, WorktreeRequest{...})
        if err != nil { return nil, err }
        prepared = &preparedWorkspace{worktree: wt}
    }

    // 4. center 准入
    if err := r.center.StartTask(ctx, r.agentID, req.TaskID); err != nil {
        if prepared != nil { r.materializer.RemoveWorktree(ctx, prepared.worktree) }
        return nil, err
    }

    // 5. fork
    launched, err := r.engine.HandleWork(ctx, buildWorkItem(req.TaskID, task, prepared))
    if err != nil {
        // fork 失败但 task 已 running（step 4 start_task 已成功）。
        // - worktree 必须清理（否则泄漏）；
        // - task 状态依赖 lease reclaim 自动回收到 open（与现有 W4a
        //   行为一致：forkOnWorkAvailable 也不回滚 start_task）。
        if prepared != nil {
            r.materializer.RemoveWorktree(ctx, prepared.worktree)
        }
        return nil, err
    }

    go r.drain(launched.Handle)
    return &SpawnResult{...}, nil
}
```

Feature flag：`AC_EXECUTOR_GIT_WORKTREE=1` → 创建 materializer；off → nil → plain workspace。

**测试**：prepare 在 start_task 前；prepare 失败 → error 不调 start_task；start_task 失败 → 清理 worktree；at capacity → ErrAtCapacity；无 repo → plain workspace

### Phase 4：Pool.Launch 接入 prepared worktree + Monitor cleanup

**LaunchSpec 扩展**：`PreparedWorkspace string`（非空 → 跳过 workspace 创建）。

**Monitor cleanup**：`MonitorConfig` 新增可选 `Materializer RepoMaterializer`；`Finalize` 时有 worktree metadata → `materializer.RemoveWorktree`。`Record` 新增 `RepoKey` + `SourcePath`。

### Phase 5：Recovery 扩展

Reconciler 读到有 RepoKey 的 Record → 终态 executor 用 materializer 清理 worktree。无 metadata → 现有行为。

### Phase 6（后续）：supervisor 接入 fork_executor MCP tool

不在本次范围。Phase 0-5 的结构已为此做好准备：

1. center 新增 `fork_executor` agent-tool endpoint
2. MCP host 注册 `fork_executor` tool
3. tool handler 调 `runtime.SpawnExecutor(req)`
4. `NotifyWorkAvailable` 从直接 SpawnExecutor 改为 inject supervisor session
5. supervisor system prompt 补充 fork 决策指导

`SpawnExecutor` 接口和内部流程不变。

## 5. 开发计划

### Phase 依赖关系

```
Phase 0a (Runtime 接口 + 骨架)
    │
    ▼
Phase 0b (session 生命周期搬迁)
    │
    ▼
Phase 0c (executor 面搬迁)          Phase 1 (数据面)      Phase 2 (RepoMaterializer)
    │                                │                    │
    │  ← Phase 1/2 与 Phase 0       │                    │
    │    无代码依赖，可并行开发      │                    │
    │                                │                    │
    └───────────────┬────────────────┘────────────────────┘
                    ▼
             Phase 3 (SpawnExecutor 接入 repo)
                    │        依赖 0c（SpawnExecutor 在 runtime 内）
                    │              + 1（repo hint 数据）
                    │              + 2（RepoMaterializer 实现）
                    ▼
             Phase 4 (Pool/Monitor 接入)
                    │
                    ▼
             Phase 5 (Recovery)
```

### 每 Phase 交付物

| Phase | 交付物 | 可独立合并 | 验证方式 |
|---|---|---|---|
| 0a | `Runtime` 接口 + `LocalRuntime` 骨架 + daemon 路由改造 | 是 | 现有测试全绿 |
| 0b | session start/stop + onEvent/onExit + self-heal 搬迁 | 是 | 现有测试搬迁全绿 |
| 0c | executor engine + fork + recovery + watchdog 搬迁 | 是 | 现有测试搬迁全绿 |
| 1 | `agentTaskMap` repo hint + `TaskDetail` 解析 | 是 | 单元测试 |
| 2 | `RepoMaterializer` + `LocalGitMaterializer` | 是 | 纯单元测试 |
| 3 | `SpawnExecutor` 接入 repo 步骤 | 是（flag off） | mock materializer |
| 4 | `Pool.Launch PreparedWorkspace` + Monitor cleanup | 是 | 单元测试 |
| 5 | Recovery worktree cleanup | 是 | 单元测试 |

### 风险点

| 风险 | 缓解 |
|---|---|
| Phase 0 重构范围大（`AgentController` 40+ 方法，`managedAgent` 20+ 字段） | 分 3 子步骤，每步独立合并 + 全量测试；纯结构重构不改行为 |
| `agentruntime` 包与 `workerdaemon` 包的循环依赖 | 严格保证 agentruntime 不 import workerdaemon；共享类型提到 interface / 独立包 |
| session callback（onEvent/onExit）线程模型 | 搬迁时保持 callback 在 session reader goroutine 上运行，mutex 保护共享状态 |
| 私有仓 SSH key | v1 只支持机器 SSH agent / deploy key |

## 6. 安全自检

- [ ] executor 看不到 source repo path → `input.json` 不含 `SourcePath`
- [ ] executor 看不到 credential → `agentTaskMap` 不 emit credential；materializer 用机器 SSH
- [ ] executor 看不到 worker token → `BuildExecutorEnv` 已排除
- [ ] 错误日志不含 credential
- [ ] feature flag off → nil materializer = plain workspace
- [ ] `agentruntime` 包不依赖 `workerdaemon` 包
- [ ] `SpawnExecutor` 失败返回错误 → supervisor 决策，runtime 不越权

## 7. k8s 迁移路径

| 步骤 | 改动 |
|---|---|
| **k8s Step 1** | daemon 侧新增 `RemoteRuntime`（gRPC client）。pod 内跑 `LocalRuntime` 暴露为 gRPC server。`Runtime` 接口 = RPC 合约。 |
| **k8s Step 2** | pod 拿自己的 center transport（agent-scoped token）。CenterClient 端口在 pod 内直接实现。 |
| **daemon 不变** | `Runtime` 接口不变。daemon 的 Handle → route → runtime 逻辑不变。 |
