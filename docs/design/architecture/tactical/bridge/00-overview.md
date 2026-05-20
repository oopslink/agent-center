# Bridge BC — DDD 战术设计 Overview

> **DDD 战术层** · BC: Bridge
>
> 渠道桥接层：每个 vendor（飞书 / DingTalk / Web chat / Slack / ...）一个 **Bridge** 实现，做**双向同步**：
>
> - **Outbound**：订阅领域事件 → 渲染为 vendor 格式 → 调 vendor SDK 投递
> - **Inbound**：vendor 长连接 / webhook 回调 → 调对应领域模块的 API 写入
>
> **本 BC 是 Anti-Corruption Layer（ACL）**：Bridge 是**唯一**调用 vendor SDK 的地方；其它 BC（Conversation / Discussion / TaskRuntime / Cognition）**零 vendor 依赖**（[conventions § 9.y](../../../../rules/conventions.md)）。

---

## § 0. BC 一览

### 0.1 职责

| 维度 | 内容 |
|---|---|
| **ACL 翻译** | vendor 形态（飞书消息 / 卡片 / button click）↔ 领域形态（Message / API call） |
| **双向同步** | 订阅 outbound 事件推 vendor；收 inbound vendor 事件调领域 API |
| **渲染** | 按 `content_kind + input_request_ref` 决定 vendor 形态（普通消息 / small card / rich card with buttons / update_card 置灰）|
| **三模式交互** | D1 @bot 自由文本 / D2 Slash 命令 / D3 交互卡片 |
| **审计 ledger** | 每个 Bridge 可有自己的小审计表（如 `feishu_delivery_ledger`）；**不算业务聚合** |

### 0.2 UL 切片

来自 [strategic/03-bounded-contexts § 1](../../strategic/03-bounded-contexts.md) 标 Bridge 上下文的术语：

- `Bridge`（per vendor 实现：FeishuBridge / DingTalkBridge / WebBridge / ...）
- `LarkCard`（飞书交互卡片，vendor 渲染细节；**不是 Message.content_kind**）
- 行为动词：`Deliver`（投递）/ `Render`（渲染 content_kind 为 vendor 形态）

### 0.3 Context Map 位置

[strategic/03-bounded-contexts § 3](../../strategic/03-bounded-contexts.md)：

- **Bridge ↔ vendor**：ACL / 双向同步（vendor SDK 调用 + WebSocket 长连）
- **Bridge → Discussion / Conversation / TaskRuntime**：Customer-Supplier（inbound 时调 `conversation add-message` / `task bind-conversation` / `issue bind-conversation` / `InputRequest.respond` 等）
- **Bridge ← Discussion / Conversation / TaskRuntime**：Pub/Sub（订阅 `conversation.message_added` / `conversation.opened` (kind=task/issue) / `input_request.*` 等做 outbound）
- **Bridge → Observability**：Open Host（emit `channel.delivered` / `channel.delivery_failed` / `bridge.parse_failed`）

---

## § 1. 聚合清单（X.1）

**无业务聚合** —— Bridge BC 是 ACL，不持有领域聚合。

| Bridge 实现 | 持有 |
|---|---|
| `FeishuBridge` | WebSocket 长连接状态 + `feishu_delivery_ledger`（小审计表）|
| `DingTalkBridge`（v2+） | 同结构 |
| `WebBridge`（v2+） | 同结构 |

### 1.1 内部审计表（per Bridge）

不算业务聚合，是 ACL 内部 vendor 状态 / 翻译审计：

| Bridge | 表 | 字段 |
|---|---|---|
| FeishuBridge | `feishu_delivery_ledger` | message_id, channel, vendor_msg_ref, delivered_at, retry_count, last_error |

> **跟 ADR-0020 中间方案对比**：ADR-0020 曾计划引入 `feishu_card_ledger` 表持有 card_message_id；[ADR-0021](../../../decisions/0021-issue-as-conversation.md) supersede 后取消（Bound Card 概念消失，所有 IO 统一走 Conversation root card + Message 路径，不需要 card_message_id 反查表）。

### 1.2 不持有聚合的语义

