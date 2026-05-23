# 概述

## 项目目标

构建一个 **agent 调度中心**，统一管理用户在多台机器上的 agent CLI 工具（claude code、codex、opencode 等），通过 Web Console + CLI 接收任务、按项目维度归集、按任务粒度调度。

## 核心定位

| 维度 | 决定 |
|---|---|
| 目标用户 | 个人（单用户），不做团队 / 多租户 |
| 任务入口 | Web Console + CLI（v2: vendor 接入撤回 per [ADR-0031](../decisions/drafts/0031-v2-drop-bridge-vendor-integration.md)；v3+ 重新设计 Bridge）|
| 组织维度 | 项目（Project）—— 任务永远归属一个项目，无散单 |
| 部署形态 | VPS 常驻 Center；Worker 在用户的开发机上主动连回 |
| 中心智能 | LLM 驱动（AI native），但走 CLI（claude code）不走 LLM SDK |
| 权威模型 | 中心是任务的唯一权威；Worker **不能造任务**，只能开 **Issue（议题）** 让中心 / 用户讨论后决定是否产生 Task |
| 项目领域 | 项目类型不限于代码（编程 / 写作 / 投研 / ...）；项目本地约定文件（`CLAUDE.md` / `AGENTS.md` / `README.md` 等）由 agent CLI 在 worktree 中**自然加载**，agent-center 不管理 |
| 隔离方式 | 默认 git worktree per task |
| 存储 | SQLite v1，dialect-agnostic 写法，未来可换 PG |
| 用户接入面 | Web Console (React SPA + Go API + SSE)，仅 loopback；CLI（兄弟前端）；详见 [ADR-0037](../decisions/drafts/0037-web-console-as-main-user-ui.md) + [ADR-0038](../decisions/drafts/0038-cli-ux-enhancement.md) |
| 工具调用 | Skill + CLI（不引入 MCP；理由见 [ADR-0001](../decisions/0001-no-mcp.md)） |
| 监管者命名 | **Supervisor**（不是 Brain；它也是 agent，只是角色为监管 / 调度。理由见 [ADR-0003](../decisions/0003-supervisor-not-brain.md)） |

## 原始诉求拆解

> "agent 调度中心，可以调度不同机器上的 agent（如 codex，claude code， opencode 等）干活。可以指派任务给 agent。agent 也可以创建、修改、删除任务。"

逐句拆：

- "调度中心" → 一个集中协调的实体，区别于 agent 之间自治
- "不同机器上的 agent" → 物理分布式的执行端
- "如 codex / claude code / opencode" → 复用现成 CLI agent 工具，不自造 LLM 调用
- "指派任务给 agent" → 中心 → agent 的派单是基本操作
- "agent 也可以创建、修改、删除任务" → 需要澄清的语义（见下）

## "Agent 创建 / 修改 / 删除任务"的语义重定义

原话隐含了让 worker agent 自治造任务的模型。讨论中重定义为：

| 字面诉求 | 经讨论的实现 |
|---|---|
| Agent 创建任务 | Agent **开一个 Issue**（议题）→ 经用户 / supervisor 讨论 → 结论可能产生 0 / 1 / N 个 Task |
| Agent 修改任务 | Agent 通过 A2A 状态机更新**自己负责的任务**的状态、进度、产物；不能改别人的任务、不能改任务的派单对象 |
| Agent 删除任务 | 不允许。Agent 可标记任务 `failed` / `canceled`，但销毁 / 隐藏是中心的职责 |

**指导原则：单一来源、无野任务**。任何对任务图的修改都经 supervisor 决策、有审计血缘。

参见 [ADR-0004 Issue 而非 Suggestion](../decisions/0004-issue-not-suggestion.md)。
