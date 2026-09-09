# Collaboration Insight Redesign Package

Status: frozen design candidate for implementation planning.
Route: `/organizations/:slug/insights/collaboration`.
Base audited: `origin/main@512d1181264a706155266fa658566146b2ae58c6`.

## Deliverables

- Product and technical design: `design.md`.
- Current-state audit and benchmark notes: `current-audit.md`.
- Static interaction prototype: `prototype/index.html`.
- Technical selection ADR: `../../decisions/0061-collaboration-insight-dimensional-graph-ui.md`.
- Measurement raw data, screenshots, repeatable benchmark script, and command
  evidence: `evidence/`.

## Prototype

Open `prototype/index.html` directly in a browser. It is a static, no-backend
prototype with deterministic 100, 500, and 2k+ node/edge datasets.

Controls covered:

- switch among Collaboration network, Task impact, and Plan path;
- shared Window/Project/Agent filters that change the real fixture node/edge
  set, with clear restore for the active dimension;
- layout switching per view;
- search, real node/edge hover/select hit-testing, focus, restore,
  expand/collapse;
- separated node drag and canvas pan, node pin/unpin state, clustering,
  progressive label LOD, mini-map, wheel zoom, and selected-edge Evidence.

The prototype intentionally demonstrates interaction semantics and performance
budgets, not final production rendering fidelity.

## Acceptance Matrix

| Gate | Prototype evidence |
|---|---|
| Project, Agent, and Window filters change graph data | `evidence/raw/benchmark-summary.json` prototype assertions: `project-filter-changes`, `agent-filter-changes`, `window-filter-changes`; filter timings in `benchmark-summary.csv`. |
| Clear restores current dimension global graph | Prototype assertion `clear-restores-current-view`. |
| Three-view switch preserves compatible filters, selection, and viewport; incompatible state is explained | Prototype assertion `view-switch-explains-context`; HUD/detail notice text records preservation or explicit incompatibility. |
| Real node/edge hit-test, hover/select, selection-driven focus | Pointer handlers use `pickNode` and `pickEdge`; benchmark drives browser mouse input against sampled graph coordinates. |
| Expand/collapse changes true neighborhood | Prototype assertion `expand-changes-neighborhood`; depth changes the filtered node/edge set. |
| Node drag and canvas pan are separate; drag pins/unpins nodes | `node-drag` and `continuous-pan` are separate benchmark operations; dragged nodes enter `state.pinned`, retain coordinates, and `pin-unpin-state-changes` verifies explicit unpin. |
| Zoom LOD and non-related edge dimming are observable | Wheel zoom benchmark changes `state.camera.zoom`; labels are gated by zoom/selection and context edges draw at reduced alpha. |
| Evidence binds selected real edge fixture | Prototype assertion `selected-edge-evidence-bound`; Evidence JSON includes selected `edge_id`, `relation`, `effect`, `direction`, `occurred_at`, and scoped event IDs. |
| 100/500/2k+ prototype and current-page baseline measured | `evidence/run-benchmarks.mjs` produces baseline and prototype timelines for all three scales. |
