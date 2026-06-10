# Identity AR

> **BC**: Identity（BC9）  
> **AR**: Identity  
> 全局唯一身份记录；表示某个 actor（人类或程序）在系统中的身份标识。一个 Identity 不属于任何 Organization，跨 Org 复用。

> ADR：[ADR-0040 Identity BC carve-out](../../../decisions/0040-identity-bc-carve-out.md) + [ADR-0045 Identity ID format](../../../decisions/0045-identity-id-format.md) + [ADR-0043 Auth v2.6](../../../decisions/0043-auth-passcode-jwt.md)

---

## § 1. 状态机

```
created (active) ─── disable ───→ disabled
       ↑                              │
       └─────── re-enable ────────────┘
```

| 状态 | account_status 值 | 说明 |
|---|---|---|
| active | `active` | 可正常 signin / 作为 actor / 接收 message |
| disabled | `disabled` | 全局禁用；所有 Member 视作连带 disabled；JWT 即时失效（DS-4 fail-safe）|

两个状态都不是终态 —— `disabled` 可 re-enable 回 `active`。

---

## § 2. 字段

```
identity (
  id              TEXT PRIMARY KEY,   -- 'user-<8hex>' 或 'agent-<8hex>'
  kind            TEXT NOT NULL,      -- 'user' | 'agent' (CHECK constraint)
  display_name    TEXT NOT NULL,      -- 1-40 字符
  description     TEXT,               -- 可空，自由文本
  account_status  TEXT NOT NULL DEFAULT 'active',  -- 'active' | 'disabled'
  passcode_hash   TEXT,               -- argon2id；user 必填；agent 必 NULL
  passcode_set_at TIMESTAMP,          -- user 必填
  created_at      TIMESTAMP NOT NULL,
  updated_at      TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX identity_pk ON identity(id);
CREATE UNIQUE INDEX identity_display_name_user_uq
    ON identity(display_name)
    WHERE kind = 'user';                -- partial unique: signin 歧义防护
CREATE INDEX identity_kind_status_idx
    ON identity(kind, account_status);
```

### 字段说明

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | TEXT | ✓ | `<kind>-<8 hex chars>`；IdentityFactory 内生成 + collision retry |
| `kind` | enum | ✓ | `user`（passcode auth）/ `agent`（cert-pin + worker-token） |
| `display_name` | TEXT | ✓ | UI / signin form 用；user kind 内全局唯一；长度 1-40 |
| `description` | TEXT | — | 自由说明 |
| `account_status` | enum | ✓ | `active`（默认）/ `disabled` |
| `passcode_hash` | TEXT | user-only | argon2id（iter=3, mem=64MiB, par=4） |
| `passcode_set_at` | TIMESTAMP | user-only | passcode 设置/修改时间 |
| `created_at` / `updated_at` | TIMESTAMP | ✓ | 标准 audit |

---

## § 3. 生命周期 Ops

### 3.1 Create User Identity

走 `SignupService`（DS-1，详 [00-overview § 3.1](00-overview.md)）—— 与 Organization + Member 三件套同事务。

不允许独立 create User Identity（v2.6 没有"先有 Identity 后建 Org"路径）。

### 3.2 Create Agent Identity

走 `AgentIdentityProvisionService`（DS-5）—— 同事务 create Identity[kind=agent] + Member（指定 role + org）。

由 owner/admin 在 UI `/organizations/{slug}/members/agents` "+ Add Agent" 触发。

### 3.3 Change Passcode（user kind only）

`/me` 页提供（v2.6 必出）：
- 旧 passcode 校验
- 新 passcode + confirm（长度 6–128，且至少含 1 个字母、1 个数字、1 个符号）
- emit `identity.passcode_changed`

### 3.4 Change Display Name / Description

`/me` 页（user）/ Members 页 admin 操作（agent）：
- update + emit `identity.updated`（如有；可合并到 member.updated 事件）

### 3.5 Disable / Re-enable

