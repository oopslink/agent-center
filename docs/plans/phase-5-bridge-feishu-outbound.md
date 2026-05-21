# Phase 5: Bridge ACL Outbound（飞书）

> DDD BC: Bridge（Anti-Corruption Layer）· 依赖 Phase 1-4 · 解锁 Phase 6 (Cognition Supervisor) / Phase 7 (Bridge Inbound + 部署收尾)
> 纪律：按里程碑顺序 / 模块完备不半成品 / 单测 ≥ 90% + 集成 + e2e + 测试报告

## § 0. 目标

本 phase 把 agent-center 从「内部状态机 + events 流」第一次接到外部 vendor（飞书）。**单向 outbound**：领域 BC emit 的 `conversation.*` / `input_request.*` 事件被 Bridge 订阅后渲染为飞书 vendor 形态推过去。inbound（飞书 → 领域 API）推到 Phase 7。

DDD 意义：ACL 边界第一次落地。所有飞书 SDK 调用封进 `internal/bridge/feishu/`，领域 BC（Conversation / TaskRuntime / Discussion）继续保持**零 vendor 依赖**（[conventions § 9.y / § 4](../rules/conventions.md)），由 Bridge 通过订阅 events 表桥接外部世界。本 phase 同时落 Identity AR（vendor 用户身份 ↔ center user identity 的映射），它是 outbound 路由 + inbound 鉴权的基础（虽然 inbound 不在 phase scope，Identity 仍在本 phase 完整化以便 Phase 7 直接消费）。

用户视角：`agent-center bridge feishu setup` 一键创建飞书智能体应用 + 写配置；`agent-center identity add / list / bind / unbind` 管理 vendor 身份；`agent-center task create ...` 之后飞书侧自动收到 Task root card；agent / supervisor 写 `conversation add-message` 后飞书侧自动收到对应卡片 / 文本消息。

---

## § 1. DDD 工件清单

### 1.1 Aggregate Roots

| AR | 归属 BC | 说明 |
|---|---|---|
| **Identity** | Conversation BC（[conversation/02-identity.md](../design/architecture/tactical/conversation/02-identity.md)） | 形式化 id 字符串（`user:hayang` / `supervisor:<inv-id>` / `agent:<session-id>` / `bot`）；本 phase 落 AR + Repository + CLI manage 入口。Phase 5 是 Identity AR 的实施载体（Bridge 是其首个 vendor consumer）|

### 1.2 Entities（子从属）

| Entity | 从属 | 说明 |
|---|---|---|
| **FeishuDeliveryLedger** | Bridge BC 内部 audit 表（**不算业务聚合**；[bridge/00 § 1.1](../design/architecture/tactical/bridge/00-overview.md) / [ADR-0020 § 4 中间方案的合法部分](../design/decisions/0020-card-confined-to-bridge-bc.md) — ledger 表沿用，`feishu_card_ledger` 被 ADR-0021 删除不实施） | 记录每次 outbound 投递的状态机：pending → delivered / failed；含 `vendor_msg_ref` / `card_message_id`（飞书 interactive card 时填）+ retry / last_error |

### 1.3 Value Objects

| VO | 用在哪 | 描述 |
|---|---|---|
| **ChannelBinding** | Identity 子从属（[conversation/02-identity § 1](../design/architecture/tactical/conversation/02-identity.md)） | `{identity_id, channel, vendor_user_id, preferred, bound_at}`；Identity ↔ vendor user 的绑定 |
| **DeliveryStatus** | FeishuDeliveryLedger.status | `pending / delivered / failed`（enum，TEXT + 应用层校验）|
| **RenderedCard** | InteractiveCardRenderer 出参 → FeishuWebSocketClient 入参 | `{card_json: string, message_kind: text|interactive, idempotency_key: string}`；vendor 形态打包，无飞书 SDK 类型泄漏到 dispatcher |
| **OutboundEnvelope** | dispatcher 路由表 | `{message_id?, conversation_id, channel, thread_key?, content_kind, payload_ref}`；events 表 row → 投递路由表的中间结构 |

### 1.4 Repositories

| Repository | 接口归属 | 说明 |
|---|---|---|
| **IdentityRepository** | Conversation BC（已在 [conversation/00 § 5.3](../design/architecture/tactical/conversation/00-overview.md)）| 本 phase 实施落 SQLite。`FindByID / FindByKind / Save`|
| **ChannelBindingRepository** | Conversation BC sub-repo（已在 [conversation/00 § 5.4](../design/architecture/tactical/conversation/00-overview.md)） | `FindByIdentityID / FindByVendorUserID / Save / Delete`；`FindByVendorUserID` 是 Phase 7 inbound 反查热路径（Phase 5 预实施） |
| **FeishuDeliveryLedgerRepository** | Bridge BC（[bridge/00 § 5.1](../design/architecture/tactical/bridge/00-overview.md)） | `Append / FindByMessageID / UpdateDeliveryStatus`；ACL 审计，不持业务实体 |

### 1.5 Domain Services

| Service | 归属 BC | 职责 |
|---|---|---|
| **FeishuOutboundDispatcher** | Bridge BC | 订阅 events 表 `conversation.opened` (kind=task/issue) / `conversation.message_added` (direction=outbound) / `input_request.responded` / `.timed_out` / `.canceled` → 路由到对应 renderer → 调 FeishuWebSocketClient 投递；管 ledger |
| **InteractiveCardRenderer** | Bridge BC | 把 `(Message.content_kind, input_request_ref?)` 渲染为 `RenderedCard`；按 [bridge/01 § 6](../design/architecture/tactical/bridge/01-feishu-integration.md) 渲染规则：text → markdown / system → small card / agent_finding + input_request_ref → interactive card with buttons (join InputRequest 取 options) / supervisor_summary → rich card / conclusion_draft → rich card + 决议按钮 / task_proposal → small card |
| **FeishuWebSocketClient** | Bridge BC | vendor SDK 封装（`github.com/larksuite/oapi-sdk-go/v3`）。出栈连接 + 重连 + 调 `im.v1.messages.create`；唯一 import 飞书 SDK 的位置 |
| **IdentityRegistrationService** | Conversation BC（[conversation/00 § 3.2](../design/architecture/tactical/conversation/00-overview.md)） | 注册 Identity + 绑 ChannelBinding；本 phase 实施手动路径（CLI `identity *`），自动路径（Bridge inbound 见到新 vendor user → 自动绑）推到 Phase 7 |

### 1.6 Application Services（CLI handler 层）

| Application Service | 入口 CLI | 调用工件 |
|---|---|---|
| **IdentityCmdService** | `agent-center identity add / list / bind / unbind` | IdentityRepository + ChannelBindingRepository + EventSink |
| **BridgeFeishuSetupService** | `agent-center bridge feishu setup` | 写 `config.yaml` 的 `bridge.feishu.*` + 调 FeishuWebSocketClient 验证连接 + 初始化 ledger 表（migration 已建，仅 smoke test）|
| **FeishuOutboundService**（守护态）| `agent-center server` 启动时 spawn（goroutine） | FeishuOutboundDispatcher + FeishuWebSocketClient lifecycle |

### 1.7 Domain Events（emit 给 events 表）

