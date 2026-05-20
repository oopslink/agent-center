# Phase 7: Bridge Inbound + 部署收尾

> DDD Bridge BC（ACL inbound 完整化）+ 运维 / 部署 · 依赖 Phase 1-6 · 解锁 v1 GA
> 纪律：按里程碑顺序 / 模块完备不半成品 / 单测 ≥ 90% + 集成 + e2e + 测试报告

## § 0. 目标

**Bridge 完整化（inbound）**：让 FeishuBridge 形成"领域 ↔ 飞书"双向闭环 —— Outbound 已在 Phase 5 走通，本 phase 把 inbound（vendor → 系统）补齐。FeishuInboundRouter 把飞书消息按 (DM / @bot / group thread / slash 命令 / 交互卡片) 分类路由到领域 BC API；slash 命令直达领域层不烧 LLM；@bot 自由文本写 inbound Message + 触发 supervisor wake 走 [ADR-0017 § 6 D2 模式](../design/decisions/0017-task-as-conversation.md)。这套补齐后，[ADR-0021 § 1 / § 10](../design/decisions/0021-issue-as-conversation.md) 中"飞书用户 @bot 提需求 → Conversation thread 形成 → supervisor 决策 → Task / Issue 自创建"的完整路径才真正可执行。

**部署收尾**：把 [implementation/06-deployment.md](../design/implementation/06-deployment.md) 设计的 systemd unit / 备份脚本 / 升级流程从文档落到 `contrib/` 仓内交付物。bootstrap playbook 实际跑一次，验证 [ADR-0018](../design/decisions/0018-detached-agent-via-per-execution-shim.md) 的"daemon restart 不杀 shim"在真实 systemd `KillMode=process` + `setsid` 配置下生效。

**E2E harness + v1 release checklist**：跨 phase 端到端的可重放场景库 —— fake feishu server + fake agent CLI + 真 SQLite + 真 BlobStore（temp dir），可在 CI 重放"用户 @bot 提需求 → supervisor wake → task create → worker 跑 → 进度回流 → InputRequest → /answer → 完成"完整链路。最后产出 `docs/plans/reports/v1-release-checklist.md`，把 Phase 1-7 的覆盖率 / 关键测试用例 / 已知 issue / 部署演练状态汇总成可签发的 release artifact。

**DDD 意义**：本 phase 是 ACL 闭合 + Open Host（运维入口）落地 —— 领域 BC 在 Phase 1-4 已经独立可跑（无 vendor 依赖），Phase 5-6 把 outbound + 决策建好，Phase 7 把 inbound 这条最后通道补齐，整个 8-BC blueprint 进入 v1 GA 状态。

## § 1. DDD 工件清单

### 1.1 Aggregate Roots

**无新增**。本 phase 不引入新聚合 —— Bridge BC 本就无业务聚合（[bridge/00 § 1](../design/architecture/tactical/bridge/00-overview.md)），所有领域决策仍由 Phase 2-6 已建的聚合做。

### 1.2 Entities

**无新增**。InboundMessageBuffer / VendorEventEnvelope 是函数内瞬态值，不是 Entity。

### 1.3 Value Objects（Bridge BC 内）

| VO | 用途 | 来源 |
|---|---|---|
| `VendorEvent` | 飞书 SDK 事件归一化封装（`{kind ∈ message_receive / card_action_trigger, vendor_msg_ref, vendor_thread_key, vendor_user_id, raw_payload}`）| 本 phase 新增 |
| `SlashCommand` | 解析后的 slash 命令（`{verb ∈ track/answer/dispatch, args[], raw}`）| 本 phase 新增 |
| `RouteDecision` | inbound 分流结果（`{kind ∈ direct_add_message / wake_supervisor / slash_route / card_callback / drop_unknown, target_conversation_id?, reason, message}`）| 本 phase 新增 |
| `CardActionEvent` | 卡片按钮点击归一化（`{card_message_id, action_value, vendor_user_id, input_request_id?}`）| 本 phase 新增 |

均无身份；`==` 即等价。

### 1.4 Repositories

**无新增聚合 → 无新增 Repository**。复用：

- 已有 `FeishuDeliveryLedgerRepository`（Phase 5 落地）— 本 phase 仅 audit inbound 不写
- 已有 `VendorConnectionState`（Phase 5）— inbound WebSocket 收到事件后路由
- 跨 BC 写入复用 Phase 2-4 暴露的 ConversationRepository / TaskRepository / InputRequestRepository / IssueRepository（**调它们的 Application Service，不直连 Repository**，保 BC 边界）

### 1.5 Domain Services

| Service | 类型 | 职责 | 文件位置 |
|---|---|---|---|
| `FeishuInboundRouter` | Bridge BC Domain Service | 解析 vendor event → 派 `RouteDecision` → 调对应路径（add-message / wake / slash / card） | `internal/bridge/feishu/inbound_router.go` |
| `FeishuInboundIdentityResolver` | Bridge BC Domain Service | vendor_user_id → center identity；未绑则按 v1 规则自动绑到当前 user identity（[conversation/00 § 3.2](../design/architecture/tactical/conversation/00-overview.md)）；多 user 路径推 v2 | `internal/bridge/feishu/identity_resolver.go` |
| `SlashCommandParser` | Bridge BC Domain Service（纯函数）| 字符串 → `SlashCommand`；`/track <id>` / `/answer <ireq_id> <choice>` / `/dispatch ...`（stub）| `internal/bridge/feishu/slash_parser.go` |
| `SlashCommandRouter` | Bridge BC Domain Service | `SlashCommand` → 调领域 BC Application Service + 写留痕 Message（同事务）| `internal/bridge/feishu/slash_router.go` |
| `FeishuInteractiveCardCallback` | Bridge BC Domain Service | `card.action.trigger` → 解析 action_value → 调 `InputRequest.respond` 或其它对应路径 + 写留痕 Message | `internal/bridge/feishu/card_callback.go` |
| `FeishuInboundDedup` | Bridge BC Domain Service | 按 `vendor_msg_ref` dedupe，防 SDK 重投递（[bridge/00 § 2 Invariant 6](../design/architecture/tactical/bridge/00-overview.md)）| `internal/bridge/feishu/inbound_dedup.go` |

### 1.6 Application Services（CLI handler 层）

