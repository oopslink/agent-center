# 0034. Conversation Participants 字段（v2 CV2b）

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-05-23 |
| Related | v2 议题 CV2；并行立 [ADR-0033 Identity 模型重构](0033-identity-model-refactor.md)（CV2a）；建立于 [ADR-0032 CV1 Channel 业务一等公民](0032-conversation-channel-as-first-class.md) + [ADR-0024 AgentInstance](0024-agent-instance-first-class.md) 之上 |

## Context

CV1 [ADR-0032](0032-conversation-channel-as-first-class.md) 落地后 channel 业务一等公民化；但「**channel 必有 participant**」 invariant 留口（细节待 CV2 议题定）。当前模型无显式 participant：

- Conversation 无 participant 字段 / 表
- 「谁在 channel」靠 SQL 扫 `messages.sender_identity_id DISTINCT` 间接推断
- 缺：preemptive invite（拉人进群但还没发言）/ 通知 / 权限基础

CV2 议题要补这个 gap。

### 用户表态（2026-05-23 #agent-center）

> 「conversation 有个 participants 字段 这样如何」 → **字段形态**而非独立表

> 「对象数组」 → 元素含 role / joined_at 等

## Decision

### 1. Conversation.participants 字段（JSON 对象数组）

```diff
 conversation (
   ... 现有 CV1 字段（[ADR-0032](0032-conversation-channel-as-first-class.md)）...
+  participants  JSON     -- 对象数组，元素 schema 见下
 )
```

**元素 schema**：

```json
{
  "identity_id": "agent:01HE6T9N...",   // 引用 Identity (ADR-0033)
  "role": "member",                       // v2 仅 'member'；隐式 owner = Conversation.created_by
  "joined_at": "2026-05-23T10:30:00Z",
  "joined_by": "user:hayang",             // 谁拉的；自动加时 = 'system' / 'auto'
  "left_at": null,                        // null = 仍在 channel
  "left_reason": null                     // voluntary | kicked | archived_conversation
}
```

> 不引入独立 `participant` 表 —— 元素挂 Conversation JSON。v2 单用户 / 小规模场景查询性能 OK；v3+ 多用户场景大时再迁表。

### 2. Identity ID 引用（依赖 ADR-0033）

`participants[i].identity_id` 必须是合法 Identity.id 格式：

- `user:<name>`
- `agent:<agent_instance.id>` (ULID)
- `system`

应用层校验：identity 存在 + active（v2 暂无 identity archive，always active）。

### 3. ParticipantRole

| v2 | 含义 |
|---|---|
| `member` | 唯一 role 值；含义 = 「在 channel 里，能读写 message」 |

**隐式 owner**：channel 的 `created_by` (Conversation AR 字段，CV1 引入) 是 implicit owner —— `invite / kick / archive` 权限校验靠这个字段，不靠 role。

→ v3+ 多用户场景再加 `admin` / `viewer` 等 role。

### 4. Auto-add 规则

| 触发 | 自动加 participants |
|---|---|
| Conversation create（任意 kind）| `created_by` actor（首个 participant；joined_by='system'）|
| Channel create | 同上；creator 即首个 member |
| Task dispatch | task conversation 加：派单 supervisor (agent:<supervisor_agent_instance.id>) + 执行 agent (agent:<worker_agent_instance.id>) + 任务创建者 user identity |
| Issue create | issue conversation 加：opener identity + supervisor identity |
| DM | 双方同时加 |
| Channel invite (CLI) | 显式加 |
| Channel leave (CLI) | left_at = now, left_reason='voluntary'（不删除元素，保留审计血缘）|
| Channel kick (CLI) | left_at = now, left_reason='kicked' |

### 5. 严格 join 规则

**消息发送前必须是 active participant**（left_at IS NULL）：

```
post_message(conversation_id, sender_identity_id, ...):
  IF sender 不在 participants OR sender.left_at IS NOT NULL:
    → reject "not a participant of this conversation"
```

→ 不允许「silent join on first message」—— 必须先 invite / auto-add 完成才能 sender 写 message。

例外：系统自动流程（task dispatch / issue create 等）在派单事务内同步加 participants，所以 agent / supervisor 发 message 时 always 已 in。

### 6. Invite / Leave / Kick CLI

| 命令 | 谁能做 | 行为 |
|---|---|---|
| `agent-center channel invite <conv-name> <identity-id>` | channel owner (`created_by`) | 添加 participants 元素 |
| `agent-center channel leave <conv-name>` | 自己（取 current actor）| 把自己 element 标 left_at + voluntary |
| `agent-center channel kick <conv-name> <identity-id>` | channel owner | 把目标 element 标 left_at + kicked |
| `agent-center channel participants <conv-name>` | 任何人 | 列 channel participants（active + 历史）|

**通用版**（用于 task / issue / DM 等其他 kind）：

```
agent-center conversation participants <conv-id>
```

