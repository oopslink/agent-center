# Agent Runtime 自包含 + 自恢复（完成 T777/0b/0c 重构，为 k8s 做架构准备）

> **状态**：设计草案，待 oopslink 复核（2026-07-03）。
> **来源**：dogfood 中发现——worker daemon 重部署后，某 agent 的 managed executor 被杀成"僵尸 running"任务，系统**不自动恢复**（既不重跑也不重派，一直卡到 5h 租约过期）。根因排查后 oopslink 定性：**这不是打补丁，是把 T777/0b/0c 那次"把 session/executor 面抽进 `agentruntime.LocalRuntime`"的重构做完**——让 agent runtime 真正**自包含 + 自恢复**。
> **范围**：**现在仍单机 daemon 跑不变**；这次只做**架构 k8s-ready**（恢复/状态归 runtime 自包含），**不真上 k8s**（独立 pod 入口/PV/manifest 推迟）。

## 1. 背景与现状

### 1.1 组件关系（已核代码）
- **中心 Center**（`agent-center server`）：任务/plan/派发 + workforce 注册表。
- **Worker**：workforce 里注册的执行单元（`internal/workforce/worker.go`，WorkerID 不可变、心跳）。**agent 创建即不可变绑定一个 worker**（`internal/agent/agent.go:110`「immutable binding」/`:364`「changing worker = new Agent」）。
- **Worker daemon**（`agent-center worker run` → `workerdaemon.RunDaemon`）：一 worker 一进程，内含 `AgentController`，**托管该 worker 上的 N 个 agent**（`c.agents` map）。
- **Agent runtime**（`agentruntime.LocalRuntime`）：**每 agent 一份**，拥有该 agent 的 supervisor claude 会话 + executor 引擎（并发时 fork N 个 **executor 子进程**，每个是一个 LLM CLI，在自己的 git worktree 里跑一个任务）。

### 1.2 k8s 目标拓扑（本次只做"准备"，不落地）
- **1 个 k8s 集群 = 1 个 worker**（与 node 无关）；**worker 自己是一个协调 pod**；**每个 agent = 一个独立 pod**（pod 内：1 supervisor 会话 + N executor 进程；持久卷跨重启存活）。即：1 集群 = 1 worker pod + N agent pod。
- 关键红利：pod 模型下 pod 一重启 = supervisor + 它的 executor **一起死一起起**，**没有"会话活着但 executor 被单独杀"的中间态**（那正是现在 daemon 模型 `bootReattach` live-survivor 的乱源），恢复因此是**确定性的**。

### 1.3 重构完成度（已核代码）
- ✅ **抽取已做**：T779/T781（Phase 0b/0c，`e6b83ff8`/`057c7936`/`de768ddd`）把 session 面 + executor 面从 daemon 抽进了 `LocalRuntime`（执行侧自包含）。
- ❌ **自恢复没做完**：`Recover` 只被 daemon 触发（`agent_controller.go:661`），runtime 不自触发；CenterClient 无 list-my-tasks；`cmd/` 只有 daemon、无独立 runtime 入口；崩溃恢复靠 daemon 的 SelfHealStore 重拉循环。

## 2. 决策（已与 oopslink 拍定）

1. **单独立一个重构 plan 专做此事**；当前审计日志 plan（plan-146140fc）**保持 running 冻结**，重构落地 + 重部署后它**自动恢复推进**——这同时是本特性的**部署级验收**。
2. **executor 恢复 = 满血 resume + 三档降级阶梯**（§4.3）。
3. **范围**：单机继续跑、只做架构 k8s-ready；**崩溃恢复做可插拔 seam**（单机=daemon in-process relaunch / k8s=进程退出→pod 重启）；独立 pod 入口/PV/manifest **推迟**。
4. **控制 + 观测支柱**：supervisor/runtime 完全控制 + 观测 executor——挂了**立刻**知道、卡了检测到、并恢复；检测与恢复决策走**确定性 Go**（不靠 LLM）。

## 3. 目标：LocalRuntime 自包含 + 自恢复

runtime 拥有自己的：状态（私有锁/配置）、center 连接、boot 自恢复、executor 全生命周期控制与观测。daemon 退化为"实例化 runtime + 转发命令 + 提供重启触发"的薄壳；将来切 k8s 只替换"宿主"（daemon→pod），恢复逻辑不动。

