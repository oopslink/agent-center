# Conversation BC — DDD 战术设计 Overview

> **DDD 战术层** · BC: Conversation
>
> 系统内部"消息时间线"存储 + Identity 统一身份。纯业务模型（v2 vendor 集成已撤回 per [ADR-0031](../../../decisions/drafts/0031-v2-drop-bridge-vendor-integration.md)）。承载所有领域 thread（Channel 话题群 / DM / Task / Issue / 通知 / adhoc）的消息时间线。
>
> 业务对象层级是 Conversation 父子链的 source of truth（Channel → Issue → Task）；详 [ADR-0039 Conversation 业务模型 v2 统一](../../../decisions/drafts/0039-conversation-business-model-v2-unified.md)。

> 命名 / 定位决策见 [ADR-0007](../../../decisions/0007-conversation-as-unified-session.md) + [ADR-0039 Conversation 业务模型 v2 统一](../../../decisions/drafts/0039-conversation-business-model-v2-unified.md)。Vendor 集成已撤回（per [ADR-0031](../../../decisions/drafts/0031-v2-drop-bridge-vendor-integration.md)）；Conversation 是纯业务时间线 + 关联实体（Channel / Issue / Task / DM / adhoc / notification）。

---

## § 0. BC 一览

### 0.1 职责

| 维度 | 内容 |
|---|---|
| **聚合管理** | Conversation（AR + Message 子从属）/ Identity（AR）|
| **会话承载** | 6 种 kind：`dm` / `channel`（v2 CV1 重命名自 group_thread）/ `adhoc` / `notification` / `task` / `issue`；统一时间线存 Message |
| **Message content_kind** | 6 种：text / system / agent_finding / supervisor_summary / conclusion_draft / task_proposal |
| **Identity 统一** | 3 kind: user / agent / system（per [ADR-0033](../../../decisions/drafts/0033-identity-model-refactor.md)）；ID 格式 `kind:id`；权限模型 v3+ 绑 Identity |

### 0.2 UL 切片

来自 [strategic/03-bounded-contexts § 1](../../strategic/03-bounded-contexts.md) 标 Conversation 上下文的术语：

- `Conversation`（聚合根）+ `Message`（实体，从属）
- `Identity`（聚合根，独立）
- 行为动词：`Add-message` / `Open` / `Close`（Conversation） / `Register`（Identity）

### 0.3 Context Map 位置

[strategic/03-bounded-contexts § 3](../../strategic/03-bounded-contexts.md)：

- **Discussion ↔ Conversation**：**Shared Kernel / 1:1**（`issue.conversation_id` 强引用 `kind=issue` Conversation；per [ADR-0039](../../../decisions/drafts/0039-conversation-business-model-v2-unified.md)）
- **TaskRuntime ↔ Conversation**：**Shared Kernel / 1:1**（`task.conversation_id` 强引用 `kind=task` Conversation；per [ADR-0039](../../../decisions/drafts/0039-conversation-business-model-v2-unified.md)）
- **Cognition → Conversation**：Customer-Supplier（supervisor 调 `conversation add-message`；worker daemon 通过 RPC 调同一 API；agent instance 通过 CLI 写）
- **Web Console / CLI → Conversation**：Customer-Supplier（用户入口，per [ADR-0037](../../../decisions/drafts/0037-web-console-as-main-user-ui.md) + [ADR-0038](../../../decisions/drafts/0038-cli-ux-enhancement.md)）
- **Observability ← Conversation**：Open Host（订阅 `conversation.*` / `identity.*` 事件做投影）

---

## § 1. 聚合清单（X.1）

### 1.1 Aggregate Roots

| 聚合 | 文件 | 状态机 | 身份 / 不变性 |
|---|---|---|---|
| **Conversation** | [01-conversation.md](01-conversation.md) | 2 态（open / closed） | ULID/UUID；身份不变；kind 不可变 |
| **Identity** | [02-identity.md](02-identity.md) | 无状态机（CRUD 风格） | 形式化字符串 `kind:id`（`user:hayang` / `agent:<instance-id>` / `system:<role>`）；身份不变 |

### 1.2 Entity（子从属）

| 实体 | 从属 | 位置 |
|---|---|---|
| **Message** | Conversation（独立表 `messages`，归属 conversation） | [01-conversation.md § 3 Message](01-conversation.md) |

### 1.3 Value Objects（按使用聚合分组）

