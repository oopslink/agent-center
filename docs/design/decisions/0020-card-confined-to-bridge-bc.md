# 0020. Card 限制在 Bridge BC：Issue 字段精简 + Card 元数据归 Bridge ledger

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-20 |
| Supersedes-Partial | [ADR-0009](0009-issue-conversation-decoupled-via-bridge.md) § 3（仅"Issue Bound Card 字段结构"条款；不动 § 1 解耦 / § 2 Bridge 模式两条主决策）|
| Affects-Wording | [ADR-0017](0017-task-as-conversation.md)（Task 路线已对齐本 ADR，措辞顺手收口）|

## Context

[ADR-0009 § 3](0009-issue-conversation-decoupled-via-bridge.md) 定义 `issue.bound_card`（v1 仅 feishu，结构含 `card_message_id` / `thread_key`），把 Card 这个 vendor 渲染概念**字段化到 Issue 聚合内**。

[ADR-0017](0017-task-as-conversation.md) 让 Task 走 Conversation 1:1 路线时，Conversation 的 channel 绑定字段是 `primary_channel_thread_key`（仅 thread_key，**不持 card_message_id**），实质上**已经做对了**："Card 是 vendor 渲染产物，领域 BC 持 thread_key 足矣"。

但 Issue 路线没跟上。导致：

1. **抽象层不一致**：Conversation 持 thread_key；Issue 持 thread_key + card_message_id；两个领域 BC 对"channel 绑定"模式不对称
2. **领域层关注点泄漏**：[conversation/01 § 2 注释](../architecture/tactical/conversation/01-conversation.md)已经承认 channel 路由字段"对 Conversation 自身没有业务意义" —— 严格 DDD 视角它们本就不该在领域 BC；Issue 的 `card_message_id` 更甚 —— 它**不仅没业务意义，连"路由用途"都不属于领域**（Discussion BC 业务逻辑里**从未读过** card_message_id 字段）
3. **"Card" 词汇泄漏**：`bound_card` / `bound_card_json` / CLI `issue bind-card` 让 Card（vendor 渲染概念）出现在领域命名上；用户的真实意图是"绑定一个 thread 作为讨论空间"，跟 thread 是不是 card 启动无关

证据盘点：当前 `issue.bound_card_json` 5 字段，**只有 `card_message_id` 是纯 vendor 实现细节**：

| 字段 | Discussion BC 内是否用到 |
|---|---|
| `channel` | ✅ 路由判定（"这个 issue 绑哪个 vendor"） |
| `thread_key` | ✅ Bridge inbound 反查 + outbound 路由 + 是否"bound 路径"判定 |
| `bound_at` | ✅ 领域 audit（`inspect issue` 看得见） |
| `bound_by_identity_id` | ✅ 领域 audit |
| `card_message_id` | ❌ **从未在 Discussion BC 逻辑中被读**；仅 Bridge unbind/rebind 时 `update_card` 置灰使用 |

## Decision

**Card（vendor 渲染概念）完全归 Bridge BC；领域 BC 仅持 thread_key + audit metadata。**

### 1. Issue 聚合字段精简 + 重命名

```
issue.channel_binding_json = {
  channel: "feishu",
  thread_key: "feishu:thread:xyz",
  bound_at: "...",
  bound_by_identity_id: "..."
}
```

- 字段从 `bound_card_json` 重命名为 `channel_binding_json`
- 去掉 `card_message_id`（移到 Bridge BC，见 § 2）
- 4 字段：`channel` / `thread_key` / `bound_at` / `bound_by_identity_id`

**DDD 范畴**：依然是 Issue 聚合内的 **Value Object** 字段（nullable VO，类型名 `ChannelBinding`）。

### 2. Bridge BC 加 `feishu_card_ledger` 小审计表

```
feishu_card_ledger (
  channel              -- 'feishu'
  thread_key           -- 反查 key
  card_message_id      -- vendor msg id（unbind / rebind / update_card 用）
  subject_type         -- 'issue'（未来扩展位：'task' / ...）
  subject_id           -- 对应聚合 id
  bound_at             -- 同步领域端
)
```

- Bridge BC 内部表，跟现有 `feishu_delivery_ledger`（[strategic § BC7 注释](../architecture/strategic/03-bounded-contexts.md)说 Bridge "可有自己的小审计表"）同性质
- Bridge 在 inbound vendor 回写时同步写一行；unbind/rebind 时按 `(channel, thread_key)` 反查取 `card_message_id` 调 `update_card`
- **不算业务聚合**：仅 audit + vendor 实现细节缓存；不参与领域决策；保持 [conventions § 9.y](../../rules/conventions.md) "Bridge 模式"精神

### 3. CLI 重命名

- `issue bind-card <issue_id> --channel=feishu [--auto]` → `issue bind-thread <issue_id> --channel=feishu [--auto]`
- `issue unbind-card` → `issue unbind-thread`
- `issue rebind-card` → `issue rebind-thread`

