# 0035. 跨 Conversation Message Carry-over（v2 CV3）

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-05-23 |
| Related | v2 议题 CV3；建立于 [ADR-0032 CV1 Channel](0032-conversation-channel-as-first-class.md)（parent_conversation_id 通用字段）+ [ADR-0034 CV2b Participants](0034-conversation-participants-field.md) 之上；为 [CV4](/) (Issue/Task 从 messages 派生入口) 提供 carry-over 模型基础 |

## Context

用户故事（[2026-05-22 #agent-center](../../drafts/v2-kickoff-2026-05-22.md)）：

> 「开子话题时**可以选择主话题群里的部分聊天记录作为子话题的历史记录**」

子 Conversation（kind=issue 等）创建时含父 Conversation（kind=channel 等）的部分 message 作为初始上下文。

### 已有铺垫

- [ADR-0032 CV1](0032-conversation-channel-as-first-class.md) 引入 universal `parent_conversation_id` 字段（业务层级父子链）
- [ADR-0034 CV2b](0034-conversation-participants-field.md) 引入 participants 字段
- Conversation Message 是 **append-only**（[ADR-0014 事件溯源](../0014-event-sourcing-level.md) + conversation/01 invariants），父 message 永不删

### 三种数据形态对比

| 形态 | 描述 | 评估 |
|---|---|---|
| **(a) Copy** | 子 Conv 起始 copy 父 message 全文 | ❌ 数据冗余；父修改不同步；msg_id 重复 |
| **(b) Reference** | 子 Conv 存父 message_id 列表 | ✅ 无冗余；append-only 配；UI 渲染合并 |
| **(c) Hybrid** | metadata copy + content via 共享 BlobRef | ⚠️ 工程复杂 |

→ 选 **(b) Reference**：跟 message append-only + ADR-0022 「Conversation 时间线主角」 精神一致。

## Decision

### 1. 引入 `conversation_message_reference` 独立表

```
conversation_message_reference (
  id                ULID
  child_conv_id     FK → conversations (强引用，不可变)
  source_msg_id     FK → messages (强引用，不可变；指向某 conversation 内的 message)
  source_conv_id    FK → conversations (冗余，从 source_msg 推；方便 join 查询)
  order_in_child    INT  -- 子 conv 渲染时排序（一般按 source posted_at 升序）
  created_at        ISO8601 TEXT
  created_by        TEXT -- 谁建的引用（一般 = child conv creator）
)

UNIQUE INDEX cmr_child_source_uq (child_conv_id, source_msg_id)
INDEX cmr_child_order_idx (child_conv_id, order_in_child)
INDEX cmr_source_msg_idx (source_msg_id)   -- 反查：某 message 被哪些 child conv 引用
```

为什么用独立表（而非 Conversation JSON 字段）：

- carry-over 涉及 **跨 conversation 引用**；独立表便于 join 查询（如「这条父 message 被哪些 child conv 引用」）
- N 大时性能比 JSON 字段好（JSON 字段 SQLite 函数查慢）
- 跟 Participants（CV2b）用 JSON 字段不同 —— Participants 是 Conversation 自身属性，元素少且查询集中在「这个 conv 的 participants」；carry-over 是跨 conv 关系，独立表更对位

### 2. 创建时机：同事务

child Conversation 创建时（如 issue spawn from channel messages）**同事务** INSERT N 条 `conversation_message_reference` rows：

```
tx:
  INSERT conversation (..., kind='issue', parent_conversation_id=<channel_id>, ...)
  INSERT issue (..., conversation_id=<new>)
  INSERT conversation_message_reference rows (child_conv_id=<new>, source_msg_id=<m1/m2/.../mN>, order_in_child=...)
  emit conversation.opened (kind=issue, with_carry_over=true)
```

### 3. 跨 kind 任意允许

| 父 kind → 子 kind | 允许 | 典型场景 |
|---|---|---|
| `channel` → `issue` | ✅ | 用户故事 S3 主用例 |
| `issue` → `task` | ✅ | Issue conclude 时携 issue 议事 message 子集到 task 起始 |
| `channel` → `task` | ✅ | 罕见但允许 |
| 任何 → 任何 | ✅ | 通用机制，应用层不约束 |

→ App 层不限制 kind 组合；用户 / supervisor 决定。

### 4. order_in_child 排序约定

- 默认 = source message 的 `posted_at` 升序（保持原对话时序）
- CV4 派生入口 CLI 可指定自定义顺序（待 CV4 议题）
- v2 不支持手动 reorder（建立后不变）

### 5. Display semantics（UI 渲染原则）

> 细节由 W1（Web Console UI）议题落地；本 ADR 只声明 UI 原则。

UI 渲染子 conv 时**分段展示**：

```
┌──────────────────────────────────────┐
│ 📥 From parent conversation:         │
│   <carry-over messages 按 order_in_child 列出>
│                                       │
│ ──────────────────────────────────── │
│ 💬 Discussion:                       │
│   <子 conv 自身 messages 按 posted_at 列出>
└──────────────────────────────────────┘
```

→ 视觉上明确「初始上下文 vs 子话题新生 message」；避免父 message 跟子 message 混淆出处。

CLI `agent-center conversation show <id>` 同样按此约定输出。

### 6. Permission（v2 trust user）

v2 单用户场景无隐私问题；用户可自由 carry-over 任何能看到的 message。

v3+ 多用户场景再加权限层（绑 Identity）：

- 校验 child conv participants 是否能「看到」carry-over messages 的 sender / 内容
- 可能策略：(b) 强制 child participants ⊇ parent sender / (c) 标识可见性

本 ADR 只声明 v2 不做此校验。

### 7. Append-only 保证 → 无 dangling

Conversation Message append-only：

- 父 message 永不删除
- 子 conv archived 时引用保留（仍可查），只是 child conv UI 不主动显示
- 父 conv archived 时引用仍合法（archived conv message 仍存在，只是 read-only）

→ `source_msg_id` 引用永远 valid；无需处理 dangling case。

### 8. CLI

CV3 范围内只有**只读 CLI**（写路径由 CV4 「派生入口」议题定）：

| 命令 | 用途 |
|---|---|
| `agent-center conversation show <id>` | 显示 conversation 详情；含 carry-over 分段（如有） |
| `agent-center conversation refs <id>` | 列子 conv 的 carry-over references（child→parent message 列表） |
| `agent-center message refs <msg-id>` | 反查：某 message 被哪些 child conv 引用 |

### 9. 跨 BC

| 关系 | 描述 |
|---|---|
| Conversation 内部 | 独立表 `conversation_message_reference` 是 Conversation BC 内的；跟 Message + Conversation 同 BC |
| Discussion / TaskRuntime | Issue / Task spawn 时通过 CV4 入口建立 carry-over；本 ADR 不直接耦合这些 BC |
| Bridge | v2 撤回；v3+ vendor 重新接入时考虑「carry-over 怎么 outbound 到 vendor」（属未来议题） |

### 10. Repository 接口

```go
type ConversationMessageReferenceRepository interface {
    Save(ctx context.Context, refs []*ConversationMessageReference) error  // 一般 batch insert
    FindByChildConvID(ctx context.Context, childConvID ConversationID) ([]*ConversationMessageReference, error)
    FindBySourceMsgID(ctx context.Context, sourceMsgID MessageID) ([]*ConversationMessageReference, error)
    DeleteByChildConvID(ctx context.Context, childConvID ConversationID) error  // child conv hard-delete 时配套（v2 暂无 hard delete）
}

var (
    ErrCarryOverDuplicate = errors.New("conversation: carry-over reference already exists for this (child, source_msg)")
)
```

### 11. Invariants

1. **同 (child_conv_id, source_msg_id) UNIQUE**：不允许重复引用
2. **source_msg_id 必须 valid Message**：FK 强引用
3. **child_conv_id 必须 valid Conversation**：FK 强引用
4. **carry-over reference 创建必须跟 child Conversation 同事务**：避免 child conv 创建后才补充 refs 的零碎事务

## Consequences

**正面**：

- 用户故事「开子话题携带父群部分聊天作上下文」直接表达
- 无内容冗余；append-only 保证引用永远有效
- 独立表 join 查询好；为反查 / 多 child 引用 / etc. 留余地
- CV4「派生入口」自然在本 ADR 基础上加 CLI / UI 写路径
- 跟 ADR-0022 「Conversation 时间线主角」精神一致 —— 子 conv 是新时间线，carry-over 是初始上下文标识

**负面 / 待跟进**：

- 渲染子 conv 时需 join 父 conv messages；UI / CLI 层多一步
- 跨 conv reference 让 conversation 不再「独立」—— 删 parent conv 影响 child 渲染（v2 无 hard delete，archive 不删 message，OK）
- 反向引用计数（某 message 被多少 child 引用）需 GROUP BY；查询性能 v2 OK，v3+ 大数据量可建汇总
- carry-over reference 一旦建立 v2 不支持 reorder / 删除单个 ref（用户体验局限；如错选 message 需重建整个 child conv）

## Alternatives Considered

### A. Copy 模式（不引用，复制 message 内容）

- ✅ 子 conv 独立可读；删父 conv 不影响
- ❌ 数据冗余；父 message 修改不同步
- ❌ message_id 重复 / 跨 conv 同 content 混淆
- 否决（user 倾向 reference）

### B. JSON 字段 `Conversation.carry_over_message_ids`

- ✅ 简单
- ❌ 反向查询「某 message 被哪些 child 引用」需扫所有 conv 的 JSON；性能差
- ❌ 增删单条 ref 需 read-modify-write JSON；race risk
- 否决（独立表更对位）

### C. 用 placeholder Message rows 模拟 carry-over

- 子 conv 创建时 INSERT N 条 placeholder messages，content = "→ msg_id_in_parent"
- ✅ 跟现有 Message timeline 融合
- ❌ Message 表里多种 message kind 混杂（hack）
- ❌ 编辑 / 删除 placeholder vs 实 message 混乱
- 否决

### D. Hybrid metadata copy + BlobRef

- 子 conv 存 metadata + content blob ref
- ✅ 内容查询独立；但仍引用父
- ❌ 工程复杂；v2 单租户低规模不需要
- 否决（v3+ 必要时再演化）

## References

### v2 ADRs

- [ADR-0014 事件溯源走 L1](../0014-event-sourcing-level.md) - append-only 基础
- [ADR-0017 Task as Conversation](../0017-task-as-conversation.md) - 待 rewrite
- [ADR-0021 Issue as Conversation](../0021-issue-as-conversation.md) - 待 rewrite
- [ADR-0022 Conversation 不对齐 IM 层级](../0022-conversation-not-aligned-with-im-hierarchy.md) - 时间线主角精神延伸
- [ADR-0031 v2 Drop Bridge](0031-v2-drop-bridge-vendor-integration.md) - vendor 撤回
- [ADR-0032 CV1 Channel](0032-conversation-channel-as-first-class.md) - parent_conversation_id 字段铺基础
- [ADR-0033 Identity refactor](0033-identity-model-refactor.md)
- [ADR-0034 Participants 字段](0034-conversation-participants-field.md)

### CV4 待开

- CV4「Issue / Task 从 conversation messages 派生入口」 = 本 ADR 模型的写路径（CLI / UI workflow）

### 跨 BC

- [conversation/01-conversation.md](../../architecture/tactical/conversation/01-conversation.md) - 加 conversation_message_reference 表说明
- [conversation/00-overview.md](../../architecture/tactical/conversation/00-overview.md) - UL / Repository 更新

### 来源

- 2026-05-22 → 2026-05-23 #agent-center 讨论
