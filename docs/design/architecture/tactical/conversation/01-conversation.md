# Conversation 聚合（+ Message 子从属）

> **DDD 战术层** · BC: Conversation · 聚合: Conversation（AR）+ Message（Entity，子从属）

Conversation 是系统内部"消息时间线"的承载体。Message 是 thread 内单条消息，结构化字段化（content_kind / direction / sender_identity_id 等），不存 vendor 渲染细节。

---

## § 1. 状态机（3 态，[ADR-0032](../../../decisions/0032-conversation-channel-as-first-class.md)）

```mermaid
stateDiagram-v2
    [*] --> active: Conversation 创建
    active --> closed: 业务对象关闭触发<br/>(kind=task: task done/abandoned<br/>kind=issue: concluded/closed_*/withdrawn<br/>kind=channel/dm/adhoc: 用户显式 close 或 TTL)
    active --> archived: 用户显式 archive
    closed --> archived: 已 closed 后 archive
    archived --> [*]: terminal, read-only
```

| 状态 | 含义 |
|---|---|
| `active` | 接受新 message 写入；UI 正常显示 |
| `closed` | 业务对象生命周期 close；message 不再写入但 conversation entry 仍 active 列表里 |
| `archived` | 用户主动归档；read-only；UI 默认隐藏；CLI inspect 仍可查 |

> v2 用户主入口是 Web Console + CLI（vendor 集成已撤回 per [ADR-0031](../../../decisions/0031-v2-drop-bridge-vendor-integration.md)）。

---

## § 2. Conversation Kind 详解（6 种，v2 重命名后 [ADR-0032](../../../decisions/0032-conversation-channel-as-first-class.md)）

| kind | 何时创建 | 何时关闭 | 一致性约束 |
|---|---|---|---|
| `dm` | 用户在 Web Console 主动开 DM；或 supervisor 主动 push 时懒创建 | 一般不自动关闭；用户主动 archive 或 inactivity 超长 TTL 后 close | 每个用户 ↔ supervisor / agent 唯一一个 DM conversation |
| `channel` | 用户 `agent-center channel create` 或 Web Console 「新建 channel」（v2 CV1 重命名自 group_thread；业务一等公民）| 用户显式 archive；不自动 close | name 全局唯一；必有 participant（CV2）|
| `adhoc` | 短期一次性对话（如 supervisor 主动 push 不属于既有 dm/channel 的场合） | 完成 / TTL（默认 24h） | 一次性 |
| `notification` | Supervisor 发周期 review / 系统主动外呼 | 通常发送后短期 close | 通常单向（outbound only） |
| `task` | Task 创建时同事务建 kind=task Conversation（per [ADR-0039 § 7](../../../decisions/0039-conversation-business-model-v2-unified.md)）| Task `done` / `abandoned` → close；保留全部历史 | 跟 Task **1:1**（task.conversation_id ↔ conversation.id）|
| `issue` | Issue 创建时同事务建 kind=issue Conversation；CV4 派生入口可携 carry-over messages | Issue `concluded` / `closed_*` / `withdrawn` → close | 跟 Issue **1:1**（issue.conversation_id ↔ conversation.id）|

### 2.1 kind=task 生命周期补充

- **创建**: 同步建 conversation 走 Task spawn 路径（CLI `task new` / supervisor dispatch 等）；同事务双写 Task + Conversation
- **写入 actor**: supervisor / worker daemon (via agent) / 用户都可写；走 `conversation send` API
- **InputRequest 集成**: agent 调 `request-input` 时同事务写 InputRequest 行 + 一条 `content_kind=agent_finding, input_request_ref=<id>` 的 Message 到 task.conversation_id
- **关闭后**: status=closed 的 task conversation 不再写入；保留 message 历史

### 2.2 kind=issue 生命周期补充

- **创建**: 同步建走 CLI `issue open` / supervisor 自主开 issue 路径；可携 CV4 carry-over（per [ADR-0036](../../../decisions/0036-derive-issue-task-from-messages.md)）
- **写入 actor**: supervisor / 用户 / agent (via worker daemon) / 系统都可写；议事消息走 `conversation send` (content_kind=text/conclusion_draft/task_proposal/agent_finding/system/supervisor_summary)
- **关闭后**: status=closed 不再写入

---

## § 3. Conversation 字段（v2，[ADR-0032](../../../decisions/0032-conversation-channel-as-first-class.md)）

