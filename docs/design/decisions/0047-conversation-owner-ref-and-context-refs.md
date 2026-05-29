# 0047. Conversation owner_ref URI + Message context_refs（v2.7）

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-05-29 |
| Delivered | v2.7 design phase；详 [v2.7-domain-refactor-plan § 2.8 / § 5 A0](../../plans/v2.7-domain-refactor-plan.md) |
| Supersedes | amends [ADR-0039 Conversation 业务模型 v2 统一](0039-conversation-business-model-v2-unified.md)（owner_ref / kind / context_refs 收敛）；amends [ADR-0034 Conversation Participants 字段](0034-conversation-participants-field.md)（participant 降为投影，详 [ADR-0052](0052-subscriber-truth-and-participant-projection.md)） |
| Related | v2.7 一并立的 [ADR-0046 ProjectManager BC](0046-projectmanager-bc.md) / [ADR-0048 File URI](0048-file-uri-and-blobstore.md) / [ADR-0052 Subscriber truth + participant projection](0052-subscriber-truth-and-participant-projection.md) |

## Context

v2 GA 后 Conversation 作为独立 BC 承载消息时间线（[ADR-0032](0032-conversation-channel-as-first-class.md) / [ADR-0039](0039-conversation-business-model-v2-unified.md)）。v2.7 把工作管理收敛进 ProjectManager BC（[ADR-0046](0046-projectmanager-bc.md)），Conversation 需要一个**与业务对象解耦、又能稳定指回业务对象**的引用方式，并要能在消息上区分"同一 Task 会话里不同 WorkItem/交互段"的来源。

浮现的问题：

1. **业务归属硬编码**：现状 Conversation 通过零散字段绑业务对象，BC 间耦合。需要统一、可演进的引用。
2. **Task 重派的会话连续性**：一个 Task 跨多次 Agent 指派只保留**一个稳定会话**（plan § 2.4），同一会话里要能区分不同 WorkItem 的消息段。
3. **附件模型割裂**：现状区分文件/图片，v2.7 要统一为 URI + 元数据（详 [ADR-0048](0048-file-uri-and-blobstore.md)）。

## Decision

### 1. Conversation.kind 固定四类

```text
project_channel | issue | task | dm
```

### 2. Conversation.owner_ref 用 URI 指回业务对象

```text
pm://projects/{project_id}
pm://issues/{issue_id}
pm://tasks/{task_id}
```

- `dm` 无 owner_ref（或指向参与者，按现状）。
- `pm://` scheme 表示归属 ProjectManager BC 的业务对象；Conversation **只持有引用，不反向推断业务状态**（plan § 2.8）。
- owner_ref 是弱关联 ID 引用（跨 BC，软约束），与 [ADR-0040](0040-identity-bc-carve-out.md) 既有跨 BC 引用风格一致。

### 3. Message.context_refs 携带来源引用

第一版至少包含：

```text
work_item_ref   // 区分同一 Task 会话内不同 AgentWorkItem 的消息段（plan § 2.4）
task_ref        // 冗余便捷引用
agent_ref       // 发言 Agent
```

- UI 可按 `work_item_ref` 把消息分组为"尝试/段"。
- 第一版 Message 需要 `work_item_ref`；`interaction_ref` 不强制进 Message，交互级细节落 AgentActivityEvent（plan § 2.4 / § 2.6）。

### 4. MessageAttachment 统一结构

- 不在领域模型区分 file/image。一个 MessageAttachment = `uri` + 元数据。
- A0 最小字段：`uri: ac://files/{ulid}`、`filename`、`mime_type`、`size`。
- 展示类型由元数据派生（详 [ADR-0048](0048-file-uri-and-blobstore.md)）。

### 5. ConversationParticipant 降级为投影

- 对 task/issue 会话，业务订阅真值在 ProjectManager（[ADR-0052](0052-subscriber-truth-and-participant-projection.md)）。
- ConversationParticipant 是从订阅真值**同步出来的消息层投影**，不是订阅真值本身。同步经 outbox（plan § 10 OQ1）。

### 6. 归属于 A0

owner_ref / kind / context_refs / 统一附件结构 / FileURI 值对象与 resolver 骨架 = 开发顺序 **A0**（plan § 5），是 B/C/D 的共享基元，先于其它阶段落地。

## Consequences

### 正面

- Conversation 与 ProjectManager 解耦：URI 引用 + 不反推业务状态，边界清晰。
- 重派场景下单会话 + `work_item_ref` 分段，UI 可呈现多次尝试而不炸开会话。
- 附件模型统一，图片/文件一视同仁，前端按元数据渲染。

### 负面 / 待跟进

- v2.7 不做向后兼容，现有 Conversation schema drop-and-recreate（plan § 5 A0 验收：旧 channel/message 重建后仍可用）。
- `pm://` resolver 需要 ProjectManager 就绪才能完全校验；A0 阶段 owner_ref 先落字段，B 阶段补写时校验。
- participant 投影与订阅真值的一致性窗口为最终一致（outbox），UI 需容忍极短延迟。

## Alternatives Considered

### A. Conversation 直接外键到 Task/Issue 表

- ✅ 查询直观
- ❌ 跨 BC 硬外键，Conversation 反向依赖 ProjectManager 内部表结构，破坏 BC 边界
- 否决

### B. owner_ref 用裸 ID（不带 scheme）

- ✅ 简单
- ❌ 无法区分 project/issue/task；无法和 `ac://files` / `pm://` 统一的 URI 语义对齐
- 否决

### C. 每次重派新建会话

- ✅ 段天然隔离
- ❌ 破坏"一个 Task 一条稳定会话"（plan § 2.4），历史割裂、上下文丢失
- 否决，改用单会话 + `work_item_ref` 分段

## References

- [v2.7-domain-refactor-plan § 2.8 Conversation](../../plans/v2.7-domain-refactor-plan.md) / [§ 2.4 AgentWorkItem](../../plans/v2.7-domain-refactor-plan.md) / [§ 5 A0](../../plans/v2.7-domain-refactor-plan.md) / [§ 10 OQ1](../../plans/v2.7-domain-refactor-plan.md)
- [ADR-0048 File URI + BlobStore](0048-file-uri-and-blobstore.md)
- [ADR-0052 Subscriber truth + participant projection](0052-subscriber-truth-and-participant-projection.md)
- 来源：2026-05-29 DM 设计讨论（@oopslink ↔ @AgentCenterPD）
