# T1760 Independent Browser Reverification

Overall verdict: REJECT / blocked-lineage

Browser/API matrix verdict: PASS

Candidate under test: `d55279debfa874ecaeff90eaac020aa62d8a7a2e`

## Lineage

- Required task-input package paths were absent: `task-input/v1/README.md`, `task-input/v1/manifest.json`.
- `raw/task-input-search.log` records a zero-result workspace search.
- The reviewed SHA was therefore inferred from local git refs, not locked from the materialized task package.
- Inferred refs: `immutable/t1760-insight-null-collections-v2`, `ac-exec/task-11b66063/exec-a5dc2b4f`.

Because the task requires exact immutable SHA lock from task input/upstream delivery and mandates REJECT on lineage uncertainty, this execution cannot self-report overall PASS.

## Production Chain

- Detached candidate worktree: see `raw/worktree-path.txt`.
- Provenance: `raw/provenance.log`.
- Running binary identity: `api/system-version.json` returned commit `d55279de` and install path under the detached candidate worktree.
- No agent-center control-plane, DB, admin socket, worker token, or raw center endpoint fallback was used.

## Build And Test Gates

All passed:

- `web` frozen install: exit 0.
- `make build`: exit 0.
- `pnpm exec vitest run src/pages/InsightOverview.test.tsx`: exit 0.

Raw logs are under `raw/`.

## Browser/API Matrix

All browser matrix checks passed in Chromium against the candidate binary:

- Fresh org real API returned HTTP 200 and null `agents`/`projects` without backend crash: `api/fresh-org-real-overview.json`.
- Fresh org UI rendered user-facing empty state: `screenshots/01-fresh-org-real-empty.png`.
- Null overview collections rendered empty states: `screenshots/02-null-collections-empty.png`.
- Null drilldown collection rendered empty state: `screenshots/03-null-drilldown-empty.png`.
- Global overview rendered summary metrics: `screenshots/04-global-overview.png`.
- Agent drilldown reconciled `window=24h&agent_ref=agent:browser-agent&limit=50`: `api/mock-executions-1.json`, `screenshots/05-agent-drilldown.png`.
- Project drilldown reconciled `window=24h&project_id=proj-browser-1&limit=50` without `agent_ref`: `api/mock-executions-2.json`, `screenshots/06-project-drilldown.png`.
- Execution detail rendered identity, status, and times: `api/mock-execution-detail.json`, `screenshots/07-execution-detail-identity-status-time.png`.
- HTTP 200 null overview response rendered an unknown empty 24h window: `api/mock-null-overview-response.json`, `screenshots/08-null-overview-empty.png`.
- HTTP 403 rendered authorization state: `api/mock-403-overview.json`, `screenshots/09-forbidden-403.png`.
- HTTP 503 rebuilding envelope rendered dedicated rebuilding state: `api/mock-503-rebuilding-overview.json`, `screenshots/10-rebuilding-503.png`.

The structured browser matrix is preserved in `browser-verdict.json`.
