# 0031. v2 Drop Bridge BC / Vendor Integration（飞书等 IM 接入暂撤）

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-24 |
| Delivered | P10 § 3.9 — `91a2d40` (delete Bridge BC code). P10 F6 — `7cf72fb` (drop v1 bridge tables migration 0025). P12 S1 — `44b298a` (v1 vendor cleanup) + `d8a2d26` (lint guard). |
| Related | **Meta scope decision**，影响 Bridge BC7 + ADR-0009 / 0017 / 0020 / 0021 / 0022 + Phase 5 / 7 plans + F1 等 vendor 依赖 requirements；为后续 v2 Conversation 模型重设计（CV1-CV4）+ Web Console 升级（W1-W2）扫清污染 |

## Context

v2 议题 G1-G5 + E1 + G3 闭环过程中，用户对 Conversation 模型重新审视，提出：

> 「我觉得可以先把飞书和 bridge 这两个模块去掉，等 conversation 模型确立了，再来讨论如何和飞书对接」 —— 2026-05-22 #agent-center

随后用户进一步明确：

> 「V2 将 Bridge / 飞书重新对接 的设计和实现都删掉，在 v3 再从 0 开始讨论」

### 现有 Conversation 模型被 vendor 集成污染

[ADR-0017 Task as Conversation](../0017-task-as-conversation.md) / [ADR-0021 Issue as Conversation](../0021-issue-as-conversation.md) / [ADR-0022 Conversation 不对齐 IM 层级](../0022-conversation-not-aligned-with-im-hierarchy.md) 三个 ADR 都在「同时考虑 vendor 集成」语境下设计：

- `Conversation.primary_channel_hint` 字段挂 vendor channel ID（务实但污染）
- `Conversation.primary_channel_thread_key` 字段挂 vendor thread key
- ChannelBinding 表 vendor-coupled
- 「channel 是 routing hint」framing 限制了 channel 作为业务一等公民
- 大量 ADR 逻辑跟「飞书卡片渲染 / inbound 调 API」绑死

### v2 用户故事的新需求（待 CV 议题讨论）

用户提出新需求场景：多 human + agent 拉到话题群，群里开子话题 = Issue，开子话题可选父群部分聊天作 carry-over，Issue 派多 Task 各自有聊天 thread。

这些需求**纯业务层**（不依赖 vendor），但现有 Conversation 模型的 vendor 污染让重设计困难。

### 决定 reset 业务模型，vendor 接入 v3+ 从 0

撤回 Bridge / 飞书后：

- Conversation 模型可以**纯业务**重塑（CV1-CV4）
- channel 作为业务一等公民概念无 namespace 冲突（vendor 撤开）
- 用户主入口迁到 Web Console（W1）+ CLI（W2）

vendor 接入是 view / projection 层；v3+ 在 Conversation 业务模型确立后**重新设计 Bridge BC** 跟新业务模型适配。

## Decision

**v2 从设计 + 实现层面撤回 Bridge BC + 飞书集成 + 所有 vendor-stained 内容。v3+ 重新讨论 vendor 接入**。

### 1. v2 范围调整：撤回 Bridge BC + 飞书集成

具体撤回清单：

| 类别 | 物件 | 处置 |
|---|---|---|
| BC | BC7 Bridge | v2 删 |
| Tactical docs | `architecture/tactical/bridge/00-overview.md` / `01-feishu-integration.md` | v2 设计阶段删 |
| ADR（既往）| ADR-0009 (Issue-Conversation decoupled via Bridge)；ADR-0020 (Card confined to Bridge) | 状态保持（已被后续 superseded / cancelled），仅清 ref |
| ADR（重写）| ADR-0017 / 0021 / 0022 | **重写** —— 在 CV1-CV4 议题闭环后干净 supersede + 立新 ADR「Conversation 业务模型 v2」（或各自 rewrite，详 Step 3 决定） |
| Phase plans | `docs/plans/phase-5-bridge-feishu-outbound.md` / `phase-7-bridge-inbound-deploy.md` | v2 设计阶段删 |
| 战略层 | `architecture/strategic/03-bounded-contexts.md` § BC7 段 + Context Map 中 Bridge 节点 | v2 设计阶段删 |
| Requirements | F1 (飞书发起任务) 等 vendor 依赖项 | v2 设计阶段重写 |
| Conventions | § 9.y / § 13 中 vendor 相关条目 | v2 设计阶段重写 |
| 实现代码 | `internal/bridge/*` 飞书 SDK 集成等 | **代码删放开发计划阶段**；v2 设计阶段先标 deprecated；ADR 阶段不动代码 |