| Application | CLI 命令 | 来源 |
|---|---|---|
| `BridgeFeishuSetup` | `agent-center bridge feishu setup`（已在 Phase 5 stub，本 phase 完整化）| [03-cli § 8.6](../design/implementation/03-cli-subcommands.md) |
| `IdentityAdd` / `IdentityList` / `IdentityBind` / `IdentityUnbind` | `identity ...`（已在 Phase 5 走通基础；本 phase 不动）| 同上 |
| `BackupRun` | `agent-center admin backup [--out=...]`（systemd timer 调）| [06 § 6.2](../design/implementation/06-deployment.md) 落代码 |

Inbound 路径**不直接对应 CLI 命令** —— inbound 是 WebSocket 长连内部回调，跨 BC 写入复用 `conversation add-message` / `task bind-conversation` / `InputRequest.respond` 等 Phase 2-4 已建 Application Service。

### 1.7 Domain Events

本 phase **不引入新 event type**，复用：

| 事件 | 触发位置（新） | 用途 |
|---|---|---|
| `bridge.parse_failed { reason, message, vendor_event_kind }` | InboundRouter 解析失败 | observability + Cognition 订阅 |
| `bridge.inbound_routed { route_decision, conversation_id?, target_action }` | Router 成功分流（debug-level）| audit |
| `bridge.slash_command_received { verb, args, vendor_user_id }` | SlashCommandRouter 入口 | audit |
| `bridge.slash_command_rejected { reason, message }` | slash 参数 / 权限 / 状态校验失败 | observability |
| `bridge.card_action_received { card_action, vendor_user_id, input_request_id? }` | CardCallback 入口 | audit |
| `bridge.identity_auto_bound { vendor_user_id, identity_id }` | InboundIdentityResolver 自动绑定 | audit |

跨 BC 写动作仍 emit 对应 BC 自己的事件（`conversation.message_added` / `task.conversation_bound` / `input_request.responded` 等），由各 BC Application Service 同事务写出，**Bridge 不绕过 BC 直接 emit**。

### 1.8 Context Map 关系

| 方向 | 模式 | 用法（本 phase 新激活的部分）|
|---|---|---|
| FeishuBridge ↔ 飞书 SDK | ACL | inbound 路径首次落地：WebSocket 推回事件 → Bridge 翻译 |
| FeishuBridge → Conversation | Customer-Supplier | inbound 调 `conversation add-message`；slash `/track` 调 `task bind-conversation` |
| FeishuBridge → TaskRuntime | Customer-Supplier | slash `/answer` 调 `InputRequest.respond`；card callback 同 |
| FeishuBridge → Discussion | Customer-Supplier | @bot 自由文本 wake supervisor → 由 supervisor 决定调 `issue open` 等（Bridge 不直接调）|
| FeishuBridge → Cognition | Pub/Sub | @bot 自由文本时写 Message → events 表 → WakeScheduler 派 `conversation:C` scope（[cognition § 3.2](../design/architecture/tactical/cognition/00-overview.md)），Bridge 不需特殊触发 |
| FeishuBridge → Observability | Open Host | emit `bridge.*` 事件 |

---

## § 2. 上游依赖

| 上游工件 | 来自 | 本 phase 何处使用 |
|---|---|---|
| `FeishuOutboundService` + Bridge 共享 SDK client / Renderer | Phase 5 | inbound + outbound 共用同一 WebSocket / 同一凭据 / 同一 vendor_msg_ref 字典 |
| `FeishuDeliveryLedgerRepository` | Phase 5 | inbound 测试 setUp 时 reuse ledger 表结构（不写 inbound 行；仅借迁移）|
| `VendorConnectionState` | Phase 5 | inbound 启动前确认 connection_status=connected |
| `FeishuIdentityRegistrationService` 接口 | Phase 5 | InboundIdentityResolver 调用（Phase 5 已建 stub，本 phase 完整自动绑定路径）|
| Conversation BC `ConversationApplicationService.AddMessage` | Phase 1（Shared Kernel）| Router DM / @bot / group_thread 普通消息直接走它 |
| Conversation BC `ConversationFactory`（kind=dm/group_thread/adhoc） | Phase 1 | inbound 找不到 conversation 时建 |
| TaskRuntime `TaskApplicationService.BindConversation` | Phase 2 | slash `/track` 调用 |
| TaskRuntime `InputRequestApplicationService.Respond` | Phase 2 | slash `/answer` + card callback 调用 |
| Discussion `IssueApplicationService` | Phase 3 | supervisor 决定后调（Bridge 不直接调）|
| Observability events 表 | Phase 4 | bridge.* 事件落库 |
| WakeScheduler + supervisor invocation 链路 | Phase 6 | @bot 自由文本 wake；card_action_received 不 wake（slash / card 是规则路径不烧 LLM）|
| systemd `KillMode=process` 设计 / shim setsid | Phase 2（落 shim 代码）+ [ADR-0018](../design/decisions/0018-detached-agent-via-per-execution-shim.md)（设计）| § 3.7 升级演练验证 |
| backup 设计 / SQLite WAL checkpoint | [06 § 6](../design/implementation/06-deployment.md) | § 3.6 脚本落地 |

---

## § 3. 工作项分解（严格按依赖顺序）

### 3.1 FeishuInboundIdentityResolver

- **工件类型**：Domain Service
- **包路径**：`internal/bridge/feishu/identity_resolver.go`
- **接口签名**（草案）：
  ```go
  type FeishuInboundIdentityResolver interface {
      Resolve(ctx context.Context, vendorUserID string) (identity.ID, error)
  }
  
  var (
      ErrNoUserIdentity      = errors.New("bridge: no user identity to bind")
      ErrAmbiguousUserIdentity = errors.New("bridge: multiple user identities; cannot auto-bind")
  )
  ```
- **输入**：vendor_user_id（飞书 open_id）；依赖 `ChannelBindingRepository`（Phase 1）+ `IdentityRepository`（Phase 1）
- **输出**：center `identity_id`；未绑则按 v1 规则自动绑到唯一 user identity + emit `bridge.identity_auto_bound` + emit `identity.channel_binding_added`（由 Identity BC 同事务 emit，本 service 不自己 emit identity.* —— 保 BC 边界）
- **实现步骤**：
  1. 接收 `vendor_user_id`
  2. 查 `ChannelBindingRepository.FindByChannel(channel='feishu', vendor_user_id)` → 命中 → 返回 identity_id
  3. 未命中：
     - 查询所有 `kind=user` identity（v1 单用户，应只有 1 条）
     - 若 = 1：调 `IdentityApplicationService.AddChannelBinding(identity_id, channel='feishu', vendor_user_id)`（同事务）；Bridge 这边只 emit `bridge.identity_auto_bound`
     - 若 = 0：emit `bridge.parse_failed { reason='no_user_identity', message='cannot bind: no user identity exists' }`；返回 `ErrNoUserIdentity`
     - 若 > 1：emit `bridge.parse_failed { reason='ambiguous_user', message='cannot auto-bind: N user identities found' }`；返回 `ErrAmbiguousUserIdentity`（v2 引入交互式 enroll）
  4. **并发互斥**：两条 inbound 同时撞首次自动绑 → 依赖 `ChannelBindingRepository.Add` 的 UNIQUE (channel, vendor_user_id) 约束；冲突时 retry 一次回到 step 2
