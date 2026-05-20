# Identity 聚合（+ ChannelBinding 子 VO）

> **DDD 战术层** · BC: Conversation · 聚合: Identity（AR）+ ChannelBinding（VO，子从属）

Identity 是参与者的统一身份：`user:hayang` / `supervisor:invocation-id` / `agent:session-id` / `bot`。**跨渠道不变** —— 不管用户在飞书还是 DingTalk 发消息，归属同一个 Identity。

ChannelBinding 是 Identity ↔ 某渠道 vendor user id 的映射（例：`user:hayang ↔ feishu:open_id:ou_xxx`、`user:hayang ↔ dingtalk:userid:xxx`）。

---

## § 1. 字段

### Identity（AR）

```
identity (
  id                  TEXT  -- 'user:hayang' / 'supervisor:invocation-N' / 'agent:session-X' / 'bot'
  kind                user | supervisor | agent | bot
  display_name        TEXT  -- 显示名
  created_at          ISO8601 TEXT
)
```

`id` 是形式化字符串（含 kind 前缀），不是 UUID —— 直接表达 actor 身份。

### ChannelBinding（VO，子从属）

```
channel_binding (
  id                  ULID/UUID
  identity_id         FK → identities (强引用，不可变)
  channel             TEXT  -- 'feishu' / 'dingtalk' / 'web' / ...
  vendor_user_id      TEXT  -- 'feishu:open_id:ou_xxx' / 'dingtalk:userid:xxx' / ...
  preferred           INTEGER 0/1  -- 该 identity 的默认推送渠道？
  bound_at            ISO8601 TEXT
)
```

跨 BC 命名冲突说明：ADR-0020 引入的 Discussion BC `ChannelBinding` 已被 [ADR-0021](../../../decisions/0021-issue-as-conversation.md) 移除（Discussion 不再持 ChannelBinding 字段）；本 BC 是**唯一**持有 `ChannelBinding` VO 的地方。

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
3. **临时 Identity 跟实体生命周期同步**：supervisor:<inv-id> 跟 invocation 同生同死；agent:<session-id> 同 session 同生同死
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
