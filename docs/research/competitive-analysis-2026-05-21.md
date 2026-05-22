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
Slock server（slock.ai 托管，可能也支持私有部署）
        │
        ↓  消息总线（channels / DMs / threads / tasks / reminders）
        ↑
slock CLI（每台已注册 computer 上）
        │
        ├─ 把人接进来（Web / 客户端）
        └─ 把 agent 接进来：
            ├─ 已注册的 computer（本地 / 云）作为 agent 算力池
            ├─ agent = CLI 进程（spawn 现成的 claude / codex 类 CLI）
            ├─ 每 agent 有持久 workspace（含 MEMORY.md）绑到 (server, computer)
            └─ 消息事件触发 sleep / wake，按需拉起进程
```

**核心概念**：

| 概念 | 释义 |
|---|---|
| **Server** | 一个 Slock 实例，包含 channels 和成员 |
| **Computer** | **注册到 Slock 的算力节点（本地或云）**，agent 真正运行的物理机器；一个 server 下可挂多个 computer，形成 agent 算力池 |
| **Channel** | `#name`，公开/私密，agent 可以 join/leave |
| **DM** | 1:1 私聊（也支持 thread） |
| **Thread** | 任意消息可派生 thread（`#channel:msgshortid`），不可嵌套 |
| **Task-message** | 任意 top-level 消息可升级为 task，带 status (`todo` / `in_progress` / `in_review` / `done`) + assignee + claim 锁；**status 与 assignee 独立** |
| **Agent** | 跟 human 平等的参与者；有稳定 @handle；绑到 `(server, computer)`；workspace 目录持久（含 MEMORY.md）；**底层是 spawn 出来的 CLI 进程**（claude / codex / opencode 等） |
| **Action Card** | 给人点击执行的命令卡（B-mode quick-commit shortcut）；变体含 `agent:create` —— **支持用户在 chat 里动态创建新 agent** |
| **Reminder** | 作者所有的持久 wake-up signal，可锚定到 message/thread |

**典型工作流**：
1. 人在 `#engineering` 发消息 `@agent-X 帮我处理一下…`
2. agent 进程被事件唤醒（如在 sleep，由 daemon 在指定 computer 上按需拉起）
3. agent 用 `slock task claim` 锁定 task，进入 `in_progress`
4. agent 在 thread 里发进度更新
5. 完成后 `slock task update --status in_review`，人审查后置 `done`
6. agent 把学到的偏好 / 偏差写回 `MEMORY.md` 等下次

**它的核心赌注**：
- **Chat-as-coordination**：所有协调（任务、决策、提问、确认）都在消息流里发生，没有独立看板
- **Agent = 持久身份 + 按需拉起的 CLI 进程**：身份和 workspace 一直在，进程按消息事件 spawn/sleep；底层用现成 CLI（claude / codex / opencode），跟 agent-center / Multica 同道
- **Computer 是显式一等公民**：算力节点可注册本地或云，形成 agent 算力池；这是它的**弹性能力来源**
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
| 19 | **部署** | 单 VPS + Worker 散在用户机 | Cloud（官方） + 自托管 docker | Slock server 托管 + computer 节点可注册本地或云（弹性算力池） |
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
| **Slock** | **同道** —— Slock agent 底层也是 spawn 出来的 CLI 进程（claude / codex / opencode 等），消息事件触发拉起；Slock 自己不调 LLM SDK |

**点评**：
- **三家完全同道** —— 都走 CLI agent 路线，都不锁单一厂商。这条赌注**已被三家用脚投票**：在 v1/v2 这个时代，spawn CLI 是工程性价比最高的选项。
- **Multica 探测的 CLI 比你多得多**（11 vs 3：claude + codex stub + opencode stub）。**这是你能直接照搬的 —— agentadapter 多写几个**。
- 但要注意：Multica 的 11 个 CLI 里很多是简单 wrap（参数注入 + JSONL 解析），不复杂；麻烦的是各家 headless 输出格式不一致，adapter 层得吃这部分。
- **Slock 进一步把"agent 拉起"做成了弹性能力**（见 § 3.6 新增的对照）—— 这是从 spawn CLI 思想衍生出的、值得学的设计。

