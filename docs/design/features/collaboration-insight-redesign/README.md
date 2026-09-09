# Collaboration Insight Redesign Package

Status: frozen design candidate for implementation planning.
Route: `/organizations/:slug/insights/collaboration`.
Base audited: `origin/main@9cd47759fc25bbd34423425aa937a6e93f6eb085`.

## Deliverables

- Product and technical design: `design.md`.
- Current-state audit and benchmark notes: `current-audit.md`.
- Static interaction prototype: `prototype/index.html`.
- Technical selection ADR: `../../decisions/0061-collaboration-insight-dimensional-graph-ui.md`.
- Measurement raw data and screenshots: `evidence/`.

## Prototype

Open `prototype/index.html` directly in a browser. It is a static, no-backend
prototype with deterministic 100, 500, and 2k+ node/edge datasets.

Controls covered:

- switch among Collaboration network, Task impact, and Plan path;
- shared Window/Project/Agent filters with clear restore;
- layout switching per view;
- search, focus, restore, expand/collapse;
- clustering, progressive label LOD, mini-map, pan/zoom, keyboard Evidence.

The prototype intentionally demonstrates interaction semantics and performance
budgets, not final production rendering fidelity.
