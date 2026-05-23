# 限界上下文 & Ubiquitous Language

> **DDD 战略层**

> 本文档定义 agent-center 的领域模型：**通用语言**（vocabulary） + **限界上下文**（bounded contexts） + **上下文映射**（context map）。

DDD 战略设计层。具体聚合 / 实体 / 值对象的字段与状态机迁移细节，分散在后续各章节（[task-runtime/00-overview.md](../tactical/task-runtime/00-overview.md) / [discussion/00-overview.md](../tactical/discussion/00-overview.md) / 等）。

> 🆕 **v2 状态**（per [ADR-0031 Drop Bridge](../../decisions/drafts/0031-v2-drop-bridge-vendor-integration.md) + [ADR-0039 Conversation 业务模型 v2 统一](../../decisions/drafts/0039-conversation-business-model-v2-unified.md)）：
> - BC7 **Bridge BC v2 已撤回**；vendor 接入（飞书 / DingTalk / Web chat / etc.）+ FeishuBridge / LarkCard / ChannelBinding / vendor_msg_ref 等概念**全删**；v3+ 重新设计
> - BC8 **SecretManagement** v2 新增（user secrets 中心化管理，[ADR-0026](../../decisions/drafts/0026-user-secret-management-bc.md)）
> - 本文件下面 § 内残留 Bridge / vendor 描述属 v1 历史；以 ADR-0031 / 0039 为准

---

## § 1. 通用语言（Ubiquitous Language）

