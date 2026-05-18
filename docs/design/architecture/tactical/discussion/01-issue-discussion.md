# Issue 讨论模型

> **DDD 战术层** · BC: Discussion

## 概念定位

| 概念 | 定义 |
|---|---|
| **Task** | Agent 干活的最小单位。状态机驱动：submitted → working → completed / failed |
| **Issue** | 一件**待讨论**的事。可能由 worker、supervisor、用户提起。讨论 → 结论 → 也许产生 0/1/N 个 Task |
| **IssueComment** | Issue 的结构化评论。**Discussion BC 独有的实体，单独表 `issue_comments`** —— 不是 Conversation Message 的子类（见 [ADR-0009](../../../decisions/0009-issue-conversation-decoupled-via-bridge.md)） |

**Task 是"做"；Issue 是"议"**。讨论不是闲聊 —— 严格记录、可审计、有明确结论。

命名由来与 Suggestion 命名的对照见 [ADR-0004](../../../decisions/0004-issue-not-suggestion.md)。

## Issue 与 Conversation 的关系（解耦）

**Issue 与 Conversation 是独立聚合，不是 1:1 绑定**（见 [ADR-0009](../../../decisions/0009-issue-conversation-decoupled-via-bridge.md)）：

- Issue 可以**没有**任何 conversation（CLI / Web Console 开的 issue）
- 一个 Issue 可关联多个 conversation（弱关联 N:N，通过 `issue.related_conversation_ids` JSON 字段）
- Conversation 不知道 Issue 存在（单向引用）
- IssueComment 跟 Conversation Message 是**两套独立数据**