只 channel kind 暴露 invite / leave / kick；其他 kind 的 participants 由系统流程管理（用户不直接 mutate）。

### 7. 事件

| 事件 | payload |
|---|---|
| `conversation.participant_joined` | `conversation_id, identity_id, joined_by, joined_at` |
| `conversation.participant_left` | `conversation_id, identity_id, left_at, left_reason` |

### 8. Invariants

1. **元素 `identity_id` 必须 valid Identity ID**（kind:id 格式，应用层校验存在）
2. **同 identity 在同 conversation 至多 1 个 active 元素**（left_at IS NULL；旧 element 保留作历史不删除）
3. **创建 Conversation 时自动加 created_by actor 进 participants**（同事务 INSERT；conversation 不会有零 participant）
4. **archived Conversation 不能 invite / kick / leave**（read-only invariant from CV1）
5. **kind=channel 必有至少 1 active participant**（创建时 owner 自动加；leave / kick 后若剩 0，channel 自动转 archived？v2 不强制此条；owner 永远是 active）

### 9. CLI 例子

```
$ agent-center channel create --name=ac-design --description="agent-center 设计讨论"
✓ Created channel 'ac-design'. Owner: user:hayang. Initial participants: [user:hayang]

$ agent-center channel invite ac-design agent:01HE6T9N0X3
✓ Added agent:01HE6T9N0X3 (display_name: coder-mbp) to 'ac-design'.

$ agent-center channel participants ac-design
identity_id                role     joined_at              joined_by         left_at  left_reason
user:hayang                member   2026-05-23T10:30:00Z   system            -        -
agent:01HE6T9N0X3          member   2026-05-23T10:31:12Z   user:hayang       -        -

$ agent-center channel kick ac-design agent:01HE6T9N0X3
✓ Removed agent:01HE6T9N0X3 from 'ac-design'.
```

## Consequences

**正面**：

- 显式 participant 模型；channel 「谁在群里」明确
- 字段形态简单（无新表 / 新 AR）；JSON 数组 v2 小规模查询足够
- 元素含 role / joined_at 等 metadata，未来加 mention preference / read status 等不破结构
- 跟 [ADR-0033 Identity refactor](0033-identity-model-refactor.md) 配合：identity_id 引用稳定 actor

**负面 / 待跟进**：

- JSON 字段查询「user X 在哪些 channel」需扫表（SQLite JSON 函数支持但慢）；v3+ 多用户场景大时迁 table
- 同事务 SQLite 更新 JSON 字段需读 → 改 → 写；高并发场景有 race；v2 单用户低频可接受
- 「严格 join 规则」意味着系统流程必须保证 participant add 在 message post 之前 —— 一些路径（如 inbound message from 未知 actor）要先 auto-add，否则 message 写入失败；v2 vendor 撤回后入口仅 CLI / Web Console，控制点明确

## Alternatives Considered

### A. 独立 participant 表（v2 起步就 table）

- ✅ 索引 / 查询性能好；scale 友好
- ❌ v2 单用户场景规模小；JSON 字段足够
- ❌ 增加 BC schema 复杂度（多一张表 + 一个 sub-Entity）
- 否决（v2 节制 + 必要时 v3 迁表）

### B. 元素只存 identity_id 数组（不含 role / joined_at）

- ✅ 最简
- ❌ 加 join 审计 / role 等就要 schema 升级（破坏现有 array）
- 否决（用户明确「对象数组」）

### C. 严格 join 改为 lazy auto-add（first send 自动加）

- ✅ UX 友好（用户发消息自动入群）
- ❌ channel 是有 owner 控制权的实体；任意 identity 发 message 自动入有点失控
- ❌ task / issue 等系统流程已经 explicit add，UX 上不少 invite 一步
- 否决（严格 invite）

### D. 不支持 leave（用户 archive 整 channel 反而离开）

- ❌ v2 多 agent 场景常见「移除单个 agent 而不删 channel」需求
- 否决

## References

### v2 ADRs

- [ADR-0024 AgentInstance 一等公民化](0024-agent-instance-first-class.md) - participants 引用 agent identity = agent_instance.id
- [ADR-0029 Supervisor as Built-in AgentInstance](0029-supervisor-as-builtin-agent-instance.md) - supervisor identity 也走 agent: 前缀
- [ADR-0031 v2 Drop Bridge](0031-v2-drop-bridge-vendor-integration.md) - vendor 撤回，user identity 仅 v2 单用户
- [ADR-0032 CV1 Channel 一等公民](0032-conversation-channel-as-first-class.md) - channel.created_by = implicit owner
- [ADR-0033 Identity 模型重构](0033-identity-model-refactor.md) - participants.identity_id 引用此 schema

### 跨 BC

- [conversation/01-conversation.md](../../architecture/tactical/conversation/01-conversation.md) - schema 加 participants 字段
- [conversation/00-overview.md](../../architecture/tactical/conversation/00-overview.md) - UL + VO 更新

### 来源

- 2026-05-23 #agent-center 讨论