| Event | emitter | 主要 payload | 用途 |
|---|---|---|---|
| `identity.registered` | IdentityRegistrationService | `{identity_id, kind, display_name}` | Phase 7 inbound 自动注册 audit |
| `identity.channel_bound` | IdentityCmdService / IdentityRegistrationService | `{identity_id, channel, vendor_user_id}` | 投递路由依据 |
| `identity.channel_unbound` | IdentityCmdService | `{identity_id, channel}` | 同上 |
| `channel.delivered` | FeishuOutboundDispatcher | `{message_id, channel, vendor_msg_ref, delivered_at}` | 成功投递；触发 Conversation BC `UpdateVendorMsgRef`（同事务） |
| `channel.delivery_failed` | FeishuOutboundDispatcher | `{message_id?, conversation_id, channel, reason, message, retry_count}` | 最终失败；Cognition BC 订阅决策（reason+message 双字段；[conventions § 16](../rules/conventions.md)） |
| `bridge.parse_failed` | （Phase 7 才 emit；Phase 5 stub 函数留口） | — | Phase 7 inbound |
| `bridge.feishu.connection_state_changed` | FeishuWebSocketClient | `{state: connected/disconnected/reconnecting, reason?, message?}` | 长连状态可观测 |

### 1.8 Context Map 关系

| 方向 | 模式 | 内容 |
|---|---|---|
| **Bridge → vendor（飞书）** | ACL | Bridge 是唯一 import 飞书 SDK 的位置；outbound vendor 调用全在此 |
| **Bridge ← Conversation / TaskRuntime / Discussion** | Pub/Sub（订阅 events 表）| 订阅 `conversation.message_added` / `.opened` / `input_request.*`；**Bridge 不调领域 BC 写 API** |
| **Bridge → Conversation** | Customer-Supplier | 投递成功后**调** `conversation update-vendor-msg-ref --id=<message_id> --ref=<vendor_msg_ref>`（应用层 facade，内部走 MessageRepository.UpdateVendorMsgRef）+ `conversation update-primary-channel --id=<conversation_id> --channel=feishu --thread-key=<...>`（root card 发出后回写 primary_channel_thread_key） |
| **Bridge → Observability** | Open Host | emit `channel.*` / `bridge.*` / `identity.*` 事件进 events 表 |
| **Bridge ↔ Bridge BC 内部 ledger** | 内部 | FeishuDeliveryLedger append + UpdateDeliveryStatus |

---

## § 2. 上游依赖（来自 Phase 1-4 的工件）

| 上游工件 | 出处 | 本 phase 哪一步用 |
|---|---|---|
| `events` 表 schema + EventRepository.Append + EventSink domain service | Phase 1 § 8.2 | § 3.5 dispatcher 订阅 events；§ 3.6 各 service emit 事件 |
| `conversations` 表 + ConversationRepository（含 `FindByID` / `FindByChannelAndThreadKey` / `UpdatePrimaryChannel`） | Phase 1（Shared Kernel）| § 3.2 ChannelBinding VO 关联；§ 3.5 dispatcher 投递成功后回写 `primary_channel_thread_key` |
| `messages` 表 + MessageRepository（含 `Append` / `UpdateVendorMsgRef` / `FindByConversationID`） | Phase 1 | § 3.5 dispatcher 渲染前 `FindByConversationID` 取 Message；投递成功后 `UpdateVendorMsgRef` |
| `task_executions` 表 + InputRequest 表（含 options） | Phase 2 | § 3.6 InteractiveCardRenderer 渲染 `agent_finding + input_request_ref != null` 时 join InputRequest 取 buttons options |
| `issues` 表 + `issue.conversation_id` | Phase 3 | § 3.5 dispatcher 处理 `conversation.opened (kind=issue)` 发 root card |
| Observability 五动词（`inspect / query / ps / stats / logs`） | Phase 4 | § 5.3 e2e 用 `inspect conversation <id>` / `query events --type=channel.*` 断言 |
| `notification.default_channel` 配置项 | Phase 1（配置框架） | § 3.7 懒创建路径 fallback 渠道 |
| BlobStore（local impl） | Phase 1 | 本 phase 不直接使用；ledger 内 last_error 字段限长（< 4KB），无大字段；预留接口 |
| `task bind-conversation` / `issue bind-conversation` CLI handler | Phase 2 / 3 | 本 phase 不调（Bridge 不主动调领域 API）；Phase 7 inbound slash 命令 `/track` 走它，本 phase 仅确认接口存在 |

---

## § 3. 工作项分解（严格按依赖顺序）

工作项排序原则：**先底层 audit / vendor SDK 封装 → 再 domain service → 再 application service / CLI**。整体路径 Identity → Ledger → vendor client → dispatcher（其中 renderer 作为 dispatcher 入参先于 dispatcher 落） → CLI → e2e。

### 3.1 Identity AR + Repository + ChannelBinding sub-repo

- 工件类型：Aggregate Root + 2 个 Repository
- Go 包路径：`internal/conversation/identity/`（AR 归 Conversation BC，跟 Phase 1 同 BC；本 phase 仅 *实施*）
- 文件：
  - `internal/conversation/identity/identity.go` — Identity struct + IdentityID typed alias
  - `internal/conversation/identity/channel_binding.go` — ChannelBinding VO struct
  - `internal/conversation/identity/repository.go` — IdentityRepository / ChannelBindingRepository interfaces（已在 conversation/00 § 5.3-5.4 定义；本 phase 落到代码）
  - `internal/conversation/identity/sqlite_repo.go` — SQLite 实现
- 输入：Phase 1 的 SQLite driver + tx-via-ctx 机制（[02-persistence § 5](../design/implementation/02-persistence-schema.md)）+ EventSink interface
- 实现步骤：
  1. 写 migration `000X_identity.up.sql` + `000X_identity.down.sql`：
     - `identities (id TEXT PK, kind TEXT NOT NULL, display_name TEXT NOT NULL, created_at TEXT NOT NULL, version INTEGER NOT NULL DEFAULT 1)`；kind 应用层 enum 校验
     - `channel_bindings (id TEXT PK, identity_id TEXT NOT NULL, channel TEXT NOT NULL, vendor_user_id TEXT NOT NULL, preferred INTEGER NOT NULL DEFAULT 0, bound_at TEXT NOT NULL, UNIQUE (channel, vendor_user_id))` —— `identity_id` 语义上引用 `identities.id`，**不声明 FK**（[conventions § 9.w](../rules/conventions.md)），完整性由 `IdentityRegistrationService.BindChannel` 在写入前校验 identity 存在
     - 索引：`idx_channel_bindings_identity`, `uniq_channel_bindings_channel_vendor_user`
  2. 实现 Repository 方法（CAS 模板见 02-persistence § 4；Identity AR 用 version + 乐观锁，ChannelBinding 是子从属可 delete row）
  3. 实现 IdentityFactory（[conversation/00 § 4.3](../design/architecture/tactical/conversation/00-overview.md)）
  4. 实现 IdentityRegistrationService.RegisterIdentity / BindChannel / UnbindChannel（手动路径）；自动路径方法签名留口但 Phase 5 不实现（Phase 7 实施 inbound 自动绑）
  5. 同事务 emit `identity.registered` / `identity.channel_bound` / `identity.channel_unbound` 进 events 表（用 EventSink）
