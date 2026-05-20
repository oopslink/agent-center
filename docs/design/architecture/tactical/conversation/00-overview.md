# Conversation BC — DDD 战术设计 Overview

> **DDD 战术层** · BC: Conversation
>
> 系统内部"消息时间线"存储 + Identity 统一身份 + ChannelBinding vendor 关联。**跟具体接入渠道（飞书 / DingTalk / Web chat / ...）解耦**：Conversation BC 不调用任何 vendor SDK，外部渠道集成由 Bridge BC 通过事件驱动的双向同步实现。
>
> 类比一个会议：有人在会议室、有人在飞书、有人在电话上 —— 都是同一个会议的上下文。但 Conversation 模块**不知道也不关心**这些接入方式；它只是消息的内部存储。

> 命名 / 定位决策见 [ADR-0007](../../../decisions/0007-conversation-as-unified-session.md)（Refined by 0009 → 0021）+ [ADR-0017](../../../decisions/0017-task-as-conversation.md)（Task ↔ Conversation 1:1）+ [ADR-0021](../../../decisions/0021-issue-as-conversation.md)（Issue ↔ Conversation 1:1）。

---

## § 0. BC 一览

### 0.1 职责

| 维度 | 内容 |
|---|---|
| **聚合管理** | Conversation（AR + Message 子从属）/ Identity（AR + ChannelBinding 子 VO）|
| **会话承载** | 6 种 kind：dm / group_thread / adhoc / notification / task / issue；统一时间线存 Message |
| **Message content_kind** | 6 种：text / system / agent_finding / supervisor_summary / conclusion_draft / task_proposal |
| **vendor 解耦** | 不调任何 vendor SDK；Bridge 订阅 conversation.* 事件做双向同步 |
| **Identity 统一** | user / supervisor / agent / bot 4 kind；跨渠道不变的身份；ChannelBinding 关联到 vendor user id |

### 0.2 UL 切片

来自 [strategic/03-bounded-contexts § 1](../../strategic/03-bounded-contexts.md) 标 Conversation 上下文的术语：

- `Conversation`（聚合根）+ `Message`（实体，从属）
- `Identity`（聚合根，独立）+ `ChannelBinding`（VO，从属 Identity）
- 行为动词：`Add-message` / `Open` / `Close`（Conversation） / `Register` / `Bind` / `Unbind`（Identity / ChannelBinding）

### 0.3 Context Map 位置

[strategic/03-bounded-contexts § 3](../../strategic/03-bounded-contexts.md)：

- **Discussion ↔ Conversation**：**Shared Kernel / 1:1**（`issue.conversation_id` 强引用 `kind=issue` Conversation；[ADR-0021](../../../decisions/0021-issue-as-conversation.md)）
- **TaskRuntime ↔ Conversation**：**Shared Kernel / 1:1**（`task.conversation_id` 强引用 `kind=task` Conversation；[ADR-0017](../../../decisions/0017-task-as-conversation.md)）
- **Cognition → Conversation**：Customer-Supplier（supervisor 调 `conversation add-message`；worker daemon 通过 RPC 调同一 API）
- **Bridge → Conversation**：Customer-Supplier（inbound 时 Bridge 调 `conversation add-message`）
- **Bridge ← Conversation**：Pub/Sub（订阅 `conversation.message_added` / `conversation.opened`(kind=task/issue) 等做 outbound）
- **Observability ← Conversation**：Open Host（订阅 `conversation.*` / `identity.*` 事件做投影）

---

## § 1. 聚合清单（X.1）

### 1.1 Aggregate Roots

| 聚合 | 文件 | 状态机 | 身份 / 不变性 |
|---|---|---|---|
| **Conversation** | [01-conversation.md](01-conversation.md) | 2 态（open / closed） | ULID/UUID；身份不变；kind 不可变 |
| **Identity** | [02-identity.md](02-identity.md) | 无状态机（CRUD 风格） | 形式化字符串如 `user:hayang` / `supervisor:<inv-id>`；身份不变 |

### 1.2 Entity（子从属）

| 实体 | 从属 | 位置 |
|---|---|---|
| **Message** | Conversation（独立表 `messages`，归属 conversation） | [01-conversation.md § 3 Message](01-conversation.md) |

