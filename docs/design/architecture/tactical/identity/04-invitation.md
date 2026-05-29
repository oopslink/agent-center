# Invitation AR

> **BC**: Identity（BC9）  
> **AR**: Invitation  
> 邀请 Identity 加入某个 Organization 的记录。  
> **⚠ v2.6 仅建 schema 占位；流程实现延后到 v2.7**（详 [roadmap.md `v2.7+ Identity / Auth 进阶 → Email`](../../../roadmap.md)）

> ADR：[ADR-0040 Identity BC carve-out § 2](../../../decisions/0040-identity-bc-carve-out.md)（AR 列表）

---

## § 1. 状态机（v2.7 实现）

```
pending ─── accept ───→ accepted (terminal)
   │
   ├─── expire ────────→ expired  (terminal)
   │
   └─── revoke ────────→ revoked  (terminal)
```

| 状态 | 说明 |
|---|---|
| pending | 已创建未接受；token 有效，在 expires_at 之前 |
| accepted | 已被某 Identity 接受；触发 create Member（如 Identity 不存在则一并 create）|
| expired | 超过 expires_at；定时任务 sweep 标记 |
| revoked | 邀请方主动撤回 |

v2.6 不进入此状态机 —— `status` 字段值固定 `pending`，但**不暴露 create / accept / expire / revoke 入口**。

---

## § 2. 字段（v2.6 落库 schema）

```
invitations (
  id                       TEXT PRIMARY KEY,  -- 'inv-<8hex>'
  organization_id          TEXT NOT NULL,     -- FK organizations.id
  invitee_handle           TEXT NOT NULL,     -- v2.7 决定：display_name 或 email
  role_to_grant            TEXT NOT NULL,     -- 'owner' | 'admin' | 'member'
  invited_by_identity_id   TEXT NOT NULL,     -- FK identities.id
  status                   TEXT NOT NULL DEFAULT 'pending',  -- 'pending'|'accepted'|'expired'|'revoked'
  token                    TEXT NOT NULL,     -- random hex; UNIQUE
  created_at               TIMESTAMP NOT NULL,
  expires_at               TIMESTAMP NOT NULL,
  accepted_by_identity_id  TEXT,
  accepted_at              TIMESTAMP
);

CREATE UNIQUE INDEX invitation_pk ON invitations(id);
CREATE UNIQUE INDEX invitation_token_uq ON invitations(token);
CREATE INDEX invitation_org_status_idx ON invitations(organization_id, status);
```

### 字段说明

| 字段 | 必填 | 说明 |
|---|---|---|
| `id` | ✓ | `inv-<8hex>` |
| `organization_id` | ✓ | 邀请加入的 Org |
| `invitee_handle` | ✓ | 被邀请人标识；v2.7 决定具体语义（display_name lookup or email 投递）|
| `role_to_grant` | ✓ | 接受后 Member 的 role |
| `invited_by_identity_id` | ✓ | 邀请方 Identity（必为该 Org 的 owner/admin Member）|
| `status` | ✓ | 默认 `pending` |
| `token` | ✓ | random 32-byte hex；唯一；用于 invite link |
| `expires_at` | ✓ | 过期时间；v2.7 实际生效；v2.6 schema 必填以避免 NULL 语义模糊 |

---

## § 3. 生命周期 Ops（v2.7 实现）

### 3.1 Create Invitation（v2.7）

owner/admin 在 Members 页 "+ Invite" 触发：

```go
func (s *InvitationCreateService) Create(ctx, form InviteForm, invitedByID string) (*Invitation, error) {
    member := s.memberRepo.GetByOrgAndIdentity(ctx, form.OrgID, invitedByID)
    if !member.Role.AtLeast(RoleAdmin) { return nil, ErrForbidden }
    inv := InvitationFactory.New(form.OrgID, form.InviteeHandle, form.Role, invitedByID, form.ExpiresIn)
    s.invitationRepo.Save(ctx, inv)
    // v2.7: emit email + invite link
    s.events.Emit(InvitationCreated{...})
    return inv, nil
}
```

### 3.2 Accept Invitation（v2.7）

被邀请人点 invite link：
- token 查 Invitation，校验 status=pending + 未 expire
- 如 Identity 不存在 → 引导 signup flow（passcode 设置）→ create Identity + Member 同事务
- 如 Identity 已存在（同 display_name / email）→ signin + create Member 同事务
- emit `invitation.accepted` + `member.added`

### 3.3 Expire（v2.7，定时任务）

cron 扫描 `status=pending AND expires_at < now()` → set `status=expired` + emit `invitation.expired`。

### 3.4 Revoke（v2.7）

邀请方在 Invitations 列表撤回 → set `status=revoked` + emit `invitation.revoked`。

---

## § 4. Invariants（v2.7 实现）

| ID | 描述 | 守门 |
|---|---|---|
| **IV1** | `invited_by_identity_id` 必为同 `organization_id` 的 owner/admin Member | InvitationCreateService |
| **IV2** | accept 时若 Identity 不存在，事务内 create Identity + Member（与 signup 流程同步进行）| InvitationAcceptService |
| **IV3** | `status=accepted` ⇔ `accepted_at` 非空 + `accepted_by_identity_id` 非空 | state transition logic + DB CHECK |

v2.6 不实现这些 invariants —— 表结构存在但不可写（AppService 不暴露 create 入口）。

---

## § 5. Domain Events（v2.7 实现）

| Event | Payload | 触发 |
|---|---|---|
| `invitation.created` | id, org_id, invitee_handle, role_to_grant | create |
| `invitation.accepted` | id, accepted_by_identity_id, member_id | accept |
| `invitation.expired` | id | sweep 时间到 |
| `invitation.revoked` | id, revoked_by | revoke |

v2.6 不发射这些事件。

---

## § 6. 为什么 v2.6 仍建 schema？

@oopslink Q-B 决策（[v2.6-design 决策 #11 + § 11.2](../../../../plans/v2.6-design.md)）：
- v2.6 schema 一次推到底（A2 策略，per [ADR-0041](../../../decisions/0041-organization-multi-tenant.md)）
- 邀请功能从产品形态上是 v2.6 的必然延伸；v2.7 直接复用现有 schema 无破坏性变更
- v2.6 实现成本极低（仅 CREATE TABLE）

v2.6 dev 只需：
- 落 `invitations` 表 schema
- `InvitationRepository` 接口签名 + 空实现（Save / GetByID / GetByToken / ListByOrganization）
- 不出 AppService / Domain Service
- 不出 UI（Members 页 Invite Link 按钮等 v2.7）

---

## § 7. References

- [00-overview.md](00-overview.md) — Identity BC entry
- [03-member.md](03-member.md) — Member AR（接受邀请的产物）
- [ADR-0040 § 2](../../../decisions/0040-identity-bc-carve-out.md) — AR 列表
- [v2.6-design § 4.2.4 + § 11.2](../../../../plans/v2.6-design.md)
- [roadmap.md `v2.7+ Identity / Auth 进阶 → Email`](../../../roadmap.md)
