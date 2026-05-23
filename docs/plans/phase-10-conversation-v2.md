# Phase 10: Conversation v2

> DDD Conversation BC 大重写 + 删 v1 Bridge BC + Discussion / TaskRuntime 跟 Conversation 关系同步 · 依赖 Phase 8 · 解锁 Phase 11
> 纪律：按里程碑顺序 / 模块完备不半成品 / TDD + 单测 ≥ 90% + 集成 + e2e + 测试报告

覆盖 ADR：[ADR-0031 v2 Drop Bridge](../design/decisions/0031-v2-drop-bridge-vendor-integration.md) · [ADR-0032 CV1 Channel](../design/decisions/0032-conversation-channel-as-first-class.md) · [ADR-0033 Identity refactor](../design/decisions/0033-identity-model-refactor.md) · [ADR-0034 Participants 字段](../design/decisions/0034-conversation-participants-field.md) · [ADR-0035 Carry-over](../design/decisions/0035-cross-conversation-message-carryover.md) · [ADR-0036 派生入口](../design/decisions/0036-derive-issue-task-from-messages.md) · [ADR-0039 Conversation 业务模型 v2 统一](../design/decisions/0039-conversation-business-model-v2-unified.md)（supersedes 0017/0021/0022）

## § 0. 目标

把 Conversation 模型重塑为 v2 纯业务时间线 + 关联实体；撤回 v1 Bridge BC + 飞书集成代码：

