# T1613 S2C-R3 Independent Acceptance Report

Verdict: **REJECT**

Date: 2026-08-27

## Candidate

- Remote ref checked: `origin-push/ac-exec/task-38ab4598/exec-ea18819f`
- Remote HEAD: `0f53752aea1061bdc4cc761a5e9fe5a9f11d0f53`
- `origin/main`: `b66fe30eb3c3d5bbcedda4ef711150d391f67b81`
- `origin-push/main`: `b66fe30eb3c3d5bbcedda4ef711150d391f67b81`
- Merge-base: `b66fe30eb3c3d5bbcedda4ef711150d391f67b81`
- Ancestry: `origin/main` is an ancestor of the candidate; candidate is not an ancestor of `origin/main`.

## Diff Review

Diff against `origin/main` modifies only:

- `internal/insight/service.go`
- `internal/insight/service_test.go`

The remediation adds `projectorFaultHook` to `Service`, defaulting to nil. The hook is reached through `runProjectorFaultHook`, which returns nil when unset, so production default is off.

The hook is non-inert in tests and is placed on the real projector transaction path for all three projectors:

- `projectQueue`: after fact/checkpoint writes and before `tx.Commit()`, then again after `tx.Commit()`.
- `projectActivity`: after fact/checkpoint writes and before `tx.Commit()`, then again after `tx.Commit()`.
- `projectSlots`: after fact/checkpoint writes and before `tx.Commit()`, then again after `tx.Commit()`.

## Required Crash Semantics

Command:

```bash
go test ./internal/insight -run 'TestInsightPreCommitCrashReplaysAndAppliesExactlyOnce|TestInsightPostCommitCrashRestartDoesNotDuplicate' -count=1 -v
```

Result: PASS

Evidence:

- `TestInsightPreCommitCrashReplaysAndAppliesExactlyOnce` passed.
- `TestInsightPostCommitCrashRestartDoesNotDuplicate` passed.

The pre-commit test verifies the injected error occurs before commit, no `projected_event`, checkpoint, or finished fact is committed for the stopped event, then replay with the hook disabled applies exactly one execution fact and exactly one projected stop event.

The post-commit test verifies the injected error occurs after commit, the fact/checkpoint/projected event are already present, and reopening the DuckDB file plus refreshing does not duplicate the execution or projected event.

## Regression Runs

Focused/full Insight:

```bash
go test ./internal/insight/... -count=1 -v
```

Result: PASS

Insight race:

```bash
go test -race ./internal/insight -count=10 -v
```

Result: PASS, final package line:

```text
ok  	github.com/oopslink/agent-center/internal/insight	388.992s
```

Repository race target:

```bash
make test-race
```

Result: PASS for configured `RACE_PKGS=./internal/agentruntime/...`.

## Frozen Harness

Frozen harness SHA required by the task:

```text
738bc0a6769b413dd4d04c6834207c62c2918fae
```

Ancestry check:

```text
git merge-base --is-ancestor 738bc0a6769b413dd4d04c6834207c62c2918fae 0f53752aea1061bdc4cc761a5e9fe5a9f11d0f53
```

Result: false (`exit 1`). The merge-base is `16a4120322f23007511d4609d0cb64d5982d0600`, so the frozen harness is not included in the candidate ancestry.

I then overlaid `internal/insight/service_test.go` from `738bc0a6769b413dd4d04c6834207c62c2918fae` onto a throwaway worktree at candidate `0f53752aea1061bdc4cc761a5e9fe5a9f11d0f53` and ran:

```bash
go test ./internal/insight -count=1 -v
```

Result: FAIL

Failing assertions:

```text
--- FAIL: TestInsightSlotObservation_AdmissionCapOnlyChangeClosesCapacityInterval
    service_test.go:264: slot 1 cap-boundary intervals = [{admissible:true from:2026-08-26 11:00:00+00 to:}], want admissible closed interval then inadmissible open interval
```

```text
--- FAIL: TestInsightSlotObservation_HeartbeatTTLExcludesUnknownTail
    service_test.go:302: slot coverage = 0.083333333, want 0.000694444; time after heartbeat TTL must be unknown
```

Passing frozen-harness tests in the same overlay run included:

- `TestInsightReplay_IdempotentLateEventsBoundariesQuantilesAndRebuild`
- `TestInsightCheckpointRestartDoesNotDuplicateFacts`
- `TestInsightSlotObservation_DuplicateHeartbeatAdmissionAndStaleGap`
- `TestInsightInvalidTimeOrder`
- `TestInsightExecutionsCursorDoesNotSkipLimitPlusOneRow`
- `TestInsightCrashRecoveryTransactionAndCheckpointRestart`

## Contract Decision

REJECT. Although the candidate proves the requested pre-commit and post-commit crash semantics through a real projector commit-path fault hook, it does not include the required frozen harness SHA in ancestry and fails the frozen harness when that harness is run against the candidate implementation. The task contract says any missing item is a structured REJECT.