- P8b 02-persistence-schema § 1-7 对位：贴 § 1 SQLite driver + § 2 ULID PK + § 3 TEXT 时间戳 + § 4 乐观锁 CAS + § 5 tx-via-ctx + § 6 migration FS embed
- DoD：
  - [ ] 两张表 migration up/down 都能跑（`migrate up` + `migrate down` + `migrate up` 幂等）
  - [ ] Repository 单测覆盖：`FindByID / FindByKind / Save / FindByIdentityID / FindByVendorUserID / Save / Delete` 全部路径含异常分支（ErrIdentityNotFound / ErrIdentityAlreadyExists / ErrChannelBindingNotFound / ErrChannelBindingAlreadyExists）
  - [ ] Invariants（[conversation/02 § 4](../design/architecture/tactical/conversation/02-identity.md)）有显式测试：id 不可变 / kind 不可变 / `(channel, vendor_user_id)` 唯一 / preferred 唯一 per identity
  - [ ] EventSink 在同 tx 内 Append 三类事件（用真实 SQLite `:memory:` 集成测试断言）

### 3.2 ChannelBinding 落地 + Conversation `primary_channel_thread_key` 字段

- 工件类型：VO + 现有表加字段
- Go 包路径：`internal/conversation/identity/`（ChannelBinding 在 3.1 同包）+ `internal/conversation/conversation/`（仅 1 行字段读写）
- 输入：3.1 ChannelBinding sub-repo + Phase 1 conversations 表
- 实现步骤：
  1. 确认 Phase 1 `conversations` 表已含 `primary_channel_hint TEXT` + `primary_channel_thread_key TEXT`（[conversation/01 § 字段](../design/architecture/tactical/conversation/01-conversation.md)）；缺失则写 additive migration 补
  2. 实现 ConversationRepository.UpdatePrimaryChannel（Phase 1 接口已定；本 phase 落 SQL）：
     ```sql
     UPDATE conversations
     SET primary_channel_hint = ?, primary_channel_thread_key = ?, version = version + 1, updated_at = ?
     WHERE id = ? AND version = ?
     RETURNING version;
     ```
  3. **不**在 Conversation 上加 `card_message_id` —— ADR-0020 / ADR-0021 显式拒绝（Card 归 Bridge BC ledger）
- P8b 对位：跟 task / task_executions 表 UpdateStatus 同模板（CAS + RETURNING version）
- DoD：
  - [ ] UpdatePrimaryChannel CAS 冲突路径有测试（version 不匹配返回 ErrConversationVersionConflict）
  - [ ] 仅 dispatcher (Bridge BC) 调用此方法的测试（在 Conversation BC 包外无其它 caller —— 用 `import` 关系图断言；见 § 3.8）

### 3.3 FeishuDeliveryLedger 内部表 + Repository

- 工件类型：Bridge BC 内部 audit Entity（非业务聚合）+ Repository
- Go 包路径：`internal/bridge/feishu/ledger/`
- 文件：
  - `internal/bridge/feishu/ledger/ledger.go` — FeishuDeliveryLedger struct + DeliveryStatus enum
  - `internal/bridge/feishu/ledger/repository.go` — FeishuDeliveryLedgerRepository interface（已在 [bridge/00 § 5.1](../design/architecture/tactical/bridge/00-overview.md)）
  - `internal/bridge/feishu/ledger/sqlite_repo.go` — SQLite 实现
- 输入：Phase 1 SQLite + tx
- 实现步骤：
  1. migration `000X_feishu_delivery_ledger.up.sql`：
     ```sql
     CREATE TABLE feishu_delivery_ledger (
         id              TEXT PRIMARY KEY,            -- ULID
         message_id      TEXT NOT NULL,               -- Conversation.Message.id（弱引用，audit）
         conversation_id TEXT NOT NULL,
         channel         TEXT NOT NULL,               -- 'feishu'
         thread_key      TEXT,                        -- 投递目标 thread；root card 发出时填 + Conversation 回写依据
         vendor_msg_ref  TEXT,                        -- 飞书 message_id
         card_message_id TEXT,                        -- interactive card 时的 card msg id；ADR-0020 § 4 audit
         status          TEXT NOT NULL,               -- pending / delivered / failed
         retry_count     INTEGER NOT NULL DEFAULT 0,
         last_error      TEXT,
         delivered_at    TEXT,
         updated_at      TEXT NOT NULL,
         created_at      TEXT NOT NULL,
         version         INTEGER NOT NULL DEFAULT 1
     );
     CREATE INDEX idx_feishu_ledger_message ON feishu_delivery_ledger (message_id);
     CREATE INDEX idx_feishu_ledger_status ON feishu_delivery_ledger (status) WHERE status = 'pending';
     CREATE UNIQUE INDEX uniq_feishu_ledger_message ON feishu_delivery_ledger (message_id);
     ```
  2. Repository：
     - `Append` — INSERT pending row（dispatcher 决定投递前调）
     - `FindByMessageID` — 反查（unbind / debug）
     - `UpdateDeliveryStatus` — pending → delivered (含 vendor_msg_ref) / pending → failed (含 last_error + retry_count++) CAS
  3. 错误类型 sentinel：`ErrLedgerNotFound` / `ErrLedgerDuplicate`
- P8b 对位：UPSERT 不用（每 message_id 唯一 row）；CAS UPDATE 模板同 task_executions
- DoD：
  - [ ] migration up/down 跑通
  - [ ] Repository 单测覆盖：Append 重复 message_id → ErrLedgerDuplicate；UpdateDeliveryStatus 状态非 pending → 错误；CAS 冲突
  - [ ] ledger 表 schema 不含任何 Conversation Message 字段冗余（不存 content_kind / content；仅 audit 跟 vendor 实现细节缓存）

### 3.4 FeishuWebSocketClient（vendor SDK 封装；唯一 import 飞书 SDK 处）

- 工件类型：Domain Service（vendor 封装 / port-adapter pattern）
- Go 包路径：`internal/bridge/feishu/client/`
- 文件：
  - `internal/bridge/feishu/client/client.go` — interface `FeishuClient` 定义（领域侧 port，不 import 飞书 SDK）
  - `internal/bridge/feishu/client/oapi_adapter.go` — 真实实现，`import "github.com/larksuite/oapi-sdk-go/v3"`
  - `internal/bridge/feishu/client/fake_server.go`（test 友好的 stub）— HTTP server 模拟飞书 open API，e2e 用
- 输入：`bridge.feishu.app_id` / `app_secret_file` 配置（Phase 1 配置加载框架）
- interface 设计：
  ```go
  // 领域 port（无飞书 SDK 类型泄漏）
  type FeishuClient interface {
      Connect(ctx context.Context) error                                                  // WebSocket 长连建立 + 重连
      SendTextMessage(ctx context.Context, target Target, markdown string) (SendResult, error)
      SendInteractiveCard(ctx context.Context, target Target, cardJSON string) (SendResult, error)
      UpdateCard(ctx context.Context, cardMessageID string, cardJSON string) error       // v2+ 推迟，phase 5 不实现，但 interface 留口（[bridge/01 § 11 v1 简化](../design/architecture/tactical/bridge/01-feishu-integration.md) 内说 update_card 卡片置灰由 Phase 5+ 决；本 phase 仅发新消息，update_card 留为 Phase 5 后置）
      Close() error
      ConnectionStatus() ConnectionStatus
      OnEvent(handler func(VendorEvent)) // Phase 7 inbound 用；Phase 5 注册 no-op handler
  }
  type Target struct{ Channel, ThreadKey, VendorUserID string }
  type SendResult struct{ VendorMsgRef string; CardMessageID string }
  type ConnectionStatus string  // "connected" / "disconnected" / "reconnecting"
  type VendorEvent struct{ Kind, RawJSON string }  // Phase 7 inbound 用
  ```
