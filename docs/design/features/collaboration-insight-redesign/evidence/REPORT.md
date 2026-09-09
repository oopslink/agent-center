# Collaboration Insight Redesign Evidence

Date: 2026-09-09.
Workspace base: `origin/main@512d1181264a706155266fa658566146b2ae58c6`.
Prior reusable delivery: `origin/ac-exec/task-39d72e32/exec-e5de8dfb@43177ded3e8ed2eff3fd37ba33ade3653d23d5f7`.

## What Was Repaired

- Project, Agent, and Window filters now change the actual fixture graph
  node/edge set. Clear restores the active dimension's global graph.
- View switching preserves compatible filters, selection, and viewport; visible
  HUD/detail text explains incompatible selection clearing.
- Node and edge hover/select use geometric hit-testing. Focus and
  expand/collapse act on the selected node, not a fixed first node.
- Canvas pan and node drag are separate pointer states. Dragged nodes are pinned
  and keep coordinates across redraws.
- Wheel zoom drives observable LOD: labels hide at low zoom and selected/heavy
  labels remain visible; unrelated edges are dimmed under focus/selection.
- Evidence is bound to the selected real edge fixture and includes relation,
  effect, direction, occurrence time, effect/project scope, and event payloads.

## Repeatable Benchmark

Command:

```sh
node --check docs/design/features/collaboration-insight-redesign/prototype/prototype.js
node --check docs/design/features/collaboration-insight-redesign/evidence/run-benchmarks.mjs
node docs/design/features/collaboration-insight-redesign/evidence/run-benchmarks.mjs
```

Exit code: `0`.

The script serves `prototype/index.html` locally, opens headless Chrome through
CDP, and runs both `mode=baseline` and `mode=prototype` at 100, 500, and 2200
requested scale. Each run measures first interactive, idle redraw, continuous
pan, wheel zoom, node drag, view switching, filter response, and heap memory.

Raw artifacts:

- Timelines: `raw/baseline-100-timeline.json`,
  `raw/baseline-500-timeline.json`, `raw/baseline-2200-timeline.json`,
  `raw/prototype-100-timeline.json`, `raw/prototype-500-timeline.json`,
  `raw/prototype-2200-timeline.json`.
- Summary: `raw/benchmark-summary.json` and `raw/benchmark-summary.csv`.
- Screenshots: `screenshots/baseline-100.png`, `screenshots/baseline-500.png`,
  `screenshots/baseline-2200.png`, `screenshots/prototype-100.png`,
  `screenshots/prototype-500.png`, `screenshots/prototype-2200.png`.
- Script: `run-benchmarks.mjs`.

## Results

| Mode | Scale | TTI ms | Idle p95 ms | Pan wall ms | Zoom wall ms | Drag wall ms | Switch wall ms | Filter wall ms | Heap MB | Assertions |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|
| Current DOM/SVG baseline | 100 | 35 | 2.4 | 1043.91 | 412.4 | 160.11 | 84.76 | 83.42 | 2 | PASS |
| Current DOM/SVG baseline | 500 | 8 | 3.7 | 1320.64 | 519.19 | 230.3 | 90.02 | 87.78 | 7 | PASS |
| Current DOM/SVG baseline | 2200 | 11 | 14.1 | 1056.96 | 722.83 | 136.28 | 114.05 | 107.74 | 25 | PASS |
| Prototype Canvas interaction | 100 | 6 | 7.2 | 1287.72 | 1137.43 | 155.33 | 82.14 | 81.72 | 20 | PASS |
| Prototype Canvas interaction | 500 | 7 | 3.1 | 1581.74 | 1057.27 | 237.48 | 86.92 | 85.12 | 11 | PASS |
| Prototype Canvas interaction | 2200 | 10 | 7.5 | 1442.34 | 802.36 | 323.21 | 90.88 | 88.22 | 26 | PASS |

Prototype assertion keys in `benchmark-summary.json`:

- `project-filter-changes`
- `agent-filter-changes`
- `window-filter-changes`
- `clear-restores-current-view`
- `selected-edge-evidence-bound`
- `expand-changes-neighborhood`
- `pin-unpin-state-changes`
- `view-switch-explains-context`

## Interpretation

The baseline is a real browser DOM/SVG page path using the current Collaboration
Insight rendering constraints: retained SVG elements, SVG text labels, and
viewBox viewport changes. The prototype is the repaired Canvas interaction path.
The evidence is not a production implementation verdict; it freezes the
interaction contract and performance measurement method for the developer who
implements the production Graph.
