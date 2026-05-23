# 0032. Conversation Channel 业务一等公民 + Conversation schema reset（v2 CV1）

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-24 |
| Delivered | P10 § 3.0-3.3 — `cefc135` (Conversation v2 schema + AR/Repo + Identity refactor), `1e3918a` (ChannelManagementService CV1) |
| Related | v2 议题 CV1（[v2-kickoff-2026-05-22](../../drafts/v2-kickoff-2026-05-22.md) Conversation 模型组）；建立于 [ADR-0031 v2 Drop Bridge / Vendor Integration](0031-v2-drop-bridge-vendor-integration.md) 之上；为后续 CV2 (Participant) / CV3 (carry-over) / CV4 (派生入口) 议题打基础；触发 ADR-0017 / 0021 / 0022 在 CV1-CV4 闭环后 rewrite / supersede |

## Context

### Conversation 重新审视（撤回 vendor 后）

[ADR-0031](0031-v2-drop-bridge-vendor-integration.md) 撤回 Bridge / 飞书后，Conversation 业务模型可以纯业务重塑。

用户故事（[2026-05-22 #agent-center 讨论](../../drafts/v2-kickoff-2026-05-22.md)）：

> 多 human + agent 拉到话题群讨论 → 群里开子话题 → 子话题派多 Task 各自有 thread

「话题群」就是 **channel** 概念。当前 Conversation kind 枚举里 `group_thread` 名字含糊，且 v1 设计被 vendor channel 概念污染（[ADR-0022](../0022-conversation-not-aligned-with-im-hierarchy.md) 把 channel 当 routing hint）—— 撤回 vendor 后 channel 可以**升业务一等公民**。

### 现有 Conversation schema 的 vendor 污染

[conversation/01-conversation.md](../../architecture/tactical/conversation/01-conversation.md) 现有字段含：

| 字段 | 污染 |
|---|---|
| `primary_channel_hint` | vendor 路由 ID（飞书群 ID 等）|
| `primary_channel_thread_key` | vendor thread key |

按 [ADR-0031 § 6](0031-v2-drop-bridge-vendor-integration.md)，这些字段 v2 删除。CV1 ADR 顺手把这部分 reset 落地。

## Decision

### 1. kind 枚举重命名：`group_thread → channel`

| 旧 | 新 | 备注 |
|---|---|---|
| `group_thread` | `channel` | 撤回 vendor 后 channel 是业务一等公民 |

其他 kind 不变：`dm` / `adhoc` / `notification` / `task` / `issue` / **`channel`**（新）。

### 2. Conversation schema reset（v2 形态）

```
conversation (
  id                       ULID
  kind                     enum    -- dm | channel | adhoc | notification | task | issue
  name                     str?    -- universal nullable；channel 创建时 app 层强制；其他 kind 可填可空
  description              str?    -- universal nullable
  status                   enum    -- active | closed | archived
  parent_conversation_id   ULID?   -- universal nullable；通用父子链（CV1 + 为 CV3 carry-over / CV4 派生入口铺基础）
  created_by               str     -- universal NEW；任何 Conversation 都有创建者 actor
  created_at               timestamp
  updated_at               timestamp
  archived_at              timestamp?  -- archive 时填
  archived_by              str?
  message_count            int
  last_message_at          timestamp?
  version                  int     -- 乐观锁
)

-- 名字唯一性：仅 channel 强制全局唯一；其他 kind 任意（task/issue 可重名）
UNIQUE INDEX conversation_channel_name_uq (name) WHERE kind = 'channel' AND name IS NOT NULL

-- 父子链索引（CV3 carry-over 时需要按 parent 查 children）
INDEX conversation_parent_idx (parent_conversation_id) WHERE parent_conversation_id IS NOT NULL
```

**删除字段**（vendor 撤回，per [ADR-0031](0031-v2-drop-bridge-vendor-integration.md)）：

- `primary_channel_hint`
- `primary_channel_thread_key`

### 3. Channel 业务一等公民

| 维度 | 内容 |
|---|---|
| **必需字段**（app 层校验） | `name` 必填 + 全局唯一 |
| **可选字段** | `description` |
| **创建** | CLI `agent-center channel create --name=<n> [--description=<d>]` + Web Console（W1 议题） |
| **创建者** | 自动设 `created_by`；creator 自动成为首个 participant（CV2 议题定细节） |
| **参与者** | channel 必有 participant 列表（CV2 议题定模型） |
| **生命周期** | `active` → `archived`（archive 转 read-only） |
| **父子关系** | channel 一般无 parent；子话题（kind=issue）的 parent_conversation_id 指向 channel |

### 4. Universal 字段语义

| 字段 | 各 kind 行为 |
|---|---|
| `name` | channel 必填；其他 kind 可填（如 user 想给 DM 起名 / 给 task conversation 加业务名）|
| `description` | 各 kind 都可选 |
| `created_by` | universal；任何 Conversation 创建时填；新 schema 比 v1 严格（v1 没显式 created_by）|
| `archived_at` / `archived_by` | universal；archive 时填；status 转 `archived` 同事务做 |
| `parent_conversation_id` | universal；channel/issue/task 等都能有 parent；为 CV3 跨 Conv message carry-over 铺基础（carry-over 隐含父子链）|

### 5. Status 状态机扩展

```
[*] --> active
active --> closed     (task done / issue concluded 等触发；现有语义)
active --> archived   (用户显式 archive；read-only)
closed --> archived   (用户后续 archive 已 closed 的 Conversation)
archived --> [terminal]
```

`archived` 是 terminal；之后 **不可再发 message**（write API 拒绝）。

### 6. CLI

| 命令 | 用途 |
|---|---|
| `agent-center channel create --name=<n> [--description=<d>]` | 创建 kind=channel 的 Conversation |
| `agent-center channel list [--status=<s>]` | 列 channels |
| `agent-center channel show <name>` | 详情（含 messages count / participants / parent 等）|
| `agent-center channel archive <name>` | archive（status → archived，read-only）|
| `agent-center conversation show <id>` | 通用：查任何 kind 的 Conversation 详情 |

### 7. 触发后续 ADR rewrite

跟 [ADR-0031 Step 3](0031-v2-drop-bridge-vendor-integration.md) 协议一致：

- **ADR-0017 (Task as Conversation)** / **ADR-0021 (Issue as Conversation)** / **ADR-0022 (Conversation 不对齐 IM 层级)** 在 CV1-CV4 全部闭环后统一 rewrite / supersede
- 本 ADR-0032 仅 record CV1 决定 + 触发 Conversation BC docs 同步更新

## Consequences

**正面**：

- Channel 作为业务一等公民，跟 v2 用户故事「话题群」概念对齐
- Conversation schema 撤回 vendor 污染；纯业务模型
- name / description universal 字段简化模型（不引入 channel-specific 字段语法）
- `parent_conversation_id` 通用父子链为 CV3 carry-over / CV4 派生入口铺基础
- `created_by` 显式化让审计 / 权限更严谨（v1 隐式从第一 message sender 推断）

**负面 / 待跟进**：

- Conversation schema 改动大：删 2 字段 + 加 5 字段；v2 不考虑向后兼容，直接重建表
- 现有 ADR-0017 / 0021 / 0022 等待 CV1-CV4 全闭环后 rewrite —— **临时状态下 3 个 ADR 含过时 vendor 描述**；通过 ADR-0031 / 0032 引用让读者知道在演化中
- Channel name 全局唯一可能在多用户场景受限（v2 单租户 OK；未来多租户改 scoped unique）
- Participant 模型 CV2 没定前，「channel 必有 participant」语义留口（暂用 default = creator only）

## Alternatives Considered

### A. 保留 `group_thread` 名字

- ✅ 不动 vocabulary
- ❌ vendor 撤回后 channel 是业务一等公民；`group_thread` 名字含糊
- ❌ 用户明确要 channel 命名（2026-05-22 表态）
- 否决

### B. 加 `channel_name` / `channel_description` 专用字段

- 我早期提议；user 否决：「这些字段是不是 conversation 也应该有 不是 channel 独有的」
- ❌ 引入 channel-specific 字段语法（DB CHECK constraint）；不简洁
- 否决（用户 2026-05-23 表态）

### C. Channel 单独建 AR（不复用 Conversation）

- 把 Channel 升 AR；Channel 跟 Conversation 1:1 关联
- ❌ Conversation 已经能承载 channel 语义（加 kind 值即可）；新 AR 复杂度大
- ❌ Channel 跟其他 kind Conversation 的统一 API（list / show / archive）会分裂
- 否决

### D. Parent-child 关系不在 Conversation base，挂业务实体

- 比如 Issue.parent_channel_conversation_id 而非 Conversation.parent_conversation_id
- ❌ CV3 carry-over message 需要跨 Conversation 父子链；放业务实体不够通用
- ❌ 多类型父子（channel→issue / issue→task / channel→adhoc 等）要每对 业务实体都加字段
- 否决（universal 字段更简洁）

## References

### v2 ADRs 相关

- [ADR-0031 v2 Drop Bridge / Vendor Integration](0031-v2-drop-bridge-vendor-integration.md) - 撤回 vendor 上游决定
- [ADR-0017 Task as Conversation](../0017-task-as-conversation.md) - 待 CV1-CV4 闭环后 rewrite
- [ADR-0021 Issue as Conversation](../0021-issue-as-conversation.md) - 同
- [ADR-0022 Conversation 不对齐 IM 层级](../0022-conversation-not-aligned-with-im-hierarchy.md) - 同

### 后续 CV 议题

- CV2 Participant 显式建模（待开）
- CV3 跨 Conversation Message Carry-over（待开）
- CV4 Issue/Task 从 messages 派生入口（待开）

### 跨 BC

- [conversation/00-overview.md](../../architecture/tactical/conversation/00-overview.md) - BC 入口需同步更新
- [conversation/01-conversation.md](../../architecture/tactical/conversation/01-conversation.md) - schema 重写

### 来源

- 2026-05-22 → 2026-05-23 #agent-center 讨论