全部术语统一遵守：**代码包 / 文档 / CLI / event_type 字符串 / 数据库表名都用同一组词**。违反命名一致性见 [conventions § 12](../../../rules/conventions.md#-12-命名一致)。

### 1.1 核心实体（按上下文归属）

| 术语 | 上下文 | 定义 |
|---|---|---|
| **Task（任务）** | TaskRuntime | 工作单元身份；独立 Aggregate Root，**身份不变**。属于一个 project；可被多次 dispatch（每次产生一条 TaskExecution）。**只有 center 能创建 task，无野任务**（[conventions § 1](../../../rules/conventions.md#-1-单一来源--无野任务)）。**跟 `kind=task` Conversation 1:1 绑定**（`task.conversation_id` 字段；按来源分同步建 / 懒创建两支，见 [ADR-0017](../../decisions/0017-task-as-conversation.md)）。详见 [task-runtime/01-task.md](../tactical/task-runtime/01-task.md) |
| **TaskExecution（任务执行）** | TaskRuntime | Task 的一次执行（dispatch → 结束的运行痕迹）。**独立 Aggregate Root**（持 `task_id` 强引用 Task；[ADR-0019](../../decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md)）；1:N 于 Task；retry = 同 task 新 TaskExecution，**不创建新 task**。每条 TaskExecution 永远绑一个 worker。A2A 状态机驱动。详见 [task-runtime/02-task-execution.md](../tactical/task-runtime/02-task-execution.md) 与 [ADR-0010](../../decisions/0010-task-execution-two-layer-model.md) |
| **InputRequest（请示）** | TaskRuntime | TaskExecution 进行中 agent 卡住需要外部输入的**同步阻塞**请求 |
| **Issue（议题）** | Discussion | 待讨论的事。可由 user / supervisor / worker (agent) 提起；多轮讨论 conclude 后可能产生 0/1/N 个 Task。**跟 `kind=issue` Conversation 1:1 绑定**（`issue.conversation_id` 字段；按来源分同步建 / 懒创建两支，见 [ADR-0021](../../decisions/0021-issue-as-conversation.md)）。详见 [discussion/00-overview.md](../tactical/discussion/00-overview.md) |
| **Project（项目）** | Workforce | 任务归属的逻辑容器（代码 repo / 写作 / 投研 / ...）。每个 task 必属于一个 project |
| **Worker（工人/工作节点）** | Workforce | 用户开发机上的守护进程，能执行任务 |
| **
** | Workforce | 某个 worker 在本地哪个路径有哪个 project 的 worktree-root |
| **AgentInstance（agent 实例）** | Workforce | 一等公民身份：用户起名（如 `coder-mbp`）+ 绑定 Worker + 配置（instructions / mcp / skill）+ 状态机（idle / active / sleeping / archived）。**实际执行进程**仍是 per-TaskExecution spawn 的 CLI 子进程，但所有 execution 都"记账"到对应 AgentInstance；1:N（一个 AgentInstance 可同时承担多个并行 TaskExecution，受 Worker concurrency + AgentInstance.max_concurrent cap）。**Supervisor 是 built-in AgentInstance**（`is_builtin=true`，`worker_id=NULL`，跑在 center 机；archive 禁止；[ADR-0029](../../decisions/drafts/0029-supervisor-as-builtin-agent-instance.md)）。详见 [workforce/04-agent-instance.md](../tactical/workforce/04-agent-instance.md) 与 [ADR-0024](../../decisions/drafts/0024-agent-instance-first-class.md) |
| **UserSecret（用户密钥）** | SecretManagement | 用户管理的密钥实体：name（全局唯一）+ kind（mcp / cloud_credential / ...）+ AES-GCM 加密 value + state machine（active / revoked）。仅服务于 user-domain（MCP env vars 等）；系统内部凭证（BootstrapToken / session_token / 飞书 app_secret）不归本实体。详见 [secret-management/01-user-secret.md](../tactical/secret-management/01-user-secret.md) 与 [ADR-0026](../../decisions/drafts/0026-user-secret-management-bc.md) |
| **SecretRef（密钥引用）** | SecretManagement | VO，引用语法 `secret:<name>`；用在 `mcp_config` 等需要密钥的 config 字段里；DB 持久化时只存引用、不存明文 |
| **Worktree（工作树）** | TaskRuntime | per-execution **动态创建**的 git worktree（仅 `workspace_mode='worktree'` 模式），提供文件隔离。**临时**：跑完保留 24h 再 GC。不进 mapping 表 —— 通过 events + TaskExecution 投影实时呈现。`workspace_mode='direct'` 模式下不创建 worktree，CWD 直接是 `base_path` |
| **WorkerProjectMapping（映射）** | Workforce | (worker_id, project_id) → `base_path` 的稳定映射。**只存稳定的 base_path**；worktree_root 按约定 = `base_path + ".wt"`，不存 |
| **WorkerProjectProposal（提议）** | Workforce | Worker 自动扫描发现的"候选项目映射"。需要用户飞书确认才能升级成 WorkerProjectMapping。状态机 pending → accepted/ignored/superseded |
| **Artifact（产物）** | TaskRuntime | TaskExecution 产生的文件 / 引用（PR URL / 文件 / 报告 / 等），独立表 `artifacts`，归属 execution（TaskExecution 的子实体），free-form kind。详见 [task-runtime/02-task-execution.md § 12](../tactical/task-runtime/02-task-execution.md) |
| **Supervisor（监督者）** | Cognition | 中心的调度官 agent；LLM 驱动；事件触发；spawn 一次 claude code 进程做一次决策周期。**它也是 agent，不是"中央意识"**（[ADR-0003](../../decisions/0003-supervisor-not-brain.md)） |
| **SupervisorInvocation（监督者调用）** | Cognition | Supervisor 一次启动 → 退出的审计单元，含触发事件、prompt、输出、决策记录 |
| **Memory（记忆）** | Cognition | Supervisor 的持久脑，scoped notes（task / issue / conversation / worker / project / global / supervisor）。**物理形态 = file-based + git 仓**（每 scope 一个 `CLAUDE.md`，存 `$AGENT_CENTER_MEMORY_DIR/`），不走 DB 表 —— 详见 [ADR-0012](../../decisions/0012-memory-file-based.md) 与 [cognition/02-memory.md](../tactical/cognition/02-memory.md) |
| **DecisionRecord（决策记录）** | Cognition | Supervisor 在一次 invocation 中显式 emit 的具体决策（通过 CLI 调用动作时记录） |
| **Event（领域事件）** | Observability | 跨上下文的离散状态变化记录。落到 events 表，append-only |
| **AgentTraceEvent** | Observability | Agent JSONL 流中解析出的单条事件（thinking / tool call / tool result / 等）。**不入 events 表**：worker daemon 实时投影到 TaskExecution 摘要 + 写本地 `trace.jsonl`，execution 结束归档至 BlobStore（[ADR-0015](../../decisions/0015-agent-trace-not-in-events-table.md)） |
| **Conversation（会话）** | Conversation | 系统**内部**的消息时间线存储。跟 vendor 解耦（Bridge 负责同步）；`kind=task` 跟 Task 1:1（[ADR-0017](../../decisions/0017-task-as-conversation.md)）；`kind=issue` 跟 Issue 1:1（[ADR-0021](../../decisions/0021-issue-as-conversation.md)） |
| **Message（消息）** | Conversation | Conversation 内的一条留言。`content_kind` ∈ {text / system / agent_finding / supervisor_summary / conclusion_draft / task_proposal}；可空字段 `input_request_ref` 关联到 InputRequest（非空时 Bridge 渲染附按钮，[ADR-0017 § 5](../../decisions/0017-task-as-conversation.md)）；`kind=issue` Conversation 内的 Message 即"议事讨论"（不再有独立 IssueComment 实体，[ADR-0021](../../decisions/0021-issue-as-conversation.md)）；不存 vendor 渲染（卡片由 Bridge 翻译） |
| **Identity（身份）** | Conversation | 参与者的统一身份：`user:hayang` / `supervisor:invocation-id` / `agent:session-id` / `bot`。跨渠道不变 |

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

**Task（4 态，工作单元身份）:**

| 状态 | 含义 |
|---|---|
| `open` | 等开干 / 可被 dispatch |
| `suspended` | 暂停，可恢复 |
| `done` | 已完成（某次 execution `completed` 自动联动）|
| `abandoned` | 决定不做了（终态） |

> Task **没有 `failed`**。失败是某次执行的状态，不是 task 的状态。详见 [task-runtime/01-task.md § 2](../tactical/task-runtime/01-task.md) 与 [ADR-0010](../../decisions/0010-task-execution-two-layer-model.md)。

**TaskExecution（A2A 6 态，一次执行）:**

| 状态 | 含义 |
|---|---|
| `submitted` | 已创建，envelope 已发 / 等 ACK / 等 worker spawn agent |
| `working` | Agent 正在跑 |
| `input_required` | Agent 卡在 InputRequest |
| `completed` | 成功结束（终态） |
| `failed` | 失败结束（含 timeout / worker_lost / dispatch_no_ack 等，详 reason taxonomy）（终态） |
| `killed` | 被显式 kill（user / supervisor / abandon 或 suspend 前置）（终态） |

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

**Worker:**

| 状态 | 含义 |
|---|---|
| `online` | 长连接活跃，心跳正常 |
| `offline` | 长连接断开 |
| `enrolling` | 注册过程中（短暂）|

### 1.4 基础设施词汇

| 术语 | 定义 |
|---|---|
| **BlobStore** | 大文件存储抽象（v1 LocalDirBlobStore，未来 S3）。见 [implementation/01-blob-store.md](../../implementation/01-blob-store.md) |
| **Worktree-root** | Worker 上某个 project 用于派生 task worktree 的根目录 |
| **Dispatch envelope** | Supervisor 派单时下发到 worker 的载荷结构。见 [08-prompt-assembly.md](../tactical/agent-harness/01-prompt-assembly.md) |
| **Skill** | 教 agent 怎么用工具的 markdown 文档（worker-agent.md / supervisor.md）。见 [10-skill-cli-tooling.md](../tactical/agent-harness/02-skill-cli-tooling.md) |
| **CLI 子命令** | `agent-center <op>` 形式的实际工具入口 |
| **Trigger event** | 触发 SupervisorInvocation 的源事件 |

### 1.5 易混淆术语对照

| 用 | **不要**用 | 理由 / 见 |
|---|---|---|
| Supervisor | ~~Brain~~ | [ADR-0003](../../decisions/0003-supervisor-not-brain.md) |
| Issue | ~~Suggestion~~ | [ADR-0004](../../decisions/0004-issue-not-suggestion.md) |
| Worker daemon | ~~Worker（指 host）~~ | "Worker" 一词在本系统专指守护进程 |
| Agent / Worker agent | ~~Worker~~ | 避免与 Worker daemon 混 |
| Memory | ~~knowledge / brain memory~~ | conventions § 12 |
| Worktree | ~~workspace / sandbox~~ | conventions § 12 |
| BlobStore | ~~ObjectStore / FileStore~~ | conventions § 12 |
| TaskExecution | ~~AgentSession / run / session~~ | "一次执行"唯一名词；AgentSession 已下线，见 [ADR-0010](../../decisions/0010-task-execution-two-layer-model.md) |

---

## § 2. 限界上下文（Bounded Contexts）

agent-center 的领域划分为 **7 个限界上下文**。Web Console / CLI / BlobStore 不是 BC（属于表现层 / 基础设施）。

### BC1: TaskRuntime（任务运行时）

**职责**: Task / TaskExecution / InputRequest 全生命周期 + 派单协议 + 派单可靠性 + 子任务层级 + 任务依赖 + Worker 侧运行时（workspace 物理 / shim 模型 / Agent CLI 子进程 / JSONL 解析 / per-execution 目录 / reconcile worker 端 / kill 进程级机制）+ Artifact 收集。**协议与运行时实施同 BC**（[ADR-0019](../../decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md)）；状态权威在 center，实际执行在 worker，物理 split 不切 BC。

**核心聚合**:
- `Task`（独立 Aggregate Root，4 态状态机；身份不变；`conversation_id` 字段绑定 `kind=task` Conversation 1:1；`parent_task_id` 自引用记血缘）
- `TaskExecution`（独立 Aggregate Root；持 `task_id` 强引用 Task，1:N；A2A 6 态状态机；`execution_id` = 主身份 + 幂等 + fencing key；[ADR-0019](../../decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md) / [ADR-0010](../../decisions/0010-task-execution-two-layer-model.md)）
- `InputRequest`（独立 Aggregate Root；通过 `task_execution.pending_input_request_id` 关联到 TaskExecution；UI 投递走 Conversation Message + `input_request_ref` 字段，见 [ADR-0017 § 5](../../decisions/0017-task-as-conversation.md)）
- `Artifact`（TaskExecution 子实体；独立表 `artifacts`；归属 execution；append-only）

**核心事件**:
- `task.created`（payload 含 `conversation_id`）/ `task.priority_changed` / `task.eta_changed` / `task.workspace_mode_changed` / `task.dependency_added` / `task.dependency_removed` / `task.suspended` / `task.resumed` / `task.done` / `task.abandoned` / `task.dispatch_limit_reached`
- `task_execution.created` / `task_execution.dispatched` / `task_execution.working` / `task_execution.input_required` / `task_execution.completed` / `task_execution.failed` / `task_execution.kill_requested` / `task_execution.killed`
- `input_request.requested` / `input_request.responded` / `input_request.timed_out` / `input_request.canceled`
- `worktree.created` / `worktree.released` / `artifact.uploaded` / `task_log.archived` / `task_trace.archived`（agent_trace JSONL 不作为事件流入 events 表，见 [ADR-0015](../../decisions/0015-agent-trace-not-in-events-table.md)）

> **不引入**: `task.bound_card_requested` / `task.progress_milestone_reached`（[ADR-0016](../../decisions/0016-task-progress-via-bound-thread.md) 规划，被 [ADR-0017](../../decisions/0017-task-as-conversation.md) supersede 后撤回 —— 进度走 `conversation.message_added`，绑定信息携带在 `task.created` payload）

**核心操作**:
- 中心端：`dispatch` / `kill-execution` / `abandon-task` / `suspend-task` / `resume-task` / `query tasks` / `query executions` / `respond-to-input-request`
- Worker 端：agent 通过本机 unix socket 调 CLI（`request-input` / `report-progress` / `report-artifact` / `open-issue` / `read-task-context`）
- Worker daemon 也是 Conversation 合法 actor：通过 center 长连 RPC 调 `conversation add-message` 把进度 milestone / agent 请示载体写入 `task.conversation_id`（[ADR-0017 § 4 / § 8](../../decisions/0017-task-as-conversation.md)）

**派单可靠性**: ACK + `execution_id` 幂等 + worker 本机 per-execution 目录 + reconcile 协议 + Shim 模型（detached agent，daemon 升级不影响 agent）（详见 [ADR-0011](../../decisions/0011-dispatch-reliability-protocol.md) + [ADR-0018](../../decisions/0018-detached-agent-via-per-execution-shim.md)）

**关键约束**: 一次 execution = 一个 agent 进程（v1 1:1，[ADR-0010](../../decisions/0010-task-execution-two-layer-model.md)）；AgentSession 概念已下线

**详细设计**: [task-runtime/00-overview.md](../tactical/task-runtime/00-overview.md)（BC wrap）+ [01-task.md](../tactical/task-runtime/01-task.md) + [02-task-execution.md](../tactical/task-runtime/02-task-execution.md) + [03-input-request.md](../tactical/task-runtime/03-input-request.md)

### BC2: Discussion（讨论）

**职责**: Issue 全生命周期 + Issue conclude spawn Tasks（议事消息走 Conversation BC Message，[ADR-0021](../../decisions/0021-issue-as-conversation.md)）

**核心聚合**:
- `Issue`（根，单聚合）

> [ADR-0021](../../decisions/0021-issue-as-conversation.md) 后：**删除 `IssueComment` 实体**；Issue 跟 `kind=issue` Conversation 1:1，议事消息复用 Conversation Message。

**核心事件**: `issue.opened` / `issue.discussion_started` / `issue.concluded` / `issue.withdrawn` / `issue.tasks_spawned`

> 删除项：~~`issue.commented`~~（议事消息走 `conversation.message_added` (kind=issue) 路径）

**核心操作**: `issue open / comment (facade) / conclude / close / bind-conversation` / 飞书 thread 双向同步走 Conversation 路径

**详细设计**: [discussion/00-overview.md](../tactical/discussion/00-overview.md)

### BC3: Workforce（工作池）

**职责**: Worker / AgentInstance / Project 元数据 + BootstrapToken / WorkerProjectMapping / WorkerProjectProposal（自动发现）+ 注册 / 在线状态

**核心聚合**:
- `Worker`（根）+ `BootstrapToken`（实体，子从属；enroll 凭证 stateful，[ADR-0023](../../decisions/drafts/0023-worker-enroll-lightweight.md)）+ `WorkerProjectMapping`（实体，子从属；已生效的稳定映射）
- `AgentInstance`（根，独立聚合；agent 一等公民身份，[ADR-0024](../../decisions/drafts/0024-agent-instance-first-class.md)）
- `WorkerProjectProposal`（根，独立聚合；自动发现的候选，等用户飞书确认才升级为 Mapping）
- `Project`（根，独立聚合）

**核心事件**:
- `worker.enrolled` / `worker.online` / `worker.offline` / `worker.heartbeat` / `worker.config.updated` / `worker.capability.detected`
- `worker.bootstrap_token.issued / used / expired / reissued / revoked`
- `agent_instance.created / config_updated / activated / idle / sleeping / awakened / archived`
- `worker_project_proposal.proposed` / `worker_project_proposal.accepted` / `worker_project_proposal.ignored` / `worker_project_proposal.unignored`
- `worker_project_mapping.added` / `worker_project_mapping.invalidated`（base_path 不再有 git 内容时）
- `project.created` / `project.updated` / `project.removed`

**核心操作**: `worker token issue / reissue / revoke / list` / `worker join` / `worker list / status / config set / capability enable/disable` / `agent create / list / show / config set / archive` / `project add / update / remove / list` / `worker proposal list / unignore`

**详细设计**: [workforce/00-overview.md](../tactical/workforce/00-overview.md)（BC 入口 + § X.1-X.6 wrap）+ [01-worker.md](../tactical/workforce/01-worker.md) + [02-project.md](../tactical/workforce/02-project.md) + [03-worker-project-proposal.md](../tactical/workforce/03-worker-project-proposal.md)（含 WorkerProjectMapping 自动发现流程）+ [04-agent-instance.md](../tactical/workforce/04-agent-instance.md)（[ADR-0024](../../decisions/drafts/0024-agent-instance-first-class.md)）

### BC4: Cognition（认知 / 监督者）

**职责**: Supervisor 运行模型 + Memory + Decision 记录

**核心聚合**:
- `SupervisorInvocation`（根，DB 表 `supervisor_invocations`） + `DecisionRecord`（实体，从属，DB 表 `decision_records`，append-only）
- `Memory`（根，独立聚合；**物理形态 = file-based + git 仓**，存 `$AGENT_CENTER_MEMORY_DIR/`，7 种 scope 按目录树组织；见 [ADR-0012](../../decisions/0012-memory-file-based.md)）

**核心事件**: `supervisor.invocation_started` / `supervisor.invocation_ended` / `supervisor.decision_made` / `supervisor.invocation_failed_alert`

（v1 不 emit `memory.*` 事件 —— memory 变更由 invocation `trace.jsonl.gz`（claude `Edit`/`Write` 工具调用记录在 JSONL 行内）+ `git log` 双渠道审计；[ADR-0012 § 8](../../decisions/0012-memory-file-based.md) / [ADR-0015](../../decisions/0015-agent-trace-not-in-events-table.md)）

**核心操作**:
- 内部 supervisor 唤醒触发器（事件驱动，see [ADR-0013](../../decisions/0013-supervisor-invocation-concurrency.md)）
- CLI `record-decision`（写 no_op 决策行）；动作 CLI（`dispatch` / `kill-execution` / `issue *` / `conversation add-message` / etc.）内部自动 INSERT decision_record
- CLI `supervisor retrigger <invocation-id>`（失败 / 超时人工重发）
- Memory 走 file ops（`Edit` / `Write` 原生工具 + `git commit`），无专用 CLI

**详细设计**: [cognition/00-overview.md](../tactical/cognition/00-overview.md)（BC 入口 + § X.1-X.6 wrap）+ [01-supervisor-invocation.md](../tactical/cognition/01-supervisor-invocation.md) + [02-memory.md](../tactical/cognition/02-memory.md)

### BC5: Observability（观测）

**职责**: 跨上下文事件总线（`events` 表）+ 实时投影（task_execution / worker_status / task_status / fleet）+ 查询接口

**核心聚合**:
- `Event`（append-only 行；表本身是 event stream，单条 immutable）
- `TaskExecutionProjection` / `TaskStatusProjection` / `FleetSnapshot`（读模型，从 events 投影出来）

**核心事件**: 不产生新事件（订阅其它上下文的事件，做投影 / 归档 / 查询）

**核心操作**: `inspect (task|supervisor|session|issue) <id>` / `query <type>` / `ps` / `stats` / `logs`

**详细设计**: [observability/00-overview.md](../tactical/observability/00-overview.md)

### BC6: Conversation（会话）

**职责**: 系统**内部**的会话消息时间线存储。**跟 vendor 解耦**（不直接调 vendor SDK）；承载所有领域 thread（用户对话 / Task / Issue / 通知）的消息时间线。

**核心聚合**:
- `Conversation`（根） + `Message`（实体，从属于 Conversation）
- `Identity`（根，独立聚合）+ `ChannelBinding`（值对象，Identity 在各渠道的 vendor id 映射）

**Conversation 的种类（`kind` 字段）**:
- `dm` —— 用户与 bot 的长期 DM
- `group_thread` —— 群里 @bot 触发的 thread
- `adhoc` —— 短期一次性交互
- `notification` —— 单向通知（周期 review 推送等）
- `task` —— Task 对应的 1:1 会话（[ADR-0017](../../decisions/0017-task-as-conversation.md)）；task done/abandoned → conversation closed
- `issue` —— Issue 对应的 1:1 会话（[ADR-0021](../../decisions/0021-issue-as-conversation.md)）；issue concluded/closed/withdrawn → conversation closed

> [ADR-0021](../../decisions/0021-issue-as-conversation.md) 重新加回 `issue` kind（[ADR-0009](../../decisions/0009-issue-conversation-decoupled-via-bridge.md) 当年删的）；议事消息全部走 `kind=issue` Conversation 的 Message 时间线，IssueComment 实体已删。

**核心事件**: `conversation.opened` / `conversation.message_added` / `conversation.closed` / `identity.registered` / `channel_binding.added`

**核心操作**:
- `conversation add-message --id=X --content=...` (**内部写入**；Bridge 自动订阅事件外发)
- `conversation list [--participant=...] [--kind=...]`
- `inspect conversation <id>` (整个会话的消息时间线)

**对外接口（被其它 BC 调用）**:
- "往 Conversation X 写一条 Message" —— Cognition 等通过 CLI 调用
- emit `conversation.message_added` 事件 —— 任何 BC（含 Bridge）订阅

**详细设计**: [conversation/00-overview.md](../tactical/conversation/00-overview.md)（BC 入口 + § X.1-X.6 wrap）+ [01-conversation.md](../tactical/conversation/01-conversation.md) + [02-identity.md](../tactical/conversation/02-identity.md)

### ~~BC7: Bridge（渠道桥接层）~~

> **v2 已撤回**（per [ADR-0031 v2 Drop Bridge / Vendor Integration](../../decisions/drafts/0031-v2-drop-bridge-vendor-integration.md)）。
>
> v1 Bridge BC 负责 vendor（飞书 / DingTalk / Web chat 等）双向同步；v2 撤回所有 vendor 接入 + Bridge BC 设计/实装。**v2 用户主入口 = Web Console（[ADR-0037](../../decisions/drafts/0037-web-console-as-main-user-ui.md)）+ CLI（[ADR-0038](../../decisions/drafts/0038-cli-ux-enhancement.md)）**。
>
> v3+ 重新设计 Bridge / vendor 接入；vendor 作为 Conversation 业务模型的 view / projection 层（[roadmap.md](../../roadmap.md) v3+「AgentImage 模型 + Memory git 化 / Bridge 重新设计」条）。

### BC8: SecretManagement（用户密钥管理）

**职责**: 中心化管理**用户密钥**（user-domain secrets）：MCP env vars / 云凭据 / 未来 repo deploy key 等。提供加密存储 / 解析 / rotate / revoke / audit。**不管系统内部凭证**（BootstrapToken / session_token / 飞书 app_secret / S3 key）。

**Subdomain kind**: Supporting Domain（不差异化但必要的能力）

**核心聚合**:
- `UserSecret`（根，独立聚合；含 AES-GCM 加密 value + state machine `active / revoked`）

**核心事件**: `user_secret.created` / `user_secret.rotated` / `user_secret.revoked` / `user_secret.accessed` / `user_secret.access_denied`

**核心操作**: `secret create / list / rotate / revoke / usage`（明文不跨 CLI 边界）

**关键约束**:
- DB 不存明文（仅 AES-GCM ciphertext + nonce）
- Master key 从配置文件加载（[implementation § 7.10](../tactical/secret-management/00-overview.md)），**不入 DB / 不入 event / 不入 trace**
- CLI 不打印明文
- worker daemon resolve 校验 `caller.worker_id == agent_instance.worker_id`（防越权）
- 明文仅 worker daemon spawn agent 前短暂落 `home_dir/mcp_config.runtime.json`（mode 0600），execution 后清理

**详细设计**: [secret-management/00-overview.md](../tactical/secret-management/00-overview.md)（BC 入口 + § X.1-X.6 wrap）+ [01-user-secret.md](../tactical/secret-management/01-user-secret.md) + [ADR-0026](../../decisions/drafts/0026-user-secret-management-bc.md)

---

## § 3. 上下文映射（Context Map）

```mermaid
flowchart TB
    subgraph Vendor [vendor 渠道]
        feishu[飞书]
        dingtalk[DingTalk<br/>v2+]
        web[Web chat<br/>v2+]
    end

    subgraph B [BC7 Bridge · ACL · 唯一调 vendor SDK]
        FB[FeishuBridge<br/>v1 必做]
        DB[DingTalkBridge<br/>v2+]
        WB[WebBridge<br/>v2+]
    end

    feishu <-.WebSocket.-> FB
    dingtalk <-.WebSocket.-> DB
    web <-.WebSocket.-> WB

    subgraph Domain [领域层 BC1-BC6]
        BC1["BC1 TaskRuntime<br/>Task / TaskExecution / InputRequest / Artifact<br/>协议 dispatch / kill / reconcile / timeout<br/>+ worker 侧运行时 shim / workspace / JSONL<br/>状态权威 center，物理执行 worker（ADR-0019）"]
        BC2["BC2 Discussion<br/>Issue 单聚合"]
        BC6["BC6 Conversation<br/>Conversation / Message / Identity / ChannelBinding<br/>消息时间线（kind=task / issue / dm / ...）"]
        BC3["BC3 Workforce<br/>Worker / AgentInstance / Project / Mapping / Proposal<br/>算力节点 + agent 一等公民 + 元数据管理"]
    end

    BC2 <-.Shared Kernel 1:1<br/>issue.conversation_id<br/>ADR-0021.-> BC6
    BC1 <-.Shared Kernel 1:1<br/>task.conversation_id<br/>ADR-0017.-> BC6
    BC2 -- Customer-Supplier<br/>IssueConcludeSpawn --> BC1
    BC1 -- Customer-Supplier<br/>open-issue --> BC2
    BC1 <-.Shared Kernel<br/>worker_id / project_id.-> BC3
    BC2 <-.Shared Kernel<br/>project_id.-> BC3

    B == inbound 调 API ==> BC2
    B == inbound 调 API ==> BC6
    B == inbound 调 API ==> BC1
    BC6 -. Pub/Sub<br/>conversation.* .-> B
    BC2 -. Pub/Sub<br/>issue.* .-> B
    BC1 -. Pub/Sub<br/>task.* / input_request.* .-> B

    subgraph CrossCutting [跨切 actor]
        BC4["BC4 Cognition Supervisor<br/>SupervisorInvocation / Memory / DecisionRecord<br/>事件驱动 spawn claude 子进程做决策"]
        BC5["BC5 Observability<br/>events 表 + 读模型 projections<br/>Open Host · subscribe-only"]
    end

    BC1 -. emit events .-> BC5
    BC2 -. emit events .-> BC5
    BC3 -. emit events .-> BC5
    BC4 -. emit events .-> BC5
    BC6 -. emit events .-> BC5
    B -. emit events .-> BC5

    BC4 ==>|"User via tools<br/>dispatch / kill / issue conclude<br/>conversation add-message"| BC1
    BC4 ==>|User via tools| BC2
    BC4 ==>|User via tools| BC6

    classDef domainBox fill:#e8f4f8,stroke:#1e88e5,stroke-width:2px,color:#0d47a1
    classDef bridgeBox fill:#fff3e0,stroke:#fb8c00,stroke-width:2px,color:#e65100
    classDef obsBox fill:#f3e5f5,stroke:#8e24aa,stroke-width:2px,color:#4a148c
    classDef cogBox fill:#fce4ec,stroke:#d81b60,stroke-width:2px,color:#880e4f
    classDef vendorBox fill:#fafafa,stroke:#9e9e9e,stroke-width:1px,color:#424242
    class BC1,BC2,BC3,BC6 domainBox
    class FB,DB,WB bridgeBox
    class BC5 obsBox
    class BC4 cogBox
    class feishu,dingtalk,web vendorBox
```

**关键性质**：

- **领域层 BC1-BC6**：零 vendor 依赖；只跟其它 BC 通过 Shared Kernel / Customer-Supplier 模式交互；所有外发通过 emit domain events
- **Bridge BC7（ACL）**：唯一调 vendor SDK 的地方；订阅领域事件做 outbound；inbound 调领域 API；不持业务聚合（仅 `feishu_delivery_ledger` 等 ACL 内部审计表）
- **Observability BC5（Open Host / Subscribe-only）**：所有 BC emit 事件到 `events` 表；只订阅不发起；提供统一查询接口（inspect / query / ps / stats / logs）；详见 [§ 6 Published Language](#-6-published-language)
- **Cognition BC4（跨切）**：Supervisor 通过 CLI 工具调任何 BC 的动作命令（同 user 用同一套）；不为 supervisor 单造 RPC

   Observability (BC5) ←── subscribe-only ←── ALL contexts emit events
     (订阅所有事件，做投影 / 查询)
```

### 3.1 上下游关系一览

| 上游 → 下游 | 模式 | 内容 |
|---|---|---|
| Discussion → TaskRuntime | Customer-Supplier | Issue conclude 后命令 TaskRuntime 批量创建 Tasks |
| TaskRuntime ↔ Workforce | Shared Kernel | Task / TaskExecution 引用 worker_id / project_id / agent_instance_id（[ADR-0024](../../decisions/drafts/0024-agent-instance-first-class.md)）|
| Workforce → SecretManagement | Customer-Supplier | AgentInstance.config.mcp_config 内嵌 SecretRef；worker daemon spawn 前调 SecretResolutionService.resolve 拿明文（[ADR-0026](../../decisions/drafts/0026-user-secret-management-bc.md) / [ADR-0027](../../decisions/drafts/0027-mcp-per-agent-injection.md)）|
| Cognition → SecretManagement | Read-only metadata | Supervisor 可 `secret list / usage` 看元数据做派单判断；**永不读明文**；create / rotate / revoke 仅用户可做 |
| Discussion ↔ Workforce | Shared Kernel | Issue 引用 project_id |
| TaskRuntime → Discussion | Customer-Supplier | Worker 上的 agent 调 `open-issue` 命令 Discussion 创建 Issue（worker daemon 跑在 TaskRuntime BC 内；agent 通过本机 unix socket 调 CLI 入 TaskRuntime → 跨 BC 调 Discussion） |
| Bridge ↔ vendor | ACL / 双向同步 | Bridge 是唯一调用 vendor SDK 的地方；翻译 incoming 为领域 API 调用；订阅 outbound 事件推到 vendor |
| Bridge → Discussion / Conversation / TaskRuntime | Customer-Supplier | inbound 时 Bridge 调领域模块 API（`conversation add-message` / `task bind-conversation` / `issue bind-conversation` / `InputRequest.respond`）写入数据；slash 命令直接路由（[ADR-0017 § 6](../../decisions/0017-task-as-conversation.md)） |
| Bridge ← Conversation | Pub/Sub | Bridge 订阅 `conversation.message_added` / `conversation.opened` (kind=task/issue) / `input_request.*` 等领域事件做 outbound 投递 / update_card |
| Discussion ↔ Conversation | Shared Kernel / 1:1 | `issue.conversation_id` 强引用 `kind=issue` Conversation（1:1）；`issue.opened` + `conversation.opened` 同事务（[ADR-0021](../../decisions/0021-issue-as-conversation.md)）；议事消息走 Conversation Message；`issue.related_conversation_ids` JSON 数组保留作触发血缘弱关联 |
| TaskRuntime ↔ Conversation | Shared Kernel | `task.conversation_id` 强引用 `kind=task` Conversation（1:1）；`task.created` + `conversation.opened` 同事务；worker daemon / InputRequest 写 Message 走 Conversation API（[ADR-0017](../../decisions/0017-task-as-conversation.md)） |
| Cognition → ALL | "User" via tools | Supervisor 通过 CLI 工具调用其它上下文（`dispatch` / `query` / `issue comment` / `conversation add-message` / 等）—— 都是内部写入，**不知道 vendor 存在** |
| Observability ← ALL | Open Host (subscribe-only) | 所有上下文 emit domain events，Observability 是订阅方 |

> **TaskRuntime 内"协议 vs 运行时"不再是 BC 边界**：派单（center 协议）→ envelope 发出 → worker 端 reconcile / shim / spawn agent 等运行时实施 → emit `task_execution.*` 事件回 center —— 这条链整体在 TaskRuntime BC 内闭合，物理跨机器但概念同 BC（[ADR-0019](../../decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md)）。

### 3.2 Anti-Corruption Layers

| ACL | 位置 | 隔离对象 |
|---|---|---|
| **Bridge（per vendor）** | Bridge BC 内 | 各 vendor（飞书 SDK / DingTalk SDK / Web chat 协议）↔ 领域模块（Issue / Conversation 等）的 API 与事件 |
| **Agent CLI Adapter** | TaskRuntime 内（worker 端） | 各 agent CLI（claude code / codex / opencode）命令格式 / JSONL 模式差异 ↔ 统一 TaskExecution 概念 |
| **BlobStore Adapter** | implementation/01-blob-store.md | LocalDir / S3 实现 ↔ 统一 BlobStore 接口 |

---

## § 4. 不是限界上下文的部件

| 部件 | 性质 |
|---|---|
| **Web Console** | 表现层 / UI 层，不持有自己的聚合；是 Observability + TaskRuntime + Discussion 的呈现 |
| **CLI** | 命令入口层，跨多个上下文 |
| **BlobStore** | 基础设施抽象，跨多个上下文使用 |
| **`agent-center` binary** | 单一可执行文件容器，不是 BC |
| **Skill 文档** | Agent prompt 注入资源，跟随 binary embed，不是 BC |

---

## § 5. 命名一致性约定（代码 / event_type / 表名）

代码包前缀 / event_type 前缀 / 表名遵循上下文命名：

| 上下文 | 代码包前缀 | event_type 前缀 | 主要表 |
|---|---|---|---|
| TaskRuntime | `taskruntime` | `task.*` / `task_execution.*` / `input_request.*` / `worktree.*` / `artifact.*` / `task_log.*` / `task_trace.*` | `tasks`, `task_executions`, `input_requests`, `artifacts` |
| Discussion | `discussion` | `issue.*` | `issues`（议事消息归 Conversation BC `messages` 表；[ADR-0021](../../decisions/0021-issue-as-conversation.md) 删 `issue_comments` 表）|
| Workforce | `workforce` | `worker.*` / `agent_instance.*` / `project.*` / `worker_project_proposal.*` / `worker_project_mapping.*` | `workers`, `bootstrap_tokens`, `agent_instances`, `projects`, `worker_project_mappings`, `worker_project_proposals` |
| SecretManagement | `secretmgmt` | `user_secret.*` | `user_secrets` |
| Cognition | `cognition` | `supervisor.*` | `supervisor_invocations`, `decision_records`（Memory 走 `$AGENT_CENTER_MEMORY_DIR/` git 仓不入 DB；[ADR-0012](../../decisions/0012-memory-file-based.md)） |
| Observability | `observability` | (不产事件) | `events`（跨上下文事件总线表）|
| Conversation | `conversation` | `conversation.*` / `message.*` / `identity.*` | `conversations`, `messages`, `identities`, `channel_bindings` |
| Bridge | `bridge` | `channel.*` / `bridge.*` | (无业务表；各 Bridge 实现可有自己的小审计表，如 `feishu_delivery_ledger`) |

具体 schema 见 [implementation/02-persistence-schema.md](../../implementation/02-persistence-schema.md)（TBD）。

---

## § 6. Published Language

跨 BC 通信的稳定接口（即 DDD "Published Language"）由**两层共同构成**：

| 层 | 内容 | 权威定义位置 |
|---|---|---|
| **领域事件流** | `events` 表 schema + 各 BC emit 的 event_type 闭集（`task.*` / `issue.*` / `worker.*` / `supervisor.*` / `conversation.*` / `channel.*` / `bridge.*`）+ payload 形态（含 reason+message 双字段，[§ 16](../../../rules/conventions.md#-16-错误--状态信息双字段reason--message)）| 各 BC 战术文档 § 8 / `tactical/observability/00-overview.md § 7.5 事件总览` |
| **CLI 命令** | `agent-center <subcommand>` 的子命令集 + 参数 / 返回 schema（user / supervisor / Web Console 共用同一套）| 各 BC 战术文档 § 7 CLI 命令 / `tactical/agent-harness/02-skill-cli-tooling.md` |

**关键性质**：

- **稳定 / 演进**：事件 schema + CLI 接口加版本化策略；ADR 控制 breaking changes（如 [ADR-0019](../../decisions/0019-bc-scheduling-execution-merged-to-task-runtime.md) 改变 BC 边界时，相关 event_type 不动；[ADR-0021](../../decisions/0021-issue-as-conversation.md) 删 `issue.commented` 事件 + 新增 content_kind 是 schema 演进）
- **跨 BC 共享**：所有 BC 一致用同一组词（[conventions § 12](../../../rules/conventions.md#-12-命名一致)）；不允许重命名 / 同义词
- **Open Host Service**：Observability BC 是 PL 的**订阅方**，所有上下文 emit 事件后由 Observability 订阅做投影 / 查询 / 审计；详见 [observability/00-overview.md](../tactical/observability/00-overview.md)
- **零 vendor 依赖**：PL 跟 vendor 无关；Bridge BC 是唯一翻译 vendor 形态 ↔ PL 的 ACL（[conventions § 9.y](../../../rules/conventions.md#-9y-外部集成走-bridge-模式不在领域模块内调-vendor-sdk)）

**不属于 Published Language 的**：

- BC 内部聚合的私有方法 / 字段（如 TaskExecution 的 cancel_requested_at 是 BC 内字段，不暴露为 PL）
- 各 BC 内部表的物理结构（属于 implementation 层）
- AgentTraceEvent（JSONL trace，**不入 events 表**，[ADR-0015](../../decisions/0015-agent-trace-not-in-events-table.md)）
- Bridge BC 内 vendor 翻译 ledger（`feishu_delivery_ledger` 是 BC 内私有审计）

**自检**（设计新跨 BC 接口时必答）：

- 我新增的事件 / CLI 命令属于 PL 吗？是的话 schema 跟现有事件 schema 兼容吗？
- 我用的术语在 [§ 1.1 通用语言表](#-1-通用语言ubiquitous-language) 里吗？
- 我新增的字段是 PL 一部分（跨 BC 可见）还是 BC 内私有？

---

## § 7. 给 § 3-§ 6 的指引

本文件定下"概念地图"。后续各章节展开**单个 BC 的内部细节**，应遵循：

- **不重新定义术语**：直接引用本文 § 1.1-1.3
- **不跨 BC 引入新动词**：跨 BC 操作走"上下游模式"（如 Customer-Supplier），不重命名
- **BC 内 schema** 归 [implementation/02-persistence-schema.md](../../implementation/02-persistence-schema.md)，本架构层只给"聚合 + 字段语义"概念
