# AI Runtime Stage 6 Preflight Report

Date: 2026-08-07

Branch inspected: `ac-exec/task-c937fd2f/exec-0b554596`

Verdict: **BLOCKED - do not start Stage 6 cleanup.**

## Gate Check

Stage 6 requires all of the following before deleting legacy fields, APIs,
frontend constants, or compatibility adapters:

- `runtime_legacy_fallback_total{object_type}` is zero for one full release window.
- The migration report has no pending items.
- The new path has deployment-level acceptance for retry, resume, reassign, cancel,
  historical execution reads, flag/fallback boundaries, and no plaintext Secret leaks.
- The rollback window is closed and owner-confirmed.

This isolated workspace does not contain production metric evidence, a completed
migration report, or deployment-level acceptance artifacts satisfying those
conditions. The implementation plan still has S0-S6 acceptance boxes unchecked in
`docs/plans/2026-07-22-ai-runtime-configuration-implementation.md`.

## Evidence That Cleanup Is Not Yet Safe

- `internal/airuntime/legacy.go` still defines `LegacyAdapter`,
  `LegacyRuntime`, `MigrationIssue`, and `LegacyFallbackCounter`.
- `internal/airuntime/resolver_test.go` still asserts legacy exact mapping,
  fallback counting, and unmapped legacy issue behavior.
- `internal/webconsole/api/server.go` still routes the legacy Model Catalog API:
  `GET/POST/PUT/DELETE /api/orgs/{slug}/model-catalog` and
  `POST /api/orgs/{slug}/model-catalog/import`.
- `internal/webconsole/api/handlers_model_catalog.go` still adapts Model Catalog
  imports through AI Runtime while preserving the legacy projection.
- `internal/admin/api/agent_tools_model_catalog.go` still exposes agent-tools
  Model Catalog CRUD/import.
- `web/src/App.tsx` and `web/src/shell/nav/WorkspaceSecondaryNav.tsx` still expose
  the `/model-catalog` frontend route.
- `web/src/api/modelCatalog.ts` and `web/src/pages/OrgModelCatalog.tsx` still call
  `/model-catalog` APIs.
- `web/src/api/types.ts`, `internal/webconsole/api/handlers_agent.go`, and
  `internal/webconsole/api/handlers_members.go` still retain the legacy
  `allowed_models` mirror/input boundary around `allowed_executors`.
- `internal/agentruntime/modelrouter` is still active and covered by tests.

These are not static grep findings alone: the routed/tested surfaces below passed
their current regression suites, which means the repository still treats them as
supported behavior.

## Verification Run

Command:

```sh
go test ./internal/airuntime ./internal/webconsole/api ./internal/admin/api ./internal/agentruntime/modelrouter
```

Result:

```text
ok  	github.com/oopslink/agent-center/internal/airuntime
ok  	github.com/oopslink/agent-center/internal/webconsole/api
ok  	github.com/oopslink/agent-center/internal/admin/api
ok  	github.com/oopslink/agent-center/internal/agentruntime/modelrouter
```

Frontend tests were not run because `web/node_modules` is absent in this isolated
worktree and this preflight made no frontend code changes.

## Required Next Step

Do not delete legacy runtime paths from this branch. First produce and commit the
missing release-window evidence:

1. Production or production-equivalent metric capture proving
   `runtime_legacy_fallback_total{object_type}` stayed at zero for one release
   window.
2. A migration report with every unknown, fallback, and manual item resolved.
3. Deployment-level acceptance covering retry/resume/reassign/cancel, historical
   execution readability, flag/fallback rollback boundaries, and Secret no-plaintext
   checks.
4. Owner confirmation that the rollback window is closed.

Only after those artifacts exist should Stage 6 remove old fields, old APIs,
frontend constants, compatibility adapters, and replaced modelrouter paths.