- **Conversation BC** 持 `primary_channel_thread_key` 字段 —— 不是 Bridge 的；是 Conversation 顺手存放的路由 hint（[conversation/01 § 3](../conversation/01-conversation.md) 注释）
- **Bridge 不持 vendor message 到领域实体的反查表** —— inbound 时按 (channel, thread_key) 查 Conversation；不需要 ledger
- **唯一 vendor 翻译细节存放点**：feishu_delivery_ledger 仅做 outbound 投递审计（retry / failure 跟踪）

---

## § 2. Invariants

1. **唯一 vendor SDK caller**：Bridge BC 是整个 agent-center 内唯一 `import` vendor SDK 的位置；其它 BC 代码不允许出现 vendor 包名（[conventions § 9.y](../../../../rules/conventions.md)）
2. **无业务聚合**：Bridge 不持任何领域聚合；状态权威永远在领域模块
3. **领域模块零 vendor 依赖**：没 Bridge 时领域模块仍能跑（无 IO 但状态机正常）
4. **Bridge 不主动调领域 API（除 inbound 路径）**：outbound 走事件订阅；不主动"拉"领域状态
5. **inbound 路径限 ACL 翻译**：vendor msg → 调领域 API 写入；不做领域决策（决策权在 supervisor）
6. **幂等保证**：同一 `vendor_msg_ref` 不重复写入领域（Bridge 内 dedupe）
7. **错误隔离**：Bridge 解析失败 / 投递失败不影响领域模块；仅 emit `bridge.parse_failed` / `channel.delivery_failed`

---

## § 3. Domain Services（X.3）

### 3.1 OutboundDeliveryService

**职责**：订阅领域事件 → 按 content_kind + input_request_ref 渲染 → 调 vendor SDK 投递 → 回填 vendor_msg_ref。

| 维度 | 内容 |
|---|---|
| 订阅事件 | `conversation.message_added` (direction=outbound) / `conversation.opened` (kind=task/issue) / `input_request.responded / timed_out / canceled` |
| 渲染规则 | content_kind + input_request_ref → vendor 形态（普通 text / small card / rich card with buttons / update_card 置灰），详见 [01-feishu-integration § 6](01-feishu-integration.md) |
| 重试 | 3 次指数退避；最终失败 emit `channel.delivery_failed` → Cognition BC 订阅决策 |

### 3.2 InboundRoutingService

**职责**：vendor inbound 事件 → 路由到对应 conversation → 调 `conversation add-message`。

| 维度 | 内容 |
|---|---|
| 路由 | 按 (channel, vendor_thread_key) 查 conversation；找不到则按 event context 建新 kind=dm / adhoc / group_thread |
| 跨 BC | 调 Conversation BC API（`conversation add-message`）；不直接写 Discussion / TaskRuntime |
| 例外 | kind=task / kind=issue conversation 不由此分支创建（走各自的同步建 / 懒创建路径）|
| 幂等 | 同 vendor_msg_ref 不重复写 |

> ADR-0021 后取消了原 ADR-0009 的"bound thread → 写 IssueComment 不进 Conversation"例外路径。所有 inbound 议事消息统一走 Conversation 路径。

### 3.3 SlashCommandRouter

**职责**：vendor slash 命令（如 `/answer` / `/track`）直接路由到领域 API，**不经 supervisor**（规则解析、不烧 LLM、低延迟、误解风险低）。

| 命令 | 行为 |
|---|---|
| `/answer <task_id> <text>` | Bridge 调 `InputRequest.respond` + 写 Message 留痕 |
| `/track <task_id>` | Bridge 调 `task bind-conversation --to=<当前 thread 对应 conversation>` |
| `/dispatch ...`（v2 推迟）| TBD |

详见 [01-feishu-integration § 9.1](01-feishu-integration.md)。

### 3.4 CardLifecycleService

**职责**：Conversation root card 同步建路径（task / issue create 时 emit `conversation.opened` (kind=task/issue) → Bridge 发 root card → 回写 `conversation.primary_channel_thread_key`）+ update_card 置灰（input_request 状态变化时）。

详见 [01-feishu-integration § 7.5](01-feishu-integration.md)。

---

## § 4. Factories（X.4）

**无**。Bridge 不创建领域聚合；它只调领域 API 让领域 BC 自己 Factory 创建。

---

## § 5. Repositories（X.5）

