# 0039. Conversation 业务模型 v2 统一

| Field | Value |
|---|---|
| Status | Accepted（v2 设计阶段闭环后；2026-05-23）|
| Date | 2026-05-23 |
| Supersedes | [ADR-0017 Task as Conversation](../0017-task-as-conversation.md) / [ADR-0021 Issue as Conversation](../0021-issue-as-conversation.md) / [ADR-0022 Conversation 不对齐 IM 层级](../0022-conversation-not-aligned-with-im-hierarchy.md) |
| Related | 建立于 [ADR-0031 v2 Drop Bridge](0031-v2-drop-bridge-vendor-integration.md) + CV1-CV4（[ADR-0032](0032-conversation-channel-as-first-class.md) / [ADR-0033](0033-identity-model-refactor.md) / [ADR-0034](0034-conversation-participants-field.md) / [ADR-0035](0035-cross-conversation-message-carryover.md) / [ADR-0036](0036-derive-issue-task-from-messages.md)）+ W1 / W2（[ADR-0037](0037-web-console-as-main-user-ui.md) / [ADR-0038](0038-cli-ux-enhancement.md)）|

## Context

v1 通过 ADR-0017 / 0021 / 0022 三个 ADR 定义 Conversation 模型：

- **ADR-0017** (Accepted 2026-05): Task ↔ Conversation 1:1；卡片走飞书 Bridge；inbound 走 Bridge
- **ADR-0021** (Accepted 2026-05): Issue ↔ Conversation 1:1；议事消息走 Conversation Message；删 v0 Issue Comment 表
- **ADR-0022** (Accepted 2026-05): Conversation 不对齐 IM channel/thread 层级；业务对象层级是 source of truth

三者**深度耦合**：都讲 Conversation 跟业务实体 (Task/Issue) 的 1:1 + IM 层级 vs 业务层级；且都依赖当时的 vendor (飞书) 集成。

### v2 后这三个 ADR 全部需要 rewrite

[ADR-0031 v2 Drop Bridge](0031-v2-drop-bridge-vendor-integration.md) 撤回 vendor + CV1-CV4 重新设计 Conversation 业务模型后：

- 0017 / 0021 大量「飞书卡片渲染」/「Bridge inbound 翻译」描述失效
- 0022 「channel 是 Bridge routing hint」描述失效（vendor 撤回后 channel 是业务一等公民 = CV1）
- 三个 ADR 的「v2 形态」散在 [ADR-0032 ~ 0038](0032-conversation-channel-as-first-class.md) 各处

### 决定 supersede + 立一篇 ADR-0039 统一承载 v2 Conversation 业务模型

> 跟用户 2026-05-23 拍板的「(ii) Supersede + 立新 ADR-0039 一篇承载全部」一致。

## Decision

### 1. Conversation 业务模型 v2 = 纯业务时间线 + 关联业务实体

**Conversation 是系统内部「消息时间线」的承载体**，含 Message 子从属 Entity。**不持任何 vendor / 渠道路由信息**（per [ADR-0031](0031-v2-drop-bridge-vendor-integration.md)）；vendor 接入在 v3+ 重新设计时作为 view / projection 层。

业务对象层级是 Conversation 父子链的 source of truth（per [ADR-0022](../0022-conversation-not-aligned-with-im-hierarchy.md) 精神，强化）：

```
Channel (kind=channel, parent=NULL)
├── Issue (kind=issue, parent=Channel.id)
│   ├── carry-over messages（CV3，引用父 channel 部分 message）
│   ├── Issue 议事 messages
│   ├── Task (kind=task, parent=Issue.id)
│   │   └── Task thread (dispatch / agent_finding / InputRequest / 等)
│   └── Task ...
└── Issue ...

DM (kind=dm)，adhoc / notification 不参与父子链
```

### 2. Conversation kind 枚举（6 种，CV1 重命名）

