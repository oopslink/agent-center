# 0021. Issue 即 Conversation：1:1 绑定 + 所有 Issue IO 走统一 Message 时间线

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-20 |
| Supersedes | [ADR-0009](0009-issue-conversation-decoupled-via-bridge.md)（§ 1 Issue ↔ Conversation 解耦 + § 3 Issue Bound Card 字段结构）/ [ADR-0020](0020-card-confined-to-bridge-bc.md)（中间方案，被本 ADR 升级为统一方案）|
| Refines | [ADR-0017](0017-task-as-conversation.md)（把 Task ↔ Conversation 1:1 模式推广到 Issue）|
| Affects-Wording | [ADR-0016](0016-task-progress-via-bound-thread.md)（"bound card" 措辞）|

## Context

设计演进链：

- [ADR-0007](0007-conversation-as-unified-session.md) 引入 Conversation 作渠道无关会话层，初稿 Issue ↔ Conversation 1:1 + IssueComment = Message
- [ADR-0009](0009-issue-conversation-decoupled-via-bridge.md) **驳回 1:1**，选 N:N 弱关联 + IssueComment 独立表，理由是 R1「Issue 评论可能需要独有维度（is_pinned / linked_task_ids / reactions / parent_comment_id 等），把它绑死 Conversation Message（通用模型）会牺牲灵活性」
- [ADR-0017](0017-task-as-conversation.md) 让 **Task ↔ Conversation 1:1**，IssueComment 之外建立了第二条"Conversation 即承载 IO"的路径（仅限 Task）
- [ADR-0020](0020-card-confined-to-bridge-bc.md) 修正 Issue 字段泄漏 Card 概念问题（`bound_card_json` → `channel_binding_json`，4 字段 VO），是**中间方案**

2026-05-20 P3 推进 Discussion BC 重组时再次盘点 ADR-0009 R1 反驳前提，发现：

**R1 列的 5 个具体担忧字段，没一个是"Issue 独有"维度**：

| ADR-0009 当年说 | v1 状态 | 现在判定 |
|---|---|---|
| `is_pinned` | v1 不实现 | **普适**：Conversation Message 加 is_pinned 同样有用（置顶 supervisor_summary / agent_finding 之类）|
| `parent_comment_id` | v1 不实现 | **普适**：嵌套回复是通用对话能力 |
| `reactions` | v1 不实现 | **普适**：表情反应通用 |
| `linked_task_ids` | v1 选择性用 | **普适**：supervisor_summary 提及 task 也能用 |
| `kind = conclusion_draft / task_proposal / finding` | v1 实际用 | 扩展 Conversation Message.content_kind 即可（轻量改动）|

5 个字段全部是**普适 Message 能力**或**轻量 content_kind 扩展**，没一个证明 IssueComment "必须脱离 Conversation Message 通用模型"。**R1 反驳前提实际不成立**。

ADR-0009 其它驳回理由的处境：

- R2「`notify-conversation` 命名暗示外发」—— 已通过 CLI 改名 `conversation add-message` 解决，不再相关
- R3「Issue 可从多 conversation 触发」—— **依然成立但不冲突**：Issue 有 1 个专属 Conversation 承载议事 + 其它 conversation（DM / group thread）通过 `related_conversation_ids` 弱关联做触发血缘；不矛盾
- R4「CLI / Web Console 开的 Issue 可能没 conversation」—— **被 ADR-0017 路线解决**：按来源决定创建时机（同 Task a/b/c/d/e 路径模板）

**所以 ADR-0009 当年驳回 1:1 的理由现在要么不成立、要么已被 ADR-0017 路线解决**。继续保留 ADR-0009 的 N:N + IssueComment 独立表方案 + ADR-0020 的中间字段精简，是历史包袱，不是当下最佳选择。

更重要的是 ADR-0017 之后 Issue 跟 Task 的设计**模式不对称**：

