# Prompt 组装

派单时，agent 子进程收到的 prompt 由几个不同来源的内容**分层叠加**而成。本文说明分层、来源、责任归属。

## 层次

```
agent-center 注入的部分:
┌─────────────────────────────────────────┐
│ worker-agent.md (skill, 通用)          │ ← 教 agent 怎么用 request-input / report-progress / open-issue / 等
├─────────────────────────────────────────┤
│ relevant supervisor memory (per-task)  │ ← supervisor 觉得相关的记忆片段
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
| worker-agent.md skill | agent-center 项目（bundled with binary） | `${install_dir}/skills/worker-agent.md` |
| supervisor memory | supervisor 历史决策、人工录入 | DB 表 `agent_memory` |
| constraints_extra | supervisor 派单时附加 | dispatch envelope 内 |
| task.prompt | supervisor 加工后 | DB 表 `task` |
| 项目本地约定（CLAUDE.md 等） | 用户在项目仓库维护 | 项目自己的 git 仓库 |

参见 [ADR-0005 Charter 留在项目仓库](../decisions/0005-project-charter-stays-in-project-repo.md)。

## 责任归属

**决定：Worker daemon 负责组装 agent-center 那部分的 prompt**（不是 supervisor）。项目本地约定层由 agent CLI 自己处理。

| 角色 | 拿到的信息 | 适合做 prompt 组装吗 |
|---|---|---|
| Supervisor | task 的"意图"、应注入的 memory 选择、project_id | ❌ 不知道目标 agent CLI 的格式细节 |
| Worker daemon | 派单 envelope + 本机 agent CLI 知识 | ✅ 知道本机跑什么 CLI、每种 CLI 如何吃 system prompt / skill |
| Agent CLI 自己 | worktree cwd | ✅ 读项目本地约定文件（CLAUDE.md 等）天经地义 |

## Dispatch Envelope

Supervisor 派单时发的载荷（载荷小，不含 charter / skill 原文）：

```yaml
task_id: 42
project_id: "agent-center"
target_worker: "mac-mini-1"
target_agent_cli: "claude-code"
user_intent: "...原始用户表述..."          # 给 worker / agent 看的原始意图
task_prompt: "...supervisor 加工后指令..." # 真正动作指令
memory_refs: [mem_id_3, mem_id_8]         # supervisor 觉得相关的 memory id
constraints_extra: "...本次额外约束（如有）..."
```

## Worker daemon 拼装流程

1. 用 `memory_refs` 向 server 拉对应 memory 内容
2. 装载 bundled `worker-agent.md` skill（随 binary 一起发，文件在 `${install_dir}/skills/worker-agent.md`）
3. 按上面的"层次"顺序拼出 `final_prompt`
4. `cd` 到 worktree 路径
5. 调对应 agent adapter spawn 子进程：
   - `claude-code` adapter: `claude --output-format stream-json --session-id=<agent_session_id> -p "$final_prompt"`
   - `codex` adapter: codex 的等价参数
   - `opencode` adapter: 同上

## 好处

- Supervisor 不必关心 agent CLI 差异；它的职责是"决定干什么 / 用哪台 / 用哪种 agent / 注哪些记忆"
- 派单 envelope 小，跨网络通信便宜
- 项目本地约定由 agent 自取，无 cache / 同步问题
- Adapter per agent CLI 在 worker 侧集中管理