- **对位**：[bridge/00 § 1.1 内部审计](../design/architecture/tactical/bridge/00-overview.md) + [conversation § 3.2](../design/architecture/tactical/conversation/00-overview.md) "v1 自动注册 + 自动绑定 ChannelBinding"
- **可测性**：依赖通过构造函数注入（`NewResolver(channelBindingRepo, identityAppService, eventSink, clock)`）；不持有具体 SDK
- **DoD**：
  - [ ] 三分支（已绑 / 自动绑 / 拒绝）单测覆盖；事件正确 emit
  - [ ] 并发场景：两条 inbound 同时撞首次自动绑 → UNIQUE 约束上挡掉一个 + retry 走入"已绑"分支（用真实 SQLite）
  - [ ] Sentinel error 用 `errors.Is` 可识别（不是仅 string match）
  - [ ] 单测行覆盖率 ≥ 90%

### 3.2 SlashCommandParser

- **工件类型**：Domain Service（纯函数）
- **包路径**：`internal/bridge/feishu/slash_parser.go`
- **输入**：raw message text
- **输出**：`SlashCommand{verb, args}` 或 `nil`（非 slash）；解析失败 → `error`（带 reason）
- **实现步骤**：
  1. 文本不以 `/` 开头 → 返回 `nil, nil`（非 slash，让 Router 走 @bot / 普通消息路径）
  2. 切 token：`/track T-42` → `verb=track, args=["T-42"]`；`/answer I-7 B` → `verb=answer, args=["I-7","B"]`
  3. 校验 verb 在白名单：`track / answer / dispatch`；未知 verb → `error{reason=unknown_slash_verb}`
  4. arg 数量基本校验（`track` 1 arg / `answer` 2 args / `dispatch` ≥ 2 args）；不够 → `error{reason=insufficient_args, message="usage: /xxx ..."}`
  5. 返回结构体；语义校验（如 task_id 是否存在）由下游 SlashCommandRouter 做
- **对位**：[bridge/01 § 9.1](../design/architecture/tactical/bridge/01-feishu-integration.md) + [ADR-0017 § 6](../design/decisions/0017-task-as-conversation.md)
- **DoD**：
  - [ ] 表驱动单测：白名单覆盖 + 各 verb 正负样本 + 边界（多空格 / 中文混排 / 转义引号 / 跨行）
  - [ ] 纯函数，无 IO 依赖；覆盖率 100%

### 3.3 SlashCommandRouter

- **工件类型**：Domain Service
- **包路径**：`internal/bridge/feishu/slash_router.go`
- **输入**：`SlashCommand` + `RouteContext{ identity_id, current_conversation_id?, vendor_thread_key, raw_msg_ref }`
- **输出**：调对应领域 BC Application Service + 同事务写一条 留痕 Message + emit `bridge.slash_command_received`
- **实现步骤（按 verb）**：
  - **`/track <task_id>`**：
    1. 解析 task_id；调 `TaskApplicationService.FindByID`
    2. 不存在 → 回 ephemeral 提示（不进 Conversation 留痕）+ emit `bridge.slash_command_rejected { reason=task_not_found }`
    3. 存在但当前 thread 没对应 conversation → 走 Phase 1 ConversationFactory 建 dm / group_thread / adhoc conversation 再 bind；同 [bridge/01 § 7.5.2](../design/architecture/tactical/bridge/01-feishu-integration.md)
    4. 调 `TaskApplicationService.BindConversation(task_id, channel=feishu, to=conversation_id)`（同事务）
    5. 调 `ConversationApplicationService.AddMessage(conversation_id, kind=text, direction=inbound, sender=identity_id, content="/track T-42")` 留痕
    6. emit `bridge.slash_command_received`
  - **`/answer <input_request_id> <choice>`**：
    1. 调 `InputRequestApplicationService.FindByID`；不存在 / 状态非 waiting → reject + ephemeral
    2. 调 `InputRequestApplicationService.Respond(input_request_id, identity_id, choice)`（同事务）
    3. 同事务写一条 留痕 Message 到 task.conversation_id：`kind=text, direction=inbound, content="/answer I-7 B"`
    4. emit `bridge.slash_command_received`；下游 input_request.responded 由 InputRequest BC emit；Bridge outbound 订阅会 update_card 置灰（Phase 5 已建）
  - **`/dispatch ...`**：v1 stub —— 回 ephemeral："dispatch via slash 推迟到 v2；当前请用 @bot 自由文本"；emit `bridge.slash_command_rejected { reason=feature_deferred }`
- **错误处理**：所有 reject 路径不污染 Conversation 留痕（仅 vendor 侧 ephemeral 回复）；同 [bridge/01 § 9.1 错误处理](../design/architecture/tactical/bridge/01-feishu-integration.md)
- **DoD**：
  - [ ] 每个 verb 正常 + 各异常分支单测
  - [ ] 集成测试：真 SQLite + fake 飞书 SDK，验证留痕 Message 落库 + 跨 BC 状态推进
  - [ ] 单测行覆盖率 ≥ 90%

### 3.4 FeishuInteractiveCardCallback

- **工件类型**：Domain Service
- **包路径**：`internal/bridge/feishu/card_callback.go`
- **输入**：飞书 SDK `card.action.trigger` event → 归一化为 `CardActionEvent`
- **输出**：按 action_value 路由到 `InputRequest.respond`（最常见）或其它（v2 扩展）+ 写留痕 Message
- **实现步骤**：
  1. 反解 action_value 取出 `input_request_id` + `choice`（Phase 5 OutboundRenderer 渲染按钮时已埋）
  2. resolver 查 identity_id（复用 § 3.1）
  3. 调 `InputRequestApplicationService.Respond(input_request_id, identity_id, choice)`
  4. 同事务写一条 留痕 Message：`kind=text, direction=inbound, sender=identity, content="<choice>", input_request_ref=<id>`
  5. emit `bridge.card_action_received`
