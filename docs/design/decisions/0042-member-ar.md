# 0042. Member AR (Identity↔Organization)（v2.6）

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-05-27 |
| Delivered | v2.6 design phase；详 [v2.6-design § 4.2.3 / § 4.8.2 DS-2 / § 8](../../plans/v2.6-design.md) |
| Supersedes | — |
| Related | [ADR-0040 Identity BC carve-out](0040-identity-bc-carve-out.md) / [ADR-0041 Organization](0041-organization-multi-tenant.md) / [ADR-0043 Auth](0043-auth-passcode-jwt.md) |

## Context

v2.6 引入 Organization 概念后，需要承载"某个 Identity 属于某个 Org 且有什么权限"的关系：

- Identity 是全局唯一的（跨 Org 复用）
- Organization 是多租户工作空间
- 一个 Identity 可同时属于多个 Org，且在不同 Org 内可能 role 不同

把 role 直接挂在 Identity 上违反"一个 Identity 在 Org A 是 owner、在 Org B 是 member"的核心需求。**Member 是必备的关系 AR**。

## Decision

### 1. Member 作为 BC9 Identity 的 AR

```
Member AR
├── id                       string PK    // "mem-<8hex>"
├── organization_id          string       // FK Organization.id
├── identity_id              string       // FK Identity.id
├── role                     enum         // owner | admin | member
├── status                   enum         // joined | disabled
├── joined_at                timestamp
├── invited_by_identity_id   string?      // 创建该 Member 的人；signup-bootstrap 时 NULL
├── invited_at               timestamp?
├── disabled_at              timestamp?
└── disabled_reason          string?

INDEXES:
- UNIQUE (organization_id, identity_id) WHERE status='joined'
- INDEX (identity_id, status)
- INDEX (organization_id, role)
```

详 AR 形态见 [tactical/identity/03-member.md](../architecture/tactical/identity/03-member.md)。

### 2. Role 三级 RBAC

```
owner > admin > member
```

| Role | 语义 |
|---|---|
| `owner` | Org 所有者；最高权限；可改 slug；可 delete Org；可 change Member role to owner |
| `admin` | 管理员；可 add Member / change role admin↔member / disable Member；不能改 slug / delete Org |
| `member` | 普通成员；可创建 / 编辑自己的资源；不能管理别人 |

详 Permission Matrix 见 [v2.6-design § 8](../../plans/v2.6-design.md)。

### 3. Member.status 二态（joined / disabled）

- `joined`：活跃 Member
- `disabled`：在该 Org 内被禁用（不可访问该 Org 资源，但 Identity 本身在其它 Org 不受影响）

区别于 `Identity.account_status`（账号级开关，disabled = 全局所有 Member 视作连带失效）：

| 场景 | 操作 | 效果 |
|---|---|---|
| Owner 离开 Org A | `Member.status = disabled` | Identity 本身可继续 Member 别的 Org |
| Owner 注销账号 | `Identity.account_status = disabled` | 该 Identity 所有 Member 视作 disabled |

中间件层 fail-safe：`Identity.account_status = disabled` 时，JWT verify 即时拒绝（DS-4）。

### 4. AR-internal Invariants

| ID | 描述 |
|---|---|
| M1 | `(organization_id, identity_id)` 在 `status='joined'` 内唯一 |
| M2 | 每个 active Org 至少有 1 个 `role=owner AND status=joined` 的 Member（[[organization-min-owner]]）|
| M3 | role 变更不能让 Org 失去最后一个 owner |
| M4 | disable 最后一个 owner Member 不允许 |
| M5 | `disabled_at` 非 NULL ⇔ `status=disabled` |
| M6 | Identity.account_status=disabled 时，该 Identity 的所有 Member 视作 disabled（运行时检查，不强制写 DB） |

M2 / M3 / M4 由 `MemberRoleChangeService` + `MemberDisableService` 守门，使用 in-process per-org mutex 保护并发（详 [v2.6-design § 4.8.2 DS-2](../../plans/v2.6-design.md) Concurrency 节）。

### 5. Agent 是 Member（M-a 决策）