- 实现步骤：
  1. interface 定义 + 飞书 SDK adapter（基于 `github.com/larksuite/oapi-sdk-go/v3` 的 WebSocket client + `im.v1.messages.create` 调用）
  2. 出栈连接 + 指数退避重连（[bridge/01 § 10](../design/architecture/tactical/bridge/01-feishu-integration.md)）；状态变化 emit `bridge.feishu.connection_state_changed` 进 events 表（reason + message 双字段）
  3. 错误显式化：所有 `if err != nil` 分支要么 return err 给 caller (dispatcher)，要么 emit event；**不允许仅 log**（[conventions § 17](../rules/conventions.md)）
  4. fake_server.go（test only build tag `//go:build !integration` 或独立 package）：用 `net/http/httptest` 做飞书 open API 的 mock，可注入响应 / 故障；e2e 通过 DSN 替换飞书 endpoint 指向 fake server
  5. 单测用 fake server，**不**真连飞书；mock interface 注入到 dispatcher
- vendor SDK 零泄漏验证：`internal/bridge/feishu/client/oapi_adapter.go` 是**唯一** `import "github.com/larksuite/oapi-sdk-go"` 的源文件；其它任何 `internal/conversation/` / `internal/task-runtime/` / `internal/discussion/` 包 import 该 SDK → 构建失败（CI 加 `go list -deps ./internal/conversation/... | grep larksuite` 必须空，见 § 5.3 e2e-5）
- DoD：
  - [ ] FeishuClient interface 在 `client.go` 不 import 任何飞书 SDK 包
  - [ ] `oapi_adapter.go` 是唯一 import 飞书 SDK 的源文件（grep 自动化检查）
  - [ ] fake server stub 能模拟 happy path + 4xx/5xx + WebSocket 断开 + 重连 token 过期
  - [ ] 重连测试覆盖：连接断开 → 重试 3 次（用注入的 `clock.Clock` 跳过 backoff sleep，[conventions § 14.x](../rules/conventions.md)）→ 状态事件 emit
  - [ ] mock 注入到 dispatcher 后，dispatcher 单测无需真连任何飞书 endpoint

### 3.5 FeishuOutboundDispatcher（订阅 events → 路由 → render → push → ledger）

- 工件类型：Domain Service（订阅 events 表，长跑 goroutine）
- Go 包路径：`internal/bridge/feishu/dispatcher/`
- 文件：
  - `internal/bridge/feishu/dispatcher/dispatcher.go` — FeishuOutboundDispatcher struct + Start/Stop
  - `internal/bridge/feishu/dispatcher/routing.go` — events 表 row → OutboundEnvelope 路由
  - `internal/bridge/feishu/dispatcher/cursor.go` — 持久化 cursor（events.id 单调，新 events 表加 `bridge_cursor` 应用层维护，或 ledger 自己持 cursor row）
- 输入：3.1 IdentityRepo / 3.2 ConversationRepo + MessageRepo / 3.3 LedgerRepo / 3.4 FeishuClient interface / 3.6 InteractiveCardRenderer / Phase 1 EventRepository（QueryByTimeWindow + cursor）
- 订阅规则（[bridge/00 § 7.1](../design/architecture/tactical/bridge/00-overview.md) + [bridge/01 § 5](../design/architecture/tactical/bridge/01-feishu-integration.md)）：

  | events.event_type | 处理 |
  |---|---|
  | `conversation.opened` (kind=task) | 渲染 Task root card → 调 SendInteractiveCard → ledger append → 投递成功后 `ConversationRepository.UpdatePrimaryChannel(channel=feishu, thread_key=<vendor 返回>)` + ledger update_status delivered + emit `channel.delivered` |
  | `conversation.opened` (kind=issue) | 同上，Issue root card 模板 |
  | `conversation.message_added` (direction=outbound) | 查 Conversation.primary_channel_thread_key → 按 content_kind + input_request_ref 渲染 → SendText / SendInteractiveCard → ledger + UpdateVendorMsgRef + emit `channel.delivered` |
  | `input_request.responded` / `.timed_out` / `.canceled` | Phase 5 简化处理：**仅 emit `channel.delivered` audit**（不做 update_card 置灰；update_card v2+ 推迟，[bridge/01 § 11](../design/architecture/tactical/bridge/01-feishu-integration.md) 显式拒绝 v1）。Phase 5 仍订阅这些事件以便落 audit + 验证 dispatcher 路由表完整 |

- 实现步骤：
  1. 主循环：`select { case <-ticker.C: poll events with cursor; case <-stop: return }`；不允许 sleep / 真实网络等待（用注入的 `clock.Clock`）
  2. 路由表 `routing.go`：`map[event_type]handler`，每个 handler 是 `(ctx, EventRow) → OutboundEnvelope`，每个分支必须显式处理（包括"忽略"也必须显式 return + emit observability event `bridge.event_ignored`，不允许默认 drop —— [conventions § 17](../rules/conventions.md)）
  3. 投递 + retry：3 次指数退避（client 内已实现连接级重连；dispatcher 这层处理业务级 5xx / rate limit）；最终失败 emit `channel.delivery_failed` (reason + message 双字段)
  4. cursor 持久化：每批投递完 commit 一次（events.id 单调 ULID）
  5. 跨聚合写：投递成功后**调** `MessageRepository.UpdateVendorMsgRef` + `ConversationRepository.UpdatePrimaryChannel`（Conversation BC API，作 Customer-Supplier；不直接 SQL）；Phase 1 已有 update vendor_msg_ref 接口
- 错误隔离（[bridge/00 § 2 invariant 7](../design/architecture/tactical/bridge/00-overview.md)）：dispatcher 自身异常**不影响**领域 BC；所有失败 emit 事件可观测
- DoD：
  - [ ] 每个 event_type 路由分支有独立单测（happy path + dispatch_failure path + 渲染失败 path）
  - [ ] cursor 持久化：dispatcher 重启后从上次 cursor 续推；测试断言"重启不丢不重"
  - [ ] retry 3 次后失败 emit `channel.delivery_failed { reason, message }`（reason 枚举 `connect_lost / rate_limit / 4xx_permanent / 5xx_exhausted / render_failed`；message 人话）
  - [ ] dispatcher 不调任何领域 BC 写 API 除 `MessageRepository.UpdateVendorMsgRef` / `ConversationRepository.UpdatePrimaryChannel`（这两个是合法应用层 facade，**不算**业务 mutation）；用导入图测试断言（见 § 5.3 e2e-5）

### 3.6 InteractiveCardRenderer（content_kind + input_request_ref → vendor 形态）

- 工件类型：Domain Service（无状态 pure function）
- Go 包路径：`internal/bridge/feishu/renderer/`
- 文件：
  - `internal/bridge/feishu/renderer/renderer.go` — Renderer interface + 实现
  - `internal/bridge/feishu/renderer/templates/` — 卡片模板 JSON（硬编码，[bridge/01 § 11 v1 简化](../design/architecture/tactical/bridge/01-feishu-integration.md) 卡片模板硬编码不支持运行时模板）
