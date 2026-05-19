# 飞书集成（FeishuBridge）

> **DDD 战术层** · BC: Bridge

飞书是 agent-center **v1 唯一实现的 Bridge**。本文档是 [Bridge BC](../../strategic/03-bounded-contexts.md#bc7-bridge渠道桥接层) 下 `FeishuBridge` 的具体实现说明。

> **结构定位：** 飞书不是"主入口" —— 它只是众多潜在 vendor 中第一个被支持的。其它 vendor（DingTalk / Web chat / Slack / ...）按同样的 Bridge 模式加入，详见 [roadmap](../../../roadmap.md)。
>
> 领域模块（[Conversation](../conversation/01-conversation.md) / [Discussion / Issue](../discussion/01-issue-discussion.md) / [TaskRuntime](../task-runtime/00-overview.md) / [Cognition](../cognition/01-supervisor-model.md)）**不知道飞书是啥**；所有 vendor 集成都在 FeishuBridge 内。
>
> 设计原则见 [conventions § 9.y "外部集成走 Bridge 模式"](../../../../rules/conventions.md#-9y-外部集成走-bridge-模式不在领域模块内调-vendor-sdk)。

---

## § 1. FeishuBridge 角色定位

| 维度 | 决定 |
|---|---|
| 部署 | 嵌入 `agent-center server` 进程（不是独立 binary） |
| 触发模型 | **事件驱动 + 长连接订阅** |
| Inbound 来源 | 飞书 WebSocket 长连接（vendor 事件推回） |
| Outbound 触发 | 订阅领域事件（`conversation.message_added` / `issue.comment_added` / 等） |
| 状态持有 | 仅 vendor 连接状态 + 小审计表；**不持有领域状态** |
| 调用方向 | inbound: Bridge → 领域 API；outbound: 领域 events → Bridge → vendor API |

## § 2. 一次性设置流程

```bash
# 在 center 同机执行
agent-center feishu setup
# → 打开浏览器到一键创建 URL（或终端打印二维码）
# → 用户飞书扫码确认
# → SDK 回调拿到 App ID + App Secret
# → 写入 agent-center 配置文件
```

利用飞书开放平台 **"一键创建飞书智能体应用"** 能力：自动预置必要权限 + 事件订阅，免手动开通。

SDK 选型：`github.com/larksuite/oapi-sdk-go/v3`（飞书官方 Go SDK）。

## § 3. 网络方向

**Center 主动出站建立 WebSocket** 到飞书，**不暴露任何端口给飞书**。所有事件 / 回调通过同一长连推回。

```
[Center on VPS, 含 FeishuBridge] ──→ 主动建立 WebSocket ──→ 飞书开放平台
                                  ←── 事件 / 卡片回调通过这条长连 ───
```

VPS 仅需要出站到飞书。

## § 4. Inbound 流程（vendor → 系统）

```
FeishuBridge 收到 vendor 事件 (im.message.receive_v1 / card.action.trigger / ...)
   ↓
路由判断:
  Step 1: 该 thread/dm 是否绑到某 issue 的 bound_card_json.thread_key?
    是 → 调 agent-center issue comment <issue_id> --content=... --source-message-ref=<vendor_msg_id>
         写 IssueComment, **不进 Conversation**
         ✋ 流程结束

  Step 2: 路由到 Conversation:
    按 (channel='feishu', vendor_thread_key) 查 conversation
    找到 → 调 agent-center conversation add-message --id=<conv> --content=... (direction=inbound, vendor_msg_ref=...)
           （此 lookup 同样命中 kind=task conversation —— 用户在 task thread 内说话由该路径写入；
            "/answer" / "/track" 等 slash 命令的留痕也走此分支，详见 § 9.1）
    找不到 → 按 event context 创建新 conversation
              (DM → kind=dm; 群里 @bot 但无 thread → kind=adhoc; 群里 thread 内 → kind=group_thread)
              （注意：kind=task conversation **不**由此分支创建 —— 走 § 7.5.1 同步建 / § 7.5.2 懒创建）
              然后调 conversation add-message 写入
   ↓
领域模块写入数据 + emit 事件
其它 BC 订阅事件做业务 (如 Supervisor 决定是否回复)
```

**幂等保证：** 同一 `vendor_msg_ref` 不重复写入（Bridge 内 dedupe）。

## § 5. Outbound 流程（系统 → vendor）

FeishuBridge 订阅以下事件：

| 事件 | 触发 outbound 行为 |
|---|---|
| `conversation.message_added` (direction=outbound) | 投递到对应 conversation 的 feishu thread/dm；按 message.content_kind + input_request_ref 渲染（§ 6） |
| `conversation.opened` (kind=task) | 发 Task root card 到适合位置 → 回写 `primary_channel_thread_key`（详见 § 7.5.1） |
| `issue.comment_added` | 查 issue.bound_card_json → 有 bound thread → 投递到该 thread |
| `issue.opened` | 决策（按 `agent-center issue bind-card --auto`）：是否自动发 issue card？ |
| `input_request.responded` / `input_request.timed_out` / `input_request.canceled` | update_card 置灰 + 显示终态文案（详见 [03-input-request.md § 完整流程](../task-runtime/03-input-request.md)）|

投递逻辑：

```
FeishuBridge 订阅到 conversation.message_added 事件
   ↓
查 conversation.primary_channel_hint = 'feishu' & primary_channel_thread_key = '...'
   ↓
按 message.content_kind + input_request_ref 渲染（详见 § 6）:
  - text / agent_finding (input_request_ref 为空) → markdown 消息 (im.v1.messages.create text)
  - system → 简单卡片 (im.v1.messages.create interactive, 小)
  - supervisor_summary → 富卡片 (含按钮: 确认 / 改 / 不做)
  - agent_finding + input_request_ref ≠ null → interactive card with buttons
    (join InputRequest 取 options 渲染 [A][B][自己写][取消])
   ↓
调飞书 SDK 投递:
  - 成功 → 回填 message.vendor_msg_ref；emit channel.delivered
  - 失败 → 重试 3 次指数退避；最终失败 → emit channel.delivery_failed
```

## § 6. 渲染规则（content_kind + input_request_ref → vendor 形态）

| content_kind | input_request_ref | 飞书形态 | 说明 |
|---|---|---|---|
| `text` | — | text message | markdown |
| `system` | — | small interactive card | 包含简短文本 + 标签（如 "Issue #7 已开" / "Task #42 created"）|
| `agent_finding` | null | text message | 跟 text 类似，标签上注明来自 agent / worker |
| `agent_finding` | **≠ null** | rich interactive card with buttons | join InputRequest 取 options → 渲染 [选项 A][B][自己写][取消] 按钮；状态变化时 update_card 置灰（详见 [03-input-request.md 完整流程](../task-runtime/03-input-request.md) 与 [ADR-0017 § 5](../../../decisions/0017-task-as-conversation.md)） |
| `supervisor_summary` | — | rich interactive card | 含按钮：[确认结论] [改后确认] [不做] |

具体卡片模板由 FeishuBridge 内置；不由领域模块定义。

## § 7. Bound Card 机制（Issue）

`agent-center issue bind-card <issue_id> --channel=feishu --auto`：

- Discussion BC emit `issue.bound_card_requested` 事件
- FeishuBridge 订阅 → 在适合位置（user 的 DM / 同 conversation 所在群）发一张 "Issue card"（含 issue 元信息 + 操作按钮）
- 取到 vendor 返回的 message_id + 形成 thread_key
- 调 Discussion BC API 回写 `issue.bound_card_json`

后续 bound thread 内的 inbound / outbound 走 § 4 / § 5 的 "bound thread 特殊路由"。

详见 [03-issue-discussion.md § Bound Card 机制](../discussion/01-issue-discussion.md#bound-card-机制)。

## § 7.5 Task Conversation 创建流程

跟 § 7 Issue Bound Card 同构但通道独立：Task 的"卡片"是 `kind=task` Conversation 的 root message + 后续 thread，所有 task IO 都在同一 thread 内时间线呈现（见 [ADR-0017](../../../decisions/0017-task-as-conversation.md)）。

### § 7.5.1 同步建（飞书用户 @bot 直接发起；a 路径）

```
t0  用户在飞书 DM / 群里 @bot："帮我写个 xxx"
t0  FeishuBridge 收 im.message.receive_v1
t0  Bridge 走 § 4 inbound → 写 Message 到对应 dm / group_thread conversation
t0  Conversation BC emit conversation.message_added
t1  Supervisor wake → 决定开 Task → 在单事务内（[ADR-0014 § 2](../../../decisions/0014-event-sourcing-level.md)）:
      a. TaskRuntime BC create Task
      b. Conversation BC create Conversation (kind=task) + 关联 channel binding
         （继承当前用户所在飞书渠道）
      c. Task.conversation_id ← Conversation.id
      d. emit task.created (payload 含 conversation_id) + conversation.opened
t2  Bridge 订阅 conversation.opened (kind=task):
      - 发 Task root card 到 vendor 当前位置（DM / 群里 thread root）
      - 该 card 形成 thread root → 回写 conversation.primary_channel_thread_key
t3  Supervisor 立即写一条 Message:
      kind=supervisor_summary, content="收到，正在分析 / 寻找 worker / ..."
      → Bridge 投递到 thread
t4+ 后续 supervisor 分析 / worker 进展 / 用户回应都流进同一 thread
    （worker daemon 也可直接通过 [BC4 长连 RPC](../../strategic/03-bounded-contexts.md) 调
     conversation add-message 写 agent_finding，详见 [02-task-execution.md § 9.6 进度上报 milestone](../task-runtime/02-task-execution.md)）
```

### § 7.5.2 懒创建（b/c/d 路径触发）

| 触发 | 处理 |
|---|---|
| CLI `task bind-card <task_id> --channel=feishu --to=<conv_id>` | center 直接绑既有 conversation；Bridge 不动作 |
| CLI `task bind-card <task_id> --channel=feishu --auto` | center 新建 kind=task Conversation + emit `conversation.opened` → Bridge 发 root card 到 `notification.default_channel`（或 CLI 指定的渠道）|
| 飞书 slash `/track <task_id>` | Bridge 翻译为 `task bind-card --channel=feishu --to=<当前 thread 对应 conversation>`（详见 § 9.1）|
| 飞书 @bot 自由文本 "盯一下 T-42" | Bridge 写 inbound Message → supervisor wake → 解析意图 → 调 `task bind-card`（烧 LLM；兜底用）|
| Center 硬规则 fallback（InputRequest） | agent 调 request-input 且 task.conversation_id=null → center 自动 bind 到 `notification.default_channel`（[ADR-0017 § 10.4](../../../decisions/0017-task-as-conversation.md)）|

### § 7.5.3 关闭

Task `done` / `abandoned` → Conversation BC 把对应 kind=task conversation `status=closed`（保留全部历史，v1 不删；用户翻飞书 thread 仍能看）。Bridge 不主动 update_card 或发"已完成"消息；这一类 finish summary 由 supervisor 主动写一条 Message 完成（普通 outbound 流程）。

### § 7.5.4 跟 § 7 Issue Bound Card 的差异

| 维度 | Issue Bound Card（§ 7） | Task Conversation（§ 7.5） |
|---|---|---|
| 模型 | Issue 聚合 + `bound_card_json` 字段记录 vendor thread | Task ↔ Conversation 1:1（task.conversation_id 引用 Conversation 聚合） |
| Inbound 写哪 | bound thread 内的 vendor message → `issue comment`（IssueComment 表）| task conversation 内的 vendor message → `conversation add-message`（Message 表） |
| 是否独立 thread root | 是（Issue card 是 thread root） | 是（Task root card 是 thread root） |
| 用户认知 | "讨论一件事" | "做一件事" |

## § 8. 接收事件类型（v1 订阅清单）

通过 WS 长连订阅：

- `im.message.receive_v1` — DM / 群消息
- `card.action.trigger` — 卡片按钮 / 表单提交
- 其它（用户加 bot、好友、入群等）暂不处理

## § 9. 三模式交互（D1 / D2 / D3）

| 模式 | v1 | 描述 |
|---|---|---|
| **D1. @bot + 自由文本** | ✅ 必做 | DM 直接说话；群里 @bot 说话；supervisor LLM 解析意图 |
| **D2. Slash 命令** | **部分 v1** | `/answer <task_id> <text>` + `/track <task_id>` 必做（[ADR-0017 § 6](../../../decisions/0017-task-as-conversation.md)）；`/dispatch project=X agent=claude "..."` 等后置 |
| **D3. 交互卡片** | ✅ 必做 | Suggestion 升级、Issue 收尾、任务完成报告、InputRequest、周期 review 等 |

v1 走 D1 + D3 + D2（部分）覆盖 90% 体验。D2 剩余命令视后续需求开放（见 [roadmap](../../../roadmap.md)）。

### § 9.1 Slash 命令处理

Slash 命令的核心特征：**Bridge 直接调对应领域 API + 写一条 Message 留痕，不经 supervisor**（规则解析、不烧 LLM、低延迟、误解风险低）。

| 命令 | 行为 | 留痕 |
|---|---|---|
| `/answer <task_id> <text>` | Bridge 解析 → 调 `InputRequest.respond`（找 task.pending_input_request_id）+ 单事务写 Message (`kind=text, direction=inbound, sender=user, content=<text>`) 到 task.conversation_id | "/answer T-42 选 B" 类的留痕条目；InputRequest 状态变化时 Bridge update_card 置灰 |
| `/track <task_id>` | Bridge 解析 → 翻译为 `task bind-card --channel=feishu --to=<当前 thread 对应 conversation_id>`（懒创建路径之一，见 § 7.5.2）；若当前 thread 还没对应 conversation，先按 § 4 inbound 流程创建 dm / group_thread conversation 再绑 | center 写 task.conversation_id + emit conversation 关联记录；supervisor 唤醒后主动写一条 supervisor_summary 到 task.conversation_id 提示绑定成功并续后续进度 |
| `/dispatch ...` | v1 不实现（[roadmap](../../../roadmap.md)） | — |

**错误处理：**

- `/answer` 找不到 pending InputRequest → Bridge 回一条 ephemeral message："Task #X 当前没有等待的 InputRequest"
- `/track` 找不到 task_id → 同上
- 参数解析失败 → 同上 + 简短 usage 提示

错误处理本身不进 Conversation Message（避免污染留痕）；仅作为 vendor 侧 ephemeral 提示。

## § 10. 失败 / 错误处理

- inbound 解析失败 → emit `bridge.parse_failed` + 日志；不传到领域模块
- outbound 投递重试 3 次失败 → emit `channel.delivery_failed`；Cognition BC 订阅决策
- vendor 连接断开 → 自动重连（指数退避）；累计失败时上报状态

## § 11. v1 简化

- 仅 FeishuBridge；多 vendor 推迟
- 每个 conversation 一个 channel（v1 通过 `primary_channel_hint` 字段固定）
- 无跨 vendor fallback
- 卡片模板硬编码在 Bridge 内（不支持运行时模板）

## § 12. 飞书是无状态通道

- **状态权威永远在 agent-center 领域模块**（Issue / Conversation / Message / IssueComment）
- 飞书 thread 内的每条消息都同步写入领域模块
- 飞书侧若失踪 / 重启 / 改版，agent-center 仍可从领域数据恢复历史

> **本节其余内容待 §9 讨论补全：卡片模板细节、消息格式、跨群 / 单聊行为差异。**
