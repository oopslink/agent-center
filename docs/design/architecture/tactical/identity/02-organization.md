# Organization AR

> **BC**: Identity（BC9）  
> **AR**: Organization  
> 多租户工作空间；所有顶级业务实体（Channel / Project / Worker / AgentInstance / Secret）通过 `organization_id` 引用本 AR。一个 Identity 可同时属于多个 Organization（通过 Member 关系）。

> ADR：[ADR-0041 Organization concept + multi-tenant schema](../../../decisions/0041-organization-multi-tenant.md) + [ADR-0040 Identity BC carve-out](../../../decisions/0040-identity-bc-carve-out.md)

---

## § 1. 状态机

```
created (active) ─── soft-delete ───→ deleted (terminal in v2.6)
```

| 状态 | 标识 | 说明 |
|---|---|---|
| active | `deleted_at IS NULL` | 可正常访问；slug 路由有效 |
| deleted | `deleted_at IS NOT NULL` | 软删；slug 释放可被新 Org 复用；下游资源跨 BC 异步软删 |

v2.6 软删是 terminal —— 不出 re-enable 路径（如需可走 DBA 手工 UPDATE）。硬删（purge）UI 延后 v2.7+（详 [roadmap.md](../../../roadmap.md)）。

---

## § 2. 字段

```
organizations (
  id                       TEXT PRIMARY KEY,   -- 'org-<8hex>'
  slug                     TEXT NOT NULL,      -- URL key; 3-40 chars; lowercase + hyphen
  name                     TEXT NOT NULL,      -- 显示名; 1-80
  description              TEXT,               -- 自由说明
  created_by_identity_id   TEXT NOT NULL,      -- FK identities.id
  created_at               TIMESTAMP NOT NULL,
  updated_at               TIMESTAMP NOT NULL,
  deleted_at               TIMESTAMP           -- 软删时间; NULL = active
);

CREATE UNIQUE INDEX organization_pk ON organizations(id);
CREATE UNIQUE INDEX organization_slug_active_uq
    ON organizations(slug)
    WHERE deleted_at IS NULL;        -- partial: 软删后 slug 可复用
CREATE INDEX organization_created_by_idx
    ON organizations(created_by_identity_id);
```

### 字段说明

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | TEXT | ✓ | `org-<8 hex>`；Factory 生成 + collision retry |
| `slug` | TEXT | ✓ | URL key；regex `^[a-z0-9]([a-z0-9-]{1,38}[a-z0-9])?$`；长度 3-40；用户输入（不 auto-derive） |
| `name` | TEXT | ✓ | 显示名；中英文皆可；1-80 |
| `description` | TEXT | — | 自由文本 |
| `created_by_identity_id` | TEXT | ✓ | 创建者 Identity；首批 owner Member 的 identity_id |
| `deleted_at` | TIMESTAMP | — | 软删时间戳 |

---

## § 3. 生命周期 Ops

### 3.1 Create Organization

任意已登录 Identity（user）都能 create Organization（[ADR-0041 § 4](../../../decisions/0041-organization-multi-tenant.md)）：

入口：
- **Signup 表单**（首次部署）：与 Identity + Member 三件套同事务 create（DS-1）
- **Org switcher 下拉 "+ Create new organization"**（后续）：current Identity 调 `OrganizationCreateService` → 同事务 create Organization + owner Member

```go
func (s *OrganizationCreateService) Create(ctx, form CreateOrgForm, creatorID string) (*Organization, *Member, error) {
    return s.tx.Run(ctx, func(tx Tx) (*Organization, *Member, error) {
        if _, err := s.orgRepo.GetBySlug(tx, form.Slug); err == nil {
            return nil, nil, ErrOrganizationSlugTaken
        }
        org := OrganizationFactory.New(form.Slug, form.Name, creatorID)
        if err := s.orgRepo.Save(tx, org); err != nil { return nil, nil, err }
        member := MemberFactory.New(org.ID, creatorID, RoleOwner, nil)
        if err := s.memberRepo.Save(tx, member); err != nil { return nil, nil, err }
        s.events.Emit(OrganizationCreated{...}, MemberAdded{...})
        return org, member, nil
    })
}
```

### 3.2 Update Organization Settings

