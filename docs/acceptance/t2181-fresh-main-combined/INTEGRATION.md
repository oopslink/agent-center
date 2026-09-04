# T2181 Fresh-Main Combined Integration

Fresh base:

- `origin-push/main`: `ddfb87d47e8253fb2e4f22d014f497301fa4b7ec`
- T2176 product SHA: `1e14098b213d97087c3aa2fbda9a90f065c74a3e`
- T2179 product SHA: `94945e8fb91b7e217e71ad393127a6232aa3f37e`
- Both product merge-bases with fresh main: `ddfb87d47e8253fb2e4f22d014f497301fa4b7ec`

Combination gap reproduced before transplant:

- `git merge-base --is-ancestor T2176 T2179`: exit `1`
- `git merge-base --is-ancestor T2179 T2176`: exit `1`
- fresh main differed from T2176 graph UI path: exit `1`
- fresh main differed from T2179 project-scope backend path: exit `1`
- Raw log: `raw/pre-combination-gap.log`

Coverage matrix:

| Capability | Source | Integrated paths |
|---|---|---|
| Formal Insight navigation and direct Collaboration route | T2176 / T2179 equivalent web delta | `web/src/AppLayout.tsx`, `web/src/App.test.tsx`, `web/src/AppLayout.insightnav.test.tsx`, `web/src/AppLayout.mobilenav.test.tsx`, `web/src/shell/nav/InsightSecondaryNav.test.tsx` |
| Organization graph default, clear-filter restore, four relation classes | T2176 / T2179 equivalent web delta | `web/src/pages/InsightCollaboration.tsx`, `web/src/pages/InsightCollaboration.test.tsx`, `web/src/api/insights.ts`, locale files |
| Wheel, zoom, pan, drag, Fit, Reset, focus interactions | T2176 / T2179 equivalent web delta | `web/src/pages/InsightCollaboration.tsx`, `web/src/pages/InsightCollaboration.test.tsx` |
| LOD, cluster, truncation, readability, Evidence drill-down | T2176 / T2179 equivalent web delta | `web/src/pages/InsightCollaboration.tsx`, `web/src/pages/InsightCollaboration.test.tsx`, `web/src/api/insights.ts`, locale files |
| `project_id` scoped 200, cross-org/no-auth fail-closed, malformed parameter non-503 | T2179 backend/API delta | `internal/observability/collaborationeffect/graph_sqlite.go`, `internal/observability/collaborationeffect/query_test.go`, `internal/webconsole/api/handlers_collaboration_insight_test.go` |
| Combined production-like evidence | New T2181 evidence | `docs/acceptance/t2181-fresh-main-combined/**` |

Validation summary:

| Command | Exit | Evidence |
|---|---:|---|
| `go test ./internal/observability/collaborationeffect ./internal/webconsole/api -run 'Test.*Collaboration|Test.*Project' -count=1` | 0 | `raw/go-focused-tests.log` |
| `go test ./internal/observability/... ./internal/webconsole/api ./internal/projectmanager/... -count=1` | 0 | `raw/go-related-tests.log` |
| `pnpm exec vitest run src/pages/InsightCollaboration.test.tsx src/App.test.tsx src/AppLayout.insightnav.test.tsx src/AppLayout.mobilenav.test.tsx src/shell/nav/InsightSecondaryNav.test.tsx` | 0 | `raw/web-focused-vitest.log` |
| `pnpm run typecheck` | 0 | `raw/web-typecheck.log` |
| `pnpm run build` | 0 | `raw/web-build.log` |
| `make build` | 0 | `raw/make-build.log` |
| `node docs/acceptance/t2181-fresh-main-combined/run-collaboration-graph-acceptance.mjs` | 0 | `raw/isolated-smoke.log`, `evidence/verdict.json` |
| fresh-main `pnpm lint` | 1 | `raw/web-lint-main.log`; pre-existing `web/src/components/WorkItemFilterBar.tsx:352` |
| candidate `pnpm lint` | 1 | `raw/web-lint-candidate.log`; same pre-existing `web/src/components/WorkItemFilterBar.tsx:352`, no touched-file/new failure |

Production-like smoke result:

- Verdict: PASS
- API graph scale: `120` nodes, `256` edges
- Relations observed: Agent-Agent, Agent-Task, Agent-Plan, Plan-Task
- Valid scoped project API: HTTP `200`
- Bad project: HTTP `404`; bad cursor: HTTP `400`; unauthenticated: HTTP `401`; foreign project in owner org: HTTP `404`
- Screenshots, HAR, network, console, server log, API payloads, and recording are under `evidence/`.
