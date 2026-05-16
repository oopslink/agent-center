# 0009. Issue 与 Conversation 解耦 + 外部集成走 Bridge

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-16 |
| Refines | [ADR-0007](0007-conversation-as-unified-session.md) |

## Context

[ADR-0007](0007-conversation-as-unified-session.md) 引入了 Conversation 作为渠道无关的会话层，初稿设计是 Issue ↔ Conversation 1:1 强绑：

- 每个 Issue 必须有一个专属的 `kind=issue` Conversation
- IssueComment 实现为该 Conversation 内的 Message（单源存储）
- 上游 BC 通过 `notify-conversation` "发声"，由 ChannelAdapter 投递到 vendor

讨论中发现两个问题：

**1. Issue 的"评论"应该更结构化、Discussion BC 自治**

Issue 评论可能需要独有维度（is_pinned / linked_task_ids / reactions / parent_comment_id / 等），把它绑死 Conversation Message（通用模型）会牺牲灵活性。Issue 是结构化议题，Conversation 是会话时间线，二者关心的事不同。

**2. `notify-conversation` 暗示"对外通知"，跟"领域模块内部写入"的角色混淆**

Supervisor 不应该有"对外发消息"的视角；它只跟系统内部 API 打交道。外部 vendor 集成应该是**外挂的 Bridge 程序**，做双向同步：
- 系统内写入 → bridge 订阅事件 → 推 vendor
- Vendor 回调 → bridge 写回系统内 API

这样：
- 领域模块零 vendor 依赖、可独立测试 / 运行
- 加新 vendor 不动领域模块代码

## Decision

**两件事：**

### 1. Issue 与 Conversation 解耦（不再 1:1）

- Issue 和 Conversation 是**独立聚合根**，各自独立生命周期
- IssueComment 是 Discussion BC 独有的**结构化实体**，单独表 `issue_comments`（不再"实现为 Conversation Message"）
- Issue ↔ Conversation 是**N:N 弱关联**：`issue.related_conversation_ids`（JSON 数组，遵循 [conventions § 9.x](../../rules/conventions.md#-9x-db-schema-尽量减少-join-操作)）
- Conversation 不知道 Issue 存在（单向引用）
- Conversation kind 简化：`dm` / `group_thread` / `adhoc` / `notification`（**删 `issue`**）；删 `context_ref` 字段

### 2. 外部集成走 Bridge 模式（不在领域模块内调 vendor SDK）

- **重命名 BC8**：Channel Gateway → **Bridge**
- 每个 vendor 一个 Bridge 实现（v1 仅 `FeishuBridge`）
- Bridge 职责：
  - **Outbound**：订阅 `issue.*` / `conversation.*` 事件 → 推到 vendor
  - **Inbound**：收 vendor 回调 → 写回 Issue / Conversation 模块的 API
- CLI 重命名：`notify-conversation` → `conversation add-message`（明确"内部写入"）；supervisor 调它跟调 `issue comment` 一样是写系统数据，**不暗示对外**

### 3. Issue Bound Card（在 Bridge 模型下）

Issue 可**可选**绑定一张飞书 card（thread）作为"专属讨论区"：

- `issue.bound_card`（v1 仅 feishu，结构含 `card_message_id` / `thread_key`）
- v1 一个 Issue 至多 1 张 bound card；允许 rebind
- **Bound thread 内的消息只写 IssueComment，不进 Conversation**（不冗余）
- FeishuBridge 负责双向同步：
  - 用户在 bound thread 内回复 → FeishuBridge → 写 IssueComment（kind=text, source_message_ref=vendor_msg_id）
  - Discussion 写入 IssueComment → 事件 → FeishuBridge 推到 bound thread
- 关联 conversation 通过 `related_conversation_ids` 单独管，跟 bound card 是两件事

## Consequences

正面：

- **Issue 模型可独有维度**，未来加 is_pinned / linked_task_ids / reactions 等扩展点无需动 Conversation
- **领域模块零 vendor 依赖**：Conversation / Issue / Task 模块代码不 import 任何飞书 SDK
- **可独立运行**：没 Bridge 时系统仍跑，只是没人能从 vendor 侧 IO
- **加新 vendor 简单**：写一个 Bridge 实现即可，不改领域代码
- **职责清晰**：supervisor 调 `issue comment` / `conversation add-message` 都是内部写入，事件触发后 Bridge 自动同步

负面 / 待跟进：

- 多一份 IssueComment 表（之前是 Message 兼任）
- "Bound thread 不写 Conversation" 是约束 —— FeishuBridge 收消息时要先看是不是 bound thread；增加 Bridge 实现的判断逻辑
- Conversation 跟 Issue 通过 `related_conversation_ids` JSON 关联，做"从 conversation 反查 issue" 查询时需要 LIKE 或全表扫（v1 接受；规模上去再加 cache / 反查表）

## Alternatives Considered

### A. 保留 ADR-0007 原方案（Issue ↔ Conversation 1:1 + IssueComment = Message）

- Pro: 单源存储，无 IssueComment / Message 冗余
- Con: Issue 受 Conversation Message 模型约束，加 issue 特有维度（is_pinned 等）困难

### B. Issue ↔ Conversation 1:1 + IssueComment 独立（hybrid）

- Pro: Issue 自治；Conversation 仍承载 issue 讨论
- Con: 双源存储但生命周期强绑，约束多；conversation 还得知道 issue（kind=issue 仍在）

### C. 当前方案（本 ADR）

- Pro: 各方面解耦最彻底；扩展空间最大
- Con: 关联管理需要额外字段 / 关联触发逻辑

## 影响范围

- 重写 [architecture/03-issue-discussion.md](../architecture/03-issue-discussion.md)
- 重写 [architecture/12-conversation.md](../architecture/12-conversation.md)
- 重写 [architecture/09-feishu-integration.md](../architecture/09-feishu-integration.md)（FeishuBridge）
- 更新 [architecture/01-bounded-contexts.md](../architecture/01-bounded-contexts.md)（UL / BC8 改名 / Context Map）
- 更新 [architecture/10-skill-cli-tooling.md](../architecture/10-skill-cli-tooling.md)（CLI 命名）
- 更新 [architecture/06-supervisor-model.md](../architecture/06-supervisor-model.md)（工具列表）
- 更新 [roadmap.md](../roadmap.md)（Bridge 命名）
- [conventions.md](../../rules/conventions.md) 已加 § 9.x "DB 减少 JOIN" + § 9.y "Bridge 模式"
