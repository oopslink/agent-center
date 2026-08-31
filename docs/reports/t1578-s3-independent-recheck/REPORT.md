# T1578 S3 Insight Phase 1 independent production recheck

Date: 2026-08-31 UTC
Executor branch: `ac-exec/task-7b2039ec/exec-8f47c3db`
Evidence root: `docs/reports/t1578-s3-independent-recheck/evidence/`

## Formal Verdict

**REJECT for formal acceptance packaging**: the required `task-input/v1/README.md`
and `task-input/v1/manifest.json` package is absent from this isolated workspace.
Fresh terminal evidence is in `terminal.log` command `task_input_find`:
`find . -maxdepth 4 -path '*task-input*' -print` returned rc=0 with no paths.

**Candidate technical recheck result**: the independently re-run provenance,
runtime identity/health, and Insight hard-red probes below all passed on the
remote S3 candidate SHA.

## Provenance

Fresh commands:

- `terminal.log` / `remote_refs`:
  - `refs/heads/s3/insight-phase1-candidate-20260827` =
    `16dc58155dfa0cafd79c08595c8a8e378b5eede9`
  - `refs/heads/ac-exec/task-5493aaeb/exec-9bd82743` =
    `16dc58155dfa0cafd79c08595c8a8e378b5eede9`
  - historical input `refs/heads/ac-exec/task-eebbbe21/exec-26365c40` =
    `9419d5da2e21c9b9e15183efe33c869ef2413ccc`
  - `refs/heads/main` =
    `3b2b45f480c297f44b0e2deb877ebc6cdad7f5f5`
- `candidate_head`: candidate worktree HEAD =
  `16dc58155dfa0cafd79c08595c8a8e378b5eede9`, commit
  `docs(insight): converge S3 candidate identity`.
- Ancestry probes all rc=0:
  - `16a4120322f23007511d4609d0cb64d5982d0600` direct ancestor.
  - `b9e25a6381b55a687b0d894a1f56fefc8ccbc5e0` direct ancestor.
  - `738bc0a6769b413dd4d04c6834207c62c2918fae` direct ancestor.
  - `968dd76157d4b79755cc59a23163bbffbb1e5dc7` direct ancestor.

Conclusion: **PASS** for candidate ref convergence and upstream provenance.

## Binary And Runtime Identity / Health

Fresh commands:

- `build_backend`: `make build-backend`, rc=0.
- `binary_sha`: `shasum -a 256 ./bin/agent-center` =
  `4b6c69aec1f283941f9123726bca48c8f293f25cb3b9c144ef232b893cee75f3`.
- `binary-version-subcommand.log`: `./bin/agent-center version` returned
  `agent-center HEAD-16dc5815 (commit 16dc5815)`.
- `runtime-probe.log`:
  - server started as PID `49933` with
    `./bin/agent-center server -config /tmp/insight-runtime-probe.alNhkS/config.yaml`;
  - `/api/health` returned HTTP 200 with
    `{"status":"ok","version":"HEAD-16dc5815"}`;
  - `/api/system/version` returned HTTP 200 with `commit:"16dc5815"`,
    `branch:"HEAD"`, `version:"HEAD-16dc5815"`, `pid:49933`,
    `install_path` pointing at the candidate worktree binary.

Conclusion: **PASS** for actual running candidate identity and public health.

## Insight Hard Reds

### HR-1 Production Source Chain And API Read Separation

Expected: projection consumes production activity/control/slot sources; HTTP reads
must not mutate/project; invalid windows fail closed; org scope must not leak.

Fresh evidence:

- `insight_api`: `go test -v ./internal/webconsole/api -run TestInsights -count=1`, rc=0.
- Passed tests include:
  - `TestInsightsOverviewAPI_WindowValidationAndShape`
  - `TestInsightsHTTPReadDoesNotTriggerProjection`
  - `TestInsightsExecutionAPI_ReadsSingleProjectedExecution`
  - `TestInsightsExecutionAPI_ForeignOrgExecutionIsNotFound`

Conclusion: **PASS**.

### HR-2 Frozen 24h Formulas, Boundaries, Quantiles, Replay/Rebuild

Expected: completed executions are execution attempts, failure rate follows frozen
outcome semantics, queue/duration percentiles use continuous quantiles, 24h
window is half-open, late arrivals and rebuild preserve facts.

Fresh evidence:

- `insight_unit`: `go test -v ./internal/insight -count=1`, rc=0.
- Relevant passed tests:
  - `TestInsightReplay_IdempotentLateEventsBoundariesQuantilesAndRebuild`
  - `TestInsightInvalidTimeOrder`
  - `TestInsightExecutionsCursorDoesNotSkipLimitPlusOneRow`

Conclusion: **PASS**.

### HR-3 Slot Observation Durability, Capacity, TTL Coverage

Expected: slot observations become non-overlapping intervals, duplicate
heartbeats coalesce safely, admission cap and draining semantics affect
denominator, heartbeat TTL excludes unknown tail and exposes coverage.

Fresh evidence:

- `insight_unit`, rc=0.
- Relevant passed tests:
  - `TestInsightSlotObservation_DuplicateHeartbeatAdmissionAndStaleGap`
  - `TestInsightSlotObservation_AdmissionCapOnlyChangeClosesCapacityInterval`
  - `TestInsightSlotObservation_HeartbeatTTLExcludesUnknownTail`

Conclusion: **PASS**.

### HR-4 Projector Transaction Idempotency And Crash Recovery

Expected: `projected_event`, fact writes, and checkpoint advancement are atomic;
pre-commit crash replays exactly once; post-commit crash restarts without
duplicate facts.

Fresh evidence:

- `insight_unit`, rc=0.
- `insight_race`: `go test -race -p 1 ./internal/insight -count=3`, rc=0.
- Relevant passed tests:
  - `TestInsightPreCommitCrashReplaysAndAppliesExactlyOnce`
  - `TestInsightPostCommitCrashRestartDoesNotDuplicate`

Conclusion: **PASS**.

## UI Contract Probe

Fresh evidence:

- `insight_ui`: `pnpm exec vitest run src/pages/InsightOverview.test.tsx`, rc=0,
  11/11 tests passed.
- `insight_tsc`: `pnpm exec tsc -b --force`, rc=0.

Covered UI assertions include fixed `window=24h`, overview summary cards,
coverage states, drilldown filters/cursor behavior, user-facing enum mapping,
execution detail/not-found states, rebuilding/auth states, and Chinese copy.

Conclusion: **PASS**.

## Evidence Files

- `evidence/terminal.log`: raw terminal output for provenance/build/test probes.
- `evidence/verdict.jsonl`: structured command id/start/end/rc for main probes.
- `evidence/runtime-probe.log`: raw terminal output for running binary health and
  identity probes.
- `evidence/runtime-probe.jsonl`: structured command id/start/end/rc for runtime
  probes.
- `evidence/runtime-server.log`: server startup log.
- `evidence/binary-version-subcommand.log`: direct binary version subcommand.

## Residual Risk

Because `task-input/v1` was absent, this recheck could not verify attachment
hashes, MIME metadata, or any Supervisor-materialized task contract beyond the
text available in the prompt and repository. Under the task's "missing evidence
=> REJECT" rule, that is enough to reject formal acceptance packaging even though
the candidate's independently executable technical checks passed.
