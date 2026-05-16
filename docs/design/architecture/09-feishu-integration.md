# 飞书集成（FeishuBridge）

飞书是 agent-center **v1 唯一实现的 Bridge**。本文档是 [Bridge BC](01-bounded-contexts.md#bc8-bridge渠道桥接层) 下 `FeishuBridge` 的具体实现说明。

> **结构定位：** 飞书不是"主入口" —— 它只是众多潜在 vendor 中第一个被支持的。其它 vendor（DingTalk / Web chat / Slack / ...）按同样的 Bridge 模式加入，详见 [roadmap](../roadmap.md)。
>
> 领域模块（[Conversation](12-conversation.md) / [Discussion / Issue](03-issue-discussion.md) / Scheduling / Cognition）**不知道飞书是啥**；所有 vendor 集成都在 FeishuBridge 内。
>
> 设计原则见 [conventions § 9.y "外部集成走 Bridge 模式"](../../rules/conventions.md#-9y-外部集成走-bridge-模式不在领域模块内调-vendor-sdk)。

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
    找不到 → 按 event context 创建新 conversation
              (DM → kind=dm; 群里 @bot 但无 thread → kind=adhoc; 群里 thread 内 → kind=group_thread)
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
| `conversation.message_added` (direction=outbound) | 投递到对应 conversation 的 feishu thread/dm |
| `issue.comment_added` | 查 issue.bound_card_json → 有 bound thread → 投递到该 thread |
| `issue.opened` | 决策（按 `agent-center issue bind-card --auto`）：是否自动发 issue card？ |

投递逻辑：

```
FeishuBridge 订阅到 conversation.message_added 事件
   ↓
查 conversation.primary_channel_hint = 'feishu' & primary_channel_thread_key = '...'
   ↓
按 message.content_kind 渲染:
  - text / agent_finding → markdown 消息 (im.v1.messages.create text)
  - system → 简单卡片 (im.v1.messages.create interactive, 小)
  - supervisor_summary → 富卡片 (含按钮: 确认 / 改 / 不做)
   ↓
调飞书 SDK 投递:
  - 成功 → 回填 message.vendor_msg_ref；emit channel.delivered
  - 失败 → 重试 3 次指数退避；最终失败 → emit channel.delivery_failed
```

## § 6. 渲染规则（content_kind → vendor 形态）

| content_kind | 飞书形态 | 说明 |
|---|---|---|
| `text` | text message | markdown |
| `system` | small interactive card | 包含简短文本 + 标签（如 "Issue #7 已开"）|
| `agent_finding` | text message | 跟 text 类似，标签上注明来自 agent |
| `supervisor_summary` | rich interactive card | 含按钮：[确认结论] [改后确认] [不做] |

具体卡片模板由 FeishuBridge 内置；不由领域模块定义。

## § 7. Bound Card 机制

`agent-center issue bind-card <issue_id> --channel=feishu --auto`：

- Discussion BC emit `issue.bound_card_requested` 事件
- FeishuBridge 订阅 → 在适合位置（user 的 DM / 同 conversation 所在群）发一张 "Issue card"（含 issue 元信息 + 操作按钮）
- 取到 vendor 返回的 message_id + 形成 thread_key
- 调 Discussion BC API 回写 `issue.bound_card_json`

后续 bound thread 内的 inbound / outbound 走 § 4 / § 5 的 "bound thread 特殊路由"。

详见 [03-issue-discussion.md § Bound Card 机制](03-issue-discussion.md#bound-card-机制)。

## § 8. 接收事件类型（v1 订阅清单）

通过 WS 长连订阅：

- `im.message.receive_v1` — DM / 群消息
- `card.action.trigger` — 卡片按钮 / 表单提交
- 其它（用户加 bot、好友、入群等）暂不处理

## § 9. 三模式交互（D1 / D2 / D3）

| 模式 | v1 | 描述 |
|---|---|---|
| **D1. @bot + 自由文本** | ✅ 必做 | DM 直接说话；群里 @bot 说话；supervisor LLM 解析意图 |
| **D2. Slash 命令** | ⏸ 后置 | `/dispatch project=X agent=claude "..."` 等；规则解析、不烧 LLM |
| **D3. 交互卡片** | ✅ 必做 | Suggestion 升级、Issue 收尾、任务完成报告、InputRequest、周期 review 等 |

v1 走 D1 + D3 覆盖 90% 体验。D2 视后续需求开放（见 [roadmap](../roadmap.md)）。

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
