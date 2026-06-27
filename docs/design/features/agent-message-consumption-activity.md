# 把"消息送达 agent"做成一等 activity 事件

> **状态：** 设计已确认，待实现计划。
> **目标：** 排障时能在 agent 的 activity 时间线上一眼分清三态——**没收到 / 收到了卡在处理中 / 已处理**——消除"agent 半天没动静，分不清是没收到还是卡住"的盲区。
> **锚点：** 复用现有 `agent_activity_events`（通用 `event_type` + JSON payload，ADR-0049 append-only 观测流）与 `conversation.read_state.changed` 事件（migration 0026 read-state 元组）。**零 schema 改动。**

## 0. 问题与定位

当前系统里"agent 做了什么"（activity 流，来自 Claude 运行时）与"消息是否被消费"（read-state 游标 `last_seen_message_id`）是**两条互不交叉的机制**。看 activity 无法判断某条具体入站消息是否进了 agent 的上下文：`system_init` / `rate_limit` 这类事件被前端折叠成模糊的灰色 **"Checking messages ×N"**，信息量极低。

关键架构事实（决定方案形状）：消息到达 agent 有两条路——

- **PUSH（主流）** `AgentController.converse()`：`sess.Inject()` 把消息塞进 agent stdin（`internal/workerdaemon/agent_controller.go:927`）才是消息**真正进入上下文**的时刻；紧接着 `ReportMarkSeen()`（`:945`）**由系统自动推进游标**并触发 `read_state.changed`。
- **PULL** agent 自调 `get_my_unread` 拉取；处理完调 `mark_seen`。

由此推出：**PUSH 路径里游标是系统在注入瞬间自动盖的章，不是 agent 处理完的主动确认**。所以"received 但未 ack = 卡在处理中"这个直觉在 PUSH 路径**不成立**——真正能分清三态的信号是 **(a) delivered（送达）这一条，加上它后面本就存在的 thinking/tool/result 活动**。

**方案收敛（已确认）：** 主打 `message_delivered`，`message_acknowledged` 作为仅 PULL 路径的轻量点缀。

## 1. 排障判读（最终效果，验收基准）

| activity 时间线上看到 | 含义 |
|---|---|
| **Received** 事件，后面跟 thinking/tool/result | 收到并在处理 / 已处理完 |
| **Received** 事件，后面**空白** | ✅ **收到了，卡在处理中** |
| **连 Received 都没有** | ✅ **根本没收到**（wake 漏投 / agent 挂了 / 未命中投递条件） |
| **Acknowledged** 事件（仅 PULL） | agent 主动确认已读某条拉取的消息 |

## 2. 改动点

纯增量。`agent_activity_events` 表与前端 `AgentActivityEvent` DTO 均为通用结构，新增 `event_type` 与 payload 无需迁移。

### ① `message_delivered`（主角，对应 (a) received）

- **领域常量**：`internal/agent/activity_event.go` 新增 `EventTypeMessageDelivered = "message_delivered"`。
- **埋点位置**：`AgentController.converse()` 中 `sess.Inject()` 成功**之后**（`internal/workerdaemon/agent_controller.go:927` 附近）。只有 worker daemon 知道注入这一刻，且能反映"投递漏了/agent 挂了"——这正是它区别于 `conversation.message_added`（消息刚入库）的价值，故**不能**用 message_added 的 projector 代替。
- **发送方式**：复用现成的 `c.cfg.Reporter.ReportAgentActivity(agentID, eventType, payload, taskRef, interactionRef, ts)` 管道（与 `onEvent` 同路）。best-effort：失败记日志、不阻断注入。
- **payload（JSON）**：
  ```json
  {
    "conversation_id": "<ULID>",
    "message_id": "<ULID>",
    "sender_ref": "user:... | agent:... | system",
    "sender_display": "<显示名，可空>",
    "content_preview": "<首段截断预览>",
    "attachments_count": 0
  }
  ```
- **`interaction_ref`**：用 converse 当轮的 interaction/turn ref，使 delivered 与随后的 thinking/tool/result 归于同一逻辑轮。
- **PULL 不重复埋**：agent 自调 `get_my_unread` 的 receipt 已是现成的 `tool_result` activity 事件，故 `message_delivered` 仅覆盖 PUSH 注入路径。

### ② `message_acknowledged`（轻量点缀，仅 PULL，对应 (b)）

