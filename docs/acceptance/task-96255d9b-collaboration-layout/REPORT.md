# Collaboration ECharts Canvas Acceptance

Generated: 2026-09-10T08:42:40Z

## Verdict

PASS. The formal production-isomorphic route `/organizations/acme/insights/collaboration` loads the ECharts graph from the production SPA bundle. The final DOM has one ECharts host and one Canvas renderer, renders only one active graph view at a time, and has no graph SVG renderer.

The prior SVG/viewBox report and screenshots are deprecated. The current acceptance set is:

- `evidence/1440x900.png`
- `evidence/1280x720.png`
- `evidence/1024x768.png`
- `evidence/1024x768-more-filters.png`
- `evidence/1024x768-evidence.png`
- `raw/acceptance-results.json`
- `raw/api-log.json`
- `raw/network-log.json`
- `raw/console-log.json`
- `raw/smoke-100.json`
- `raw/smoke-500.json`
- `raw/smoke-2200.json`

## Provenance

- Original ECharts implementation baseline: `ec4fc0d40d8b69fea90f1ebda47bd6c04b0038e6`
- Main before supervisor drag-evidence remediation: `960850cb9de84955391ce31b6c942ffa24f0f2a1`
- Implementation/evidence build SHA: `8d8d7f2d7514ff653e91077b7d2978b7009cb6bc`
- Acceptance asset commit / main after code and evidence: `5ee49e2735bd6269e50d490825e7f744d8437b08`
- Production bundle: `internal/webconsole/spa/dist`
- Harness: `tests/e2e/v2/collaboration-echarts-acceptance.mjs`
- Browser: Chromium `148.0.7778.96`
- Node: `v25.6.0`
- Platform: `darwin`
- Data provenance: isolated local HTTP harness serving the production SPA and public `/api/orgs/acme/...` API routes with deterministic generated collaboration graph payloads.

The page consumed the same public collaboration API route used by production. `max_nodes` was passed through the route query. No static prototype, hidden mock renderer, or SVG fallback was used.

## Viewport Acceptance

| Viewport | Toolbar rect | Graph rect | Visible graph | Panel rect | document scroll/client | Canvas CSS | Result |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1440x900 | x=320 y=86 w=1095 h=84 | x=333 y=395 w=769 h=342 | 767x340 | x=320 y=318 w=795 h=558 | 1440/1440 | 767x340 | no horizontal overflow, no clipping |
| 1280x720 | x=320 y=86 w=935 h=84 | x=333 y=395 w=609 h=162 | 607x160 | x=320 y=318 w=635 h=378 | 1280/1280 | 607x160 | no horizontal overflow, no clipping |
| 1024x768 | x=320 y=86 w=679 h=128 | x=333 y=497 w=449 h=108 | 447x106 | x=320 y=362 w=475 h=382 | 1024/1024 | 447x106 | no horizontal overflow, no clipping |

## Interaction Assertions

Automated PASS:

- ECharts Canvas present: `echartsHosts=1`, `canvasCount=1`, `svgGraphCount=0`
- Active graph isolation: `visibleGraphs=1`
- Three view switches work across impact, network, and sequence
- Shared filters and Locate remain usable
- Clearing filters restores the current dimension full graph
- Fit and Reset execute on the ECharts instance
- Continuous pan and wheel zoom execute on the production page
- Every 100/500/2200-node run records a changed Canvas image after pan and zoom
- Direct ECharts node hit-test selects a node
- Direct ECharts edge hit-test opens Evidence
- Focus and Restore work from selected graph state
- Expand/collapse works in the active graph
- Drag/pin persists to `sessionStorage` and restores after remount
- Every 100/500/2200-node production-page run drags the rendered `agent:hub` node and records `drag_pinned=true`
- Production mouseup/dragend converts the live Canvas pixel coordinate back into graph coordinates before persisting the pin

## Performance Smoke

| Input | Rendered nodes/edges | TTI | Filter | Pan | Zoom | Drag | Heap used | Long tasks |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 100 | 100 / 160 | 760.2 ms | 47.3 ms | 236.3 ms | 68.1 ms | 660.9 ms | 35.1 MB | 61 ms |
| 500 | 500 / 900 | 677.9 ms | 113.2 ms | 382.2 ms | 65.9 ms | 841.2 ms | 35.1 MB | 247, 57, 154, 52 ms |
| 2200 | 2200 / 3600 | 735.2 ms | 141.5 ms | 1636.9 ms | 67.1 ms | 1280.8 ms | 35.1 MB | 265, 96, 159, 71, 206, 129, 163, 129, 165, 131, 129, 125, 128, 129, 129 ms |

The 2200-node run is the requested 2k+ case. The public API returned and the page rendered the full requested size for all three runs; no API-side clipping occurred in this harness.

The original evidence run only timed pointer gestures and left `sessionPins` empty. That result was rejected during supervisor review. The final harness now fails unless pan and zoom visibly change the Canvas and a real rendered-node drag persists `agent:hub`; the replacement raw JSON records those booleans and coordinates for every scale.

## Gates

PASS:

- `cd web && pnpm vitest run src/pages/InsightCollaboration.test.tsx` -> 18 passed
- `cd web && pnpm run typecheck` -> PASS
- `make build-frontend` -> PASS, with existing non-fatal CSS minifier and chunk-size warnings
- `cd web && pnpm test` -> 200 files / 1930 tests passed
- `cd tests/e2e/v2 && ACCEPTANCE_SHA=8d8d7f2d7514ff653e91077b7d2978b7009cb6bc node collaboration-echarts-acceptance.mjs` -> PASS