- 输入：Message struct + 可选 InputRequest（含 options[]）+ Conversation kind（root card 时用）
- 渲染规则（按 [bridge/01 § 6](../design/architecture/tactical/bridge/01-feishu-integration.md)）：

  | (content_kind, input_request_ref?, conversation kind) | 输出 |
  |---|---|
  | `text`, —, — | text/markdown message |
  | `system`, —, — | small interactive card（含简短文本 + 标签）|
  | `agent_finding`, null, — | text message + agent 标签 |
  | `agent_finding`, **≠ null**, — | **rich interactive card with buttons**（核心场景；join InputRequest 取 options → 渲染 `[选项 A][B][自己写][取消]` 按钮）|
  | `supervisor_summary`, —, — | rich interactive card with action buttons（confirm / change / abandon）|
  | `conclusion_draft`, —, — | rich interactive card（Issue conclude flow；[确认结论][改后确认][不做] 按钮；[ADR-0021 § 10](../design/decisions/0021-issue-as-conversation.md)）|
  | `task_proposal`, —, — | small card（议事中草案条目）|
  | `conversation.opened`, —, kind=task | Task root card（含 task_id / title / status badge）|
  | `conversation.opened`, —, kind=issue | Issue root card（同模板，标签 Issue #N）|

- 实现步骤：
  1. 每个 content_kind 一个 renderer function；返回 `RenderedCard{card_json, message_kind, idempotency_key}`
  2. 模板 JSON 用 `text/template` 渲染；不引入飞书 SDK 类型（renderer 包**不 import** 飞书 SDK，仅产出 raw JSON 字符串给 client）
  3. `agent_finding + input_request_ref != null` 时：dispatcher 把 InputRequest（来自 TaskRuntime BC `InputRequestRepository.FindByID`）传入；renderer 把 `options[]` 平铺为 buttons + 加 `[自己写][取消]` 兜底按钮
  4. button payload 编码：`{action: "input_request_respond", input_request_id, option_id}` —— Phase 7 inbound 解析（本 phase 仅发，不收）
  5. 异常输入显式拒绝：未知 content_kind → 返回 ErrUnknownContentKind（dispatcher 转 `bridge.parse_failed` audit；不 silently fallback 为 text，[conventions § 17 / § 14.x](../rules/conventions.md)）
- DoD：
  - [ ] 每个 content_kind 至少一个单测（含 happy / 异常 input 路径）
  - [ ] `agent_finding + input_request_ref` 测试覆盖 option 数 1 / 3 / 10 / 0（空 options 时 fallback 仅 `[自己写][取消]`）
  - [ ] renderer 包 import 关系图断言：不 import 飞书 SDK；不 import dispatcher
  - [ ] 卡片 JSON 输出快照测试（`testdata/renderer/*.json`）

### 3.7 CLI handlers（identity * / bridge feishu setup）

- 工件类型：Application Service + CLI handler
- Go 包路径：`internal/cmd/identity/` + `internal/cmd/bridge/`
- 文件：
  - `internal/cmd/identity/add.go` / `list.go` / `bind.go` / `unbind.go`
  - `internal/cmd/bridge/feishu_setup.go`
- 输入：3.1 IdentityRepo / ChannelBindingRepo / IdentityRegistrationService / 3.4 FeishuClient（用于 setup 时 smoke test 连接）
- CLI 签名（[03-cli-subcommands § 8.6](../design/implementation/03-cli-subcommands.md)）：

  | 命令 | flag / arg | 行为 |
  |---|---|---|
  | `identity add <identity_id> --kind=user|supervisor|agent|bot --display-name=...` | identity_id 形式化字符串（如 `user:hayang`）| 调 IdentityFactory + Repository.Save + emit `identity.registered` |
  | `identity list [--kind=user|supervisor|agent|bot]` | optional kind filter | 查 IdentityRepository.FindByKind / 全表扫；输出 table / `--format=json` |
  | `identity bind <identity_id> --channel=feishu --vendor-user-id=<...> [--preferred]` | — | 调 ChannelBindingRepository.Save + emit `identity.channel_bound` |
  | `identity unbind <identity_id> --channel=feishu` | — | 调 ChannelBindingRepository.Delete + emit `identity.channel_unbound` |
  | `bridge feishu setup [--app-id=...] [--app-secret-file=...]` | flag 缺省走交互式（v1 简化：必填 flag，交互式留 v2+）| 写 `bridge.feishu.{enabled,app_id,app_secret_file}` 到 config.yaml + 调 FeishuClient.Connect smoke test + 如成功 emit `bridge.feishu.connection_state_changed{state=connected}`；失败明示 reason + message |

- 实现步骤：
  1. 用 Phase 1 的 cobra / urfave/cli 框架挂子命令（[03-cli-subcommands § 2.1](../design/implementation/03-cli-subcommands.md)）
  2. `bridge feishu setup` 调用流程：
     - 校验 `--app-id` 非空、`--app-secret-file` 存在可读
     - 写 config.yaml（用 atomic rename 防中断坏文件；[conventions § 17](../rules/conventions.md)）
     - 创建 FeishuClient + Connect → 验证连接（30s timeout，用注入 clock）
     - 失败显式 emit `bridge.feishu.connection_state_changed{state=disconnected, reason=..., message=...}`；exit non-zero
     - 成功打印 "feishu bridge enabled, app_id=cli_abc..."；exit 0
  3. CLI handler 单测：mock Repository + mock FeishuClient；断言 events emit / config 写入 / 输出格式
- DoD：
  - [ ] `--help` 输出跟 03-cli-subcommands § 8.6 一字不差
  - [ ] `identity add` 重复 id → ErrIdentityAlreadyExists → CLI exit 2 + 友好错误消息
  - [ ] `identity bind` 重复 (channel, vendor_user_id) → ErrChannelBindingAlreadyExists → CLI exit 2
  - [ ] `bridge feishu setup --app-id=X --app-secret-file=/missing` → exit 2 + "app secret file not found"
  - [ ] `bridge feishu setup` 成功 / 失败两条路径都 emit `bridge.feishu.connection_state_changed`（reason + message）

### 3.8 端到端验证（领域 BC 包 import 测试 + e2e flow）

- 工件类型：测试 + boot wiring
- Go 包路径：`tests/e2e/` + `internal/server/wiring.go`（spawn dispatcher 进 server goroutine）
- 实现步骤：
  1. `agent-center server` boot 流程加：若 `bridge.feishu.enabled == true` → spawn FeishuOutboundDispatcher 作 goroutine；启动失败 fail-fast
  2. 优雅关闭：SIGTERM → dispatcher.Stop() 等待当前批次投递完成 → FeishuClient.Close()
  3. 编写 `tests/e2e/feishu_outbound_test.go`：
     - setup：tmpdir + SQLite + fake feishu server（3.4 fake_server.go）+ spawn agent-center server 子进程（CLI invoke）
     - 跑 e2e 路径 1（task create → root card）：
       - `agent-center identity add user:test --kind=user --display-name="Test"` 
       - `agent-center identity bind user:test --channel=feishu --vendor-user-id=ou_test --preferred`
       - `agent-center bridge feishu setup --app-id=test --app-secret-file=./testdata/secret`
       - `agent-center task create proj-1 "test task"`（同事务建 task + conversation + emit task.created + conversation.opened）
       - 等待 dispatcher 处理（用 Phase 4 `query events --type=channel.delivered --since=...` 轮询，**用注入 clock + done channel，不 sleep**）
       - 断言：fake feishu server 收到 SendInteractiveCard 调用 + payload 含 Task #N title；ledger 表有 status=delivered row；Conversation.primary_channel_thread_key 已回填
     - 跑 e2e 路径 2（input_request emit → interactive card with buttons）：
       - 已有 task；spawn task_execution；agent 模拟调 `request-input --options=A,B,C`（Phase 2 InputRequest 接口）
       - 同事务写 InputRequest + emit `input_request.created` + 写 Message (agent_finding, input_request_ref=...) + emit `conversation.message_added`
       - 断言：fake feishu server 收到 SendInteractiveCard，card_json 含 4 个 buttons（A / B / C / [自己写]）
     - 跑 e2e 路径 3（issue open → Issue root card）：
       - `agent-center issue open --title="test issue" --channel-from-thread=...`（Phase 3 issue open 同步建路径）
       - 断言：fake feishu server 收到 Issue root card；Conversation kind=issue 的 primary_channel_thread_key 回填
  4. 加 `tests/internal/import_graph_test.go`（独立 build tag）：
     - 用 `go list -deps ./internal/conversation/...` / `./internal/task-runtime/...` / `./internal/discussion/...` / `./internal/cognition/...`
     - 断言：依赖图中**不含** `github.com/larksuite/oapi-sdk-go` 任何子包
     - 断言：仅 `internal/bridge/feishu/client/oapi_adapter.go` import 飞书 SDK