- Task ↔ Conversation 1:1 强引用，所有 Task IO 走 Message
- Issue ↔ Conversation N:N 弱关联，IssueComment 独立表 + Bound Card 独立机制
- 用户 / agent 视角：「我在跟一个 Issue 讨论」vs「我在跟一个 Task 讨论」其实是同样的"在跟一个领域 thread 互动"，没有性质差异 —— 当前两套机制是**实现差异**，不是**领域差异**

DDD 视角：**Issue 跟 Task 在领域语义上都是"领域 thread"**（议事 thread / 干活 thread），应该共享同一个会话承载模型。

## Decision

**Issue ↔ Conversation 1:1：Issue 关联唯一一个 `kind=issue` Conversation；所有 Issue IO（评论 / 结论 / 系统消息 / 用户响应 / supervisor 分析）都是该 Conversation 内的 Message；没有独立 IssueComment 实体。**

### 1. Issue ↔ Conversation 1:1（同 ADR-0017 Task 模板）

Issue 表加 `conversation_id` (nullable，按来源决定时机)。创建时机分两支（同 ADR-0017 § 1）：

| Issue 来源 | conversation 创建 |
|---|---|
| 飞书用户 @bot / slash 直接开 issue（"讨论一下 X"）| Issue 创建时**同步**新建 Conversation + Bridge 发 Issue root card 形成 thread |
| Web Console 新建 | 同上（Web Console 是 Conversation 另一个 channel binding） |
| Supervisor 自主开 issue（从用户对话推断意图）| 同步建：supervisor 知道 caller channel，立即关联 |
| CLI `agent-center issue open ...` | **不**新建，`issue.conversation_id = null`；后续 `issue bind-thread` 触发懒创建（同 ADR-0017 task bind-card 模板） |
| Worker (agent) `agent-center open-issue ...` | 同 CLI 路径（懒创建） |

写 IssueComment / 评论 IO 前 null-safe：`conversation_id != null` 才走 Conversation；为 null 时 issue 仍可跑（CLI inspect 可查），但飞书侧无可见性。懒创建路径见 § 7。

### 2. Conversation kind 加 `issue`（重新引入）

[conversation/01](../architecture/tactical/conversation/01-conversation.md) Conversation kind 枚举：

```
dm / group_thread / adhoc / notification / task / issue
```

`kind=issue` 跟 Issue 1:1（issue.conversation_id ↔ conversation.id）。生命周期与 issue 绑定：

- Issue 创建（同步建路径）→ Conversation 创建
- Issue concluded / closed / withdrawn → Conversation `status=closed`（保留全部历史；CLI `inspect conversation` 仍可查；用户翻飞书 thread 仍能看）

### 3. IssueComment 实体删除；所有 Issue IO 复用 Message

- 删 `issue_comments` 表 / IssueComment 实体 / IssueCommentRepository
- Discussion BC 内不再有 IssueComment 概念；议事讨论数据完全归 Conversation BC

| 原 IssueComment kind | 新 Message content_kind |
|---|---|
| `text` | `text` |
| `conclusion_draft` | `conclusion_draft`（**新增**） |
| `task_proposal` | `task_proposal`（**新增**） |
| `system` | `system` |
| `finding` | `agent_finding`（复用现有） |

Conversation BC 的 Message.content_kind 枚举扩展为：

```
text / system / agent_finding / supervisor_summary / conclusion_draft / task_proposal
```

（task_progress 不引入；保持 [ADR-0017 § 3](0017-task-as-conversation.md) 撤回 task_progress 的决定）

### 4. Issue 表去掉所有 channel 字段

由 [ADR-0020](0020-card-confined-to-bridge-bc.md) 引入的 `issue.channel_binding_json` 字段**完全删除**。Issue 跟 vendor 的关联**通过其 Conversation** 实现（Conversation 持 `primary_channel_thread_key`，由 Bridge 自动回写，同 ADR-0017 Task 路线）。

精简后的 Issue 表字段：

```
issue (
  id, project_id,
  title, description,
  opened_by_identity_id,
  opened_at,
  status,                       -- open | under_discussion | concluded | closed_no_action | closed_with_tasks | withdrawn
  concluded_at, conclusion_summary, concluded_by_identity_id,

  conversation_id,              -- nullable; 1:1 ref to conversation
  related_conversation_ids      -- JSON array (其它 conversation 触发血缘，保留)
)
```

