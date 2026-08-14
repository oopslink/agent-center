# T1343 Unified Authorization Migration Report

Date: 2026-08-14

## Inputs

- Frozen contract reviewed from upstream commit `29a7a738` (`docs: freeze unified permission contract`).
- Authorization Service implementation reviewed and integrated from upstream commit `3afd25f3` (`implement unified authorization service`).
- Repo rules reviewed from `CLAUDE.md`, `docs/rules/conventions.md`, and `docs/rules/testing.md`.
- `AGENTS.md` was scanned for and was not present in this worktree.

## Migration Matrix

| Legacy entry | Unified permission | Resource | Mode |
| --- | --- | --- | --- |
| `admin/api.RequireScope` | bearer scope mapping (`secret.resolve`, `blob.put`, `admin_token.manage`, etc.) | `secret/blob/admin_token/worker/task` | `CheckMigrated` |
| `admin/api.requireAgentOnWorker` | `agent.operate.self` | `agent` | `CheckMigrated` |
| `workerReportCapabilitiesHandler` worker owner guard | `worker.capability.report` | `worker` | `CheckMigrated` |
| `webconsole/api.requireOrgMember` | `org.read` | `org` | `CheckMigrated` |
| `pmRequireProjectInOrg` | `project.read` | `project` | `CheckMigrated` |
| `fileReachableForHuman` | `file.download` | `file` + live refs | `CheckMigrated` |
| `requireOrgAdmin` for workspace repos | `coderepo.workspace.manage` | `org` | `CheckMigrated` |
| `aiRuntimeDeps` | `ai_runtime.catalog.read` / `ai_runtime.catalog.manage` | `org` | `CheckMigrated` |
| `requireTeamMemoryManage` | `team.memory.review` | `team` | `CheckMigrated` |

`CheckMigrated` keeps the old verdict available for rollback/shadow, compares it with the unified verdict, and emits `authorization.shadow_mismatch` audit events when the decisions diverge.

## Feature Flags

- `AC_AUTHZ_MODE=enforce|shadow|legacy`; default is `enforce`.
- `AC_AUTHZ_SHADOW_COMPARE=0|1`; default is enabled.
- `AC_AUTHZ_CACHE=0|1`; default is enabled.

Rollback path: set `AC_AUTHZ_MODE=legacy` to make legacy verdicts authoritative without removing Authorization Service wiring. Cache can be disabled independently with `AC_AUTHZ_CACHE=0`.

## Revision And Cache Invalidation

Migration `0130_authorization_revision` adds a singleton revision row and triggers on legacy authority tables plus authorization role tables. The service cache keys effective permission derivation by subject, permission, transport, bearer scope, and resolved resource; cache is cleared when revision changes.

Trigger coverage:

- Legacy authority: `admin_tokens`, `members`, `organizations`, `pm_projects`, `pm_project_members`, `pm_tasks`, `pm_issues`, `pm_plans`, `teams`, `team_members`, `team_memory_policy_curators`, `conversations`, `file_references`, `agents`.
- Authorization data: `permission_definitions`, `authorization_roles`, `authorization_role_permissions`, `authorization_role_assignments`.

## Scan Evidence

Commands run:

- `rg -n "RequireScope\\(|requireAgentOnWorker\\(|requireOrgMember\\(|pmRequireProjectInOrg\\(|requireOrgAdmin\\(|aiRuntimeDeps\\(|requireTeamMemoryManage\\(|AuthFromContext\\(" internal --glob '*.go'`
- `rg -n "\\.Check\\(r\\.Context\\(\\)|\\.Check\\(ctx|\\.Explain\\(r\\.Context\\(\\)|\\.Explain\\(ctx|HasScope\\(|Role\\(\\)\\.AtLeast|CheckMigrated\\(" internal/admin/api internal/webconsole/api internal/authorization --glob '*.go'`
- `rg -n "want 129|!= 129|targetSchemaVersion = 129|version.*129" internal tests --glob '*test.go' --glob '*.go'`

Findings:

- All admin bearer-scope endpoints still converge on `RequireScope`, now backed by `CheckMigrated`.
- All MCP agent-tool endpoints still converge on `requireAgentOnWorker`, now backed by `CheckMigrated(agent.operate.self)`.
- Web org and project fan-out helpers now converge on `CheckMigrated(org.read/project.read)`.
- Direct `Check`/`Explain` in web handlers is retained only for the explicit permissions API (`handlers_permissions.go`) and inside Authorization Service internals/tests.
- Remaining `Role().AtLeast` checks are either the legacy verdict input for migrated gates or action-specific business invariants not removed in this slice.
- No stale schema-version `129` assertion remained after adding migration `0130`.

## Behavioral Fixes

- File uploader references now grant `file.attach`/`file.upload` only; they no longer grant `file.download` without a live reachable business scope.

## Tests

- `go test ./internal/authorization ./internal/persistence ./internal/admin/api ./internal/webconsole/api`
- `go test ./internal/cli ./internal/conversation/service ./internal/conversation/sqlite`
- `go test ./...`

All passed.
