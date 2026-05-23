# 0033. Identity 模型重构（v2 CV2a）

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-24 |
| Delivered | P10 § 3.2 — `cefc135` (Identity 4→3 kinds + kind:id prefix as part of Conversation v2 schema reset) |
| Related | v2 议题 CV2（与 [ADR-0034](0034-conversation-participants-field.md) participants 字段并行立）；触发自 [ADR-0024 G1 AgentInstance 一等公民化](0024-agent-instance-first-class.md) + [ADR-0029 Supervisor as Built-in AgentInstance](0029-supervisor-as-builtin-agent-instance.md) + [ADR-0031 v2 Drop Bridge](0031-v2-drop-bridge-vendor-integration.md) 后 actor 模型协调 |

## Context

v1 Identity AR（[conversation/02-identity.md](../../architecture/tactical/conversation/02-identity.md)）承载 4 种 kind：

| kind | id 例 | 说明 |
|---|---|---|
| `user` | `user:hayang` | 人类用户 |
| `supervisor` | `supervisor:<invocation-id>` | per-invocation 临时身份 |
| `agent` | `agent:<session-id>` | per-execution 临时身份 |
| `bot` | `bot` | 系统 actor |

ChannelBinding 子 VO 关联到 vendor user id（飞书 open_id 等）。

### v2 后浮现的问题

1. **AgentInstance 一等公民化（[ADR-0024](0024-agent-instance-first-class.md)）** 后，agent 身份持久（`AgentInstance.id` ULID 全局唯一不变）；但 Identity[kind=agent] 仍是 per-session 临时身份 —— **同一概念分两处**
2. **Supervisor 是 built-in AgentInstance（[ADR-0029](0029-supervisor-as-builtin-agent-instance.md)）** —— supervisor 跟 worker agent 在 actor 层不应区分；但 Identity 仍有 `kind=supervisor` 单独枚举
3. **Bridge 撤回（[ADR-0031](0031-v2-drop-bridge-vendor-integration.md)）** —— ChannelBinding（Identity↔vendor user id 映射）失去意义；`bot` 跟 vendor 强绑的隐含语义需重审
4. **CV2 Participant 模型（[ADR-0034](0034-conversation-participants-field.md)）需要稳定 actor 引用** —— per-session 临时 id 不能作 participant，必须用持久 id

### 用户表态（2026-05-23 #agent-center）

> 「V2 不做多租户 也不做组织这个概念 但是得有身份 后续权限绑定身份 身份就是 identity」
>
> 「AgentInstance 应该有个全局唯一的 id」

→ v2 保留 Identity AR；但简化 kind + 跟 AgentInstance.id 对齐。

## Decision

### 1. Identity kind 枚举简化（4 → 3）

| v1 kind | v2 处置 | 含义 |
|---|---|---|
| `user` | ✅ 保留 | 人类用户 |
| `supervisor` | ❌ 删 | 用 `agent` kind（supervisor 是 built-in AgentInstance） |
| `agent` | ✅ 保留（含义变 stable） | 引用持久 AgentInstance.id；不再是 per-session 临时身份 |
| `bot` | ❌ 删 | 用 `system` 替代 |
| —— | ➕ 新增 `system` | 系统 actor（singleton） |

→ v2 Identity kinds = **`user` | `agent` | `system`**（3 种）。

### 2. Identity ID 命名约定：`kind:id`

```
identity (
  id            TEXT        -- 形式：'user:<name>' / 'agent:<agent_instance.id>' / 'system'
  kind          enum        -- user | agent | system
  display_name  TEXT        -- "Hayang" / "Coder MBP" / "System"
  created_at    timestamp
)

UNIQUE INDEX identity_id_uq (id)
```

具体 id 格式：

| kind | id 格式 | 例 |
|---|---|---|
| `user` | `user:<name>` | `user:hayang`（v2 单用户场景；name 由用户初始化时设）|
| `agent` | `agent:<agent_instance.id>` | `agent:01HE6T9N...`（ULID）|
| `system` | `system` | `system`（singleton）|

### 3. 跨聚合 invariant：Identity[kind=agent] ↔ AgentInstance

应用层约束：

- `agent:<x>` Identity 行存在 ⇔ `agent_instances` 表存在 row with `id=<x>`
- AgentInstance create 时同事务 INSERT 对应 Identity row（trigger / app-layer code）
- AgentInstance archive 时 Identity 保留（历史 message sender 引用不能断）
- Identity 不可独立创建 `agent:<x>` —— 必须 AgentInstance create 触发

→ 应用层保证；SQLite 限制 FK + CHECK 跨表组合困难，主要靠 app-layer。

### 4. 删除 ChannelBinding（与 ADR-0031 vendor 撤回配套）