**没有** `bound_card_json` / `channel_binding_json` / 任何 channel 字段。

### 5. Bridge BC 无需新增 `feishu_card_ledger` 表

[ADR-0020](0020-card-confined-to-bridge-bc.md) 计划引入的 `feishu_card_ledger` **取消**。所有 Issue / Task 跟 vendor 的双向同步走**统一路径**：

- Outbound：`conversation.message_added` → Bridge 订阅 → 投递到 thread
- Inbound：vendor msg → Bridge 按 `(channel, thread_key)` 反查 conversation_id → 调 `conversation add-message`
- Bridge 内 vendor msg id 缓存（`vendor_msg_ref`）已存在 Message 表，不需要额外 ledger

ADR-0020 的"Card 限制在 Bridge BC"原则**保留并升级**为更强约束：**领域 BC（Issue / Task / Conversation）都不持有 vendor 实现细节字段；Conversation 的 `primary_channel_thread_key` 是务实例外（[conversation/01 § 2 注释](../architecture/tactical/conversation/01-conversation.md)）**，但不扩散到 Issue / Task。

### 6. "Bound Card" 概念消失

- 删除 Discussion BC 的 "Bound Card" / "Channel Binding" 机制章节
- CLI 删除 `issue bind-card` / `bind-thread` / `unbind-*` / `rebind-*` 系列；**懒创建用 `issue bind-conversation` 命令**（同构 ADR-0017 § 10 `task bind-card`）：

```
agent-center issue bind-conversation <issue_id> --channel=feishu --to=<conversation_id>
agent-center issue bind-conversation <issue_id> --channel=feishu --auto      # 新建 kind=issue Conversation + 推到 default channel
```

- 飞书 slash `/track-issue <issue_id>`（v2，推迟）— 等价 Task 的 `/track`

### 7. Issue 创建时机懒创建路径（同 ADR-0017 § 10 模板）

| 路径 | 触发 | 行为 |
|---|---|---|
| 7.1 用户主动 CLI | `agent-center issue bind-conversation <id> --auto` | Discussion 新建 kind=issue Conversation + emit `conversation.opened` → Bridge 发 Issue root card 到 `notification.default_channel`（或 CLI 指定） |
| 7.2 用户主动 飞书 slash | `/track-issue <id>` (v2 推迟) | 同 ADR-0017 § 10.2 `/track` 模式 |
| 7.3 用户主动 飞书 @bot 自由文本 | "盯一下 Issue #7" | supervisor 解析意图调 `issue bind-conversation`（烧 LLM；兜底）|
| 7.4 Center 硬规则兜底 | v1 不引入（Issue 不像 InputRequest 那样"卡死"，没 conversation 不影响 issue 状态机推进；不强制 fallback）| 保持 7.1 / 7.3 是用户主动路径 |

### 8. CLI 命名与 facade

原 `issue comment <id>` 命令两种处理选项：

**采用 facade**：保留 `agent-center issue comment <id> --content=...` CLI；内部翻译为 `conversation add-message --to=<issue.conversation_id> --content=...`；用户视角不变。理由：用户视角"给一个 issue 加评论"是清晰意图，不该让用户每次都查 conversation_id。

新增 `agent-center issue conclude <id>` CLI 不变 —— 内部触发 supervisor"conclude flow"（写 conclusion_draft Message + spawn N Tasks）。

### 9. Issue ↔ 其它 Conversation 的弱关联（保留）

`issue.related_conversation_ids` JSON 数组**保留**：

- R1 用户在飞书 DM @bot 触发开 issue → 那个 DM Conversation 自动关联（血缘）
- R2 Supervisor 派子任务关联：supervisor 调 agent 做研究 → 那个 task Conversation 自动关联
- R3 用户手动 `agent-center issue link-conversation <issue_id> <conv_id>`