```
conversation (
  id                       ULID/UUID
  kind                     dm | channel | adhoc | notification | task | issue
  name                     TEXT, nullable      -- universal；channel 创建时 app 层必填；其他 kind 可填可空
  description              TEXT, nullable      -- universal nullable
  status                   active | closed | archived  -- v2 加 'archived' 状态
  parent_conversation_id   ULID/UUID, nullable -- universal 父子链（channel→issue / issue→task 等；为 CV3 carry-over 铺基础）
  participants             JSON                -- CV2b ([ADR-0034](../../../decisions/0034-conversation-participants-field.md))：对象数组，元素含 identity_id / role / joined_at / joined_by / left_at? / left_reason?
  created_by               TEXT                -- universal；任何 Conversation 都有创建者 actor
  created_at               ISO8601 TEXT
  updated_at               ISO8601 TEXT
  archived_at              ISO8601 TEXT, nullable
  archived_by              TEXT, nullable
  message_count            INT                 -- universal 计数（v1 已有）
  last_message_at          ISO8601 TEXT, nullable
  version                  INT                 -- 乐观锁
)

-- channel name 强制全局唯一（其他 kind 任意）
UNIQUE INDEX conversation_channel_name_uq (name) WHERE kind = 'channel' AND name IS NOT NULL

-- 父子链按 parent 查 children（CV3 carry-over 需）
INDEX conversation_parent_idx (parent_conversation_id) WHERE parent_conversation_id IS NOT NULL
```

### v2 删除的字段（[ADR-0031](../../../decisions/0031-v2-drop-bridge-vendor-integration.md) vendor 撤回）

- ❌ `primary_channel_hint` — 历史 vendor 路由 ID
- ❌ `primary_channel_thread_key` — 历史 vendor thread key
- ❌ `title` 字段 → 改为 universal `name`（含 channel 的 channel name 用同字段表达）

### v2 新增独立表 `conversation_message_reference`（CV3，[ADR-0035](../../../decisions/0035-cross-conversation-message-carryover.md)）

跨 Conversation message carry-over：子 Conversation 创建时引用父 Conversation 部分 message 作初始上下文。

```
conversation_message_reference (
  id                ULID
  child_conv_id     FK → conversations (强引用)
  source_msg_id     FK → messages (强引用，跨 conv)
  source_conv_id    FK → conversations (冗余便于 join)
  order_in_child    INT
  created_at        ISO8601 TEXT
  created_by        TEXT
)
UNIQUE INDEX (child_conv_id, source_msg_id)
INDEX (child_conv_id, order_in_child)
INDEX (source_msg_id)
```

→ 详 [ADR-0035 CV3 跨 Conversation Message Carry-over](../../../decisions/0035-cross-conversation-message-carryover.md)。

> v3+ 若重新设计外部 IM / 渠道接入，vendor 路由信息走 **独立绑定表**（不再 inline 在 Conversation），跟业务模型清晰解耦：「Conversation 是纯业务时间线，vendor 是 view」。

---

## § 4. Message（Entity，子从属）

### 4.1 字段

```
message (
  id                      ULID/UUID
  conversation_id         FK → conversations (强引用，不可变)
  sender_identity_id      TEXT  -- 'user:hayang' / 'agent:<instance-id>' / 'system:<role>'（per ADR-0033）
  content_kind            text | system | agent_finding | supervisor_summary | conclusion_draft | task_proposal
  content                 TEXT  -- markdown / JSON 视 kind 而定
  direction               inbound | outbound | internal  -- v2 vendor 撤回后 direction 主要表达 "user→system" vs "system→user"
  input_request_ref       ULID/UUID, nullable  -- 跨 BC 关联到 TaskRuntime InputRequest.id（per ADR-0039）
  carry_over_ref          ULID/UUID, nullable  -- 跨 conversation 弱引用（per ADR-0035）
  posted_at               ISO8601 TEXT  -- 服务器时间
)
```

> v2 删除字段（[ADR-0031](../../../decisions/0031-v2-drop-bridge-vendor-integration.md) vendor 撤回）：
> - ❌ `vendor_msg_ref` — 历史 vendor message id；v3+ 若需重新接入外部 IM，再独立设计

### 4.2 Content Kinds 详解（6 种）

| kind | 用途 | content 格式 |
|---|---|---|
| `text` | 用户自由文本输入 / 普通 markdown / slash 命令留痕 | markdown 字符串 |
| `system` | 系统元信息（"已 spawn task X" / "task created / dispatched / status changed" 之类）| markdown 字符串 |
| `agent_finding` | Agent / worker daemon 的进展 / 请示 / 完成报告（含 `input_request_ref` 非空时承载 InputRequest 问题）| markdown 字符串 |
| `supervisor_summary` | Supervisor 的分析 / 决策 / 中间思考摘要 | markdown 字符串 |
| `conclusion_draft` | Issue conclude flow 中 supervisor 写的"结论草案"（在 `kind=issue` Conversation 内）；Web Console / CLI 渲染为含 [确认结论] [改后确认] [不做] 按钮的卡片（per [ADR-0037](../../../decisions/0037-web-console-as-main-user-ui.md)）| markdown 字符串（含 Task spawn 列表）|
| `task_proposal` | Issue 议事中提出的"建议 spawn 的 Task"（独立条目，supervisor 或 user 写）| markdown 字符串 |

**Message 只存语义内容**，渲染由 presentation 层（Web Console / CLI）按 `content_kind + input_request_ref` 决定（per [ADR-0037](../../../decisions/0037-web-console-as-main-user-ui.md) / [ADR-0038](../../../decisions/0038-cli-ux-enhancement.md)）。

