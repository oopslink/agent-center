# I166 Independent Verification — Plan DAG React Flow + ELK

Date: 2026-09-16

Verdict: **REJECT**

Candidate: `origin/task-dbcaf649-final@c24623332d4c4ab0edafce5d12021db532672bda`
Frozen base: `ee9eab20c82083ae526f89ecf3907846a03c5dd7`

## Scope Readback

The issue requires independent validation that Plan DAG rendering has migrated to mature open-source graph/layout libraries, covers both graph-backed and legacy `depends_on` DAG paths, preserves business semantics and permissions, and passes production-like browser evidence with ordinary/staged plans, cross-stage dependencies, branch/join, long titles, empty/single node, dynamic refresh, topology updates, mobile, themes, async layout races, and 100/500-node metrics.

I did not use agent-center DB/admin socket/admin HTTP/process-argument bypasses or any center state channel. The local `task-input/v1` package was read, but it is for an unrelated T1850 replay task and has no I166 attachments; this report uses the inlined I166 contract and specified SHA.

## Static Reconciliation

PASS:

- `web/package.json` pins `@xyflow/react@12.8.4` and `elkjs@0.10.0`; lockfile contains those versions.
- `web/src/main.tsx` imports `@xyflow/react/dist/style.css`.
- `web/src/pages/planDagFlow.ts` uses `elkjs/lib/elk.bundled.js` and React Flow node/edge types.
- `PlanGraphDag` calls `layoutGraphFlow(...)`; `LegacyPlanDag` calls `layoutLegacyFlow(...)`. Both desktop render paths pass through `PlanFlowCanvas` and `ReactFlow`.
- `useElkFlowLayout` uses a monotonic request id to prevent stale async layout results from overwriting the latest topology.
- Status/title/stage data refresh is separated from layout coordinates via `refreshGraphFlowNodes` / `refreshLegacyFlowNodes`, so status-only updates do not change the topology key.

REJECT/RISK:

- Old layout/canvas code is still present in `web/src/pages/PlanDetail.tsx`: `DagCanvas`, `layoutDag`, `layoutGraph`, `layoutStagedGraph`, and `layoutLegacyStagedDag`. `rg` shows no non-test call into these old functions except within the retained old layout helpers themselves, but the contract said the old common canvas/layout had retired. This is residual dead/exported implementation, not a production path blocker by itself, but it contradicts the requested cleanup.
- Real browser evidence found a stronger blocker: graph-backed React Flow renders nodes but no visible dependency edges on the production-built page.

## Checks Run

Focused:

- `cd web && pnpm exec vitest run src/pages/planGraphLayout.test.ts src/pages/PlanDetail.test.tsx`
- Result: PASS, 2 files / 130 tests.

Full web:

- `cd web && pnpm test`
- Result: PASS, 200 files / 1946 tests.

Full Go:

- `go test ./...`
- Result: PASS.

Production build:

- `cd web && pnpm run build`
- Result: PASS. Vite emitted existing warnings for one CSS minification syntax issue and large chunks; build completed.

Production-like binary:

- `make build`
- Result: PASS. Built SPA with `VITE_BUILD_SHA=c24623332d4c4ab0edafce5d12021db532672bda` and produced `bin/agent-center`.

Browser acceptance runner:

- `node docs/acceptance/i166-plan-dag-real-chain.mjs`
- Result: script completed and wrote `docs/acceptance/i166-evidence/real-chain-results.json`.
- No route mocks were used. Data was created through the real HTTP API after real signup/signin against a fresh local binary.

## Production-Like Instance Provenance

From `docs/acceptance/i166-evidence/real-chain-results.json`:

- Started: `2026-09-16T03:36:50.454Z`; finished: `2026-09-16T03:36:56.742Z`.
- Binary: `bin/agent-center`.
- Base URL: `http://127.0.0.1:56326`.
- Web port: `56326`; server/listen port: `56327`.
- Config: `/var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/i166-dag-qELpo8/config.yaml`.
- DB: `/var/folders/td/p6yqhr2j6qlc_tsq9z7fxpn40000gn/T/i166-dag-qELpo8/agent-center-i166.db`.
- Runtime identity: `AGENT_CENTER_INVOCATION_ID=i166-dag-acceptance`.
- Session namespace: isolated Playwright browser context, `ac_session` on `127.0.0.1`.
- Test data: real org/project/tasks/plans created through `/api/auth/*` and `/api/orgs/{slug}/...`.