- **幂等**：同一 (card_message_id, action_value, vendor_user_id) 在 input_request 已 responded 时 → 静默 ack（避免重复 SDK 回调 / 用户多点）；emit `bridge.card_action_received { reason=already_responded }` debug-level
- **对位**：[ADR-0017 § 5](../design/decisions/0017-task-as-conversation.md) + [bridge/01 § 9 D3 模式](../design/architecture/tactical/bridge/01-feishu-integration.md)
- **DoD**：
  - [ ] 单测覆盖：正常响应 / 重复点击 / input_request 已 timed_out / 已 canceled / 已 responded 各分支
  - [ ] 集成测试：fake 飞书 card.action 事件 → InputRequest 状态推进 → outbound update_card 触发（链 § 3.8 e2e harness）
  - [ ] 行覆盖率 ≥ 90%

### 3.5 FeishuInboundRouter（顶层分流）

- **工件类型**：Domain Service（Bridge BC 的入口 service，所有 vendor inbound 唯一入口）
- **包路径**：`internal/bridge/feishu/inbound_router.go`
- **接口签名**（草案）：
  ```go
  type FeishuInboundRouter interface {
      // OnVendorEvent 由 WS 长连回调调用；返回的 RouteDecision 是 audit-only
      // （Router 已经在内部执行了路由动作）
      OnVendorEvent(ctx context.Context, ev VendorEvent) (RouteDecision, error)
  }
  
  type RouteDecisionKind int
  const (
      RouteDecisionDirectAddMessage RouteDecisionKind = iota + 1
      RouteDecisionWakeSupervisor   // 隐式：写完 Message 后由 WakeScheduler 自动 wake
      RouteDecisionSlashRoute
      RouteDecisionCardCallback
      RouteDecisionDropDedupe
      RouteDecisionDropUnknown
  )
  ```
- **输入**：飞书 SDK `im.message.receive_v1` / `card.action.trigger` 等事件 → 归一化为 `VendorEvent`
- **输出**：`RouteDecision` + 执行（调 § 3.1-3.4 之一）
- **实现步骤**：
  1. `FeishuInboundDedup.SeenBefore(vendor_msg_ref)` → 已见 → 静默 drop + emit debug-level `bridge.inbound_dedupe_drop`；返回 `RouteDecisionDropDedupe`
  2. `FeishuInboundIdentityResolver.Resolve(vendor_user_id)` → 拿 identity_id；失败按 § 3.1 规则 emit + 返回 error
  3. 事件 kind 分流：
     - `card.action.trigger` → § 3.4 CardCallback；返回 `RouteDecisionCardCallback`
     - `im.message.receive_v1`：
       - 文本以 `/` 开头 → `SlashCommandParser.Parse`：
         - 解析成功 → § 3.3 SlashCommandRouter；返回 `RouteDecisionSlashRoute`
         - 解析失败 → reject + ephemeral；emit `bridge.slash_command_rejected`
       - 否则（DM / @bot / group thread 普通文本）：
         a. 按 (channel='feishu', vendor_thread_key) 查 conversation
         b. 找不到则按 event context 建（kind=dm / adhoc / group_thread）：
            - 来源是 DM → kind=dm
            - 来源是群里有 thread_id → kind=group_thread
            - 来源是群里 @bot 无 thread → kind=adhoc
         c. 调 `ConversationApplicationService.AddMessage(..., direction=inbound, sender=identity_id, vendor_msg_ref=...)`
         d. emit `conversation.message_added` 由 Conversation BC 同事务 emit
         e. **Phase 6 WakeScheduler 自动按 `conversation:C` scope wake supervisor**，Bridge 不需额外触发；返回 `RouteDecisionDirectAddMessage`（即使 wake 是隐式发生的）
  4. 未知事件 kind → emit `bridge.parse_failed { reason='unknown_event_kind', message }`（[conventions § 17](../rules/conventions.md) 未知协议显式上报）；返回 `RouteDecisionDropUnknown`
- **关键不变式**：
  - 不在 Router 内做"决策"（如不自己判断"这是要开 task 吗"）—— 决策权在 supervisor（[bridge/00 § 2 Invariant 5](../design/architecture/tactical/bridge/00-overview.md)）
  - Slash 命令路径**不**触发 wake（[ADR-0017 § 6](../design/decisions/0017-task-as-conversation.md)）—— 实现上是因为 slash 不写 Message direction=inbound + content_kind=text（它写的是留痕，sender=user_via_slash，被 [cognition § 3.2 白名单](../design/architecture/tactical/cognition/00-overview.md) 区分），或者更精确地：Slash 路径写的 Message 也会 emit `conversation.message_added` 但 supervisor 在 wake 后第一时间识别 "/answer" / "/track" prefix 直接结束（这条 wake spike 在 Phase 6 已决策；Phase 7 仅遵守）
  - Card callback 路径**不**触发 wake（D3 模式直接路由）
  - Router 内部不持任何状态（除 InboundDedup 短期缓存）；可水平扩缩（v2 多 instance）
- **错误处理**（[conventions § 17](../rules/conventions.md)）：
  - 每个 step 都有 explicit decision：emit event / 改状态 / 返回 err / panic（不允许吞）
  - panic 隔离：顶层 `defer recover()` → emit `bridge.parse_failed { reason='panic', message }` → 不传到 WS 长连让连接断
- **对位**：[bridge/01 § 4 inbound 流程图](../design/architecture/tactical/bridge/01-feishu-integration.md) + [bridge/00 § 3.2 InboundRoutingService](../design/architecture/tactical/bridge/00-overview.md)
- **可测性**：依赖全部 interface（dedup / resolver / parser / slashRouter / cardCallback / convAppService / eventSink / clock）；Spin 时全部 mock；fake 飞书 server 把 VendorEvent 注入 Router 的 OnVendorEvent
- **DoD**：
  - [ ] 单测覆盖所有 RouteDecision 分支（dm 新建 / dm 已存在 / group thread / adhoc / @bot / slash 各 verb / card callback / 未知事件 / 重复 dedupe）
  - [ ] 异常路径单测：vendor_msg_ref 重复 / identity 未绑 / conversation 不存在 / 解析 panic 隔离
  - [ ] 行覆盖率 ≥ 90%；分支覆盖率报告输出

### 3.6 systemd unit + install 脚本

- **工件类型**：交付物（不是 Go 代码）
- **路径**：
  - `contrib/agent-center.service`（系统级）
  - `contrib/agent-center-worker.service`（user 级）
  - `contrib/agent-center-backup.service` + `contrib/agent-center-backup.timer`
  - `contrib/install.sh`（VPS 端一键安装）
  - `contrib/install-worker.sh`（worker 端一键安装）