### 1.3 Value Objects（按使用聚合分组）

| VO | 用在哪 | 描述 |
|---|---|---|
| **ChannelBinding** | Identity 子从属 | `{identity_id, channel, vendor_user_id, preferred, bound_at}`；Identity ↔ vendor user 的绑定（同名于 Discussion BC 之前 ADR-0020 引入的 ChannelBinding，**ADR-0021 后 Discussion BC 已不持 ChannelBinding**，本 BC 是唯一持有方）|
| **ConversationKind** | conversation.kind 字段 | 6 种枚举：dm / group_thread / adhoc / notification / task / issue |
| **MessageContentKind** | message.content_kind 字段 | 6 种枚举：text / system / agent_finding / supervisor_summary / conclusion_draft / task_proposal |
| **MessageDirection** | message.direction 字段 | inbound / outbound / internal |
| **InputRequestRef** | message.input_request_ref 字段 | 跨 BC 弱引用到 TaskRuntime InputRequest |
| **IdentityRef** | message.sender_identity_id / 各处 actor | `user:<id>` / `supervisor:<inv-id>` / `agent:<session-id>` / `bot` 形式化字符串 |

---

## § 2. Invariants 索引（X.2）

每个聚合自己维护 invariants 节，本 § 仅做索引：

- **Conversation Invariants** → [01-conversation.md § 6](01-conversation.md)
- **Message Invariants** → [01-conversation.md § 6](01-conversation.md)（Message 是 Conversation 子从属）
- **Identity Invariants** → [02-identity.md § 4](02-identity.md)
- **ChannelBinding Invariants** → [02-identity.md § 4](02-identity.md)

**跨聚合的不变量**：

1. **Conversation BC 不调任何 vendor SDK**（[conventions § 9.y](../../../../rules/conventions.md)）
2. **Message.sender_identity_id 必须有对应 Identity**（应用层校验）
3. **vendor_msg_ref 唯一**（per channel 内 dedupe；不允许 inbound 重复写入）

---

## § 3. Domain Services（X.3）

### 3.1 ConversationLifecycleService

**职责**：Conversation 创建 / 关闭 / add-message。

| 维度 | 内容 |
|---|---|
| 入参 | `OpenConversationCommand` / `AddMessageCommand` / `CloseConversationCommand` |
| 出参 | Conversation 状态迁移 / Message 入库 + emit conversation.* 事件 |
| 跨聚合（task/issue kind）| 同步建路径：task.create / issue.open 跨 BC 调用本服务建 conversation + 写 Message |
| 写入 actor | user / supervisor / worker daemon / agent 都可写（通过 CLI 或 RPC）|

### 3.2 IdentityRegistrationService

**职责**：v1 自动注册 + 自动绑定 ChannelBinding。

| 维度 | 内容 |
|---|---|
| 触发 | Bridge inbound 时见到新 vendor user id → 自动绑定到 user identity（v1 单用户简化）|
| 出参 | Identity（如不存在则建）+ ChannelBinding + emit identity.* 事件 |
| v1 简化 | 任何 vendor 非 bot 来源 → 默认归属当前 user identity；不二次确认（多 user 推到 v2+）|

### 3.3 BridgeInboundRouter（caller 视角，主体在 Bridge BC）

**职责**：Bridge 收 vendor 事件后按 (channel, vendor_thread_key) 查 conversation + 路由到 add-message。

主体在 Bridge BC（[bridge/01-feishu-integration § 4](../bridge/01-feishu-integration.md)），Conversation 是被调方。详见 § 7.2。

---

## § 4. Factories（X.4）

### 4.1 ConversationFactory

**多个 caller**（按 kind 分）：

| Caller | Kind | 同步 / 懒创建 |
|---|---|---|
| Bridge inbound（DM / group thread） | `dm` / `group_thread` / `adhoc` | inbound 时自动建 |
| TaskRuntime（task 创建同步建路径） | `task` | 同步建（a/e 路径，[ADR-0017](../../../decisions/0017-task-as-conversation.md)）|
| TaskRuntime（task 创建懒创建路径） | `task` | 后续 `task bind-conversation` 触发 |
| Discussion（issue 创建同步建路径） | `issue` | 同步建（[ADR-0021](../../../decisions/0021-issue-as-conversation.md)）|
| Discussion（issue 创建懒创建路径） | `issue` | 后续 `issue bind-conversation` 触发 |
| Cognition（supervisor 主动 push） | `dm` / `adhoc` / `notification` | 按需 |

