# Agent 提示词与记忆系统改进计划

**Goal:** 修复讨论中发现的四个设计缺口：① Orchestrator 系统提示词缺少调度职能描述；② 系统提示词不落盘，无法运行时排障；③ task.model 不校验可用性，失败时不 block；④ 记忆文件硬编码 `CLAUDE.md`，耦合 Claude Code CLI。

**来源:** 2026-07-01 agent 模型设计讨论（基于实现代码 review）。

**涉及 BC:** Agent Harness（提示词组装）、Cognition（记忆）、Orchestrator（并发调度）、Model Router（F3 模型路由）。

---

## Global Constraints

- 所有提交必须通过测试（`go test ./...` + `cd web && pnpm test`），0 failures。
- 单元行覆盖率 ≥ 90%。
- 禁止 `--no-verify`。
- DDD 分层：不跨 BC 共享物理表；跨 BC 数据流走 events/RPC。
- 项目规约 § 4（零 LLM SDK）：DifficultyJudge 只是 PORT，不在仓库中引入 LLM SDK。

---

## 变更概览

| # | 变更 | 风险 | 依赖 |
|---|---|---|---|
| T1 | 系统提示词落盘 | 低（纯增量，不影响 argv 构建） | 无 |
| T2 | 记忆文件名 `CLAUDE.md` → `MEMORY.md` | 中（已有 agent 需迁移） | 无 |
| T3 | task.model 校验 + 不可用时 block_task | 中（改变 routing 语义） | 无 |
| T4 | Orchestrator 系统提示词增加调度职能描述 | 高（涉及 LLM 行为变更，需灰度） | T1 |

T1–T3 互相独立，可并行。T4 依赖 T1（提示词落盘后便于验证 T4 的提示词效果）。

---

## T1: 系统提示词落盘到 agent home

**动机:** 系统提示词是 agent 行为的核心定义，但当前是纯内存态（拼装后直接传入 `--append-system-prompt` 参数）。出了行为异常时无法从运行时产物直接确认 agent 收到了什么指令。MCP config 已有落盘先例（`mcp_config.runtime.json`）。

**Files:**

| 文件 | 动作 |
|---|---|
| `internal/cli/handlers_agentsupervisor.go` | 在 `BuildStreamingArgv` 返回后，将完整 sysPrompt 写到 `<home>/SYSTEM.md` |
| `internal/claudestream/argv.go` | `BuildStreamingArgv` 新增返回值 `sysPrompt string`，将拼装好的提示词连同 argv 一起返回 |
| `internal/workerdaemon/orchestrator/runner.go` | executor 的 system prompt 写到 `executors/<executor_id>/SYSTEM.md` |
| `internal/workerdaemon/concurrent_exec.go` | `launchExecutor` 路径上写盘 executor system prompt |

**设计要点:**
- 文件名 `SYSTEM.md`，与 `mcp_config.runtime.json` 同级，位于 agent home 根目录
- 每次启动（fresh/resume/crash-relaunch）覆盖写，保持与实际注入内容一致
- Executor 的 system prompt 写到 `executors/<executor_id>/SYSTEM.md`，与 `input.json` / `status.json` 同目录
- 写盘失败 log + continue（best-effort，不阻塞启动）

**验收:**
- [ ] agent home 下存在 `SYSTEM.md`，内容与 `--append-system-prompt` 传入值一致
- [ ] executor 目录下存在 `SYSTEM.md`
- [ ] 写盘失败不影响 agent 启动
- [ ] 单元测试覆盖 `BuildStreamingArgv` 新返回值

---

## T2: 记忆文件名通用化（`CLAUDE.md` → `MEMORY.md`）

**动机:** `CLAUDE.md` 是 Claude Code 特有约定。agent-center 已支持多种 CLI（claude-code / codex），记忆是 Cognition BC 的产物，不应耦合特定 CLI。且 Claude Code 会自动加载 `CLAUDE.md`，导致记忆被注入两次（agent-center 显式注入 + CLI 自动加载）。

**Files:**

| 文件 | 动作 |
|---|---|
| `internal/cognition/memory/path.go` | 所有 `CLAUDE.md` → `MEMORY.md`（`ScopeToFSPath` 返回值） |
| `internal/cognition/memory/skeleton.go` | skeleton 模板中的文件名 + 注释 |
| `internal/cognition/memory/runtime.go` | `HarnessContext` 中教 agent 编辑的文件名 |
| `internal/cognition/memory/path_test.go` | 期望值更新 |
| `internal/cognition/memory/skeleton_test.go` | 期望值更新 |
| `internal/cognition/memory/runtime_test.go` | 期望值更新 |
| `internal/cognition/memory/extra_test.go` | 期望值更新 |
| `internal/cognition/memory/gitops_test.go` | 期望值更新 |

