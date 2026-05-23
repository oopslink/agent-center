# 0022. Conversation 不对齐 IM 软件的 channel/thread 层级模型

| Field | Value |
|---|---|
| Status | **Superseded by [ADR-0039](drafts/0039-conversation-business-model-v2-unified.md)**（2026-05-23 v2 设计阶段后）|
| Date | 2026-05-21 |
| Superseded-By | [ADR-0039 Conversation 业务模型 v2 统一](drafts/0039-conversation-business-model-v2-unified.md) —— v2 撤回 vendor 后「channel 是 Bridge routing hint」描述失效；ADR-0039 升级为「channel 是业务一等公民」（CV1 / ADR-0032），跟本 ADR 「业务对象层级是 source of truth」精神一致并更彻底 |

> ⚠️ **v2 读者**：请直接看 [ADR-0039](drafts/0039-conversation-business-model-v2-unified.md)；本 ADR 是 v1 历史记录，内含「channel 是 routing hint」等 vendor-coupled 描述已不适用。

## Context

讨论起点：用户提出聊天软件里 `conversation / channel / thread` 是有层级结构的 ——

```
conversation
├── channel
│   ├── message
│   │   └── thread
│   └── message
└── direct message / group chat
```

并追问：

1. 本项目的 `Conversation` 模型是否应该跟 IM 软件对齐？
2. IM 软件的 `channel/chat/thread` 层级结构是否有借鉴意义？

讨论中盘了一遍当前模型与 IM 模型的差异、各自服务的领域问题、两者关键张力。本 ADR 把结论记下来，避免后续重复讨论。

### 当前模型的"反向设计"

[conversation/00-overview](../architecture/tactical/conversation/00-overview.md) + [conversation/01-conversation](../architecture/tactical/conversation/01-conversation.md) 当前模型：

| | IM 软件 | agent-center |
|---|---|---|
| **结构主角** | channel / thread（容器） | `Conversation`（时间线） |
| **conversation 角色** | 抽象上下文（次要） | 一等公民聚合根 |
| **channel / thread 角色** | 一等公民（容器） | 退化为 vendor 路由 hint（`primary_channel_hint` + `primary_channel_thread_key` 两个字符串字段） |

这是**故意倒过来**的，因为 agent-center 不是 IM 产品 —— 是 agent 编排系统，IM（飞书）只是若干 I/O channel 之一（[conversation/00-overview § 0.1](../architecture/tactical/conversation/00-overview.md)：会议室隐喻）。

### IM 层级结构服务什么

IM 软件的 `channel → chat → thread → message` 是为下面 4 件事服务的：

| 能力 | 解决的问题 |
|---|---|
| A. 容器 / scoping | 把无序消息按"工作场景"装盒 |
| B. 默认落地页 + drill-down 导航 | 进 channel 看概况，进 thread 看细节 |
| C. 生命周期解耦 | channel 长期存在、thread 独立 archive |
| D. 访问边界 | channel 控制谁能进；thread 控制谁参与 |

### agent-center 对应需求盘点

| IM 能力 | 本项目有需求吗 | 现在怎么解决 | 借鉴价值 |
|---|---|---|---|
| A. 容器 / scoping | **有，但已在另一条轴上**：Project / Issue / Task 本身就是业务容器 | 业务对象层（`task.project_id` / `task.issue_id` / `task.parent_task_id`），不靠 Conversation 层级 | **低**（被业务层覆盖） |
| B. 默认落地页 + drill-down | **有真实需求** | 当前靠 [Observability 投影](../architecture/tactical/observability/) + Web Console / CLI 拼出来 | **中** —— 但是 **view 层**问题，不是模型层问题 |
| C. 生命周期解耦 | **弱** | 每个 Conversation 跟业务对象一起生灭（task done → conversation close） | **低** |
| D. 访问边界 | v1 单 user 单 bot，**无需求** | 不存在 | **零** |

### 关键 insight

借鉴价值最高的 **B（drill-down 导航）不需要靠 Conversation 层级实现**，因为本项目的层级**已经存在**于业务对象轴：

```
业务对象轴（已有）：
  Project ─┬─ Task ──── Conversation (kind=task)
           ├─ Task ──── Conversation (kind=task)
           └─ Issue ─┬─ Conversation (kind=issue)
                     ├─ spawned Task ── Conversation (kind=task)
                     └─ spawned Task ── Conversation (kind=task)
```

如果再在 Conversation 上加 `parent_conversation_id` 形成另一条层级轴，就是**同一层级关系在两条轴上重复表达** —— 迟早会有 source-of-truth 不一致 bug（Issue 跟它的 Conversation 是一种描述；Issue's Conversation 跟 Task's Conversation 又一种描述，哪个为准？）。

## Decision

**Conversation BC 不对齐 IM 软件的层级模型** ——

1. **不**给 Conversation 加 `parent_conversation_id` / `channel_id` 等嵌套字段
2. **不**把 channel 提升为一等公民聚合根
3. **不**给 Message 加 `parent_message_id` 做 reply 链（input_request_ref 已显式建模 agent 请示 ↔ 用户回复，不需要 IM-style reply）
4. **保留**当前的"反向设计"：Conversation 是时间线主角，channel/thread 是 Bridge 路由 hint

