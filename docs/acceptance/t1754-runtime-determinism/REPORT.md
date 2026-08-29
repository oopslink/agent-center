# T1754 runtime terminal determinism acceptance

Verdict: PASS

Scope: evidence-only validation on exact post-ship `origin/main` commit `5a18901eaea33c48247e2e8847a29f1d66038d40`.

## Preconditions

- The expected `task-input/v1` package was not present in this isolated workspace; this is recorded in `raw/00-fresh-origin-main.log`.
- Initial checkout was stale (`f61c3110eb830f544b87b85d4d4d94e90633a0d5`). The run was fail-closed until `git ls-remote origin refs/heads/main` and `refs/remotes/origin/main` both resolved to `5a18901eaea33c48247e2e8847a29f1d66038d40`; see `raw/02-fetch-switch-exact-origin-main.log`.
- Fresh binary was built from that exact SHA. `bin/agent-center version` reported `agent-center t1754-origin-main-5a18901e (commit 5a18901e)`; binary SHA256 was `23dd138f59b158730320a885167b8e74a37289d974816c06fd390e17f0716232`; see `raw/03-build-fresh-origin-main.log`.

## Evidence Matrix

| Gate | Evidence | Outcome |
| --- | --- | --- |
| Real deployed runtime, not worker-daemon-only | `raw/04-deployed-smoke.log` ran `scripts/smoke/deploy-smoke.sh`: real server + worker run + agent-runtime pipeline and runtime-version assertions | PASS |
| Runtime process version and environment reachability | `raw/04-deployed-smoke.log` includes `/system/version`, worker info, agent health identity assertions via `TestDeployedBinaryRuntimeVersion*` | PASS |
| Restart/replay/crash recovery | `raw/05-real-runtime-restart-replay-fork.log` proves full-host relaunch+resume+directed-message reinjection and worker-only survivor re-adoption | PASS |
| Duplicate event / replay idempotency | `raw/06-pm-terminal-frontier-history-slo.log`, `raw/07-migration-outbox-insight-crash.log`, `raw/08-agentruntime-recovery-crash-duplicates.log` include orchestrator replay, outbox relay, insight replay, executor duplicate/finalize tests | PASS |
| Terminal determinism and empty frontier | `raw/06-pm-terminal-frontier-history-slo.log` includes terminal monotonic tests, `TestDeriveFrontier_Empty`, completion gates, and runnable-stage fail-closed tests | PASS |
| Historical migration | `raw/07-migration-outbox-insight-crash.log` includes full persistence migration round-trip and historical cleanup/backfill tests | PASS |
| Superseded/discarded history isolation | `raw/06-pm-terminal-frontier-history-slo.log` includes superseded historical-only plan view and discarded terminal generation tests | PASS |
| Outbox/crash points | `raw/07-migration-outbox-insight-crash.log` includes outbox backlog/relay idempotency plus insight pre-commit and post-commit crash exactly-once tests | PASS |
| SLO/race | `raw/06-pm-terminal-frontier-history-slo.log` includes progress-control hold/wake SLO tests; `raw/09-agentruntime-race-slo.log` passed `go test -race -count=10 ./internal/agentruntime/...` | PASS |

## Raw Command Results

- `raw/04-deployed-smoke.log`: `smoke pass: 67 seconds`
- `raw/05-real-runtime-restart-replay-fork.log`: `ok github.com/oopslink/agent-center/tests/e2e 28.113s`
- `raw/06-pm-terminal-frontier-history-slo.log`: projectmanager targeted suites passed
- `raw/07-migration-outbox-insight-crash.log`: persistence/outbox/insight suites passed
- `raw/08-agentruntime-recovery-crash-duplicates.log`: agentruntime recovery suites passed
- `raw/09-agentruntime-race-slo.log`: race suites passed, slowest package `internal/agentruntime/executor` at `200.654s`

## Notes

The deployed smoke script rebuilt the binary during the run with branch field `HEAD` because the worktree was detached at the exact `origin/main` SHA. The asserted version and commit remained `t1754-origin-main-5a18901e` / `5a18901e`, and the runtime-version spec passed against the running server, worker, and agent-runtime processes.