### 4.2 MessageFactory

**职责**：往 Conversation 写一条 Message（facade caller 包括 issue comment / 各 BC 通用 conversation add-message）。

入参：`AddMessageCommand{ conversation_id, sender_identity_id, content_kind, content, direction, vendor_msg_ref?, input_request_ref? }`。

### 4.3 IdentityFactory

**Caller**：IdentityRegistrationService（自动注册路径）+ CLI `agent-center identity add`（手动初始化路径）。

详见 [02-identity.md § 2 创建路径](02-identity.md)。

---

## § 5. Repositories（X.5）

接口签名（Go-style，含 `ctx context.Context` 参数；架构层契约，跟实现解耦）：

### 5.1 ConversationRepository

```go
type ConversationFilter struct {
    Kind     *ConversationKind   // nil = 所有 kind
    Status   *ConversationStatus // nil = 所有 status
    Cursor   *ConversationID
    Limit    int
}

type ConversationRepository interface {
    FindByID(ctx context.Context, id ConversationID) (*Conversation, error)
    Find(ctx context.Context, filter ConversationFilter) ([]*Conversation, error)                // 按 kind / status 灵活查（替代原 FindByKind 强制 status）
    FindByChannelAndThreadKey(ctx context.Context, channel string, threadKey string) (*Conversation, error)  // Bridge inbound 反查（高频热路径，独立方法）
    Save(ctx context.Context, c *Conversation) error
    UpdateStatus(ctx context.Context, id ConversationID, from, to ConversationStatus) error
    UpdatePrimaryChannel(ctx context.Context, id ConversationID, channel, threadKey string) error           // Bridge 回写 root card thread_key
}

// Domain errors
var (
    ErrConversationNotFound          = errors.New("conversation: conversation not found")
    ErrConversationAlreadyExists     = errors.New("conversation: (channel, thread_key) already maps to existing conversation")
    ErrConversationClosed            = errors.New("conversation: conversation is closed, cannot accept new message")
    ErrConversationInvalidKind       = errors.New("conversation: invalid kind for operation")
)
```

### 5.2 MessageRepository（sub-repo of Conversation）

```go
type MessageRepository interface {
    FindByConversationID(ctx context.Context, conversationID ConversationID, filter MessageFilter) ([]*Message, error)
    FindByVendorMsgRef(ctx context.Context, channel string, vendorMsgRef string) (*Message, error)        // inbound dedupe
    FindRecent(ctx context.Context, conversationID ConversationID, n int) ([]*Message, error)             // supervisor read context
    Append(ctx context.Context, m *Message) error                                                          // append-only；INSERT 后不修改
    UpdateVendorMsgRef(ctx context.Context, id MessageID, vendorMsgRef string) error                       // 唯一允许的 mutate：outbound 投递成功后回填 vendor_msg_ref（其它字段 append-only）
}

// Domain errors
var (
    ErrMessageNotFound       = errors.New("conversation: message not found")
    ErrMessageDuplicate      = errors.New("conversation: vendor_msg_ref duplicate (inbound dedupe)")
    ErrMessageImmutable      = errors.New("conversation: message is append-only, cannot modify (only vendor_msg_ref backfill allowed)")
    ErrMessageInvalidSender  = errors.New("conversation: message sender_identity_id does not exist")
)
```

### 5.3 IdentityRepository

```go
// IdentityID 是形式化字符串（'user:hayang' / 'supervisor:<inv-id>' / 'agent:<session-id>' / 'bot'），
// 用 typed alias 而非裸 string 提高类型安全。
type IdentityID string

type IdentityRepository interface {
    FindByID(ctx context.Context, id IdentityID) (*Identity, error)
    FindByKind(ctx context.Context, kind IdentityKind) ([]*Identity, error)
    Save(ctx context.Context, i *Identity) error
}

// Domain errors
var (
    ErrIdentityNotFound      = errors.New("conversation: identity not found")
    ErrIdentityAlreadyExists = errors.New("conversation: identity id already taken")
)
```