- **实现步骤**：
  1. 把 [06 § 4.1 / § 4.2](../design/implementation/06-deployment.md) ini 文件 1:1 抄到 `contrib/`
  2. 写 install.sh：useradd + mkdir 目录 + install binary + install service + daemon-reload + enable
  3. 写 install-worker.sh：user 目录创建 + 写 config.yaml 模板 + bootstrap-token prompt + user systemd enable
  4. `KillMode=process` 在 worker service 中**强校验**：写入 `agent-center bootstrap --check-systemd` 命令，启动时读取 `~/.config/systemd/user/agent-center-worker.service` 验证 KillMode=process；缺则报错（防 [ADR-0018](../design/decisions/0018-detached-agent-via-per-execution-shim.md) 失效）
- **对位**：[06 § 4 / § 10 全文](../design/implementation/06-deployment.md)
- **DoD**：
  - [ ] 5 个 contrib 文件齐全 + 内容对照设计文档
  - [ ] install.sh / install-worker.sh shellcheck 0 warning
  - [ ] CI 在 Linux runner 上 `bash install.sh --dry-run` 通过

### 3.7 backup script + systemd timer

- **工件类型**：Go 代码 + shell 脚本 + systemd 文件
- **包路径**：`internal/admin/backup/`（Go）+ `contrib/agent-center-backup` shell wrapper + `contrib/agent-center-backup.timer`
- **实现步骤**：
  1. Go 实现 `BackupRun(ctx, opts)` —— `SELECT * FROM ...` 不可行（大库），改走 `sqlite3 ... "PRAGMA wal_checkpoint(FULL)"` + `cp` 文件级备份（[06 § 6.2](../design/implementation/06-deployment.md)）
  2. CLI 入口 `agent-center admin backup [--dest=...] [--retention-days=30]`
  3. shell wrapper 调 CLI；timer 每日 03:00 触发
  4. 保留 N 天清理 + 失败时 emit `admin.backup_failed { reason, message }`
- **对位**：[06 § 6.2 / § 6.3](../design/implementation/06-deployment.md)
- **DoD**：
  - [ ] 单测：mock 文件系统跑 backup → 验证 wal_checkpoint 调用 + 文件生成 + 旧文件清理
  - [ ] 集成测试：真实 SQLite + 真 tmp 目录跑完整 backup → restore 流程
  - [ ] timer 在 CI runner 上 `systemd-analyze verify` 通过

### 3.8 E2E harness 完整化

- **工件类型**：测试基础设施（Go test 内）
- **路径**：`tests/e2e/`（顶层目录，跨 phase 共享）
- **组成**：
  - `tests/e2e/fakeserver/feishu/` —— 完整 fake 飞书服务器
    - HTTP 端：`POST /open-apis/im/v1/messages` / `PATCH .../update` 等必需 API（按 [bridge/01 § 5 outbound 投递](../design/architecture/tactical/bridge/01-feishu-integration.md)）
    - WebSocket 端：模拟 [bridge/01 § 3](../design/architecture/tactical/bridge/01-feishu-integration.md) "Center 主动出站建 WS" 路径
    - 事件注入 API：测试代码 `feishu.Inject(VendorEvent{...})` → fake server 通过 WS push 给 Bridge
    - 投递断言 API：`feishu.AwaitOutbound(filter, timeout)` → 阻塞等 Bridge send 出来的消息（不 sleep；用 chan）
  - `tests/e2e/fakeagent/` —— fake agent CLI binary（`cmd/fakeagent/main.go`）
    - 接受脚本输入：`--script=scenario1.jsonl`，按行输出（每行带 `delay_ms` 字段控制释放时机，但 delay 用 fake clock 控制，不真 sleep）
    - 支持调 `agent-center request-input` / `report-artifact` / `conversation add-message`（通过 worker daemon unix socket）
    - env 注入异常：`FAKEAGENT_FAIL_AT=step_3` / `FAKEAGENT_HANG=true` 等
  - `tests/e2e/harness/` —— driver
    - `func Spin(t *testing.T) *Harness`：拉起 server / worker / fake feishu，all-in-one
    - 真实临时 SQLite（每个 test 独立 tmp dir）+ 真实临时 BlobStore（local 实现，tmp dir）
    - `harness.SeedFixture(...)`：写测试前置数据（user identity / project / worker 等）
    - `harness.Events()` 返回 EventStream 用于显式同步（`stream.AwaitType("task.done", refs, timeout)`）
    - shutdown：tx 优雅退出 + tmp 清理
  - `tests/e2e/scenarios/` —— 场景库（见 § 5.3 表）；每个场景一个 _test.go 文件
- **关键约束**：
  - 真 SQLite + 真 BlobStore（temp dir）—— 与 [testing.md § 4](../rules/testing.md) "BlobStore 契约测试" 对齐
  - **不 sleep**：harness 用 events 表 polling + 显式同步原语（chan / cond）等"事件到达"；超时上限统一可注入 clock
  - fake feishu 暴露 channel 让测试**主动注入 inbound 事件** + **assert outbound 投递**
  - 关闭 LLM SDK（[conventions § 4](../rules/conventions.md)）—— supervisor 在 e2e 也用 fake agent CLI 模拟决策
  - **fakeagent 即 fake supervisor**：[conventions § 4](../rules/conventions.md) "零 LLM SDK 依赖" → supervisor 也是 spawn agent CLI；e2e 里 supervisor scope spawn 的就是 fakeagent（不同 script）
  - 失败保留 artifact：`go test -keep-tmp` 时保留临时目录便于排查
- **可重用性**：Phase 1-6 也应该可以用本 harness 回放各自的 e2e；本 phase 提供基础设施 + 4 个跨 phase 场景；前几个 phase 的 e2e 场景可逐步迁入（spike，不阻塞本 phase）
- **DoD**：
  - [ ] harness 可在 `go test ./tests/e2e/...` 跑通；CI runner 单 binary 内嵌
  - [ ] § 5.3 四个跨 phase 场景全跑通
  - [ ] 单 case 平均运行时间 < 30s；不 flaky（连续 30× 通过率 ≥ 99.5%）
  - [ ] fakeagent 可执行；有 `cmd/fakeagent/README.md` 描述脚本格式
  - [ ] fake feishu server 协议 fixture 跟真实 SDK API 一致（手工 smoke test 跑一次比对）

### 3.9 v1 release checklist 撰写

