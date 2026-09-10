# Collaboration Insight Hard-Gate Readback

Date: 2026-09-10.
Replay branch base: `origin/main@512d1181264a706155266fa658566146b2ae58c6`.

## Product Gates

| Gate | Evidence |
|---|---|
| Default first screen does not render all dimensions | `prototype/index.html` defaults to `view=network`; `evidence/raw/autorun-results.json` has `acceptance.dimensional_first_screen=true`. |
| Three task-oriented views | `design.md` defines Collaboration network, Task impact, and Plan path with separate questions, projections, and layouts. |
| Shared time/Project/Agent filters | Prototype top bar shares Window/Project/Agent across tabs; `autorun-results.json` includes `filter-window`, `filter-project`, and `filter-agent` events plus `filter_proofs`. |
| Cross-view context | `prototype/prototype.js` retains selected context on view switch and logs `view-switch.retained_context`; absent-context cases are explicitly logged as `context-chip-retained-without-node`. |
| Search, focus, restore, expand/collapse | Prototype controls are wired to local graph state; raw events include `expand-neighborhood`, `collapse-neighborhood`, and `clear-filters-restore-global`. |
| Node/edge hit-test and hover/select | Canvas hit testing selects both nodes and edges from current graph geometry; raw events include `canvas-hit-test` and `select`. |
| Node drag/pin separated from canvas pan | UI pointer handlers branch on hit-test result; harness events include both `drag-pin-node` and `pan-canvas`. |
| Evidence bound to selected real edge scope | Evidence dialog reads `effect_scopes` and `evidence_event_ids` from the selected graph edge; raw events include `open-evidence` with non-zero counts. |
| Keyboard operation | Canvas `keydown` supports arrows, `+/-`, Escape, Enter, and `e`; raw events include `keyboard-pan` and keyboard Evidence open. |
| Clear filters restores active dimension global view | `clear-filters-restore-global` clears shared filters without changing attribution facts. |

## Performance Gates

| Scale | Raw artifact | Screenshot | Rendered | TTI | Draw p95 | FPS | Heap | Budget result |
|---|---|---|---:|---:|---:|---:|---:|---|
| 100 | `evidence/raw/replay-network-100.json` | `evidence/screenshots/replay-network-100.png` | 100 nodes / 113 edges | 90 ms | 1.0 ms | 60 | 10 MB | PASS |
| 500 | `evidence/raw/replay-impact-500.json` | `evidence/screenshots/replay-impact-500.png` | 341 nodes / 310 edges | 542 ms | 2.8 ms | 60 | 10 MB | PASS |
| 2k+ | `evidence/raw/replay-lineage-2200.json` | `evidence/screenshots/replay-lineage-2200.png` | 423 clustered nodes / 814 edges | 1051 ms | 7.5 ms | 60 | 10 MB | PASS |

Raw summary CSV: `evidence/raw/replay-prototype-measurements.csv`.
Replay artifact: `evidence/recordings/dimensional-view-replay.webm`.

## Implementation Boundaries

- Reuse: CollaborationEffect facts, effect scopes, evidence bundle endpoint,
  current filters, empty/403/500 states, and accessible list contracts.
- Additive API: `view`, `layout_hint`, `focus_id`, `depth`, `aggregate_by`,
  coordinates metadata, bounds, and per-view summaries.
- Frontend replacement: dimensional adapter, layout cache boundary, canvas/WebGL
  renderer, Worker aggregation/culling path, and measurement harness.
- Forbidden change: do not modify relation attribution, polarity, evidence event
  IDs, before/after state, or rule-version semantics.