- DoD：
  - [ ] 3 条 e2e flow 在 fake feishu server 下 reliable 跑通（无 flaky；用 done channel / 注入 clock）
  - [ ] 导入图测试通过：领域 BC 包零飞书 SDK 依赖
  - [ ] `agent-center server` 启动时 dispatcher 真的注册 events 订阅（用 `query events --type=bridge.feishu.connection_state_changed` 验证 boot 顺序）
  - [ ] `task create` 后端到端 latency p99 < 3s（fake server）

---

## § 4. Definition of Done（整体）

- [ ] § 1 所有工件实现并通过单元测试（Identity AR + ChannelBinding VO + FeishuDeliveryLedger Entity + 3 个 Repository + 4 个 Domain Service + 3 个 Application Service）
- [ ] § 5 所有测试场景通过（unit + 集成 + e2e）
- [ ] 单测行覆盖率 ≥ 90%（diff + 整体；按 [testing.md § 1](../rules/testing.md)，CI 阻断 < 90% 合并）
- [ ] 测试报告归档到 `docs/plans/reports/phase-5-test-report.md`，§ 5 计划项 1:1 对位
- [ ] 触发的 domain event 全部进 events 表（集成测试验证）：`identity.registered / .channel_bound / .channel_unbound / channel.delivered / channel.delivery_failed / bridge.feishu.connection_state_changed`
- [ ] CLI 命令 `--help` 跟 [03-cli-subcommands § 8.6](../design/implementation/03-cli-subcommands.md) 对齐
- [ ] **vendor SDK 零泄漏**（[conventions § 9.y / § 4](../rules/conventions.md)）：仅 `internal/bridge/feishu/client/oapi_adapter.go` import 飞书 SDK；其它源文件 grep 全空；导入图自动化测试通过
- [ ] 项目本地 lint + `go vet` + `go test ./...` 全过
- [ ] § 6 风险项要么处理要么显式 defer 到具体后续 phase（不能"待定"）
- [ ] errors 不吞（[conventions § 17](../rules/conventions.md)）：所有 `if err != nil` 分支要么 emit event / 改状态 / return err，**不允许仅 log**
- [ ] reason + message 双字段（[conventions § 16](../rules/conventions.md)）：`channel.delivery_failed` / `bridge.feishu.connection_state_changed` 等带 reason 字段同时携带 message

---

## § 5. 测试计划

### 5.1 单测场景（按工件分类）

| # | 工件 | 测试场景 | 关键断言 |
|---|---|---|---|
| U1 | Identity AR | 创建新 Identity（4 kind 各一） | Save 成功 + emit `identity.registered`；id 不可变性 |
| U2 | Identity AR | 重复 id Save | ErrIdentityAlreadyExists |
| U3 | Identity AR | FindByKind 多 kind 过滤 | 仅返回匹配 kind 的 Identity |
| U4 | Identity AR | kind 不可变性 | 试图改 kind → 应用层拒绝 |
| U5 | ChannelBinding | Save 新绑定 | binding 入表 + emit `identity.channel_bound` |
| U6 | ChannelBinding | 重复 (channel, vendor_user_id) | ErrChannelBindingAlreadyExists |
| U7 | ChannelBinding | preferred 唯一 per identity | 第二个 preferred=1 → 应用层拒绝或自动 demote 前一个 |
| U8 | ChannelBinding | Delete + FindByVendorUserID | binding 删除后 FindByVendorUserID 返回 ErrChannelBindingNotFound |
| U9 | ChannelBinding | 自动注册路径方法签名留口 | Phase 5 不实现，编译通过 |
| U10 | FeishuDeliveryLedger | Append pending | row 入表，status=pending |
| U11 | FeishuDeliveryLedger | Append 重复 message_id | ErrLedgerDuplicate |
| U12 | FeishuDeliveryLedger | UpdateDeliveryStatus pending→delivered | status=delivered, vendor_msg_ref 写入, delivered_at 非空 |
| U13 | FeishuDeliveryLedger | UpdateDeliveryStatus pending→failed | status=failed, retry_count++, last_error 写入 |
| U14 | FeishuDeliveryLedger | CAS 冲突（并发 update）| 第二次 update 返回 version conflict |
| U15 | FeishuWebSocketClient | Connect happy | ConnectionStatus → "connected"；emit state_changed |
| U16 | FeishuWebSocketClient | Connect 失败 4xx | err return + emit state_changed{reason="auth_failed", message=...} |
| U17 | FeishuWebSocketClient | 断线重连指数退避 | 注入 clock；3 次 retry 间隔 1s/2s/4s；最终失败 emit |
| U18 | FeishuWebSocketClient | SendTextMessage happy | fake server 收到正确 payload；返回 vendor_msg_ref |
| U19 | FeishuWebSocketClient | SendInteractiveCard happy | 同上 + card_message_id |
| U20 | FeishuWebSocketClient | Send 时连接断开 | err return（不 silently retry，由 dispatcher 决定）|
| U21 | InteractiveCardRenderer | content_kind=text | markdown text message |
| U22 | InteractiveCardRenderer | content_kind=system | small card |
| U23 | InteractiveCardRenderer | content_kind=agent_finding + input_request_ref=null | text + agent label |
| U24 | InteractiveCardRenderer | content_kind=agent_finding + input_request_ref（3 options） | interactive card with 5 buttons (3+ [自己写][取消]) |
| U25 | InteractiveCardRenderer | content_kind=agent_finding + input_request_ref（0 options） | interactive card with 2 buttons ([自己写][取消]) |
| U26 | InteractiveCardRenderer | content_kind=supervisor_summary | rich card with action buttons |
| U27 | InteractiveCardRenderer | content_kind=conclusion_draft | rich card + [确认][改][不做] buttons |
| U28 | InteractiveCardRenderer | content_kind=task_proposal | small card |
| U29 | InteractiveCardRenderer | conversation.opened kind=task | Task root card |
| U30 | InteractiveCardRenderer | conversation.opened kind=issue | Issue root card |
| U31 | InteractiveCardRenderer | unknown content_kind | ErrUnknownContentKind（不 silently fallback） |
| U32 | InteractiveCardRenderer | renderer 包不 import 飞书 SDK | 编译期断言 |
| U33 | FeishuOutboundDispatcher | route conversation.opened (kind=task) | 调 SendInteractiveCard + ledger append + UpdatePrimaryChannel + emit channel.delivered |
| U34 | FeishuOutboundDispatcher | route conversation.opened (kind=issue) | 同上，Issue 模板 |
| U35 | FeishuOutboundDispatcher | route conversation.message_added (outbound, text) | SendTextMessage + ledger + UpdateVendorMsgRef |
| U36 | FeishuOutboundDispatcher | route conversation.message_added (outbound, agent_finding + input_request_ref) | SendInteractiveCard with buttons |
| U37 | FeishuOutboundDispatcher | route input_request.responded | Phase 5 仅 audit；不 update_card；emit channel.delivered |
| U38 | FeishuOutboundDispatcher | route input_request.timed_out | 同上 |
| U39 | FeishuOutboundDispatcher | route input_request.canceled | 同上 |
| U40 | FeishuOutboundDispatcher | unknown event_type | 显式 emit `bridge.event_ignored` 不 silently drop |
| U41 | FeishuOutboundDispatcher | retry 3 次后失败 | emit channel.delivery_failed{reason, message}, retry_count=3 |
| U42 | FeishuOutboundDispatcher | render 失败 | emit channel.delivery_failed{reason="render_failed"} |
| U43 | FeishuOutboundDispatcher | cursor 持久化 + 重启 | 重启后从 cursor 续推；无重复投递（用 message_id 唯一性兜底） |
| U44 | FeishuOutboundDispatcher | Conversation 不存在 | emit `bridge.routing_failed`；不 crash |
| U45 | CLI identity add | happy | identity 入表 + emit |
| U46 | CLI identity add | 重复 id | exit 2 + 错误消息 |
| U47 | CLI identity list | --kind filter | 仅匹配 kind |
| U48 | CLI identity bind | happy | binding 入表 + emit |
| U49 | CLI identity bind | 未知 identity_id | exit 2 + ErrIdentityNotFound |
| U50 | CLI identity unbind | happy | row 删除 + emit |
| U51 | CLI identity unbind | binding 不存在 | exit 2 + ErrChannelBindingNotFound |
| U52 | CLI bridge feishu setup | happy | config.yaml 写入 + Connect smoke 通过 + emit connected |
| U53 | CLI bridge feishu setup | app_secret_file 不存在 | exit 2 + 友好错误 |
| U54 | CLI bridge feishu setup | Connect 失败 | exit non-zero + emit disconnected{reason, message} |
| U55 | CLI bridge feishu setup | atomic config 写入中断 | 不破坏原 config.yaml |

