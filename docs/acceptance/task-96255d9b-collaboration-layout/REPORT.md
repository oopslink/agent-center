# Collaboration ECharts Canvas Acceptance

Generated: 2026-09-10T09:25:27Z

## Verdict

PASS. The prior PASS report was wrong: its own measurements showed a thin, unusable graph band. This run fixes the formal production-isomorphic route `/organizations/acme/insights/collaboration` so the ECharts canvas is the dominant interactive surface. The final DOM has one ECharts host, one Canvas renderer, no SVG graph renderer, no desktop horizontal overflow, and no visible edge list occupying document-flow graph height.

The old unusable evidence is preserved under `before/`. The final evidence set is:

- `evidence/1440x900.png`
- `evidence/1280x720.png`
- `evidence/1024x768.png`
- `evidence/1024x768-more-filters.png`
- `evidence/1024x768-evidence.png`
- `raw/acceptance-results.json`
- `raw/acceptance-run.log`
- `raw/api-log.json`
- `raw/network-log.json`
- `raw/console-log.json`
- `raw/smoke-100.json`
- `raw/smoke-500.json`
- `raw/smoke-2200.json`

## Provenance

- Original ECharts implementation baseline: `ec4fc0d40d8b69fea90f1ebda47bd6c04b0038e6`
- Main before this remediation: `960850cb9de84955391ce31b6c942ffa24f0f2a1`
- Upstream main integrated before this remediation commit: `98a45ce2335586f73beabf9173ce9013427f5f4d`
- Dense-layout remediation baseline: `26d1bd6036d4aa55651bc607e5ecb6fe8d8f9903`
- Implementation/evidence build SHA recorded by harness: `cca1581c72ecc57c646ea5529a7ead4af25a7952`
- Production bundle: `internal/webconsole/spa/dist`
- Harness: `tests/e2e/v2/collaboration-echarts-acceptance.mjs`
- Browser: Chromium `148.0.7778.96`
- Node: `v25.6.0`
- Platform: `darwin`
- Data provenance: isolated local HTTP harness serving the production SPA and public `/api/orgs/acme/...` API routes with deterministic 100/500/2200-node collaboration graph payloads.

## Viewport Acceptance

| Viewport | Canvas CSS | Node bbox | Center offset | Out of bounds | Overlap | Result |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| 1440x900 | 1075x538 | 0.976w / 0.885h | 0.001x / 0.020y | 0 / 100 | 0 / 100 | PASS >= 480px high |
| 1280x720 | 915x358 | 0.972w / 0.828h | 0.001x / 0.031y | 0 / 100 | 0 / 100 | PASS >= 320px high |
| 1024x768 | 659x362 | 0.961w / 0.830h | 0.002x / 0.030y | 0 / 100 | 0 / 100 | PASS >= 340px high |

For all three viewport gates, `horizontal_overflow=false` and `graph_clipped=false`. The graph toolbar, legend, timeline, collapse controls, and edge list are overlays/popovers. Opening the Evidence drawer did not reduce graph height, and closing it restored the same canvas dimensions.

## View Mode Spread

| View | Canvas CSS | Visible nodes | Node bbox | Out of bounds | Result |
| --- | ---: | ---: | ---: | ---: | --- |
| Network force | 1075x538 | 25 | 0.976w / 0.885h | 0 | PASS, 0 overlapping nodes |
| Impact bipartite | 1075x538 | 100 | 0.976w / 0.885h | 0 | PASS, 0 overlapping nodes |
| Lineage DAG | 1075x538 | 100 | 0.976w / 0.885h | 0 | PASS, 0 overlapping nodes |

## Interaction Acceptance

- ECharts Canvas present: `echartsHosts=1`, `canvasCount=1`, `svgGraphCount=0`
- Active graph isolation: `visibleGraphs=1`
- Three view switches work across impact, network, and lineage
- Shared filters, More filters, Locate, Fit, and Reset remain usable
- Expand/collapse works in the active graph
- Edge Evidence opens from the overlay drawer and closes without shrinking the canvas
- Every 100/500/2200-node production-page run records `pan_changed=true`, `zoom_changed=true`, and `drag_pinned=true`
- Production mouseup/dragend converts the live Canvas pixel coordinate back into graph coordinates before persisting the pin
- The active-view readability cap is 100 nodes / 240 edges; compact symbols and the acceptance overlap check keep every rendered node independently hit-testable

## Performance Smoke

| Input | Rendered nodes | TTI | Filter | Pan | Zoom | Drag | Heap used | Long tasks |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 100 | 88 | 654.3 ms | 118.9 ms | 278.8 ms | 61.9 ms | 714.5 ms | 20.5 MB | 84 ms |
| 500 | 100 cropped | 629.8 ms | 61.5 ms | 294.6 ms | 64.8 ms | 755.6 ms | 20.5 MB | 75 ms |
| 2200 | 100 cropped | 669.4 ms | 68.3 ms | 366.2 ms | 65.3 ms | 807.1 ms | 20.5 MB | 126 ms |

The 500- and 2200-node runs intentionally record the visible cropped size after the page-side 100-node / 240-edge readability cap. The harness rejects any viewport or view mode with more than 8% overlapping rendered nodes; the final run recorded zero overlaps in every checked case. The previous 397/520-node Canvas screenshots were rejected because a broad bbox did not make their stacked nodes readable.

## Gates

PASS:

- `cd web && pnpm exec vitest run src/pages/InsightCollaboration.test.tsx` -> 18 passed
- `cd web && npx tsc -b --force` -> PASS
- `make build` -> PASS, including production frontend bundle, backend binary, and fakeagent binary; existing non-fatal CSS minifier and chunk-size warnings remain
- `cd web && pnpm test` -> 200 files / 1930 tests passed
- `NODE_PATH=tests/e2e/v2/node_modules ACCEPTANCE_SHA=cca1581c72ecc57c646ea5529a7ead4af25a7952 node tests/e2e/v2/collaboration-echarts-acceptance.mjs` -> PASS
