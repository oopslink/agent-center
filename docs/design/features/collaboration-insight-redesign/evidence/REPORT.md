# Collaboration Insight Redesign Evidence

Date: 2026-09-09.
Workspace base: `origin/main@9cd47759fc25bbd34423425aa937a6e93f6eb085`.
Prototype URL during capture: `http://127.0.0.1:4177/index.html`.

## Browser Capture Environment

- Local server: `python3 -m http.server 4177 --bind 127.0.0.1`
- Browser: `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`
- Capture mode: `--headless=new --disable-gpu --window-size=1440,900 --virtual-time-budget=2500`
- Note: Chrome emitted macOS `CVDisplayLinkCreateWithCGDisplay failed` warnings
  during headless capture, but exited 0 and wrote PNG/DOM artifacts.

## Prototype Measurements

These numbers are from the static prototype measurement panel, not the production
SPA. They validate the proposed LOD/cluster/rendering approach and provide a
repeatable harness for later implementation comparison.

| Artifact | View | Requested scale | Rendered nodes | Rendered edges | First interactive | Draw p95 | FPS estimate | Heap | Budget | Result |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `raw/network-100.json` | Collaboration network | 100 | 100 | 120 | 101 ms | 6.0 ms | 60 | 1 MB | 16 ms | PASS |
| `raw/impact-500.json` | Task impact | 500 | 500 | 600 | 116 ms | 6.6 ms | 60 | 1 MB | 24 ms | PASS |
| `raw/lineage-2200.json` | Plan path | 2200 | 422 | 900 | 133 ms | 17.5 ms | 57 | 2 MB | 40 ms | PASS |

The 2k+ case is intentionally rendered as a clustered global view. This is the
required behavior for organization-scale first paint; explicit focus/expand can
load a local neighborhood after the first interactive frame.

## Screenshots

- `screenshots/network-100.png`
- `screenshots/impact-500.png`
- `screenshots/lineage-2200.png`

## Interaction Evidence

`raw/interaction-evidence-snapshot.txt` was captured after opening the prototype,
activating Focus, and opening Evidence. The snapshot includes the graph controls,
view tabs, shared filters, and Evidence dialog controls.

## Commands

```sh
node --check docs/design/features/collaboration-insight-redesign/prototype/prototype.js
python3 -m http.server 4177 --bind 127.0.0.1
agent-browser open 'http://127.0.0.1:4177/index.html?view=network&scale=100'
agent-browser wait 1000
agent-browser snapshot -i
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --disable-gpu --no-first-run --no-default-browser-check --window-size=1440,900 --virtual-time-budget=2500 --screenshot=docs/design/features/collaboration-insight-redesign/evidence/screenshots/network-100.png 'http://127.0.0.1:4177/index.html?view=network&scale=100'
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --disable-gpu --no-first-run --no-default-browser-check --window-size=1440,900 --virtual-time-budget=2500 --screenshot=docs/design/features/collaboration-insight-redesign/evidence/screenshots/impact-500.png 'http://127.0.0.1:4177/index.html?view=impact&scale=500'
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --disable-gpu --no-first-run --no-default-browser-check --window-size=1440,900 --virtual-time-budget=2500 --screenshot=docs/design/features/collaboration-insight-redesign/evidence/screenshots/lineage-2200.png 'http://127.0.0.1:4177/index.html?view=lineage&scale=2200'
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --disable-gpu --no-first-run --no-default-browser-check --window-size=1440,900 --dump-dom 'http://127.0.0.1:4177/index.html?view=network&scale=100' > docs/design/features/collaboration-insight-redesign/evidence/raw/network-100-dom.html
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --disable-gpu --no-first-run --no-default-browser-check --window-size=1440,900 --dump-dom 'http://127.0.0.1:4177/index.html?view=impact&scale=500' > docs/design/features/collaboration-insight-redesign/evidence/raw/impact-500-dom.html
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --disable-gpu --no-first-run --no-default-browser-check --window-size=1440,900 --dump-dom 'http://127.0.0.1:4177/index.html?view=lineage&scale=2200' > docs/design/features/collaboration-insight-redesign/evidence/raw/lineage-2200-dom.html
```
