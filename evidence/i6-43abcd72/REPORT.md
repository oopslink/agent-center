# T1940 I6 Product/Visual Acceptance Evidence

Verdict: **REJECT**

## Candidate And Provenance

- Immutable candidate ref: `refs/heads/immutable/i6-insight-acceptance-43abcd72`
- Candidate SHA reviewed: `43abcd7250528694a662724c99d0a80e042ce8bb`
- Remote readback: `git ls-remote origin refs/heads/immutable/i6-insight-acceptance-43abcd72` returned `43abcd7250528694a662724c99d0a80e042ce8bb`
- Ancestry: `origin/main` is an ancestor of the candidate; `t1939-i5-executions-list-detail` is an ancestor; local `t1937-projects-insight-ui` is not an ancestor, although similar v2 project files are present.
- Task input package caveat: `task-input/v1` was stale for T1850 and contained no I6 attachments. I used the remote immutable ref and inlined task text as the I6 source.

## Frozen Inputs Read

- `docs/design/features/insight-metric-semantics-and-information-architecture.md` at `2e04ab8610f2c07bef847b11183a27e2b5cd7512`
- Current `docs/design/features/insight-phase-1-contract.md`
- `docs/contracts/insight-metrics-v2.md` at `a427a2640621762506f5f98dc3179b0ab43b18e7`
- `docs/contracts/insight-v2.schema.json` at `a427a2640621762506f5f98dc3179b0ab43b18e7`

## Commands Run

- `go test ./internal/insight ./internal/webconsole/api -run 'Insight|Insights' -count=1` — PASS
- `cd web && pnpm exec vitest run src/utils/insightPresentation.test.ts src/pages/InsightOverview.test.tsx src/pages/InsightAgents.test.tsx src/pages/InsightProjects.test.tsx` — PASS, 62 tests
- `make build` — PASS; SPA build emitted a CSS minifier warning for invalid selector text, then built `bin/agent-center` and `bin/fakeagent`
- `go test ./...` — PASS
- Interrupted mistaken broad web test run: `pnpm test -- ...` forwarded incorrectly and began the whole suite; stopped manually, not used for verdict.

## Isolated Served Instance

- Instance ID: `i6viz43abcd72`
- Prefix: `/Users/oopslink/.agent-center-test/i6viz43abcd72`
- Web URL: `http://127.0.0.1:49156`
- Runtime readback: `raw/system-version.json`
- Served version: `HEAD-43abcd72`
- Served commit: `43abcd72`
- Install path: `/Users/oopslink/.agent-center-test/i6viz43abcd72/center/current/bin/agent-center`

Data provenance:

- `install test-instance --with-agent` failed before creating an agent/task because the default model `claude-opus-4-8` was absent from the runtime catalog.
- I reinstalled with `--with-seed`, then used normal served org-scoped HTTP API with the seeded owner session.
- Added runtime model `claude-opus-4-8` successfully, but agent creation still failed with `worker_not_in_org`.
- Created project task `task-52da605a` (`T1`) and plan `plan-011e5cc2` (`P1`) through the product API for Projects/delivery/evolution/lineage rendering.

## Evidence Files

Screenshots are under `screenshots/`; raw API/text snapshots are under `raw/`; `SHA256SUMS` covers the package.

Key screenshots:

- `screenshots/desktop-overview-empty.png`
- `screenshots/desktop-agents-empty.png`
- `screenshots/desktop-projects.png`
- `screenshots/desktop-project-detail-delivery-evolution.png`
- `screenshots/desktop-lineage-null.png`
- `screenshots/desktop-executions-filter-empty.png`
- `screenshots/desktop-executions-filter-cursor.png`
- `screenshots/desktop-execution-detail-404.png`
- `screenshots/desktop-auth-redirect.png`
- `screenshots/mobile-overview-empty.png`
- `screenshots/mobile-project-detail.png`
- `screenshots/mobile-executions-filter-empty.png`

## Blocking Findings

1. Full-fidelity execution-chain validation could not be completed. `install test-instance --with-agent` failed with `runtime_model_not_found` for `claude-opus-4-8`; after adding that model, normal agent creation still failed with `worker_not_in_org`. This blocks real terminal execution, queued command, invalid timestamp, and low coverage heartbeat validation through the served instance.

2. API empty collection contract is violated. `raw/api-overview-v1.json` returns `"agents":null,"projects":null` despite the Phase 1 contract freezing empty arrays. `raw/api-overview-v2.json` also returns `"agents":null`. The UI normalizes some nulls, but the HTTP contract is still wrong.

3. Project Insight main view exposes backend/internal tokens in primary UI. `desktop-project-detail-delivery-evolution.png` and `mobile-project-detail.png` show `coverage_unknown`, `unknown_source_state`, and raw JSON drilldown filters. This fails the product/visual requirement to avoid raw enums/internal classifications in the main view.

4. Invalid cursor handling is not acceptable in the real page. Opening `/insights/executions?window=24h&project_id=project-ffb1c198&cursor=page-test` rendered `Execution request failed [503 [object Object]] Service Unavailable` (`raw/current-executions-body.txt`, `screenshots/desktop-executions-filter-cursor.png`) instead of a clear state.

## Acceptance Matrix

### B. Coverage, Unknown, Zero — **REJECT**

- Component tests cover null/zero/49.9/50/89.9/90 and `0.001 + utilization 0`.
- Real served Overview with no capacity baseline displays `Cannot determine`, not `0%`.
- Blocker: unable to create real low-coverage heartbeat/execution-chain data in the isolated instance because agent creation failed. This prevents the required full-fidelity `coverage=.001` page proof.

### C. Enums And Explanation — **REJECT**

- Component tests cover execution status mapping, recovered badge, invalid time order, and raw outcome/quality absence.
- Detail 404 page renders user copy.
- Blocker: Projects main view renders raw reason codes and raw JSON drilldown filters; no real execution rows/detail could be produced from the served instance.

### D. Time, Statistics, Ranking — **REJECT**

- Component tests cover duration formatting and percentile/sample copy.
- Real Overview displays rolling 24h, local window, timezone, refreshed time, empty denominator copy, and no inline execution table.
- Blocker: no real execution samples could be produced, so ranking rows, samples, percentiles, and duration copy could not be validated on served terminal data.

### E. Data State And Real Chain — **REJECT**

- Focused Go/API tests, exact web tests, full build, and full `go test ./...` pass.
- Real instance screenshots cover empty, filtered empty, auth redirect, detail 404, Projects delivery/evolution/lineage pages.
- Blockers: isolated `--with-agent` bootstrap fails; API returns null collections where frozen contract requires arrays; real terminal/queued/invalid/low-coverage chain could not be exercised.

## Owner Questions

On the real empty Overview, an owner can answer: 0 completed executions, no completed denominator for failure rate, no valid queue/duration samples, and capacity cannot be determined because there is no computable capacity baseline.

On Project Insight, the owner-facing answer is degraded by raw backend codes and JSON filters, so the page does not meet the visual/product acceptance bar.
