# Identity BC — DDD 战术设计 Overview

> **DDD 战术层** · BC: Identity（BC9）
>
> 系统内 actor / multi-tenant / permission 三联挂载点。v2.6 周期从 Conversation BC 抽出 Identity AR 并新增 Organization / Member / Invitation 三 AR（详 [ADR-0040 Identity BC carve-out](../../../decisions/0040-identity-bc-carve-out.md)）。
>
> Identity BC 是 Customer-Supplier 上游：所有其它 BC 通过 published ID 引用本 BC 的 Identity / Organization / Member；跨 BC 协调通过 `IdentityBCFacade` 暴露 read-only 查询给下游。

> 定位 ADR：[ADR-0040](../../../decisions/0040-identity-bc-carve-out.md)（BC carve-out）/ [ADR-0041](../../../decisions/0041-organization-multi-tenant.md)（Organization）/ [ADR-0042](../../../decisions/0042-member-ar.md)（Member）/ [ADR-0043](../../../decisions/0043-auth-passcode-jwt.md)（Auth）/ [ADR-0045](../../../decisions/0045-identity-id-format.md)（ID 格式）。

---

## § 0. BC 一览

### 0.1 职责

| 维度 | 内容 |
|---|---|
| **聚合管理** | Identity / Organization / Member / Invitation（4 AR）|
| **Auth 承载** | passcode (argon2id) + JWT cookie + signup / signin / logout 全流程（per [ADR-0043](../../../decisions/0043-auth-passcode-jwt.md)）|
| **多租户根** | Organization 是所有顶级业务实体（Channel / Project / Worker / AgentInstance / Secret）的 org_id 引用根 |
| **Permission 挂载** | Member.role 三级 RBAC (owner / admin / member) + Identity.account_status 二态（详 [ADR-0042](../../../decisions/0042-member-ar.md) + [v2.6-design § 8](../../../../plans/v2.6-design.md)）|

### 0.2 UL 切片

来自 [v2.6-design § 2](../../../../plans/v2.6-design.md) 标 Identity BC 的术语：

- `Identity`（AR，全局，kind=user / agent）
- `Organization`（AR，多租户）
- `Org Slug`（VO，URL key）
- `Member`（AR，Identity↔Org 关系）
- `Member Role`（VO，owner / admin / member）
- `Member Status`（VO，joined / disabled）
- `Account Status`（VO，active / disabled）
- `Passcode`（VO，6 位数字，argon2 hash）
- `Invitation`（AR，v2.7）
- 行为动词：`Signup` / `Signin` / `Logout` / `Provision`（Agent Identity）/ `Disable` / `Re-enable`

### 0.3 Context Map 位置

[strategic/03-bounded-contexts § 3](../../strategic/03-bounded-contexts.md)（v2.6 update）：

- **Identity → Conversation**：Customer-Supplier（`Message.sender_identity_id` 弱引用 `Identity.id`；AppService 写时校验）
- **Identity → Workforce**：Customer-Supplier（`AgentInstance.identity_id` FK + `worker.organization_id` 弱引用；AS-1）
- **Identity → TaskRuntime**：Customer-Supplier（`task.created_by_identity_id` / actor 弱引用）
- **Identity → SecretManagement**：Customer-Supplier（`user_secrets.organization_id` 弱引用）
- **Identity ← Observability**：Open Host（订阅 `identity.*` / `organization.*` / `member.*` / `auth.*` events）

跨 BC 一致性窗口：弱关联，Identity / Organization 软删走 Domain Event 驱动级联（最终一致），详 § 7.3。

---

## § 1. 聚合清单（X.1）

### 1.1 Aggregate Roots

