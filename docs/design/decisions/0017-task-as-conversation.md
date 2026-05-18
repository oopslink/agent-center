# 0017. Task 即 Conversation：1:1 绑定 + 所有 task UI 走统一 Message 时间线

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-17 |
| Supersedes | [ADR-0016](0016-task-progress-via-bound-thread.md) |

## Context

[ADR-0016](0016-task-progress-via-bound-thread.md) 把"飞书侧看 worker 进度"框定为"在 Issue bound_card 之上加一层 Task bound_card"，并新增 `task_progress` content_kind 承载进度消息流。但落 ADR-0017 这次讨论中识别出几个深层问题：

- **"进度卡 / tracking card" 是个不必要的独立概念**：用户的真实诉求是"task 一切相关的对话都在一个地方"，包括 supervisor 的分析、worker 的进度、agent 的请示、用户的回应。把"进度"从其他 task IO 中切出来单独搞机制是过度抽象
- **`task_progress` content_kind 多余**：现有 `text` / `agent_finding` / `supervisor_summary` 已能区分 actor；进度本质就是 worker 的 `agent_finding`、supervisor 的分析就是 `supervisor_summary`
- **InputRequest UI 在 ADR-0016 框架下没法统一路由**：要么单独"InputRequest 卡片走另一套"，要么把它强塞 task_progress；都不优雅。根本问题是 ADR-0016 把"bound thread"做成可选的 tracking 机制，没把它升格为 task 的"对话主场"
- **多 tracker（1:N）模型用户场景弱**：v1 单用户，"两个用户在不同群盯同一 task"是 v2 才有的诉求；为此付出 cross-card update 复杂度不值

新方向（用户给出）：

> 1. 人 @bot 开 task → 2. supervisor 在卡片回"收到"+分析展示 → 3. worker 干活的中间输出在卡片展示（含请示）→ 4. 用户 @bot 确认或输入新信息（也支持 /slash 绕过 supervisor）
>
> 所有这些都在**同一个 Conversation** 中

提炼成模型：**Task ↔ Conversation 1:1（按来源决定创建时机）；所有 task 相关可见 IO 都是该 Conversation 内的 Message；没有"进度卡"独立概念**。

## Decision

### 1. Task 1:1 Conversation，按来源决定创建时机（模型 iii）

Task 表新增 `conversation_id` (nullable)。创建时机分两支：

| Task 来源 | conversation 创建 |
|---|---|
| 飞书用户 @bot 直接发起（a） | Task 创建时**同步**新建 Conversation + 绑 vendor thread |
| Web Console 新建（e） | 同上（Web Console 是 Conversation 的另一个 channel binding） |
| Issue concluded spawn 出（b） | **不**新建，`task.conversation_id = null`；用户后续 `task bind-card` 触发懒创建 |
| Supervisor 自主开（c，v1 暂无） | 同 b |
| CLI 直接 `task create`（d） | 同 b |

Worker / supervisor 写 IO 前 null-safe：`conversation_id != null` 才走 Conversation；为 null 时 task 仍正常跑，只是无飞书可见性，可 `inspect task` 看状态。懒创建 actor 与触发条件见 § 10。

### 2. Conversation 新增 `kind = task`

12-conversation 现有 kind 枚举 `dm` / `adhoc` / `group_thread`，加 `task`。生命周期与 task 绑定：

- Task 创建 → Conversation 创建
- Task done / abandoned → Conversation 标 closed（保留全部历史，不删；用户翻飞书 thread 仍能看；CLI `inspect conversation` 仍可查）

### 3. 所有 task 相关 IO 走 Conversation Message，**不**新增 content_kind

复用现有：

| Actor / 内容 | content_kind |
|---|---|
| 用户输入（@bot / 卡片按钮回执 / slash 命令的留痕） | `text` |
| supervisor 分析 / 决策 / 中间思考摘要 | `supervisor_summary` |
| worker / agent 的进展 / 请示 / 完成报告 | `agent_finding` |
| 系统级提示（task created / dispatched / status changed 摘要） | `system` |

撤回 [ADR-0016 § 4](0016-task-progress-via-bound-thread.md) 引入的 `task_progress` content_kind。

### 4. 5 条 milestone 节流仍有效，但**执行位置改为 worker daemon**