> **Bridge BC 无业务聚合**（[§ 1](#-1-聚合清单x1) 已明示）。本节列的是 Bridge ACL 内部 audit / vendor state 抽象 —— 严格说不算 DDD Repository（不持业务实体），但形态相同：领域层定接口，实现层落 vendor SDK / SQL 细节。

### 5.1 FeishuDeliveryLedgerRepository（ACL 审计）

```go
type FeishuDeliveryLedgerRepository interface {
    Append(ctx context.Context, l *FeishuDeliveryLedger) error                                              // 投递时记录
    FindByMessageID(ctx context.Context, messageID MessageID) (*FeishuDeliveryLedger, error)
    UpdateDeliveryStatus(ctx context.Context, messageID MessageID, vendorMsgRef string, status DeliveryStatus, lastError string) error
}

// Domain errors
var (
    ErrLedgerNotFound = errors.New("bridge: delivery ledger not found")
    ErrLedgerDuplicate = errors.New("bridge: ledger entry for message_id already exists")
)
```

### 5.2 VendorConnectionState（in-memory + reconnect token 持久化）

```go
type VendorConnectionState interface {
    GetConnectionStatus(ctx context.Context) ConnectionStatus                       // connected / disconnected / reconnecting
    SaveReconnectToken(ctx context.Context, token string, expiresAt time.Time) error
    LoadReconnectToken(ctx context.Context) (string, time.Time, error)
    ClearReconnectToken(ctx context.Context) error
}

// Domain errors
var (
    ErrReconnectTokenExpired = errors.New("bridge: reconnect token expired")
    ErrVendorDisconnected    = errors.New("bridge: vendor connection lost")
)
```

### 5.3 约定

- 这些都是 Bridge 内部 audit / vendor state；**不参与领域决策**
- 跟 [strategic § BC7 注释](../../strategic/03-bounded-contexts.md)"无业务表；各 Bridge 实现可有自己的小审计表"对齐
- Repository 接口在 Bridge BC 内定义（per vendor 实现）；实现层 vendor SDK / SQL schema 落到 [01-feishu-integration.md](01-feishu-integration.md) + [implementation/02-persistence-schema.md](../../../implementation/) (TBD)
- Domain errors 用 sentinel error pattern；调用方用 `errors.Is` 判定

---

## § 6. 跨聚合引用出方向（X.6）

**无聚合 → 无跨聚合引用**。Bridge 内部 ledger 表通过 `message_id` 弱引用 Conversation BC 的 Message（仅 audit，非业务）。

---

## § 7. 跨 BC 交互

### 7.1 Outbound（系统 → vendor）

订阅以下事件做 outbound：

| 事件 | 触发动作 |
|---|---|
| `conversation.message_added` (direction=outbound) | 投递到对应 conversation 的 vendor thread/dm；按 content_kind + input_request_ref 渲染 |
| `conversation.opened` (kind=task) | 发 Task root card → 回写 `primary_channel_thread_key`（[ADR-0017](../../../decisions/0017-task-as-conversation.md)） |
| `conversation.opened` (kind=issue) | 发 Issue root card → 回写 `primary_channel_thread_key`（[ADR-0021](../../../decisions/0021-issue-as-conversation.md)） |
| `input_request.responded` / `input_request.timed_out` / `input_request.canceled` | update_card 置灰 + 显示终态文案 |

### 7.2 Inbound（vendor → 系统）

```
Bridge 收 vendor 事件
   ↓
按 (channel, vendor_thread_key) 查 conversation
   ↓
找到 → 调 conversation add-message
找不到 → 建 dm / adhoc / group_thread conversation 再 add-message
   (kind=task / kind=issue 不由此分支建)
   ↓
emit conversation.message_added → 其它 BC 订阅做业务
```

详见 [01-feishu-integration § 4](01-feishu-integration.md)。

### 7.3 D1 / D2 / D3 三模式

| 模式 | 实现 | 烧 LLM？ |
|---|---|---|
| **D1. @bot + 自由文本** | inbound 走 § 7.2 → emit conversation.message_added → supervisor wake 解析意图 | ✅ |
| **D2. Slash 命令** | Bridge 直接调对应领域 API + 写 Message 留痕（§ 3.3） | ❌（规则解析）|
| **D3. 交互卡片** | Bridge 收 `card.action.trigger` → 调对应领域 API（如 `InputRequest.respond`） | ❌（一般直接路由）|

详见 [01-feishu-integration § 9](01-feishu-integration.md)。

### 7.4 Customer-Supplier 上下游汇总

| 方向 | 模式 | 内容 |
|---|---|---|
| Bridge ↔ vendor | ACL / 双向同步 | 唯一调 vendor SDK 的地方；翻译 incoming 为领域 API 调用；订阅 outbound 事件推到 vendor |
| Bridge → Discussion / Conversation / TaskRuntime | Customer-Supplier | inbound 时调领域模块 API（`conversation add-message` / `task bind-conversation` / `issue bind-conversation` / `InputRequest.respond`）；slash 命令直接路由（[ADR-0017 § 6](../../../decisions/0017-task-as-conversation.md)）|
| Bridge ← Conversation | Pub/Sub | 订阅 `conversation.message_added` / `conversation.opened` (kind=task/issue) / `input_request.*` 做 outbound |
| Observability ← Bridge | Open Host | 订阅 `channel.delivered` / `channel.delivery_failed` / `bridge.parse_failed` |

---

## § 8. Out-of-Scope / Future Work

| 项 | 归属 |
|---|---|
| 多 vendor 实现（DingTalkBridge / WebBridge / SlackBridge / ...）| [roadmap](../../../roadmap.md)（v1 仅 FeishuBridge）|
| 跨 vendor fallback（飞书失败 → DingTalk 补送）| [roadmap](../../../roadmap.md) |
| 卡片模板运行时配置（v1 硬编码在 Bridge 内）| [roadmap](../../../roadmap.md) |
| 跨 Bridge dedupe | [roadmap](../../../roadmap.md)（v1 单 vendor 没需求）|
| Bridge 升级热重启（不丢 in-flight inbound）| [roadmap](../../../roadmap.md) |
| Vendor 限流自适应 | [roadmap](../../../roadmap.md) |
| Web Console 内嵌聊天入口（WebBridge）| [roadmap](../../../roadmap.md) |

---

## § 9. References

### 相关 ADR

- [ADR-0009 § 2 Bridge 模式](../../../decisions/0009-issue-conversation-decoupled-via-bridge.md)（仅 § 2 部分仍有效，§ 1 / § 3 已 superseded）
- [ADR-0017 Task ↔ Conversation 1:1](../../../decisions/0017-task-as-conversation.md)（Bridge 渲染 kind=task root card）
- [ADR-0021 Issue ↔ Conversation 1:1](../../../decisions/0021-issue-as-conversation.md)（Bridge 渲染 kind=issue root card；取消 Bound Card / feishu_card_ledger）

### 战略层

- [strategic/03-bounded-contexts § 1 UL](../../strategic/03-bounded-contexts.md)（Bridge / LarkCard 术语）
- [strategic/03-bounded-contexts § 2 BC7 Bridge](../../strategic/03-bounded-contexts.md)
- [strategic/03-bounded-contexts § 3 Context Map](../../strategic/03-bounded-contexts.md)

### 同 BC 内 vendor-specific 实现

- [01-feishu-integration.md](01-feishu-integration.md) — FeishuBridge 完整实现（一次性设置 / 网络方向 / Inbound / Outbound / 渲染规则 / Bound Card→Conversation 创建 / 三模式 / 失败处理）

### 跨 BC 协作文档

- [conversation/00-overview.md](../conversation/00-overview.md) — Conversation BC 入口（Message + ChannelBinding + add-message API）
- [conversation/01-conversation.md](../conversation/01-conversation.md) — Conversation AR + Message content_kind 详情
- [task-runtime/00-overview.md](../task-runtime/00-overview.md) — Task / TaskExecution / InputRequest
- [task-runtime/03-input-request.md](../task-runtime/03-input-request.md) — InputRequest UI（agent_finding + input_request_ref → card with buttons）
- [discussion/00-overview.md](../discussion/00-overview.md) — Issue 创建 / conclude 流程
- [cognition/00-overview.md](../cognition/00-overview.md) — Supervisor 唤醒触发 Bridge outbound
- [observability/00-overview.md](../observability/00-overview.md) — `channel.*` / `bridge.*` 事件订阅

### 横切方法论

- [conventions § 9.y](../../../../rules/conventions.md) — 外部集成走 Bridge 模式
- [conventions § 3 AI Native](../../../../rules/conventions.md) — 不引入 MCP
- [conventions § 13 安全](../../../../rules/conventions.md) — vendor 凭据 / 长连接