## 4. 设计（改动点，均带 file:line）

### 4.1 去共享状态（前提）
- 现状：`cfg.Mu == &AgentController.mu`（`local_runtime.go:53`）、`cfg.BG == &AgentController.bg`、全局 map `c.agents`/`c.execConfig`/`c.recoveredExec`、`cfg.RemoveAgent`/`cfg.SelfHeal`。
- 改：LocalRuntime 用**私有** `sync.Mutex` + `sync.WaitGroup`；exec-config/recovered-once 变**每 runtime 一份**；从 `LocalRuntimeConfig` 去掉 `RemoveAgent`/`SelfHeal`（崩溃语义见 §4.5）。`forkMu` 已是 per-runtime，保留。

### 4.2 runtime 自建 center 连接 + list-my-tasks
- 现状：center 访问是 daemon 注入的共享 `*AdminClient`（`run.go:213`，`cfg.ToolCaller`）；`centerClientAdapter`（`center_client.go`）只有 complete/block/post，`get_task` 走 `toolCaller().CallAgentTool`。
- 改：runtime 从自己的 config 构造 center client；给 center client 加 **`ListMyInflightTasks`**。`list_my_tasks` 端点已存在（`admin/api/agent_tools.go:99` → `reads.go:418 ListRunnableAgentTasks`）**但按 deps 过滤会漏报 running-但-依赖未满足** 的任务；reconcile 需要**不过滤的全量在途清单**——surface `ListAssignedAgentTasks`（`reads.go:448`，全部 open/running）为一个 agent-tool。

### 4.3 executor session-id 持久化 + 三档降级恢复阶梯
- 现状（已核）：executor 是一次性 `claude -p <prompt> --output-format stream-json --verbose`（`orchestrator/runner.go:163`），**明确 NO --session-id**（`:144`）；`executor.Record`（`executor/recovery.go:32`）无 session 字段。→ 现在**无法 resume**。
- 改：
  1. executor launch 时**分配并持久化 session-id**（写进 `Record`，落在 executor 目录的 `orchestrator.json`）；executor runner 从一次性 `-p` 改成**带 session、可 `--resume`** 的调用方式。
  2. 恢复时按**三档降级阶梯**（对每个"该继续"的 executor）：
     - **档1 满血**：`session-id 有` 且 `worktree 在`（RepoKey/SourcePath）→ 原 worktree `--resume <sid>`，**保住 LLM 对话上下文**。
     - **档2 降级**：`worktree 在` 但 `session-id 丢` → 在**原 workspace** 重跑持久化的 `RunnerCmd`（上下文丢，但已提交到 worktree 的进度还在）。
     - **档3 从头**：`worktree 也没了` → 新 worktree + 全新跑。

### 4.4 runtime 自触发 boot 自恢复（reconcile）
- 现状：`Recover` 只在 `agent_controller.go:661`（daemon attach 时）被调；supervisor 恢复全在 daemon 的 `boot_reconcile.go`（`decideBootAction`/`bootReattach`/`bootReapRelaunch`）。
- 改：新增 `LocalRuntime.Boot(ctx)`/`selfReconcile(ctx)`，runtime **自己 boot 时跑**：
  1. **恢复 supervisor 会话**：把 daemon 的探测→reattach/relaunch 决策搬进 runtime（supervisor 会话本已可 resume：epoch+generation→`SessionUUIDGen` + `sessioninstance` + `CompletedTurn`，`local_runtime.go:420/466`）。
  2. `recoverExecutors`（`executor_runtime.go:313`）扫磁盘 Record。
  3. 拉 `ListMyInflightTasks`，**对账**每个 executor Record × 我的在途任务：
     - 该继续 + 进程活着（单机 daemon 重启可能有存活 executor）→ **adopt**（`ee.addOrphan`，watchdog 接管）；
     - 该继续 + 进程死了 → 走 §4.3 **三档阶梯**恢复；
     - 该取消（任务不在我的在途清单/已 discard/completed/改派/plan 停）→ **停 + 清理 worktree**。

