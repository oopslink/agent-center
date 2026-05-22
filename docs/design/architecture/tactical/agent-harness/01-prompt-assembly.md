# Prompt 组装（Worker 侧）

> **DDD 战术层** · 主题: agent-harness

派单时，**worker agent 子进程**收到的 prompt 由几个不同来源的内容**分层叠加**而成。本文说明 worker 侧的分层、来源、责任归属。

> **Supervisor 自己**（center 上的 agent）有一套独立的 prompt 组装机制，跟本文 worker 侧的方案**两套独立**。详见 [cognition/00-overview.md § 7.4 Prompt 组装边界](../cognition/00-overview.md)。

## 层次

```
agent-center 注入的部分:
┌─────────────────────────────────────────┐
│ worker-agent.md (skill, 通用)          │ ← 教 agent 怎么用 request-input / report-progress / open-issue / 等
├─────────────────────────────────────────┤
│ agent_instance.instructions.md          │ ← agent-level（per AgentInstance home_dir, [ADR-0024])
├─────────────────────────────────────────┤
│ constraints_extra (per-task, 可选)      │ ← 本次额外约束（supervisor 加，如有）
├─────────────────────────────────────────┤
│ task.prompt (本次具体要求)              │ ← supervisor 加工后的指令
└─────────────────────────────────────────┘
   ↓
agent CLI 一次性吃下作为 system + user prompt

项目本地约定（CLAUDE.md / AGENTS.md / 等）:
   由 agent CLI 自己在 worktree cwd 加载，agent-center 不接触
```

各层来源：

| 层 | 谁产生 | 何处存储 |
|---|---|---|
| `worker-agent.md` skill | agent-center 项目（bundled with binary） | `${install_dir}/skills/worker-agent.md` |
| `agent_instance.instructions.md` | 用户为该 AgentInstance 配置 | `~/.agent-center-worker/agents/<agent_instance_id>/instructions.md`（[workforce/04-agent-instance.md § 3](../workforce/04-agent-instance.md)）；execution 期间只读 |
| `constraints_extra` | supervisor 派单时附加 | dispatch envelope 内 |
| `task.prompt` | supervisor 加工后 | DB 表 `tasks` |
| 项目本地约定（`CLAUDE.md` 等） | 用户在项目仓库维护 | 项目自己的 git 仓库 |

参见 [ADR-0005 Charter 留在项目仓库](../../../decisions/0005-project-charter-stays-in-project-repo.md)。

**Supervisor 的 Memory 不注入到 worker prompt**：[ADR-0012](../../../decisions/0012-memory-file-based.md) 后 Memory 是 supervisor 的私事（存 `$AGENT_CENTER_MEMORY_DIR/` git 仓，在 center 机器上），worker 拿不到也不应拿到。supervisor 想给 worker 共享上下文 → 把要分享的事实**显式**写进 `task.prompt` 或 `constraints_extra`，由派单 envelope 带过去。

## 责任归属

**决定：Worker daemon 负责组装 agent-center 那部分的 prompt**（不是 supervisor）。项目本地约定层由 agent CLI 自己处理。

| 角色 | 拿到的信息 | 适合做 prompt 组装吗 |
|---|---|---|
| Supervisor | task 的"意图"、project_id；想分享给 worker 的事实需要显式写进 envelope 字段 | ❌ 不知道目标 agent CLI 的格式细节 |
| Worker daemon | 派单 envelope + 本机 agent CLI 知识 | ✅ 知道本机跑什么 CLI、每种 CLI 如何吃 system prompt / skill |
| Agent CLI 自己 | worktree cwd | ✅ 读项目本地约定文件（`CLAUDE.md` 等）天经地义 |

## Dispatch Envelope

Supervisor 派单时发的载荷（载荷小，不含 charter / skill 原文）。详细字段见 [task-runtime/02-task-execution § 5 字段 / DispatchEnvelope](../task-runtime/02-task-execution.md)，相关 prompt 部分摘录：

```yaml
task_id: ...
project_id: "agent-center"
worker_id: "..."
agent_instance_id: "..."                        # 强引用 AgentInstance（[ADR-0024](../../../decisions/drafts/0024-agent-instance-first-class.md)）；worker daemon 据此 join 出 agent_cli + home_dir
task_title: "..."
task_description: "...supervisor 加工后指令..."  # 真正动作指令；≤10KB 内联，超过走 task_description_blob_ref
task_description_blob_ref: "..."                # BlobStore 引用（[conventions § 8](../../../../rules/conventions.md)）
extra_skill_files: ["..."]                      # 可选，本次额外注入的 skill 路径
```

不含 `memory_refs` 字段 —— 上文已说明 supervisor memory 不跨进程注入。

## Worker daemon 拼装流程

1. 装载 bundled `worker-agent.md` skill（随 binary 一起发，文件在 `${install_dir}/skills/worker-agent.md`）
2. 通过 `agent_instance_id` 解析本机 AgentInstance：拿到 `agent_cli`、`home_dir = ~/.agent-center-worker/agents/<id>/`
3. 装载 `agent_instance.instructions.md`（位于 `home_dir/instructions.md`；不存在则跳过该层）
4. 如 envelope 带 `extra_skill_files`，按列表拼上去
5. 按"层次"顺序组合：`worker-agent.md skill + agent_instance.instructions.md(若有) + constraints_extra(若有) + task_description (或从 blob_ref 取)` → `final_prompt`
6. `cd` 到 worktree 路径（`workspace_mode=worktree` 时为 `base_path + ".wt/task-<execution_id>"`；`workspace_mode=direct` 时为 `base_path`）
7. 调对应 agent adapter spawn 子进程：
   - `claude-code` adapter: `claude --output-format stream-json --session-id=<execution_id> -p "$final_prompt"`
   - `codex` adapter: codex 的等价参数
   - `opencode` adapter: 同上

> **home_dir 在 execution 期间只读**：worker daemon 读 instructions.md / mcp_config / skills 一次性拼进 prompt；agent 进程不直接读写 home_dir（avoid 并发 race，详见 [workforce/04-agent-instance.md § 3.2](../workforce/04-agent-instance.md)）。

## 好处

- Supervisor 不必关心 agent CLI 差异；它的职责是"决定干什么 / 用哪台 / 用哪种 agent / 把哪些事实显式写进 task_description"
- 派单 envelope 小，跨网络通信便宜
- 项目本地约定由 agent 自取，无 cache / 同步问题
- Adapter per agent CLI 在 worker 侧集中管理
- Supervisor 自己的 prompt 不跟 worker prompt 混 —— 边界清晰