Screenshots:

- `docs/acceptance/i166-evidence/graph-backed-desktop-light.png`
- `docs/acceptance/i166-evidence/graph-backed-desktop-dark.png`
- `docs/acceptance/i166-evidence/graph-backed-mobile.png`
- `docs/acceptance/i166-evidence/legacy-single-node.png`
- `docs/acceptance/i166-evidence/empty-plan.png`

## Browser Evidence Findings

PASS:

- Graph-backed started plan returned `has_graph: true`, 7 graph nodes and 7 graph edges from the real API.
- Graph-backed desktop rendered 7 React Flow nodes without measured node overlap.
- Fit-view control was present.
- Dark-theme screenshot rendered.
- Mobile graph stepper rendered.
- Pending legacy single-node plan rendered through React Flow and exposed dependency edit controls.
- Empty plan rendered the expected empty state.
- Console error list was empty.

REJECT:

- `graph-backed-desktop-light.png` shows nodes but no visible dependency lines.
- DOM edge collection also found `edgeCount: 0` for rendered React Flow edge paths, while the API graph had 7 edges.
- This fails the frozen requirements for dependency semantic completeness, branch/join readability, edge rendering, and graph interaction correctness. A user cannot visually inspect dependencies even though the backend graph contains them.

BLOCKED/NOT FULLY VERIFIABLE IN THIS EXECUTOR:

- Real staged plan creation was not possible through the Web API. The repository exposes stage creation via agent-facing MCP/admin tool names `create_stage` / `get_stage`, and the task explicitly says to report missing tools rather than bypass. This executor has no allowed center/MCP channel and did not use DB/admin socket/admin HTTP fallback. Therefore real staged graph and cross-stage dependency browser evidence remains unverified.

## 100/500 Node Metrics

Measured via local Node + `elkjs/lib/elk.bundled.js` with the same layered/orthogonal layout options used by the adapter.

Environment: Node `v25.6.0`, `darwin/arm64`.

- 100 nodes, 123 edges: `67.8 ms` layout, output `353 x 16178`.
- 500 nodes, 623 edges: `83.8 ms` layout, output `353 x 80978`.

These metrics cover raw ELK layout only, not React Flow browser interactivity at that size.

## Frozen Acceptance Matrix

- Ordinary graph: PASS for node rendering; REJECT for missing visible edges.
- Graph-backed route: PASS API and nodes; REJECT visible dependency edges.
- Legacy route: PASS single-node and pending edit controls in real browser.
- Branch/join: REJECT because API had branch/join edges but browser did not render visible edges.
- Long title: PASS visually in node card without overlap in screenshot.
- Empty graph: PASS.
- Single node: PASS.
- Dynamic status/topology refresh: PARTIAL static/code PASS; no live topology mutation browser proof beyond pending edit controls.
- Node overlap: PASS for sampled real graph nodes.
- Stage containment/cross-stage dependencies: BLOCKED by missing allowed MCP/admin tool access for real staged data creation.
- Pan/zoom/fit: PASS fit control and React Flow controls present; full pan/zoom not exhaustively measured.
- Task open: PARTIAL via existing tests; not independently clicked in real browser runner.
- Topology edit/permission: PASS pending legacy edit controls visible; backend permission semantics covered by existing full tests, not independently negative-tested in browser.
- Mobile: PASS graph stepper screenshot.
- Light/dark theme: PASS screenshots.
- Async old-result race: PASS by code inspection and focused tests; not independently forced in browser.
- Status refresh should not reset view: PASS by code inspection; not independently forced in browser.

## Conclusion

Do not complete or integrate this candidate. The candidate has useful migration pieces and passes the automated suites, but the production-built real page fails the core DAG requirement because graph-backed dependencies are not visibly rendered. In addition, staged-plan evidence cannot be completed in this isolated executor without the formal stage MCP/admin tool path, and old layout helpers remain in the source.
