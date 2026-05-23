# Identity 聚合

> **DDD 战术层** · BC: Conversation · 聚合: Identity（AR）
>
> v2 [ADR-0033](../../../decisions/drafts/0033-identity-model-refactor.md) 简化：kind 3 种（`user` / `agent` / `system`）；ID 格式 `kind:id`；`kind=agent` 直接引用 AgentInstance.id (ULID, [ADR-0024](../../../decisions/drafts/0024-agent-instance-first-class.md))；**ChannelBinding 子 VO 删**（vendor 撤回，[ADR-0031](../../../decisions/drafts/0031-v2-drop-bridge-vendor-integration.md)）。

Identity 是参与者的统一身份。v2 形态：

| Identity 例 | id | kind | 说明 |
|---|---|---|---|
| 人类用户 | `user:hayang` | `user` | v2 单用户场景；id = `user:<name>` |
| Worker agent | `agent:01HE6T9N...` | `agent` | 引用持久 AgentInstance.id (ULID) |
| Built-in supervisor | `agent:<supervisor_agent_instance.id>` | `agent` | supervisor 也是 AgentInstance（[ADR-0029](../../../decisions/drafts/0029-supervisor-as-builtin-agent-instance.md)）；用 agent: 前缀统一 |
| 系统 | `system` | `system` | singleton |

权限模型（v3+）将绑 Identity.id；v2 暂无权限实装。

---

## § 1. 字段

### Identity（AR）

```
identity (
  id            TEXT        -- 'user:<name>' / 'agent:<agent_instance.id>' / 'system'
  kind          enum        -- user | agent | system （3 种，v2 简化）
  display_name  TEXT        -- "Hayang" / "Coder MBP" / "System"
  created_at    ISO8601 TEXT
)

UNIQUE INDEX identity_id_uq (id)
```

`id` 是形式化字符串（含 kind 前缀），不是 UUID —— 直接表达 actor 身份。

### 跨聚合 invariant：Identity[kind=agent] ↔ AgentInstance

应用层约束（[ADR-0033 § 3](../../../decisions/drafts/0033-identity-model-refactor.md)）：

- `agent:<x>` Identity 行存在 ⇔ `agent_instances` 表存在 row with `id=<x>`
- AgentInstance create 时同事务 INSERT 对应 Identity row
- AgentInstance archive 时 Identity 保留（历史 message sender 引用不能断）
- Identity 不可独立创建 `agent:<x>` —— 必须 AgentInstance create 触发

### ChannelBinding（v2 已删除）

⚠️ v1 的 `ChannelBinding` 子 VO 跟 ADR-0031 vendor 撤回一起 **全删**；v2 不再有 Identity ↔ vendor user id 映射。v3+ 重新设计 vendor 接入时若需 vendor 映射，独立 BC 处理；推荐命名 `VendorMapping`（避免跟 Conversation kind=channel 冲突）。

---

## § 2. 创建路径

### 2.1 User Identity（v2 单用户）

- 一个 user identity：`user:<configured-name>`，通过 `agent-center identity add user:<name>` 初始化建立
- 多 user / 权限绑定 → v3+

### 2.2 Agent Identity（跟随 AgentInstance）

| 创建时机 | id |
|---|---|
| `agent-center agent create <name>` → 同事务建 Identity | `agent:<agent_instance.id>` |
| Built-in supervisor 启动初始化 | `agent:<supervisor_agent_instance.id>` |

AgentInstance ↔ Identity 1:1（per [ADR-0033 § 3](../../../decisions/drafts/0033-identity-model-refactor.md)）。

### 2.3 System Identity

`system` singleton：安装时一次性建，永久存在；用于"系统生成 message"（如 system 类 content_kind）。

### 2.4 CLI 手动管理

| 命令 | 用途 |
|---|---|
| `agent-center identity add user:<name> --display-name="..."` | 创建 user identity（通常 setup 时跑一次）|
| `agent-center identity list` | 列所有 identity |
| `agent-center identity show <id>` | 看单个 identity 详情 |

---

## § 3. 事件

| Event | 触发 | 主要 payload |
|---|---|---|
| `identity.registered` | 新 Identity 创建 | identity_id, kind |

---

## § 4. Identity Invariants

1. **id 不可变**：形式化字符串（`kind:id` 形式）创建时定，永不改
2. **kind 不可变**：创建时定，永不改
3. **agent identity 跟 AgentInstance 1:1**：`agent:<x>` 必有 `agent_instances.id=<x>` row；AgentInstance archive 时 Identity 保留（历史 message sender 引用不能断）
4. **system identity 单例永久**：安装时一次性建，不允许删

---

## § 5. References

- [00-overview.md](00-overview.md) — BC 入口（IdentityRegistrationService）
- [01-conversation.md](01-conversation.md) — Message.sender_identity_id 强引用本 AR
- [ADR-0033 Identity 模型重构](../../../decisions/drafts/0033-identity-model-refactor.md)
- [ADR-0024 AgentInstance 一等公民化](../../../decisions/drafts/0024-agent-instance-first-class.md)
- [ADR-0031 v2 撤回 Bridge / vendor 集成](../../../decisions/drafts/0031-v2-drop-bridge-vendor-integration.md)（ChannelBinding 删除原因）
- [conventions § 13 安全](../../../../rules/conventions.md)