| 聚合 | 文件 | 状态机 | 身份 / 不变性 |
|---|---|---|---|
| **Identity** | [01-identity.md](01-identity.md) | 2 态（active / disabled）| `<kind>-<8hex>` worker-style ID；身份不变；kind 不可变 |
| **Organization** | [02-organization.md](02-organization.md) | 2 态（active / deleted-soft）| `org-<8hex>`；slug 在 active 行内唯一 |
| **Member** | [03-member.md](03-member.md) | 2 态（joined / disabled）| `mem-<8hex>`；(org_id, identity_id) 唯一（status=joined）|
| **Invitation** | [04-invitation.md](04-invitation.md) | 4 态（pending / accepted / expired / revoked）| `inv-<8hex>`；token UNIQUE；**v2.6 schema only，流程 v2.7** |

### 1.2 Entity（子从属）

无。BC9 4 AR 都独立。

### 1.3 Value Objects

| VO | 用在哪 | 描述 |
|---|---|---|
| **IdentityKind** | `identity.kind` | `user | agent`（v2.6 删 `system` per [ADR-0045](../../../decisions/0045-identity-id-format.md)）|
| **IdentityId** | `identity.id` 等所有引用列 | `kind-<8hex>` 格式包装；validate prefix |
| **OrgSlug** | `organization.slug` | regex `^[a-z0-9]([a-z0-9-]{1,38}[a-z0-9])?$` |
| **MemberRole** | `member.role` | `owner | admin | member`；hierarchy 比较 owner > admin > member |
| **MemberStatus** | `member.status` | `joined | disabled` |
| **AccountStatus** | `identity.account_status` | `active | disabled` |
| **PasscodeHash** | `identity.passcode_hash` | argon2id hash 包装；构造时 hash + verify |
| **InvitationToken** | `invitation.token` | random 32 字节 hex；UNIQUE |
| **JWTClaims** | session cookie | `{ sub=identity_id, exp, iat, jti }`；HS256 + master_key 签名（[ADR-0043 § 6](../../../decisions/0043-auth-passcode-jwt.md)）|

---

## § 2. Invariants 索引（X.2）

每个聚合自己维护 invariants 节，本 § 仅做索引（详 [v2.6-design § 4.8](../../../../plans/v2.6-design.md)）：

### 2.1 AR-internal

- **Identity Invariants (I1-I5)** → [01-identity.md § 4](01-identity.md)
- **Organization Invariants (O1-O4)** → [02-organization.md § 4](02-organization.md)
- **Member Invariants (M1-M6)** → [03-member.md § 4](03-member.md)
- **Invitation Invariants (IV1-IV3)** → [04-invitation.md § 4](04-invitation.md)（v2.7 实现）

### 2.2 跨 AR（Domain Service 守门）

5 个 Domain Service 守 cross-AR invariants：

| ID | Invariant | 守门 |
|---|---|---|
| **DS-1** | Signup 三件套同事务（Identity + Organization + Member）| SignupService |
| **DS-2** | [[organization-min-owner]]：每 Org ≥1 active owner Member | MemberRoleChangeService + MemberDisableService |
| **DS-3** | Org 软删 ⇒ Members 同事务级联 disabled | OrganizationLifecycleService |
| **DS-4** | Identity.account_status=disabled ⇒ JWT 即时失效 | Auth Middleware |
| **DS-5** | Agent Identity + Member 同事务 | AgentIdentityProvisionService |

详 § 3 Domain Service + [v2.6-design § 4.8.2](../../../../plans/v2.6-design.md)。

### 2.3 跨 BC（Application Service 协调）

3 个跨 BC invariants：

| ID | Invariant | 协调 |
|---|---|---|
| **AS-1** | Workforce 实体 organization_id 必引用 active Organization | Workforce AppService 调 IdentityBCFacade |
| **AS-2** | Conversation Message.sender_identity_id 必引用存在 Identity | Conversation AppService 调 IdentityBCFacade |
| **AS-3** | 任何 org-scoped 实体的 organization_id 必引用未软删 Org | 各 BC AppService 写时校验 + organization.deleted 事件驱动级联清理 |

---

