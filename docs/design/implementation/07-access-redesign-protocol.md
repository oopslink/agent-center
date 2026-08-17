# Access Redesign Implementation Protocol

| 字段 | 值 |
|---|---|
| 状态 | Frozen implementation protocol |
| 日期 | 2026-08-17 |
| Source of truth | [Access Management Redesign API / Security / Migration Contract](../features/2026-08-17-access-management-redesign.md) |
| 前置契约 | [统一权限契约与现状冻结](../features/unified-permission-contract.md)、[ADR-0058](../decisions/0058-unified-permission-contract.md) |

## 1. 实现边界

本协议只描述实现必须遵守的落地顺序和验收门槛，不在本文档任务中实现业务代码。

实现必须保持三类事实分离：

- `access`：Authorizer 的 allow/deny 判定。
- `membership`：Org / Project / Team / Conversation / File 等 bounded context 的关系事实。
- `runtime capability`：Worker capability、Agent tag、Team runtime role、Task required capability；永不直接授予 access。

## 2. Authorizer Adapter Protocol

每个旧入口迁移时必须使用 adapter，而不是在 handler 中重新拼权限逻辑：

| 入口 | Adapter 输入 | 必须调用 | Legacy fallback |
|---|---|---|---|
| Web org | JWT user + org slug | `Check(org.read, ResourceScope{Kind:"org"})` | `MemberRepo` joined / disabled owner compatibility |
| Web project | user + org/project path | `Check(project.read, ResourceScope{Kind:"project", OrgID:...})` | project belongs-to-org + legacy member |
| Admin bearer | token owner + bearer scope | `PermissionForBearerScope` then `Check(mapped_permission, bearerResource)` | legacy `AuthContext.HasScope` only while P1 flag disabled |
| Agent tools | worker token + fixed agent id | `Check(agent.operate.self, ResourceScope{Kind:"agent"})` then resource-level checks | worker-agent binding only while P1 flag disabled |
| Permission API | org member session + request body | `Check` / `Explain` / `ListEffective` / batch APIs | none for writes; writes fail closed |

Adapter rules:

- Transport subject parsing belongs at the edge; resource authorization belongs in Authorizer / AppService.
- Handler must not call `ListEffective` as an authorization cache.
- Handler must not trust front-end role, MCP tool visibility, worker capability, or agent capability tags as grants.
- Cross-org resource resolution fails before writes and responds as opaque 404/deny.

## 3. Graph / Explain / Effective Protocol

Graph implementation:

- Web plan graph must resolve `org_slug -> org_id`, `project_id -> org_id`, `plan_id -> project_id` before projection.
- Raw orchestration graph handlers remain internal/admin unless the MCP canonical set explicitly includes them.
- If a raw graph tool becomes agent-facing, the implementation must add graph-to-plan authorization and parity tests in the same change.
- Graph mapper must only expose fields allowed by the source contract.

Explain / Effective implementation:

- Explain can include reasons and evidence refs only after redaction.
- Web explain for another subject requires owner/admin in the same org.
- Worker subject explain is internal diagnostics only.
- Effective projection is read-only and must include `source` and `delegatable`.

## 4. Pagination Completeness Protocol

Every list/read-model API touched by Access Redesign must choose exactly one completeness model:

- Offset: `total`, `page_size`, `offset`, `has_more`.
- Keyset: `has_more`, `next_*_cursor`.
- Partial diagnostic: explicit `complete:false` or `truncated:true`.

Multi-layer endpoints must propagate child truncation. If any child source is page-capped, post-filtered, timed out, or permission-filtered
without a stable continuation cursor, the parent response must not claim completeness.

`permissions/audit` cannot graduate to P2 until it uses a stable cursor or declares incomplete output.

## 5. Idempotency / TTL / CAS Protocol

Apply/Revoke implementation requirements:

- Require idempotency key for every mutating batch request.
- Store `actor`, `operation`, `request_hash`, status, response, `created_at`, and expiry state.
- Completed replay window is 24 hours.
- Pending reclaim window is 15 minutes.
- Same key + same hash replays; same key + different hash returns conflict.
- Mutations and audit writes happen in one transaction.
- Revoke uses CAS so one concurrent writer returns `revoked`; repeats return `unchanged` or idempotent replay.

Migration requirements:

- Add TTL support additively; do not rewrite or drop existing authorization tables for rollback.
- Do not copy `members`, `pm_project_members`, `team_members`, `participants`, or `file_references` into RBAC grants.
- Legacy membership remains authoritative during rollback.

## 6. Delegation Protocol

An Apply/Revoke path must verify all three layers:

- Manage authority: actor can manage org RBAC metadata.
- Delegatable permission: actor already holds every granted permission on the target resource with `Delegatable=true`.
- Boundary: actor, target subject, role, and resource resolve to the same org / parent scope.

Runtime capability, MCP tool visibility, worker owner status, or agent binding cannot satisfy delegation by themselves.

## 7. Rollout Gates

| Phase | Gate |
|---|---|
| P0 | docs frozen, route inventory complete, registry tests green, diagnostics read-only. |
| P1 | helper delegation enabled under flag, custom writes guarded by idempotency, audit written, rollback to legacy verified. |
| P2 | TTL persisted, audit completeness fixed, projection parity tests green, graph redaction tests green, delegation tests green. |

Rollback must be observable by re-running route / Authorizer tests and by reading persisted feature state. Logs or executor reports are not enough.

## 8. Required Acceptance Commands

Run these before presenting an Access Redesign candidate:

```sh
git diff --check
go test ./internal/authorization ./internal/webconsole/api ./internal/admin/api ./internal/mcphost
rg -n "type SubjectRef|func \\(s SubjectRef\\) Validate" internal/authorization/types.go
rg -n "permissions/(definitions|check|explain|effective|audit|batch/(preview|apply|revoke))" internal/webconsole/api/server.go
rg -n "Authorizer.Check" internal/webconsole/api/handlers.go internal/webconsole/api/handlers_pm.go internal/admin/api/auth.go internal/admin/api/agent_tools.go
rg -n "AgentFacingToolNames|pmGetPlanGraphHandler|pmPlanGraphMap" internal/mcphost internal/webconsole/api
rg -n "beginIdempotency|completeIdempotency|revokeAssignment" internal/authorization
```

For documentation-only changes, the Go test set still acts as contract regression evidence for the current authorization surfaces.
