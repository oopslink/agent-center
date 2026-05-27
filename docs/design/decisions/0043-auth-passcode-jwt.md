# 0043. Auth v2.6（passcode + JWT cookie）

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-05-27 |
| Delivered | v2.6 design phase；详 [v2.6-design § 6](../../plans/v2.6-design.md) |
| Supersedes | — |
| Related | [ADR-0040 Identity BC carve-out](0040-identity-bc-carve-out.md) / [ADR-0026 SecretManagement BC](0026-user-secret-management-bc.md)（master_key 来源） |

## Context

v2.5 完全无认证：localhost 访问 = 默认 trust（依赖 OS / hostname 边界）。v2.6 引入 Organization + Member + RBAC 后，必须知道"当前请求的 actor 是哪个 Identity"才能 enforce 权限。

### 最终形态对照

v2.6 之前先盘最终形态（per [[feedback-end-state-first]]）：

| 维度 | 最终形态 | v2.6 |
|---|---|---|
| 注册入口 | 自助 / 邀请 / SSO / Agent | 自助（passcode） + Agent (cert-pin) |
| Signin | Email+Pass / Magic Link / SSO / 2FA | display_name + passcode |
| Session | JWT + refresh + remote logout | JWT cookie 7d |
| Credential | hash + reset + rotation + audit | argon2 hash |
| API Token | PAT + scope + revoke | — (worker-token only) |
| Verification | email / phone / KYC | — |
| Recovery | password reset + 2FA recovery | — (清库) |
| Permission | RBAC + ABAC + audit | RBAC 3 级 |
| Multi-Identity 浏览器 | account switcher | /signin 切换 |
| 审计 | login log / lockout / IP | Domain Event |

v2.6 选最简集合：passcode + JWT cookie + 基础 RBAC。其它项延后 v2.7+（详 [roadmap.md `v2.7+ Identity / Auth 进阶`](../roadmap.md)）。

## Decision

### 1. Credential：Passcode

- 6 位数字（`^\d{6}$`）
- argon2id hash（OWASP 推荐参数：iterations=3, memory=64MiB, parallelism=4）
- 仅 `kind=user` Identity 有 passcode；`kind=agent` Identity 无（agent 走 cert-pin + worker-token，由 [ADR-0023](0023-worker-enroll-lightweight.md) 体系承载）

### 2. Signup 流程

```
center 启动 → DB 中无任何 Identity → 任意 URL 跳 /signup
↓
表单：
  - your_display_name (1-40)
  - your_passcode (6 digits) + confirm_passcode
  - first_organization_name (1-80)
  - first_organization_slug (3-40, regex)
↓
SignupService.execute(form):
  TX:
    create Identity (kind=user, id=user-<8hex>, passcode_hash=argon2(passcode), account_status=active)
    create Organization (id=org-<8hex>, slug, name, created_by_identity_id)
    create Member (id=mem-<8hex>, organization_id, identity_id, role=owner, status=joined)
  emit identity.created + organization.created + member.added
↓
set httpOnly cookie session=jwt
↓
302 → /organizations/{slug}/
```

详 DS-1 守门见 [v2.6-design § 4.8.2 DS-1](../../plans/v2.6-design.md)。

### 3. Signin 流程

```
GET /signin →
  - 表单：display_name + passcode
↓
SigninService.execute(displayName, passcode):
  identity := IdentityRepository.GetByDisplayName(displayName)
  if identity == nil { return ErrPasscodeInvalid }   -- 不暴露 enumeration
  if identity.account_status == disabled { return ErrPasscodeInvalid }
  if !argon2_verify(passcode, identity.passcode_hash) {
      emit auth.signin_failed; return ErrPasscodeInvalid
  }
  jwt := mint(claims{sub=identity.id, exp=now+7d, jti=random})
  emit auth.signed_in
  return jwt
↓
set httpOnly cookie session=jwt
↓
302 → last visited organization slug or /organizations
```

### 4. Logout 流程

```
POST /signout →
  emit auth.signed_out
  clear cookie
  return 302 /
```

v2.6 仅本机 logout（清当前浏览器 cookie）；remote logout 延后 v2.7+。

### 5. JWT Cookie 形态

| 字段 | 值 |
|---|---|
| Name | `session` |
| HttpOnly | true |
| Secure | true (生产) / false (dev) |
| SameSite | `Lax` |
| Path | `/` |
| Exp | 7 days fixed；过期 = re-signin |
| 算法 | HS256 |
| **签名密钥** | **复用 `master_key`（Pi-1 closed）** |
| Claims | `{ sub: identity_id, exp, iat, jti }` |

### 6. JWT 签名密钥来源（Pi-1 closed）