- **Conversation BC schema 重写**：删 vendor stained 字段；加 universal name/description/parent_conversation_id/created_by/archived_*/participants/version
- **kind 重命名** group_thread → channel；channel 业务一等公民
- **Identity refactor**: 4 kind → 3 (user/agent/system)；ID 格式 `kind:id`；Identity[kind=agent].id = AgentInstance.id；删 ChannelBinding
- **conversation_message_reference 表**: 跨 conv message carry-over
- **派生入口 CLI/服务**: `issue open / task new --from-conversation=... --select-messages=... --project=...` 含 carry-over refs 同事务
- **删 v1 Bridge BC 代码**: internal/bridge/* / 飞书 SDK 集成

## § 1. DDD 工件清单

### 1.1 Aggregate Roots

| AR | BC | 备注 |
|---|---|---|
| **Conversation**（重写）| Conversation | 字段大改：删 primary_channel_hint / primary_channel_thread_key / title；加 name/description/parent_conversation_id/participants/created_by/archived_at?/archived_by?/version |
| **Identity**（重写）| Conversation | kind 简化 4 → 3；ID 格式约定 |
| **Message**（小改）| Conversation | 删 vendor_msg_ref；direction 语义淡化（v3 vendor 接入时复活）|

### 1.2 Entities

无新增；Message 是 Conversation 子从属保留。

### 1.3 Value Objects

| VO | 来源 ADR |
|---|---|
| **ConversationKind**（重命名）| 0032 group_thread → channel；新枚举 6 个值 |
| **ConversationStatus**（扩）| 0032 加 archived 状态 |
| **ParticipantElement** | 0034 JSON 对象 {identity_id, role, joined_at, joined_by, left_at?, left_reason?} |
| **CarryOverReference** | 0035 conversation_message_reference 表的 Go 类型 |

### 1.4 Repositories

```go
type ConversationRepository interface {
    // 大改：FindByChannelAndThreadKey 删（vendor 撤回）
    FindByID(ctx, id) (*Conversation, error)
    FindByName(ctx, name string) (*Conversation, error)  // 新：用于 channel name 查找
    FindByKind(ctx, kind, filter) ([]*Conversation, error)
    FindByParent(ctx, parentID) ([]*Conversation, error)  // 新：父子链
    Save(ctx, c) error
    UpdateStatus(ctx, id, from, to, version) error
    UpdateParticipants(ctx, id, participants []ParticipantElement, version) error  // 新
}

type IdentityRepository interface {
    FindByID(ctx, id) (*Identity, error)
    FindByKind(ctx, kind) ([]*Identity, error)
    Save(ctx, i) error
    // 删：ChannelBinding 相关方法
}

type MessageRepository interface {
    // 删 FindByVendorMsgRef / UpdateVendorMsgRef
    FindByConversationID(ctx, convID, filter) ([]*Message, error)
    Save(ctx, m) error
    // append-only；无 update
}

type ConversationMessageReferenceRepository interface {
    Save(ctx, refs []*ConversationMessageReference) error  // batch insert
    FindByChildConvID(ctx, childConvID) ([]*ConversationMessageReference, error)
    FindBySourceMsgID(ctx, sourceMsgID) ([]*ConversationMessageReference, error)
    DeleteByChildConvID(ctx, childConvID) error
}
```

### 1.5 Domain Services

| Service | 备注 |
|---|---|
| **ChannelManagementService**（新）| CV1: channel create / archive (kind=channel only)；含 name 全局唯一校验 |
| **ConversationLifecycleService**（重写）| 通用 create / archive / status 变化；不再有 BridgeRoutingService |
| **ParticipantManagementService**（新）| CV2b: invite / leave / kick；调用方 = channel owner |
| **IdentityRegistrationService**（重写）| CV2a: 简化（v2 单用户 init `user:hayang` + auto-add Identity[kind=agent] when AgentInstance create）|
| **CarryOverService**（新）| CV3: 跨 conv message reference 建立 + 查询 |
| **MessageDerivationService**（新）| CV4: 从 messages 派生 Issue / Task 同事务流程 |

### 1.6 Application Services（CLI）

```
agent-center channel create --name=<n> [--description=<d>]
agent-center channel list [--status=<s>]
agent-center channel show <name>
agent-center channel archive <name>
agent-center channel invite <name> <identity-id>
agent-center channel leave <name>
agent-center channel kick <name> <identity-id>
agent-center channel participants <name>

agent-center conversation send <conv> <text>   # 通用
agent-center conversation show <id>
agent-center conversation refs <id>            # carry-over 反查
agent-center conversation tail <id> [-f]       # v1 已有；本 phase 改实现走 admin endpoint polling

agent-center message refs <msg-id>             # 反查

agent-center issue open --from-conversation=<c> --select-messages=<m1,m2,...> --project=<p> --title=<t>
agent-center task new --from-conversation=<c> --select-messages=<m> --project=<p> --agent=<a> --title=<t>

agent-center identity add --name=<n>           # v2 简化；通常 init 一次
agent-center identity list [--kind=<k>]
agent-center identity show <id>
```

### 1.7 Domain Events

| 事件 |
|---|
| `conversation.opened`（payload 加 parent_conversation_id? / with_carry_over: bool）|
| `conversation.message_added`（已有；不变）|
| `conversation.archived`（新）|
| `conversation.participant_joined / participant_left`（新）|
| `conversation.message_references_added`（新）|

删除事件：`channel.delivered / channel.delivery_failed / bridge.parse_failed`（Bridge BC 已删）

### 1.8 Context Map 关系

- **Conversation ↔ Workforce**：Identity[kind=agent].id = AgentInstance.id (跨聚合 invariant, 应用层校验)
- **TaskRuntime ↔ Conversation**：Task ↔ Conversation 1:1 不变（per ADR-0039 § 7）；CV4 派生入口跨 BC 同事务
- **Discussion ↔ Conversation**：Issue ↔ Conversation 1:1 不变；CV4 派生入口跨 BC 同事务
- **~~Bridge ↔ Conversation~~**：v2 已撤回

## § 2. 上游依赖（来自 Phase 8 + v1）

| 上游工件 | 用在 |
|---|---|
| AgentInstance.id (P8 3.5) | Identity[kind=agent].id 引用 + 跨聚合 invariant |
| AgentInstance create 同事务 INSERT Identity (P8 3.5 +) | 本 phase 完整 spec |
| Task / Issue AR (v1 P2 / P3) | CV4 派生入口同事务 |

## § 3. 工作项分解

### 3.0 DB Schema Migration

| Migration | 内容 |
|---|---|
| `0020_v2_conversation_schema_reset.up.sql` | Conversation 表 drop 字段 (primary_channel_hint / primary_channel_thread_key / title) + add (name / description / parent_conversation_id / participants JSON / created_by / archived_at / archived_by / version)；CHECK constraint UNIQUE name WHERE kind='channel' |
| `0021_v2_identity_simplify.up.sql` | identity 表 kind enum 改 (user/agent/system)；drop channel_bindings 表 |
| `0022_v2_conversation_message_reference.up.sql` | 新 conversation_message_reference 表 |
| `0023_v2_message_strip_vendor.up.sql` | message 表 drop vendor_msg_ref 字段 |
| `0024_v2_kind_rename.up.sql` | conversation.kind 行 group_thread → channel 重命名（如有 v1 行）|

### 3.1 Conversation AR 重写

- **工件**：`internal/conversation/conversation.go` + `_repository.go`
- **依赖**：3.0
- **步骤**：
  1. 重写 struct 字段
  2. Repository 实现新 method（FindByName, FindByParent, UpdateParticipants 等）
  3. 删 vendor-coupled methods
- **DoD**：单测 / 集成测覆盖；append-only message 验证

### 3.2 Identity 重写

- **工件**：`internal/conversation/identity.go`
- **依赖**：3.0
- **步骤**：
  1. Identity struct kind 简化
  2. Repository 删 ChannelBinding 相关
  3. ID 格式 parser 校验 (`kind:id`)
  4. 跨聚合 invariant 校验（agent:<x> 必须 AgentInstance exist）
- **DoD**：Identity[kind=agent] create 跟 AgentInstance create 同事务（应用层）

### 3.3 ChannelManagementService

- 实装 CV1 channel create / archive；name 全局唯一校验；creator 自动入 participants
- CLI handlers
- **DoD**：CLI e2e；name 冲突 / archive built-in conv 拒（虽 channel 不 builtin）

### 3.4 ParticipantManagementService

- 实装 invite / leave / kick；JSON participants 字段 read-modify-write 加乐观锁
- CLI handlers
- emit participant_joined / participant_left 事件
- 严格 join 规则（消息发送前必须 active participant）
- **DoD**：CV2b 全部 case 单测 + 集成测

### 3.5 CarryOverService + conversation_message_reference Repository

- 实装 Save batch + FindByChildConvID + FindBySourceMsgID
- 单测验证 append-only / source_msg 跨 conv FK 校验
- **DoD**：单测覆盖；集成测 cross-conv reference

### 3.6 MessageDerivationService（CV4 派生入口）

- 实装 issue open / task new --from-conversation=... 同事务
- 校验链：from-conversation 存在 active / user 是 active participant / select messages 都属于 from-conv / project 存在 / agent_instance 存在 (Task 才需)
- 同事务 INSERT Conversation + Issue/Task + carry-over refs + participants
- CLI handlers
- **DoD**：所有校验 path 单测 + 集成测；e2e

### 3.7 conversation send + tail CLI

- conversation send 通用 + alias (channel post / issue comment / task comment)
- conversation tail (-f) polling 实现（不通过 Web SSE）
- **DoD**：CLI e2e；polling 1s 间隔；--format=table|json|yaml 三格式

### 3.8 IdentityRegistrationService 重写

- v2 简化：`agent-center identity add --name=<n>` init 一次；后续 AgentInstance create 自动 register Identity[kind=agent]
- system identity center 启动 auto-provision
- **DoD**：单测 + 集成测

### 3.9 Bridge BC 代码删除

- 删 `internal/bridge/` 整个包（含 feishu / 飞书 SDK 集成等）
- 删 `internal/bridgeapp/` （如有）
- 删相关 cmd subcommand（如 `agent-center bridge run` 等）
- 删配置项 `bridge.*`
- **DoD**：grep 验证 `internal/bridge` 不存在；compile / test 全过

### 3.10 跨 BC 同事务集成（Discussion / TaskRuntime）

- Issue create 时同事务 INSERT Conversation (kind=issue)（v1 已有；本 phase 验证 vendor 撤回后流程仍 work）
- Task create 时同事务 INSERT Conversation (kind=task)（同）
- CV4 派生入口 = 跨 BC 同事务（Discussion / TaskRuntime + Conversation）—— 3.6 已涵盖
- **DoD**：集成测 Task / Issue create 流程完整

### 3.11 docs sync

- 现 conversation/00/01/02 docs 已 update（CV1+CV2 阶段），bulk purge 阶段已加 v2 横幅
- 本 phase 完成时把横幅 + 「待 rewrite」 note 全部去掉（模型已稳定）
- 加 CV3 / CV4 / Identity 重写 等示例

## § 4. Definition of Done

- [ ] § 3.0 - 3.11 全部完成 + TDD
- [ ] 单测 ≥ 90%
- [ ] internal/bridge 包不存在（grep 验证）
- [ ] vendor_msg_ref / primary_channel_hint 等字段不在 DB / 不在 Go struct
- [ ] CV1 channel CRUD + invite/leave/kick e2e 通
- [ ] CV3 carry-over reference 同事务建立 + 反查通
- [ ] CV4 派生入口 (issue/task) 完整 e2e
- [ ] Identity refactor: 旧 v1 Identity[kind=supervisor/bot] 数据 migrate / drop（v2 不考虑 back-compat 直接 drop）
- [ ] supervisor.md skill 加「user identity / system identity 引用」 说明
- [ ] phase-10-test-report.md 归档

## § 5. 测试计划

### 5.1 单测场景

| 工件 | 场景 |
|---|---|
| Conversation 新 schema | name 全局唯一 (channel only) / parent 链 / participants JSON r-m-w |
| Identity refactor | 4→3 kind 校验；ID 格式 parser；跨聚合 invariant 校验 |
| ChannelManagementService | create + name 冲突 / archive read-only |
| ParticipantManagementService | invite / leave / kick / 严格 join 校验 |
| CarryOverService | batch save / source_msg 跨 conv 验证 |
| MessageDerivationService | 6+ 校验 path |

### 5.2 集成测

| 场景 |
|---|
| Conversation create (kind=channel) + INSERT participants 同事务 |
| AgentInstance create → Identity[kind=agent] INSERT 同事务 (P8 集成) |
| Issue/Task derive from messages 全链路 |
| Participants race condition（多 invite 并发）|

### 5.3 e2e

| 场景 |
|---|
| User 创建 channel → invite agent → send message → archive |
| User 在 channel 里选 messages → 派 Issue with carry-over → CV3 reference 反查 |
| Issue → spawn Task with carry-over from Issue → 双层父子链验证 |
| Identity refactor: 旧 v1 数据 (`supervisor:invocation-X`) migrate fail-safely |

## § 6. 风险

| 风险 | 缓解 |
|---|---|
| 旧 Conversation 数据 vendor 字段含值 → migration | v2 不考虑 back-compat；migration 时直接 drop 列；v1 数据丢弃 / archive 别处 |
| Bridge BC code 删除影响 cmd / config | 全 grep + repo-wide refactor；compile 失败即知道哪还引用 |
| Identity[kind=agent].id 跟 AgentInstance.id 跨表一致性 | 应用层校验 + tests + 集成测 |
| Participants JSON r-m-w 并发 race | 乐观锁 version；测试覆盖 |
| MessageDerivationService 跨 BC 事务（Discussion / TaskRuntime / Conversation） | 单 SQLite 库 + 同 tx；测试覆盖 rollback |

## § 7. 下游解锁

本 phase 完成 → **Phase 11 启动**（phase 间严格串行）。所有 v2 业务模型稳定 + Bridge code 已删。