- **工件类型**：文档
- **路径**：`docs/plans/reports/v1-release-checklist.md`
- **结构**：
  ```
  # v1 Release Checklist
  
  ## § 1. Phase 完成度汇总
  | Phase | 覆盖率 (overall / diff) | 测试用例数 | 已知 issue | 完成日期 / commit SHA |
  
  ## § 2. 关键 e2e 路径状态
  - [ ] worker enroll → dispatch → 完成 / 失败 / 取消
  - [ ] feishu @bot → supervisor wake → task → 完成 → 回流
  - [ ] InputRequest 完整往返（agent → 卡片 → /answer → 续）
  - [ ] worker daemon restart 不杀 shim 实测
  - [ ] center restart 不丢请求实测
  
  ## § 3. 部署演练记录
  - 时间 / 演练者 / VPS spec / 实际跑通的命令清单 / 遇到的问题
  
  ## § 4. 部署交付物清单
  - contrib/*.service / install.sh / backup script / 文档链接
  
  ## § 5. 待 release 阻塞 issue（必须清零）
  
  ## § 6. v2 已 defer 清单（不阻塞 release，参考 roadmap）
  ```
- **DoD**：
  - [ ] 表格全部填实（不留 TBD）；阻塞 issue 清零
  - [ ] 链接到各 phase 测试报告 1:1 对应
  - [ ] 部署演练至少一次完整跑通（VPS install + worker enroll + feishu setup + 跑一个 task → 完成）

---

## § 4. Definition of Done（整体）

- [ ] § 1 所有 Domain Service 实现并通过单元测试
- [ ] § 3.1-3.5 inbound 链路完整：DM / @bot / slash / card 四路径 e2e 跑通
- [ ] § 3.6-3.7 contrib 交付物齐全；install.sh 在干净 Ubuntu VPS 上一次跑通
- [ ] § 3.8 e2e harness 在 CI 跑绿；跨 phase 4 场景全 pass
- [ ] § 3.9 release checklist 100% 填实
- [ ] § 5 所有测试场景通过（unit + 集成 + e2e + 升级演练）
- [ ] 单测行覆盖率 ≥ 90%（diff + 整体）
- [ ] 测试报告 `docs/plans/reports/phase-7-test-report.md` 归档；条目跟 § 5 1:1 对齐
- [ ] 触发的 domain event 实际进 events 表（集成测试验证）
- [ ] CLI 命令 `--help` 跟 [03-cli § 8.6 / § 8.8](../design/implementation/03-cli-subcommands.md) 对齐
- [ ] 项目本地 lint + go vet + go test ./... 全过
- [ ] `bash contrib/install.sh --dry-run` / shellcheck 全过
- [ ] `systemd-analyze verify contrib/*.service contrib/*.timer` 全过
- [ ] § 6 风险项要么处理要么显式 defer 到 v2 roadmap（不能"待定"）

---

## § 5. 测试计划

### 5.1 单测场景

| 工件 | 用例 | 关键断言 |
|---|---|---|
| FeishuInboundIdentityResolver | 已绑 / 未绑 + 自动绑成功 / 未绑 + 0 user identity / 未绑 + >1 user identity | identity_id 正确 / event emit / fail-fast |
| FeishuInboundIdentityResolver | 并发首次绑（两 goroutine 撞）| 仅 1 个 binding；CAS / unique constraint 上挡 |
| SlashCommandParser | `/track T-42` / `/answer I-7 B` / `/dispatch x` / 非 slash / 未知 verb / 不够 args / 过多 args / 中英文混排 / 多空格 | verb / args 解析正确；错误带 reason+message |
| SlashCommandRouter `/track` | task 存在 + 当前 thread 有 conv / task 存在 + 无 conv（自建 dm）/ task 不存在 / task 已 done | 调用 TaskApplicationService.BindConversation 参数正确；留痕 Message 写入；reject 走 ephemeral |
| SlashCommandRouter `/answer` | input_request waiting / responded / timed_out / canceled / 不存在 | 仅 waiting 调 Respond；其它 reject |
| SlashCommandRouter `/dispatch` | 永远 reject | emit `slash_command_rejected { reason=feature_deferred }` |
| FeishuInteractiveCardCallback | 正常响应 / 同 button 重复点击 / 已 responded / 已 timed_out / 已 canceled / action_value 解析失败 | 状态推进正确；重复幂等 |
| FeishuInboundRouter | DM 新建 / DM 已存在 / @bot 群里新 group_thread / @bot 群里已 thread / adhoc / slash 各 verb / card callback / dedupe drop / 未知事件 kind / parse panic 隔离 | RouteDecision 字段正确；下游调用 mock 验证 |
| FeishuInboundDedup | 首次 / 重复 / dedup 窗口失效 | 幂等 |
| BackupRun | 正常 / wal_checkpoint 失败 / 拷贝失败 / 旧文件清理 / dest 不存在 | 全路径覆盖 |
| install.sh shellcheck | 无 warning | static check pass |

### 5.2 集成测试场景

| # | 场景 | 涉及工件 | 关键断言 |
|---|---|---|---|
| I-1 | fake 飞书发 DM 消息（新 vendor_user） | InboundRouter + IdentityResolver + Conversation Factory | identity 自动绑 + conversation 自建 + message 入库 + events 表有 `conversation.message_added` + `bridge.identity_auto_bound` |
| I-2 | fake 飞书发 @bot 群消息（新 group_thread）| InboundRouter + Conversation Factory | kind=group_thread conversation 建；message 入库 |
| I-3 | fake 飞书发 `/track T-42`（task 存在 + 当前 thread 无 conv）| SlashCommandRouter + ConversationFactory + TaskApplicationService | task.conversation_id 回写；conversation 建；留痕 Message 写入 |
| I-4 | fake 飞书发 `/answer I-7 B`（input_request waiting）| SlashCommandRouter + InputRequestApplicationService | input_request.status → responded；留痕 Message 写入 |
| I-5 | fake 飞书发 card.action.trigger（button choice=B）| InteractiveCardCallback + InputRequestApplicationService | 同 I-4；额外 message.input_request_ref 设置正确 |
| I-6 | 同 vendor_msg_ref 重发两次 | InboundDedup | 第二次静默 drop；conversation 仅一条 message |
| I-7 | vendor_user 在 0 user identity 下发消息 | IdentityResolver | fail-fast emit `bridge.parse_failed { reason=no_user_identity }`；事件落 events 表 |
| I-8 | `agent-center admin backup --dest=/tmp/...` | BackupRun + 真实 SQLite | dest 出现 .db 文件 + 旧文件按 retention 清；emit `admin.backup_ok` |

### 5.3 e2e 测试场景（跨 phase 完整链路）