> **撤回 `task_progress` content_kind**：[ADR-0016](../../../decisions/0016-task-progress-via-bound-thread.md) 曾规划新增 `task_progress` kind 承载 worker 进度流；后撤回 —— 进度本质就是 worker 的 `agent_finding`，复用既有 kind 即可。

未来 kind（v3+）：voice / image / file / is_pinned / parent_message_id 等视需求按需新增。

### 4.3 Message Direction 语义

| direction | 含义 |
|---|---|
| `inbound` | 用户 → 系统（Web Console / CLI 用户消息写入）|
| `outbound` | 系统 → 用户（supervisor / agent / system 主动 push）|
| `internal` | 纯系统内消息（不展示给用户，如 supervisor 内部摘要）|

---

## § 5. Lifecycle Operations

| Op | 行为 | 跨聚合写 |
|---|---|---|
| `open` | 创建 Conversation（按 kind 走不同 Factory 路径，详见 [00-overview § 4.1](00-overview.md)）| task/issue kind 时跨 BC 跟 Task/Issue 同事务建（强 1:1）|
| `add-message` | 往 Conversation 写一条 Message | emit `conversation.message_added` 供订阅方（Observability / Cognition）响应 |
| `close` | 状态终结 | 同事务（如 task done / issue concluded 时跨 BC 触发）|

---

## § 6. Invariants

### Conversation 不变量

1. **id / kind 不可变**：创建时定，永不改
2. **terminal 状态 closed 不可逆**
3. **closed 后不再接受 add-message**：应用层校验
4. **kind=task / kind=issue 必有上游强引用**：`task.conversation_id` / `issue.conversation_id` 跟 conversation 互为镜像（per [ADR-0039](../../../decisions/0039-conversation-business-model-v2-unified.md)）
5. **kind=task / kind=issue 必走 TaskRuntime / Discussion 的同步建 / 懒创建路径**

### Message 不变量

1. **append-only**：Message INSERT 后不修改（v3+ 加 edit history 是别的事；v2 不支持）
2. **conversation_id / sender_identity_id 不可变**：创建时填，永不改
3. **direction 不可变**：创建时定
4. **closed Conversation 不接 add-message**：跟 § 6.3 配合
5. **input_request_ref 跟 InputRequest 同事务双写**（per [ADR-0039](../../../decisions/0039-conversation-business-model-v2-unified.md) + [ADR-0014 § 2](../../../decisions/0014-event-sourcing-level.md)）

---

## § 7. CLI 命令

| 命令 | 用途 |
|---|---|
| `agent-center conversation add-message --id=X --content="..." [--kind=text/system/...] [--dedupe-key=...]` | 往指定 Conversation 内写一条 Message |
| `agent-center conversation list [--participant=...] [--kind=...] [--since=...]` | 列 / 过滤 Conversation |
| `agent-center inspect conversation <id>` | 时间线（全部 Message） |
| `agent-center conversation read <id> [--tail=N]` | 拉最近 N 条消息（给 supervisor 当 context 用）|

完整 CLI 见 [agent-harness/02-skill-cli-tooling.md](../agent-harness/02-skill-cli-tooling.md)。

---

## § 8. 领域事件

| Event | 触发 | 主要 payload |
|---|---|---|
| `conversation.opened` | 新建 Conversation | conversation_id, kind |
| `conversation.closed` | Conversation 终结 | conversation_id, reason+message |
| `conversation.message_added` | Message 入库（不区分 inbound/outbound，由 message.direction 字段标识）| conversation_id, message_id, sender, content_kind, direction |

详见 [observability/00-overview.md § 7.5 事件总览](../observability/00-overview.md)。

---

## § 9. References

- [ADR-0007 引入 Conversation 层](../../../decisions/0007-conversation-as-unified-session.md)（Refined by 0039）
- [ADR-0031 v2 撤回 Bridge / vendor 集成](../../../decisions/0031-v2-drop-bridge-vendor-integration.md)
- [ADR-0032 channel 升业务一等公民](../../../decisions/0032-conversation-channel-as-first-class.md)
- [ADR-0033 Identity 模型重构](../../../decisions/0033-identity-model-refactor.md)
- [ADR-0034 participants 字段](../../../decisions/0034-conversation-participants-field.md)
- [ADR-0035 跨 conversation message carry-over](../../../decisions/0035-cross-conversation-message-carryover.md)
- [ADR-0039 Conversation 业务模型 v2 统一](../../../decisions/0039-conversation-business-model-v2-unified.md)（supersedes 0017/0021/0022，已删）
- [00-overview.md](00-overview.md) — BC 入口（Domain Services / Factory / 跨 BC 交互）
- [02-identity.md](02-identity.md) — Identity AR
- [task-runtime/03-input-request.md](../task-runtime/03-input-request.md) — InputRequest + Message 集成
