# 0016. Task 进度跟踪走 bound thread + 进度消息流

| Field | Value |
|---|---|
| Status | **Superseded by [ADR-0017](0017-task-as-conversation.md)** |
| Date | 2026-05-17 |
| Superseded-Date | 2026-05-17 |

> **本 ADR 已被 [ADR-0017](0017-task-as-conversation.md) 接管**。讨论 ADR-0016 时把"进度推送"独立为一类机制（bound_card / task_progress content_kind / progress_milestone_reached event）；后续发现把 task 全部 IO（progress / InputRequest / supervisor 分析 / 用户回应）都收进 **同一个 Conversation**（Task ↔ Conversation 1:1）更统一、零新概念。ADR-0017 § 8 节流规则仍沿用 ADR-0016 § 3 的 5 条 milestone，但执行位置 / 数据通道完全变更。保留本文供 history 追溯，不要据此实现。

## Context

[ADR-0015](0015-agent-trace-not-in-events-table.md) 让 worker 进度数据由 TaskExecution 投影（实时摘要）+ BlobStore 归档（post-hoc）+ worker peek-trace RPC（按需深挖）三层承载。但这些都是"数据可拿到"，**没解决"飞书侧用户怎么看见 worker 工作进度"**：

- [09-feishu-integration.md § 5](../architecture/tactical/bridge/01-feishu-integration.md) 的 outbound 触发器只订阅 `conversation.message_added` / `issue.comment_added` / `issue.opened`
- TaskExecution 投影变化（`current_activity` 变了）**不是 domain event** → Bridge 不会自动推送
- 用户想"持续看进度"只能反复 @bot 问 → 每次烧一次 supervisor invocation 回静态卡 —— 不可接受

讨论中识别出可行模式有三档：

- **B (update_card refresh)**：Bridge 周期调飞书 `update_card` API 重渲染同一张卡片
- **B' (bound thread + 进度消息流)**：把卡片绑到 thread，后续进度作为 thread 内 message 追加
- **B'' (混合)**：主卡片 update + 关键 milestone 作消息

[09 § 7 Bound Card](../architecture/tactical/bridge/01-feishu-integration.md) 已经在 Issue 上跑通 bound card 模式（卡片 + thread + thread 内消息=IssueComment）。把同样机制扩展到 Task 复用度高、用户认知一致。

## Decision

### 1. 绑定对象 = Task（不是 TaskExecution）

用户问 "T-42 进度" 是关于"T-42 这件事"，跨多次 execution（重派单后绑定仍然有效）。

- TaskRuntime BC 给 `Task` 加 `bound_card_json` 字段（结构同构于 [03-issue-discussion.md](../architecture/tactical/discussion/01-issue-discussion.md) 的 `issue.bound_card_json`）
- 新 CLI `agent-center task bind-card <task_id> --channel=feishu --auto`（同构于 `issue bind-card`）

### 2. 复用 Issue bound_card 机制

- Bridge 收到绑定请求 → 发 Task 主卡片 → 拿 vendor msg_id + thread_key → 回写 `task.bound_card_json`
- 后续进度变化 → TaskRuntime BC emit 进度事件 → Bridge 订阅 → 推 message 到 bound thread

### 3. 进度推送语义节流（5 条 milestone，任一触发就发）

不按"每条 JSONL 都发"或"每个 tool call 都发"的物理节流；按**语义 milestone**：

| Milestone | 触发条件 |
|---|---|
| **status 变化** | TaskExecution status 转换（dispatched → working → input_required → completed / failed / killed） |
| **activity 类型变化** | `current_activity` 大类切换（如 "editing files" → "running tests"）；不按具体文件名变 |
| **长 tool call** | 单次 tool call 持续 > 30s（开始 + 结束各发一次） |
| **累计摘要** | 每累计 N 个 tool call 发一条摘要（N 默认 20，可配） |
| **用户在 thread 内询问** | 用户在 bound thread 里说话 → 立即 push 一条当前快照（不烧 supervisor） |

具体阈值（30s / N=20）走配置，不写死。

### 4. 新 content_kind = `task_progress`，进 messages 表

- Conversation BC 新增 `content_kind = task_progress`
- 进度消息**写入 messages 表**（保 [09 § 12](../architecture/tactical/bridge/01-feishu-integration.md)"飞书是无状态通道；thread 内每条消息都同步写入领域模块"承诺）
- 默认 `inspect conversation <id>` 时过滤掉 `task_progress`（不污染对话历史浏览）
- 保留期可短（默认 30 天 GC，可配，独立于普通 message 的保留期）

### 5. 显式驳回 B（update_card 周期重渲染）

不引入 Bridge in-memory tracking map + timer + vendor update_card API 调用。理由：

