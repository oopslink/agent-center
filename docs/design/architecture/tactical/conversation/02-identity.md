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

### 2.1 自动注册（v1 单用户简化，主路径）

agent-center 定位个人工具（[需求 § 1.2](../../../requirements/00-overview.md)）。简化：

- **一个用户 Identity**：`user:<configured-name>`（通过 `agent-center identity add` 初始化时建立）
- **任何 vendor 非 bot 来源** → 默认归属这个 user identity
- 新 vendor 来源（首次见到 feishu open_id）→ **自动绑定**为该 user identity 的 ChannelBinding，不再二次确认
- 多 user / 跨用户消息归属逻辑 → v2+（多用户场景），见 [roadmap](../../../roadmap.md)

### 2.2 临时 Identity（系统自建）

| Identity | 创建时机 | 生命周期 |
|---|---|---|
| **Supervisor** | 每次 SupervisorInvocation 启动时临时创建 `supervisor:<invocation-id>` | invocation 生命周期同步 |
| **Agent** | 每个 AgentSession（v1 = 一次 TaskExecution）启动时临时创建 `agent:<session-id>` | session 生命周期同步 |
| **Bot** | 安装时创建一次 `bot`，永久存在 | 永久 |

### 2.3 CLI 手动管理

| 命令 | 用途 |
|---|---|
| `agent-center identity add user:<name> --display-name="..."` | 创建 user identity（v1 通常 setup 时跑一次）|
| `agent-center identity list` | 列所有 identity |
| `agent-center identity bind <identity-id> --channel=feishu --vendor-user-id=...` | 手动绑定 ChannelBinding |
| `agent-center identity unbind <identity-id> --channel=feishu` | 取消绑定 |

---

## § 3. 事件

| Event | 触发 | 主要 payload |
|---|---|---|
| `identity.registered` | 新 Identity 创建 | identity_id, kind |
| `identity.channel_bound` | 加 ChannelBinding | identity_id, channel, vendor_user_id |
| `identity.channel_unbound` | 解绑 | identity_id, channel |

---

## § 4. Identity / ChannelBinding Invariants

### Identity 不变量

1. **id 不可变**：形式化字符串（kind:name 形式）创建时定，永不改
2. **kind 不可变**：创建时定，永不改
3. **临时 Identity 跟实体生命周期同步**：`supervisor:<inv-id>` 跟 invocation 同生同死；`agent:<session-id>` 同 session 同生同死
4. **bot Identity 永久存在**：安装时一次性建，不允许删

### ChannelBinding 不变量

5. **identity_id 不可变**：跟 Identity 强引用
6. **(channel, vendor_user_id) 唯一**：同 vendor 内同 user id 至多对应一个 ChannelBinding（应用层校验，防多 identity 抢同一个 vendor user）
7. **preferred 唯一性 per identity**：一个 identity 在多 channel 之间至多 1 个 preferred；v1 单 vendor 时是该 channel
8. **绑定可解除**：unbind 删除 row（v2+ 可改为 soft delete + 保留 audit）

---

## § 5. References

- [00-overview.md](00-overview.md) — BC 入口（IdentityRegistrationService 自动注册逻辑）
- [01-conversation.md](01-conversation.md) — Message.sender_identity_id 强引用本 AR
- [ADR-0009 § 2 Bridge 模式](../../../decisions/0009-issue-conversation-decoupled-via-bridge.md) — vendor SDK 调用仅在 Bridge
- [ADR-0021 § 4 Issue 字段精简](../../../decisions/0021-issue-as-conversation.md) — Discussion BC `ChannelBinding` 已删，本 BC 是唯一持有方
- [bridge/01-feishu-integration.md § 4 Inbound](../bridge/01-feishu-integration.md) — 自动绑定路径
- [conventions § 13 安全](../../../../rules/conventions.md) — vendor user id 不外泄