### 3.6 算力节点 / Worker 拓扑模型

> agent-center 把 Worker 当"开发机上的常驻 daemon"，每台机器一个，长连接挂着；agent 进程在事件触发时被该 daemon spawn。

| | 怎么做的 | 弹性 |
|---|---|---|
| **agent-center** | Worker daemon 是常驻进程，agent 是 per-execution spawn 短命进程；Worker 在 Center 长连接里 enroll 一次后稳定，**Worker 数量 = 用户开发机数量**（手动拉起） | ⬇️ 低 —— 加节点要装 daemon + bootstrap token + 改配置 |
| **Multica** | Daemon 同上，每机器一个；3s poll + 15s heartbeat；runtime 注册后稳定；通过 `Max Concurrent Tasks` 控制单 agent 并发 | 🟡 中 —— 多机器手动装，但 runtime usage / activity 可视化 |
| **Slock** | **Computer 是显式一等公民**：可注册本地机器或云节点，形成 agent 算力池；agent 跨 computer 散布；**新建 agent 可在任意已注册 computer 上拉起**（含云）；action card 含 `agent:create` 让人在 chat 里直接动态创建新 agent | ⭐ 高 —— **按需拉起 + 算力池弹性** |

**点评**：
- **Slock 的 computer 抽象是真有意思的设计**。它把"算力节点注册" 跟"agent 身份"显式分开：computer 是机器、agent 是身份，N 个 agent 可以跑在同一 computer，agent 可以迁机器。
- agent-center 现在把这两件事耦合在 Worker daemon 上 —— "我有 worker daemon 在机器 A" = "机器 A 上能跑活"。Worker daemon 死了 → 整机失能。
- **加云 computer 节点的能力是 agent-center 缺的弹性维度**。当你想临时跑大量 agent 干一个项目时，agent-center 必须有现成的 Worker；Slock 可以临时拉起云 computer。
- **跟 v1 vision 的冲突**：Vision B2 明确 "v1 不是 SaaS / 不是多租户"，云节点本质上把 v1 推向"我得管别人机器" —— 跟个人工具定位有张力。**但本地多机器 + 即开即用的设计仍可学**：把 Worker daemon 的 enroll 流程做轻（v1 现在的 bootstrap token 流程不轻），让加节点像 `slock` 那样秒级。
- **可立即抄的点**：把"agent 配置实体"（Multica 用法）跟"Worker daemon 节点"（agent-center 现状）解耦 —— agent 是逻辑身份 + 选择跑在哪台 Worker 上；这样未来加云节点不会动 agent 的身份。

### 3.7 Slock Computer + Agent 模型 vs agent-center 概念深扎

> § 3.6 给的是"算力节点弹性"的结论；本节是同一主题往下挖一层 —— **Slock 的 Computer / Agent 两个一等实体，在 agent-center 的 DDD 模型里有没有对应物？**
> 这里只看 Slock vs agent-center 两家（Multica 的 agent / runtime 两实体是另一种解法，前面已述）。

#### 1:1 对应表

| Slock | agent-center 现有 | 对应度 |
|---|---|---|
| **Server**（chat 服务端 / 状态权威） | **Center server**（VPS 常驻 / 状态权威） | ⭐⭐⭐ 几乎完全对应 |
| **Computer**（注册到 Slock 的算力节点） | **Worker**（开发机上的 daemon） | ⭐⭐ 半对应 |
| **Agent**（持久身份，绑到 `(server, computer)`） | **❌ 没显式实体** | ⭐ 缺位 |
| **Agent 进程**（spawn 的 CLI 实例） | **Worker agent 进程**（per-execution spawn 的 CLI 实例） | ⭐⭐⭐ 对应（生命周期不同） |
| **`agent:create` action card** | **❌ 没对应** | ⭐ 缺位 |

#### 关键差异（三处）

**差异 1：Worker ≠ Computer，概念边界不同**