`/organizations/{slug}/settings` 页（admin+）：
- `name` editable（admin+）
- `description` editable（admin+）
- `slug` editable（**owner only**；改 slug 时 redirect + 304 缓存清理；新 slug 必通过 uniqueness check）

emit `organization.updated`（payload 含 changed fields）。

### 3.3 Delete Organization（软删）

`/organizations/{slug}/settings` "Delete Organization" 按钮（**owner only**，二次确认）：

走 `OrganizationLifecycleService.Delete`（DS-3，详 [00-overview § 3.5](00-overview.md)）：
1. 同事务设 `organizations.deleted_at = now()`
2. 同事务级联所有 Member.status='disabled'
3. emit `organization.deleted`
4. 下游 BC 订阅事件 → 异步软删自己的 org-scoped 资源（最终一致）

---

## § 4. Invariants

| ID | 描述 | 守门 |
|---|---|---|
| **O1** | `slug` 满足 regex `^[a-z0-9]([a-z0-9-]{1,38}[a-z0-9])?$` | Factory + DB CHECK |
| **O2** | `slug` 在 active Organization (`deleted_at IS NULL`) 中唯一 | DB partial unique index |
| **O3** | `deleted_at` 非 NULL 时，所有该 Org 下的 Member / Channel / Project / Worker / AgentInstance / Secret 必软删（事件驱动级联）| DS-3 + Cross-BC AS-3 |
| **O4** | Organization 创建时必同事务创建创建者的 owner Member 行 | OrganizationCreateService + DS-1 |

### 跨 AR / 跨 BC Invariants 涉及 Organization

- **DS-1** Signup 三件套（Identity + Organization + Member）同事务
- **DS-3** Org 软删 ⇒ Members 同事务级联
- **AS-1** Workforce 实体 organization_id 必引用 active Organization
- **AS-3** 任何 org-scoped 实体的 organization_id 必引用未软删 Org

---

## § 5. Slug 规则细化

```
slug regex: ^[a-z0-9]([a-z0-9-]{1,38}[a-z0-9])?$
```

约束：
- 长度 3-40
- 首尾字符必须是 `[a-z0-9]`
- 中间允许 `[a-z0-9-]`
- 不允许连续 `--`（regex 已覆盖）

示例：

| slug | valid |
|---|---|
| `default` | ✓ |
| `team-alpha` | ✓ |
| `my-org-1` | ✓ |
| `a` | ✗（太短）|
| `-default` | ✗（首字符 `-`）|
| `default-` | ✗（尾字符 `-`）|
| `Team-Alpha` | ✗（大写）|
| `my--org` | ✗（连续 `--`）|

软删后 slug 释放：

| 时刻 | slug=`default` 行 | 新 Organization can use `default` ? |
|---|---|---|
| T0 | active | ✗ |
| T1 (delete) | `deleted_at=T1` | ✓（partial unique index 不覆盖 deleted）|

---

## § 6. Domain Events

| Event | Payload | 触发 |
|---|---|---|
| `organization.created` | id, slug, name, created_by_identity_id | create |
| `organization.updated` | id, slug?, name?, description? | update settings |
| `organization.deleted` | id, deleted_by_identity_id | soft-delete |

`organization.deleted` 是 cross-BC fan-out 事件（下游 BC 订阅做级联软删）。

---

## § 7. URL 路由模型

per [ADR-0041 § 3](../../../decisions/0041-organization-multi-tenant.md)：

```
/organizations              → Organization switcher / list
/organizations/{slug}/       → 该 Org 工作空间根
/organizations/{slug}/...    → 所有 Org-scoped 路由前缀
```

- `slug` 解析到 active Organization → 中间件注入 `current_org_id` 到 ctx
- `slug` 不存在 / 已软删 → 404 + 给出 "Organization not found or has been deleted" 提示
- current_identity 不是该 Org 的 active Member → 403 + 给出 "Not a member of this Organization" 提示

多 tab 并发：因为 URL 自带 slug，同浏览器多 tab 可同时打开不同 Organization，无 cookie state 冲突。

---

## § 8. References

- [00-overview.md](00-overview.md) — Identity BC entry
- [01-identity.md](01-identity.md) — Identity AR
- [03-member.md](03-member.md) — Member AR
- [ADR-0041 Organization](../../../decisions/0041-organization-multi-tenant.md)
- [v2.6-design.md § 4.2.2 + § 5 + § 7](../../../../plans/v2.6-design.md)
