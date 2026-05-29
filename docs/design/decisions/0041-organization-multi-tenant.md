# 0041. Organization concept + multi-tenant schema（v2.6）

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-05-27 |
| Delivered | v2.6 design phase；详 [v2.6-design § 4.2.2 / § 5](../../plans/v2.6-design.md) |
| Supersedes | — |
| Related | [ADR-0040 Identity BC carve-out](0040-identity-bc-carve-out.md) / [ADR-0042 Member AR](0042-member-ar.md) / [ADR-0043 Auth](0043-auth-passcode-jwt.md) |

## Context

[ADR-0033](0033-identity-model-refactor.md) v2 GA 时明示「V2 不做多租户 也不做组织这个概念」（用户表态）。v2.6 重新评估这个决策：

### v2.6 改变态度的原因

1. **schema 一次到位胜过分次迁移**：v2.7+ 演进到 SaaS 时引入 org_id 是 inevitable；现在做一次大改 vs 未来做迁移 + 数据修复的 工作量对比悬殊
2. **多 Organization 测试需求**：v2.6 引入 Member / role / RBAC 后，单 Org 测不出 multi-tenant 边界 bug；@oopslink 明确 v2.6 必出 org switch + create org（避免埋 bug）
3. **URL 多 tab 多 org 并发使用**：用户希望在同一浏览器同时打开多个 Org 工作（[v2.6-design 决策 #4 + § 7](../../plans/v2.6-design.md)），URL 携带 org slug 是天然路径
4. **未来 SaaS 演进 / 多团队场景**：远期目标，schema 提前 align

## Decision

### 1. Organization 作为 BC9 Identity 的显式 AR

```
Organization AR (BC9 Identity)
├── id            string PK   // "org-<8hex>"
├── slug          string      // UNIQUE; [a-z0-9-]{3,40}; URL key
├── name          string      // 显示名；1-80
├── description   string?
├── created_by_identity_id  // FK Identity.id
├── created_at
├── updated_at
└── deleted_at?               // 软删除
```

详 AR 形态见 [tactical/identity/02-organization.md](../architecture/tactical/identity/02-organization.md)。

### 2. Schema 一次推到底（A2 决策）

所有顶级业务实体一次性加 `organization_id` 列：

| 表 | BC | v2.6 改动 |
|---|---|---|
| `channels` | Conversation | + `organization_id` (NOT NULL) |
| `projects` | Workforce | + `organization_id` (NOT NULL) |
| `workers` | Workforce | + `organization_id` (NOT NULL) |
| `agent_instances` | Workforce | + `organization_id` (NOT NULL) + `identity_id` (NOT NULL，per [ADR-0045](0045-identity-id-format.md)) |
| `user_secrets` | SecretManagement | + `organization_id` (NOT NULL) |
| `members` | Identity | 新建（含 `organization_id`） |
| `invitations` | Identity | 新建（含 `organization_id`） |

间接归属（不直接带 `organization_id`，通过 parent 推导）：

| 表 | parent | 推导路径 |
|---|---|---|
| `messages` | channel | channels.organization_id |
| `tasks` | channel | channels.organization_id |
| `issues` | channel | channels.organization_id |
| `threads` | channel | channels.organization_id |

### 3. URL 路由模型 `/organizations/{slug}/...`

所有用户工作空间路由强制 org 上下文：

```
/                                 → 根（cookie 检查后跳 /signup / /signin / /organizations/{last-slug}/）
/signup                           → 注册（未登录）
/signin                           → 登录（未登录）
/organizations                    → org switcher / 列表
/organizations/{slug}/            → 该 org 工作空间根
/organizations/{slug}/channels    → channels
/organizations/{slug}/projects    → projects
/organizations/{slug}/workers     → workers (former /fleet)
/organizations/{slug}/secrets     → secrets
/organizations/{slug}/members     → members (Humans / Agents tabs)
/organizations/{slug}/settings    → organization 元信息 + delete
/me                               → 当前 Identity profile
```

**多 tab 多 org 并发**：因为 URL 自带 slug，同一浏览器多 tab 可同时打开不同 Org，无 cookie state 冲突。

**Slug 唯一性**：未软删的 Org 之间 slug 唯一；软删后 slug 释放（可被新 Org 复用）。

### 4. Create Organization 权限

任何已登录 Identity（user）都能 `POST /api/organizations` 创建新 Org；创建者自动成为该 Org 的 owner Member。

不需要"超级管理员"角色（v2.6 不引入跨 Org 的 super-admin 概念）。

### 5. Delete Organization 软删 + 级联

- UI 在 `/organizations/{slug}/settings` 提供 "Delete Organization" 按钮（owner only，二次确认）
- 软删 = 设 `organizations.deleted_at = now()`
- 级联软删该 Org 下所有 Member（同事务，由 `OrganizationLifecycleService` 守门，DS-3）
- 跨 BC 级联（Channel / Project / Worker / AgentInstance / Secret）走 Domain Event `organization.deleted` 异步处理（最终一致）
- 详 [v2.6-design § 4.8.2 DS-3 + § 4.8.3 AS-3](../../plans/v2.6-design.md)

### 6. Slug 规则

- 用户在 create form 显式输入（不 auto-derive from name —— 中文 name 派生不出干净 slug）
- 正则：`^[a-z0-9]([a-z0-9-]{1,38}[a-z0-9])?$`
- 长度 3-40
- 在 active organizations 中唯一（软删后可复用）

### 7. Bootstrap 流程取代

v2.5 启动时 auto-create system Identity + owner Identity + Default Org 的 bootstrap 流程被 signup-page 替代：

- center 启动 DB 空 → 任意 URL 跳 `/signup` → 表单（display_name + passcode + first_organization_name + first_organization_slug）→ 同事务 create Identity + Organization + Member(owner)
- 详 [ADR-0043 Auth § Signup](0043-auth-passcode-jwt.md)

## Consequences

### 正面

- schema 一次到位：v2.7+ 演进时不再做大规模 migration
- URL 多 tab 多 org 并发 = 测试 / 实际使用都顺畅
- Org create / delete 自助化，不依赖 admin
- 为 v2.7 邀请流程 / v3 SaaS 多团队场景预备好基础

### 负面 / 待跟进

- **现有 schema 大量 ALTER**：v2.6 不向后兼容，drop-and-recreate（详 [v2.6-design § 9](../../plans/v2.6-design.md)）
- **所有顶级 BC AppService 写入路径加 org 校验**：每个 BC AppService 调 `IdentityBCFacade.GetActiveOrganization(orgID)`（AS-1/AS-3 invariants）
- **events 表跨 org 审计 JOIN 推导**：v2.6 events.organization_id 暂不 denormalized 列（性能可接受），v2.7 优化
- **Org 硬删（purge）v2.6 不出 UI**：留 CLI / DBA 兜底
- **多 Organization 复杂度引入 UI**：Top Bar Organization switcher + create / delete UI（详 [v2.6-design § 7](../../plans/v2.6-design.md)）

## Alternatives Considered

### A. v2.6 不引入 Organization，留 v2.7

- ✅ v2.6 周期更小
- ❌ Member / role 失去多 Org 测试维度，单 Org 测不出多 Org bug（@oopslink 决策依据）
- ❌ v2.7 还得做 schema 大改 + 数据迁移
- 否决

### B. Organization 隐式 singleton（无 AR，配置文件存）

- ✅ 简化模型
- ❌ 多 Org 测不到（同 A 问题）
- ❌ UI 没法 list / create / delete
- 否决

### C. URL 不带 slug，用 cookie / Header 存 active org

- ✅ URL 简洁
- ❌ 多 tab 多 org 并发场景失败（cookie 全局共享）
- ❌ Deep-link 分享时丢失 org 上下文
- 否决（@oopslink 在 Q-C 拍板 URL slug 路由）

## References

### v2.6 ADRs

- [ADR-0040 Identity BC carve-out](0040-identity-bc-carve-out.md)
- [ADR-0042 Member AR](0042-member-ar.md)
- [ADR-0043 Auth v2.6](0043-auth-passcode-jwt.md)
- [ADR-0045 Identity ID format](0045-identity-id-format.md)

### 影响 BC

- [tactical/identity/02-organization.md](../architecture/tactical/identity/02-organization.md)

### 来源

- 2026-05-27 #agent-center DM（@oopslink Q-A `A2 一次做完`）
- [v2.6-design.md](../../plans/v2.6-design.md) § 4.2.2 + § 5 + § 7