| # | 场景 | 用户视角 / 入口 | 关键断言 |
|---|---|---|---|
| E2E-A | **完整链路 A**：用户飞书 @bot 提需求 → supervisor 决策 → 派 task → worker 跑 → 进度推回 → 完成 | fake feishu inject `im.message.receive_v1` 携 "@bot 帮我写个 X"。具体断言序列见下表 A1-A12 | 见下方 A1-A12 |

**E2E-A 详细步骤断言**：

| 步骤 | 动作 | events 表预期事件 | 飞书 outbound 投递断言 |
|---|---|---|---|
| A1 | fake feishu inject im.message.receive_v1 | `bridge.identity_auto_bound` + `conversation.message_added (direction=inbound, kind=text)` | — |
| A2 | WakeScheduler tick | `supervisor.invocation_scheduled (scope=conversation:C)` | — |
| A3 | supervisor invocation spawned（fakeagent） | `supervisor.invocation_started` | — |
| A4 | fakeagent script 输出"决定开 task" + 调 `task create` CLI | `task.created (conversation_id=C')` + `conversation.opened (kind=task)` 同事务 | `send_message` 到 dm thread root，content="Task #X / 状态" |
| A5 | supervisor 写 supervisor_summary | `conversation.message_added (direction=outbound, kind=supervisor_summary)` | `send_message` 到 task thread |
| A6 | Center DispatchService 派单到 worker | `task_execution.submitted` + `dispatch.envelope_sent` | — |
| A7 | worker daemon spawn shim + fakeagent | `task_execution.working` + `shim.hello_received` | — |
| A8 | fakeagent 输出 progress milestone（agent_finding）| `conversation.message_added (direction=outbound, kind=agent_finding)` | `send_message` 到 task thread |
| A9 | fakeagent 输出 done | `task_execution.completed` | — |
| A10 | center 端 task.done | `task.done` + `conversation.closed` | `send_message` 到 task thread (system kind, "Task #X done") |
| A11 | worker daemon 上传 agent.log | `task_log.archived` | — |
| A12 | exec 目录 24h GC（注入 clock advance） | `worker.exec_gc_completed` | — |
| E2E-B | **跟踪绑定**：用户飞书 `/track T-42` → bind-conversation → 后续进度自动推飞书 thread | fake feishu inject `/track T-42` text；前置 task T-42 已存在但 conversation_id=null | (1) SlashCommandRouter 调 BindConversation；(2) task.conversation_id 回写；(3) 再 dispatch 一个 progress event → Bridge 推到该 thread |
| E2E-C | **InputRequest 全往返**：agent 调 request-input → 飞书 interactive card → 用户点 button → InputRequest.respond → execution 续 | fake agent 调 `agent-center request-input ...`；测试驱动 fake feishu 模拟用户点 button | (1) InputRequest 写入；(2) Message 含 input_request_ref；(3) Bridge 渲染 card with buttons；(4) fake feishu 模拟 card.action.trigger；(5) InteractiveCardCallback 调 Respond；(6) Bridge update_card 置灰；(7) agent 收到 response 续跑 |
| E2E-D | **worker 离线告警**：worker daemon 心跳停 60s → emit `worker.offline` → WakeScheduler 派 `worker:W` scope → supervisor 决策 → Bridge 推飞书告警卡片 | harness 杀掉 fake worker daemon | (1) center 端 worker.offline 入 events；(2) supervisor 被 wake；(3) fake supervisor 决定通知用户；(4) outbound Message 经 Bridge 推到 default channel |
| E2E-U1 | **升级演练 1**：center restart 不丢请求 | E2E-A 跑到 worker 派单中途，`systemctl restart agent-center` | (1) restart 后 reconcile 协议工作；(2) task 状态正确恢复；(3) 后续 inbound 仍可写入 |
| E2E-U2 | **升级演练 2**：worker daemon restart 不杀 active shim | E2E-A 跑到 worker shim 在跑 fake agent 中途，`systemctl --user restart agent-center-worker` | (1) shim 进程依旧存活（PID + start_time 校验）；(2) daemon 重启后 reconcile + catchup events.jsonl；(3) agent 完成时 execution.completed 正常到 center |

### 5.4 异常路径覆盖（贯穿 § 5.1-5.3）

| # | 异常 | 期望行为 | 在哪验证 |
|---|---|---|---|
| X-1 | vendor SDK 解析失败 / payload 格式异常 | emit `bridge.parse_failed { reason='unknown_event_kind' / 'malformed_payload', message }`；不抛 panic 上传 | 单测 § 5.1 InboundRouter |
| X-2 | vendor_msg_ref 重复 | dedupe drop（不写入；不报错）；emit debug-level `bridge.inbound_dedupe_drop` | 集成 I-6 |
| X-3 | identity 解析失败（0 user / >1 user）| fail-fast；emit `bridge.parse_failed`；返回 sentinel error | 集成 I-7 |
| X-4 | slash 参数错误 / 未知 verb | reject + ephemeral；不污染 Conversation | 单测 SlashCommandRouter |
| X-5 | task / input_request 不存在 | reject + ephemeral；emit `bridge.slash_command_rejected` | 单测 |
| X-6 | InputRequest 已终态（responded / timed_out / canceled） | slash 路径 reject；card 路径静默 ack | 单测 + 集成 |
| X-7 | card_action_value 解析失败（恶意 / 老版本卡片） | emit `bridge.parse_failed { reason='malformed_card_action' }`；不调下游 | 单测 InteractiveCardCallback |
| X-8 | SQLite tx 冲突 / version CAS 失败 | 重试上限 3 次 → emit `bridge.persist_failed { reason, message }` + 抛 caller | 集成 |
| X-9 | WebSocket 断开 | 自动重连指数退避（已在 Phase 5）；inbound 暂停期间 SDK 端 buffer；恢复后追 | 集成 |
| X-10 | backup wal_checkpoint 失败 | emit `admin.backup_failed { reason, message }`；next timer 重试；保留上一次成功备份不动 | 单测 + 集成 I-8 |
| X-11 | center restart 时 inbound 路径正在写一半 | tx 回滚；vendor_msg_ref 未入 dedupe → 飞书 SDK 重投递时正常处理 | e2e E2E-U1 |
| X-12 | worker daemon restart 时 active shim 在跑 | shim 不死（PID + start_time 校验存活）；daemon catchup events.jsonl | e2e E2E-U2 |
| X-13 | shim 进程被意外 SIGKILL（OOM / kill -9） | daemon reconcile 时探活失败 → emit `task_execution.failed { reason='shim_crashed' }` | e2e（异常注入） |
| X-14 | fake feishu inject 大量并发 inbound（压测）| Router 不死锁；events 表无重复；conversation 写入正确序列化 | 集成（spike）|