| VO | 用在哪 | 描述 |
|---|---|---|
| **ConversationKind** | conversation.kind 字段 | 6 种枚举：`dm` / `channel` / `adhoc` / `notification` / `task` / `issue`（v2 CV1: `channel` 升业务一等公民；详 [ADR-0032](../../../decisions/drafts/0032-conversation-channel-as-first-class.md)）|
| **MessageContentKind** | message.content_kind 字段 | 6 种枚举：text / system / agent_finding / supervisor_summary / conclusion_draft / task_proposal |
| **MessageDirection** | message.direction 字段 | inbound / outbound / internal（v2 取消 vendor 路径后，inbound/outbound 仅用于区分用户↔系统 vs 系统内部）|
| **InputRequestRef** | message.input_request_ref 字段 | 跨 BC 弱引用到 TaskRuntime InputRequest |
| **IdentityRef** | message.sender_identity_id / 各处 actor | `kind:id` 形式化字符串（per [ADR-0033](../../../decisions/drafts/0033-identity-model-refactor.md)）|
| **Participants** | conversation.participants 字段 | JSON 数组，元素 = IdentityRef；详 [ADR-0034](../../../decisions/drafts/0034-conversation-participants-field.md) |
| **CarryOverRef** | message.carry_over_ref 字段 | 跨 conversation 弱引用，详 [ADR-0035](../../../decisions/drafts/0035-cross-conversation-message-carryover.md) |

---

## § 2. Invariants 索引（X.2）

每个聚合自己维护 invariants 节，本 § 仅做索引：

- **Conversation Invariants** → [01-conversation.md § 6](01-conversation.md)
- **Message Invariants** → [01-conversation.md § 6](01-conversation.md)（Message 是 Conversation 子从属）
- **Identity Invariants** → [02-identity.md § 4](02-identity.md)

**跨聚合的不变量**：

1. **Conversation BC 不调任何外部 IM / 渠道 SDK**（v2 vendor 撤回；[conventions § 9.y](../../../../rules/conventions.md)）
2. **Message.sender_identity_id 必须有对应 Identity**（应用层校验）
3. **Message 是 append-only**：所有字段 INSERT 后不可变；不存在"事后回填"路径（v2 vendor_msg_ref 字段已删）

---

## § 3. Domain Services（X.3）

### 3.1 MessageWriter（ConversationLifecycleService 实现）

**职责**：通用 Conversation 创建 / 关闭 / Archive / AddMessage（per [ADR-0014 § 2](../../../decisions/0014-event-sourcing-level.md) same-tx double-write）。

| 维度 | 内容 |
|---|---|
| 入参 | `OpenCommand` / `AddMessageCommand` / `CloseCommand` / `ArchiveCommand` |
| 出参 | Conversation 状态迁移 / Message 入库 + emit conversation.opened / message_added / closed / archived |
| 跨聚合（task/issue kind）| 同步建路径：TaskService / IssueLifecycleService 跨 BC 同事务调用 |
| 写入 actor | user / agent / system 都可写（Web Console / CLI / RPC） |

### 3.2 ChannelManagementService（CV1，[ADR-0032](../../../decisions/drafts/0032-conversation-channel-as-first-class.md)）

**职责**：kind=channel 业务一等公民管理（create / archive；name 全局唯一）。

| 维度 | 内容 |
|---|---|
| 入参 | `CreateChannelCommand{Name, Description, CreatedBy, Actor}` / `ArchiveChannelCommand{Name, ArchivedBy, Actor}` |
| 出参 | conversation.opened（带 name + created_by）/ conversation.archived |
| 不变性 | name 全局唯一（partial unique index on `name` WHERE `kind='channel'`）；creator 自动入 owner participant |
| CLI | `agent-center channel create / list / show / archive` |

### 3.3 ParticipantManagementService（CV2b，[ADR-0034](../../../decisions/drafts/0034-conversation-participants-field.md)）

**职责**：channel participants JSON r-m-w 加乐观锁；invite / leave / kick。

| 维度 | 内容 |
|---|---|
| 入参 | `InviteCommand` / `LeaveCommand` / `KickCommand` |
| 出参 | participants 列表 UPDATE（CAS via version）+ emit participant_joined / participant_left |
| 不变性 | already-active 拒；archived conv 拒；kick 要求 caller 是 owner role |
| CLI | `agent-center channel invite / leave / kick / participants` |

### 3.4 CarryOverService（CV3，[ADR-0035](../../../decisions/drafts/0035-cross-conversation-message-carryover.md)）