[v2.6-design 决策 #28](../../plans/v2.6-design.md)：**JWT 签名密钥复用 `master_key`**（[ADR-0026](0026-user-secret-management-bc.md) SecretManagement BC 体系内已存在的 AES-256 master_key）。

理由：
- 最低工程成本：不引入独立的 `auth_signing_key` 生成 / 持久化 / rotation 流程
- master_key 已经是 plaintext-never-at-rest invariant 的根，复用扩展自然
- 单机 / 自用 / 内网场景下，密钥隔离边际收益低

权衡：
- master_key 泄露 = secret store + auth 双暴雷（耦合）
- v2.7+ 多用户 / SaaS / 远程暴露场景，再考虑独立 `auth_signing_key`（@oopslink 明确接受这个 trade-off）

### 7. Auth Middleware

```go
func (m *AuthMiddleware) Authenticate(ctx, req) (*Identity, error) {
    cookie, _ := req.Cookie("session")
    if cookie == nil { return nil, ErrUnauthenticated }
    claims, err := jwt.Verify(cookie.Value, m.masterKey)
    if err != nil { return nil, ErrUnauthenticated }
    identity, err := m.identityRepo.GetByID(ctx, claims.Subject)
    if err != nil { return nil, ErrUnauthenticated }
    if identity.AccountStatus == StatusDisabled {
        return nil, ErrUnauthenticated  // DS-4 fail-safe
    }
    return identity, nil
}
```

DS-4 fail-safe（[v2.6-design § 4.8.2 DS-4](../../plans/v2.6-design.md)）：
- DB-hit-per-request 检查 `account_status`
- Identity.account_status = disabled ⇒ 即时 401（不等 JWT 自然过期）
- v2.7+ 性能优化：in-memory cache + TTL；token_version 列

### 8. JWT 过期 SPA 处理（OQ-2 closed）

[v2.6-design 决策 #21](../../plans/v2.6-design.md)：JWT verify 失败 → SPA 直接全屏跳 `/signin` 重输 passcode。

理由：
- v2.6 不出 refresh token；过期事件低频
- 跳页 acceptable，无 inline modal 的复杂度

### 9. Per-organization Authorization

中间件层校验 `/organizations/{slug}/...` 路由：

- Slug 存在且未软删
- current_identity 在该 Org 有 `status=joined` 的 Member
- 否则 403（含"加入此 Org" 提示）

### 10. Action 级 Role 检查

每个 mutating endpoint 在 AppService 内显式校验 Member.role：

```go
func (s *ChannelAppService) Archive(ctx, channelID) error {
    member := s.identity.MemberForCtx(ctx)
    if !member.Role.AtLeast(MemberRoleAdmin) {
        return ErrForbidden
    }
    // ...
}
```

详 Permission Matrix 见 [v2.6-design § 8](../../plans/v2.6-design.md)。

## Consequences

### 正面

- 最简实现满足 v2.6 单机自用场景的 auth 需求
- 配合 RBAC 三级 + Member 模型，权限语义清晰
- 复用 master_key 降低 infrastructure 复杂度
- DS-4 fail-safe 保证 admin disable Identity 后即时生效

### 负面 / 待跟进

- **每请求 1 DB 查询**：DS-4 fail-safe 不缓存；单机 SQLite 内存索引可接受，远程 SaaS 时再加 cache
- **master_key 与 auth_signing_key 耦合**：密钥泄露影响范围更大；v2.7+ 解耦
- **passcode 6 位 = 10^6 = 100w 空间**：本地自用场景 brute-force 风险低；公网暴露时需扩到 8+ 位 + lockout（v2.7+）
- **无 email / 无 reset**：忘 passcode 只能清库；延后到 v2.7
- **无 2FA / 无 SSO**：详 roadmap.md `v2.7+ Identity / Auth 进阶`

## Alternatives Considered

### A. Cookie-only 无密码（标识 Identity 即可）

- ✅ 最简
- ❌ 无法多 Identity 测试 role 切换
- ❌ Cookie 偷走 = 完全暴露
- 否决（@oopslink Auth-β 选择）

### B. Email + Password 标准实现

- ✅ SaaS-ready
- ❌ 需要 email 投递基础设施（v2.6 单机自用没意义）
- ❌ password reset flow 是 nontrivial 工程量
- 延后 v2.7+

### C. OAuth / SSO

- ✅ 企业场景理想
- ❌ v2.6 自用 / 单机无意义；外部 IdP 配置成本
- 延后 v3+

### D. Magic Link（邮件无密码）

- ✅ UX 流畅
- ❌ 同 B，依赖 email 基础设施
- 延后 v2.7+

### E. 单独 auth_signing_key（不复用 master_key）

- ✅ 密钥隔离
- ❌ 多一套 key 管理流程
- ❌ v2.6 自用场景边际收益低（@oopslink Pi-1 决策）
- 否决

## References

### v2.6 ADRs

- [ADR-0040 Identity BC carve-out](0040-identity-bc-carve-out.md)
- [ADR-0041 Organization](0041-organization-multi-tenant.md)
- [ADR-0042 Member AR](0042-member-ar.md)
- [ADR-0045 Identity ID format](0045-identity-id-format.md)

### 前置 ADRs

- [ADR-0026 SecretManagement BC](0026-user-secret-management-bc.md) — master_key 体系
- [ADR-0023 Worker enroll lightweight](0023-worker-enroll-lightweight.md) — agent identity 路径 (worker-token + cert-pin)

### 来源

- 2026-05-27 #agent-center DM（@oopslink Auth-β 决策 + Pi-1 closure）
- [v2.6-design.md § 6](../../plans/v2.6-design.md)
- [roadmap.md `v2.7+ Identity / Auth 进阶`](../roadmap.md)