由 admin 在 Members 页操作：
- 写 `account_status = 'disabled'` + emit `identity.account_disabled`
- 该 Identity 的所有 Member 运行时视作 disabled（不强制写 DB；查询时通过 Identity.account_status JOIN 检查）
- DS-4 fail-safe：Auth Middleware 每请求重读 account_status

---

## § 4. Invariants

| ID | 描述 | 守门 |
|---|---|---|
| **I1** | `kind=user` ⇒ `passcode_hash` 非空 + `passcode_set_at` 非空 | IdentityFactory.NewUser + DB CHECK |
| **I2** | `kind=agent` ⇒ `passcode_hash` IS NULL | IdentityFactory.NewAgent + DB CHECK |
| **I3** | `id` 前缀必须匹配 `kind`（`user-<8hex>` ⇔ user / `agent-<8hex>` ⇔ agent）| Factory + DB CHECK（per [ADR-0045](../../../decisions/0045-identity-id-format.md)）|
| **I4** | `display_name` 非空，长度 1-40 | Factory + DB CHECK |
| **I5** | `account_status='disabled'` 是可恢复状态（不是终态）；可 re-enable 回 'active' | state transition logic |

### 跨 AR Invariants 涉及 Identity

- **DS-1** Signup：Identity + Organization + Member 同事务（详 [DS-1 in 00-overview § 2.2](00-overview.md)）
- **DS-4** Identity.account_status=disabled ⇒ JWT 即时失效
- **DS-5** Agent Identity create 必同事务 create Member

---

## § 5. Domain Events

| Event | Payload 关键字段 | 触发 |
|---|---|---|
| `identity.created` | id, kind, display_name | Signup 完成 / Agent provision 完成 |
| `identity.passcode_changed` | id | passcode 改 |
| `identity.updated` | id, changed_fields | display_name / description 改 |
| `identity.account_disabled` | id, reason? | disable |
| `identity.account_re_enabled` | id | re-enable |

---

## § 6. ID 生成策略

per [ADR-0045](../../../decisions/0045-identity-id-format.md)：

```go
func generateIdentityID(kind IdentityKind, repo IdentityRepository) (string, error) {
    for retry := 0; retry < 5; retry++ {
        hex := randomHex(8)  // 8 chars from [0-9a-f]
        id := fmt.Sprintf("%s-%s", kind, hex)
        existing, _ := repo.GetByID(ctx, id)
        if existing == nil { return id, nil }
    }
    return "", errors.New("identity id generation collision after 5 retries")
}
```

8 hex = 32-bit space = 4_294_967_296 per kind；collision rate 单机自用场景可忽略；retry 兜底。

---

## § 7. AgentInstance ↔ Identity 显式 FK 关联

per [ADR-0045 § 4](../../../decisions/0045-identity-id-format.md)：

```
agent_instances (
  id            ULID PRIMARY KEY,   -- AgentInstance 自身 id（不变）
  identity_id   TEXT NOT NULL,      -- FK identities.id（kind=agent）
  organization_id TEXT NOT NULL,    -- FK organizations.id
  ...
);
```

- AgentInstance.id（ULID）跟 Identity.id（agent-<8hex>）独立生成，不再 1:1 派生
- AgentInstance create 同事务 INSERT Identity 行（DS-5）
- AgentInstance archive 时 Identity 保留（历史 message sender 引用不能断；维持 [ADR-0033 § 3](../../../decisions/0033-identity-model-refactor.md) 精神）

---

## § 8. References

- [00-overview.md](00-overview.md) — Identity BC entry
- [02-organization.md](02-organization.md) — Organization AR
- [03-member.md](03-member.md) — Member AR
- [ADR-0040](../../../decisions/0040-identity-bc-carve-out.md) / [0043](../../../decisions/0043-auth-passcode-jwt.md) / [0045](../../../decisions/0045-identity-id-format.md)
- [v2.6-design.md § 4.2.1](../../../../plans/v2.6-design.md)