唯一例外：**Issue 可绑定一张"飞书 card"作为专属讨论区**，bound thread 内的消息直接写 IssueComment（不进 Conversation）。详见 [§ Bound Card 机制](#bound-card-机制)。

## 来源（谁能开 Issue）

所有入口最终都走 Discussion BC 内部 `create_issue` API；区别只在调用栈起点。

| 入口 | 方式 |
|---|---|
| 人 - CLI | `agent-center issue open --title=... --description=... --project=...`（server 同机） |
| 人 - Web Console | 表单提交 |
| 人 - 飞书 @bot 自由文本 | "讨论一下 X" → FeishuBridge 把消息写到对应 Conversation → Supervisor 订阅事件 → 解析意图 → 调 `create_issue` |
| 人 - 飞书 /slash 命令 | `/open-issue title="..." project=...`（推迟 v2，[roadmap](../../../roadmap.md)）|
| Supervisor | Supervisor 调 `agent-center issue open ...`（通过 Bash 工具） |
| Worker (agent) | Agent 调 `agent-center open-issue --title=...`，worker daemon 中转 |

## 生命周期

```
open ──▶ under_discussion ──┬──▶ concluded
                            │       ├──▶ closed_no_action
                            │       └──▶ closed_with_tasks
                            │
                            └──▶ withdrawn
```

转换规则：

- `open → under_discussion`：第一条**非 opener** 的 IssueComment 写入（基于 IssueComment，不是基于 Message）
- `under_discussion → concluded`：用户点 [得出结论] 按钮 / `agent-center issue conclude` 命令
- `concluded → closed_with_tasks`：结论里包含要 spawn 的 Task 描述，confirm 后批量创建
- `closed_no_action`：结论是"不做"，issue 关闭无产物
- `withdrawn`：开 issue 的人撤回 / 用户标记不再相关

## 数据模型

聚合根：**Issue**；从属：**IssueComment**。

```
issue (
  id, project_id,
  title, description,
  opened_by_identity_id,         -- 'user:hayang' | 'supervisor:invocation-N' | 'agent:session-X'
  opened_at,
  status,                        -- open | under_discussion | concluded | closed_no_action | closed_with_tasks | withdrawn
  concluded_at, conclusion_summary, concluded_by_identity_id,

  bound_card_json,               -- 可选 bound card 元数据（v1 仅 feishu, 见下）
  related_conversation_ids       -- JSON 数组 (conversation_id list), 弱关联引用
)

issue_comment (
  id, issue_id, author_identity_id, posted_at, edited_at,
  content,                       -- markdown
  kind,                          -- text | conclusion_draft | task_proposal | system | finding

  -- Issue 特有 metadata (v1 部分 / v2 扩展):
  is_pinned,                     -- v2: 置顶
  parent_comment_id,             -- v2: 回复嵌套
  linked_task_ids,               -- JSON array, v1 选择性使用
  source_message_ref             -- 可选: 引用某条 vendor message (如果来自 bound thread)
)

-- Task 加字段:
task.from_issue_id  nullable
```

**不再有 conversation_id 字段**（之前 1:1 绑定那个）。Issue 跟 Conversation 是弱关联 N:N，靠 `related_conversation_ids` JSON 数组。

## IssueComment 写入策略（全显式）

[ADR-0009](../../../decisions/0009-issue-conversation-decoupled-via-bridge.md) 选 **A 全显式**：

- **任何 IssueComment 都通过显式 API 调用产生**（直接调 `agent-center issue comment`，或者经由 Bridge 从 bound thread 写入 —— 后者本质也是显式：用户主动在 bound thread 内回复 = 显式投递到该 issue）
- **不存在"自动从任意 conversation message 推断出 IssueComment"** —— Conversation 里聊 issue 的话，不会自动变成 IssueComment

入口：

| 入口 | 操作 |
|---|---|
| 人 - CLI | `agent-center issue comment <id> --content=...` |
| 人 - Web Console | 在 issue 详情页评论框 |
| 人 - 飞书 bound card thread 内回复 | FeishuBridge 收消息 → 写 IssueComment（kind=text, source_message_ref=vendor_msg_id） |
| Supervisor | 调 `agent-center issue comment <id> --content=...` |
| Worker (agent) | 调 `agent-center issue comment <id> --content=...` (worker daemon 中转) |

Supervisor 在 issue 讨论中的典型角色：**记录员 + 决策者**。
- 跟用户聊的过程中（无论在飞书 / Web Console），主动调 `issue comment` 把关键回合（用户决策 / 重要事实）固化到 issue 上
- 提出"结论草案"时调 `issue comment --kind=conclusion_draft`

## Bound Card 机制

Issue 可绑定一张飞书 card，作为该 issue 的**专属讨论区**。

### 模型

```
issue.bound_card_json = {
  "channel": "feishu",
  "card_message_id": "...",
  "thread_key": "...",
  "bound_at": "...",
  "bound_by_identity_id": "..."
}
```

- **可选**：Issue 可没有 bound card（CLI / Web Console 开的 issue 可后续补绑）
- **1:1**（v1）：一个 Issue 至多 1 张 bound card；多 card 推迟 v2
- **可 rebind**：原 thread 失效或想换时调 `agent-center issue rebind-card <id>`

### CLI

```
agent-center issue bind-card <issue_id> --channel=feishu [--auto]
agent-center issue unbind-card <issue_id>
agent-center issue rebind-card <issue_id> --channel=feishu [--auto]
```

`--auto` 模式：bridge 自动在合适位置发一张新的 issue card（DM 或群中），把 thread_key 写回 issue。无 `--auto` 时：用户先在飞书发 card，把 message_id 提供给命令。

### Bound thread 内的双向同步（FeishuBridge 负责）

```
Outbound (系统 → 飞书):
  Discussion BC 写入 IssueComment → emit issue.comment_added
  FeishuBridge 订阅 → 查 issue.bound_card_json → 有 → 投递到 thread

Inbound (飞书 → 系统):
  FeishuBridge 收 vendor 消息 → 看 thread_key 是否匹配某 issue.bound_card.thread_key →
    匹配 → 直接调 Discussion BC API 写 IssueComment (kind=text, source_message_ref=vendor_msg_id)
            不进 Conversation (该 thread 是 issue 专属, 不冗余)
    不匹配 → 走 Conversation 普通流程
```

**关键约束：Bound thread 的 vendor 消息只产生 IssueComment，不产生 Conversation Message**。Bridge 负责区分。

## 讨论的完整流程（含 bound card）

```
1. 用户在飞书 DM 内 @bot: "讨论一下 worker 注册要不要做自动化"
   → FeishuBridge 收消息 → 写到 DM Conversation (kind=dm) 的 Message
   → emit conversation.message_received
   → Supervisor 唤醒 → 解析为"开 issue 意图"

2. Supervisor 调 agent-center issue open ... (内部 API)
   → Discussion BC 创建 Issue 7

3. Supervisor 调 agent-center issue bind-card 7 --channel=feishu --auto
   → Discussion BC 标记意图 + emit issue.bound_card_requested
   → FeishuBridge 订阅 → 在适合位置发一张 issue card → 回写 issue.bound_card_json

4. Bound thread 出现, 用户后续讨论在那里:
   - 用户回复 → FeishuBridge 收 → 看 thread 匹配 bound_card → 写 IssueComment (kind=text, source_message_ref=...)
   - emit issue.comment_added → Supervisor 唤醒 → 读相关上下文 → 决定回复
   - Supervisor 调 agent-center issue comment 7 --content=... → Discussion 写 IssueComment
   - emit issue.comment_added → FeishuBridge 订阅 → 投递到 bound thread

5. 用户觉得讨论够了 → 点 [得出结论] 按钮 (card.action 在 bound thread):
   → FeishuBridge 收 card.action → 调 agent-center issue conclude 7
   → Discussion BC 触发 supervisor "conclude flow"
   → Supervisor 读 IssueComments 全部 → 写一条 kind=conclusion_draft 评论 (含建议 Tasks 列表)
   → FeishuBridge 投递到 bound thread (渲染为带按钮的卡片)

6. 用户 [确认] / [改改再确认] / [不做了]
   → 确认 → Discussion BC: issue.status=closed_with_tasks, batch create Tasks
   → emit issue.tasks_spawned / 等
```

**关键看点**：上面整个流程，Discussion BC 不知道飞书是啥；调用栈里没有任何 vendor SDK 调用。所有 vendor 集成都在 FeishuBridge 内。

## Issue 与其他实体的区别

| 维度 | Issue | InputRequest |
|---|---|---|
| 性质 | 议题讨论 | 任务执行中的同步请示 |
| 阻塞性 | 不阻塞 worker | 阻塞 worker（agent 等回答） |
| 多轮 | 是 | 否（一问一答） |
| 结果 | 0/1/N 个 Task | 一段答复给 agent |
| 生命周期 | 复杂状态机 | pending → responded / timed_out |
| 跟外部渠道 | 可选 bound card | 通过飞书卡片升级（无 bound） |

## v1 简化范围

- Issue 默认显式创建（CLI / Web Console / 飞书 @bot 触发 supervisor），**不**自动从 agent 输出片段推断
- IssueComment 全显式（[Q1=A](../../../decisions/0009-issue-conversation-decoupled-via-bridge.md)），不做自动镜像
- Agent 不主动在 issue 内评论（agent 可通过 worker-agent CLI 调 `issue comment`，但 agent 主动加入"讨论"的政策推迟，[roadmap](../../../roadmap.md)）
- Issue 结论由用户拍板，supervisor 只能"建议结论"不能自动收敛
- 不做"Issue 之间的引用关系" / "epic / story 分层"
- Bound card v1 仅飞书；1 issue 1 card；可 rebind；多 card 推迟
- `issue_comment.is_pinned` / `parent_comment_id` / `reactions` 等扩展字段 v1 不实现，schema 留位

## 关联 Conversation 的触发

什么情况下一个 conversation 会进入 `issue.related_conversation_ids`：

| 触发 | 自动 / 手动 |
|---|---|
| R1：用户在飞书 DM / group thread 内 @bot 开 issue → 那个 conversation 自动关联 | 自动 |
| R2：Supervisor 派子任务时关联：supervisor 调 agent 做研究，agent 干活的过程产生一个执行 conversation → 自动关联到原 issue | 自动 |
| R3：手动关联：用户在 web console / CLI 上 `agent-center issue link-conversation <issue_id> <conv_id>` | 手动 |

关联是**弱引用**，仅用于"看 issue 时知道这些会话提到过它 / 衍生于它"，不影响 IssueComment 写入。