### 2. 用户主入口 v2 = Web Console + CLI

撤回 vendor 后 v2 用户接入面：

| 通道 | v2 职能 |
|---|---|
| Web Console | **升为全量 UI**（看 channel / 发消息 / 回 InputRequest / 发起任务）—— W1 议题 |
| CLI | **跟 Web Console 等价 channel / message / inputrequest 处理命令** —— W2 议题 |

### 3. v3+ 重新讨论 Bridge / vendor

[roadmap.md](../../roadmap.md) 加项目：

> **v3+ Bridge BC 重新设计**：基于 v2 确立的 Conversation 业务模型，重新设计 Bridge BC 怎么接入 vendor（飞书 / DingTalk / Slack / Web chat 等）；vendor 作为 Conversation 业务模型的 **view / projection**，业务模型不再被 vendor 污染。设计起点 = 0（不复用 v1 Bridge ADR/code）。

### 4. v2 议题列表更新（6 → 12 项）

原 v2 议题（已闭环）：

| 议题 | ADR | 状态 |
|---|---|---|
| E1 Worker enroll 轻量化 | 0023 | ✅ |
| G1 AgentInstance 一等公民化 | 0024 | ✅ |
| G2 agent:create 协议 | 0025 | ✅ |
| G4 MCP per-agent 注入 + SecretManagement BC | 0026 + 0027 | ✅ |
| G5 Skill File Mount + Supervisor 统一 | 0028 + 0029 | ✅ |
| G3 AgentAdapter 矩阵扩展 | 0030 | ✅ |

v2 新增议题（待讨论）：

| 议题 | 内容 | 状态 |
|---|---|---|
| Meta（本 ADR）| v2 drop Bridge / vendor | ✅（已 Accepted）|
| CV1 | Channel 升业务一等公民 | ⏭ 待讨论 |
| CV2 | Conversation Participant 显式建模 | ⏭ |
| CV3 | 跨 Conversation Message Carry-over | ⏭ |
| CV4 | Issue / Task 从 messages 派生入口 | ⏭ |
| W1 | Web Console 全量 UI（聊天 + 发起任务等）| ⏭ |
| W2 | CLI UX 增强（跟 Web Console 等价）| ⏭ |

### 5. ADR-0017 / 0021 / 0022 重写策略

[user 拍板 (Step 3)]：**讨论 CV1-CV4 清楚再写**。具体路径：

- CV1-CV4 议题闭环 → 沉淀出 v2 Conversation 业务模型
- 然后决定：3 个 ADR 各自 rewrite，**或** supersede 整套 + 立一个新 ADR「Conversation 业务模型 v2」（破立结合，更清晰）
- 本 ADR-0031 不强行规定哪种；待 CV 议题结束再定

### 6. 现有 vendor stained 字段处置

以下字段 / 表在 v2 删除（具体在 ADR 重写 + bulk purge 时执行）：

| 字段 / 表 | 来源 | v2 处置 |
|---|---|---|
| `Conversation.primary_channel_hint` | conversation/01 | 删 |
| `Conversation.primary_channel_thread_key` | 同上 | 删 |
| `ChannelBinding` 表 + 聚合 | conversation BC | 删 |
| Identity AR 中跟 vendor 强绑的字段 | conversation/02-identity | 重新审视；如 vendor_id / handle 可能留作内部 ID（不依赖 vendor 实体存在） |
| Bridge 相关 events（`channel.delivered` / `channel.delivery_failed` / `bridge.parse_failed`）| BC7 | 删 |

## Consequences

**正面**：

- Conversation 业务模型可以 clean-slate 重设计；不受 vendor 历史包袱拖累
- 「channel」作为业务一等公民有干净的 namespace
- 用户主入口归 Web Console + CLI，更聚焦 v2 thesis（个人工具 + 单机 + 不依赖 vendor）
- v3+ Bridge 重新设计时基于成熟的 Conversation 业务模型，vendor 集成成为干净的 projection 层