[ADR-0016 § 3](0016-task-progress-via-bound-thread.md) 5 条节流规则（status_change / activity_kind_change / long_tool_call_start / long_tool_call_end / cumulative_summary / user_inquiry）整体保留。变更：

- 不再 emit `task.progress_milestone_reached` 事件（不需要专门的进度事件类）
- worker daemon 边解析 JSONL 边判断 milestone 命中 → 直接调 `agent-center conversation add-message --to=<task.conversation_id> --kind=agent_finding --content="..."`
- worker 通过 dispatch envelope 拿 conversation_id；为 null 跳过该 message 写入

### 5. InputRequest 走 Message + Bridge 渲染附按钮

InputRequest 仍是独立聚合（独立表、独立状态机、独立超时）。UI 集成：

- agent 调 `request-input` → worker daemon 先写 InputRequest 行 → **再**写一条 Conversation Message：
  - `content_kind = agent_finding`
  - `content` = 问题文本
  - `input_request_ref` = InputRequest.id（**Message 新增 nullable 字段**）
- Bridge 渲染该 Message 时检测 `input_request_ref` 非空 → 飞书 vendor 形态附带 [选项 A] [B] [自己写] [取消] 按钮
- 用户响应路径（三选一）：
  - 点卡片按钮 → Bridge 收 card.action → 调 `InputRequest.respond` + 写一条 Message（`kind=text, from=user, content=answer`）留痕
  - 在 thread 内 @bot 自由文本 → 写 Message → supervisor 解析意图 → 调 `InputRequest.respond`
  - 用 `/answer <task_id> <text>` slash 命令（详见 § 6）→ Bridge 直接调 `InputRequest.respond` + 写 Message 留痕
- Bridge 监听 InputRequest 状态变化 → InputRequest done/timed_out/canceled → `update_card` 把按钮置灰，显示"✅ 已答复 X" / "⏰ 已超时" 等

**这是 update_card 的合法使用**（状态机驱动的一次性变更），不是 ADR-0016 驳回的"周期 refresh"。

**`task.conversation_id = null` 的 fallback**：b/c/d 来源 task 上 agent 调 `request-input` 时若 task 没绑 conversation → 自动触发 § 10.4 硬规则 bind 到 `notification.default_channel`，然后再走上面正常路径。这是唯一一个"无需用户主动就自动建 conversation"的场景（InputRequest 不响应 task 卡死，必须有兜底）。普通 progress milestone 不走 fallback。

### 6. Slash 命令前置到 v1（D2 模式）

[09 § 9](../architecture/tactical/bridge/01-feishu-integration.md) 原把 D2 (Slash 命令) 标为"⏸ 后置"，本 ADR 把其中一条提到 v1：

- `/answer <task_id> <text>` —— 响应 task 当前的 InputRequest

理由：input 响应是高频 + 结构化场景，自由文本 @bot 走 supervisor 解析增加延迟 + 误解风险，slash 直达更可靠。其他 slash 命令（`/dispatch` 等）按需后置。

Slash 命令路径**不经 supervisor**，Bridge 直接调对应领域 API + 写 Message 留痕。

### 7. Task 创建流程升格为标准化"开台"动作

a 路径下用户 @bot → supervisor 决定开 Task → 同步：

1. Scheduling BC create Task
2. Conversation BC create Conversation (kind=task) + 关联 channel binding（来自当前用户所在飞书渠道）
3. Task.conversation_id ← Conversation.id（同事务，[ADR-0014](0014-event-sourcing-level.md) L1 同事务双写）
4. Conversation BC emit `conversation.opened`（已有事件，无需新增）
5. Bridge 订阅 → 发 Task root card 到 vendor 当前位置 → 该 card 形成 thread root → 回写 conversation.primary_channel_thread_key
6. supervisor 立即写一条 Message（`supervisor_summary`：「收到，正在分析 / 寻找 worker / ...」）

后续 supervisor 的分析、worker 的进展、用户的回应都自然流进同一 Conversation。

### 8. Worker daemon 通过 CLI 写 Conversation

[07-worker-model.md](../architecture/tactical/workforce/01-worker-model.md) 已有 worker daemon 暴露本机 unix socket 让 agent 调 CLI 的机制。worker daemon 自己（不是 agent）也可以通过等价路径写 Conversation：