**职责**：跨 Conversation message reference 物化（child conv ← source messages）+ 双向反查。

| 维度 | 内容 |
|---|---|
| 入参 | `MaterialiseCommand{ChildConvID, SourceConvID, SourceMessageIDs, CreatedBy, Actor}` |
| 出参 | conversation_message_reference 批量 INSERT + emit message_references_added；append-only + unique (child, source_msg) |
| 反查 | `FindByChildConv` / `FindBySourceMsg` |
| CLI | `agent-center conversation refs` / `agent-center message refs` |

### 3.5 MessageDerivationService（CV4 派生入口，[ADR-0036](../../../decisions/drafts/0036-derive-issue-task-from-messages.md)）

**职责**：从源 Conversation 选 messages 派生 Issue / Task；编排 IssueOpener / TaskCreator 端口 + CarryOverService。

| 维度 | 内容 |
|---|---|
| 入参 | `DeriveIssueCommand` / `DeriveTaskCommand{SourceConvID, SourceMessageIDs, ProjectID, ...}` |
| 校验链 | source 存在 + active；channel kind 要求 caller active participant；source_messages 全属 source_conv |
| 出参 | new Issue/Task + 自带 conversation + carry-over refs |
| CLI | `agent-center issue open --from-conversation=<c> --select-messages=...` / `agent-center task create --from-conversation=...` |

### 3.6 IdentityRegistrationService（[ADR-0033](../../../decisions/drafts/0033-identity-model-refactor.md)）

**职责**：Identity CRUD + 跨聚合 invariant（Identity[kind=agent] ↔ AgentInstance 同 tx）。

| 维度 | 内容 |
|---|---|
| 触发 | CLI `agent-center identity add`（手动 user 初始化）/ AgentInstance create 时同事务 auto-register `agent:<instance_id>` |
| 出参 | Identity + emit identity.registered |
| Bootstrap | `EnsureSystemIdentity` 在 center 启动时幂等 provision `system` identity |
| v2 简化 | 单用户场景；3 kind 枚举：`user` / `agent` / `system` |

---

## § 4. Factories（X.4）

### 4.1 ConversationFactory

**多个 caller**（按 kind 分）：

| Caller | Kind | 同步 / 懒创建 |
|---|---|---|
| Web Console / CLI（用户开 channel / DM / adhoc） | `dm` / `channel` / `adhoc` | 用户操作时同步建 |
| TaskRuntime（task 创建同步建路径） | `task` | 同步建（per [ADR-0039](../../../decisions/drafts/0039-conversation-business-model-v2-unified.md)）|
| Discussion（issue 创建同步建路径） | `issue` | 同步建（per [ADR-0039](../../../decisions/drafts/0039-conversation-business-model-v2-unified.md)）|
| Cognition（supervisor 主动 push） | `dm` / `adhoc` / `notification` | 按需 |

### 4.2 MessageFactory

**职责**：往 Conversation 写一条 Message（facade caller 包括 issue comment / 各 BC 通用 conversation add-message）。

入参：`AddMessageCommand{ conversation_id, sender_identity_id, content_kind, content, direction, input_request_ref?, carry_over_ref? }`。

### 4.3 IdentityFactory

**Caller**：IdentityRegistrationService（CLI 手动 + AgentInstance 创建联动）。

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
    Find(ctx context.Context, filter ConversationFilter) ([]*Conversation, error)                // 按 kind / status 灵活查
    FindByParent(ctx context.Context, parentID ConversationID) ([]*Conversation, error)          // Channel → Issue/Task 子查询
    Save(ctx context.Context, c *Conversation) error
    UpdateStatus(ctx context.Context, id ConversationID, from, to ConversationStatus) error
}

// Domain errors
var (
    ErrConversationNotFound      = errors.New("conversation: conversation not found")
    ErrConversationAlreadyExists = errors.New("conversation: conversation id already taken")
    ErrConversationClosed        = errors.New("conversation: conversation is closed, cannot accept new message")
    ErrConversationInvalidKind   = errors.New("conversation: invalid kind for operation")
)
```

### 5.2 MessageRepository（sub-repo of Conversation）

```go
type MessageRepository interface {
    FindByConversationID(ctx context.Context, conversationID ConversationID, filter MessageFilter) ([]*Message, error)
    FindRecent(ctx context.Context, conversationID ConversationID, n int) ([]*Message, error)             // supervisor read context
    Append(ctx context.Context, m *Message) error                                                          // append-only；INSERT 后不修改
}