| kind | 描述 |
|---|---|
| `dm` | 1:1 私聊 |
| `channel`（CV1 重命名自 group_thread）| **业务一等公民**；用户起名；多 participant 话题群 |
| `adhoc` | 临时不绑业务对象 |
| `notification` | 系统单向通知 |
| `task` | 跟 Task **1:1** 绑（强引用 task.conversation_id ↔ conversation.id）|
| `issue` | 跟 Issue **1:1** 绑（强引用 issue.conversation_id ↔ conversation.id）|

### 3. Conversation AR 完整字段（v2）

```
conversation (
  id                       ULID
  kind                     dm | channel | adhoc | notification | task | issue
  name                     str?     -- universal nullable；channel 创建时 app 层必填；其他可选
  description              str?     -- universal nullable
  status                   active | closed | archived
  parent_conversation_id   ULID?    -- universal 父子链
  participants             JSON     -- 对象数组，元素 {identity_id, role, joined_at, joined_by, left_at?, left_reason?}
  created_by               str
  created_at               timestamp
  updated_at               timestamp
  archived_at              timestamp?
  archived_by              str?
  message_count            int
  last_message_at          timestamp?
  version                  int
)

UNIQUE INDEX (name) WHERE kind='channel' AND name IS NOT NULL
INDEX (parent_conversation_id) WHERE parent_conversation_id IS NOT NULL
```

### 4. Message Entity（v2）

```
message (
  id                  ULID
  conversation_id     FK → conversations (强引用，不可变)
  sender_identity_id  str         -- 引用 Identity (ADR-0033)
  content_kind        text | system | agent_finding | supervisor_summary | conclusion_draft | task_proposal
  content             text
  direction           inbound | outbound | internal  -- 语义简化；v3+ vendor 接入时再复活
  input_request_ref   ULID?
  posted_at           timestamp
)
```

→ **append-only**：永不修改 / 删除（[ADR-0014](../0014-event-sourcing-level.md)）。

### 5. Identity AR（v2，per ADR-0033）

```
identity (
  id            str         -- 'user:<name>' / 'agent:<agent_instance.id>' / 'system'
  kind          user | agent | system
  display_name  str
  created_at    timestamp
)

UNIQUE INDEX (id)
```

跨聚合 invariant：`agent:<x>` ⇔ AgentInstance 存在 row with id=`<x>`。

### 6. 跨 Conversation Message Carry-over（CV3，ADR-0035）

```
conversation_message_reference (
  id                ULID
  child_conv_id     FK → conversations
  source_msg_id     FK → messages
  source_conv_id    FK → conversations
  order_in_child    int
  created_at        timestamp
  created_by        str
)

UNIQUE INDEX (child_conv_id, source_msg_id)
INDEX (child_conv_id, order_in_child)
INDEX (source_msg_id)
```

子 Conversation 创建时（如 Issue/Task 从 channel messages 派生，CV4）同事务 INSERT 0..N 条 references。append-only message 保证引用永远 valid。

### 7. Task / Issue ↔ Conversation 1:1 关系（保留 ADR-0017 / 0021 精神）

| 关系 | 字段 | 同事务双写 |
|---|---|---|
| Task ↔ Conversation | `task.conversation_id` ↔ `conversation.id`（双向 1:1）| Task create 时同事务建 Conversation kind=task |
| Issue ↔ Conversation | `issue.conversation_id` ↔ `conversation.id` | Issue create 时同事务建 Conversation kind=issue |

→ 1:1 binding 这条规则不变（ADR-0017 / 0021 的核心结论延续），但 vendor 集成 / 飞书卡片 / inbound Bridge 等附属内容删除。

### 8. 状态机（3 态）

```
[*] → active ──→ closed (task done/issue concluded 等触发)
   ↓
   active → archived (用户显式 archive)
   ↓
   closed → archived
   ↓
   archived [terminal, read-only]
```

`archived` = read-only；不再接受 message 写入；UI 默认隐藏；CLI inspect 仍可查。

### 9. 派生入口（CV4，ADR-0036）

CLI: `agent-center issue open / task new --from-conversation=<conv> --select-messages=<msgs> --project=<p>` 同事务建 child Conversation + carry-over refs + Issue/Task。