## § 3. Domain Services（X.3）

### 3.1 SignupService

**职责**：接受 signup 表单 → 一事务里 create Identity + Organization + Member (role=owner)。守 DS-1。

| 维度 | 内容 |
|---|---|
| 入参 | `SignupForm{DisplayName, Passcode, FirstOrgName, FirstOrgSlug}` |
| 出参 | `{Identity, Organization, Member, JWT}` |
| 跨聚合 | Identity + Organization + Member 同事务 |
| 错误码 | `ErrIdentityDisplayNameTaken` / `ErrOrganizationSlugTaken` / `ErrOrganizationSlugInvalid` / form validation 错误 |
| Events | `identity.created` + `organization.created` + `member.added` |

伪代码详见 [v2.6-design § 4.8.2 DS-1](../../../../plans/v2.6-design.md)。

### 3.2 SigninService

**职责**：display_name + passcode → JWT。

| 维度 | 内容 |
|---|---|
| 入参 | `{DisplayName, Passcode}` |
| 出参 | `JWT` |
| 错误码 | `ErrPasscodeInvalid`（统一，不暴露 enumeration）|
| Events | 成功 `auth.signed_in` / 失败 `auth.signin_failed` |

### 3.3 MemberRoleChangeService

**职责**：变更 Member.role + 守 DS-2 [[organization-min-owner]]。

| 维度 | 内容 |
|---|---|
| 入参 | `{MemberID, NewRole, ChangedByIdentityID}` |
| 出参 | 更新 Member.role + emit `member.role_changed` |
| 并发保护 | in-process per-org mutex（`OrganizationLockManager`，SQLite 不支持 SELECT FOR UPDATE 的补偿）|
| 错误码 | `ErrLastOwnerCannotChangeRole` / `ErrMemberNotFound` / `ErrForbidden` |

### 3.4 MemberDisableService

**职责**：disable Member + 守 DS-2。

| 维度 | 内容 |
|---|---|
| 入参 | `{MemberID, Reason}` |
| 出参 | Member.status=disabled + emit `member.disabled` |
| 并发保护 | 同 § 3.3 per-org mutex |
| 错误码 | `ErrLastOwnerCannotDisable` / `ErrMemberNotFound` |

### 3.5 OrganizationLifecycleService

**职责**：Org 软删（+ Members 级联软删）+ emit `organization.deleted`。守 DS-3。

| 维度 | 内容 |
|---|---|
| 入参 | `{OrgID, DeletedByIdentityID}` |
| 出参 | Organization.deleted_at + 所有 Member.status=disabled（同事务）+ emit `organization.deleted` |
| 并发保护 | 同 § 3.3 per-org mutex |
| 跨 BC 级联 | 异步事件驱动；下游 BC 订阅 `organization.deleted` 软删自己的 org-scoped 资源 |

### 3.6 AgentIdentityProvisionService

**职责**：owner/admin 在 UI "Add Agent" 触发 create Identity[kind=agent] + Member 同事务。守 DS-5。

| 维度 | 内容 |
|---|---|
| 入参 | `AgentProvisionForm{DisplayName, Description, Role, WorkerID?}` + `OrgID` + `ProvisionedByIdentityID` |
| 出参 | `{Identity, Member}` + (if WorkerID) Workforce 协调 register AgentInstance |
| 错误码 | `ErrForbidden`（非 admin 调用）/ form validation |
| Events | `identity.created` + `member.added` |

### 3.7 Auth Middleware（非 Service 但承载 DS-4）

**职责**：每请求 JWT verify + DS-4 fail-safe 检查 Identity.account_status。

伪代码详见 [v2.6-design § 4.8.2 DS-4](../../../../plans/v2.6-design.md)。

---

## § 4. Factories（X.4）