- **领域常量**：新增 `EventTypeMessageAcknowledged = "message_acknowledged"`。
- **区分系统自动 ack vs agent 主动 ack**：在 `MarkSeenCommand`（`internal/conversation/service/read_state.go`）增加枚举字段 `Trigger`：
  - `delivery` —— PUSH 路径系统自动推进（`agent_controller.go` 的 `ReportMarkSeen` 链路设此值）。
  - `agent_tool` —— PULL 路径 agent 主动调 `mark_seen` 工具（`internal/admin/api/agent_tools_inbox.go` 的 `markSeenHandler` 链路设此值）。
  - 默认/未指定时按 `agent_tool` 之外的安全值处理（见 §4 边界）。
  - 该字段透传进 `conversation.read_state.changed` 的 payload（新增 `"trigger"` 键）。
- **新增 outbox Projector**：仿 `WakeProjector`（`internal/environment/service/wake_projector.go`）实现 `outbox.Projector`，订阅 `conversation.read_state.changed`：
  - 仅当 `actor` 为 agent **且** `trigger == "agent_tool"` 时，向 `ActivityEventRepository.Append` 追加 `message_acknowledged`（payload 含 `conversation_id` / `message_id` / `previous_last_seen_message_id`）。
  - `trigger == "delivery"`（PUSH 自动）直接跳过 —— 避免与 `message_delivered` 产生几乎同时的冗余成对事件。
  - 注册进 `outboxProjectors()`（`internal/cli/webconsole_wiring.go:146`，projector 单一来源）。
- **跨域说明**：该 projector 读 conversation 域事件、写 agent 域 activity 仓储，与既有 `WakeProjector`（读 conversation 事件、写 agent 控制命令）同构，符合现有 projector 模式。

### ③ 前端渲染（`web/src/components/AgentActivityRow.tsx`）

- `message_delivered` → 独立类目：信封图标，标签 `Received: <sender_display>: <content_preview>`，区别色（如 teal/indigo）。**不得**折进灰色 "Checking messages" 组（`agentActivityGrouping.ts` 的折叠集合不含这两个新类型）。
- `message_acknowledged` → 对勾图标，`Acknowledged: <preview>`，muted。
- 两者可展开查看完整 `message_id` / 会话引用。
- DTO 无需改：`web/src/api/types.ts` 的 `AgentActivityEvent` 已是通用 `event_type` + `payload(string)`。

## 3. 数据流

```
PUSH 路径（主角）：
  human posts → conversation.message_added
    → WakeProjector → agent.converse 命令
    → AgentController.converse() → sess.Inject()  ← 此处后埋 message_delivered
                                 → ReportMarkSeen(trigger=delivery)
                                    → read_state.changed(trigger=delivery) → projector 跳过

PULL 路径（点缀）：
  agent get_my_unread (tool_result 已可见 receipt)
    → 处理 → mark_seen 工具 → markSeenHandler(trigger=agent_tool)
    → MarkSeen → read_state.changed(trigger=agent_tool)
    → AckProjector → Append(message_acknowledged)
```

## 4. 边界与不变量

- **append-only**：两个新事件均经 `ActivityEventRepository.Append`，遵守 ADR-0049 观测流不可变。
- **best-effort 不阻断**：`message_delivered` 埋点失败仅记日志，绝不影响消息注入。
- **幂等**：ack projector 复用 outbox `AppliedStore` 幂等保护（同 `read_state.changed` 事件重放不会重复 Append）。
- **only-forward 不变**：`Trigger` 字段不改变 `MarkSeen` 的 only-forward / 乐观锁语义，仅作来源标注。
- **Trigger 默认值**：现存所有调用点显式设值；为防遗漏，projector 端以"白名单"判定（仅 `agent_tool` 出 ack），未知/缺省值一律不出 ack，宁缺勿滥。
- **不触碰人类未读**：本特性只读 agent 的 activity 流与 read-state，不改人类 unread/badge（Q-T1：agent 永不累计 badge）。

## 5. 测试要点

**后端**
- `converse()` 在 `sess.Inject()` 成功后必发一条 `message_delivered`，payload 字段完整；注入失败或埋点失败不影响注入主流程。
- `MarkSeenCommand.Trigger` 在 PUSH（`delivery`）与 PULL（`agent_tool`）两链路分别正确赋值，并透传进 `read_state.changed` payload。
- Ack projector：`trigger=agent_tool` 且 actor=agent → Append 一条 `message_acknowledged`；`trigger=delivery` → 跳过；事件重放 → 幂等不重复。

**前端**
- `message_delivered` / `message_acknowledged` 渲染正确的 label / icon / 色彩。
- `message_delivered` 不被折叠进 "Checking messages" 组。
- payload 解析容错（字段缺失不崩）。

## 6. 出范围（YAGNI）

- 不在会话消息气泡上做"已读回执"角标（本期主展示面定为 activity 时间线）。
- 不为 PUSH 自动 ack 单独出事件。
- 不新增 schema / 迁移。
- 不改 `get_my_unread` / `mark_seen` 工具的对外契约（仅内部链路加 `Trigger` 标注）。