- worker daemon 进程内 → 调 center 的 `conversation add-message` API（gRPC 长连复用，[01-bounded-contexts.md BC4](../architecture/strategic/03-bounded-contexts.md)）
- 不一定走 CLI 命令；实现上是 worker daemon 直接 RPC
- 但语义对齐："worker daemon 是 Conversation 的一个 actor"

### 9. 显式驳回多 tracker（1:N）

[ADR-0016](0016-task-progress-via-bound-thread.md) 讨论里倾向 1:N（多用户多 tracker），本 ADR 退回 **1:1**：

- v1 单用户场景，1:N 无明确需求
- 多渠道镜像（一 Conversation echo 到多 Conversation）是 Conversation BC 的 v2+ 议题（[roadmap](../roadmap.md)）
- "另一个用户想看 T-42" 的 v1 答复：去 owner 的 conversation 找 / 用 CLI inspect

### 10. 懒创建路径

b/c/d 来源 task 的 conversation 从 null → 非 null 的触发场景：

#### 10.1 用户主动 CLI

```
agent-center task bind-card <task_id> --channel=feishu --to=<conversation_id>
agent-center task bind-card <task_id> --channel=feishu --auto       # 新建 conversation + 推到 default channel
```

用户在 VPS shell / 开发场景的兜底通道。

#### 10.2 用户主动 飞书 slash

`/track <task_id>` —— Bridge 收 slash → **直接**调 bind-card，绑到当前 thread。不经 supervisor。属于 § 6 D2 slash 首批命令之一。

#### 10.3 用户主动 飞书 @bot 自由文本

"盯一下 T-42" —— Bridge 收消息 → 转 supervisor 解析意图 → supervisor 调 bind-card。烧 LLM；兜底用（用户不知道 slash 命令时仍能用自然语言）。

#### 10.4 Center 硬规则兜底（InputRequest fallback）

唯一一个**非用户主动**的自动 bind 场景。触发条件 + 行为：

- 触发：agent 调 `request-input` → 写 InputRequest 行 → 关联 task.conversation_id 为 null
- 行为：center 内部规则自动调 bind-card，目标 = `notification.default_channel` 配置项指定的渠道
- 默认渠道未配 → InputRequest 创建失败，task fail（reason=`no_input_channel`，配 message）

不引入"普通 progress milestone 自动 bind"。原因：progress 没人看丢了不致命，task 仍能跑完；InputRequest 没人响应 task 会卡死，必须保证有响应通道。

#### 10.5 默认渠道配置

center config 新增 `notification.default_channel`：

```yaml
notification:
  default_channel: "feishu:user:hayang:dm"   # v1 单用户场景
```

v1 单用户场景值就是 user hayang 的飞书 DM。配置为空 → § 10.4 fallback 不可用，b/c/d task 的 InputRequest 直接 fail。

具体配置 schema 见 [implementation/04-configuration.md](../implementation/) (TBD)。

## Consequences

正面：

- **模型统一**：所有 task UI 都是 Conversation Message；没有"卡片类型 / 推送机制"特殊概念
- **复用现有机制**：Conversation BC / Bridge outbound / message_added 事件链路全套用；无新组件
- **InputRequest 自然融入**：是 Message 附带 input_request_ref，不是独立的"卡片"流
- **slash 命令兼顾效率与可靠**：input 响应不烧 LLM 不延迟
- **删除多余抽象**：撤回 `task_progress` content_kind、撤回 `task.bound_card_json` / `bound_card_json_list`、撤回 `task.progress_milestone_reached` 事件、撤回 ADR-0016 的多 tracker 模型

负面 / 待跟进：

- **Conversation 表会因 task 类型行增长**：每个用户发起的 task 一行；接受（Conversation 表本就是高基数实体，且 task done 后 archived 状态过滤）
- **Conversation kind 枚举多一个值**：v1 4 个（dm / adhoc / group_thread / task），可控
- **InputRequest 同事务双写 Message**：写 InputRequest 行 + 写 Message 必须同事务（[ADR-0014](0014-event-sourcing-level.md) L1）；额外约束但合理
- **Bridge 渲染依赖 join InputRequest 表**：发 Message 时如果 input_request_ref 非空，要 join 取当前状态 / options；增加渲染路径复杂度，可接受
- **多用户场景推到 v2**：v1 单用户够用；将来上多用户 / 团队，需要 Conversation 镜像 / forward 机制