| Factory | 入参 | 产出 |
|---|---|---|
| `IdentityFactory.NewUser(displayName, passcodePlain)` | display_name + passcode 明文 | Identity[kind=user] 实例（passcode 已 argon2 hash）|
| `IdentityFactory.NewAgent(displayName, description)` | display_name + 描述 | Identity[kind=agent] 实例（无 passcode）|
| `OrganizationFactory.New(slug, name, createdByIdentityID)` | slug + name + 创建者 ID | Organization 实例 |
| `MemberFactory.New(orgID, identityID, role, invitedBy?)` | 关键字段 | Member 实例 |
| `InvitationFactory.New(orgID, inviteeHandle, role, invitedBy, expiresIn)` | ... | Invitation 实例（v2.7）|

ID 生成在 Factory 内部完成（8 hex random + collision retry）。

---

## § 5. Repositories（X.5）

Go-style 接口签名 + sentinel error pattern：

```go
package identity

type IdentityRepository interface {
    Save(ctx context.Context, id *Identity) error
    GetByID(ctx context.Context, id string) (*Identity, error)
    GetByDisplayName(ctx context.Context, name string) (*Identity, error)
    List(ctx context.Context, filter ListFilter) ([]*Identity, error)
}

type OrganizationRepository interface {
    Save(ctx context.Context, org *Organization) error
    GetByID(ctx context.Context, id string) (*Organization, error)
    GetBySlug(ctx context.Context, slug string) (*Organization, error)
    ListForIdentity(ctx context.Context, identityID string) ([]*Organization, error)
    SoftDelete(ctx context.Context, id string) error
}

type MemberRepository interface {
    Save(ctx context.Context, m *Member) error
    GetByID(ctx context.Context, id string) (*Member, error)
    GetByOrganizationAndIdentity(ctx context.Context, orgID, identityID string) (*Member, error)
    ListByOrganization(ctx context.Context, orgID string) ([]*Member, error)
    CountActiveOwners(ctx context.Context, orgID string) (int, error)
}

type InvitationRepository interface {
    Save(ctx context.Context, inv *Invitation) error
    GetByID(ctx context.Context, id string) (*Invitation, error)
    GetByToken(ctx context.Context, token string) (*Invitation, error)
    ListByOrganization(ctx context.Context, orgID string) ([]*Invitation, error)
}

// Domain errors (sentinel pattern per conventions § 0.3)
var (
    ErrIdentityNotFound              = errors.New("identity not found")
    ErrIdentityDisplayNameTaken      = errors.New("identity display_name taken")
    ErrOrganizationNotFound          = errors.New("organization not found")
    ErrOrganizationNotFoundOrDeleted = errors.New("organization not found or deleted")
    ErrOrganizationSlugTaken         = errors.New("organization slug taken")
    ErrOrganizationSlugInvalid       = errors.New("organization slug format invalid")
    ErrMemberNotFound                = errors.New("member not found")
    ErrMemberAlreadyExists           = errors.New("member already exists in organization")
    ErrLastOwnerCannotChangeRole     = errors.New("cannot change role of last owner of organization")
    ErrLastOwnerCannotDisable        = errors.New("cannot disable last owner of organization")
    ErrInvitationNotFound            = errors.New("invitation not found")
    ErrInvitationExpired             = errors.New("invitation expired")
    ErrPasscodeInvalid               = errors.New("passcode incorrect")
    ErrForbidden                     = errors.New("forbidden: insufficient member role")
)
```

---

## § 6. 跨聚合引用 + 跨 BC 交互（X.6）

### 6.1 BC 内跨聚合引用

| 引用方 → 被引用 | 关系 | 一致性 |
|---|---|---|
| `Member.organization_id` → `Organization.id` | 强引用（FK） | 同事务 |
| `Member.identity_id` → `Identity.id` | 强引用（FK） | 同事务 |
| `Member.invited_by_identity_id` → `Identity.id` | 弱引用（nullable）| 写时校验 |
| `Organization.created_by_identity_id` → `Identity.id` | 强引用 | 写时校验 |
| `Invitation.organization_id` → `Organization.id` | 强引用 | v2.7 |
| `Invitation.invited_by_identity_id` → `Identity.id` | 强引用 | v2.7 |

