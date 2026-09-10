# Collaboration ECharts Canvas Acceptance

Generated: 2026-09-10T08:58:00Z

## Verdict

PASS. The prior PASS report was wrong: its own measurements showed a thin, unusable graph band. This run fixes the formal production-isomorphic route `/organizations/acme/insights/collaboration` so the ECharts canvas is the dominant interactive surface. The final DOM has one ECharts host, one Canvas renderer, no SVG graph renderer, no desktop horizontal overflow, and no visible edge list occupying document-flow graph height.

The old unusable evidence is preserved under `before/`. The final evidence set is:

- `evidence/1440x900.png`
- `evidence/1280x720.png`
- `evidence/1024x768.png`
- `evidence/1024x768-more-filters.png`
- `evidence/1024x768-evidence.png`
- `evidence/agent-browser-1280x720.png`
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
- Implementation/evidence build SHA recorded by harness: `8dab5e850433aeb4216e260ae63748e8cc3ffd8d`
- Production bundle: `internal/webconsole/spa/dist`
- Harness: `tests/e2e/v2/collaboration-echarts-acceptance.mjs`
- Browser: Chromium `148.0.7778.96`
- Node: `v25.6.0`
- Platform: `darwin`
- Data provenance: isolated local HTTP harness serving the production SPA and public `/api/orgs/acme/...` API routes with deterministic 100/500/2200-node collaboration graph payloads.

## Viewport Acceptance

| Viewport | Canvas CSS | Node bbox | Center offset | Out of bounds | Flow edge lists | Result |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| 1440x900 | 1075x538 | 0.976w / 0.885h | 0.001x / 0.020y | 0 / 397 | 0 | PASS >= 480px high |
| 1280x720 | 915x358 | 0.972w / 0.828h | 0.001x / 0.031y | 0 / 397 | 0 | PASS >= 320px high |
| 1024x768 | 659x362 | 0.961w / 0.830h | 0.002x / 0.030y | 0 / 397 | 0 | PASS >= 340px high |

For all three viewport gates, `horizontal_overflow=false` and `graph_clipped=false`. The graph toolbar, legend, timeline, collapse controls, and edge list are overlays/popovers. Opening the Evidence drawer did not reduce graph height, and closing it restored the same canvas dimensions.

## View Mode Spread

| View | Canvas CSS | Visible nodes | Node bbox | Out of bounds | Result |
| --- | ---: | ---: | ---: | ---: | --- |
| Network force | 1075x538 | 25 | 0.976w / 0.885h | 0 | PASS |
| Impact bipartite | 1075x538 | 397 | 0.976w / 0.885h | 0 | PASS |
| Lineage DAG | 1075x538 | 475 | 0.976w / 0.885h | 0 | PASS |

## Interaction Acceptance

- ECharts Canvas present: `echartsHosts=1`, `canvasCount=1`, `svgGraphCount=0`
- Active graph isolation: `visibleGraphs=1`
- Three view switches work across impact, network, and lineage
- Shared filters, More filters, Locate, Fit, and Reset remain usable
- Expand/collapse works in the active graph
- Edge Evidence opens from the overlay drawer and closes without shrinking the canvas
- Every 100/500/2200-node production-page run records `pan_changed=true`, `zoom_changed=true`, and `drag_pinned=true`
- Production mouseup/dragend converts the live Canvas pixel coordinate back into graph coordinates before persisting the pin

## Performance Smoke

| Input | Rendered nodes | TTI | Filter | Pan | Zoom | Drag | Heap used | Long tasks |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 100 | 88 | 610.6 ms | 55.1 ms | 284.7 ms | 73.4 ms | 659.3 ms | 48 MB | 0 |
| 500 | 397 | 616.6 ms | 58.2 ms | 306.1 ms | 61.7 ms | 735.1 ms | 48 MB | 2 |
| 2200 | 520 cropped | 890.0 ms | 63.4 ms | 1472.0 ms | 64.0 ms | 1336.4 ms | 48 MB | 13 |

The 2200-node run intentionally records the visible cropped size after the page-side 520-node readability cap.

## Gates

PASS:

- `cd web && pnpm exec vitest run src/pages/InsightCollaboration.test.tsx` -> 18 passed
- `cd web && npx tsc -b --force` -> PASS
- `make build` -> PASS, including production frontend bundle, backend binary, and fakeagent binary; existing non-fatal CSS minifier and chunk-size warnings remain
- `cd web && pnpm test` -> 200 files / 1930 tests passed
- `NODE_PATH=tests/e2e/v2/node_modules ACCEPTANCE_SHA=$(git rev-parse HEAD) node tests/e2e/v2/collaboration-echarts-acceptance.mjs` -> PASS
- `agent-browser --session collaboration-layout-smoke open http://127.0.0.1:4181/... && wait '[data-testid="collaboration-echarts"] canvas && get box` -> PASS, ECharts host `917x360` at 1280x720; supplemental screenshot saved to `evidence/agent-browser-1280x720.png`