**用途**：触发血缘 / inspect 看 issue 时知道哪些会话提及过它。**不影响 IO 路径** —— Issue 的所有评论 / 议事 IO 仍只走 `issue.conversation_id` 这一个专属 Conversation。

### 10. Issue conclude 流程

跟 ADR-0009 § 3 当年描述类似，但 IO 全部在 Conversation 内：

```
1. 用户在 Issue conversation 内点 [得出结论] / CLI `issue conclude <id>`
2. Discussion 触发 supervisor "conclude flow"
3. Supervisor 读 Conversation Message 历史 + 写一条 conclusion_draft Message（含建议 Tasks 列表）
4. Bridge 渲染为带 [确认结论] [改后确认] [不做] 按钮的 supervisor_summary card
5. 用户响应：
   - [确认] → Discussion BC: issue.status=closed_with_tasks，调 TaskRuntime IssueConcludeSpawn 批量建 Tasks（同事务）
   - [不做] → issue.status=closed_no_action
   - [改改] → 改后回到 step 3
6. emit issue.concluded / issue.tasks_spawned（事件不变）
```

事件 schema 不变（issue.* 事件依然由 Discussion BC emit）。

### 11. UL 修订

[strategic/03-bounded-contexts § 1](../architecture/strategic/03-bounded-contexts.md) UL 表：

- **删** "IssueComment（讨论评论）" 条 —— 实体已删除
- **新增** Conversation kind 列表里加 `issue`
- **新增** Message.content_kind 扩展 `conclusion_draft` / `task_proposal`
- **保留** "Channel Binding (Conversation BC, Identity 子从属)"，删 ADR-0020 引入的 "Channel Binding (Discussion BC)" 条

## Rationale

1. **R1 反驳前提失效**：ADR-0009 当年担忧的 5 个 Issue 独有字段实际全是普适 Message 能力或轻量 kind 扩展
2. **跟 Task 路线对称**：ADR-0017 已让 Task 走通 1:1 路线；Issue 跟上是自然延伸，**两条线统一**就是收益
3. **删除概念冗余**：删 IssueComment 实体 + 删 Bound Card 机制 + 删 ADR-0020 中间方案的字段精简过渡态
4. **领域 thread 统一抽象**：Conversation 是所有领域 thread（议事 / 干活 / 闲聊）的承载层；Issue / Task 都是"特殊 subject of a Conversation"
5. **Bridge 职责更纯粹**：所有领域 ↔ vendor 同步走 `conversation.message_added` / `conversation.opened` 一条路径
6. **v1 修改成本低**：还没用户、schema 没 lock-in、CLI 没 lock-in；现在统一比 v2+ 改成本低 10×

## Consequences

### 战略层

- [strategic/03-bounded-contexts.md § 1 UL](../architecture/strategic/03-bounded-contexts.md)：
  - 删 IssueComment 条
  - Conversation kind 加 `issue`
  - Message.content_kind 加 `conclusion_draft` / `task_proposal`
  - 删 ADR-0020 引入的 "Channel Binding (Discussion)"
- [strategic/03-bounded-contexts.md § 2 BC2 Discussion](../architecture/strategic/03-bounded-contexts.md)：
  - 核心聚合改为仅 Issue（删 IssueComment）
  - 核心事件清单微调（`issue.commented` 删除，议事消息走 `conversation.message_added`；但 `issue.opened` / `issue.concluded` / `issue.withdrawn` / `issue.tasks_spawned` 保留）
  - 删除 BC2 Discussion 的 `issue_comments` 表（[strategic § 主表归属](../architecture/strategic/03-bounded-contexts.md)）
- [strategic/03-bounded-contexts.md § 2 BC7 Bridge](../architecture/strategic/03-bounded-contexts.md)：
  - 不引入 ADR-0020 计划的 `feishu_card_ledger` 表
- [strategic/03-bounded-contexts.md § 3 Context Map](../architecture/strategic/03-bounded-contexts.md)：
  - Issue ↔ Conversation 关系从 "弱关联 (JSON id list)" 改 "Shared Kernel / 1:1（issue.conversation_id 强引用，同事务双写）"