### 6.2 跨 BC 交互（Identity BC 是上游 Supplier）

下游 BC 通过 `IdentityBCFacade` 调用，**不直读 Identity BC 的 Repository**：

```go
type IdentityBCFacade interface {
    // 校验类
    GetActiveOrganization(ctx context.Context, orgID string) (*OrganizationSummary, error)
    IdentityExists(ctx context.Context, identityID string) (bool, error)
    MemberRoleForIdentity(ctx context.Context, orgID, identityID string) (MemberRole, error)
    AtLeast(ctx context.Context, orgID, identityID string, minRole MemberRole) (bool, error)

    // 关联类
    ListMembersOfOrganization(ctx context.Context, orgID string) ([]*MemberSummary, error)
    GetIdentityDisplayName(ctx context.Context, identityID string) (string, error)
}
```

### 6.3 跨 BC 事件订阅（Identity BC 作为 Event Source）

下游 BC 订阅 Identity BC 发出的 Domain Events 做异步处理：

| Event | 下游 BC | 反应 |
|---|---|---|
| `organization.deleted` | Workforce | soft-delete Worker / AgentInstance / Project where org=X |
| `organization.deleted` | Conversation | soft-delete Channel where org=X |
| `organization.deleted` | SecretManagement | soft-delete UserSecret where org=X |
| `organization.deleted` | TaskRuntime | （间接，通过 Channel 级联）|
| `identity.account_disabled` | （全 BC） | 中间件 DS-4 即时拦截；其它 BC 数据保留 |
| `member.disabled` | （UI 投射）| 该 Identity 失去对该 Org 资源访问；数据保留 |

事件订阅故障容忍：v2.6 in-memory event bus；soft-delete idempotent；进程重启重跑无副作用。v2.7+ 升级 outbox pattern。

---

## § 7. Out of Scope

- **Email 注册 / Password Reset / 2FA / SSO / PAT**：详 [roadmap.md `v2.7+ Identity / Auth 进阶`](../../../roadmap.md)
- **Account Switcher（多 Identity 同浏览器）**：v2.7+
- **ABAC fine-grained permission**：v3+
- **Org 硬删除（purge）UI**：v2.7+
- **Identity self-disable UI**（`/me` Delete Account）：v2.7+
- **Invitation 流程 + Email 投递**：v2.7（v2.6 schema only）

---

## § 8. References

### v2.6 ADRs

- [ADR-0040 Identity BC carve-out](../../../decisions/0040-identity-bc-carve-out.md)
- [ADR-0041 Organization concept + multi-tenant schema](../../../decisions/0041-organization-multi-tenant.md)
- [ADR-0042 Member AR](../../../decisions/0042-member-ar.md)
- [ADR-0043 Auth v2.6 (passcode + JWT)](../../../decisions/0043-auth-passcode-jwt.md)
- [ADR-0044 Supervisor cut](../../../decisions/0044-supervisor-cut.md)
- [ADR-0045 Identity ID format](../../../decisions/0045-identity-id-format.md)

### 影响 BC（v2.6 后需 cross-reference 调整）

- [tactical/conversation/02-identity.md](../conversation/02-identity.md) — 归档 historical（Identity 已迁出）
- [tactical/workforce/](../workforce/) — `agent_instances.identity_id` FK 显式关联
- [tactical/cognition/](../cognition/) — Supervisor 砍除（[ADR-0044](../../../decisions/0044-supervisor-cut.md)）

### 来源

- [v2.6-design.md](../../../../plans/v2.6-design.md) § 4 + § 5 + § 6 + § 7 + § 8
- 2026-05-26 / 2026-05-27 #agent-center DM（@oopslink ↔ @AgentCenterPD）
