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
| 0007 | [引入 Conversation 层作为渠道无关的会话上下文](0007-conversation-as-unified-session.md) | Accepted |
| 0008 | [WorkerProjectMapping 走"自动发现 + 用户确认"流程](0008-worker-project-mapping-via-discovery-proposal.md) | Accepted |
| 0010 | [Task / TaskExecution 两层模型 + AgentSession 合并](0010-task-execution-two-layer-model.md) | Accepted |
| 0011 | [Dispatch 可靠性协议：ACK + execution_id 幂等 + Reconcile](0011-dispatch-reliability-protocol.md) | Accepted (Refined by 0018) |
| 0012 | [Supervisor Memory 走 file-based + git](0012-memory-file-based.md) | Accepted |
| 0013 | [Supervisor Invocation 并发模型：per-scope 串行 + 跨 scope 并行](0013-supervisor-invocation-concurrency.md) | Accepted |
| 0014 | [事件溯源走 L1：状态表为权威，事件表是审计流](0014-event-sourcing-level.md) | Accepted |
| 0015 | [agent_trace 不进 events 表：归 BlobStore + TaskExecution 投影摘要](0015-agent-trace-not-in-events-table.md) | Accepted |
| 0016 | [Task 进度跟踪走 bound thread + 进度消息流](0016-task-progress-via-bound-thread.md) | Superseded by 0017→已 v2 删 → ADR-0039 接管 |
| 0018 | [Detached agent execution via per-execution shim](0018-detached-agent-via-per-execution-shim.md) | Accepted |
| 0019 | [BC1 Scheduling + BC4 Execution 合并为 TaskRuntime](0019-bc-scheduling-execution-merged-to-task-runtime.md) | Accepted |
| 0023 | [Worker Enroll 轻量化](0023-worker-enroll-lightweight.md) | Accepted |
| 0024 | [AgentInstance 一等公民化](0024-agent-instance-first-class.md) | Accepted (amended by 0029) |
| 0025 | [`agent:create` 协议 = G1 CLI Endpoint](0025-agent-create-via-cli-not-protocol.md) | Accepted |
| 0026 | [SecretManagement BC：中心化用户密钥管理](0026-user-secret-management-bc.md) | Accepted |
| 0027 | [MCP per-agent 注入](0027-mcp-per-agent-injection.md) | Accepted |
| 0028 | [Skill File Mount（v2 lite，G5）](0028-skill-file-mount-lite.md) | Accepted |
| 0029 | [Supervisor as Built-in AgentInstance](0029-supervisor-as-builtin-agent-instance.md) | Accepted (amends 0024) |
| 0030 | [AgentAdapter 矩阵扩展（v2 G3）](0030-agentadapter-matrix-expansion.md) | Accepted |
| 0031 | [v2 Drop Bridge / Vendor Integration（飞书等 IM 接入暂撤）](0031-v2-drop-bridge-vendor-integration.md) | Accepted |
| 0032 | [Conversation Channel 业务一等公民 + Conversation schema reset（v2 CV1）](0032-conversation-channel-as-first-class.md) | Accepted |
| 0033 | [Identity 模型重构（v2 CV2a）](0033-identity-model-refactor.md) | Accepted |
| 0034 | [Conversation Participants 字段（v2 CV2b）](0034-conversation-participants-field.md) | Accepted |
| 0035 | [跨 Conversation Message Carry-over（v2 CV3）](0035-cross-conversation-message-carryover.md) | Accepted |
| 0036 | [从 Conversation Messages 派生 Issue / Task（v2 CV4）](0036-derive-issue-task-from-messages.md) | Accepted |
| 0037 | [Web Console 升为 v2 用户主入口（v2 W1）](0037-web-console-as-main-user-ui.md) | Accepted |
| 0038 | [CLI UX 增强（v2 W2）](0038-cli-ux-enhancement.md) | Accepted |
| 0039 | [Conversation 业务模型 v2 统一（supersedes 0017/0021/0022）](0039-conversation-business-model-v2-unified.md) | Accepted |
| 0054 | [Task 增加 delivered / blocked 两个非终态（park），修正 ADR-0046](0054-task-delivered-blocked-parked-states.md) | Accepted (amends 0046) |

## 规则提示

- 编号严格递增、永不复用
- 推翻先前决定 → 原 ADR 标 `Superseded by ADR-NNNN`，**不删旧 ADR**
- **例外（v2 一次性）**：v2 撤回 vendor / Bridge 时，0009 / 0017 / 0020 / 0021 / 0022 经用户决定（2026-05-23 "删干净"）彻底删除。空号但接续：v2 ADR 0023+ 沿用；新 ADR 从 0040+ 起
- **v2 ADR drafts 全部 promoted to Accepted on 2026-05-24** (P12 S4 — see [s4-adr-promote-audit.md](../../plans/phase-12-audits/s4-adr-promote-audit.md))；`decisions/drafts/` 目录已空，未来 ADR 直接在 `decisions/` 下创建（Status: Draft → 成熟后原地翻 Accepted）
- 详见 [文档管理规则 §5-6](../../rules/documentation.md#5-adr-格式)
