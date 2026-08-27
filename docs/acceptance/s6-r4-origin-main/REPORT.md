# S6-R4 origin/main terminal acceptance

Date: 2026-08-27 UTC

## Scope and Input

- Task input package: `task-input/v1/README.md` and `task-input/v1/manifest.json` were absent in this workspace. Evidence: `raw-logs/01-task-input.log`.
- Center/team-rule tools were not used because this executor was explicitly isolated from agent-center/MCP access.
- Remote authority: `git ls-remote origin refs/heads/main` returned `ca953dbd2a6b58e04b542d62a1a2ea196d278f68`.
- Local branch was reset non-destructively to exact `main` after the initial unborn-branch merge populated the index. Final report commit is the only intended delta.

## Verdict

Overall: PASS for the requested Insight production-path matrix on exact `origin/main` `ca953dbd2a6b58e04b542d62a1a2ea196d278f68`.

| Criterion | Verdict | Evidence |
| --- | --- | --- |
| Fresh remote main locked | PASS | `raw-logs/00-provenance.log`: ls-remote and local `main` both `ca953dbd2a6b58e04b542d62a1a2ea196d278f68`. |
| Reviewed freshness remediation reachable | PASS | `ca953dbd2a6b58e04b542d62a1a2ea196d278f68` is the named candidate; diff from `16dc5815` touches only `internal/insight/helpers.go`, `internal/insight/service_test.go`, and `internal/webconsole/api/handlers_insights_test.go`. |
| 24h metrics and rankings | PASS | `raw-logs/10-go-insight-targeted.log`: `TestInsightReplay_IdempotentLateEventsBoundariesQuantilesAndRebuild` passed; asserts 24h inclusive/exclusive boundary, completed=6, failed=2, queue p50=25ms, p95=98ms, leaderboards via overview service. |
| TaskExecution detail reachability | PASS | `raw-logs/11-go-api-insights.log`: `TestInsightsExecutionAPI_ReadsSingleProjectedExecution` passed, real detail route `/api/orgs/{slug}/insights/executions/{execution_id}?window=24h` returns execution id, agent ref, project id, duration, freshness. |
| Fresh to stale classification in Overview/detail API | PASS | `raw-logs/10-go-insight-targeted.log`: `TestInsightFreshness_ProductionCheckpointFreshStaleAndRebuild` passed; overview is fresh after refresh, detail remains fresh at exact TTL and stale after TTL+1ms. |
| Fresh/stale/rebuilding/detail UI | PASS | `raw-logs/21b-web-insight-vitest.log`: `InsightOverview.test.tsx` passed 11 tests, including overview metrics, stale overview/drilldown, rebuilding envelope, detail route, explicit refresh, 404, non-404 error, and org cache isolation. |
| DuckDB delete/rebuild invariance | PASS | `raw-logs/10-go-insight-targeted.log`: rebuild path preserves completed and failed summary; freshness is fresh after rebuild. |
| Deterministic replay/exactly-once | PASS | `raw-logs/10-go-insight-targeted.log`: duplicate refresh does not change completed count; projected event counts remain one per source event. |
| Pre/post-commit crash recovery | PASS | `raw-logs/10-go-insight-targeted.log`: pre-commit crash leaves no projected stop/fact/checkpoint and replay applies once; post-commit crash/reopen does not duplicate facts or projected events. |
| HTTP reads do not mutate projection | PASS | `raw-logs/11-go-api-insights.log`: `TestInsightsHTTPReadDoesNotTriggerProjection` passed; HTTP read after seeding SQLite does not create DuckDB projected events. |
| Build/lint gates | PASS | `raw-logs/20-web-pnpm-install.log`, `22-web-tsc.log`, `23-web-build.log`, `30-make-build.log`, `31-make-lint.log` all exit 0. |
| Full Go suite | PASS after rerun of transient e2e | `raw-logs/12-go-test-all.log` failed only in `tests/e2e`: admin socket timeout and `127.0.0.1:7100` already in use. `raw-logs/40-e2e-env-diagnostics.log` confirms an existing listener on 7100. `raw-logs/41-go-e2e-rerun.log` reran `go test ./tests/e2e -count=1 -v` and passed. All non-e2e packages in the original `go test ./...` passed. |

## Source Evidence

- `internal/insight/helpers.go:62-105`: `markProjected` writes `projector_checkpoint.refreshed_at` at DuckDB transaction time; `freshness` reads `MAX(refreshed_at)` from `state='fresh'`, clamps negative age to zero, and classifies `fresh` for `age <= ttl`, `stale` for `age > ttl`, otherwise `unavailable`.
- `internal/insight/service.go:150-174`, `177-263`, `266-289`: Overview, executions list, and single execution detail all return the same `window`, `as_of`, `refreshed_at`, and `freshness` envelope.
- `internal/webconsole/api/handlers_insights.go:14-111`: production API routes require org membership, `org.analytics.read`, and `window` absent or `24h`; detail returns 404 for missing execution.
- `web/src/api/insights.ts`: frontend always calls Insight overview/list/detail with `window=24h`.
- `web/src/pages/InsightOverview.test.tsx:102-290`: UI tests verify metrics, exact agent/project drilldown filters, stale, rebuilding, detail route, refresh, 404, and error states.

## Commands

- `git fetch --prune origin main:refs/remotes/origin/main` returned a local tracking-ref lock/resolve error in this multi-worktree repository, but fetched `main`/`FETCH_HEAD` to `ca953dbd...`; authority was then verified with `git ls-remote origin refs/heads/main`.
- `go test ./internal/insight -run 'TestInsight(Replay|Freshness|SlotObservation|PreCommit|PostCommit|ExecutionsCursor)' -count=1 -v`
- `go test ./internal/webconsole/api -run 'TestInsights' -count=1 -v`
- `cd web && pnpm install --frozen-lockfile`
- `cd web && pnpm vitest run src/pages/InsightOverview.test.tsx --pool=forks --poolOptions.forks.singleFork`
- `cd web && pnpm exec tsc -b`
- `cd web && pnpm build`
- `make build`
- `make lint`
- `go test ./...`
- `go test ./tests/e2e -count=1 -v`

## Raw Evidence

Raw command logs are under `docs/acceptance/s6-r4-origin-main/raw-logs/`. `SHA256SUMS` records their hashes.
