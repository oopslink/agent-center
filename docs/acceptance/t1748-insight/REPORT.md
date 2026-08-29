# T1748 Insight TaskExecution Drilldown Browser Acceptance

## Verdict

`REJECT`

Blocking reason: the reviewed immutable candidate is not cleanly based on current `origin-push/main`.

- Reviewed SHA: `bda5d14adbe874ef829cc87085a65e84e51536e9`
- Immutable ref: `refs/heads/immutable/t1740-insight-drilldown-bda5d14a`
- Upstream execution ref: `refs/heads/ac-exec/task-2f50961b/exec-0ffcfff7`
- Both refs resolved remotely to the reviewed SHA.
- Current `origin-push/main`: `5a18901eaea33c48247e2e8847a29f1d66038d40`
- Merge base against current main: `f61c3110eb830f544b87b85d4d4d94e90633a0d5`
- `origin-push/main...candidate`: `behind=4`, `ahead=2`
- Candidate parent: `75427e3d3c9c09b3535379bde5e275da2af639cf`, the previously rejected SHA named in the task.

## Scope

Independent fail-closed browser acceptance was run from detached candidate worktree:

`/tmp/t1748-insight-candidate-bda5d14a.9LQyQE/candidate`

The app was served from the exact candidate via Vite at `http://127.0.0.1:5174/`. The API was an isolated deterministic mock on `127.0.0.1:17100`; no agent-center database, socket, token, MCP, admin endpoint, or origin/main mutation was used. The requested `task-input/v1` package was not present in this executor worktree.

## Browser Gates

| Gate | Result | Evidence |
| --- | --- | --- |
| Global Execution details stable load | PASS | `01-global-overview.png`, `02-global-executions.png`, `03-global-detail.png` |
| Agent/Builder entry drills down without cross-scope leakage | PASS | `04-agent-builder-clickthrough.png`; URL preserved `agent_ref=agent%3Abuilder`; Observer row absent |
| Project scope entry drills down without cross-scope leakage | PASS | `05-project-checkout-clickthrough.png`; URL preserved `project_id=proj-checkout`; Ops project row absent |
| Selected row ID/status/time matches detail | PASS | Row `exec-global-001` displayed Failed, queued `11:01:00 PM`, started `11:02:22 PM`, finished `11:09:42 PM`; detail displayed the same ID/status/timeline |
| Detail facts reconcile to identical window/scope | PASS | API log shows `exec-global-001` detail window `2026-08-28T16:00:00Z` to `2026-08-29T16:00:00Z` and row facts matching the list |
| Empty state explicit | PASS | `06-empty-overview.png`, `07-empty-filtered.png`; no false populated state |
| Permission state explicit and not `network_error` | PASS | `09-permission-overview.png`; displays permission copy and `[403 permission_denied] missing org.analytics.read` |
| API unavailable state explicit and not `network_error` | PASS with note | `08-unavailable-overview.png`; displays `[503 insight_unavailable] Insight projector unavailable...`; not `[0 network_error] Failed to fetch`. UI uses generic "Insight overview failed" instead of the available localized "Insight is unavailable" because the degraded envelope has empty `refreshed_at`. |

## Reconciliation Notes

- Normal global list API returned three rows: `exec-global-001`, `exec-global-002`, `exec-global-003`.
- Builder scoped API returned two rows, both `agent_ref=agent:builder`; no `agent:observer`.
- Project scoped API returned two rows, both `project_id=proj-checkout`; no `proj-ops`.
- Detail API for `exec-global-001` returned the same execution object rendered from the selected global list row.
- Empty and denied paths produced explicit UI states after React Query retry settling.

## Commands

- `git ls-remote origin-push refs/heads/immutable/t1740-insight-drilldown-bda5d14a refs/heads/ac-exec/task-2f50961b/exec-0ffcfff7 refs/heads/main`
- `git worktree add --detach /tmp/t1748-insight-candidate-bda5d14a.9LQyQE/candidate bda5d14adbe874ef829cc87085a65e84e51536e9`
- `pnpm install --frozen-lockfile` in candidate `web/`
- `node docs/acceptance/t1748-insight/mock-api.mjs`
- `pnpm exec vite --host 127.0.0.1 --config ../docs/acceptance/t1722-insight/vite.config.mjs`
- `agent-browser open`, `snapshot`, `click`, `screenshot`, and `get text` for all browser evidence
- `pnpm exec vitest run src/pages/InsightOverview.test.tsx` passed: 7 tests

## Evidence Files

- Screenshots: `01-global-overview.png` through `09-permission-overview.png`, plus click-through scoped screenshots.
- Raw text/API evidence: `raw/*.txt`, `raw/api-observations.jsonl`
- Test output: `raw/10-vitest-insight.log`
- Candidate status: `raw/11-candidate-status.txt`
- Hashes: `raw/SHA256SUMS`
