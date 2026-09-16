# I166 Independent Reverification — 724c9b8

Verdict: BLOCKED, not PASS.

Authoritative candidate reverified: `724c9b8660c1c269bf6e05913f2815d97828d40b`

Preserved lineage/evidence anchors:

- Rejected ancestor: `c24623332d4c4ab0edafce5d12021db532672bda`
- Prior reject evidence: `98df37c22cbf391155af1940123d458207c5b61f`
- `git merge-base --is-ancestor c24623332d4c4ab0edafce5d12021db532672bda HEAD` returned `0`.
- Ancestry path from rejected SHA: `c2462333 -> 88f7eacef -> 724c9b866`.

## Scope Readback

The local `task-input/v1` package exists but describes unrelated task `T1850` and contains no attachments. This reverify therefore used the user-specified I166 contract and the exact candidate SHA above.

## Passed Evidence

- Production build: `make build` passed from `724c9b8660c1c269bf6e05913f2815d97828d40b`.
  - Vite emitted existing warnings for CSS syntax and chunk size; build completed and produced `bin/agent-center`.
- Focused/full frontend: `cd web && pnpm test -- src/pages/planGraphLayout.test.ts src/pages/PlanDetail.test.tsx` completed as a full web suite: `200` files passed, `1936` tests passed.
- Full Go/regression: `make test` passed all packages, including `tests/e2e`.
- Real production-built isolated browser chain: `node docs/acceptance/i166-plan-dag-real-chain.mjs` passed against the freshly built binary.
  - Provenance JSON: `docs/acceptance/i166-evidence/real-chain-results.json`
  - Candidate SHA in provenance: `724c9b8660c1c269bf6e05913f2815d97828d40b`
  - Isolated web API read: `7` nodes / `7` edges / `has_graph: true`
  - React Flow DOM read: `7` visible `path[data-testid="plan-graph-edge"]`
  - No node overlap recorded.
  - Fit view, mobile stepper, legacy single-node fallback, dependency edit controls, and empty plan state all passed.
  - Browser console errors: none.
- Refreshed screenshots:
  - `docs/acceptance/i166-evidence/graph-backed-desktop-light.png`
  - `docs/acceptance/i166-evidence/graph-backed-desktop-dark.png`
  - `docs/acceptance/i166-evidence/graph-backed-mobile.png`
  - `docs/acceptance/i166-evidence/legacy-single-node.png`
  - `docs/acceptance/i166-evidence/empty-plan.png`

## Source Scope

- React Flow/ELK dependencies are pinned in `web/package.json`: `@xyflow/react@12.8.4`, `elkjs@0.10.0`.
- `web/src/pages/planDagFlow.ts` owns the React Flow + ELK adapter and maps graph/stage/legacy data.
- `web/src/pages/PlanDetail.tsx` renders the desktop DAG with `ReactFlow`, `Controls`, `MiniMap`, custom nodes, and custom edges.
- Old self-layout helpers `DagCanvas`, `layoutStagedGraph`, and the old inline generic layout implementation are absent from `web/src`.
- The remaining hidden `plan-dag-svg`/`plan-dag-semantic-edges` compatibility markers are not the old canvas/layout renderer; visible edges are supplied by React Flow.

## Blocked Evidence

Required true stage/cross-stage data could only be created/read through an allowed formal MCP target bound to the isolated candidate instance. This executor has no such formal MCP connection. Per contract, I did not use source, mock data, DB/admin socket/admin HTTP/process args/raw center HTTP, or current live center MCP as a substitute.

Result: staged/cross-stage real MCP evidence is BLOCKED. Because that is a required acceptance item, the candidate cannot receive an all-item PASS from this executor.

## Decision

Do not integrate as PASS from this evidence alone. The non-stage production graph remediation evidence is green, including the critical 7 API edges => 7 visible React Flow edge paths. Final PASS requires a separate allowed isolated-candidate MCP connection and successful real staged/cross-stage verification.