### 5.4 ChannelBindingRepository（sub-repo of Identity）

```go
type ChannelBindingRepository interface {
    FindByIdentityID(ctx context.Context, identityID IdentityID) ([]*ChannelBinding, error)
    FindByVendorUserID(ctx context.Context, channel string, vendorUserID string) (*ChannelBinding, error)  // Bridge inbound 反查
    Save(ctx context.Context, b *ChannelBinding) error
    Delete(ctx context.Context, identityID IdentityID, channel string) error
}

// Domain errors
var (
    ErrChannelBindingNotFound      = errors.New("conversation: channel binding not found")
    ErrChannelBindingAlreadyExists = errors.New("conversation: (channel, vendor_user_id) already bound to another identity")
)
```

### 5.5 约定

- 外部只通过 Root.id 引用各 AR（Conversation.id / Identity.id）（[conventions § 0.3](../../../../rules/conventions.md) AR 守门）
- Message 是 Conversation 子从属，通过 conversation_id 关联
- ChannelBinding 是 Identity 子从属（VO），通过 identity_id 关联；可 delete
- Repository 是**领域层抽象接口**；实现层落到 [implementation/02-persistence-schema.md](../../../implementation/)
- Domain errors 用 sentinel error pattern；调用方用 `errors.Is` 判定

**Message append-only 不变性**：

- **正常 IO 字段（id / conversation_id / sender_identity_id / content_kind / content / direction / input_request_ref / posted_at）一律 immutable**；INSERT 后不可修改
- **唯一例外**：`vendor_msg_ref` 字段允许 outbound 投递成功后回填一次（INSERT 时为 null，UpdateVendorMsgRef 设值；再次写入返回 `ErrMessageImmutable`）
- 应用层保证：(channel, vendor_msg_ref) 唯一性 + inbound dedupe（avoid Bridge 重写）
- 实现层可加 DB 触发器兜底防 UPDATE 其它列

---

## § 6. 跨聚合引用出方向（X.6）

| 引用方 → 被引方 | 强弱 | 一致性窗口 | 触发场景 |
|---|---|---|---|
| **Message → Conversation**（`message.conversation_id`） | 强 / 不可变 | tx 同步 | add-message |
| **Message → Identity**（`message.sender_identity_id`） | 强 / 不可变 | tx 同步 | add-message |
| **Message → InputRequest**（`message.input_request_ref`，跨 BC） | 弱 / nullable | tx 同步（InputRequest 创建时同事务写）| [ADR-0017 § 5](../../../decisions/0017-task-as-conversation.md) |
| **ChannelBinding → Identity**（`binding.identity_id`） | 强 / 不可变 | tx 同步 | Identity 创建后追加 binding |
| **Task → Conversation**（`task.conversation_id`，TaskRuntime BC） | 强 / 1:1 | tx 同步（同步建路径）| [ADR-0017](../../../decisions/0017-task-as-conversation.md) |
| **Issue → Conversation**（`issue.conversation_id`，Discussion BC） | 强 / 1:1 | tx 同步（同步建路径）| [ADR-0021](../../../decisions/0021-issue-as-conversation.md) |

**跨聚合一致性策略汇总**：

- **task / issue 同步建路径**：跨 BC tx 内建 Conversation + 写 sub-aggregate id 字段（[ADR-0014 § 2](../../../decisions/0014-event-sourcing-level.md)）
- **InputRequest 集成**：写 InputRequest 行 + 写一条 Message (`input_request_ref=<id>`) 同事务（[ADR-0017 § 5](../../../decisions/0017-task-as-conversation.md)）

---

## § 7. 跨 BC 交互

### 7.1 Bridge 协作模式

Conversation BC **不主动调 Bridge**。Bridge **订阅 Conversation 事件**做同步：

**Outbound（系统 → vendor）**：