### 10. 入口面（Web + CLI，per ADR-0037 / 0038）

| Surface | 角色 |
|---|---|
| Web Console (W1) | 用户主入口；SPA + Go API + SSE 实时；channel chat / 派生 UI / InputRequest 回复 / 等 |
| CLI (W2) | 兄弟前端；`conversation send` / `tail -f` / `input-request respond` / 等 |
| Vendor (飞书 etc.) | v2 全删；v3+ 重新设计 |

### 11. 跨 BC 关系

| 关系 | 类型 | 描述 |
|---|---|---|
| TaskRuntime ↔ Conversation | Shared Kernel | task.conversation_id 1:1；同事务双写 |
| Discussion ↔ Conversation | Shared Kernel | issue.conversation_id 1:1；同事务双写 |
| Workforce ← Conversation | Identity[agent].id ↔ AgentInstance.id | 跨聚合 invariant (ADR-0033) |
| ~~Bridge → Conversation~~ | ~~Customer-Supplier~~ | **v2 删**（per ADR-0031）|

## Consequences

**正面**：

- v2 Conversation 模型一篇 ADR 承载全部；读者看 ADR-0039 一篇就懂全图
- 旧 ADR-0017 / 0021 / 0022 保留作历史；status=Superseded 引导读者到 0039
- 业务模型纯净（无 vendor 污染）；v3+ vendor 接入作为 view / projection 添加，干净分层
- 跟 CV1-CV4 完整对齐；模型形态稳定

**负面 / 待跟进**：

- ADR-0017 / 0021 / 0022 内容历史化；新读者需先看 ADR-0039 才理解 v2 模型
- v3+ Bridge 重接入时本 ADR 可能需补充「vendor view 接入点」段落（届时新立 ADR or 本 ADR 加 § 12）

## Alternatives Considered

### A. 各自 rewrite ADR-0017 / 0021 / 0022（保编号）

- ✅ 编号不变
- ❌ 三个 ADR 内容深度耦合；分写后相互引用绕；不如统一一篇
- 否决（用户 pick (ii)）

### B. Hybrid（部分 rewrite + 部分 supersede）

- ❌ 不一致；选定 ii）

### C. 完全删除 ADR-0017 / 0021 / 0022

- ❌ ADR 是历史记录，永不删（[conventions / documentation rules](../../../rules/documentation.md)）
- 否决（标 Superseded 不删）

## References

### Supersedes

- [ADR-0017 Task as Conversation](../0017-task-as-conversation.md)
- [ADR-0021 Issue as Conversation](../0021-issue-as-conversation.md)
- [ADR-0022 Conversation 不对齐 IM 层级](../0022-conversation-not-aligned-with-im-hierarchy.md)

### 建立于

- [ADR-0031 v2 Drop Bridge](0031-v2-drop-bridge-vendor-integration.md)
- [ADR-0032 CV1 Channel](0032-conversation-channel-as-first-class.md)
- [ADR-0033 Identity refactor](0033-identity-model-refactor.md)
- [ADR-0034 Participants 字段](0034-conversation-participants-field.md)
- [ADR-0035 CV3 Carry-over](0035-cross-conversation-message-carryover.md)
- [ADR-0036 CV4 派生入口](0036-derive-issue-task-from-messages.md)
- [ADR-0037 W1 Web Console](0037-web-console-as-main-user-ui.md)
- [ADR-0038 W2 CLI UX](0038-cli-ux-enhancement.md)

### 跨 BC

- [conversation/00-overview.md](../../architecture/tactical/conversation/00-overview.md) - BC 入口
- [conversation/01-conversation.md](../../architecture/tactical/conversation/01-conversation.md) - Conversation + Message schema
- [conversation/02-identity.md](../../architecture/tactical/conversation/02-identity.md) - Identity AR

### 来源

- 2026-05-22 → 2026-05-23 #agent-center 讨论；v2 设计阶段闭环后用户 pick (ii) supersede + 新立
