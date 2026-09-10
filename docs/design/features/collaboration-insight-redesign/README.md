# Collaboration Insight Redesign Package

Status: frozen design candidate for implementation planning.
Route: `/organizations/:slug/insights/collaboration`.
Base audited: `origin/main@512d1181264a706155266fa658566146b2ae58c6`.

## Deliverables

- Product and technical design: `design.md`.
- Current-state audit and benchmark notes: `current-audit.md`.
- Static interaction prototype: `prototype/index.html`.
- Technical selection ADR: `../../decisions/0061-collaboration-insight-dimensional-graph-ui.md`.
- Measurement raw data and screenshots: `evidence/`.
- Owner hard-gate readback: `hard-gate-readback.md`.

## Prototype

Open `prototype/index.html` directly in a browser. It is a static, no-backend
prototype with deterministic CollaborationEffect-shaped 100, 500, and 2k+
node/edge datasets. The prototype derives each view from the same effect records
instead of hard-coding a separate graph per tab.

Controls covered:

- switch among Collaboration network, Task impact, and Plan path;
- shared Window/Project/Agent filters with clear restore;
- layout switching per view;
- search, focus, restore, expand/collapse;
- clustering, progressive label LOD, mini-map, pan/zoom, keyboard Evidence.
- node/edge hit testing, hover/select dimming, selected-context carryover,
  node drag pinning, and canvas pan separation.

The prototype intentionally demonstrates interaction semantics and performance
budgets, not final production rendering fidelity.