```
Supervisor / Worker daemon / 其它 BC 调 agent-center conversation add-message --id=X --content=...
   ↓
Conversation BC 写入 Message (direction=outbound)
   ↓
emit conversation.message_added
   ↓
FeishuBridge 订阅 conversation.message_added:
   - 查 conversation.primary_channel_hint / thread_key → 路由
   - 按 message.content_kind + input_request_ref 渲染 (text / card / interactive card with buttons)
   - 调飞书 SDK 投递
   - 成功 → 回填 message.vendor_msg_ref；emit channel.delivered
   - 失败 → 重试 3 次；最终失败 → emit channel.delivery_failed
```

> **Worker daemon 是合法 actor**：worker daemon 通过 [TaskRuntime BC 长连 RPC](../task-runtime/00-overview.md) 调 `conversation add-message`，用于把 worker 进度 milestone / agent 请示写到 task.conversation_id（[ADR-0017 § 8](../../../decisions/0017-task-as-conversation.md)）。

### 7.2 Bridge Inbound 路由（被 Bridge 调）

```
FeishuBridge 收 vendor 事件 (im.message.receive_v1 等)
   ↓
路由到 Conversation:
   - 按 (channel, vendor_thread_key) 查 conversation
   - 找到 → 调 agent-center conversation add-message --id=<conv> 写入
     （此 lookup 同样命中 kind=task / kind=issue conversation —— 用户在 task / issue thread 内说话都由该路径写入；slash 命令留痕同样走此路径）
   - 找不到 → 按 event context 创建新 conversation (kind=dm / group_thread / adhoc)
     （注意：kind=task / kind=issue conversation 不由此分支创建 —— 走各自的同步建 / 懒创建路径，详见 [ADR-0017](../../../decisions/0017-task-as-conversation.md) + [ADR-0021](../../../decisions/0021-issue-as-conversation.md)）
   ↓
Conversation BC 写入 Message (direction=inbound, vendor_msg_ref=...)
emit conversation.message_added
   ↓
其它 BC 订阅事件做业务 (如 Supervisor 决定是否回复)
```

> [ADR-0021](../../../decisions/0021-issue-as-conversation.md) 后**取消**了原 [ADR-0009](../../../decisions/0009-issue-conversation-decoupled-via-bridge.md) 的"路由判断 1：是否绑到 issue bound_card → 写 IssueComment 不进 Conversation"例外路径。所有 inbound 议事消息统一走 Conversation Message 写入。

### 7.3 Bridge 渲染表

详见 [bridge/01-feishu-integration § 6](../bridge/01-feishu-integration.md)。Bridge 按 `content_kind + input_request_ref` 决定 vendor 形态（普通消息 / small card / rich card with buttons）。

### 7.4 Customer-Supplier 上下游汇总

| 方向 | 方式 | 例子 |
|---|---|---|
| **Conversation → ALL** | Pub/Sub | 所有 BC 可订阅 `conversation.message_added`，按需响应 |
| **Discussion → Conversation** | Shared Kernel / 1:1 | issue.conversation_id 强引用（[ADR-0021](../../../decisions/0021-issue-as-conversation.md)） |
| **TaskRuntime → Conversation** | Shared Kernel / 1:1 | task.conversation_id 强引用（[ADR-0017](../../../decisions/0017-task-as-conversation.md)） |
| **Cognition → Conversation** | Customer-Supplier | Supervisor 调 `conversation add-message` |
| **Bridge → Conversation** | Customer-Supplier | Bridge 收 vendor inbound 时调 `conversation add-message` |
| **Bridge ← Conversation** | Pub/Sub | Bridge 订阅 `conversation.message_added` → outbound |
| **Observability ← Conversation** | Open Host | 订阅 `conversation.*` / `identity.*` 事件 |

**关键约束**：Conversation BC **不直接调用** 任何 vendor SDK / API。所有外呼通过 Bridge 订阅事件后处理。

完整 context map 见 [strategic/03-bounded-contexts § 3](../../strategic/03-bounded-contexts.md)。

### 7.5 失败 / 错误处理（跟 Bridge 协作时）

**Inbound 失败**：

- Bridge 解析 vendor event 失败 → Bridge 自己写 `bridge.parse_failed` 事件 + 日志；不传到 Conversation BC
- Bridge 找不到 conversation 也找不到合适 kind 创建 → 异常事件 + 日志

**Outbound 失败**：

