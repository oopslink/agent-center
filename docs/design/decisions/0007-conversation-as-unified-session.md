# 0007. 引入 Conversation 层作为渠道无关的会话上下文

> ⚠ **v1-era ADR** — vendor / Bridge / deleted-ADR refs are v1 residue. v2 撤回了 Bridge BC + 飞书集成 (per [ADR-0031](drafts/0031-v2-drop-bridge-vendor-integration.md))；ADR-0017/0021/0022 已被 [ADR-0039](drafts/0039-conversation-business-model-v2-unified.md) supersede + 删除。本 ADR 仍 Accepted（核心结论延续）；只是 vendor 附属内容失效。

| Field | Value |
|---|---|
| Status | Accepted (Refined by → ; both deleted — see banner) |
| Date | 2026-05-16 |

> **2026-05-16 refinement** ( deleted per [ADR-0031](drafts/0031-v2-drop-bridge-vendor-integration.md)): 当年调整了本 ADR 中两点：
> - Issue ↔ Conversation 1:1 强绑 → 解耦为 N:N 弱关联；IssueComment 改为 Discussion BC 独立结构化实体
> -
>
> **2026-05-20 refinement** ( superseded by [ADR-0039](drafts/0039-conversation-business-model-v2-unified.md)): 上面**第 1 点**已被推翻：Issue ↔ Conversation 1:1 重新成为正解（跟 Task 路线对齐，ADR-0017 已被 ADR-0039 supersede），IssueComment 实体被删（= Message）；。本 ADR 的 Conversation 作为渠道无关会话层的核心立意**不变**，仅 1:1 / N:N 关系判定回归。
> 本 ADR 中"Conversation 是渠道无关的会话层"这个核心论点仍然成立。

## Context

> 以下 Context 内容是 v1 历史背景；v2 已删 vendor 集成 per ADR-0031，飞书 / Bridge 引用是 v1 残留。

初稿设计里有个 BC 叫 **Communication Gateway**，职责是" vendor ↔ agent-center 的 ACL"。它把两件事耦在一起：

1. 跟 vendor 的协议翻译（API / SDK / WS 处理）
2. "会话本身"的概念（thread / DM / 群上下文）

讨论中发现：

- 未来很可能接入 **DingTalk / Web chat（伴随 Web Console UI）/ Slack 等其它 channel**
- 同一个"会议"（如一个 Issue 的讨论）应该能跨渠道：用户上午在 一个渠道说话，下午切到 Web chat 继续，supervisor 不应被这种切换打乱
- 类比："一个会议室，有人在 vendor A、有人在 vendor B、有人在 Web 上 —— 都是同一个会议的上下文"
- 现有设计把"会话"绑死 单 vendor，未来重构成本高

## Decision

**把原 Communication Gateway 拆成两个 BC：**

- **Conversation（会话）** —— 一等公民。维护 "会议本身"：参与者、消息时间线、状态，**跟具体接入渠道无关**

>

引入新概念：

- `Conversation`（聚合根）+ `Message`（实体）
- `Identity`（聚合根）+ `ChannelBinding`（值对象）

上游 BC（Discussion / Scheduling / Cognition）**只跟 Conversation 打交道**，通过 `notify-conversation` 在会话内发声；。

。

详见 [architecture/12-conversation.md](../architecture/tactical/conversation/01-conversation.md) 与 [architecture/01-bounded-contexts.md](../architecture/strategic/03-bounded-contexts.md)。

## Consequences

正面：

- **抽象更准**：上游 BC 不再"知道 vendor"，符合 [conventions § 12 命名一致](../../rules/conventions.md#-12-命名一致)
- **Issue ↔ Conversation 1:1** 让讨论的载体清晰
- **Web Console 内嵌聊天** 未来作为另一个 channel 接入，跟 supervisor 的统一交互打通

负面 / 待跟进：

- 多一层抽象：v1 也得维护 Conversation / Message / Identity / ChannelBinding 四张表
- IssueComment ↔ Message 的关系需在 v1 拍定（[12-conversation.md § Issue ↔ Conversation 1:1](../architecture/tactical/conversation/01-conversation.md) 倾向把 IssueComment 实现为 Conversation 内的 Message）
- 历史名"Communication Gateway"需要全文档替换为新分工

## Alternatives Considered

### A. 保持 Communication Gateway 单层（耦合渠道与会话）

- Pro: 少一层抽象，v1 实现更快
- Con: 未来接 DingTalk / Web chat 时强制重构上游 BC（所有"vendor 消息"用例要改）；违反"开放-封闭"

### B. Conversation 抽象，但只在 v2 引入

- Pro: v1 不增加复杂度
- Con: v1 上游 BC 已经写死"vendor"接口，v2 重构成本远高于一次性引入抽象；数据迁移也复杂

### C. 完全收归 vendor SDK，不抽象

- Pro: 最简
- Con: vendor 锁死；Web Console 这种"非聊天 vendor"的输入入口无处安放

## 影响范围

需更新 / 已更新的文档：

- [architecture/01-bounded-contexts.md](../architecture/strategic/03-bounded-contexts.md) —— BC 列表 / UL / 上下文映射 / 命名表
- [architecture/12-conversation.md](../architecture/tactical/conversation/01-conversation.md) —— **新建**
- [architecture/06-supervisor-model.md](../architecture/tactical/cognition/01-supervisor-model.md) —— 同上
- [architecture/03-issue-discussion.md](../architecture/tactical/discussion/01-issue-discussion.md) —— 待修订（IssueComment ↔ Message 关系）
- [roadmap.md](../roadmap.md) ——