### 5.2 集成测试场景

| # | 场景 | 涉及工件 | 关键断言 |
|---|---|---|---|
| I1 | events 表订阅 → dispatcher routing → fake vendor 收到 + ledger 写入（同事务）| EventRepository + FeishuOutboundDispatcher + FeishuClient(fake) + LedgerRepo + ConversationRepo | events row 进表 → cursor 推进 → SendXxx 调用 → ledger status=delivered → UpdatePrimaryChannel 完成 |
| I2 | dispatcher + Conversation `UpdatePrimaryChannel` 回写竞态 | dispatcher × 2 instance | 同 conversation 仅 1 个回写成功（CAS）；另一个观察到 update + skip |
| I3 | Identity 同事务双写 events | IdentityRegistrationService + EventSink | identity row + identity.registered event 在同 tx |
| I4 | ChannelBinding 同事务双写 events | ChannelBindingRepo + EventSink | 同上 |
| I5 | FeishuClient 断线重连 + 期间 dispatcher 排队 | FeishuClient + Dispatcher | 重连后 dispatcher 续推 cursor；无丢失 |
| I6 | FeishuDeliveryLedger UpdateDeliveryStatus 状态机 | LedgerRepo | pending→delivered 通过；delivered→failed 拒绝（状态机非法跃迁）|
| I7 | events.refs JSON shape 跟 dispatcher routing 表对齐 | EventRepository.Append + Dispatcher | dispatcher 能从 refs 取 conversation_id / message_id / input_request_id |
| I8 | `bridge feishu setup` 写 config + 启动 server + dispatcher up | CLI + boot wiring | server log 含 "feishu bridge enabled"；events 含 connected 事件 |
| I9 | 大量 events 批处理（100 条 conversation.message_added）| Dispatcher | 全部投递；ledger 全部 delivered；cursor 推进到 last id |
| I10 | dispatcher 关闭：处理中 batch 提交后再关 | Dispatcher.Stop | 关闭前 in-flight 投递完成，cursor 持久化；重启不重不丢 |
| I11 | Phase 4 `inspect conversation <id>` 看得到 primary_channel_thread_key | ObservabilityQuery + ConversationRepo | UpdatePrimaryChannel 后 inspect 输出含 thread_key |
| I12 | Phase 4 `query events --type=channel.delivered` 走 events 表索引 | EventRepository.QueryByTimeWindow | 返回正确 events；按 type 索引命中 |

### 5.3 e2e 测试场景

| # | 场景 | 用户视角 / 入口 CLI | 关键断言 |
|---|---|---|---|
| E1 | task create → 自动建 conversation → emit `conversation.opened` → Bridge 推飞书 root card → 回填 primary_channel_thread_key | `agent-center task create proj-1 "test"` | fake server 收 SendInteractiveCard 模板 = Task root card；Conversation.primary_channel_thread_key != null；ledger 1 row status=delivered |
| E2 | InputRequest emit → 飞书侧收到 interactive card with buttons | `agent-center request-input --task=T-X --options=A,B,C`（agent 视角；Phase 2 接口）| fake server 收 card_json 含 5 buttons (A/B/C/自己写/取消)；ledger 1 row |
| E3 | issue open（飞书源同步建路径） → Bridge 发 Issue root card | `agent-center issue open --title="..." --from-conversation=<conv>` | fake server 收 SendInteractiveCard 模板 = Issue root card；Conversation kind=issue thread_key 回填 |
| E4 | conversation add-message (supervisor outbound) → 飞书侧收到 markdown 文本 | `agent-center conversation add-message --id=<conv> --content="..." --content-kind=text --direction=outbound` | fake server 收 SendTextMessage；ledger 1 row；Message.vendor_msg_ref 回填 |
| E5 | vendor SDK 零泄漏：导入图测试 | `go list -deps ./internal/conversation/... ./internal/task-runtime/... ./internal/discussion/... ./internal/cognition/...` | 输出**不含** `github.com/larksuite/oapi-sdk-go`；`grep -rn larksuite internal/ \| grep -v 'internal/bridge/feishu/client/oapi_adapter.go'` 输出为空 |
| E6 | 投递失败重试 3 次后 emit channel.delivery_failed | fake server 强制 503 持续返回 | dispatcher retry 3 次（用注入 clock 跳 backoff）→ emit channel.delivery_failed{reason="5xx_exhausted", message="..."}；ledger status=failed retry_count=3 |
| E7 | `bridge feishu setup --app-id=X --app-secret-file=...` happy flow | CLI | config.yaml 写入；启动 `agent-center server` 后 dispatcher 注册成功；query events 看得到 connected 事件 |
| E8 | server 优雅关闭：dispatcher 处理完 in-flight batch 再退出 | SIGTERM | exit 0；events 流末尾有 disconnected{reason="shutdown"} |
| E9 | identity * CLI 全套流程 | `add → list → bind → unbind` | 每步 emit 对应 event；list 输出对齐 |