v1 还没用户，直接换。**不保留向后兼容别名**（[conventions § 12 命名一致](../../rules/conventions.md)精神：术语表一旦定就一刀切，不留同义词）。

### 4. UL 修订 + 同名命名风险

[strategic/03-bounded-contexts § 1](../architecture/strategic/03-bounded-contexts.md) 通用语言表：

- **删** "Bound Card"（若有条目）
- **新增** "Channel Binding (Discussion)"：Issue 聚合内的 VO 字段，4 字段（`channel` / `thread_key` / `bound_at` / `bound_by_identity_id`）；表示"Issue 跟某个 vendor channel thread 的绑定关系"
- **保留** "LarkCard"（Bridge 上下文）：飞书交互卡片，vendor 渲染细节；仅 Bridge BC 持有相关数据

**同名命名风险**：Conversation BC 的 `Identity` 聚合已有 VO 名 `ChannelBinding`（Identity ↔ vendor user 的绑定，如 feishu `open_id` ↔ `user:hayang`）。本 ADR 引入的 Discussion BC `ChannelBinding`（Issue ↔ vendor thread 的绑定）跟它**同名异义**。

**处理方案**：

- 通用语言表上各自加 BC 限定（`ChannelBinding (Discussion)` / `ChannelBinding (Conversation)`）
- 代码层若需要在同一文件 / 包内同时使用，加 BC 前缀（`DiscussionChannelBinding` / `IdentityChannelBinding`）
- 两者本质都是「领域实体 ↔ vendor channel 的绑定关系」VO，**同一抽象家族**；同名不算坏 —— 它真实反映了概念同形

### 5. 协议事件名同步重命名

- `issue.bound_card_requested` → `issue.channel_binding_requested`
- 若有 `issue.bound_card_*` 系列事件 → 统一改 `issue.channel_binding_*`

### 6. conventions § 12 表加行

在 [conventions § 12 命名一致](../../rules/conventions.md)表加：

| 概念 | 用 | 不用 |
|---|---|---|
| Issue 跟 vendor channel thread 的绑定 | **Channel Binding**（VO）/ `channel_binding_json`（字段）/ `bind-thread` (CLI) / `issue.channel_binding_*` (事件) | ~~Bound Card~~ / ~~bound_card_json~~ / ~~bind-card~~ / ~~issue.bound_card_*~~ |

## Rationale

1. **DDD 关注点分离**：Card 是 vendor 渲染概念，领域 BC 不该持有任何 vendor 实现细节字段。Discussion BC 业务逻辑里 `card_message_id` 字段值**从未被读过** —— 它存在 Issue 聚合上是**纯泄漏**
2. **跟 Conversation 模式对齐**：ADR-0017 让 Task 路线只持 `primary_channel_thread_key` —— Issue 路线本就该同步；现在两条线不对称是历史遗留
3. **Bridge "无业务聚合"约束保持**：`feishu_card_ledger` 跟现有 `feishu_delivery_ledger` 同性质 —— vendor 翻译状态 / audit，不算业务聚合；[strategic § BC7](../architecture/strategic/03-bounded-contexts.md) 已明示这种小表合法
4. **命名清晰度**：用户视角"我在 thread 里讨论 Issue"，跟"thread 是 card 启动的"无关；用 `channel_binding` / `bind-thread` 命名比 `bound_card` / `bind-card` 更准
5. **v1 修改成本低**：还没用户、CLI 还没 lock-in；现在改 vs 未来改的成本比 ≈ 1:10

## Consequences

### 战略层

- [strategic/03-bounded-contexts.md § 1 UL](../architecture/strategic/03-bounded-contexts.md) 加 "Channel Binding (Discussion)"；同时给 Conversation BC 已有的 `ChannelBinding` 加 BC 限定
- [strategic/03-bounded-contexts.md § 2 BC2 Discussion](../architecture/strategic/03-bounded-contexts.md) 核心事件列表把 `issue.bound_card_requested` → `issue.channel_binding_requested`
- [strategic/03-bounded-contexts.md § 2 BC7 Bridge](../architecture/strategic/03-bounded-contexts.md) 内部表表格加 `feishu_card_ledger` 条
- [conventions § 12](../../rules/conventions.md) 命名表加新行

### 战术层

- [tactical/discussion/01-issue-discussion.md](../architecture/tactical/discussion/01-issue-discussion.md)（即将在 P3 重组为 `tactical/discussion/00-overview.md`）：「Bound Card 机制」节重写为「Channel Binding 机制」；字段表更新；CLI 命名更新；事件名更新；§ X.1.3 VO 表把 BoundCard → ChannelBinding
- [tactical/bridge/01-feishu-integration.md § 7 Bound Card 机制](../architecture/tactical/bridge/01-feishu-integration.md)：标题改 "Channel Binding 机制（Discussion BC 协作）"；新增"`feishu_card_ledger` 内部表"说明；inbound 流程 step 改为"按 (channel, thread_key) 反查 → 是否走 issue channel binding 路径"
- [tactical/conversation/01-conversation.md](../architecture/tactical/conversation/01-conversation.md)：§ 6 Inbound 流程"路由判断 1"句子里的 `bound_card.thread_key` → `channel_binding.thread_key`；其它正文提及"bound card"的位置更新