**迁移策略:**
- `EnsureRootInit` 增加迁移逻辑：如果 `CLAUDE.md` 存在且 `MEMORY.md` 不存在，执行 `git mv CLAUDE.md MEMORY.md`（递归，所有 scope 目录）
- 迁移是 idempotent 的：已迁移的 repo 无 `CLAUDE.md`，跳过
- `supervisor.md` 保持不变（已经是通用名）

**设计要点:**
- 新文件名 `MEMORY.md` 而非 `memory.md`，保持全大写惯例（与 `README.md` 对齐），且在目录中醒目
- `HarnessContext` 中指导文字同步更新：`"MEMORY.md"` 替代 `"CLAUDE.md"`
- 考虑在 `.claude/settings.json` 或 `--setting-sources` 层面排除 agent memory 目录下的自动 `CLAUDE.md` 加载（scope: 如果排除不可行则接受双重加载作为 known issue，迁移到 `MEMORY.md` 后自然消除）

**验收:**
- [ ] 新 agent 的 memory repo 只有 `MEMORY.md`，无 `CLAUDE.md`
- [ ] 已有 agent 启动时自动迁移 `CLAUDE.md` → `MEMORY.md`（git history 保留）
- [ ] `HarnessContext` 输出引用 `MEMORY.md`
- [ ] 所有 scope（global / project / task / issue / conversation / worker）路径正确
- [ ] 全部测试通过

---

## T3: task.model 校验 + 不可用时 block_task

**动机:** 当前 `ResolveExecutor` 对 task.model 无条件采用（`SourceTaskOverride`），不检查模型是否在 `allowed_executors` 中或是否实际可用。fork 失败后任务处于"假运行"状态，只能等 execution lease 超时回收。应该在模型不可用时主动 block_task 并告知原因。

**Files:**

| 文件 | 动作 |
|---|---|
| `internal/workerdaemon/modelrouter/router.go` | `ResolveExecutor` path 1 增加 `allowed_executors` 校验；新增 `ErrModelNotAllowed` sentinel |
| `internal/workerdaemon/modelrouter/router_test.go` | 新增：task.model 不在 allowed_executors → error 的用例 |
| `internal/workerdaemon/orchestrator/engine.go` | `HandleWork` 区分 `ErrModelNotAllowed` 和其他错误 |
| `internal/workerdaemon/concurrent_work_available.go` | `forkOnWorkAvailable` 识别 model-not-allowed → 调 block_task |
| `internal/workerdaemon/concurrent_work_available_test.go` | 新增测试用例 |

**设计要点:**

- `ResolveExecutor` path 1 变更逻辑：
  ```
  task.model 设了 →
    若 allowed_executors 非空 且 task.model 不在其中 → ErrModelNotAllowed
    若 allowed_executors 为空（单任务模式）→ 保持现有行为（无条件采用）
    否则 → 采用（SourceTaskOverride）
  ```
- 新增 `ErrModelNotAllowed` sentinel error（`errors.Is` 可匹配）
- `forkOnWorkAvailable` 中：`HandleWork` 返回包装了 `ErrModelNotAllowed` 的 error 时，调 `block_task(task_id, reason="model <X> not in allowed_executors", reason_type="obstacle")`，而不是 log + 吞掉
- 任务被 block 后，人工修改 task.model 或 agent 的 allowed_executors，然后 unblock 即可
- 单任务模式（`allowed_executors` 为空）不受影响：保持现有的无条件 task.model 采用（由 CLI 自己在运行时报错），因为单任务模式下 agent 自己跑 pull loop，有上下文自行处理错误

**验收:**
- [ ] task.model 设为 allowed_executors 中不存在的模型 → 任务被 block（不是假运行）
- [ ] block reason 清晰说明原因（包含 task.model 值和 allowed_executors 列表）
- [ ] task.model 在 allowed_executors 中 → 正常 fork（行为不变）
- [ ] task.model 未设 → 走 judge / default 链（行为不变）
- [ ] 单任务模式下 task.model 行为不变
- [ ] router_test.go 覆盖新的 ErrModelNotAllowed 路径

---

## T4: Orchestrator 系统提示词增加调度职能描述

**动机:** 并发模式下 resident Claude session 实际承担 orchestrator 角色（任务复杂度判断、executor 调度），但系统提示词仍然是单任务模式的 pull-loop 指令（"Only ONE task runs at a time"）。这导致：
1. Claude 可能试图自己执行任务而非委派 executor
2. DifficultyJudge 接线后，Claude 需要知道自己的判断职责
3. 工具描述中 `start_task` 已经提到 concurrency cap，与系统提示词矛盾

