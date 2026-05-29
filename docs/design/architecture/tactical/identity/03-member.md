# Member AR

> **BC**: Identity（BC9）  
> **AR**: Member  
> Identity ↔ Organization 关系；承载 role + status。一个 Identity 可同时是多个 Organization 的 Member；同一 Organization 内 (org_id, identity_id) 唯一。

> ADR：[ADR-0042 Member AR](../../../decisions/0042-member-ar.md) + [ADR-0041 Organization](../../../decisions/0041-organization-multi-tenant.md)

---

## § 1. 状态机

```
joined ─── disable ───→ disabled ─── re-enable ───→ joined
```

| 状态 | member.status | 说明 |
|---|---|---|
| joined | `joined` | 活跃 Member；可访问该 Organization 资源 |
| disabled | `disabled` | 在该 Org 内被禁用；UI 上仍显示在 Members 列表（带"disabled" badge）；Identity 在其它 Org 不受影响 |

区别于 Identity.account_status：
- Member.status 是 **Org 局部状态**
- Identity.account_status 是 **全局账号状态**；disabled 时所有 Member 视作连带 disabled（运行时检查）

---

## § 2. 字段

```
members (
  id                       TEXT PRIMARY KEY,  -- 'mem-<8hex>'
  organization_id          TEXT NOT NULL,     -- FK organizations.id
  identity_id              TEXT NOT NULL,     -- FK identities.id
  role                     TEXT NOT NULL,     -- 'owner' | 'admin' | 'member'
  status                   TEXT NOT NULL DEFAULT 'joined',  -- 'joined' | 'disabled'
  joined_at                TIMESTAMP NOT NULL,
  invited_by_identity_id   TEXT,              -- 创建该 Member 的人；signup 时 NULL
  invited_at               TIMESTAMP,
  disabled_at              TIMESTAMP,
  disabled_reason          TEXT
);

CREATE UNIQUE INDEX member_pk ON members(id);
CREATE UNIQUE INDEX member_org_identity_joined_uq
    ON members(organization_id, identity_id)
    WHERE status = 'joined';                  -- partial: 同 org 同 identity 只能 1 个 joined
CREATE INDEX member_identity_status_idx
    ON members(identity_id, status);
CREATE INDEX member_org_role_idx
    ON members(organization_id, role);
```

### 字段说明

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | TEXT | ✓ | `mem-<8hex>`；Factory 生成 |
| `organization_id` | TEXT | ✓ | FK organizations.id |
| `identity_id` | TEXT | ✓ | FK identities.id |
| `role` | enum | ✓ | `owner` / `admin` / `member` |
| `status` | enum | ✓ | `joined`（默认）/ `disabled` |
| `joined_at` | TIMESTAMP | ✓ | 加入时间 |
| `invited_by_identity_id` | TEXT | — | 创建者 Identity；signup 时为 NULL（自助）|
| `disabled_at` | TIMESTAMP | — | disable 时间戳 |
| `disabled_reason` | TEXT | — | "user-action" / "org-deleted" / "identity-disabled" / 自定义 |

---

## § 3. 生命周期 Ops

### 3.1 Create Member

| 路径 | 触发 | 走的 Service |
|---|---|---|
| Signup | 首次部署，与 Identity + Organization 三件套同事务 | DS-1 SignupService |
| Org Create | 已登录 Identity create 新 Organization | OrganizationCreateService 同事务 create owner Member |
| Add User | owner/admin 在 Members 页 "+ Add User" | MemberAdmissionService.AddUser（新建 Identity + Member 同事务）|
| Add Agent | owner/admin 在 Members 页 "+ Add Agent" | DS-5 AgentIdentityProvisionService |
| Invitation accept | v2.7 实现 | — |

### 3.2 Change Role

走 `MemberRoleChangeService`（DS-2 守门，详 [00-overview § 3.3](00-overview.md)）：

```go
func (s *MemberRoleChangeService) Change(ctx, memberID, newRole, changedBy) error {
    return s.orgLock.WithLock(member.OrgID, func() error {
        return s.tx.Run(ctx, func(tx Tx) error {
            member := ...
            if member.Role == RoleOwner && newRole != RoleOwner {
                count := s.memberRepo.CountActiveOwners(tx, member.OrgID)
                if count <= 1 { return ErrLastOwnerCannotChangeRole }
            }
            member.Role = newRole
            s.memberRepo.Save(tx, member)
            s.events.Emit(MemberRoleChanged{...})
            return nil
        })
    })
}
```

### 3.3 Disable / Re-enable

走 `MemberDisableService` / `MemberReEnableService`（DS-2 守门）：

- Disable：写 `status='disabled'` + `disabled_at = now()` + `disabled_reason`；emit `member.disabled`
- Re-enable：写 `status='joined'` + clear `disabled_at` + `disabled_reason`；emit `member.re_enabled`

Disable 最后一个 owner 拒绝（DS-2 invariant M4）。

### 3.4 Member 物理删除