但分开两个层面的"对齐"——

**A. 词汇 / UX 对齐**（继续做，零代价）：
- Bridge 边界 + CLI 输出 + 飞书卡片用 IM 词汇（`thread` / `channel` / `DM`）
- 用户读出来不出戏

**B. 领域模型对齐**（拒绝）：
- Conversation BC 内部结构**不**按 IM 嵌套层级建模
- 业务对象层级（Project → Issue → Task）已经覆盖了 IM 层级要解决的"容器 / scoping"问题

**真正值得借鉴的 IM 能力**（B drill-down 导航）走 view 层：
- Web Console / CLI `inspect project X` 输出"概况 + 子 Issue / Task 列表 + drill-down"
- 实现是聚合查询拼装（走业务对象关联），不是 Conversation 数据结构嵌套

## Consequences

正面：

- 守住"agent 编排 ≠ IM 产品"的领域定位 —— 模型不被 IM 习惯反向污染
- 单一 source of truth：业务对象层级 = 唯一层级；Conversation 层无层级，永远扁平
- 跨 vendor 时 Bridge 内部用 IM 词汇翻译，但不污染 Conversation BC（已有约束的延续）
- v1 模型不动，零迁移代价

负面 / 待跟进：

- `Conversation.kind` 仍把"受众场景轴（dm/group_thread/adhoc/notification）"和"业务对象耦合轴（task/issue）"拍扁在一个 6 值枚举里 —— 这是另一个张力，但跟 IM 对齐**无关**，已记入 [roadmap v3 Conversation 模型扩展](../roadmap.md)
- drill-down 导航的 UX 需求要在 Web Console / CLI inspect 命令里实现 —— 是 view 层 backlog，不是当前 ADR 的范围
- 后续要在 [conversation/01-conversation § 3 字段注释](../architecture/tactical/conversation/01-conversation.md)（已承认"务实而非纯粹"那段）附近加一句指针指向本 ADR，让读者直接看到"为什么 channel/thread 不上升为一等公民"的判断

## Alternatives Considered

### A. 完全对齐 IM 层级模型（拒绝）

把 Conversation 重构成 `Channel → Thread → Message` 的嵌套结构：
- Pro: 跟用户对 IM 的直觉对齐；多 vendor 时每个 vendor 的 channel/thread 嵌套结构有现成位
- Con:
  - `kind=task` Conversation 要变成"tasks channel 下的某个 thread"，但 Task 不属于哪个"房间"，它属于哪个 Project
  - 多 vendor 时同一 Task 在飞书是 thread、在 DingTalk 又是 thread —— 1:1 模型崩
  - 业务对象先有，承载它的 vendor 容器后有（Bridge 发 root card 后才回填 thread_key），时序上 IM"先有 channel 才有 thread"假设不成立
  - 跟 ADR-0017 / ADR-0021 的 1:1 强引用打架，需要重做这两个 ADR

### B. 部分对齐 —— 给 Conversation 加 `parent_conversation_id`（拒绝）

只引入"父 Conversation"指针表达层级，不动 channel 一等公民化：
- Pro: 改动小；可以表达"Issue Conversation 是 Task Conversations 的 parent"
- Con:
  - 层级关系跟业务对象层（`task.issue_id`）重复表达，双 source of truth
  - 维护成本：业务对象 spawn 关系变更时，要同步 conversation 父指针
  - 不解决 drill-down 视图的真实需求（视图问题，不是数据结构问题）

### C. 不动模型，把 drill-down 当 view 层补 backlog（采纳）

- Pro:
  - 不破坏当前"反向设计"的核心立意（[ADR-0007](0007-conversation-as-unified-session.md)）
  - 业务对象层级保持唯一 source of truth
  - drill-down 视图通过聚合查询拼装，可独立演进
- Con: drill-down 视图本身要等 Web Console / inspect 命令 backlog 排到 —— 但这不影响领域模型决策

## 影响范围

需更新 / 已更新的文档：

- [decisions/README.md](README.md) —— 加 0022 索引行
- [conversation/01-conversation.md § 3](../architecture/tactical/conversation/01-conversation.md) —— "primary_channel_hint / primary_channel_thread_key 对 Conversation 自身没有业务意义"那段注释附近加指针到本 ADR
- [conversation/00-overview.md § 0.1](../architecture/tactical/conversation/00-overview.md) —— "会议室隐喻"段落附近加指针到本 ADR
- [roadmap.md v3 Conversation 模型扩展](../roadmap.md) —— 已有；保持现状（讨论了多 vendor 落地后才回看的 `kind` 拆轴 / `group_thread` 嵌套等议题）

## 相关 ADR

- [ADR-0007 引入 Conversation 层](0007-conversation-as-unified-session.md) —— 本 ADR 延续 0007 的"渠道无关会话层"核心立意
- [ADR-0017 Task ↔ Conversation 1:1](0017-task-as-conversation.md) —— 业务对象 1:1 强引用，本 ADR 据此论证为何不能改成嵌套结构
- [ADR-0021 Issue ↔ Conversation 1:1](0021-issue-as-conversation.md) —— 同上
- [ADR-0020 Card 限制在 Bridge BC](0020-card-confined-to-bridge-bc.md) —— vendor 渲染细节归 Bridge 的同类约束