// Domain errors
var (
    ErrMessageNotFound       = errors.New("conversation: message not found")
    ErrMessageImmutable      = errors.New("conversation: message is append-only, cannot modify")
    ErrMessageInvalidSender  = errors.New("conversation: message sender_identity_id does not exist")
)
```

### 5.3 IdentityRepository

```go
// IdentityID 是形式化字符串 `kind:id`（'user:hayang' / 'agent:<instance-id>' / 'system:<role>'），
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

### 5.4 约定

- 外部只通过 Root.id 引用各 AR（Conversation.id / Identity.id）（[conventions § 0.3](../../../../rules/conventions.md) AR 守门）
- Message 是 Conversation 子从属，通过 conversation_id 关联
- Repository 是**领域层抽象接口**；实现层落到 [implementation/02-persistence-schema.md](../../../implementation/)
- Domain errors 用 sentinel error pattern；调用方用 `errors.Is` 判定

**Message append-only 不变性**：

- 所有字段（id / conversation_id / sender_identity_id / content_kind / content / direction / input_request_ref / carry_over_ref / posted_at）INSERT 后不可变
- 实现层可加 DB 触发器兜底防 UPDATE

---

## § 6. 跨聚合引用出方向（X.6）

| 引用方 → 被引方 | 强弱 | 一致性窗口 | 触发场景 |
|---|---|---|---|
| **Message → Conversation**（`message.conversation_id`） | 强 / 不可变 | tx 同步 | add-message |
| **Message → Identity**（`message.sender_identity_id`） | 强 / 不可变 | tx 同步 | add-message |
| **Message → InputRequest**（`message.input_request_ref`，跨 BC） | 弱 / nullable | tx 同步（InputRequest 创建时同事务写）| per [ADR-0039](../../../decisions/drafts/0039-conversation-business-model-v2-unified.md) |
| **Message → Message**（`message.carry_over_ref`，跨 conversation） | 弱 / nullable | tx 同步 | per [ADR-0035](../../../decisions/drafts/0035-cross-conversation-message-carryover.md) |
| **Conversation → Conversation**（`parent_conversation_id`） | 强 / nullable | tx 同步 | Channel → Issue / Task 父子链 |
| **Task → Conversation**（`task.conversation_id`，TaskRuntime BC） | 强 / 1:1 | tx 同步（同步建路径）| per [ADR-0039](../../../decisions/drafts/0039-conversation-business-model-v2-unified.md) |
| **Issue → Conversation**（`issue.conversation_id`，Discussion BC） | 强 / 1:1 | tx 同步（同步建路径）| per [ADR-0039](../../../decisions/drafts/0039-conversation-business-model-v2-unified.md) |

**跨聚合一致性策略汇总**：

- **task / issue 同步建路径**：跨 BC tx 内建 Conversation + 写 sub-aggregate id 字段（[ADR-0014 § 2](../../../decisions/0014-event-sourcing-level.md)）
- **InputRequest 集成**：写 InputRequest 行 + 写一条 Message (`input_request_ref=<id>`) 同事务

---

## § 7. 跨 BC 交互

### 7.1 写入路径

Conversation BC 是**纯被调方**。所有写入通过 `conversation add-message` API：

```
caller（user via Web/CLI / agent instance via CLI / supervisor via RPC / worker daemon via RPC）
   ↓
ConversationLifecycleService.AddMessage(cmd)
   ↓
Conversation BC 写入 Message + emit conversation.message_added
   ↓
订阅方（Observability / Cognition）按需响应
```

> **Worker daemon 是合法 actor**：worker daemon 通过 [TaskRuntime BC 长连 RPC](../task-runtime/00-overview.md) 调 `conversation add-message`，用于把 worker 进度 milestone / agent 请示写到 task.conversation_id。

### 7.2 Customer-Supplier 上下游汇总

| 方向 | 方式 | 例子 |
|---|---|---|
| **Conversation → ALL** | Pub/Sub | 所有 BC 可订阅 `conversation.message_added`，按需响应 |
| **Discussion → Conversation** | Shared Kernel / 1:1 | issue.conversation_id 强引用 |
| **TaskRuntime → Conversation** | Shared Kernel / 1:1 | task.conversation_id 强引用 |
| **Cognition → Conversation** | Customer-Supplier | Supervisor 调 `conversation add-message` |
| **Web Console / CLI → Conversation** | Customer-Supplier | 用户消息写入 |
| **Observability ← Conversation** | Open Host | 订阅 `conversation.*` / `identity.*` 事件 |

