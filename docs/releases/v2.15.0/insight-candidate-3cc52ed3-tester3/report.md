# Insight Candidate 3cc52ed3 Tester3 Browser Acceptance

Verdict: PASS

Reviewed candidate SHA: `3cc52ed300bfe00444d6487aded97e96280d2b16`

Review evidence branch: `ac-evidence/insight-3cc52ed3-tester3`

Date: 2026-08-29

## Scope

Independently exercised the real rendered Web candidate with `agent-browser` against a deterministic local fixture API. The app source was checked out detached at the exact requested candidate SHA before runtime work.

The expected `task-input/v1/README.md` and `task-input/v1/manifest.json` package was not present in this workspace or immediate parent worktrees. Team-rule lookup tools were also unavailable by task isolation instruction; the review followed the rule text embedded in the task prompt.

## Harness

Runnable fixture API:

```bash
node docs/releases/v2.15.0/insight-candidate-3cc52ed3-tester3/fixture-api.mjs
```

Runnable Web app:

```bash
cd web
pnpm install --frozen-lockfile
pnpm exec vite --config vite.tester3.config.mjs --host 127.0.0.1 --port 5177 --strictPort
```

Fixture API port: `127.0.0.1:7110`.

Vite port: `127.0.0.1:5177`.

The default backend port `127.0.0.1:7100` was already occupied, so the review-only Vite config proxies `/api` to `7110` without modifying the candidate page code.

## Evidence Index

Screenshots, text extracts, snapshots, and hashes are under:

`docs/releases/v2.15.0/insight-candidate-3cc52ed3-tester3/screenshots/`

Hash manifest:

`docs/releases/v2.15.0/insight-candidate-3cc52ed3-tester3/evidence-sha256.txt`

## Contract Observations

| Contract class | URL / action | Expected | Actual | Evidence |
|---|---|---|---|---|
| Overview 24h context and rankings | `/organizations/acme/insights/overview?scenario=multi` | Shows rolling 24h window, refreshed time, summary cards, agent/project rankings, sample counts, diagnostics. | PASS: visible `Past 24 hours (rolling)`, `UTC+08:00`, `Fresh data`, completed/failure/utilization cards, By agent/By project tables, P50/P95 samples, invalid/late diagnostics. | `01-desktop-overview-multi.png`, `.txt`, `.snapshot.txt` |
| Agent drilldown | Click first By agent `View executions` | Navigates to execution list with 24h and agent filter context. | PASS: URL ended at `/insights/executions?window=24h&agent_ref=agent%3Abuilder`; filter chip `Agent: agent:builder` visible; rows limited to Builder Agent. | `02-agent-filter-list.png`, `02-agent-filter-url.txt` |
| Execution detail from filtered list | Click `Ship Insight drilldown` | Detail page remains usable and shows execution timeline/result. | PASS: URL `/insights/executions/exec-ok-single`; visible `TaskExecution detail`, `Past 24 hours (rolling)`, `Completed`, timeline times, queue wait, duration, worker, execution id, command id. | `03-execution-detail.png`, `.txt`, `.snapshot.txt` |
| Project drilldown | Click first By project `View executions` | Navigates to execution list with 24h and project filter context. | PASS: URL ended at `/insights/executions?window=24h&project_id=proj-alpha`; filter chip `Project: proj-alpha` visible; rows limited to Alpha Project. | `04-project-filter-list.png`, `04-project-filter-url.txt` |
| User-facing status/time labels | `/organizations/acme/insights/executions?window=24h&scenario=multi` | No raw enums in primary table; running/rejected/unknown/invalid rows readable. | PASS: visible `Completed`, `Failed`, `Running`, `Did not start`, `Outcome unavailable`, `Not started`, `Not finished`, `Invalid time data`; times formatted as local clock values. | `05-executions-multi.png`, `.txt` |
| Failure detail | `/organizations/acme/insights/executions/exec-failed-multi` | Shows failed status and concrete failure message. | PASS: visible `Failed`, `Process exited with code 1.`, timeline, result, task/project/agent/worker fields. | `06-failed-detail.png`, `.txt` |
| Invalid/recovered detail | `/organizations/acme/insights/executions/exec-invalid-time` | Keeps invalid record visible, labels quality, excludes invalid intervals by explanation. | PASS: visible `Outcome unavailable`, `Recovered by system`, `Invalid time data`, and quality explanation. | `07-invalid-detail.png`, `.txt` |
| Empty sample | `overview?scenario=empty` and `executions?scenario=empty` | Empty overview/list states are explicit. | PASS: overview shows no dimension rows and `No executions in the past 24 hours`; execution list shows same empty state. | `08-empty-overview.png`, `09-empty-executions.png` |
| Single numeric-zero sample | `overview?scenario=single` | Numeric zero remains distinct from unknown. | PASS: visible `Failure rate 0%`, `Slot utilization 0%`, `P50 0 ms / P95 0 ms`, single agent/project rows. | `10-single-zero-overview.png`, `.txt` |
| Unknown/no-sample sample | `overview?scenario=unknown` | Unknown capacity/no samples are not shown as numeric zero. | PASS: visible `Slot utilization Cannot determine`, `No computable capacity baseline`, `No valid samples`, `-- · 0` in dimension rows. | `11-unknown-overview.png`, `.txt` |
| Stale state | `overview?scenario=stale` | Stale freshness is visible while metrics remain available. | PASS: visible `Data delayed`, `Data is delayed`, stale body with last updated time, partial observation note, diagnostics. | `12-stale-overview.png`, `.txt` |
| zh locale | `ac.lang=zh`, overview/list URLs | Insight pages switch to zh copy. | PASS: primary Insight labels switch to zh: `洞察`, `Insight 概览`, `查看全部执行记录`, `过去 24 小时（滚动）`, and zh statuses such as `已完成`, `执行失败`, `执行中`, `未开始`. Some product nouns remain English by current locale resource. | `13-zh-overview.png`, `.txt`; `16-zh-executions.png`, `.txt` |
| en locale | default / `ac.lang=en` | English pages render with expected labels. | PASS: all desktop and mobile English captures show English labels. | `01`, `05`, `14`, `15` evidence files |
| Responsive overview | viewport `390x844`, overview multi | Mobile shell usable, cards stack, no incoherent overlap in first viewport. | PASS: mobile screenshot shows top bar, action button, 24h window card, stacked metric cards, bottom nav. | `14-mobile-overview.png`, `.txt` |
| Responsive executions | viewport `390x844`, execution list multi | Mobile execution table remains usable with horizontal overflow. | PASS: first mobile viewport shows key row identity/status columns; explicit scroll check advanced `scrollLeft` to `572` of `scrollWidth 928`, making right-side columns visible. | `15-mobile-executions.png`; `17-mobile-executions-scroll.png`, `.json` |
| Filtered empty | `/executions?window=24h&agent_ref=agent%3Anobody&scenario=multi` | No-match filter keeps chip/context and shows filtered empty state. | PASS: visible `Agent: agent:nobody`, `Clear filters`, and `No executions match these filters`. | `18-filtered-empty.png`, `.txt`, `.url.txt` |

## Notes

- Browser console/page errors were empty after final harness correction: `browser-errors-final.txt`.
- The first attempted run captured a Vite overlay from an out-of-package temporary config; that failed harness path was removed and not used for the verdict.
- No remediation successor is required because the candidate passed the requested acceptance surface.
