# Independent Insight Semantics Acceptance

summary: REJECT
verdict: reject
reviewed_sha: 75427e3d3c9c09b3535379bde5e275da2af639cf
base_sha: f61c3110eb830f544b87b85d4d4d94e90633a0d5

## Source and Ref Verification

Evidence: `git-sha-ref-merge-base.log`

- `75427e3d3c9c09b3535379bde5e275da2af639cf` is present on remote ref `refs/heads/immutable/insight-rebuild-75427e3d`.
- `f61c3110eb830f544b87b85d4d4d94e90633a0d5` is present as remote `HEAD` and `refs/heads/main`.
- `git merge-base base reviewed` returned `f61c3110eb830f544b87b85d4d4d94e90633a0d5`.
- `git merge-base --is-ancestor base reviewed` returned exit `0`.

`task-input/v1` was not present in this workspace. Team rules named in the task could not be retrieved because this isolated executor has no center/MCP access by instruction; validation proceeded from the frozen contract already present in the repository.

## Frozen Contract Checked

Contract source: `docs/design/features/insight-phase-1-contract.md` at reviewed SHA.

Acceptance points covered:

- 24h fixed trailing half-open window.
- `completed_executions` counts terminal execution attempts, not Tasks.
- Failure rate is `failed/crashed/quiet_finalized / completed`, with `null` when denominator is zero.
- Queue wait and execution duration use continuous quantiles.
- Pending/running/spawned commands remain visible in drilldown and are excluded from percentile aggregates.
- Drilldowns are org-scoped and must support aggregate-to-TaskExecution reconciliation.
- Unknown/no-sample values must remain null/unknown, not coerced to zero.

## Durable Evidence Files

- Independent Go fixture/test source: `zz_independent_acceptance_test.go`.
- Full failing Go command output: `go-insight-tests-pipefail.log`.
- Earlier non-pipefail run retained for audit: `go-insight-tests.log`.
- Frontend install output: `web-pnpm-install.log`.
- Frontend targeted test output: `web-insight-vitest.log`.
- Frontend explicit tsc output: `web-typecheck.log`.
- Frontend build output: `web-build.log`.

## Passing Evidence

The independent fixture created two org-1 execution attempts on one task plus one org-2 execution attempt. Actual API and raw projection values are in `go-insight-tests-pipefail.log`.

Observed org-1 API summary:

- `completed_executions=2`
- `failed_executions=1`
- `failure_rate=0.5`
- `queue_wait_ms={p50:20,p95:29,samples:2}` from samples `[10,30]`
- `execution_duration_ms={p50:150,p95:195,samples:2}` from samples `[100,200]`

Observed raw `execution_fact` projection:

- org-1: `exec-o1-a`, `exec-o1-b`
- org-2: `exec-o2-a`

Observed org isolation:

- org-1 execution list contained only `project-1` rows.
- org-1 detail lookup for org-2 `exec-o2-a` returned `ErrExecutionNotFound`.

Observed zero/unknown API summary in an empty org:

- `completed_executions=0`
- `failed_executions=0`
- `failure_rate=null`
- queue and duration percentiles `p50=null,p95=null,samples=0`
- `slot_utilization=null`
- `slot_coverage_ratio=null`

Frontend commands:

- `cd web && pnpm install --frozen-lockfile`: pass.
- `cd web && pnpm vitest run src/pages/InsightOverview.test.tsx`: pass, 6 tests.
- `cd web && pnpm typecheck`: pass.
- `cd web && pnpm build`: pass; Vite emitted existing CSS/chunk warnings but exited 0.

## Reject Evidence

Failing command:

```sh
set -o pipefail; go test -v ./internal/insight -run 'TestIndependentAcceptance|TestInsight' 2>&1 | tee docs/acceptance/t1745-insight-independent/go-insight-tests-pipefail.log
```

Exit code: `1`.

Failing assertion:

```text
pre-start queue command with execution_id is missing from drilldown:
api_executions=null
raw_queue=[{"agent_ref":"agent:agent-1","command_id":"cmd-prestart","command_status":"pending","execution_id":"exec-prestart","organization_id":"org-1","project_id":"project-1","quality":"valid","queued_at":"2026-08-26 11:50:00+00","started_at":null,"task_id":"task-1"}]
raw_execution_fact=null
want command:cmd-prestart row
```

Why this rejects the candidate:

- The frozen contract says pre-start commands are visible in drill-down but excluded from percentiles, and that `command:<id>` remains the API compatibility presentation before a real executor start.
- Candidate `projectQueue` writes the queue event into `queue_interval_fact`, but if `worker_control_events.execution_id` is non-empty and no `execution_fact` row exists yet, it only executes an `UPDATE execution_fact ... WHERE execution_id = ?`.
- That update affects no rows. It does not insert a pseudo `command:<id>` row.
- `Executions` reads only `execution_fact`, so the pending pre-start command disappears from object drilldown even though the durable queue fact exists.

Relevant candidate code:

- `internal/insight/service.go:512` to `520`: non-empty `execID` path only updates `execution_fact`.
- `internal/insight/service.go:521` to `537`: pseudo `command:<id>` insert exists only when `execID == ""`.
- `internal/insight/service.go:212` to `219`: execution list reads from `execution_fact` only.

## Remediation

Change `internal/insight/service.go` in `projectQueue`:

1. For any queue command without a real `executor.start` fact, insert or upsert a pseudo `execution_fact` row with `execution_id='command:' || command_id`, even when `worker_control_events.execution_id` is non-empty.
2. Preserve the real `execution_id` in `queue_interval_fact.execution_id` for later linkage.
3. When a real `executor.start` event arrives, ensure the real execution row is created/updated from `queue_interval_fact`, and either remove/replace the pseudo row or make `Executions` suppress the pseudo row once the real execution row is present.
4. Keep aggregates unchanged: pseudo/pre-start rows must have `started_at=null`, `finished_at=null`, `outcome=null`, and must not contribute to completed counts or percentile samples.
5. Add a production-chain test equivalent to `TestIndependentAcceptance_PreStartQueueWithExecutionIDRemainsDrilldownVisible`.

Retest commands and expected assertions:

```sh
set -o pipefail; go test -v ./internal/insight -run 'TestIndependentAcceptance_PreStartQueueWithExecutionIDRemainsDrilldownVisible|TestInsight' 2>&1 | tee /tmp/insight-retest.log
```

Expected:

- command exits `0`;
- pre-start API executions include one row with `execution_id="command:cmd-prestart"`;
- row has `command_id="cmd-prestart"`, `started_at=null`, `outcome=null`, `command_status="pending"`;
- summary remains `completed_executions=0`, `queue_wait_ms.samples=0`.

```sh
cd web && pnpm vitest run src/pages/InsightOverview.test.tsx
cd web && pnpm typecheck
cd web && pnpm build
```

Expected: all exit `0`.