---

## § 6. 风险 / Spike 项

1. **Identity AR 归属 vs Phase 5 实施载体**
   - 风险：Identity AR 战略归属 Conversation BC（[conversation/00 § 1.1](../design/architecture/tactical/conversation/00-overview.md) + [conversation/02-identity.md](../design/architecture/tactical/conversation/02-identity.md)）。Phase 5 第一个 vendor consumer 是 Bridge，所以 Phase 5 实施它。
   - 缓解：包路径放 `internal/conversation/identity/`（按 BC 归属）；本 phase 仅实施它的 Repository + Service + CLI；自动注册路径（Bridge inbound 见到新 vendor user → auto-bind）方法签名留口，**Phase 7 落地**。文档在 § 1.1 显式说明。

2. **update_card 推迟到 v2+**
   - 风险：[bridge/01 § 11 v1 简化](../design/architecture/tactical/bridge/01-feishu-integration.md) 说 v1 不上 update_card；但 input_request.responded / timed_out / canceled 事件按设计应"update_card 置灰"（[bridge/00 § 3.4 / bridge/01 § 5](../design/architecture/tactical/bridge/00-overview.md)）。
   - 处置：Phase 5 不实现 update_card；input_request.* 三事件 dispatcher 仍订阅但**仅做 audit emit `channel.delivered`**（route 表完整 / 无 unknown event 漏报）。**显式 defer 到 v2+ ADR**：在 [bridge/01 § 11](../design/architecture/tactical/bridge/01-feishu-integration.md) 补一行 "update_card v1 不上"；在 phase-5 报告 § 4 已知问题列此条 + 拍 Phase 7+ 跟进。

3. **events 表订阅 cursor 持久化策略**
   - 风险：dispatcher 是 events 表的第一个真实订阅者；cursor 怎么存（events 表加列 / bridge 自有表 / KV）Phase 1 设计未明示。
   - 处置：本 phase 在 `bridge/feishu/dispatcher/cursor.go` 内决定（独立表 `bridge_subscription_cursors (subscriber TEXT PK, last_event_id TEXT, updated_at TEXT)`）。**不**改 events 表 schema（[conventions § 9.1 additive](../rules/conventions.md) 不动主表）。Phase 6 Supervisor 订阅 events 表时可复用同表（PK = subscriber name）。

4. **同事务跨 BC 写**
   - 风险：dispatcher 投递成功后调 ConversationRepository.UpdatePrimaryChannel + MessageRepository.UpdateVendorMsgRef + LedgerRepo.UpdateDeliveryStatus + EventRepository.Append(channel.delivered) —— **4 个 BC 表跨 tx**，违反 [conventions § 9.z BC 物理隔离](../rules/conventions.md)？
   - 处置：**不**算违反 —— § 9.z 禁的是"跨 BC 共享物理表 / partial-column update"；这里每个 BC 各自表，dispatcher 作为 application service 跨 BC 调用是合法 customer-supplier 模式。但为避免 4 表锁导致 SQLite WAL 阻塞，**拆 tx**：
     - Tx1: LedgerRepo.UpdateDeliveryStatus + EventRepository.Append（Bridge BC 自己同 tx）
     - Tx2: ConversationRepository.UpdatePrimaryChannel + MessageRepository.UpdateVendorMsgRef（Conversation BC 内部同 tx）
   - 两 tx 失败补偿：Tx2 失败 → dispatcher 标 ledger.status=delivered_but_callback_failed + retry 一次；二次失败 → emit `bridge.callback_failed` 警告（不影响领域状态，仅 audit 不一致）。

5. **fake feishu server fidelity**
   - 风险：fake server 跟真飞书行为不一致 → 集成测试通过但生产挂。
   - 处置：Phase 5 仅做 outbound + 3 类 API（SendText / SendInteractiveCard / WebSocket connect 握手）；fake server 用真实 OpenAPI schema 文档（飞书官方）的子集；e2e Phase 7 收尾时跑一次**真飞书 sandbox** 烟囱测试（标 spike，不阻断 Phase 5 DoD，但 Phase 7 前必须验）。

6. **`agent_finding + input_request_ref` 渲染需要跨 BC join**
   - 风险：renderer 渲染 button options 时需读 TaskRuntime BC `InputRequestRepository.FindByID` —— 跨 BC 读 OK（[conventions § 9.z](../rules/conventions.md) 允许跨 BC API 调用），但要保证 InputRequest 在 Message 写入 emit 之前已落库。
   - 处置：[ADR-0017 § 5](../design/decisions/0017-task-as-conversation.md) 已说 InputRequest 与 Message 同事务双写；dispatcher 处理 `conversation.message_added` 时 InputRequest 必然已落库。测试场景 U24 / E2 显式断言。

7. **vendor SDK go module 体积 / 编译时间**
   - 风险：`larksuite/oapi-sdk-go/v3` 体积 / 依赖很大。
   - 处置：本 phase 接受（v1 单 vendor 唯一选择）；用 `go mod why` / `go build -ldflags` 验证二进制大小；超过 P8b 约定阈值（如 80MB）开 spike Phase 7 优化（按子包细化 import 或换轻量 client）。

---

## § 7. 下游解锁

本 phase 完成后解锁：

- **Phase 6 Cognition Supervisor**
  - 提供 surface：`channel.delivery_failed` events（Supervisor 订阅决策；单条偶发 → memory；连续失败 → 唤醒决策）；Identity AR + ChannelBinding（Supervisor 主动 push 时取 preferred channel）
  - 不解锁：update_card（Supervisor 写 supervisor_summary 后想置灰旧卡片 v2+ 实现）

- **Phase 7 Bridge Inbound + 部署收尾**
  - 提供 surface：
    - Identity AR + ChannelBindingRepository.FindByVendorUserID（inbound 反查 vendor user → center identity）
    - IdentityRegistrationService.RegisterIdentity / BindChannel（自动注册路径方法签名已在本 phase 留好，Phase 7 仅添加 inbound caller）
    - FeishuWebSocketClient.OnEvent 注入点（本 phase 注册 no-op handler；Phase 7 注册真实 inbound handler）
    - FeishuDeliveryLedger.FindByMessageID（slash 命令 `/answer` 留痕 + audit 反查）
    - Conversation BC API（`conversation add-message --direction=inbound`）— 不归本 phase，但本 phase 通过同 BC dispatcher 验证了同事务双写工作；Phase 7 inbound 直接调
    - SlashCommandRouter / D1 自由文本路由 / D3 button click 路由 —— Phase 7 实施，本 phase 渲染器输出的 button payload 编码（`{action: input_request_respond, input_request_id, option_id}`）就是 Phase 7 inbound 解析依据

- **接口冻结清单**（[plans README § 2.1](README.md) "Phase 完成后冻结该 phase 工件接口"）：
  - IdentityRepository / ChannelBindingRepository 方法签名
  - FeishuDeliveryLedgerRepository 方法签名
  - FeishuClient interface（领域 port，不 import vendor SDK）
  - events emit 的 7 个事件 type + payload shape
  - CLI 5 条命令 `--help` 输出

后续 phase 只能在这些接口上**扩列**（加方法 / 加字段），不能改语义 / 删列。