v2.6 不出 UI；保 audit history。Identity disabled + Member soft-disable 已足够覆盖"清退"场景。

---

## § 4. Invariants

| ID | 描述 | 守门 |
|---|---|---|
| **M1** | `(organization_id, identity_id)` 在 `status='joined'` 内唯一 | DB partial unique index |
| **M2** | 每个 active Org 至少 1 个 `role=owner AND status=joined` Member（[[organization-min-owner]]）| DS-2 in 3 services: MemberRoleChange / MemberDisable / OrgLifecycle |
| **M3** | role 变更不能让 Org 失去最后一个 owner | DS-2 + MemberRoleChangeService |
| **M4** | disable 最后一个 owner Member 不允许 | DS-2 + MemberDisableService |
| **M5** | `disabled_at` 非 NULL ⇔ `status=disabled` | state transition logic + DB CHECK |
| **M6** | Identity.account_status=disabled 时，该 Identity 的所有 Member.status 视作 disabled（运行时检查；不强制写 DB）| Auth Middleware + UI 投射 |

### M2 race condition 解法（重要）

场景：Org 有 2 owner（M1, M2），两个 admin 并发 disable M1 + M2。SQLite 不支持 `SELECT ... FOR UPDATE`，纯 DB transaction 不足以防 race。

**解法**：in-process per-organization mutex（`OrganizationLockManager`）。所有涉及 Org owner 增减的 service（MemberRoleChange / MemberDisable / OrgLifecycle）必先获取该 org 的 mutex 才能进事务。

详 [v2.6-design § 4.8.2 DS-2 Concurrency](../../../../plans/v2.6-design.md)。

---

## § 5. Role 三级 RBAC

```
owner > admin > member
```

| Role | 语义 | 典型操作 |
|---|---|---|
| `owner` | Org 所有者；最高权限 | 改 slug / delete Org / change role to owner / add+remove admin / 一切 admin 能做的 |
| `admin` | 管理员 | add Member / change role admin↔member / disable Member / 业务实体 CRUD others' / 一切 member 能做的 |
| `member` | 普通成员 | 业务实体 CRUD own / read all / 不能管理别人 |

完整 Permission Matrix 见 [v2.6-design § 8](../../../../plans/v2.6-design.md)。

VO `MemberRole.AtLeast(minRole)` 用于 AppService 内权限检查：

```go
if !member.Role.AtLeast(RoleAdmin) { return ErrForbidden }
```

---

## § 6. Domain Events

| Event | Payload | 触发 |
|---|---|---|
| `member.added` | id, org_id, identity_id, role, invited_by? | create Member |
| `member.role_changed` | id, old_role, new_role, changed_by | role change |
| `member.disabled` | id, reason? | disable |
| `member.re_enabled` | id | re-enable |
| `member.removed` | id | 物理删除（v2.6 占位）|

`member.role_changed` 统一发（不分升降级），payload 含 old/new role；Observability 端按需投射（per [v2.6-design 决策 #22](../../../../plans/v2.6-design.md)）。

---

## § 7. Agent 是 Member（M-a 决策）

per [v2.6-design 决策 #12](../../../../plans/v2.6-design.md) + [ADR-0042 § 5](../../../decisions/0042-member-ar.md)：

- Agent Identity 也是 Member；走相同 Member 表 + 相同 role 枚举
- Agent 默认 role=member；如需 admin（替代 supervisor 类行为），由 owner 显式给 admin
- UI 上 Members 页分 Humans tab / Agents tab；底层 query 由 `JOIN identity ON identity.kind` 区分

| UI 列 | DB 来源 |
|---|---|
| display_name | identities.display_name |
| id (`user-` / `agent-`) | identities.id |
| role | members.role |
| status | members.status |
| 🤖 badge | identities.kind = 'agent' |
| "running on `<worker>`" | JOIN agent_instances ON agent_instances.identity_id = identities.id |

---

## § 8. Identity.account_status 联动

M6 invariant：Identity.account_status=disabled 时，该 Identity 的所有 Member 视作 disabled。

实现策略：**运行时检查，不强制写 DB**：
- 查询 Member 时 JOIN Identity，过滤 `identity.account_status='active'`
- Auth Middleware DS-4 fail-safe 已在请求层拦截（Identity disabled 时直接 401）
- 不需要事件驱动级联写 Member.status

理由：
- 减少级联写复杂度
- Identity re-enable 时自动恢复，无需级联清理 Member 行
- DB 只需 1 个 source of truth（Identity.account_status），不需要 Member.status 重复

---

## § 9. References

- [00-overview.md](00-overview.md) — Identity BC entry
- [01-identity.md](01-identity.md) — Identity AR
- [02-organization.md](02-organization.md) — Organization AR
- [ADR-0042 Member AR](../../../decisions/0042-member-ar.md)
- [v2.6-design.md § 4.2.3 + § 4.8.2 DS-2 + § 8 Permission Matrix](../../../../plans/v2.6-design.md)