| 视角 | 释义 |
|---|---|
| agent-center 的 Worker | "**我有 daemon 跑在这台机器上**"（进程视角） |
| Slock 的 Computer | "**我有这个算力节点在池子里**"（资源视角） |

物理上都是"一台机器"，但 Slock 把它显式抽象为"可注册的算力节点"，与节点上的进程解耦。结果：
- Slock 加云节点 = 注册一个新 Computer，不动 agent 身份
- agent-center 加远程机器 = 装 Worker daemon + bootstrap token + Center 配置，**Worker daemon 死 = 整机失能**

**差异 2：agent-center 没有"Agent 持久身份"实体**

在 agent-center v1：
- "agent" 不是 BC 里的 AR / Entity / VO，只是"Worker 上跑的 CLI 工具实例"（[UL § 1.1](../design/architecture/strategic/03-bounded-contexts.md#-1-通用语言ubiquitous-language)）
- TaskExecution `1:1` Agent，**execution 结束 agent 进程就没了**
- 没有"my agent `coding-assistant-1` 在 Worker `home-mbp` 上"这种持久关系
- 跟 Slock（agent 是 first-class identity）和 Multica（agent 是配置实体）都不一样

**差异 3：动态创建 agent 的协议缺失**

| 系统 | 新 agent 怎么来 |
|---|---|
| Slock | 用户在 chat 里点 `agent:create` action card → 拉起一个新 agent |
| agent-center | agent 是隐式的：dispatch envelope 里指定 `agent_kind=claudecode`，Worker spawn 一个进程，结束就完。**没有"我想新建一个长期叫 X 的 agent"概念** |

#### 现有可类比的最近近似物

agent-center 现在有这些近邻概念，但没拼成 Slock 的 Computer+Agent 模型：

| 现有 | 跟 Slock 的 Computer+Agent 的差距 |
|---|---|
| **Worker enroll** （bootstrap token 换 session token） | 接近 Slock "computer 注册"，但流程重 + 跟 daemon 绑死 |
| **WorkerProjectMapping**（worker_id, project_id → base_path） | 接近 Slock `(server, computer)` 绑定，但**第三维 agent 缺**；Mapping 的轴是 worker × project，不是 worker × agent |
| **WorkerProjectProposal**（Worker 扫描发现候选 mapping） | [ADR-0008](../design/decisions/0008-worker-project-mapping-via-discovery-proposal.md) 的"自动发现 + 用户确认"流程，跟 Slock 的注册理念同向 |
| **DispatchEnvelope**（运行时下发的 prompt + skill + tooling） | 接近 Slock "spawn 一个 agent 进程"，但每次 dispatch 都是新进程，没有"叫 X 的那个 agent" |

#### 最小映射设计（如果决定补）

新增一个聚合 **`AgentInstance`**（暂名）放在 Workforce BC 或新建 BC：

```
AgentInstance {
  id             // 持久 ID
  name           // 用户起的名字 ("coder-mbp", "tester-server")
  agent_kind     // claudecode / codex / opencode
  worker_id      // 默认跑在哪个 Worker（可改）
  config         // instructions / mcp_config / max_concurrent / env / args
  workspace_dir  // 持久工作目录（含 CLAUDE.md / 笔记）
  state          // idle / working / sleeping / archived
}

TaskExecution.agent_instance_id     // 新增字段
WorkerProjectMapping → AgentProjectMapping  // 多一维：agent
```

带来的：
- Worker daemon 死了 = 它上面的 agent 进入 sleeping，但 AgentInstance 实体活着 → 重启 daemon 后被唤醒
- 加新 worker 节点 = 把 AgentInstance 迁过去（改 `worker_id`），agent 身份不变
- 给 Supervisor 提供"约 agent X 接 task Y"的能力（区别于现在的"派任务到 Worker，由 Worker 选 agent_kind"）

#### 跟 v1 vision 的相容性

- **不触碰多租户** —— 只动 Workforce BC 内部结构，符合"个人工具"边界（vision B2）。
- **需要新立 ADR** —— 因为推翻了"agent 不是 first-class entity"的现状；候选编号 ADR-0022 "AgentInstance as Workforce Entity"，supersede [ADR-0008](../design/decisions/0008-worker-project-mapping-via-discovery-proposal.md) 中 Mapping 的两维结构。
- **跟 Multica 的 agent 配置实体异同**：Multica 的 `agent` 表是"配置 + runtime 绑定"的统一实体；本设计的 AgentInstance 跟 Multica 的高度相似（实际上是 Multica 用法 + Slock 持久身份的合并）。这是吸收同行成熟设计的低风险路径。

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
7. **Slock 的 Computer 抽象 + 按需拉起 agent**（§ 3.6 重点）—— 把"算力节点"（Worker 机器）跟"agent 身份"显式解耦：computer 是算力池里的一个节点（本地或云均可），agent 是逻辑身份选某 computer 跑。**这是 agent-center 现在缺的弹性维度** —— 现状是 Worker daemon 死了整机失能；解耦后未来加云节点不动 agent 身份。短期落地：把 Worker enroll 流程做轻（参 Slock 的 `slock` 一键接入体验），让加节点秒级；中期落地：让 dispatch envelope 显式选 `(worker, agent)` 而不是隐式绑到单一 worker。
8. **Slock 的 `agent:create` action card**（动态新增 agent 的协议）—— 让用户/supervisor 都能在 chat 里动态拉起新 agent，而不是预配置静态列表。对 agent-center 是 "Workforce BC 是否需要 agent 配置实体" 的输入，可能 v2 考虑。

---

## § 5. 战略选择建议

> 基于 v1 [Vision § 3 边界](../design/architecture/strategic/00-domain-vision.md#-3-边界boundaries)（个人工具 + 不接 git）的总开关。

### 5.1 不要去争的事

- **不要做团队多租户**。这是 Multica 的主战场，跟你的 vision B2 (v1 不是 SaaS) 冲突，走那条要"基本重新做项目"。
- **不要做"all-in-one chat 平台"**。这是 Slock 的主战场，你的优势是**借用 IM**（飞书）而不是自建。继续把 Bridge BC 当 ACL 而不是产品本身。
- **不要追平 Multica 那种"协作中枢级 web 看板"，但保留一个"任务管理面"是值得做的**（**用户 2026-05-21 修订**）。

  *拆开两件事*：

  | 类型 | 用途 | 在 agent-center 该不该做 |
  |---|---|---|
  | **任务管理面**（task management view） | 跨项目看所有 open/working/done 任务、搜索过滤、批量改 status、查 task 历史 trace | ✅ 应当做 —— 用户原话"方便任务管理"，单人也需要全局视图，CLI grep 不顺手 |
  | **协作中枢看板**（collaboration hub） | issue/board/timeline/project/skill/inbox/comment 全套，团队成员 80% 协作动作在里面发生 | ❌ 不应追平 —— 用户是个人，无团队协作刚需；Multica 是 4 团队复杂度 UI（Next.js 16 + Electron + monorepo），追平 = frontend 1 年起步 |

  *任务管理面跟 IM-first 怎么共处*：

  | 面 | 职责 | 边界 |
  |---|---|---|
  | **飞书 IM**（S3 决策审计）| **决策动作发生地**：派单、escalate、input_request、conclude、approve 全在卡片上 | 唯一可"动手"的面 |
  | **任务管理面** | **只读 + 边边角角的运维**：list / 搜索 / trace 回看 / batch suspend/resume / 看 worker 在线状态 | **不放决策动作** —— 决策永远跳回 IM 卡片 |

  这样 S3（vision 押的"所有决策在 IM 发生"）不被稀释，同时获得"全局可见 + 管理便利"。**关键设计纪律：任务管理面禁止承载任何 LLM Supervisor 决策、issue conclude、input_request 应答这类高阶动作**。承载这类动作就回到了"卡片在 IM / 详情在 web"双源问题。

  *跟 v1 vision 的兼容路径*：任务管理面可以是个 SSR Web 服务（Go 直接渲染，无 SPA），只读为主，写动作走少量 form post 调 CLI；或者就是 `agent-center status` / `agent-center tasks` 这类增强 CLI + 一个简单的 server-side rendered HTML dashboard。**避免引入 SPA frontend + state management 这一整套技术债**。

- **不要把 agent 升级成"polymorphic actor"**。

  *什么是 polymorphic actor*：Multica 的设计范式 —— 几乎所有"谁做了什么"的字段都是 `actor_type` (`member` | `agent`) + `actor_id` 两段式（详见 [§ 1.1 Multica 速写](#11-multica) 核心概念表最后一行）。结果：**agent 跟 human 在产品里完全平权** —— agent 能创建 issue、能在评论里 @ 别人、能订阅、能被订阅、能加 emoji 反应、能在 Squad 里当 leader 派任务给其他 agent。

  *"升级成"是什么意思*：指未来某天有人提议 "为什么不让 worker agent 自己开 task？" / "为什么不让 supervisor 派给另一个 supervisor？" / "为什么 worker agent 不能 @ 别的 agent 协作？" —— 这些都是 polymorphic actor 思维的具体提案。每一条单独看都"显得合理"。

  *为什么"S4 是 thesis 支柱"*：[Domain Vision § 1](../design/architecture/strategic/00-domain-vision.md#-1-核心价值thesis) 的 thesis 是 **"代替人，看着 N 个 worker 同时干活"**。这个 thesis 成立的前提是用户**只跟一个对象（Supervisor）打交道**——supervisor 替用户盯所有 worker，用户不必肉眼盯。S4（Center 单一权威 / 无野任务）就是这个前提的协议表达：**所有任务来源经 Center 仲裁 → 用户只需要看 Supervisor 的决策卡片，不需要追踪 worker 之间在干什么**。

  *"thesis 就空了"的因果链*：如果允许 agent 之间平权派活（polymorphic actor），就会出现这样一条因果链：
  ```
  agent A 派给 agent B → B 派给 C → 用户突然收到 C 的 input_request
       ↓                              ↓
   用户：这个 C 哪冒出来的？        用户：我要回 C 还是回 A？
       ↓
  用户必须重新理解"谁在干什么"的拓扑 → 这恰恰是 thesis 想消除的认知负担
  ```
  Multica 这条能成立是因为它**没押"代替人盯"这个 thesis**——它的 thesis 是 "agent 当同事，跟人平权" —— 用户**本来就在团队里跟多人协作**，加个 agent 同事不增加认知负担。**两家 thesis 不同，所以同样的"polymorphic actor"设计对 Multica 是核心，对 agent-center 是毒药**。

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
- **Computer / 算力节点解耦**（Slock § 3.6 + § 4.3.7）—— 把 Worker 当算力节点而非 agent 绑死的宿主；先做轻量 enroll，未来给本地多机和云节点留口子。**这是 § 4.3 中最值得近期吸收的一条**，因为它直接影响 v1 Workforce BC 的边界（[ADR-0008 Worker-Project Mapping](../design/decisions/0008-worker-project-mapping-via-discovery-proposal.md) 现有的 mapping 模型可能要重新审视）。
- **任务管理面（task management view）**（参 § 5.1 修订条）—— 提供"跨项目全局视图 + 搜索过滤 + 历史 trace 回看 + 批量运维"，单人场景下也有真实价值。**实施纪律**：只读为主、写动作只覆盖运维（suspend/resume/cancel），决策动作（dispatch / conclude / input_request 应答）永远跳回飞书卡片。技术形态优先选 Go SSR + 增强 CLI，避免 SPA 全家桶。

### 5.4 战略层一句话

> **你做的是"agent OS"，Multica 做的是"agent JIRA"，Slock 做的是"agent Slack"** —— 不要在 JIRA 和 Slack 的领域跟它们打，把 agent OS 的事做扎实（supervisor 决策 + 单一权威 + IM 审计）就赢了。**辅助管理面（任务列表 / 全局视图）该做就做，但严守"决策在 IM、运维在面板"的分工，不要让面板长成第二个 JIRA。**

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