**关键约束**：Conversation BC **不直接调用** 任何外部 IM / 渠道 SDK / API。v2 vendor 集成全部撤回（per [ADR-0031](../../../decisions/drafts/0031-v2-drop-bridge-vendor-integration.md)）。

完整 context map 见 [strategic/03-bounded-contexts § 3](../../strategic/03-bounded-contexts.md)。

### 7.3 失败 / 错误处理

- 调用方 add-message 失败（conversation 不存在 / 已 closed / sender_identity_id 不存在）→ 返 domain error；调用方按需重试或转人工
- 一致性 broken（异常导入 / bug 导致 message 关联的 conversation_id 不存在）→ 丢入 dead-letter 表 + 报警

---

## § 8. Out-of-Scope / Future Work

| 维度 | v2 简化 | 未来扩展 |
|---|---|---|
| 用户数 | 单 user identity | 多 user / 跨用户消息归属（v3+）|
| 外部 IM / 渠道集成 | 不做（v2 已撤回）| v3+ 重新设计 Bridge / Channel 抽象 |
| Conversation kind | 6 种 | 按需新增 |
| Message content_kind | 6 种 | voice / image / file / is_pinned / parent_message_id / reactions 等普适扩展 |
| 多 tracker 镜像 | Task / Issue ↔ Conversation 1:1 | 多渠道镜像 / 多用户 tracker（[roadmap](../../../roadmap.md)）|
| 权限 | 单用户简化 | v3+ 权限模型绑 Identity |

---

## § 9. References

### 相关 ADR

- [ADR-0007 引入 Conversation 层](../../../decisions/0007-conversation-as-unified-session.md)（Refined by 0039）
- [ADR-0031 v2 撤回 Bridge / vendor 集成](../../../decisions/drafts/0031-v2-drop-bridge-vendor-integration.md)
- [ADR-0032 channel 升业务一等公民](../../../decisions/drafts/0032-conversation-channel-as-first-class.md)
- [ADR-0033 Identity 模型重构（3 kind: user / agent / system）](../../../decisions/drafts/0033-identity-model-refactor.md)
- [ADR-0034 Conversation participants 字段](../../../decisions/drafts/0034-conversation-participants-field.md)
- [ADR-0035 跨 conversation 消息 carry-over](../../../decisions/drafts/0035-cross-conversation-message-carryover.md)
- [ADR-0036 Issue / Task 从 Message 派生](../../../decisions/drafts/0036-derive-issue-task-from-messages.md)
- [ADR-0039 Conversation 业务模型 v2 统一](../../../decisions/drafts/0039-conversation-business-model-v2-unified.md)（supersedes ADR-0017 / 0021 / 0022，已删）
- [ADR-0014 事件溯源走 L1](../../../decisions/0014-event-sourcing-level.md)（同事务双写原则）

### 战略层

- [strategic/03-bounded-contexts § 1 UL](../../strategic/03-bounded-contexts.md)（Conversation / Message / Identity 术语）
- [strategic/03-bounded-contexts § 2 BC6 Conversation](../../strategic/03-bounded-contexts.md)
- [strategic/03-bounded-contexts § 3 Context Map](../../strategic/03-bounded-contexts.md)

### 同 BC 内聚合详情

- [01-conversation.md](01-conversation.md) — Conversation AR + Message 子从属（kind / content_kind / 生命周期 / Invariants）
- [02-identity.md](02-identity.md) — Identity AR（v2 简化 3 kind）

### 跨 BC 协作文档

- [discussion/00-overview.md](../discussion/00-overview.md) — Issue ↔ Conversation 1:1（kind=issue）
- [task-runtime/00-overview.md](../task-runtime/00-overview.md) — Task ↔ Conversation 1:1（kind=task）
- [task-runtime/03-input-request.md](../task-runtime/03-input-request.md) — InputRequest 集成 Message
- [cognition/00-overview.md](../cognition/00-overview.md) — Supervisor / worker daemon 调 `conversation add-message`
- [observability/00-overview.md](../observability/00-overview.md) — `conversation.*` / `identity.*` 事件订阅

### 横切方法论

- [conventions § 0 DDD](../../../../rules/conventions.md) / § 16 reason+message
