# 限界上下文 & Ubiquitous Language

> 本文档定义 agent-center 的领域模型：**通用语言**（vocabulary） + **限界上下文**（bounded contexts） + **上下文映射**（context map）。

DDD 战略设计层。具体聚合 / 实体 / 值对象的字段与状态机迁移细节，分散在后续各章节（[02-task-model.md](02-task-model.md) / [03-issue-discussion.md](03-issue-discussion.md) / 等）。

---

## § 1. 通用语言（Ubiquitous Language）

全部术语统一遵守：**代码包 / 文档 / CLI / event_type 字符串 / 数据库表名都用同一组词**。违反命名一致性见 [conventions § 12](../../rules/conventions.md#-12-命名一致)。

### 1.1 核心实体（按上下文归属）

| 术语 | 上下文 | 定义 |
|---|---|---|
| **Task（任务）** | Scheduling | Agent 干活的最小单位。属于一个 project，由 supervisor 创建，dispatch 给 worker，状态机驱动。**只有 center 能创建 task，无野任务**（[conventions § 1](../../rules/conventions.md#-1-单一来源--无野任务)） |
| **InputRequest（请示）** | Scheduling | Task 进行中 agent 卡住需要外部输入的**同步阻塞**请求 |
| **Issue（议题）** | Discussion | 待讨论的事。可由 user / supervisor / worker (agent) 提起；多轮讨论 conclude 后可能产生 0/1/N 个 Task |
| **IssueComment（讨论评论）** | Discussion | Issue thread 内单条留言。kind ∈ {message, system, agent_finding, supervisor_summary} |
| **Project（项目）** | Workforce | 任务归属的逻辑容器（代码 repo / 写作 / 投研 / ...）。每个 task 必属于一个 project |
| **Worker（工人/工作节点）** | Workforce | 用户开发机上的守护进程，能执行任务 |
| **
** | Workforce | 某个 worker 在本地哪个路径有哪个 project 的 worktree-root |
| **Agent（执行体）** | Execution | 在 worker 上跑的 CLI 工具实例（claude code / codex / opencode） |
| **AgentSession（会话）** | Execution | 一次 agent 子进程 spawn → 退出的运行周期；实时投影 |
| **Worktree（工作树）** | Execution | per-task **动态创建**的 git worktree，提供文件隔离。**临时**：跑完保留 24h 再 GC。不进 mapping 表 —— 通过 events + AgentSession 投影实时呈现 |
| **WorkerProjectMapping（映射）** | Workforce | (worker_id, project_id) → `base_path` 的稳定映射。**只存稳定的 base_path**；worktree_root 按约定 = `base_path + ".wt"`，不存 |
| **WorkerProjectProposal（提议）** | Workforce | Worker 自动扫描发现的"候选项目映射"。需要用户飞书确认才能升级成 WorkerProjectMapping。状态机 pending → accepted/ignored/superseded |
| **TaskExecution（任务执行）** | Execution | worker 侧任务运行时聚合，关联一个 Task + 一个 Worktree + 一组 AgentSession |
| **Artifact（产物）** | Execution | 任务产生的文件 / 引用（PR URL / chapter 文件 / 报告 / 等），自由形态 |
| **Supervisor（监督者）** | Cognition | 中心的调度官 agent；LLM 驱动；事件触发；spawn 一次 claude code 进程做一次决策周期。**它也是 agent，不是"中央意识"**（[ADR-0003](../decisions/0003-supervisor-not-brain.md)） |
| **SupervisorInvocation（监督者调用）** | Cognition | Supervisor 一次启动 → 退出的审计单元，含触发事件、prompt、输出、决策记录 |
| **Memory（记忆）** | Cognition | Supervisor 的持久脑，scoped notes（global / project / task / issue / worker / supervisor） |
| **DecisionRecord（决策记录）** | Cognition | Supervisor 在一次 invocation 中显式 emit 的具体决策（通过 CLI 调用动作时记录） |
| **Event（领域事件）** | Observability | 跨上下文的离散状态变化记录。落到 events 表，append-only |
| **AgentTraceEvent** | Observability | Agent JSONL 流中解析出的单条事件（thinking / tool call / tool result / 等） |
| **Conversation（会话）** | Conversation | 系统**内部**的消息时间线存储。跟 vendor 解耦（Bridge 负责同步）；**跟 Issue 解耦**（独立聚合，N:N 弱关联，见 [ADR-0009](../decisions/0009-issue-conversation-decoupled-via-bridge.md)） |
| **Message（消息）** | Conversation | Conversation 内的一条留言。`content_kind` ∈ {text / system / agent_finding / supervisor_summary}；不存 vendor 渲染（卡片由 Bridge 翻译） |
| **IssueComment（议题评论）** | Discussion | Issue 的结构化评论。Discussion BC **独立表** `issue_comments`，跟 Conversation Message 是两套数据 |
| **Identity（身份）** | Conversation | 参与者的统一身份：`user:hayang` / `supervisor:invocation-id` / `agent:session-id` / `bot`。跨渠道不变 |
| **ChannelBinding（渠道绑定）** | Conversation | Identity ↔ 某渠道的 vendor user id 映射（例：`user:hayang ↔ feishu:open_id:ou_xxx`、`user:hayang ↔ dingtalk:userid:xxx`） |
| **Bridge（桥接器）** | Bridge | 每个 vendor 一个 Bridge 实现：订阅领域事件 → 推到 vendor（outbound）；收 vendor 回调 → 写回领域模块 API（inbound）。**单向: vendor ↔ 系统数据，没人能调它"发消息"** |
| **LarkCard（飞书卡片）** | Bridge | 飞书交互卡片（vendor 渲染细节）。**不是 Message 的 content_kind** —— Bridge 根据 content_kind 翻译时按需渲染为 card |

### 1.2 行为动词

| 动词 | 主语 | 宾语 | 释义 |
|---|---|---|---|
| **Dispatch（派单）** | Supervisor | Task | 创建 Task 并指定 worker / agent CLI / prompt |
| **Conclude（收敛）** | User | Issue | 对 issue 做出结论；可能 spawn 0/1/N 个 Task |
| **Spawn（衍生）** | Issue conclude / Supervisor | Task | 创建子 Task / 由 Issue 结论创建 Task |
| **Escalate（升级到人）** | Supervisor | InputRequest / Issue | 把决策推给用户（飞书卡片） |
| **Enroll（注册）** | Worker | (Center) | 首次连接 center，凭 bootstrap token 换 session token |
| **Adopt（采纳）** | User | InputRequest 的 supervisor suggested answer | 选用 supervisor 倾向的答案 |
| **Withdraw（撤回）** | Issue opener | Issue | 撤销开启的 issue |
| **Open（开）** | User / Supervisor / Agent | Issue | 创建新 issue |
| **Cancel（取消）** | User / Supervisor | Task / InputRequest | 中止 |
| **Add-message（写入会话消息）** | Supervisor / Bridge / 等 | Conversation | 调 `agent-center conversation add-message` 往 Conversation 内写一条 Message（内部存储，Bridge 自动同步外发） |
| **Comment（评论议题）** | User / Supervisor / Agent | Issue | 调 `agent-center issue comment` 往 Issue 写一条 IssueComment（结构化）|
| **Deliver（投递）** | Bridge | Message / IssueComment → Vendor | Bridge 订阅事件，把消息内容翻译并投递到 vendor；回执回写 vendor_msg_ref |

### 1.3 状态机词汇

**Task（A2A 状态机）:**

| 状态 | 含义 |
|---|---|
| `submitted` | 已创建，等待 dispatch / agent 接收 |
| `working` | Agent 正在跑 |
| `input_required` | Agent 卡在 InputRequest |
| `completed` | 成功结束 |
| `failed` | 失败结束 |
| `canceled` | 用户 / supervisor 取消 |

**Issue:**

| 状态 | 含义 |
|---|---|
| `open` | 刚开，无人响应 |
| `under_discussion` | 已有非 opener 的 comment 进入 |
| `concluded` | 用户拍板，准备收尾 |
| `closed_no_action` | 结论是不做 |
| `closed_with_tasks` | 结论是做这些，已 spawn tasks |
| `withdrawn` | 撤回 |

**InputRequest:**

| 状态 | 含义 |
|---|---|
| `pending` | 等回应 |
| `responded` | 已应答，agent 继续 |
| `timed_out` | 超时 |
| `canceled` | 任务取消导致 |

**AgentSession:**

| 状态 | 含义 |
|---|---|
| `running` | Agent 子进程在跑 |
| `waiting_input` | 阻塞在 InputRequest |
| `completed` | 正常退出 |
| `failed` | 异常退出 |

**Worker:**

| 状态 | 含义 |
|---|---|
| `online` | 长连接活跃，心跳正常 |
| `offline` | 长连接断开 |
| `enrolling` | 注册过程中（短暂）|

### 1.4 基础设施词汇

| 术语 | 定义 |
|---|---|
| **BlobStore** | 大文件存储抽象（v1 LocalDirBlobStore，未来 S3）。见 [implementation/01-blob-store.md](../implementation/01-blob-store.md) |
| **Worktree-root** | Worker 上某个 project 用于派生 task worktree 的根目录 |
| **Dispatch envelope** | Supervisor 派单时下发到 worker 的载荷结构。见 [08-prompt-assembly.md](08-prompt-assembly.md) |
| **Skill** | 教 agent 怎么用工具的 markdown 文档（worker-agent.md / supervisor.md）。见 [10-skill-cli-tooling.md](10-skill-cli-tooling.md) |
| **CLI 子命令** | `agent-center <op>` 形式的实际工具入口 |
| **Trigger event** | 触发 SupervisorInvocation 的源事件 |

### 1.5 易混淆术语对照

| 用 | **不要**用 | 理由 / 见 |
|---|---|---|
| Supervisor | ~~Brain~~ | [ADR-0003](../decisions/0003-supervisor-not-brain.md) |
| Issue | ~~Suggestion~~ | [ADR-0004](../decisions/0004-issue-not-suggestion.md) |
| Worker daemon | ~~Worker（指 host）~~ | "Worker" 一词在本系统专指守护进程 |
| Agent / Worker agent | ~~Worker~~ | 避免与 Worker daemon 混 |
| Memory | ~~knowledge / brain memory~~ | conventions § 12 |
| Worktree | ~~workspace / sandbox~~ | conventions § 12 |
| BlobStore | ~~ObjectStore / FileStore~~ | conventions § 12 |
| AgentSession | ~~run / execution（指 agent CLI 实例）~~ | TaskExecution 是 worker 侧上层概念 |

---

## § 2. 限界上下文（Bounded Contexts）

agent-center 的领域划分为 **7 个限界上下文**。Web Console / CLI / BlobStore 不是 BC（属于表现层 / 基础设施）。

### BC1: Scheduling（调度）

**职责**: Task 全生命周期 + InputRequest + 派单逻辑 + 子任务层级

**核心聚合**:
- `Task`（根） + 子 Task （通过 `parent_task_id` 引用）
- `InputRequest`（独立聚合，但通过 `task.pending_input_request_id` 与 Task 关联）

**核心事件**: `task.created` / `task.dispatched` / `task.working` / `task.input_required` / `task.completed` / `task.failed` / `task.canceled` / `input_request.requested` / `input_request.responded` / `input_request.timed_out`

**核心操作**: `dispatch` / `cancel-task` / `query tasks` / `respond-to-input-request` / `request-input`（来自 Execution）

**详细设计**: [02-task-model.md](02-task-model.md) / [04-input-required.md](04-input-required.md)

### BC2: Discussion（讨论）

**职责**: Issue 全生命周期 + IssueComment 流 + Issue conclude spawn Tasks

**核心聚合**:
- `Issue`（根） + `IssueComment`（实体，从属）

**核心事件**: `issue.opened` / `issue.commented` / `issue.discussion_started` / `issue.concluded` / `issue.withdrawn` / `issue.tasks_spawned`

**核心操作**: `issue open / comment / conclude / close` / 飞书 thread 双向同步

**详细设计**: [03-issue-discussion.md](03-issue-discussion.md)

### BC3: Workforce（工作池）

**职责**: Worker / Project 元数据 + WorkerProjectMapping + WorkerProjectProposal（自动发现） + 注册 / 在线状态

**核心聚合**:
- `Worker`（根） + `WorkerProjectMapping`（实体，已生效的稳定映射）
- `WorkerProjectProposal`（根，独立聚合；自动发现的候选，等用户飞书确认才升级为 Mapping）
- `Project`（根，独立聚合）

**核心事件**:
- `worker.enrolled` / `worker.online` / `worker.offline` / `worker.heartbeat`
- `worker_project_proposal.proposed` / `worker_project_proposal.accepted` / `worker_project_proposal.ignored` / `worker_project_proposal.unignored`
- `worker_project_mapping.added` / `worker_project_mapping.invalidated`（base_path 不再有 git 内容时）
- `project.created` / `project.updated` / `project.removed`

**核心操作**: `worker enroll / list / status` / `project add / update / remove / list` / `worker proposal list / unignore`

**详细设计**: [06-supervisor-model.md](06-supervisor-model.md) 与 [07-worker-model.md](07-worker-model.md)（含 WorkerProjectMapping 自动发现流程）

### BC4: Execution（执行运行时, worker 侧）

**职责**: 在 worker 机器上的运行时上下文 —— Worktree 管理、Agent 子进程生命周期、JSONL trace 解析、artifact 收集、日志归档。**这个上下文物理上跑在 worker 上，不在 center**。

**核心聚合**:
- `TaskExecution`（根） + `Worktree`（值对象，附带 lifecycle） + `AgentSession`（实体）
- `Artifact`（值对象集合）

**核心事件**: `worktree.created` / `agent_session.started` / `agent_session.activity_changed` / `agent_trace.event` / `agent_session.ended` / `worktree.released` / `artifact.uploaded` / `task_log.archived`

**核心操作**: 内部 worker daemon API；agent 通过本机 unix socket 调 CLI 命令（request-input / report-progress / open-issue / read-task-context）

**详细设计**: [07-worker-model.md](07-worker-model.md)

### BC5: Cognition（认知 / 监督者）

**职责**: Supervisor 运行模型 + Memory + Decision 记录

**核心聚合**:
- `SupervisorInvocation`（根） + `DecisionRecord`（实体，从属）
- `Memory`（根，独立聚合；按 scope 组织）

**核心事件**: `supervisor.invocation_started` / `supervisor.decision_made` / `supervisor.invocation_ended` / `memory.recorded` / `memory.pruned`

**核心操作**: 内部 supervisor 触发器（事件驱动）；CLI `memory list / add / prune` / `record-decision`

**详细设计**: [06-supervisor-model.md](06-supervisor-model.md)

### BC6: Observability（观测）

**职责**: 跨上下文事件总线（`events` 表）+ 实时投影（agent_session / worker_status / task_status / fleet）+ 查询接口

**核心聚合**:
- `Event`（append-only 行；表本身是 event stream，单条 immutable）
- `AgentSessionProjection` / `TaskStatusProjection` / `FleetSnapshot`（读模型，从 events 投影出来）

**核心事件**: 不产生新事件（订阅其它上下文的事件，做投影 / 归档 / 查询）

**核心操作**: `inspect (task|supervisor|session|issue) <id>` / `query <type>` / `ps` / `stats` / `logs`

**详细设计**: [05-observability.md](05-observability.md)

### BC7: Conversation（会话）

**职责**: 系统**内部**的会话消息时间线存储。**跟 vendor 解耦**（不直接调 vendor SDK）、**跟 Issue 解耦**（独立聚合，N:N 弱关联）。

**核心聚合**:
- `Conversation`（根） + `Message`（实体，从属于 Conversation）
- `Identity`（根，独立聚合）+ `ChannelBinding`（值对象，Identity 在各渠道的 vendor id 映射）

**Conversation 的种类（`kind` 字段）**:
- `dm` —— 用户与 bot 的长期 DM
- `group_thread` —— 群里 @bot 触发的 thread
- `adhoc` —— 短期一次性交互
- `notification` —— 单向通知（周期 review 推送等）

**没有 `issue` kind** —— Issue 跟 Conversation 是解耦的两个聚合（见 [ADR-0009](../decisions/0009-issue-conversation-decoupled-via-bridge.md)），通过 `issue.related_conversation_ids` 弱关联。

**核心事件**: `conversation.opened` / `conversation.message_added` / `conversation.closed` / `identity.registered` / `channel_binding.added`

**核心操作**:
- `conversation add-message --id=X --content=...` (**内部写入**；Bridge 自动订阅事件外发)
- `conversation list [--participant=...] [--kind=...]`
- `inspect conversation <id>` (整个会话的消息时间线)

**对外接口（被其它 BC 调用）**:
- "往 Conversation X 写一条 Message" —— Cognition 等通过 CLI 调用
- emit `conversation.message_added` 事件 —— 任何 BC（含 Bridge）订阅

**详细设计**: [12-conversation.md](12-conversation.md)

### BC8: Bridge（渠道桥接层）

**职责**: 每个 vendor（飞书 / DingTalk / Web chat / ...）一个 **Bridge**，做**双向同步**：

- **Outbound（系统 → vendor）**：订阅领域事件（`conversation.message_added` / `issue.comment_added` / 等）→ 渲染为 vendor 格式 → 调 vendor SDK 投递
- **Inbound（vendor → 系统）**：维持 vendor 长连接 / webhook → 收回调 → 路由判断（bound card thread vs 普通 conversation）→ 调对应领域模块的 API 写入

**核心聚合**:
- 无自己的聚合 —— 纯 ACL / 翻译层，不持有领域状态

**核心组件（每个 vendor 一个 Bridge）**:
- `FeishuBridge` —— v1 必做。WebSocket 长连接 + 飞书 SDK 调用
- `DingTalkBridge` —— 推迟（[roadmap](../roadmap.md)）
- `WebBridge` —— 推迟（Web Console 内嵌的聊天入口）

**核心事件**（Bridge 本身 emit）: `channel.delivered` / `channel.delivery_failed` / `bridge.parse_failed`

**关键约束**: Bridge 是**唯一**调用 vendor SDK 的地方。其它 BC（Conversation / Discussion / Scheduling / Cognition）零 vendor 依赖（见 [conventions § 9.y](../../rules/conventions.md#-9y-外部集成走-bridge-模式不在领域模块内调-vendor-sdk)）。

**详细设计**: [09-feishu-integration.md](09-feishu-integration.md)（FeishuBridge 具体实现）

---

## § 3. 上下文映射（Context Map）

```
        飞书           DingTalk        Web chat        ...
         ↕                ↕                ↕
   ┌───────────────────────────────────────────────────────┐
   │ Bridge (BC8)                                          │
   │   FeishuBridge  DingTalkBridge  WebBridge   ...       │ ← 每 vendor 一个 Bridge
   │   双向同步: 订阅领域事件 → 推 vendor；                  │
   │             收 vendor 回调 → 调领域模块 API 写入        │
   └────────┬──────────────────────────────────────────┬───┘
            │ inbound 调 API 写                         ↑ outbound 订阅事件
            ↓                                          │
   ┌─────────────────┐                       ┌─────────────────┐
   │ Discussion      │                       │ Conversation    │
   │ (Issue,         │                       │ (BC7, 系统内部   │
   │  IssueComment,  │                       │  消息时间线)     │
   │  bound_card)    │←弱关联(JSON id list)→│                  │
   └────────┬────────┘                       └────────┬────────┘
            │                                         │
            │ Shared Kernel:                          │ Shared Kernel:
            │ project_id                              │ (无)
            ↓                                         ↓
   ┌──────────────────────────────────────────────────────────┐
   │ Scheduling (Task / InputRequest)                         │
   └──────┬───────────────────────────────────────────────────┘
          │ Shared Kernel: worker_id / project_id
          ↓
   ┌──────────────────────────────────────────────────────────┐
   │ Workforce (Worker / Project / WorkerProjectMapping)      │
   └──────┬───────────────────────────────────────────────────┘
          │ Worker daemon ↔ Center 长连接
          ↓
   ┌──────────────────────────────────────────────────────────┐
   │ Execution (worker-side runtime)                          │
   │ Customer-Supplier upstream to Scheduling                 │
   └──────────────────────────────────────────────────────────┘


   Cognition (Supervisor) ─── cross-cutting actor ─→ ALL contexts
     (Supervisor 通过 CLI 写入内部模块: issue comment / conversation add-message / 等
      Bridge 自动订阅事件外发 vendor, Supervisor 不知道 vendor 存在)

   Observability ←── subscribe-only ←── ALL contexts emit events
     (订阅所有事件，做投影 / 查询)
```

### 3.1 上下游关系一览

| 上游 → 下游 | 模式 | 内容 |
|---|---|---|
| Discussion → Scheduling | Customer-Supplier | Issue conclude 后命令 Scheduling 创建 Tasks |
| Scheduling ↔ Workforce | Shared Kernel | Task 引用 worker_id / project_id；Issue 引用 project_id |
| Discussion ↔ Workforce | Shared Kernel | Issue 引用 project_id |
| Execution → Scheduling | Customer-Supplier | Execution 实时回报 task 状态 / 产物，触发 Scheduling 状态迁移 |
| Execution → Discussion | Customer-Supplier | Agent 调 `open-issue` 命令 Discussion 创建 Issue |
| Execution → Scheduling | Customer-Supplier | Agent 调 `request-input` 命令 Scheduling 创建 InputRequest |
| Bridge ↔ vendor | ACL / 双向同步 | Bridge 是唯一调用 vendor SDK 的地方；翻译 incoming 为领域 API 调用；订阅 outbound 事件推到 vendor |
| Bridge → Discussion / Conversation | Customer-Supplier | inbound 时 Bridge 调领域模块 API（`issue comment` / `conversation add-message`）写入数据 |
| Bridge ← Discussion / Conversation | Pub/Sub | Bridge 订阅 `issue.comment_added` / `conversation.message_added` 等领域事件做 outbound 投递 |
| Discussion ↔ Conversation | 弱关联（JSON id list） | `issue.related_conversation_ids` 弱引用关联；无强依赖 |
| Cognition → ALL | "User" via tools | Supervisor 通过 CLI 工具调用其他上下文（dispatch / query / issue comment / conversation add-message / 等）—— 都是内部写入，**不知道 vendor 存在** |
| Observability ← ALL | Open Host (subscribe-only) | 所有上下文 emit domain events，Observability 是订阅方 |

### 3.2 Anti-Corruption Layers

| ACL | 位置 | 隔离对象 |
|---|---|---|
| **Bridge（per vendor）** | Bridge BC 内 | 各 vendor（飞书 SDK / DingTalk SDK / Web chat 协议）↔ 领域模块（Issue / Conversation 等）的 API 与事件 |
| **Agent CLI Adapter** | Execution 内 | 各 agent CLI（claude code / codex / opencode）命令格式 / JSONL 模式差异 ↔ 统一 Execution 概念 |
| **BlobStore Adapter** | implementation/01-blob-store.md | LocalDir / S3 实现 ↔ 统一 BlobStore 接口 |

---

## § 4. 不是限界上下文的部件

| 部件 | 性质 |
|---|---|
| **Web Console** | 表现层 / UI 层，不持有自己的聚合；是 Observability + Scheduling + Discussion 的呈现 |
| **CLI** | 命令入口层，跨多个上下文 |
| **BlobStore** | 基础设施抽象，跨多个上下文使用 |
| **`agent-center` binary** | 单一可执行文件容器，不是 BC |
| **Skill 文档** | Agent prompt 注入资源，跟随 binary embed，不是 BC |

---

## § 5. 命名一致性约定（代码 / event_type / 表名）

代码包前缀 / event_type 前缀 / 表名遵循上下文命名：

| 上下文 | 代码包前缀 | event_type 前缀 | 主要表 |
|---|---|---|---|
| Scheduling | `scheduling` | `task.*` / `input_request.*` | `tasks`, `input_requests` |
| Discussion | `discussion` | `issue.*` | `issues`, `issue_comments` |
| Workforce | `workforce` | `worker.*` / `project.*` / `worker_project_proposal.*` / `worker_project_mapping.*` | `workers`, `projects`, `worker_project_mappings`, `worker_project_proposals` |
| Execution | `execution` | `task_execution.*` / `agent_session.*` / `agent_trace.*` / `worktree.*` / `artifact.*` | `task_executions` (可选), `agent_sessions`, `artifacts` |
| Cognition | `cognition` | `supervisor.*` / `memory.*` | `supervisor_invocations`, `agent_memory`, `decision_records` |
| Observability | `observability` | (不产事件) | `events`（跨上下文事件总线表）|
| Conversation | `conversation` | `conversation.*` / `message.*` / `identity.*` | `conversations`, `messages`, `identities`, `channel_bindings` |
| Bridge | `bridge` | `channel.*` / `bridge.*` | (无业务表；各 Bridge 实现可有自己的小审计表，如 `feishu_delivery_ledger`) |

具体 schema 见 [implementation/02-persistence-schema.md](../implementation/02-persistence-schema.md)（TBD）。

---

## § 6. 给 §3-§10 的指引

本文件定下"概念地图"。后续各章节展开**单个 BC 的内部细节**，应遵循：

- **不重新定义术语**：直接引用本文 § 1.1-1.3
- **不跨 BC 引入新动词**：跨 BC 操作走"上下游模式"（如 Customer-Supplier），不重命名
- **BC 内 schema** 归 [implementation/02-persistence-schema.md](../implementation/02-persistence-schema.md)，本架构层只给"聚合 + 字段语义"概念