**负面 / 待跟进**：

- v2 没有 IM 接入 = 用户日常体验**集中在 Web Console**；如果 Web Console 体验不达标，v2 可用性受影响 —— W1 议题必须做好
- 现有飞书集成代码已写过（v1 Phase 5/7）—— 该部分 v2 删除等于扔掉一部分工程投入；用户已认账（决策权）
- 现有 ADR-0017 / 0021 / 0022 三个 Accepted ADR 要 rewrite —— 历史决策记录会改动；保留 superseded ADR 作历史可查
- v1 用户（如已部署 v1 飞书的 user）从 v1 → v2 升级时**失去飞书入口**，必须切到 Web Console；用户已声明「v2 不考虑向后兼容」
- v3+ Bridge 重新设计是大事，roadmap 加项；不限定时间表

## Alternatives Considered

### A. v2 保留 Bridge BC 但简化（只飞书 outbound，不要 inbound）

- 维持现有 vendor 集成但精简
- ❌ Conversation 模型仍被 vendor 污染；CV1-CV4 设计困难
- ❌ user 已表态「v3 重新从 0 开始讨论」，明确不要部分保留
- 否决

### B. v2 撤回飞书但保留 Bridge BC 框架（vendor-neutral）

- 删飞书 SDK 集成，留 Bridge BC 的 vendor-agnostic 设施
- ❌ Bridge BC 框架本身基于现有 ADR-0009 / 0017 等假设；空架子没意义
- ❌ user 明确「设计和实现都删掉」+「零保留」
- 否决

### C. v2 不撤，让 Conversation 重设计自己解决 vendor 污染

- 维持现状；CV1-CV4 设计自己想办法 work-around vendor stained 字段
- ❌ 设计上要不断 work-around 现有污染，结果不干净
- ❌ user 已表态彻底撤回
- 否决

### D. v3+ Bridge 用现有 v1 ADRs 演化（不从 0）

- v3 时基于 v1 ADR-0009 等基础上演化
- ⚠️ 跟 user「v3 从 0 开始讨论」直接冲突
- 否决（roadmap 写明 v3 从 0）

## References

### 影响的现有 ADR

- ADR-0009 [Issue-Conversation decoupled via Bridge](../0009-issue-conversation-decoupled-via-bridge.md)（已被 0021 superseded；本 ADR 后整理 refs）
- ADR-0017 [Task as Conversation](../0017-task-as-conversation.md) —— **本 ADR 后将被 rewrite 或 superseded**（CV 议题闭环后决定）
- ADR-0020 [Card confined to Bridge BC](../0020-card-confined-to-bridge-bc.md)（已被 0021 superseded；本 ADR 后整理 refs）
- ADR-0021 [Issue as Conversation](../0021-issue-as-conversation.md) —— **本 ADR 后将被 rewrite 或 superseded**
- ADR-0022 [Conversation 不对齐 IM 层级](../0022-conversation-not-aligned-with-im-hierarchy.md) —— **本 ADR 后将被 rewrite 或 superseded**

### v2 后续议题预告

- CV1 Channel 升业务一等公民
- CV2 Conversation Participant 显式建模
- CV3 跨 Conversation Message Carry-over
- CV4 Issue / Task 从 messages 派生入口
- W1 Web Console 全量 UI
- W2 CLI UX 增强

### 跨 BC 影响

- [conversation/00-overview.md](../../architecture/tactical/conversation/00-overview.md) / [01-conversation.md](../../architecture/tactical/conversation/01-conversation.md) / [02-identity.md](../../architecture/tactical/conversation/02-identity.md) —— CV 议题闭环后整体重写
- [architecture/strategic/03-bounded-contexts.md](../../architecture/strategic/03-bounded-contexts.md) —— § BC7 删除；Context Map 移除 Bridge 节点
- [requirements/01-functional.md](../../requirements/01-functional.md) —— F1 等 vendor 依赖重写
- [rules/conventions.md](../../../rules/conventions.md) —— § 9.y 外部集成走 Bridge / § 13 vendor 相关清理

### Roadmap

- [roadmap.md](../../roadmap.md) v3+ 新增「Bridge BC 重新设计 + vendor 接入」条目

### 来源

- 2026-05-22 #agent-center 讨论
- 用户决策时间线（同 thread）
