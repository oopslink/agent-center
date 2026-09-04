# T2181 Independent Acceptance

Verdict: **PASS**

Candidate: `ac-exec/task-340201c6/exec-7a7d087b@6074ea621f178889bcfca5adca8cf27c3ad174da`

## Summary

I independently validated the fixed combined candidate on top of `origin/main` merge-base `ddfb87d47e8253fb2e4f22d014f497301fa4b7ec`. The commit graph is fresh-main: the candidate has exactly that merge-base as its parent, and the remote candidate ref resolves back to the same SHA.

The product hard gates pass in a fresh isolated production-like instance. The instance was started from the built `bin/agent-center` binary, seeded only through authenticated production HTTP endpoints, and exercised through the embedded web console with Playwright. No dev server, MSW, or mock service was used.

`make lint` still fails, but `origin/main` fails the same `web/src/components/WorkItemFilterBar.tsx:352` UX lint rule. I therefore classify it as a baseline failure, not a candidate-introduced regression.

## Instance Provenance

- Build identity: `agent-center ac-exec/task-86616b05/exec-443d50bf-6074ea62 (commit 6074ea62)`, from `raw/agent-center-version-command.log`.
- Install root, DB path, ports, runtime identity, cookie namespace, organization, worker, project, and plan IDs are recorded in `evidence/verdict.json`.
- Data provenance: fresh isolated instance, seeded through authenticated `/api` endpoints.
- Graph scale: `120` nodes and `256` edges, from `raw/api-graph-summary.json`.

## Coverage Matrix

| Requirement | Result | Evidence |
|---|---:|---|
| T2176 formal navigation and direct URL refresh | PASS | `evidence/01-authenticated-projects.png`, `evidence/03-direct-refresh.png`, `raw/source-feature-grep.log` |
| T2176 real wheel zoom, pan, drag, Fit/Reset, focus/restore | PASS | `evidence/interaction-after-reset.png`, `evidence/page@dbfb32f2ba608a1102ec052b707cd0c3.webm`, `evidence/verdict.json` |
| T2176 LOD/cluster/truncated feedback and readability | PASS | `evidence/api-lod-cluster.json`, `evidence/02-organization-graph.png`, `raw/api-graph-summary.json` |
| T2179 project_id 503 fix and scoping/fail-closed behavior | PASS | `evidence/api-project-filter-probes.json`, `evidence/04-filtered-project.png`, `evidence/verdict.json` |

## Hard Gates

| # | Gate | Result | Evidence |
|---:|---|---:|---|
| 1 | Authenticated formal navigation and direct URL refresh, no import errors or unexpected 401 | PASS | `evidence/01-authenticated-projects.png`, `evidence/03-direct-refresh.png`, `evidence/network.json`, `evidence/console.json` |
| 2 | No-query non-empty organization graph | PASS | `evidence/02-organization-graph.png`, `evidence/api-org-graph.json` |
| 3 | Agent-Agent, Agent-Task, Agent-Plan, Plan-Task same screen | PASS | `evidence/verdict.json`, `raw/api-graph-summary.json` |
| 4 | Valid project_id API/UI 200, clear restores org graph, no 503, cross-org/unauth fail closed, invalid project/cursor stable errors | PASS | `evidence/api-project-filter-probes.json`, `evidence/04-filtered-project.png` |
| 5 | Buttons plus real wheel zoom, pan, drag, Fit/Reset, focus/restore | PASS | `evidence/verdict.json`, `evidence/interaction-after-reset.png`, video |
| 6 | Evidence drill-down | PASS | `evidence/api-evidence.json`, `evidence/06-evidence-drawer.png` |
| 7 | LOD/cluster/truncated prompt and actions | PASS | `evidence/api-lod-cluster.json`, `evidence/verdict.json` |
| 8 | 117+/250+ first screen readability and interaction performance | PASS | `raw/api-graph-summary.json`, `evidence/02-organization-graph.png`, `evidence/verdict.json` |
| 9 | Contrast, labels, and overlap readability | PASS | `evidence/02-organization-graph.png`, `evidence/verdict.json` |

## Quality Gates

| Command | Exit | Result | Evidence |
|---|---:|---:|---|
| `pnpm --dir web install --frozen-lockfile` | 0 | PASS | `raw/web-pnpm-install.log` |
| `go test ./internal/observability/collaborationeffect ./internal/webconsole/api` | 0 | PASS | `raw/go-focused-tests.log` |
| `pnpm --dir web test ...focused graph/nav tests...` | 0 | PASS | `raw/web-focused-vitest.log` |
| `pnpm --dir web run typecheck` | 0 | PASS | `raw/web-typecheck.log` |
| `pnpm --dir web lint` | 1 | BASELINE FAIL | `raw/web-lint.log` |
| `make build` | 0 | PASS | `raw/make-build.log` |
| `make lint` on candidate | 2 | BASELINE FAIL | `raw/make-lint-candidate.log` |
| `make lint` on fresh `origin/main` | 2 | BASELINE FAIL | `raw/make-lint-origin-main.log` |
| Isolated production-like UI/API acceptance | 0 | PASS | `raw/isolated-acceptance-run.log` |

## Notes

The local `task-input/v1` package was read, but it contains stale T1850 metadata rather than this T2181 contract. This mismatch is recorded in `verdict.json`; the fixed candidate and base from the task instruction were used.