**前置条件:** T1（提示词落盘后便于验证效果）。

**Files:**

| 文件 | 动作 |
|---|---|
| `internal/claudestream/agent_system_prompt.go` | 新增 `OrchestratorSystemPrompt` 常量（并发模式专用） |
| `internal/claudestream/argv.go` | `BuildStreamingArgv` 新增参数 `concurrencyEnabled bool`，选择注入哪个提示词 |
| `internal/cli/handlers_agentsupervisor.go` | 传递 concurrencyEnabled 标志 |
| `internal/workerdaemon/agent_controller.go` | `startSession` 传递 concurrencyEnabled |
| `internal/agentsupervisor/supervisor.go` | `Config` 增加 concurrency 标志透传 |
| `internal/claudestream/argv_test.go` | 测试两种模式下的 argv 输出 |

**设计要点:**

- **不修改** `AgentWorkQueueSystemPrompt`（单任务模式提示词保持不变）
- 新增 `OrchestratorSystemPrompt`，并发模式下**替代**（非叠加）`AgentWorkQueueSystemPrompt`
- Orchestrator 提示词核心内容：
  1. **身份**（复用 segment A）：你是 workspace 中的一个 agent，通过 `get_my_profile` 确认身份
  2. **调度职责**（替代 segment B）：你是 orchestrator（监工），不直接执行任务；通过 `list_my_tasks` 查看队列，评估每个任务的复杂度，由系统为你分派 executor 执行；你的职责是监控进度和处理异常
  3. **消息响应**（复用 segment C）：必复规则不变
- `concurrencyEnabled` 标志的传递路径：`reconcilePayload` → `startSession` → `SupervisorSessionConfig` → `handlers_agentsupervisor.go` CLI flag → `BuildStreamingArgv`
- 第一版 orchestrator 提示词应保守：描述当前 daemon 代行调度的事实（"系统自动为你分派任务到 executor"），而非假设 Claude 自己调度。待 DifficultyJudge 接线后再迭代提示词，加入"评估任务复杂度并选择 executor 模型"的指令

**Orchestrator 提示词草案:**

```
== Who you are ==
（与单任务模式相同：get_my_profile 确认身份 + @mention 规则）

== Your role: orchestrator ==
You are an ORCHESTRATOR (监工) — you coordinate and oversee, you do NOT execute tasks yourself.

Your task queue is managed by the system: when tasks arrive, the system automatically
dispatches them to isolated executors that run in parallel (up to your concurrency cap).
Each executor works independently in its own workspace with no access to your tools or
conversations.

Your responsibilities:
1. Monitor: call list_my_tasks periodically to see your queue and running tasks.
2. Blocked tasks: when an executor cannot proceed, its task is blocked — review the
   reason and help resolve it (provide input, adjust scope, or escalate).
3. You do NOT call start_task or complete_task for executor-managed tasks — the system
   handles the lifecycle. You may still use block_task to flag an obstacle you notice.
4. Deferred tools: lower-frequency tools (plans, issues, files, etc.) load on demand
   via search_tools — the same rule as single-task mode.

== Messages directed at you ==
（与单任务模式相同：必复规则 + get_my_unread + mark_seen）
```

**验收:**
- [ ] 并发模式 agent 的 `SYSTEM.md` 包含 orchestrator 角色描述
- [ ] 单任务模式 agent 的 `SYSTEM.md` 与之前完全一致（无回归）
- [ ] "Only ONE task runs at a time" 不出现在并发模式的提示词中
- [ ] Orchestrator 提示词不提及不存在的工具（如 fork_executor）
- [ ] 单元测试验证两种模式的提示词选择逻辑

---

## 后续（Out of Scope）

以下主题在讨论中涉及但不在本计划范围内，记录为后续 backlog：

1. **DifficultyJudge 接线**：将 `nil` judge 替换为真正的 LLM reasoning port，让 orchestrator Claude 评估任务复杂度并选择 `{cli, model}`。需要先完成 T4（orchestrator 提示词），然后设计 judge 工具 / 推理协议。
2. **Orchestrator 调度工具**：暴露 `fork_executor` / `list_executors` / `get_executor_result` 等 MCP 工具，让 Claude 真正参与调度（取代 daemon 全权代理）。依赖 DifficultyJudge 接线。
3. **`start_task` tool description 动态化**：根据单任务/并发模式动态调整工具描述，消除与系统提示词的措辞不一致。
