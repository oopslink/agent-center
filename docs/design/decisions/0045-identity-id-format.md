# 0045. Identity ID 格式（worker-style `kind-<8hex>`，v2.6）

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-05-27 |
| Delivered | v2.6 design phase；详 [v2.6-design § 2.2 / § 4.2.1](../../plans/v2.6-design.md) |
| Supersedes | ~~[ADR-0033 § 1 Identity kind 简化 4→3](0033-identity-model-refactor.md)~~（v2.6 改 kind 枚举为 `user | agent`，删 `system`）<br>~~[ADR-0033 § 2 Identity ID 命名约定 `kind:id`](0033-identity-model-refactor.md)~~（v2.6 改为 `kind-<8hex>` worker-style） |
| Related | [ADR-0040 Identity BC carve-out](0040-identity-bc-carve-out.md) / [ADR-0023 Worker enroll lightweight](0023-worker-enroll-lightweight.md)（worker-style id 来源） |

## Context

[ADR-0033](0033-identity-model-refactor.md) v2 GA 时确定的 Identity 模型：

- kind 枚举：`user | agent | system`（3 种）
- id 格式：`kind:id`，例 `user:hayang` / `agent:01HE...ULID` / `system`
- `system` 是 singleton fixed id

### v2.6 后浮现的问题

1. **`kind:id` 格式跨表 join 不友好**：所有引用 Identity 的列都得带 `kind:` 前缀，多个 BC 的查询/索引开销 + 视觉噪音
2. **`agent:<agent_instance.id>` 强依赖**：[ADR-0033 § 3](0033-identity-model-refactor.md) "Identity[kind=agent].id = `agent:<agent_instance.id>`" 把两个 AR 通过 ID 派生强绑；v2.6 引入显式 FK (`agent_instances.identity_id`) 后这个派生约束反成累赘
3. **`system` kind 不再需要**：v2.6 bootstrap 自动初始化路径被 signup-page 替代后，没有"开机就存在的 system Identity"；纯系统动作 actor=NULL（v2.6-design 决策 #20）
4. **跟 Worker / AgentInstance 命名风格不统一**：v2.4 已用 `worker-<8hex>` 格式（per [ADR-0023](0023-worker-enroll-lightweight.md) + 2026-05-26 oopslink 决策 a-i），Identity 单独搞 `kind:id` 形式风格分裂
5. **可读 / 可输入 trade-off**：用户在 UI / CLI 引用 Identity 时，`user-a1b2c3d4` 比 `user:hayang` 更不依赖 display_name 改名风险（display_name 可改，id 不可改）

## Decision

### 1. Identity kind 枚举：`user | agent`（2 种）

```
v2.5: user | agent | system   (3 kinds)
v2.6: user | agent             (2 kinds; 'system' 删)
```

- `user`：人类用户
- `agent`：程序化 actor（AgentInstance 对应的 Identity）

