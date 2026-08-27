# S6 Final Main Verification

Verdict: REJECT

Reason: the required main SHA is reachable and the high-signal tests pass, but the real built runtime API/UI reports `freshness.state:"unavailable"` for a just-refreshed Insight read model. The task contract explicitly requires fresh/stale correctness, so this is a gate failure.

## Main Lock

- Required SHA: `16dc58155dfa0cafd79c08595c8a8e378b5eede9`.
- `git ls-remote origin refs/heads/main`: `16dc58155dfa0cafd79c08595c8a8e378b5eede9`.
- Authoritative candidate alias `refs/heads/ac-exec/task-5493aaeb/exec-9bd82743`: same SHA.
- Authoritative S3 ref `refs/heads/s3/insight-phase1-candidate-20260827`: same SHA.
- S4 reviewed evidence SHA `9419d5da2e21c9b9e15183efe33c869ef2413ccc` is an ancestor of main (`merge-base --is-ancestor` exit `0`).
- Main tree: `880d11a0013768c05662965f94b96b25b17e79f6`.

Raw log: `raw/git-reachability.log`.

## Fresh Checkout And Tests

Fresh detached worktree: `/tmp/s6-main-16dc5815-20260827200216` at exact main SHA.

- `make build`: PASS; built `agent-center HEAD-16dc5815`.
- `go test ./internal/insight -count=1`: PASS.
- `go test -race -p 1 ./internal/insight -count=1`: PASS.
- `go test ./internal/webconsole/api -run 'TestInsights' -count=1`: PASS.
- `go test ./internal/insight -run 'TestInsightReplay|TestInsightSlotObservation|TestInsightInvalidTimeOrder|TestInsightPreCommitCrash|TestInsightPostCommitCrash' -count=1`: PASS.
- `pnpm exec vitest run src/pages/InsightOverview.test.tsx`: PASS, 11/11 tests.

Raw logs are in `raw/`.

## Runtime/API/UI Evidence

Built binary runtime:

- Server: `bin/agent-center server --config /tmp/s6-runtime-20260827200548/config.yaml`.
- Health/version: `{"status":"ok","version":"HEAD-16dc5815"}`.
- Web session org: `organization-35e007ca`, slug `org-4ba572b5`.
- Seeded source facts: `8` `agent_activity_events`, `4` `worker_control_events`, `4` `agent_concurrency_observations`.

Real API values from `/api/orgs/org-4ba572b5/insights/overview?window=24h`:

- Window duration: `24h`.
- Completed executions: `4`.
- Failed executions: `2`.
- Failure rate: `0.5`.
- Slot utilization: `0.3333333333333333`.
- Slot coverage ratio: `0.0020833333333333333`.
- Queue wait p50/p95: `25` / `98` ms.
- Execution duration p50/p95: `2500` / `3850` ms.
- Agent leaderboards: `S6 Agent 1`, `S6 Agent 2`.
- Project leaderboards: `S6 Project 1`, `S6 Project 2`.
- TaskExecution detail route for `s6-exec-a`: present, `quality:"valid"`, queue `10` ms, duration `1000` ms.

Gate failure:

- Overview `freshness.state`: `unavailable`.
- Detail `freshness.state`: `unavailable`.
- `refreshed_at` example: `2026-08-27 12:06:18.377479+00`.
- The UI renders the same failed badge text: `unavailable 0 ms / 120 s`.

Screenshots:

- `screenshots/insight-overview.png`
- `screenshots/insight-drilldown.png`
- `screenshots/insight-detail.png`

DOM/text/browser diagnostics:

- `raw/insight-overview.snapshot.txt`
- `raw/insight-overview.text.txt`
- `raw/insight-drilldown.snapshot.txt`
- `raw/insight-detail.snapshot.txt`
- `raw/browser-console.txt` empty.
- `raw/browser-errors.txt` empty.
- `raw/browser-network.txt` reports no retained requests from agent-browser.

## DuckDB Rebuild

Moved `insight.duckdb` and WAL aside, restarted the same built binary, and let the projector recreate the read model.

- Source counts before/after rebuild remained `8` / `4` / `4`.
- Rebuilt overview metrics remained identical.
- Freshness failure reproduced after rebuild as `freshness.state:"unavailable"`.

Raw evidence:

- `raw/runtime-overview.json`
- `raw/runtime-executions.json`
- `raw/runtime-detail.json`
- `raw/runtime-overview-after-rebuild.json`
- `raw/rebuild-source-counts.log`
- `raw/runtime-seed.sql`

## Notes

The packaged `task-input/v1/README.md` and `task-input/v1/manifest.json` were absent from this executor workspace, so no task-input attachments were available to inspect.