- 飞书 thread 内消息流是天然时间线，比"当下快照"信息量大
- update_card 看不到中间过程，用户离开聊天再回错过演变
- B' 复用现有 Bridge 事件驱动 outbound 流程，无新机制
- update_card 频率限制 / Bridge 状态管理 / TTL / 退出条件等都是额外复杂度

### 6. v1 不混合（驳 B''）

只走 B'。如果未来发现"长 thread 翻历史不直观" / "想要顶部固定一张总览卡"，再考虑 B''。v1 先用简单方案验证。

## Consequences

正面：

- **零新机制**：复用 § 7 bound card + § 5 outbound + content_kind 范式
- **天然时间线**：飞书 thread 本身就是历史可回滚视图
- **不烧 supervisor**：除最初绑定 + 异常 / 用户主动问，supervisor 不参与中间过程
- **跟 IssueComment / Issue bound card 同构**：用户认知一致，开发者复用代码
- **离开聊天再回不丢信息**：往上翻就行
- **跟 ADR-0014 / 0015 不冲突**：不引入新 domain event 类（progress event 是 TaskRuntime BC 内部事件，已统一在 events 表的 schema 内）；不依赖 trace 进 events 表

负面 / 待跟进：

- **messages 表会因 task_progress 类条目增长**：单 task 跟踪期内可能产生几十条消息；接受，配独立 GC（30 天）+ `inspect conversation` 过滤即可
- **长 tool call 阈值 / 累计 N 是经验值**：v1 默认 30s / N=20，跑起来后视用户反馈调整
- **bound thread 离开后不会自动停推**：v1 接受 push 到终态为止；如果用户嫌吵，靠按钮"停止跟踪" / 说话 "别盯了"（supervisor 处理）
- **看 thinking / 工具参数细节仍走 peek-trace**：卡片 / message 不直接塞长文本；按钮 `[查看 thinking]` `[查看工具参数]` → 触发 peek-trace（[ADR-0015 § 4](0015-agent-trace-not-in-events-table.md)）→ 返回作为 thread 消息

## Alternatives Considered

### A. 用户每次 @bot 问，supervisor 回静态卡

- Pro: 零架构改动
- Con: 反复 @bot 烧 LLM；用户体验差
- 不选

### B. update_card 周期重渲染同一张卡片

- Pro: 屏幕占用小
- Con: 看不到历史 / 中间过程；Bridge 多了 in-memory tracking map + timer + 飞书频率限制管理 + TTL 退出条件；新机制堆叠
- 不选（详见 Decision § 5）

### C. 当前决定：bound thread + 进度消息流

- Pro: 复用现有机制；天然时间线；零新 Bridge 自治
- Con: 节流规则需要语义判断（5 条 milestone）；messages 表略膨胀
- 选

### D. 混合 B''

- Pro: 一目了然 + 历史可查
- Con: 实现复杂度最高（两套机制并存）
- v1 不选，未来按需

## 影响范围

- 改写 [task-runtime/01-task.md](../architecture/tactical/task-runtime/01-task.md)：
  - Task aggregate 加 `bound_card_json` 字段（结构参考 issue.bound_card_json）
  - 新增 Task 相关事件：`task.bound_card_requested` / `task.progress_milestone_reached`（含 reason ∈ {status_change / activity_kind_change / long_tool_call_start / long_tool_call_end / cumulative_summary / user_inquiry}，配 message）
  - 新增 CLI：`agent-center task bind-card <task_id> --channel=feishu --auto` / `task unbind-card <task_id>`
- 改写 [09-feishu-integration.md](../architecture/tactical/bridge/01-feishu-integration.md)：
  - § 6 渲染表加 `task_progress` content_kind 行（飞书形态：thread 内 text message + 简短标签）
  - 新增 § Task Bound Card 机制（结构同构 § 7 Issue Bound Card）
  - 新增按钮 `[查看 thinking]` / `[查看工具参数]` / `[停止跟踪]` → action 路由到 `peek-trace` / `task unbind-card`
- 改写 [12-conversation.md](../architecture/tactical/conversation/01-conversation.md)：
  - content_kind 枚举加 `task_progress`
  - `inspect conversation <id>` 默认过滤 task_progress 类条目；`--include-progress` 显式开启
  - GC 策略加 task_progress 30 天保留（默认值，可配）
- 改写 [03-bounded-contexts.md](../architecture/strategic/03-bounded-contexts.md)：
  - TaskRuntime BC 核心事件加 `task.bound_card_requested` / `task.progress_milestone_reached`
  - 术语表 Message `content_kind` 枚举加 `task_progress`
- 实现层 02-persistence-schema (TBD)：`tasks` 表加 `bound_card_json TEXT`；`messages` 表无需 schema 改（`content_kind` 是 TEXT enum）
- [conventions § 16](../../rules/conventions.md) reason+message 双字段适用于 `task.progress_milestone_reached.reason`
