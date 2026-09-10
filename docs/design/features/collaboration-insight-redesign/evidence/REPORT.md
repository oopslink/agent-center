# Collaboration Insight Redesign Evidence

Date: 2026-09-10.
Workspace base: `origin/main@512d1181264a706155266fa658566146b2ae58c6`.
Prototype URL during capture: `http://127.0.0.1:4177/index.html`.

## Browser Capture Environment

- Local server: `python3 -m http.server 4177 --bind 127.0.0.1`
- Browser: `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`
- Capture mode: `--headless=new --disable-gpu --window-size=1440,900 --virtual-time-budget=2500`
- Note: Chrome emitted macOS `CVDisplayLinkCreateWithCGDisplay failed` warnings
  during headless capture, but exited 0 and wrote PNG/DOM artifacts.

## Prototype Measurements

These numbers are from the static prototype measurement panel and browser
autorun harness, not the production SPA. They validate the proposed
LOD/cluster/rendering approach and provide a repeatable harness for later
implementation comparison. The graph records are generated in
CollaborationEffect shape, then projected into the active view.

| Artifact | View | Requested scale | Rendered nodes | Rendered edges | First interactive | Draw p95 | FPS estimate | Heap | Budget | Result |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `raw/replay-network-100.json` | Collaboration network | 100 | 100 | 113 | 90 ms | 1.0 ms | 60 | 10 MB | 16 ms | PASS |
| `raw/replay-impact-500.json` | Task impact | 500 | 341 | 310 | 542 ms | 2.8 ms | 60 | 10 MB | 24 ms | PASS |
| `raw/replay-lineage-2200.json` | Plan path | 2200 | 423 | 814 | 1051 ms | 7.5 ms | 60 | 10 MB | 40 ms | PASS |

The 2k+ case is intentionally rendered as a clustered global view. This is the
required behavior for organization-scale first paint; explicit focus/expand can
load a local neighborhood after the first interactive frame.

## Screenshots

- prior preserved screenshots: `screenshots/network-100.png`,
  `screenshots/impact-500.png`, `screenshots/lineage-2200.png`
- replay screenshots: `screenshots/replay-network-100.png`,
  `screenshots/replay-impact-500.png`, `screenshots/replay-lineage-2200.png`
- `recordings/dimensional-view-replay.webm`

## Interaction Evidence

`raw/autorun-results.json` is the authoritative interaction log. It verifies:

- shared `Window`, `Project`, and `Agent` filters and clear restore;
- selected context retained across view switch, with absent-node chip logged;
- node/edge hit testing, hover/select dimming, focus, expand, and collapse;
- node drag/pin and canvas pan as separate event paths;
- keyboard pan and keyboard Evidence open;
- Evidence bound to selected edge `effect_scopes` and `evidence_event_ids`.

`raw/replay-prototype-measurements.csv` mirrors the per-scale measurements for quick
comparison. `raw/current-page-baseline.json` records the current source-level
SPA baseline against `web/src/pages/InsightCollaboration.tsx`.

## Commands

```sh
node --check docs/design/features/collaboration-insight-redesign/prototype/prototype.js
python3 -m http.server 4177 --bind 127.0.0.1
agent-browser open 'http://127.0.0.1:4177/index.html?autorun=1'
agent-browser wait 6000
agent-browser get text '#autorun-results' > docs/design/features/collaboration-insight-redesign/evidence/raw/autorun-results.json
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --disable-gpu --no-first-run --no-default-browser-check --window-size=1440,900 --virtual-time-budget=2500 --screenshot=docs/design/features/collaboration-insight-redesign/evidence/screenshots/replay-network-100.png 'http://127.0.0.1:4177/index.html?view=network&scale=100&window=30d'
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --disable-gpu --no-first-run --no-default-browser-check --window-size=1440,900 --virtual-time-budget=2500 --screenshot=docs/design/features/collaboration-insight-redesign/evidence/screenshots/replay-impact-500.png 'http://127.0.0.1:4177/index.html?view=impact&scale=500&window=30d'
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --disable-gpu --no-first-run --no-default-browser-check --window-size=1440,900 --virtual-time-budget=2500 --screenshot=docs/design/features/collaboration-insight-redesign/evidence/screenshots/replay-lineage-2200.png 'http://127.0.0.1:4177/index.html?view=lineage&scale=2200&window=30d'
ffmpeg -y -loop 1 -t 1.2 -i docs/design/features/collaboration-insight-redesign/evidence/screenshots/replay-network-100.png -loop 1 -t 1.2 -i docs/design/features/collaboration-insight-redesign/evidence/screenshots/replay-impact-500.png -loop 1 -t 1.2 -i docs/design/features/collaboration-insight-redesign/evidence/screenshots/replay-lineage-2200.png -filter_complex '[0:v]scale=1440:900,setsar=1[v0];[1:v]scale=1440:900,setsar=1[v1];[2:v]scale=1440:900,setsar=1[v2];[v0][v1][v2]concat=n=3:v=1:a=0,format=yuv420p' -r 12 docs/design/features/collaboration-insight-redesign/evidence/recordings/dimensional-view-replay.webm
```