[conversation/02-identity § ChannelBinding](../../architecture/tactical/conversation/02-identity.md) 删除：

- ChannelBinding 子 VO 删
- `channel_bindings` 表删
- ChannelBinding factory / repository / events 全删
- Identity 不再持 vendor 路由信息

→ v3+ vendor 重新接入时若需要 vendor↔Identity 映射，另外引入（**不再叫 ChannelBinding**，跟 Conversation kind=channel 区分；推荐 `VendorMapping` 等命名）。

### 5. Identity 创建路径

| 路径 | 触发时机 |
|---|---|
| `agent-center identity add --name=<n>` （CLI）| v2 初始化时创建用户 identity；v2 单用户一般只执行一次 |
| AgentInstance create 联动 | AgentInstance create 同事务 INSERT identity (id=agent:<ulid>, kind=agent)；ADR-0024 已 implied |
| Center 启动 auto-provision | system identity / built-in supervisor AgentInstance + identity 同启动时建（idempotent） |

v2 单用户场景下用户一般不直接管 identity；通过 `agent-center identity list` 查。

### 6. CLI

| 命令 | 用途 |
|---|---|
| `agent-center identity add --name=<n>` | 创建 user identity（v2 通常初始化时一次）|
| `agent-center identity list [--kind=<k>]` | 列 |
| `agent-center identity show <id>` | 详情 |

agent / system identity 不允许 CLI 直接 create / archive（由系统流程管理）。

## Consequences

**正面**：

- Identity AR 跟 AgentInstance 一致：agent 身份持久且单一来源
- supervisor / agent 在 actor 层一视同仁，跟 [ADR-0029](0029-supervisor-as-builtin-agent-instance.md) 对齐
- Identity kind 从 4 减到 3，模型更窄
- ChannelBinding 跟 vendor 一起退出 v2，模型不再被 vendor 污染（[ADR-0031](0031-v2-drop-bridge-vendor-integration.md)）
- 为后续权限模型（v3+）打基础 —— 权限绑 Identity.id；规则模型未来扩展

**负面 / 待跟进**：

- 现有数据 schema 改动：identity 表 kind 枚举 + 删 ChannelBinding 表；v2 不考虑向后兼容直接重建
- Identity[kind=agent].id 跟 AgentInstance.id 跨表约束靠 app-layer 校验；SQLite 限制不能纯 DB 强制
- 现有 v1 中 message.sender_identity_id 含 `supervisor:invocation-id` / `agent:session-id` 等会变成无效引用 —— v2 不考虑向后兼容，旧数据丢弃 / 一次性 migrate
- ADR-0017 / 0021 / 0022 中所有 `agent:<session-id>` / `supervisor:<invocation-id>` 引用 vendor 配套 rewrite

## Alternatives Considered

### A. 保留 Identity 4 kinds（含 supervisor / bot）

- ✅ 不动现有
- ❌ Supervisor 跟 worker agent 在 Workforce 是同 AgentInstance；Identity 单列 kind 重复
- ❌ vendor 撤回后 bot 跟 vendor 强绑语义失效
- 否决

### B. 删 Identity AR；用 AgentInstance + User AR 直接做 participant

- 撤 Identity；participants 直接引用 AgentInstance.id 或 User.id
- ✅ 模型最简
- ❌ user 表态明确要保留 Identity 作为「身份层抽象 + 权限挂载点」
- 否决

### C. Identity[kind=agent].id 用 AgentInstance.name 而非 id

- ✅ id 可读（`agent:coder-mbp`）
- ❌ name 不一定不可变（rename 破坏身份）；id (ULID) 永久不变
- ❌ user 明确要求用 id（「AgentInstance 应该有个全局唯一的 id」）
- 否决

## References

### v2 ADRs

- [ADR-0024 AgentInstance 一等公民化](0024-agent-instance-first-class.md)
- [ADR-0029 Supervisor as Built-in AgentInstance](0029-supervisor-as-builtin-agent-instance.md)
- [ADR-0031 v2 Drop Bridge / Vendor Integration](0031-v2-drop-bridge-vendor-integration.md)
- [ADR-0032 Conversation Channel 业务一等公民](0032-conversation-channel-as-first-class.md)
- [ADR-0034 Conversation Participants 字段](0034-conversation-participants-field.md)（CV2b 并行立）

### 跨 BC

- [conversation/02-identity.md](../../architecture/tactical/conversation/02-identity.md) - schema 重写
- [conversation/00-overview.md](../../architecture/tactical/conversation/00-overview.md) - UL + VO 更新
- [workforce/04-agent-instance.md](../../architecture/tactical/workforce/04-agent-instance.md) - AgentInstance create 流程加 identity INSERT

### 来源

- 2026-05-23 #agent-center 讨论