### 4.5 可插拔崩溃 seam + 控制/观测硬化
- **崩溃恢复可插拔**（§2.3）：runtime 只负责"boot 自恢复"；**重启触发**抽象成一个接口——**单机**=daemon 原地 in-process relaunch（保持现状，一个 agent 崩不带崩别人）；**k8s**=进程退出→pod 重启。**不硬编码 `os.Exit`**（会把单机多 agent daemon 全带崩）。
- **控制 + 观测**（§2.4）：
  - **挂了立刻知道**：pod/单机里 executor 都是 runtime 进程内子进程 → 直接 `wait` 句柄、进程一退即刻感知（不靠租约/轮询）；跨 daemon 重启的存活 orphan 才用 `CheckOrphan` 轮询（pod 模型消灭 orphan）。
  - **卡了检测**：`RunWatchdog`（`executor_runtime.go:346`：progress 采样 + stall 标记 + graceful-kill）——**硬化 stall 阈值** + **卡/死 → 自动接恢复链**（现在只 kill+上报，要接到 §4.4 reconcile 的档位恢复/重派）。
  - **观测流不断**：executor 生命周期 + progress 事件（v2.31.0）→ AgentActivityEvent，**保证自恢复后不断**（正是最初 bug）。可把 executor 状态汇总喂 supervisor LLM 供可见性，但**检测/恢复决策走确定性 Go**。

## 5. 推迟（真上 k8s 那步，本次不做，但保持 pod-shaped）
- ⑥ 独立 `cmd/agent-runtime` main（config→center client→runtime→Boot→tick 循环[`Tick`/`RunWatchdog`/lease/GC/heartbeat]→命令入口→优雅 drain）+ 持久卷（home/`executors/`/`tasks/`/`repos/` 跨重启）+ k8s manifest。

## 6. 部署级验收（红线：真跑，不接受单测绿当验收）
沙箱隔离：只在 /tmp 沙箱起服务，绝不碰 prod。真跑步骤：
1. 起服务、跑一个真任务让 executor 起来（有 session-id + worktree）。
2. **造死**：kill 掉 executor 进程 → 验 runtime **立刻**检测到（非等租约）+ 自动按档1 `--resume` 恢复 + 事件流可见。
3. **造卡**：让 executor 停止 progress → 验 watchdog 检测到 stall + 自动恢复。
4. **丢 session**：抹掉 Record 的 session-id → 验退档2、原 worktree 重跑。
5. **丢 worktree**：抹掉 worktree → 验退档3、从头新起。
6. **取消**：把某 executor 对应任务 discard → 验 runtime 停掉并清理该 executor。
7. **端到端自动恢复**：模拟 daemon 重部署（重启进程），验之前卡住的 executor **自动恢复**、**审计 plan-146140fc 自己往下走**（零人工）——这是 §2.1 的总验收。

## 7. 开发 plan 骨架（§4 的实现，走完整 cycle）
1. **[Dev] 去共享状态**（§4.1）+ 单测。
2. **[Dev] runtime 自建 center 连接 + list-my-tasks（不过滤全量在途）**（§4.2）+ 测。
3. **[Dev] executor session-id 持久化 + runner 可 --resume + 三档恢复阶梯**（§4.3）+ 测。
4. **[Dev] runtime 自触发 boot reconcile**（§4.4，依赖 2、3）+ 测。
5. **[Dev] 可插拔崩溃 seam + 控制/观测硬化**（§4.5，依赖 4）+ 测。
6. **[验收·真跑]** §6 全部七项。
7. **[集成] → [Gate·PD] → [Ship]**。
每个 Dev 配独立**复审**节点 + **决策**节点（pass 放行 / reject 有界 loopback 回 Dev）。

## 8. 参考
- 自包含审计（本设计的依据，全部 file:line）：见 project 记忆 / 本次调查。
- 现有恢复地基：`executor/recovery.go`（Record: BaseRef/RunnerCmd/RepoKey/SourcePath，缺 session-id）、`executor_runtime.go`（Recover/recoverExecutors/RunWatchdog/SpawnExecutor）、`boot_reconcile.go`（decideBootAction/bootReattach/bootReapRelaunch）、`agent_controller.go`（maybeAttachExecutorEngine/reattachExecutorEngineFromCache/seedExecConfig）。
- 前置快修 `b74dcb7d`（bootReattach 重挂引擎）——必要不充分，本重构把自恢复补全。