- Bridge 重试 3 次失败 → Message 标 `vendor_msg_ref=null + delivery_failed_at` + emit `channel.delivery_failed`
- Cognition BC 订阅 `channel.delivery_failed`：单条偶发失败 → 写 memory，跳过；同 conversation 连续失败 → Supervisor 唤醒决策
- v2+ 多 channel 场景：Bridge 自动 fallback 到 conversation 的另一条 ChannelBinding

**Conversation 状态不一致**：

- 出现 message 关联的 conversation_id 不存在（异常导入 / bug）→ 丢入 dead-letter 表 + 报警

---

## § 8. Out-of-Scope / Future Work

| 维度 | v1 简化 | 未来扩展 |
|---|---|---|
| 用户数 | 单 user identity | 多 user / 跨用户消息归属 |
| Channel 数 | 1 channel hint per conversation | 多 channel 同时送达 |
| Bridge 实现 | 仅 FeishuBridge | DingTalkBridge / WebBridge / SlackBridge / ... |
| Identity 自动绑定 | 任何 vendor 来源 → 默认 user identity | 多用户场景下 supervisor 询问归属 |
| Conversation kind | 6 种 | 按需新增 |
| Message content_kind | 6 种 | voice / image / file / is_pinned / parent_message_id / reactions 等普适扩展 |
| Task / Issue ↔ Conversation 多 tracker | 1:1 | 多渠道镜像 / 多用户 tracker（[roadmap](../../../roadmap.md)；[ADR-0017 § 9 + ADR-0021 § 8](../../../decisions/0017-task-as-conversation.md) 显式驳回 v1）|
| 投递失败处理 | 重试 + 失败标记 | 自动 fallback 到备 channel |
| Dedupe | 单 channel 内 vendor message_id 去重；outbound 可选 `dedupe_key` | 跨 channel dedupe |

---

## § 9. References

### 相关 ADR

- [ADR-0007 引入 Conversation 层](../../../decisions/0007-conversation-as-unified-session.md)（Refined by 0009 → 0021）
- [ADR-0009 Issue ↔ Conversation 解耦 + Bridge 模式](../../../decisions/0009-issue-conversation-decoupled-via-bridge.md)（Superseded by 0021）
- [ADR-0017 Task ↔ Conversation 1:1](../../../decisions/0017-task-as-conversation.md)（Refined by 0021）
- [ADR-0021 Issue ↔ Conversation 1:1，统一 Issue/Task 模式](../../../decisions/0021-issue-as-conversation.md)
- [ADR-0014 事件溯源走 L1](../../../decisions/0014-event-sourcing-level.md)（同事务双写原则）

### 战略层

- [strategic/03-bounded-contexts § 1 UL](../../strategic/03-bounded-contexts.md)（Conversation / Message / Identity / ChannelBinding 术语）
- [strategic/03-bounded-contexts § 2 BC6 Conversation](../../strategic/03-bounded-contexts.md)
- [strategic/03-bounded-contexts § 3 Context Map](../../strategic/03-bounded-contexts.md)

### 同 BC 内聚合详情

- [01-conversation.md](01-conversation.md) — Conversation AR + Message 子从属（kind / content_kind / 生命周期 / Invariants）
- [02-identity.md](02-identity.md) — Identity AR + ChannelBinding 子 VO（v1 单用户简化 / 自动绑定）

### 跨 BC 协作文档

- [discussion/00-overview.md](../discussion/00-overview.md) — Issue ↔ Conversation 1:1（kind=issue）
- [task-runtime/00-overview.md](../task-runtime/00-overview.md) — Task ↔ Conversation 1:1（kind=task）
- [task-runtime/03-input-request.md](../task-runtime/03-input-request.md) — InputRequest 集成 Message
- [cognition/00-overview.md](../cognition/00-overview.md) — Supervisor / worker daemon 调 `conversation add-message`
- [bridge/01-feishu-integration.md](../bridge/01-feishu-integration.md) — Bridge 渲染 / outbound / inbound 实现
- [observability/00-overview.md](../observability/00-overview.md) — `conversation.*` / `identity.*` 事件订阅

### 横切方法论

- [conventions § 0 DDD](../../../../rules/conventions.md) / § 9.y Bridge 模式 / § 11 渠道选对 / § 16 reason+message
