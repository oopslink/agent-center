# T2178 Collaboration Graph project_id filter

Base: `origin/main@ddfb87d47e8253fb2e4f22d014f497301fa4b7ec`

T2176 retained candidate: `1e14098b213d97087c3aa2fbda9a90f065c74a3e` runtime Web Graph changes only; prior acceptance evidence files were not copied.

## Root Cause

`SQLiteGraphReader.readStructure` built a `WHERE` clause for `pm_projects` and then used `strings.Replace(..., 1)` to adapt it for joined queries aliased as `pr`. A legal web request with `project_id` first passed org/project authorization, then called `QueryService` with both `OrganizationID` and `ProjectID`. The plan/task/stage/dependency structure queries only replaced the first `pm_projects` occurrence, leaving `pm_projects.id=?` inside queries whose FROM clause only exposed alias `pr`, producing:

`SQL logic error: no such column: pm_projects.id (1)`

The web handler mapped that repository error to `503 insight_unavailable`.

Fix: generate project-scope predicates with an explicit table/alias (`projectScopeWhere(f, "pm_projects")` or `"pr"`), preserving project filtering instead of swallowing errors or returning an empty graph.

## Evidence

- Red reproduction: `raw/red-project-filter-503.log`
  - `go test ./internal/observability/collaborationeffect ./internal/webconsole/api -run 'TestQueryServiceOrgGraphProjectFilterScopesStructure|TestCollaborationEffectsHTTPProjectScopeAndFailures' -count=1`
  - exit `1`
  - captured response body: legal `project_id` HTTP request returned `503` with `SQL logic error: no such column: pm_projects.id (1)`.
- Green API/query: `raw/green-focused-go-api.log`
  - same isolated httptest API path with real migrations, auth cookie, org slug, PM project lookup, authorization resolver, SQLite collaboration graph reader.
  - exit `0`
  - legal `project_id` returned `200`, one in-scope effect, in-scope nodes only.
  - cleared project filter returned org-level graph (`200`, two readable project effects).
  - invalid cursor returned `400`; missing project returned `404`; unauthorized same-org project returned `403`; cross-org project returned `404`.
- T2176 Web Graph regression: `raw/web-focused-vitest.log`
  - `pnpm exec vitest run src/pages/InsightCollaboration.test.tsx src/AppLayout.insightnav.test.tsx src/AppLayout.mobilenav.test.tsx src/shell/nav/InsightSecondaryNav.test.tsx src/App.test.tsx`
  - exit `0`, 5 files / 50 tests passed.
  - covers formal nav/direct route, org-level graph request, project filters, graph interactions, Evidence drawer, LOD/cluster/truncated feedback, and large graph readability assertions.
- Typecheck: `raw/web-typecheck.log`, exit `0`.
- Focused eslint on touched Web files: `raw/web-focused-eslint.log`, exit `0`.
- Full Web lint baseline: `raw/web-lint.log`, exit `1` due to pre-existing unrelated `web/src/components/WorkItemFilterBar.tsx:352`.
- Web build: `raw/web-build.log`, exit `0`.
- Repository build: `raw/make-build.log`, exit `0`.

## Notes

The task-input package present in this workspace describes stale T1850 metadata and had no attachments; this run followed the T2178 task contract from the executor prompt.