- [strategic/01-subdomain-classification.md](../architecture/strategic/01-subdomain-classification.md)：
  - Discussion BC 分类不动（依然 Core）
  - 描述里相关"IssueComment 独立"措辞改"Issue 议事走 Conversation"

### 战术层

- [tactical/discussion/](../architecture/tactical/discussion/)：
  - 当前 `01-issue-discussion.md` 重写为 P3 模板 `00-overview.md`（单聚合 BC，聚合详情合并）
  - 删除 IssueComment 实体描述
  - 重写 Issue 创建路径（含 a-e 来源 + 懒创建）
  - 删除 "Bound Card 机制" 章节
  - § X.3 Domain Service 仅保留 IssueLifecycleService（Bound Card svc 删除）；ChannelBinding VO 删除
- [tactical/conversation/01-conversation.md](../architecture/tactical/conversation/01-conversation.md)：
  - kind 枚举加 `issue` + 描述跟 issue 1:1 关系（同 task kind 节）
  - Message.content_kind 加 `conclusion_draft` / `task_proposal` 行
  - § 6 Inbound 流程 Step 1 删除（不再有 "bound card thread → IssueComment" 例外路径；统一走 Step 2 conversation 路由）
- [tactical/bridge/01-feishu-integration.md](../architecture/tactical/bridge/01-feishu-integration.md)：
  - § 7 "Bound Card 机制（Issue）" 章节**整段删除**
  - § 4 Inbound 流程 Step 1（issue bound_card 反查）**整段删除**
  - § 5 Outbound 触发表删除 `issue.bound_card_requested` / `issue.channel_binding_requested` 行
  - § 7.5 Task Conversation 创建流程章节标题改 "Issue / Task Conversation 创建流程"（合并）
  - § 6 渲染规则加 `conclusion_draft` / `task_proposal` content_kind 行
- [tactical/task-runtime/](../architecture/tactical/task-runtime/)：不动
- [tactical/cognition/01-supervisor-model.md](../architecture/tactical/cognition/01-supervisor-model.md)：
  - "对 Issue 的工具" 章节小改：`issue comment` 命令语义不变（facade）；`issue bind-card` → `issue bind-conversation` 改名
- [tactical/agent-harness/02-skill-cli-tooling.md](../architecture/tactical/agent-harness/)（若存在）：CLI 索引同步

### 实现层

- [implementation/02-persistence-schema.md](../implementation/) (TBD)：
  - 删 `issue_comments` 表
  - `issues` 表加 `conversation_id` (nullable uuid FK)
  - `issues` 表删 `bound_card_json` / `channel_binding_json` 字段
  - `conversations` 表 `kind` enum 加 `issue`
  - `messages` 表 `content_kind` enum 加 `conclusion_draft` / `task_proposal`
- [implementation/04-configuration.md](../implementation/) (TBD)：`notification.default_channel` 配置项扩展用途说明（issue 懒创建也用）

### CLI

- 保留 `agent-center issue comment <id> --content=...`（facade，内部转 `conversation add-message`）
- 保留 `agent-center issue open / conclude / close / withdraw`
- **删除** `agent-center issue bind-card / unbind-card / rebind-card`
- **新增** `agent-center issue bind-conversation <id> --channel=feishu --to=<conv_id> | --auto`

### ADR 措辞

- ADR-0007 status 不动（"Refined by 0009" 改 "Refined by 0021"）
- **ADR-0009 status → `Superseded by 0021`**（§ 1 + § 3 被推翻；§ 2 Bridge 模式作为通用原则保留 —— Bridge ACL 性质本就不变）
- **ADR-0020 status → `Superseded by 0021`**（中间方案被升级方案取代）
- ADR-0016 / ADR-0017 措辞引用更新（仅措辞）
- ADR-0017 加 `Refined by 0021`（Task 路线被推广到 Issue）

### Blueprint

