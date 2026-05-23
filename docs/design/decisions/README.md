# 架构决策记录（ADR）

记录 agent-center 项目所有架构与实现的**分叉路口决定**。

格式见 [docs/rules/documentation.md § 5](../../rules/documentation.md#5-adr-格式)。

| # | 标题 | 状态 |
|---|---|---|
| 0001 | [不引入 MCP](0001-no-mcp.md) | Accepted |
| 0002 | [不用 LLM SDK，走 CLI agent](0002-no-llm-sdk-use-cli-agents.md) | Accepted |
| 0003 | [调度官命名为 Supervisor 而非 Brain](0003-supervisor-not-brain.md) | Accepted |
| 0004 | [Issue 取代 Suggestion](0004-issue-not-suggestion.md) | Accepted |
| 0005 | [项目宪章留在项目仓库](0005-project-charter-stays-in-project-repo.md) | Accepted |
| 0006 | [大文件走 BlobStore，DB 只存相对路径](0006-blob-store-for-large-content.md) | Accepted |
| 0007 | [引入 Conversation 层作为渠道无关的会话上下文](0007-conversation-as-unified-session.md) | Accepted (Refined by 0009 → 0021) |
| 0008 | [WorkerProjectMapping 走"自动发现 + 用户确认"流程](0008-worker-project-mapping-via-discovery-proposal.md) | Accepted |
| 0009 | [Issue 与 Conversation 解耦 + 外部集成走 Bridge](0009-issue-conversation-decoupled-via-bridge.md) | Superseded by 0021 |
| 0010 | [Task / TaskExecution 两层模型 + AgentSession 合并](0010-task-execution-two-layer-model.md) | Accepted |
| 0011 | [Dispatch 可靠性协议：ACK + execution_id 幂等 + Reconcile](0011-dispatch-reliability-protocol.md) | Accepted (Refined by 0018) |
| 0012 | [Supervisor Memory 走 file-based + git](0012-memory-file-based.md) | Accepted |
| 0013 | [Supervisor Invocation 并发模型：per-scope 串行 + 跨 scope 并行](0013-supervisor-invocation-concurrency.md) | Accepted |
| 0014 | [事件溯源走 L1：状态表为权威，事件表是审计流](0014-event-sourcing-level.md) | Accepted |
| 0015 | [agent_trace 不进 events 表：归 BlobStore + TaskExecution 投影摘要](0015-agent-trace-not-in-events-table.md) | Accepted |
| 0016 | [Task 进度跟踪走 bound thread + 进度消息流](0016-task-progress-via-bound-thread.md) | Superseded by 0017 |
| 0017 | [Task 即 Conversation：1:1 绑定 + 所有 task UI 走统一 Message 时间线](0017-task-as-conversation.md) | Accepted (Refined by 0021) |
| 0018 | [Detached agent execution via per-execution shim](0018-detached-agent-via-per-execution-shim.md) | Accepted |
| 0019 | [BC1 Scheduling + BC4 Execution 合并为 TaskRuntime](0019-bc-scheduling-execution-merged-to-task-runtime.md) | Accepted |
| 0020 | [Card 限制在 Bridge BC：Issue 字段精简 + Card 元数据归 Bridge ledger](0020-card-confined-to-bridge-bc.md) | Superseded by 0021 |
| 0021 | [Issue 即 Conversation：1:1 绑定 + 所有 Issue IO 走统一 Message 时间线](0021-issue-as-conversation.md) | Accepted |
| 0022 | [Conversation 不对齐 IM 软件的 channel/thread 层级模型](0022-conversation-not-aligned-with-im-hierarchy.md) | Accepted |
| 0023 | [Worker Enroll 轻量化](drafts/0023-worker-enroll-lightweight.md) | Draft |
| 0024 | [AgentInstance 一等公民化](drafts/0024-agent-instance-first-class.md) | Draft（amended by 0029） |
| 0025 | [`agent:create` 协议 = G1 CLI Endpoint](drafts/0025-agent-create-via-cli-not-protocol.md) | Draft |
| 0026 | [SecretManagement BC：中心化用户密钥管理](drafts/0026-user-secret-management-bc.md) | Draft |
| 0027 | [MCP per-agent 注入](drafts/0027-mcp-per-agent-injection.md) | Draft |
| 0028 | [Skill File Mount（v2 lite，G5）](drafts/0028-skill-file-mount-lite.md) | Draft |
| 0029 | [Supervisor as Built-in AgentInstance](drafts/0029-supervisor-as-builtin-agent-instance.md) | Draft（amends 0024） |
| 0030 | [AgentAdapter 矩阵扩展（v2 G3）](drafts/0030-agentadapter-matrix-expansion.md) | Draft |
| 0031 | [v2 Drop Bridge / Vendor Integration（飞书等 IM 接入暂撤）](drafts/0031-v2-drop-bridge-vendor-integration.md) | Accepted（meta scope decision）|
| 0032 | [Conversation Channel 业务一等公民 + Conversation schema reset（v2 CV1）](drafts/0032-conversation-channel-as-first-class.md) | Draft |
| 0033 | [Identity 模型重构（v2 CV2a）](drafts/0033-identity-model-refactor.md) | Draft |
| 0034 | [Conversation Participants 字段（v2 CV2b）](drafts/0034-conversation-participants-field.md) | Draft |

## 规则提示

- 编号严格递增、永不复用
- 推翻先前决定 → 原 ADR 标 `Superseded by ADR-NNNN`，**不删旧 ADR**
- 详见 [文档管理规则 §5-6](../../rules/documentation.md#5-adr-格式)
