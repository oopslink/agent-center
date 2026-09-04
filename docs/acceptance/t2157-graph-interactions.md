# T2157 collaboration graph interaction evidence

## Dataset and scope

This delivery was exercised against the repository's production-backed, sanitized
organization dataset at `docs/design/fixtures/collaboration-effect-mvp-v1.json`.
It contains 24 timestamped organization events for project `P-COLLAB`, including
assignment, reassignment, completion, blocking, unblocking, dependency release,
and review outcomes. Every sample links back to the production event producer and
its production test. The fixture validator remains the authoritative schema and
field-coverage check; no graph/evidence/LOD semantics were replaced by demo data.

The page test also loads an organization-scoped graph (no `project_id`) and asserts
that Plan, Stage, Task, Agent, semantic edges, aggregated evidence counts, and lazy
evidence remain present. This is the same API response contract used by the page.

## Auditable interaction matrix

| Interaction | Automated evidence |
| --- | --- |
| Zoom buttons | `InsightCollaboration.test.tsx`: Zoom in mutates the SVG viewBox; Reset restores it |
| Wheel zoom | A wheel event centered on the canvas mutates the SVG viewBox |
| Canvas pan | Pointer down/move/up on the canvas translates the SVG viewBox |
| Node drag | Pointer down on a real rendered Agent node and move/up on the SVG changes its circle `cx` |
| Fit | Restores graph bounds after wheel zoom and after canvas pan |
| Reset | Clears dragged positions, focused node, selected edge/evidence context, and restores original graph bounds |
| Node focus | Keyboard Enter focuses the full accessible node label and changes the viewport to its neighborhood |
| Noise reduction | Selecting a semantic edge dims unrelated edges/nodes while preserving the selected neighborhood |
| Clear filters | One action removes project/task/relation/polarity query parameters; the next request contains only `limit` and restores the organization graph |

## Reproduction

Run from `web/`:

```text
pnpm exec vitest run src/pages/InsightCollaboration.test.tsx
pnpm typecheck
pnpm exec eslint src/pages/InsightCollaboration.tsx src/pages/InsightCollaboration.test.tsx
```

The targeted Vitest suite includes 11 tests and covers API query shape, organization
scope, cursor accumulation, graph ownership/LOD, semantic-edge aggregation,
evidence scoping, accessibility, all viewport interactions above, and recovery to
the unfiltered organization view.
