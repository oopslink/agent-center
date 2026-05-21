# 竞品分析：agent-center vs Multica / Slock

> **类别**：研究 / 调研
> **日期**：2026-05-21
> **范围**：两个对照样本 —— [Multica](https://multica.ai) (OSS managed agents platform) + [Slock](https://slock.ai) (human-agent collaborative messaging)
> **不是**：完整市场扫描；不覆盖 LangGraph / CrewAI / AutoGen 这类开发者框架，也不覆盖 Devin / OpenHands 这类自治编码 agent

---

## § 0. TL;DR

| 维度 | agent-center | Multica | Slock |
|---|---|---|---|
| **形态** | 个人多 agent 中控（VPS 常驻） | 多租户 SaaS 看板平台 | 人 + agent 协作的消息平台 |
| **触达界面** | 飞书（IM-first） | Web + Electron 桌面 | 类 Slack chat（自有 web + CLI） |
| **调度核心** | **LLM Supervisor 读上下文决策** | 人工指派 + Autopilot cron + @agent 触发 | 对等协作（@mention + claim） |
| **agent 生命周期** | 短命（spawn/dispatch/exit） | 任务级会话恢复（session_id 续接） | 持久参与者（绑定 server+computer，sleep/wake） |
| **执行体** | CLI agent（claude / codex / opencode） | 同（11 个 CLI） | 同（agent 进程独立运行，订阅消息） |
| **状态权威** | Center 单一，Worker 无野任务 | server + workspace 隔离 + RBAC | 分布式参与者，消息层是真理 |
| **存储** | SQLite + 文件 Memory | PostgreSQL + pgvector | 后端不可见（云托管为主） |
| **目标用户** | 个人开发者 | 团队（2-50 人） | 个人到团队 |

**一句话定位差异**：
- **Multica** 是给团队的"agent 看板平台"，把 agent 塞进 Linear/Jira 式工作流
- **Slock** 是给"人和 agent 共用的 Slack"，把任务塞进对话流
- **agent-center** 是给个人的"agent 调度官"，把决策塞进飞书让 supervisor 替你想

三者在"用 agent 替人干活"上完全重叠，但**形态层、组织层、调度层全部不同 —— 不是同一物种**。

---

## § 1. 竞品速写

### 1.1 Multica

> 数据来源：本地仓 `/Users/oopslink/works/codes/oos/multica`（README + docs/product-overview.md + AGENTS.md，截止 2026-04-21）

**一句话**："Your next 10 hires won't be human" —— 把编码 agent 变成同事，给它分任务、追进度、沉淀技能。

**架构**：
```
Next.js / Electron (web + desktop)
        │
        ↓  REST + WebSocket
Go backend (Chi + sqlc + gorilla/websocket)
        │
        ↓
PostgreSQL 17 + pgvector
        ↑
Agent Daemon (用户机器，每 3s poll，15s heartbeat)
  └─ 自动探测 11 个 CLI：claude / codex / openclaw / opencode /
      hermes / gemini / pi / cursor-agent / kimi / kiro-cli / copilot
```

**核心概念**（每条都有对应 DB 表）：

| 概念 | 释义 |
|---|---|
| **Workspace** | 多租户隔离边界，issue/agent/skill 全部 scope 进 workspace |
| **Issue** | 核心工作对象（Linear 的 Issue），可分配给 member 或 agent —— **polymorphic actor** |
| **Agent** | 带 profile 的工作者：name + avatar + instructions + runtime + provider + skills + MCP + max-concurrent |
| **Runtime** | "agent 跑在哪台机器"，daemon = 用户机上的 runtime |
| **Skill** | workspace 级可复用文档，agent 启动时挂载到工作目录 |
| **Autopilot** | cron 或 webhook 触发，自动建 issue + 派 agent |
| **Squad** | agent 团队，leader agent 负责把 issue 派给成员（`@FrontendTeam`） |
| **Chat** | 跟 agent 的持久多轮对话（不依附 issue） |
| **Session Resumption** | 同 `(agent, issue)` 下次复用 Claude Code 的 `session_id` + 工作目录 |

**部署形态**：Multica Cloud（官方托管）+ 自托管（docker-compose，server + Postgres + Daemon）

**典型工作流**：
1. 用户在板子上 `New Issue`，assignee 选某个 agent
2. 该 agent 的本地 daemon 在 3 秒轮询里认领，启动对应 CLI 进 worktree
3. agent 的 stdout/stderr 通过 WS 实时推回 web，进入 issue 评论时间线
4. 完成时上报 session_id + work_dir + token 用量
5. 用户在评论里 `@agent` 追加请求 → 触发同 session 的新一轮 task

**它的核心赌注**：
- **Polymorphic actor**（一个表的 actor_type ∈ {member, agent}）—— 这是它能"把 agent 当人"的唯一根因
- **Issue = 任务单元**，分配即派单，简单直观
- **Skill 文件化**，挂在 agent 上，跨任务复用
- **没有 LLM 决策层** —— 调度靠人工指派或 cron，agent 只负责执行
- **多租户从 day 1 就在**，workspace + auth + PAT + daemon token 一套齐

### 1.2 Slock

> 数据来源：[slock.ai](https://slock.ai)（公开页面信息极少）+ 第一手运行时规格（本 agent 即跑在 Slock 上，规格来自系统 prompt）

**一句话**：人 + AI agent 共用的协作消息平台 —— 把任务编织进对话流，agent 是 channel 里的一等参与者。

**架构**（推断 + 第一手）：
```
chat 服务端（slock.ai 托管，可能也支持私有部署）
        │
        ↓  消息总线（channels / DMs / threads / tasks / reminders）
        ↑
slock CLI（在每台 computer 上）
        │
        ├─ 把人接进来（Web / 客户端）
        └─ 把 agent 接进来（agent 进程 + MEMORY.md workspace）
            agent 是"绑定到 server+computer 的持久身份"
            sleep / wake 由消息事件驱动
```

**核心概念**：

| 概念 | 释义 |
|---|---|
| **Server** | 一个 Slock 实例，包含 channels 和成员 |
| **Channel** | `#name`，公开/私密，agent 可以 join/leave |
| **DM** | 1:1 私聊（也支持 thread） |
| **Thread** | 任意消息可派生 thread（`#channel:msgshortid`），不可嵌套 |
| **Task-message** | 任意 top-level 消息可升级为 task，带 status (`todo` / `in_progress` / `in_review` / `done`) + assignee + claim 锁；**status 与 assignee 独立** |
| **Agent** | 跟 human 平等的参与者；有稳定 @handle，绑到 `(server, computer)`；workspace 目录持久（含 MEMORY.md） |
| **Computer** | agent 真正运行的物理机器（多个 computer 可跨在不同主机） |
| **Reminder** | 作者所有的持久 wake-up signal，可锚定到 message/thread |
| **Action Card** | 给人点击执行的命令卡（B-mode quick-commit shortcut） |

**典型工作流**：
1. 人在 `#engineering` 发消息 `@agent-X 帮我处理一下…`
2. agent 进程收到消息事件被唤醒（如果在 sleep）
3. agent 用 `slock task claim` 锁定 task，进入 `in_progress`
4. agent 在 thread 里发进度更新
5. 完成后 `slock task update --status in_review`，人审查后置 `done`
6. agent 把学到的偏好 / 偏差写回 `MEMORY.md` 等下次

**它的核心赌注**：
- **Chat-as-coordination**：所有协调（任务、决策、提问、确认）都在消息流里发生，没有独立看板
- **Agent = 持久身份**，不是无状态 worker；MEMORY.md 跨次累积
- **多 computer**：agent 散落在用户的多台机器，消息层是统一界面
- **没有中央调度官**：work 靠 @mention + claim 自组织，类比 Slack 群协作
- **平等性**：human 和 agent 在 UI 上完全平权 —— @handle / profile / 反应 / 提及 全一样

---

## § 2. 三方维度对比（速查表）

> 维度按 agent-center 的设计决策关键度排序

| # | 维度 | agent-center | Multica | Slock |
|---|---|---|---|---|
| 1 | **核心定位** | 个人 agent 调度官 | 团队 agent 看板平台 | 人/agent 协作 chat |
| 2 | **目标用户** | 单人 | 团队 (2-50) | 个人 → 团队 |
| 3 | **触达 UI** | 飞书 IM（v1 唯一） | Web + Electron 桌面 | 自有 chat UI + CLI |
| 4 | **决策机制** | **LLM Supervisor 读上下文** | **人工指派 + cron** | **@mention + claim** |
| 5 | **任务单元** | Task / TaskExecution 两层 | Issue（统一） | Task-message（消息+元数据） |
| 6 | **任务归属** | 必属 Project | 可属 Project（可选） | 属 Channel（无 project 实体） |
| 7 | **任务来源** | 仅 Supervisor / User 可建 | User / @comment / Autopilot / Agent 都可建 | 任意 message 升级 |
| 8 | **agent 生命周期** | 短命（每任务 spawn / exit） | session 跨任务续接 | 持久参与者（sleep/wake） |
| 9 | **agent 身份** | 角色（worker-agent skill） | 配置实体（name+provider+runtime） | 第一公民（@handle + profile） |
| 10 | **Memory 模型** | 文件 + git 版本（scoped） | session_id 续接（CLI 内置） | MEMORY.md 工作空间 + 笔记 |
| 11 | **审计/可见性** | Conversation + DecisionRecord + Events | activity_log + timeline | chat 消息流即审计 |
| 12 | **执行体（运行 agent CLI）** | Worker daemon spawn 进程 + per-execution shim | Daemon poll + 启动 CLI | agent 进程独立运行，订阅消息 |
| 13 | **支持的 CLI agent** | claude（done） + codex/opencode（stub） | 11 个全自动探测 | 任意（agent 自己写代码即可） |
| 14 | **多 agent 协调** | Center 仲裁，Worker 无野任务 | Squad（leader 派给成员） | 对等，@mention 互相调 |
| 15 | **discussion / 议事** | Issue（独立 BC）+ 收敛拍板 | 评论区 + @agent 触发新 task | thread + 任意 message |
| 16 | **可观测/Trace** | events 表 + AgentTrace 归档 BlobStore | Activity + token 用量 + runtime usage | 消息流 + reactions |
| 17 | **后端栈** | Go + SQLite (modernc, no CGo) | Go (Chi+sqlc+WS) + PostgreSQL+pgvector | 不可见 |
| 18 | **认证 / 多租户** | **无**（v1 SSH 进 VPS 跑 CLI） | OAuth + PAT + daemon token + RBAC + workspace 隔离 | 平台层处理 |
| 19 | **部署** | 单 VPS + Worker 散在用户机 | Cloud（官方） + 自托管 docker | 云托管（自托管未知） |
| 20 | **代码状态（2026-05-21）** | Phase 2 完成（~33k LoC / 89.9% 覆盖），P3-P7 ahead | 已上线，monorepo 成熟 | 已上线，公开 |
| 21 | **开源** | 是（个人项目） | 是（modified Apache 2.0） | 闭源（推断） |
| 22 | **商业模式** | 无 | OSS + Cloud + 自托管商业支持 | SaaS（推断） |

---

## § 3. 按 agent-center 五大策略立场对照

> 本节是核心。每条立场（S1-S4 + ADR-0002）都是 [00-domain-vision.md](../design/architecture/strategic/00-domain-vision.md) 的"反常识赌注"。下面对照 Multica 和 Slock，看你押对没、押错没、押重了没。

### 3.1 S1 调度逻辑 prompt 化

> agent-center：派单 / 重派 / Issue 收敛 / 跨任务协调全由 Supervisor agent 读上下文决策；**不写硬编码 scheduler，不维护策略配置表，不上 DAG 引擎**。

| | 怎么做的 |
|---|---|
| **agent-center** | 事件触发 → spawn `claude` 子进程跑 `supervisor.md` skill → agent 通过 Bash 调 `agent-center dispatch / kill / issue conclude` 落决策 |
| **Multica** | 三种触发：①人在 UI 手动 assign；②`@agent` 评论；③Autopilot cron。**没有 LLM 调度层** —— 派单纯靠"谁被指派" |
| **Slock** | 没有中央调度。Work 通过 `@mention` 到达，agent 自己 `slock task claim`。Agent 之间不互派 |

**点评**：
- **真正押注 LLM 调度的只有你一家**。Multica 把"分谁"留给人，Slock 把"分谁"留给 @mention 协议。你是唯一让 LLM 替人做"该派谁、该派几次、出错怎么 reschedule"的。
- **风险**：调度质量 = supervisor.md prompt 质量。Phase 6 的核心难点全压在 prompt 上，**没有 fallback 调度逻辑**。Multica 这种"人工指派"虽然朴素，但在 prompt 翻车时是稳态。
- **好处**：Multica 的 Autopilot 是 cron + 规则，新场景需要写规则；agent-center 在新场景下，supervisor 读上下文自适应，配置成本接近零。
- **可以借鉴**：Multica 的 Autopilot **触发器** 概念（cron + webhook）跟 supervisor 不冲突，可以作为"事件源 → supervisor wake"的一种 trigger 加入。

### 3.2 S2 Supervisor 短生命周期 + 文件 Memory

> agent-center：Supervisor 不是常驻 daemon，事件触发 spawn 一次就退出；跨次记忆走 markdown 文件，每 scope 一个 `CLAUDE.md`，存在 `$AGENT_CENTER_MEMORY_DIR/`，**不进 DB**。

| | 怎么做的 |
|---|---|
| **agent-center** | Supervisor 每次事件 spawn 新 `claude` 进程；scope memory (task / issue / conversation / worker / project / global / supervisor) 全部走 markdown 文件 + git 版本 |
| **Multica** | **Session resumption**：同 `(agent, issue)` 下次复用 Claude Code 自带的 `session_id` 和工作目录，CLI 内部续接对话状态；不暴露文件层 memory |
| **Slock** | **持久参与者 + workspace 目录**：每个 agent 有 MEMORY.md + notes/ 目录，跨次累积；agent 被 sleep/wake 但身份和状态都活着 |

**点评**：
- 三家其实**都在解"agent 不要每次失忆"** —— Multica 靠 CLI 自带 session，Slock 靠 agent workspace 目录，agent-center 靠 scope memory 文件 + git。
- **agent-center 的特点**：memory 是 **supervisor 的脑**，**worker agent 自己也有 CLAUDE.md 但走的是 worker 项目里的**（不是 center 管的）。这种"supervisor 脑 vs worker 脑"分离是别家没有的。
- **风险**：scope memory 多了会膨胀，且 supervisor 每次都要决定"加载哪几个 scope" —— 这是新的 prompt 调优维度，Multica 因为靠 CLI session 完全规避了。
- **可以借鉴**：Multica session_id 续接思想 —— 你的 worker agent 是不是也可以 `(worker_agent, task)` 复用 CLI 的 session_id，而不是每次 fresh start？现在的 Worker agent 模型见 [agent-harness/01-prompt-assembly](../design/architecture/tactical/agent-harness/01-prompt-assembly.md)，看下是否合适加入。

### 3.3 S3 对话即决策审计

> agent-center：Supervisor 每一步关键决策都经 IM（飞书）对用户可见 + 可干预；**不藏后台、不静默调度**。

| | 怎么做的 |
|---|---|
| **agent-center** | 决策走飞书卡片；Conversation BC + Message 内部存储，Bridge 翻译为飞书卡片；Issue/Task ↔ Conversation 1:1 |
| **Multica** | 决策 = "谁被指派"，全在 web 上可见；issue 详情页有 timeline（mix `activity_log` + `comment`）；但**调度本身不需要审计**（因为是人指派的） |
| **Slock** | **极致版** —— 所有协调动作都是 chat 消息，没有"非消息形式的决策"。审计 = 翻消息流 |

**点评**：
- **Slock 是这条路线走得最远的** —— 它直接消灭了"决策 vs 沟通"的区分。
- 你和 Slock 都把"决策可见"当核心，但 Slock 是**因为没有决策中心所以全在 chat**，agent-center 是**因为有 LLM 决策中心所以必须 surface 出来让人监督**。
- **Multica 反而是最弱的**：调度可见但不可干预（人指派是一锤子买卖，没有"中途接管"的概念）；它的"可见"只是 timeline 回看。
- **可以借鉴**：Slock 的 reactions（emoji 反馈）是极低成本的"决策反馈" —— 用户对一条 supervisor 提议 emoji 一下就是 +1/-1，比卡片按钮还轻。你的飞书卡片可以加快速反应交互。

### 3.4 S4 Center 单一权威

> agent-center：Worker / agent **不主动造任务、不互相派单**；所有任务来源由 Center 仲裁，Worker 想要新工作只能开 Issue 让 Center 拍板。

| | 怎么做的 |
|---|---|
| **agent-center** | Worker agent 无 `create-task` 工具；只有 `open-issue`；只有 supervisor / user 能 dispatch task |
| **Multica** | **完全反过来** —— agent 是 polymorphic actor，**有完整权限**创建 issue、评论、订阅，甚至能 `@` 其他 agent 派活；Squad 里 leader agent 直接派给成员 |
| **Slock** | **介于两者之间** —— agent 可以发消息、可以创建 task-message、可以 @ 其他 agent，但没有"派单权" —— 派给谁取决于 @mention 给谁 |

**点评**：
- **三家在这条上分歧最大**。你是最严格的中央集权；Multica 是 agent peer 平权；Slock 是消息平权但无派单概念。
- **你的逻辑链**：thesis 是"减少认知负担"→ S4 是必要 —— 如果 agent 能互相派活，**用户又得肉眼盯**了。这条逻辑链对个人用户成立。
- **Multica 的逻辑**：团队场景下，让 leader agent 派活是"延长团队的手" —— 不会增加用户认知负担，因为本来就是团队多人协作。所以**这条对它不适用**。
- **风险**：Center 单一权威意味着 supervisor 翻车 = 全停。Multica 的去中心化模型在一个 agent 故障时其他 agent 不受影响。
- **战略含义**：S4 是你区分自己**不是 Multica** 的最强信号 —— 别误把 Multica 的"polymorphic actor"当好东西照搬，那会把你的 thesis 拆掉。

### 3.5 ADR-0002 不引入 LLM SDK，走 CLI agent

> agent-center：Supervisor 与 worker agent 都通过 spawn 现成 CLI agent（默认 claude code）实现，**零 LLM SDK 依赖**。

| | 怎么做的 |
|---|---|
| **agent-center** | 全 spawn CLI，center 一行 anthropic SDK 不依赖；可换大脑 |
| **Multica** | **完全相同** —— 自述"不自己训模型，也不锁定某一家厂商；Multica 是调度器"。Daemon 自动探测 11 个 CLI |
| **Slock** | **不适用** —— Slock 是消息平台，不关心 agent 用什么实现 LLM 调用 |

**点评**：
- **这条你和 Multica 完全同道** —— 都走 CLI agent 路线，都不锁单一厂商。
- **Multica 探测的 CLI 比你多得多**（11 vs 3：claude + codex stub + opencode stub）。**这是你能直接照搬的 —— agentadapter 多写几个**。
- 但要注意：Multica 的 11 个 CLI 里很多是简单 wrap（参数注入 + JSONL 解析），不复杂；麻烦的是各家 headless 输出格式不一致，adapter 层得吃这部分。

---

## § 4. agent-center 的差异化与重叠

### 4.1 真差异化（独有赌注）

| 项 | 唯一性 | 解释 |
|---|---|---|
| **LLM Supervisor 调度** | ⭐⭐⭐ 唯一 | Multica 用规则，Slock 用 @mention；你唯一让 LLM 替人决策 |
| **Center 单一权威 + 无野任务** | ⭐⭐⭐ 唯一 | Multica/Slock 都让 agent 平权造任务 |
| **DDD 战略 + 21 ADR + 7 BC** | ⭐⭐ 工程哲学独特 | Multica 是 product-first monorepo，Slock 设计不可见 |
| **per-execution shim**（ADR-0018） | ⭐⭐ 设计点独特 | Multica 用"5min orphan reclaim"做容错 |
| **Issue ↔ Conversation 1:1 + Task ↔ Conversation 1:1** | ⭐⭐ 独特对称模型 | Multica 把 comment 当 comment，没拉平到统一 Conversation |
| **文件 Memory + git 版本** | ⭐ 独特但 Slock 接近 | Multica 走 CLI session，Slock 走 agent workspace |

### 4.2 重叠区（同质化风险）

| 项 | 对手 | 差异 / 风险 |
|---|---|---|
| **CLI agent 调度** | Multica | 都 spawn CLI；Multica adapter 数量碾压你 |
| **本地 Worker daemon + 远端 server** | Multica | 拓扑几乎一样；Multica 是 pull (poll) 模式，你是 push (gRPC 长连接) 模式 |
| **agent profile / instruction 注入** | Multica | Multica 把 agent 当配置实体（name + instructions + skills + MCP）；你的 worker 通过 dispatch envelope + skill 文件实现等效，但**没有"agent 配置实体"显式 first-class**化 —— Multica 这套对 v2 团队场景是稳态 |
| **任务状态机 / Activity / Timeline** | Multica + Slock | 三家都有；你的 6 态 TaskExecution 最复杂（因为有 input_required + killed 区分），Slock 4 态最简单 |

### 4.3 可借鉴（值得照搬的设计）

> 这些点跟你的 thesis 不冲突，可以直接吸收。

1. **Multica 的 11 个 CLI agentadapter 矩阵** —— `internal/agentadapter/` 多写几个，让用户能用 codex/opencode/gemini，扩大用户面。低成本。
2. **Multica 的 MCP 服务器注入**（per-agent mcp_config 字段）—— `agent.mcp_config` (JSONB) 直接进 dispatch envelope，让 worker agent 启动时把 mcp servers 注进 CLI。**Phase 2 的 dispatch envelope 可以补这块**。
3. **Multica 的 Skill 挂载**（workspace skill → 启动时挂到 agent 工作目录）—— 这其实是 Anthropic Skills 的标准实践；你的 worker agent 想要"复用知识"时可以借鉴这种 file-mount 模型，而不是塞进 prompt。
4. **Slock 的 reactions** —— 飞书卡片之外可以加 emoji 反馈机制做"轻量决策投票"。
5. **Slock 的 reminder** —— 你的 Supervisor 想"5 分钟后回来看一下任务"的能力 = reminder。当前 supervisor 短命模型必须靠"重新被事件唤醒"实现这件事，加 reminder 机制后 prompt 更自然。
6. **Multica 的 Autopilot cron 触发** —— 你的"事件触发"目前只覆盖领域事件，加 cron 后能解锁"每天早上 9 点让 supervisor 复盘所有 issue"这类用法。

---

## § 5. 战略选择建议

> 基于 v1 [Vision § 3 边界](../design/architecture/strategic/00-domain-vision.md#-3-边界boundaries)（个人工具 + 不接 git）的总开关。

### 5.1 不要去争的事

- **不要做团队多租户**。这是 Multica 的主战场，跟你的 vision B2 (v1 不是 SaaS) 冲突，走那条要"基本重新做项目"。
- **不要做"all-in-one chat 平台"**。这是 Slock 的主战场，你的优势是**借用 IM**（飞书）而不是自建。继续把 Bridge BC 当 ACL 而不是产品本身。
- **不要做 web 看板**。Multica 已经做得很完整（issue/board/timeline/project/skill 页面齐全），你跟着做没有边际收益，且违背 IM-first 原则。
- **不要把 agent 升级成"polymorphic actor"** —— S4 是你的 thesis 支柱，这一条改了 thesis 就空了。

### 5.2 必须守住的事

- **Supervisor LLM 调度** —— 这是 S1，是你区别所有调度系统（包括 Multica）的根因。Phase 6 的 prompt 调优投入要够，**这块吃饭工具**。
- **Center 单一权威** —— S4。无野任务、Worker 不互派、所有 task 来源经 Center 仲裁。任何"让 worker 自己决定"的设计提案都要回到这条 check。
- **DDD + 统一语言** —— conventions §0 立的方法论根基。已经付出的纪律成本（21 ADR / 7 BC / 测试 ≥ 90%）变成了你的"代码可维护性护城河"，Multica/Slock 都没这个层级的规范。
- **飞书作为唯一 v1 IM** —— 把 Bridge BC 留作 ACL 抽象，但**不要在 v1 做多 vendor**，会拖延 Phase 7。

### 5.3 可以学的事

- **agent adapter 矩阵**（Multica）—— 多 CLI 支持，低成本扩大覆盖（参 § 4.3）
- **MCP per-agent 注入**（Multica）—— Phase 2 dispatch envelope 补 mcp_config 字段（参 § 4.3）
- **Reminder 机制**（Slock）—— 让 supervisor 能"约自己回来"，减少对外部事件唤醒的依赖
- **Reactions**（Slock）—— 飞书卡片加 emoji 反馈做轻量决策投票
- **Autopilot 风格的 cron trigger**（Multica）—— 解锁周期性 supervisor wake 用法

### 5.4 战略层一句话

> **你做的是"agent OS"，Multica 做的是"agent JIRA"，Slock 做的是"agent Slack"** —— 不要在 JIRA 和 Slack 的领域跟它们打，把 agent OS 的事做扎实（supervisor 决策 + 单一权威 + IM 审计）就赢了。

---

## § 6. 附录：本次未覆盖的对照样本

如果未来想扩大对照面，下列样本值得加入：

| 样本 | 对照维度 |
|---|---|
| **Devin (Cognition)** | 商用自治编码 agent；调度模型、决策可见性 |
| **OpenHands** | OSS 自治编码 agent；多 agent 协调机制 |
| **Cursor Background Agents** | IDE 内 agent；session 模型、UI 集成 |
| **LangGraph / CrewAI / AutoGen** | 开发者框架；Supervisor 模式参考 |
| **Goose (Block)** | 个人本地 agent harness；MCP 集成 |
| **Tinker (Anthropic)** | 长跑训练 / agent 集群协调 |
| **Poltertty**（你自己的） | agent-friendly 终端；UI 层互补，不是竞品但能集成 |

---

## § 7. 文档元信息

- **作者**：x9527（Slock agent）
- **依据**：
  - agent-center 仓 + `docs/design/` 全部 21 个 ADR + 7 个 BC 设计
  - Multica 仓 `/Users/oopslink/works/codes/oos/multica` 的 README + AGENTS.md + docs/product-overview.md（截止 2026-04-21）
  - Slock 系统 prompt 规格 + slock.ai 公开页面
- **下一次复评建议**：Phase 6 (Cognition Supervisor) 完成后；Multica / Slock 任一发新版后
- **相关文档**：
  - [Domain Vision](../design/architecture/strategic/00-domain-vision.md)
  - [DDD Blueprint](../design/ddd-blueprint.md)
  - [Subdomain Classification](../design/architecture/strategic/01-subdomain-classification.md)