**测试不允许 sleep**（[testing.md § 4](../rules/testing.md)）：
- 时钟用 `clock.Clock` interface 注入（fake clock）
- "事件到达 / 状态变化"用 events 表 polling + chan 显式 sync
- WebSocket reconnect / shim hello timeout 等场景：用注入的 clock + 主动 advance 时间触发
- backup retention 清理：clock 注入跳过 30 天

### 5.5 测试报告归档

落 `docs/plans/reports/phase-7-test-report.md`（按 [README § 4 模板](README.md#-4-测试报告模板每-phase-完成后填)），条目与本节 5.1 / 5.2 / 5.3 / 5.4 行号 1:1 对应。每条 e2e 场景必须给出：

- 用例文件:函数路径（如 `tests/e2e/scenarios/a_user_at_bot_full_loop_test.go:TestUserAtBotFullLoop`）
- 真实运行时间（中位数 + p95）
- flake rate（连续跑 30 次的 pass 率，必须 ≥ 99.5%）
- events 表事件序列截图 / 文本归档（attach 到报告）

---

## § 6. 风险 / Spike 项

| # | 风险描述 | 缓解 |
|---|---|---|
| R1 | 飞书 SDK 在 v1 期间可能 API 变更（github.com/larksuite/oapi-sdk-go/v3）影响 inbound 解析 | Vendor SDK 调用全部走 Bridge 内 adapter 接口；测试用 fake SDK；SDK 升级时只改 adapter 实现 |
| R2 | `setsid` + `KillMode=process` 在不同 systemd 版本 / 不同发行版（Ubuntu 22 / 24 / Debian / RHEL）行为细微差异，可能导致 shim 被意外 kill | § 3.6 加 install 后强校验脚本 + § 5.3 E2E-U2 在 Ubuntu 22 + 24 双版本 CI 跑 |
| R3 | fake feishu server 跟真实飞书 WebSocket 协议偏差 → e2e 通过但生产不通 | bootstrap playbook 阶段实跑一次真实飞书（手工 smoke test）；记录到 v1 release checklist |
| R4 | v1 单用户场景下 IdentityResolver 默认绑当前 user identity；测试环境如果 setUp 没建 user identity 会大量 fail | harness 强制 setUp 阶段 fixture 写一条 user identity |
| R5 | backup script 在大 SQLite（> 5GB）上 wal_checkpoint 可能超时 / 长卡顿 | v1 单用户场景 SQLite < 1GB，不会触发；checklist 中标注规模阈值 + v2 roadmap 引入 streaming backup |
| R6 | `/answer` 参数中包含空格 / 特殊字符（用户写自由文本作为 choice）解析歧义 | Parser 仅切前 2 个 token；剩余原样为 choice；单测覆盖该路径 |
| R7 | WakeScheduler 周期性 review ticker 在 e2e 测试中可能干扰场景断言 | e2e harness setUp 关闭 ticker（用 clock 注入暂停）|
| R8 | bootstrap playbook 跑通需要真实 VPS —— CI 没法完全 mock | 留出 manual smoke test 步骤；checklist 中标注"演练记录"必须包含真实 VPS spec / 时间戳 |
| Spike-1 | flaky 检测：跨 phase e2e 在 CI 上可能因竞态 flaky | 在 phase 内 spike 一周跑 100× 收集 flake rate；> 0.5% 必须修；不允许 retry-on-fail 掩盖 |

**无"待定" / 无"v2 再说"残留**：剩下不做的全在 [roadmap](../design/roadmap.md)（多 vendor / HA / Web Console / 跨 vendor fallback 等）。

---

## § 7. 下游解锁

本 phase 完成后：

- **v1 GA**：单 VPS 单用户场景全闭环可发布；release checklist 100% 通过即可签发
- **新 Bridge vendor**：DingTalk / Slack / Web 等按本 phase 已固化的 InboundRouter / IdentityResolver / SlashCommandRouter / CardCallback 模板 1:1 复制（同样 6 个 Domain Service 接口签名复用），落 [roadmap](../design/roadmap.md) "多 vendor"项
- **HA / 多 Center**：复用本 phase backup / restore 流程做基线；v2 roadmap "HA 化"
- **Web Console**：本 phase fake feishu server 可作为 WebBridge 第一版参考实现；v2 roadmap "Web Console"
- **运维自动化**：systemd unit / backup timer 已经标准化；后续 monitoring / 告警（Prometheus / Grafana）按 [conventions § 2.x 观测层规约](../rules/conventions.md) 自然扩展

**接口 surface 给下游**：

| 接口 | 形态 | 谁来调 |
|---|---|---|
| `FeishuInboundRouter.Route(VendorEvent) → RouteDecision` | Domain Service | Bridge 自己 WebSocket 长连回调 |
| `FeishuInboundIdentityResolver.Resolve(vendor_user_id) → identity_id` | Domain Service | Bridge 内 inbound + outbound 复用 |
| `SlashCommandParser.Parse(text) → SlashCommand` | 纯函数 | Bridge 内 |
| `SlashCommandRouter.Route(cmd, ctx)` | Domain Service | Bridge 内 |
| `FeishuInteractiveCardCallback.Handle(event)` | Domain Service | Bridge 内 |
| systemd unit + install scripts | contrib/* | 运维（手动）/ CI 部署演练 |
| backup runtime | `agent-center admin backup` CLI + timer | systemd timer / 手动 |
| e2e harness | `tests/e2e/` 包 | CI + 开发者本地复现 |
| v1 release checklist | `docs/plans/reports/v1-release-checklist.md` | release manager（即用户）|

---

## § 8. 跟 v1 release 节奏对齐

Phase 7 完成 = v1 candidate；release manager（用户）按 [v1 release checklist](reports/v1-release-checklist.md) 逐项核对后签发：

1. **Phase 完成度**：1-7 全绿 ✅
2. **关键 e2e 状态**：§ 5.3 全 pass ✅
3. **部署演练**：真实 VPS 跑一次 install / enroll / feishu setup / 提一个真实 task → 完成 → 回流 → 验飞书侧呈现
4. **阻塞 issue**：清零（无 P0 / P1 在 GA 名单中）
5. **v2 defer 清单**：归档到 [roadmap](../design/roadmap.md)，不影响 release

release tag：`v1.0.0`，最后 commit message 体现 phase 完成（参考 [README § 5](README.md#-5-git-workflow)）："feat(phase-7): bridge inbound + 部署收尾 + v1 release checklist 完成"。