`system` kind 删除：
- 之前 `system` singleton 服务于"系统级动作的 actor 字段"；v2.6 纯系统动作 actor=NULL（详 [v2.6-design 决策 #20](../../plans/v2.6-design.md)）
- 数据库 schema check 约束更新为 `kind IN ('user', 'agent')`
- 历史 `sender_identity_id = 'system'` 的 Message 行随 v2.6 清库重装一并清除（[ADR-0040 § Consequences](0040-identity-bc-carve-out.md)，drop-and-recreate）

### 2. Identity ID 格式：`kind-<8hex>` worker-style

```
identity (
  id            TEXT  -- 形式：'user-<8hex>' 或 'agent-<8hex>'
  kind          TEXT  -- 'user' | 'agent'
  display_name  TEXT  -- "Hayang" / "AgentCenterPD"
  ...
)
```

具体格式：

| kind | id 格式 | 例 |
|---|---|---|
| `user` | `user-<8 hex chars>` | `user-a1b2c3d4` |
| `agent` | `agent-<8 hex chars>` | `agent-e5f67890` |

8 hex chars = 32-bit 随机；生成时 collision check + retry（系统级 max retry 上限报错）。

### 3. AR-internal Invariant：ID 前缀匹配 kind

```
I3: identity.id 前缀必须匹配 identity.kind
    - kind='user'  ⇒ id LIKE 'user-________'
    - kind='agent' ⇒ id LIKE 'agent-________'
```

由 IdentityFactory 在构造时守门 + DB CHECK 约束兜底（SQLite 表达式）。

### 4. AgentInstance ↔ Identity 改为显式 FK 关联

v2.5 时 `agent_instances.id` ULID 全局唯一 + Identity `agent:<agent_instance.id>` 派生约束（[ADR-0033 § 3](0033-identity-model-refactor.md)）。

v2.6 改为：

```
agent_instances (
  id            ULID    -- AgentInstance 自身 id（不变）
  identity_id   TEXT    -- 显式 FK to identities.id（NOT NULL，新增）
  ...
)
```

- `agent_instances.identity_id` 引用 `identities.id`（kind=agent）
- AgentInstance.id 跟 Identity.id 不再 1:1 派生；可以是 1:1 关系但 ID 独立生成
- AgentInstance create 时同事务 INSERT Identity 行（DS-5 AgentIdentityProvisionService，详 [v2.6-design § 4.8.2](../../plans/v2.6-design.md)）
- AgentInstance archive 时 Identity 保留（历史 message sender 引用不能断；维持 [ADR-0033 § 3](0033-identity-model-refactor.md) 精神）

### 5. 跟 Workforce / Bootstrap Token 的 ID 风格统一

v2.4 后系统 ID 命名公约：

| 实体 | 格式 |
|---|---|
| Worker | `worker-<8hex>` |
| Identity (user) | `user-<8hex>` |
| Identity (agent) | `agent-<8hex>` |
| Organization | `org-<8hex>` |
| Member | `mem-<8hex>` |
| Invitation | `inv-<8hex>` |

所有"用户可见的资源 ID"统一 `<kind>-<8 hex>` 格式；不再有 ULID 直露（ULID 留给系统内部不暴露 ID 用，如 AgentInstance.id / Conversation.id 等）。

## Consequences

### 正面

- 视觉一致：跨表 join + 跨 BC log 时 ID 风格统一，无 `:` vs `-` 混用
- AgentInstance ↔ Identity 解耦：FK 显式 + ID 独立生成；两者生命周期可独立演进
- 删 `system` kind = 删 magic singleton；纯系统动作走 NULL，更清晰
- display_name 可随用户偏好改名，不破坏 ID 引用稳定性

### 负面 / 待跟进

- **现有数据全清**：v2.6 不向后兼容（drop-and-recreate）；v2.5 `user:hayang` / `agent:01HE...` 历史行随清库消失
- **Conversation BC `Message.sender_identity_id` 列内容全变**：旧格式 `agent:<ulid>` 不再有效；清库后无 backfill 问题
- **代码中 hardcoded `'system'` 引用需逐一清理**（v2.6-design § 12.2 TQ-4）：grep 代码全库找 `system` literal，改为 NULL actor
- **ID 生成 + collision check**：8 hex = 4_294_967_296 空间；每个 kind 单独空间；远小于 ULID 空间，但 single-machine 场景下 collision rate 可忽略；仍需 IdentityFactory 内部 retry 兜底

## Alternatives Considered

### A. 保留 v2.5 `kind:id` 格式

- ✅ 不动现有
- ❌ 跟 worker-<8hex> 风格分裂
- ❌ 跨表 join + 视觉噪音问题不解
- 否决

### B. 用 ULID 全 ID（无 kind 前缀）

- ✅ 单一格式，最干净
- ❌ 看 ID 不知道 kind；debug / log / UI 显示需额外 JOIN
- ❌ 跟 v2.4 worker-<8hex> 既定风格不符
- 否决

### C. `kind/id` 路径形式（如 `user/hayang`）

- ✅ 类 path-like，可读
- ❌ 跟 URL slug 视觉混淆
- 否决

## References

### v2.6 ADRs

- [ADR-0040 Identity BC carve-out](0040-identity-bc-carve-out.md)
- [ADR-0041 Organization concept + multi-tenant schema](0041-organization-multi-tenant.md)
- [ADR-0042 Member AR](0042-member-ar.md)
- [ADR-0043 Auth v2.6](0043-auth-passcode-jwt.md)
- [ADR-0044 Supervisor cut](0044-supervisor-cut.md)

### 来源

- [ADR-0023 Worker enroll lightweight](0023-worker-enroll-lightweight.md) — worker-style ID 命名起源
- 2026-05-26 oopslink 决策 a-i（multi-worker isolation + id/name split）
- [v2.6-design.md § 2.2 + § 4.2.1](../../plans/v2.6-design.md)
