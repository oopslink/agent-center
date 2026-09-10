# Collaboration ECharts Canvas Acceptance

Generated: 2026-09-10T08:42:45Z

## Verdict

PASS. The prior PASS report was wrong: its own measurements showed a thin, unusable graph band. This run fixes the formal production-isomorphic route `/organizations/acme/insights/collaboration` so the ECharts canvas is the dominant interactive surface. The final DOM has one ECharts host, one Canvas renderer, no SVG graph renderer, and no desktop horizontal overflow.

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

- Main before this remediation: `960850cb9de84955391ce31b6c942ffa24f0f2a1`
- Evidence build SHA recorded by harness: `960850cb9de84955391ce31b6c942ffa24f0f2a1`
- Production bundle: `internal/webconsole/spa/dist`
- Harness: `tests/e2e/v2/collaboration-echarts-acceptance.mjs`
- Browser: Chromium `148.0.7778.96`
- Node: `v25.6.0`
- Platform: `darwin`
- Data provenance: isolated local HTTP harness serving the production SPA and public `/api/orgs/acme/...` API routes with deterministic 100/500/2200-node collaboration graph payloads.

## Viewport Acceptance

| Viewport | Canvas CSS | Node bbox | Center offset | Out of bounds | Result |
| --- | ---: | ---: | ---: | ---: | --- |
| 1440x900 | 1075x538 | 0.976w / 0.885h | 0.001x / 0.020y | 0 / 397 | PASS >= 480px high |
| 1280x720 | 915x358 | 0.972w / 0.828h | 0.001x / 0.031y | 0 / 397 | PASS >= 320px high |
| 1024x768 | 659x362 | 0.961w / 0.830h | 0.002x / 0.030y | 0 / 397 | PASS >= 340px high |

The graph toolbar, legend, timeline, and edge list are overlays/popovers. The closed edge list has no visible in-flow rectangle. The evidence drawer remained an overlay: opening it did not reduce graph height, and closing it restored the same canvas dimensions.

## View Mode Spread

| View | Canvas CSS | Visible nodes | Node bbox | Out of bounds | Result |
| --- | ---: | ---: | ---: | ---: | --- |
| Network force | 1075x538 | 25 | 0.976w / 0.885h | 0 | PASS |
| Impact bipartite | 1075x538 | 397 | 0.976w / 0.885h | 0 | PASS |
| Lineage DAG | 1075x538 | 475 | 0.976w / 0.885h | 0 | PASS |

## Performance Smoke

| Input | Rendered nodes | TTI | Filter | Pan | Zoom | Drag | Heap used | Long tasks |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 100 | 88 | 646.8 ms | 63.4 ms | 172.2 ms | 16.5 ms | 159.7 ms | 17 MB | 0 |
| 500 | 397 | 606.1 ms | 44.0 ms | 188.1 ms | 17.7 ms | 187.4 ms | 17 MB | 2 |
| 2200 | 520 cropped | 874.8 ms | 61.8 ms | 1017.0 ms | 38.7 ms | 187.6 ms | 17 MB | 6 |

The 2200-node run intentionally records the visible cropped size after the page-side 520-node readability cap.

## Gates

PASS:

- `cd web && pnpm exec vitest run src/pages/InsightCollaboration.test.tsx` -> 18 passed
- `cd web && npx tsc -b --force` -> PASS
- `make build` -> PASS, including production frontend bundle, backend binary, and fakeagent binary; existing non-fatal CSS minifier and chunk-size warnings remain
- `cd web && pnpm test` -> 200 files / 1930 tests passed
- `NODE_PATH=tests/e2e/v2/node_modules node tests/e2e/v2/collaboration-echarts-acceptance.mjs` -> PASS
- `agent-browser open http://127.0.0.1:4179/... && agent-browser wait '[data-testid="collaboration-echarts"] canvas'` -> PASS; optional screenshot command hung and was stopped, so the committed screenshots are the Playwright harness screenshots above
