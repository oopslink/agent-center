# 0052. Subscriber 真值 + Conversation participant 投影（v2.7）

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-05-29 |
| Delivered | v2.7 design phase；详 [v2.7-domain-refactor-plan § 2.1 / § 3.4 / § 4.1-4.2 / § 10 OQ1](../../plans/v2.7-domain-refactor-plan.md) |
| Supersedes | amends [ADR-0034 Conversation Participants 字段](0034-conversation-participants-field.md)（participant 由"真值字段"降为"从订阅真值同步的投影"） |
| Related | [ADR-0046 ProjectManager BC](0046-projectmanager-bc.md)（订阅真值归属） / [ADR-0047 Conversation owner_ref](0047-conversation-owner-ref-and-context-refs.md) |

## Context

[ADR-0034](0034-conversation-participants-field.md) 把 participants 作为 Conversation 上的字段。v2.7 引入 ProjectManager 后，"谁该被通知"对 task/issue 会话是**业务订阅问题**，应由 ProjectManager 拥有真值；Conversation 的 participant 退为消息层投影，避免两处各执一份真值导致漂移。

## Decision

### 1. 订阅真值归 ProjectManager

- Task/Issue 的订阅真值 = ProjectManager 的 TaskSubscriber / IssueSubscriber（[ADR-0046](0046-projectmanager-bc.md)）。
- 有效订阅者 = creator + 当前 assignee + 手动订阅者。
- creator、当前 assignee 始终是有效订阅者；手动订阅者叠加。
- **重派 / unassign：出局 assignee 被保留**——自动转成 **manual 订阅者**（sticky），继续留在会话，**不**自动移除（修订 2026-05-29，见 plan §10 OQ13）。只有显式 Unsubscribe（踢出）才离开。
- creator 不可被移除；当前 assignee 在其角色未变时不可被移除（但一旦被 unassign 即降为 manual 订阅者，此后可被显式 Unsubscribe）。creator mute 留 roadmap。

### 2. ConversationParticipant = 投影（仅 issue/task）

- **仅对 task/issue 会话**，ConversationParticipant 是从订阅真值**同步出来的消息层投影**，不是订阅真值本身（plan § 3.4）。
- **channel** 会话（org 级通用群聊，plan §10 OQ10）成员是**显式**的，由 Conversation BC 直接维护，**不从 ProjectManager 投影**。
- **dm** 会话参与者按现状，自管。
- 即：投影方向 ProjectManager→Conversation **只作用于 issue/task**；channel/dm 不在投影范围内。

### 3. 同步经 outbox（plan § 10 OQ1）

- 改订阅真值的 AppService（CreateTask、AssignTask/ReassignTask、Subscribe/Unsubscribe、AddProjectMember…）在本 BC 事务内写状态 + outbox 事件；幂等投影器把订阅 diff 同步到 ConversationParticipant。
- 方向单一：**ProjectManager → Conversation**，绝不反向。
- 一致性为最终一致（毫秒级）；UI 容忍极短延迟。

### 4. 不变量

- ConversationParticipant 不得成为 task/issue 订阅的写入真值。
- Conversation 不反推业务状态（[ADR-0047](0047-conversation-owner-ref-and-context-refs.md)）。

## Consequences

### 正面

- 单一真值（ProjectManager）+ 单向投影，杜绝两份订阅各自漂移。
- 重派/订阅变更的 participant diff 有确定来源与可重放投影。

### 负面 / 待跟进

- participant 与订阅真值最终一致，存在极短不一致窗口。
- 投影器必须幂等（按 event_id 去重），需测试覆盖重放/重复投递。
- channel（org 级）成员显式、自管，不在投影范围；投影只覆盖 issue/task，边界更清。

## Alternatives Considered

### A. participant 仍为真值（保留 [ADR-0034](0034-conversation-participants-field.md)）

- ❌ 与 ProjectManager 订阅真值两处并存、易漂移。否决，降为投影。

### B. Conversation 反向决定订阅

- ❌ 业务通知归属应在 ProjectManager；反向会让消息层污染业务真值。否决。

### C. 同步用同事务直接调（不走 outbox）

- ❌ 与 plan § 10 OQ1"现在就上 outbox"冲突；跨进程/上云需返工。否决。

## References

- [v2.7-domain-refactor-plan § 2.1](../../plans/v2.7-domain-refactor-plan.md) / [§ 3.4](../../plans/v2.7-domain-refactor-plan.md) / [§ 4.1-4.2](../../plans/v2.7-domain-refactor-plan.md) / [§ 5 B](../../plans/v2.7-domain-refactor-plan.md) / [§ 10 OQ1](../../plans/v2.7-domain-refactor-plan.md)
- [ADR-0046](0046-projectmanager-bc.md) / [ADR-0047](0047-conversation-owner-ref-and-context-refs.md)
- 来源：2026-05-27 ～ 2026-05-29 DM 设计讨论（@oopslink ↔ @AgentCenterPD）
