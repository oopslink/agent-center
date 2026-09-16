# I166 Plan DAG React Flow + ELK Migration

Date: 2026-09-14

## Scope

PlanDetail DAG rendering now uses `@xyflow/react` for the interactive canvas and `elkjs` for automatic layered layout. The migration covers both sources of topology:

- Orchestration graph plans from `/plans/{id}/graph`.
- Legacy `Plan.nodes[].depends_on` plans, including synthetic Start/End anchors.

The backend API remains the source of truth. This change does not alter lifecycle state, dependency semantics, stage semantics, task opening, or backend permission checks.

## Locked Dependencies And Licenses

- `@xyflow/react@12.8.4`, license `MIT` from local package metadata.
- `elkjs@0.10.0`, license `EPL-2.0` from local package metadata.

Both are free/open-source core capabilities. Versions are pinned in `web/package.json` and `web/pnpm-lock.yaml`.

## Design

- `web/src/pages/planDagFlow.ts` is the business adapter boundary. It translates Plan graph or legacy nodes into React Flow nodes/edges and asks ELK for layered coordinates.
- Stage grouping is represented as React Flow compound nodes (`stage:{id}`) with child task nodes using `parentId`, relative coordinates, and `extent: "parent"`.
- Cross-stage and branch/join dependencies remain real edges; no old layout fallback is used if ELK is slow or errors. The error path renders an empty layout instead of silently switching to the old SVG algorithm.
- `PlanDetail.tsx` owns business UI only: task cards, control nodes, stage headers, dependency-edit affordances, generation history, mobile stepper, and status/permission rules.
- `useElkFlowLayout` uses a monotonic request id so an older async ELK result cannot overwrite a newer topology.
- `PlanFlowFitView` fits the view only when the topology key changes. Status-only refreshes keep the user's pan/zoom viewport stable.
- Status, title, assignee, and Stage metadata are refreshed onto the existing ELK coordinates without triggering a new layout or viewport reset.

## Behavior Reconciliation

- Ordinary graph: business/control nodes render in React Flow with existing task id tags, state chips, assignees, and task links.
- Staged graph: stage boxes contain the correct member task nodes; stage status/progress/rounds and gate audit dialogs remain available.
- Cross-stage dependencies: retained as React Flow edges between task/control nodes, including historical staged revisions.
- Branch/join: ELK lays out multi-parent and multi-child DAGs without overlapping task cards.
- Legacy graph: `depends_on` still means `from_task_id` depends on `to_task_id`; synthetic Start/End edges are separated from real dependency edges for tests and editing semantics.
- Editing: pending-only connect/delete controls still call the existing add/remove dependency APIs and preserve friendly error mapping. Running/done/archived plans remain display-only in the UI and protected by the backend.
- Empty/single node: empty plans render the existing empty state; single-node plans get Start -> node and node -> End anchors.
- Long titles: task cards keep fixed dimensions and wrap/truncate within the card rather than resizing the layout.
- Mobile: the vertical stepper remains, ordered from ELK coordinates/topology, while desktop uses React Flow pan/zoom/minimap/fit controls.
- Theme: React Flow tokens are bound to existing semantic CSS variables in `web/src/index.css`.

## Verification

Automated coverage added or updated:

- `src/pages/PlanDetail.test.tsx`: 112 tests pass, including legacy DAG, graph DAG, stages, generation history, dependency editing, mobile stepper, empty/no-stage, and has_graph loading transition.
- `src/pages/planGraphLayout.test.ts`: 18 tests pass, including new React Flow + ELK adapter coverage for staged containment, cross-stage edges, branch/join non-overlap, legacy synthetic anchors, arrow markers, and status-only data refresh without coordinate changes.
- Full web suite: 200 files / 1946 tests pass on the frozen `ee9eab20` baseline.

Performance measurement:

- Command: local Node script using `elkjs/lib/elk.bundled.js` with the same layered/orthogonal options as the adapter.
- Environment: Node `v25.6.0`, `darwin/arm64`.
- 100 nodes, 127 edges: `143.9 ms` layout.
- 500 nodes, 647 edges: `610.3 ms` layout.

Observed interaction path:

- React Flow supplies pan, zoom, pinch zoom, minimap, and fit-view controls.
- Layout is asynchronous and guarded against stale results.
- Status refresh uses the same topology key and does not force viewport refit.