- [ddd-blueprint § 2.1](../ddd-blueprint.md) ADR 表加本 ADR 行
- "最后更新" 改 2026-05-20，注释加 ADR-0021 摘要
- § 1.2 战术状态表中 IssueComment 行删除
- § 4 推进路径 注：P3 Discussion 重组按本 ADR 新数据模型做

### conventions

- [conventions § 12](../../rules/conventions.md) 命名表：
  - 删 ADR-0020 加的 "Channel Binding (Discussion) vs Bound Card" 行
  - 加新行：议事 thread 跟干活 thread 的承载层统一为 Conversation（强制规范）

## Alternatives Considered

### (A) 保留 ADR-0020 中间方案

- Pro: 零额外工作量；半步剥离已完成
- Con: 两条线（Issue / Task）模式永久不对称；IssueComment 独立表持续 v1+ 维护；Bound Card 机制保留增加概念复杂度；ADR-0009 R1 反驳已不成立但设计为之"还债"
- **驳回**：v1 还没 lock-in，统一更优；中间方案是过渡态，应升级为终态

### (B) 完全剥 channel binding 但保留 IssueComment 独立表

类似前期讨论的 Y 方案：把 channel binding 移到 Bridge BC，但 Issue 仍保留 IssueComment 独立表。

- Pro: 关注点剥离最彻底
- Con: 不解决 Issue / Task 模式不对称；不解决 IssueComment 维护成本；中等改动但收益不如统一方案
- **驳回**：既然要动，不如一次到位

### (C) 当前方案（Issue ↔ Conversation 1:1，IssueComment = Message）

- Pro: 跟 Task 路线完美对称；删除 IssueComment 实体 + Bound Card 机制 + ADR-0020 中间方案；领域 thread 统一抽象；Bridge 职责更纯粹
- Con: 推翻 ADR-0009 + ADR-0020 工作量大；Message.content_kind 需要扩展；Conversation kind 加 `issue`（ADR-0009 当年删的，承认当时判断错）
- **选**：当年驳回理由现在不成立，且 ADR-0017 已为 Task 走通路线 —— Issue 跟上是自然且必要

### (D) 让 IssueComment 跟 Message **共享一张物理表 `messages`** 但保留两个逻辑实体

- Pro: 数据层统一但概念层保留两类实体（Discussion BC 内的 IssueComment "view" + Conversation BC 内的 Message "view"）
- Con: 概念冗余依旧；解耦不彻底；让两个 BC 共享物理表是反 DDD 设计
- **驳回**：违反 BC 边界 + 概念冗余

## References

- 起源：2026-05-20 P3 推进 Discussion BC 重组时跟用户深度讨论 ChannelBinding / Bound Card 归属问题；用户提出"Issue 本身就是一个 thread"洞察，触发系统性反思
- DDD 方法论：[conventions § 0](../../rules/conventions.md)
- 受影响 ADR：
  - [0007](0007-conversation-as-unified-session.md) Refined by 0021
  - [0009](0009-issue-conversation-decoupled-via-bridge.md) Superseded
  - [0017](0017-task-as-conversation.md) Refined by 0021（Task 路线被推广）
  - [0020](0020-card-confined-to-bridge-bc.md) Superseded（中间方案）
  - [0016](0016-task-progress-via-bound-thread.md)（措辞）
- 战略层影响：[strategic/03-bounded-contexts](../architecture/strategic/03-bounded-contexts.md) / [strategic/01-subdomain-classification](../architecture/strategic/01-subdomain-classification.md)
- 战术层影响：[tactical/discussion/01-issue-discussion](../architecture/tactical/discussion/01-issue-discussion.md) / [tactical/conversation/01-conversation](../architecture/tactical/conversation/01-conversation.md) / [tactical/bridge/01-feishu-integration](../architecture/tactical/bridge/01-feishu-integration.md) / [tactical/cognition/01-supervisor-model](../architecture/tactical/cognition/01-supervisor-model.md)
- 蓝图：[ddd-blueprint § 2.1 ADR 表](../ddd-blueprint.md)
- 规则：[conventions § 12 命名](../../rules/conventions.md)