[v2.6-design 决策 #12](../../plans/v2.6-design.md)：Agent 也是 Member，跟 human 用同一个 Member.role 枚举：

```
Member (kind=agent Identity 也能加入)
├── role: owner | admin | member  (默认 member)
└── ...
```

- Agent Identity 在 owner/admin 的 "Add Agent" 表单触发下创建（DS-5 `AgentIdentityProvisionService`），同事务 create Identity + Member
- Agent 默认 role=member；如需 admin 权限（特殊用途，如代替 supervisor 行为）由 owner 显式给 admin role
- 在 `/organizations/{slug}/members/agents` tab 列出（详 [v2.6-design § 7.3.2](../../plans/v2.6-design.md)）

### 6. 创建 Member 路径

| 路径 | 谁 | 何时 |
|---|---|---|
| Signup | 任意未登录访问 | DB 空时通过 `/signup` 创建 owner Identity + Org + Member 三件套 |
| Add User | owner/admin | `/organizations/{slug}/members/humans` "+ Add User"，表单输入新 user 的 display_name + 临时 passcode + role |
| Add Agent | owner/admin | `/organizations/{slug}/members/agents` "+ Add Agent"，表单输入 agent display_name + description + role + worker binding |
| Invitation accept | invitee（v2.7）| Invitation 流程 v2.6 不出（schema 占位）|

### 7. Member.role 变更 / disable Domain Events

```
member.added            (id, org_id, identity_id, role, invited_by?)
member.role_changed     (id, old_role, new_role, changed_by)
member.disabled         (id, reason?)
member.re_enabled       (id)
member.removed          (id)  -- v2.6 不出 UI；保留 verb 占位
```

role 升降级统一 `member.role_changed`（[v2.6-design 决策 #22](../../plans/v2.6-design.md)），payload 含 old/new role，Observability 端按需投射"升级 / 降级 / 同级换"。

## Consequences

### 正面

- Identity / Organization / Member 三 AR 模型对称、清晰
- 多 Org 内同一 Identity 可有不同 role
- 三级 RBAC 简单 + 满足 v2.6 自用 + 小团队场景
- Agent 也走 Member 路径，权限模型一致（无需特例）

### 负面 / 待跟进

- **并发 race condition 防护**：DS-2 last-owner 检查需要 in-process per-org mutex（SQLite 不支持 SELECT FOR UPDATE）
- **AS-2 弱关联跨 BC**：Conversation BC 读 Member.role 做权限投射时走 IdentityBCFacade；FK 不强约束
- **细粒度权限不足**：v2.6 三级 RBAC 满足通用场景；如需 "某 Channel 只给特定 Member 写" 等需求得等 v2.7+ ABAC
- **Member 物理删除 v2.6 不出**：保 audit history；Identity 全局禁用 + Member soft-disable 已足够

## Alternatives Considered

### A. role 挂在 Identity 上（无 Member AR）

- ✅ 模型最简
- ❌ 同一 Identity 在不同 Org 不能有不同 role
- ❌ 多租户场景核心需求失败
- 否决（核心问题）

### B. Member.role 用单 boolean (is_admin)

- ✅ 极简
- ❌ owner / admin / member 三级用单 bool 表达不下
- ❌ owner 概念在 [[organization-min-owner]] invariant 中必备
- 否决

### C. Agent 独立 OrgAgent 表（不走 Member）

- ✅ Member 表只装 human Identity，role 语义干净
- ❌ 多一张表 + 多套 CRUD
- ❌ 模型不对称：human 和 agent 在权限层应一视同仁
- 否决（[v2.6-design 决策 #12](../../plans/v2.6-design.md) M-a 路径）

### D. Role 枚举扩展第四个 `agent`

- ✅ 单表
- ❌ role 字段两个语义混合（权限级 vs Identity kind）
- ❌ Permission Matrix 复杂度增加
- 否决

## References

### v2.6 ADRs

- [ADR-0040 Identity BC carve-out](0040-identity-bc-carve-out.md)
- [ADR-0041 Organization concept](0041-organization-multi-tenant.md)
- [ADR-0043 Auth v2.6](0043-auth-passcode-jwt.md)
- [ADR-0045 Identity ID format](0045-identity-id-format.md)

### 影响 BC

- [tactical/identity/03-member.md](../architecture/tactical/identity/03-member.md)

### 来源

- 2026-05-27 #agent-center DM（@oopslink Member 模型讨论）
- [v2.6-design.md](../../plans/v2.6-design.md) § 4.2.3 + § 4.8.2 DS-2 + § 8