### 实现层

- [implementation/02-persistence-schema.md](../implementation/) (TBD)：Issue schema 落 `channel_binding_json TEXT` (nullable JSON)；Bridge `feishu_card_ledger` schema 落在 Bridge BC 章节
- Repository 接口（Discussion）`updateChannelBinding`（取代 `updateBoundCard`）

### CLI / skill 文档

- supervisor.md / worker-agent.md（[tactical/agent-harness/](../architecture/tactical/agent-harness/)）中 `issue bind-card` 系列命令更新
- skill-cli-tooling 索引（[02-skill-cli-tooling.md](../architecture/tactical/agent-harness/02-skill-cli-tooling.md) 若存在）同步

### ADR 措辞

- ADR-0009 status 改为 `Accepted (§ 3 partially superseded by 0020)`；§ 3 第一句话补"字段结构详见 ADR-0020"
- ADR-0016 / ADR-0017 中提到 `issue.bound_card_json` / `bind-card` 的措辞作 reference 改名（**仅措辞**，不动决策）
- README ADR 索引表加本 ADR 行（同时补 0019 行漏更）

### 不动

- ADR-0009 § 1（Issue ↔ Conversation 解耦）**不动**
- ADR-0009 § 2（Bridge 模式 / FeishuBridge ACL 性质）**不动**
- ADR-0017（Task ↔ Conversation 1:1）**不动**
- ADR-0019（BC 合并）**不动**

## Alternatives Considered

### (A) 保持现状（ADR-0009 § 3 不动）

- Pro: 零工作量
- Con: 两条线（Issue / Task）的 channel 绑定模式永久不对称；Card 概念污染 Discussion BC；DDD 关注点泄漏会随着 v2+ 改造（如多 vendor）放大
- **驳回**：v1 修改成本低、收益清晰；后期修改成本指数级高

### (B) 完全把 channel binding 移到 Bridge BC（Issue 表去除 channel_binding_json 字段）

类似前期讨论的 "Y 方案"：建立 `bridge_bindings (subject_type, subject_id, channel, thread_key, ...)` 表，Issue 完全不持任何 channel 字段。

- Pro: 关注点剥离最彻底
- Con: Discussion 自己业务逻辑要查"我有没有绑定"也要跨 BC 查 Bridge 表；违反"领域决策不依赖 Bridge"的原则；引入跨 BC 强查询依赖
- **驳回**：thread_key 在 Discussion 业务逻辑里**有领域意义**（判定 inbound 是否走 channel binding 路径写 IssueComment），不该剥离；只剥纯 vendor 形态的 card_message_id 即可

### (C) 让 Issue 也走 Conversation 1:1（建 kind=issue Conversation）

类似前期讨论的 "X 方案"：让 Issue 借道 Conversation，channel binding 完全交给 Conversation。

- Pro: 跟 Task 路线完美对称
- Con: 跟 [ADR-0009](0009-issue-conversation-decoupled-via-bridge.md) § 1 主决策（解耦 / N:N 弱关联 / Conversation 不知道 Issue）**根本对立**；需要全推翻 ADR-0009 § 1 + § 2 重做
- **驳回**：ADR-0009 § 1 的解耦理由（R1-R4，见 ADR-0009 § Context + § Alternatives B Con）仍然成立，重做工作量大、动机不足

## References

- 起源：2026-05-20 P3 推进 Discussion BC 重组时识别概念边界泄漏
- DDD 方法论：[conventions § 0](../../rules/conventions.md)
- 受影响 ADR：[0009 § 3](0009-issue-conversation-decoupled-via-bridge.md)（partial superseded）/ [0017](0017-task-as-conversation.md)（措辞）/ [0016](0016-task-progress-via-bound-thread.md)（措辞）
- 战略层影响：[strategic/03-bounded-contexts](../architecture/strategic/03-bounded-contexts.md)
- 战术层影响：[tactical/discussion/01-issue-discussion](../architecture/tactical/discussion/01-issue-discussion.md) / [tactical/bridge/01-feishu-integration](../architecture/tactical/bridge/01-feishu-integration.md) / [tactical/conversation/01-conversation](../architecture/tactical/conversation/01-conversation.md)
- 蓝图：[ddd-blueprint § 2.1](../ddd-blueprint.md)（ADR 表加本行）
- 规则：[conventions § 12](../../rules/conventions.md)（命名表加新行）
