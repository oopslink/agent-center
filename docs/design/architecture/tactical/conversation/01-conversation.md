# Conversation 统一会话层

> **DDD 战术层** · BC: Conversation

Conversation BC 是 agent-center 的**内部会话模块** —— 系统内的"消息时间线"存储，**跟具体接入渠道（飞书 / DingTalk / Web chat / ...）解耦**。

类比一个会议：有人在会议室、有人在飞书、有人在电话上 —— 都是同一个会议的上下文。但 Conversation 模块**不知道也不关心**这些接入方式；它只是消息的内部存储。**外部渠道集成由 [Bridge BC](../../strategic/03-bounded-contexts.md#bc8-bridge渠道接入层) 通过事件驱动的双向同步实现**。

> 命名 / 定位决策见 [ADR-0007](../../../decisions/0007-conversation-as-unified-session.md) + [ADR-0009](../../../decisions/0009-issue-conversation-decoupled-via-bridge.md) + [ADR-0017](../../../decisions/0017-task-as-conversation.md)（Task ↔ Conversation 1:1）。

---

## § 1. 设计原则

| 原则 | 含义 |
|---|---|
| **纯内部模块** | Conversation BC 是系统内部数据模块，**不调用任何 vendor SDK**（[conventions § 9.y](../../../../rules/conventions.md#-9y-外部集成走-bridge-模式不在领域模块内调-vendor-sdk)） |
| **跟 Issue 解耦** | Conversation 跟 Issue 是独立聚合，Conversation **不知道** Issue 存在（[ADR-0009](../../../decisions/0009-issue-conversation-decoupled-via-bridge.md)） |
| **跟 channel 解耦** | Conversation 不持有 vendor-specific 字段；vendor id 通过 `ChannelBinding` 关联到 `Identity` |
| **CLI 是"内部写入"** | `agent-center conversation add-message --id=X` 是"往系统内 conversation 写一条 message"；外发 vendor 由 Bridge 订阅事件自动处理 |
| **v1 单 vendor** | Bridge 层 v1 只实现 FeishuBridge；其它 vendor 见 [roadmap](../../../roadmap.md) |
| **可独立运行** | 没 Bridge 时 conversation 仍能跑（只是没人能从 vendor 侧 IO） |

---

## § 2. 核心聚合（概念层）

### Conversation（根）

| 字段 | 含义 |
|---|---|
| `id` | UUID |
| `kind` | `dm` / `group_thread` / `adhoc` / `notification` / `task`（**没有 `issue` kind**，见 [ADR-0009](../../../decisions/0009-issue-conversation-decoupled-via-bridge.md)；`task` kind 跟 Task 1:1 绑定，见 [ADR-0017](../../../decisions/0017-task-as-conversation.md)）|
| `title` | 可选展示标题 |
| `primary_channel_hint` | 主要承载渠道的提示（如 `feishu`），Bridge 用于决定 outbound 路由；可空 |
| `primary_channel_thread_key` | 在该 channel 中的 vendor-specific 路由 key（如 `feishu:thread:xyz` 或 `feishu:dm:ou_xxx`）；Bridge 用于反向查找 |
| `status` | open / closed |
| `opened_at, closed_at` | 时间戳 |

> 注意：`primary_channel_hint` / `primary_channel_thread_key` 是 Bridge 用来路由的字段，**对 Conversation 自身没有业务意义** —— Conversation 模块只是顺手存着不查询它。把这些字段往 conversation 上放是务实选择（避免再加一张 channel binding 表），但也可放进单独的 conversation_channel_routes 表里更纯粹。v1 inline 在 conversation 上。

### Message（实体，从属 Conversation）

| 字段 | 含义 |
|---|---|
| `id` | UUID |
| `conversation_id` | 所属 conversation |
| `sender_identity_id` | 发送者 Identity |
| `content_kind` | text / system / agent_finding / supervisor_summary（详见 § 5；**注意没有 lark_card** —— Bridge 渲染时按需翻译） |
| `content` | TEXT (markdown / JSON 视 kind 而定) |
| `direction` | inbound（从 vendor 同步进来）/ outbound（写入待 Bridge 推到 vendor）/ internal（纯系统内对话，无 vendor 同步） |
| `vendor_msg_ref` | 可选: 对应的 vendor message id（Bridge 写 inbound 时记录；outbound 投递成功后回填） |
| `input_request_ref` | 可空: 关联的 InputRequest.id；非空时该 Message 是 agent 请示的载体，Bridge 渲染时附带选项 / 取消按钮，并在 InputRequest 状态变化时 update_card 置灰（详见 [ADR-0017 § 5](../../../decisions/0017-task-as-conversation.md) 与 [04-input-required.md](../scheduling/02-input-required.md)） |
| `posted_at` | 时间戳（服务器时间） |

### Identity（根，独立聚合）

| 字段 | 含义 |
|---|---|
| `id` | `user:hayang` / `supervisor:invocation-N` / `agent:session-X` / `bot` |
| `kind` | user / supervisor / agent / bot |
| `display_name` | 显示名 |

### ChannelBinding（值对象，从属 Identity）

| 字段 | 含义 |
|---|---|
| `identity_id` | 所属 identity |
| `channel` | feishu / dingtalk / web / ... |
| `vendor_user_id` | 那条 channel 里的用户 id（feishu open_id 等）|
| `preferred` | 该 identity 的默认推送渠道？|
| `bound_at` | 时间戳 |

---

## § 3. Conversation Kind 详解

| kind | 何时创建 | 何时关闭 | 一致性约束 |
|---|---|---|---|
| `dm` | 用户首次在 DM 中触达 bot（FeishuBridge 创建）；或 supervisor 主动 push 时（无对应 dm conv 则 Bridge 自动创建） | 一般不自动关闭；用户主动 unbind 或 inactivity 超长 TTL 后 close | 每个用户 ↔ bot 唯一一个 DM conversation per channel |
| `group_thread` | 首次在群里某 thread @bot（FeishuBridge 创建） | thread 长期无活动 close（默认 30d） | 1 vendor thread ↔ 1 conversation |
| `adhoc` | 短期一次性对话（如 supervisor 主动 push 不属于既有 dm/thread 的场合） | 完成 / TTL（默认 24h） | 一次性 |
| `notification` | Supervisor 发周期 review / 系统主动外呼 | 通常发送后短期 close | 通常单向（outbound only） |
| `task` | Task 创建时按来源决定（飞书 @bot / Web Console 同步建；Issue spawn / CLI / supervisor 自主开则懒创建，详见 [ADR-0017 § 1](../../../decisions/0017-task-as-conversation.md)） | Task `done` / `abandoned` → close（保留全部历史，不删；用户翻飞书 thread 仍能看；CLI `inspect conversation` 仍可查） | 跟 Task **1:1**（task.conversation_id ↔ conversation.id；不引入 1:N 多 tracker，见 [ADR-0017 § 9](../../../decisions/0017-task-as-conversation.md)） |

**没有 `issue` kind** —— Issue 跟 Conversation 是解耦的两个聚合，参见 [03-issue-discussion.md](../discussion/01-issue-discussion.md)。

### kind=task 的生命周期补充

- **创建**: 同步建 conversation 走 a/e（飞书 @bot / Web Console）；懒创建走 b/c/d（CLI `task bind-card` / 飞书 slash `/track` / @bot 自由文本 / center 硬规则 fallback）。详见 [ADR-0017 § 7 / § 10](../../../decisions/0017-task-as-conversation.md)
- **thread_key 写入方向**: 与 a 路径（inbound 创建时已有 vendor thread）相反，task conversation 的 `primary_channel_thread_key` 由 Bridge 发出 Task root card 后回写
- **写入 actor**: supervisor / worker daemon / 用户 / 系统都可写；worker daemon 通过 [BC4 长连 RPC](../../strategic/03-bounded-contexts.md) 调 `conversation add-message`（详见 [ADR-0017 § 8](../../../decisions/0017-task-as-conversation.md)）
- **InputRequest 集成**: agent 调 `request-input` 时同事务写 InputRequest 行 + 一条 `content_kind=agent_finding, input_request_ref=<id>` 的 Message 到 task.conversation_id；conversation_id 为 null 时触发 § 10.4 fallback bind 到 `notification.default_channel`，未配置则 InputRequest 创建失败、task fail
- **关闭后**: status=closed 的 task conversation 不再写入；v1 不引入 GC（保留期由后续 [implementation/02-persistence-schema.md](../implementation/) 决定）

---

## § 4. Identity 与 ChannelBinding（v1 单用户模式）

### v1 单用户简化

agent-center 定位个人工具（[需求 § 1.2](../../../requirements/00-overview.md)）。简化：

- **一个用户 Identity**：`user:<configured-name>`（通过 `agent-center identity add` 初始化时建立）
- **任何 vendor 非 bot 来源** → 默认归属这个 user identity
- 新 vendor 来源（首次见到 feishu open_id）→ **自动绑定**为该 user identity 的 ChannelBinding，不再二次确认
- 多 user / 跨用户消息归属逻辑 → v2+（多用户场景），见 [roadmap](../../../roadmap.md)

### Supervisor / Agent / Bot Identity

- Supervisor Identity：每次 SupervisorInvocation 启动时临时创建 `supervisor:<invocation-id>`
- Agent Identity：每个 AgentSession 启动时临时创建 `agent:<session-id>`
- Bot Identity：安装时创建一次 `bot`，永久存在

### CLI

```
agent-center identity add user:hayang --display-name="Hayang"
agent-center identity list
agent-center identity bind user:hayang --channel=feishu --vendor-user-id=ou_xxx
agent-center identity unbind user:hayang --channel=feishu
```

---

## § 5. Message Content Kinds

| kind | 用途 | content 格式 |
|---|---|---|
| `text` | 用户自由文本输入 / 普通 markdown / slash 命令留痕 | markdown 字符串 |
| `system` | 系统元信息（"已 spawn task X" / "task created / dispatched / status changed" 之类）| markdown 字符串 |
| `agent_finding` | Agent / worker daemon 的进展 / 请示 / 完成报告（含 `input_request_ref` 非空时承载 InputRequest 问题） | markdown 字符串 |
| `supervisor_summary` | Supervisor 的分析 / 决策 / 中间思考摘要 | markdown 字符串 |

**没有 `lark_card`** —— card 是渲染细节，由 Bridge 翻译。Conversation Message 只存语义（"这是个 system / summary / 用户文本"），不存 vendor rendering。

Bridge 根据 content_kind + `input_request_ref` 决定渲染：
- text / agent_finding（input_request_ref 为空） → 飞书普通消息（markdown）
- system → 飞书消息或卡片
- supervisor_summary → 飞书 card
- agent_finding **+ input_request_ref ≠ null** → 飞书 interactive card 含选项 / "自己写" / 取消按钮（详见 [ADR-0017 § 5](../../../decisions/0017-task-as-conversation.md) 与 [09-feishu-integration.md § 6](../bridge/01-feishu-integration.md)）

> **撤回 `task_progress` content_kind**：[ADR-0016](../../../decisions/0016-task-progress-via-bound-thread.md) 曾规划新增 `task_progress` kind 承载 worker 进度流；[ADR-0017](../../../decisions/0017-task-as-conversation.md) supersede 后撤回 —— 进度本质就是 worker 的 `agent_finding`，复用既有 kind 即可。

未来 kind（v2+）：voice / image / file 视需求按需新增。

---

## § 6. 跟 Bridge 的协作模式

Conversation BC 不主动调 Bridge。Bridge **订阅 Conversation 事件**做同步：

### Outbound（系统 → vendor）

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
   - input_request_ref ≠ null → join InputRequest 表取 options / 当前状态 → 渲染按钮
   - 调飞书 SDK 投递
   - 成功 → 回填 message.vendor_msg_ref；emit channel.delivered
   - 失败 → 重试 3 次；最终失败 → emit channel.delivery_failed
```

> **Worker daemon 是合法 actor**: worker daemon 也可（通过 [BC4 长连 RPC](../../strategic/03-bounded-contexts.md)）调 `conversation add-message`，用于把 worker 进度 milestone / agent 请示写到 task.conversation_id（[ADR-0017 § 8](../../../decisions/0017-task-as-conversation.md)）。

### Inbound（vendor → 系统）

```
FeishuBridge 收 vendor 事件 (im.message.receive_v1 等)
   ↓
路由判断 1: 该 thread/dm 是否绑到某 issue 的 bound_card?
   是 → 直接调 agent-center issue comment <id> --content=... --source-message-ref=<vendor_msg_id>
        (写 IssueComment, 不进 Conversation, 见 [03-issue-discussion.md § Bound Card](../discussion/01-issue-discussion.md#bound-card-机制))
        ✋ 流程结束

   否 → 路由到 Conversation:
        - 按 (channel, vendor_thread_key) 查 conversation
        - 找到 → 调 agent-center conversation add-message --id=<conv> 写入
        - 找不到 → 按 event context 创建新 conversation (kind=dm / group_thread / adhoc)
                    然后写入 Message
   ↓
Conversation BC 写入 Message (direction=inbound, vendor_msg_ref=...)
emit conversation.message_added
   ↓
其它 BC 订阅事件做业务 (如 Supervisor 决定是否回复)
```

**Conversation BC 本身没有任何 vendor 集成代码** —— 它只是接受写入请求 + emit 事件。

---

## § 7. CLI 命令

### 对人 / supervisor 通用

| 命令 | 用途 |
|---|---|
| `agent-center conversation add-message --id=X --content="..." [--kind=text/system/...] [--dedupe-key=...]` | 往指定 Conversation 内写一条 Message（内部存储；Bridge 自动同步外发） |
| `agent-center conversation list [--participant=...] [--kind=...] [--since=...]` | 列 / 过滤 Conversation |
| `agent-center inspect conversation <id>` | 时间线（全部 Message） |
| `agent-center conversation read <id> [--tail=N]` | 拉最近 N 条消息（给 supervisor 当 context 用）|

### Identity 管理

| 命令 | 用途 |
|---|---|
| `agent-center identity add user:<name> --display-name="..."` | 创建 user identity（v1 通常 setup 时跑一次） |
| `agent-center identity list` | 列所有 identity |
| `agent-center identity bind <identity-id> --channel=feishu --vendor-user-id=...` | 手动绑定 ChannelBinding |
| `agent-center identity unbind <identity-id> --channel=feishu` | 取消绑定 |

完整 CLI 见 [10-skill-cli-tooling.md](../_cross-cutting/02-skill-cli-tooling.md)。

---

## § 8. 领域事件

| Event | 触发 | 主要 payload |
|---|---|---|
| `conversation.opened` | 新建 Conversation | conversation_id, kind |
| `conversation.closed` | Conversation 终结 | conversation_id, reason |
| `conversation.message_added` | Message 入库（不区分 inbound/outbound，由 message.direction 字段标识） | conversation_id, message_id, sender, content_kind, direction |
| `channel.delivered` | Bridge 投递成功（注：是 Bridge BC 的事件，不是 Conversation 的）| message_id, channel, vendor_msg_ref |
| `channel.delivery_failed` | Bridge 重试用尽 | message_id, channel, error |
| `identity.registered` | 新 Identity 创建 | identity_id, kind |
| `identity.channel_bound` | 加 ChannelBinding | identity_id, channel, vendor_user_id |
| `identity.channel_unbound` | 解绑 | identity_id, channel |

---

## § 9. 跟其它 BC 的交互

| 方向 | 方式 | 例子 |
|---|---|---|
| **Conversation → ALL** | Pub/Sub | 所有 BC 可订阅 `conversation.message_added`，按需响应 |
| **Discussion → Conversation** | Customer-Supplier | （罕见）Discussion 不直接读写 Conversation；通常只在"R1 自动关联 conversation 到 issue" 时通过 issue 的 API 间接发生 |
| **Cognition → Conversation** | Customer-Supplier | Supervisor 调 `conversation add-message` 在某 Conversation 内发声（如普通聊天）|
| **Bridge → Conversation** | Customer-Supplier | Bridge 收 vendor inbound 时调 `conversation add-message` 写入 |
| **Bridge ← Conversation** | Pub/Sub | Bridge 订阅 `conversation.message_added` → 外发到 vendor |

**关键约束**：Conversation BC **不直接调用** 任何 vendor SDK / API。所有外呼通过 Bridge 订阅事件后处理。

---

## § 10. 失败 / 错误处理

### Inbound 失败

- Bridge 解析 vendor event 失败 → Bridge 自己写 `bridge.parse_failed` 事件 + 日志；不传到 Conversation BC
- Bridge 找不到 conversation 也找不到合适 kind 创建 → 异常事件 + 日志

### Outbound 失败

- Bridge 重试 3 次失败 → Message 标 `vendor_msg_ref=null + delivery_failed_at` + emit `channel.delivery_failed`
- Cognition BC 订阅 `channel.delivery_failed`：
  - 单条偶发失败 → 写 memory，跳过
  - 同 conversation 连续失败 → Supervisor 唤醒决策

v2+ 多 channel 场景：Bridge 自动 fallback 到 conversation 的另一条 ChannelBinding。

### Conversation 状态不一致

- 出现 message 关联的 conversation_id 不存在（异常导入 / bug）→ 丢入 dead-letter 表 + 报警

---

## § 11. v1 简化总结

| 维度 | v1 简化 | 未来扩展 |
|---|---|---|
| 用户数 | 单 user identity | 多 user / 跨用户消息归属 |
| Channel 数 | 1 channel hint per conversation | 多 channel 同时送达 |
| Bridge 实现 | 仅 FeishuBridge | DingTalkBridge / WebBridge / SlackBridge / ... |
| Identity 自动绑定 | 任何 vendor 来源 → 默认 user identity | 多用户场景下 supervisor 询问归属 |
| Conversation kind | 5 种（dm / group_thread / adhoc / notification / task） | 按需新增 |
| Message content_kind | 4 种（text / system / agent_finding / supervisor_summary） | voice / image / file |
| Task ↔ Conversation 多 tracker | 1:1（一个 task 一个 conversation） | 多渠道镜像 / 多用户 tracker（[roadmap](../../../roadmap.md)；[ADR-0017 § 9](../../../decisions/0017-task-as-conversation.md) 显式驳回 v1 多 tracker） |
| 投递失败处理 | 重试 + 失败标记 | 自动 fallback 到备 channel |
| Dedupe | 单 channel 内 vendor message_id 去重；outbound 可选 `dedupe_key` | 跨 channel dedupe |

---

## § 12. 跟 ADR / 历史命名的对照

- ADR-0007：引入 Conversation 层作为渠道无关会话上下文（本文档承载具体设计）
- ADR-0009：Issue ↔ Conversation 解耦 + 外部集成走 Bridge（本文档反映重构）
- ADR-0017：Task ↔ Conversation 1:1（kind=task + Message.input_request_ref + 撤回 ADR-0016 的 task_progress）
- 旧名 `Communication Gateway` / `ChannelAdapter` → 新名 `Bridge` / `FeishuBridge`
- 旧 CLI `notify-conversation` → 新 CLI `conversation add-message`
- 旧设计 Conversation kind 含 `issue` + `context_ref` → 新设计删掉这两个
- 旧设计 IssueComment = Conversation Message → 新设计 IssueComment 是 Discussion BC 独立结构化实体

详见 [03-issue-discussion.md](../discussion/01-issue-discussion.md) 与 [09-feishu-integration.md](../bridge/01-feishu-integration.md)。