## Alternatives Considered

### A. ADR-0016 原方案（bound_card_json + task_progress content_kind）

- Pro: 不动 Conversation 模型
- Con: 把"进度"切成独立机制，跟 InputRequest / 完成报告 / supervisor 分析等其他 task UI 路由分叉；多 tracker 复杂度无意义
- 不选（被本 ADR supersede）

### B. (i) 强必带：所有 task 创建时同步建 Conversation

- Pro: 模型最齐
- Con: Issue spawn / CLI / supervisor 自主开的 task 产生大量空 conversation，DB 噪声
- 不选

### C. (ii) 全员懒：所有 task 默认无 conversation，首次 IO 触发创建

- Pro: DB 最干净
- Con: 每个写 conversation 路径都要懒创建逻辑，铺散；a 场景下"用户已经在飞书说话"还要走懒创建路径很怪
- 不选

### D. (iii) 按来源决定（本 ADR 采用）

- Pro: a/e 自然 eager、b/c/d 懒创建分清；下游写路径只需 null-safe，懒创建逻辑集中在 `task bind-card` 入口
- Con: 规则分两支，但分得合理
- 选

## 影响范围

- 标记 [ADR-0016](0016-task-progress-via-bound-thread.md) status = `Superseded by ADR-0017`
- 改写 [02-task-model.md](../architecture/tactical/scheduling/01-task-model.md)：
  - Task aggregate 加 `conversation_id` (nullable)；去掉 ADR-0016 提到的 `bound_card_json` / `bound_card_json_list` 规划
  - DispatchEnvelope 加 `conversation_id` 字段
  - 新 CLI: `agent-center task bind-card <task_id> --channel=feishu --auto`（懒创建入口）
  - 新增 Task 相关事件描述：a 路径下 task.created 与 conversation.opened 同事务发；b/c/d 路径下 task.created 时 conversation_id=null
  - 删除 ADR-0016 提到的 `task.bound_card_requested` / `task.progress_milestone_reached` 事件（不再引入）
- 改写 [04-input-required.md](../architecture/tactical/scheduling/02-input-required.md)：
  - Step 6-7 改写：InputRequest 投递路径 = 写 Message (kind=agent_finding, input_request_ref=<id>) 到 task.conversation_id；为 null 时 fallback 行为 TBD（可能 supervisor 主动 bind-card 或直接发 user DM）
  - 加 Slash `/answer` 响应路径
- 改写 [09-feishu-integration.md](../architecture/tactical/bridge/01-feishu-integration.md)：
  - § 6 渲染表加：`agent_finding + input_request_ref != null` → 飞书 interactive card with buttons
  - § 7 Bound Card 机制下移：Issue 的 bound_card 仍按原方式；Task 走"创建时同步建 Conversation"路径（不复用 issue 的 bind-card CLI 名字）；两套机制本质相同但通道不同
  - § 9 D2 Slash 命令前置（新增 `/answer` 处理 + 留口 `/dispatch` 等）
  - 新增 § Task Conversation 创建流程（按 § 7 模板写）
- 改写 [12-conversation.md](../architecture/tactical/conversation/01-conversation.md)：
  - Conversation kind 枚举加 `task` + 描述跟 task 1:1 关系
  - Message schema 加 `input_request_ref` (nullable uuid FK)
  - GC / archive 策略：kind=task 的 Conversation 在 task done/abandoned 时标 archived
- 改写 [01-bounded-contexts.md](../architecture/strategic/03-bounded-contexts.md)：
  - Conversation kind 枚举 + 术语 Message 加 input_request_ref 字段
  - Task aggregate 列字段加 conversation_id
  - Scheduling BC 事件去掉 ADR-0016 引入的 task.bound_card_requested / progress_milestone_reached
- 实现层 02-persistence-schema (TBD)：
  - tasks 表加 `conversation_id` (nullable uuid FK)
  - conversations 表 `kind` enum 加 `task`
  - messages 表加 `input_request_ref` (nullable uuid FK)
- 实现层 [04-configuration.md](../implementation/) (TBD)：新增 `notification.default_channel` 配置项（§ 10.5）
