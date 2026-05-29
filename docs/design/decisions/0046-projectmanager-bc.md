# 0046. ProjectManager BC + Task/Issue ownership（v2.7）

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-05-29 |
| Delivered | v2.7 design phase；详 [v2.7-domain-refactor-plan § 2.1 / § 2.2 / § 3.1 / § 5 B](../../plans/v2.7-domain-refactor-plan.md) |
| Supersedes | supersedes [ADR-0036 从 Conversation Messages 派生 Issue/Task](0036-derive-issue-task-from-messages.md)（v2.7 改为 ProjectManager 显式拥有 Task/Issue，状态不由消息推断） |
| Related | [ADR-0047 Conversation owner_ref](0047-conversation-owner-ref-and-context-refs.md) / [ADR-0049 Agent BC](0049-agent-bc-no-agentrun.md) / [ADR-0052 Subscriber 真值 + participant 投影](0052-subscriber-truth-and-participant-projection.md) |

## Context

v2 把 Issue/Task 作为从 Conversation 消息派生的产物（[ADR-0036](0036-derive-issue-task-from-messages.md)），工作管理逻辑散落、状态语义弱。v2.7 要把"什么工作存在、谁该被通知、状态如何流转"收敛成一个**工作管理真值** BC，并明确 Task/Issue 都从属于 Project、不存在全局/跨 Project 工作项。

## Decision

### 1. 立 ProjectManager 为独立 BC（项目管理域）

承载工作管理真值：Project、ProjectMember、Issue、Task、订阅真值、状态流转。Conversation 不反推业务状态（[ADR-0047](0047-conversation-owner-ref-and-context-refs.md)）。

### 2. 聚合清单（Project 非巨型聚合）

| AR | 职责 |
|---|---|
| Project | 项目身份、名称、描述、生命周期 |
| ProjectMember | 项目成员；投递范围 + 最小写入闸（plan § 10 OQ6） |
| Issue | 项目内问题/讨论项及其状态 |
| Task | 项目内工作单元及指派状态 |
| TaskSubscriber | Task 手动订阅真值 |
| IssueSubscriber | Issue 手动订阅真值 |
| CodeRepoRef | 挂在 Project 上的代码仓库引用 |

- Task/Issue **不**作为 `Project.tasks[]` / `Project.issues[]` 子数组存储；Project 不是巨型聚合。

### 3. 归属与作用域不变量

- Task 必属于且仅属于一个 Project；Issue 同理。
- 无全局 Task、无跨 Project Task。
- Task 可独立、或派生自某 Issue。
- Task、Issue 各绑定一条稳定 Conversation（owner_ref `pm://...`，详 [ADR-0047](0047-conversation-owner-ref-and-context-refs.md)）。

### 4. 状态机（plan § 2.2）

Issue：`open → in_progress → resolved → closed`；`open/in_progress → withdrawn`；`resolved/closed → reopened → open`。`triaged` 非 v2.7。

Task：`open → assigned → running → blocked → running`；`running → completed → verified`；`open/assigned/running/blocked → canceled`；`completed/verified → reopened → open`；`assigned → open`（unassign）。

规则：
- `blocked` 必带 Conversation 消息/原因。
- `completed` 由 assignee/agent 置。
- `verified` 由项目内有权限且**非本任务完成者**置（可含另一 Agent；禁自验，plan § 2.2 / § 10 OQ4）。
- 状态绝不由 Conversation 消息系统推断。

### 5. AppServices

CreateProject、AddProjectMember、CreateIssue、CreateTask、AssignTask、ReassignTask、Subscribe/Unsubscribe Task/Issue、BlockTask、CompleteTask、VerifyTask（订阅同步与跨 BC 副作用走 outbox，详 [ADR-0052](0052-subscriber-truth-and-participant-projection.md) / plan § 10 OQ1）。

### 6. 域隔离（无角色权限）

- ProjectMember = 投递范围 + 最小写入闸；写操作前提是该 Project 成员。
- v1 有域隔离、无角色细分权限；Org member 是 Project member 前提（plan § 10 OQ6）。
- 完备权限（Project admin/manager 角色、creator mute）留 roadmap。

## Consequences

### 正面

- 工作管理有单一真值 BC，状态语义强、可校验。
- Project 作用域不变量杜绝全局/跨项目工作项混乱。
- 为 Agent 指派（[ADR-0049](0049-agent-bc-no-agentrun.md)）与订阅投影（[ADR-0052](0052-subscriber-truth-and-participant-projection.md)）提供清晰上游。

### 负面 / 待跟进

- v2.7 不做向后兼容，旧 Issue/Task 数据 drop-and-recreate。
- 跨 BC 副作用经 outbox，最终一致；AppService 编排需保证本 BC 状态 + outbox 同事务。
- 无角色权限在多人协作下不足，需 roadmap 补。

## Alternatives Considered

### A. 保留 v2 "从消息派生 Issue/Task"

- ❌ 状态语义弱、易被消息误推；与"状态不由消息推断"冲突。否决。

### B. Project 作为巨型聚合含 Issue[]/Task[]

- ❌ 聚合过大、并发与事务边界差。否决，改多 AR。

### C. 允许全局/跨 Project Task

- ❌ 破坏作用域不变量与域隔离。否决。

## References

- [v2.7-domain-refactor-plan § 2.1](../../plans/v2.7-domain-refactor-plan.md) / [§ 2.2](../../plans/v2.7-domain-refactor-plan.md) / [§ 3.1](../../plans/v2.7-domain-refactor-plan.md) / [§ 4.1-4.2](../../plans/v2.7-domain-refactor-plan.md) / [§ 5 B](../../plans/v2.7-domain-refactor-plan.md) / [§ 10 OQ4/OQ6](../../plans/v2.7-domain-refactor-plan.md)
- [ADR-0047](0047-conversation-owner-ref-and-context-refs.md) / [ADR-0049](0049-agent-bc-no-agentrun.md) / [ADR-0052](0052-subscriber-truth-and-participant-projection.md)
- 来源：2026-05-27 ～ 2026-05-29 DM 设计讨论（@oopslink ↔ @AgentCenterPD）
