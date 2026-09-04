# issue-8d1a1443 Independent Review

Verdict: PASS

Reviewed candidate:

- SHA: `f2a3bf2d81437892b7e320f0a5736e5d798dc3cf`
- Remote ref: `ac-exec/task-95ff674d/exec-b42eb7bd`
- Remote readback: `git ls-remote origin ac-exec/task-95ff674d/exec-b42eb7bd` returned the same SHA.
- Review worktree: detached clean checkout at `/tmp/verify-f2a3bf2d-issue8.bb6m4H/wt`.

Scope and constraints:

- Implementation was not modified.
- No agent-center control-plane, database, socket, runtime config, token, or raw HTTP fallback was used.
- `task-input/v1/README.md` and `manifest.json` were read; no attachments were present.

Hard gates:

| Gate | Result | Evidence |
| --- | --- | --- |
| A dirty/undelivered executor must not report success then crash | PASS | `TestMonitor_Finalize_NonDeliveryGate`, `TestMonitor_Finalize_CommittedButUnpushed_Downgraded`, and `TestMonitor_Finalize_DirtyPushedHeadIsNonDeliveryWithPaths` passed. Dirty paths are captured in `FinalizedGitStatus.DirtyPaths`, result is `OutcomeNonDelivery`, retryable, and retained executor dir is asserted. |
| B local branch/system ref mismatch safe push | PASS | `TestMonitor_Finalize_EagerPush_UnexpectedLocalBranchPushesSafeExecutorRef`, `TestMonitor_Finalize_RecordedWorkspace_PushesActualHeadToSafeRef`, `TestMonitor_Finalize_RecordedWorkspace_RemoteBranchMismatchPushesSafeRef`, and `TestMonitor_Finalize_ExactOriginMainStillRefused` passed. HEAD is pushed to `ac-exec/<task>/<exec>`, local business branches are not created on origin, and protected `main` delivery is refused. |
| C terminal/stale executor and lease convergence | PASS | `TestResetTask_OwnerConfirmedDeadBypassesLiveLease`, `TestResetTask_StrangerConfirmedDeadStillRejectedOnLiveLease`, `TestResetTask_ConcurrentSingleIncrement`, `TestStuckNode_UpdatedAtStale_ReopenedWithoutLeaseLapse`, `TestStuckNode_LiveOwnerIdleWithoutExecutor_AutoReopenedDespiteLeaseRenewal`, and runtime recovery tests passed. `OutcomeNonDelivery` is included in `isDeath`, so dead non-delivery executor artifacts enter bounded recovery rather than terminal-crash confusion. |

Red-to-green evidence:

- Baseline worktree: `/tmp/verify-f2a3bf2d-baseline.t9eSZI/wt` at `1ba42c24ac613d23bcfa5a0c14ae1ca121032bed`.
- Only candidate test-file changes were applied to the baseline worktree, not implementation changes.
- Baseline focused tests were red at compile time:
  - `internal/agentruntime/executor`: `undefined: OutcomeNonDelivery`; `FinalizedGitStatus` missing `DirtyPaths`.
  - `internal/agentruntime/orchestrator`: `undefined: executor.OutcomeNonDelivery`.
  - `internal/agentruntime`: `undefined: executor.OutcomeNonDelivery`.
- Candidate focused and race runs were green, proving the candidate supplies the missing contract and behavior.

Candidate test commands:

- `go test ./internal/agentruntime/executor -run 'TestProbeGitStatus_DeliverySignals|TestMonitor_Finalize_(NonDeliveryGate|CommittedButUnpushed_Downgraded|DirtyPushedHeadIsNonDeliveryWithPaths|EagerPush|RecordedWorkspace|ExactOriginMainStillRefused|FailedRun|PushedDeliveryStaysSucceeded|WritebackErrorRetainsDir|RunningIsNoop|NoWritebackStillTearsDown)|TestMonitor_ReapFinalized|TestMonitor_Recover' -count=1 -v` PASS.
- `go test ./internal/agentruntime/orchestrator -run 'Test.*(NonDelivery|Delivery|Reset|Concurrent|Writeback|Judgment|Failure)' -count=1 -v` PASS.
- `go test ./internal/agentruntime -run 'Test.*(Reconcile|Recovery|NonDelivery|Reset|Lease|Activity|Executor)' -count=1 -v` PASS.
- `go test ./internal/projectmanager ./internal/projectmanager/service ./internal/admin/api ./internal/agentruntime -run 'Test(ResetToOpen|ResetTask|Complete_ZeroesRecoveryResetCount|BlockForResetExhaustion|Migration_0154|Migration_0155|StuckNode|EnactRecover|ReconcileOneExecutor|ClassifyExecutor|PlanExecutorReconcile|MapDomainError_TaskNonDelivery|CompleteTaskRejectsReportedZeroDelivery|ReportManualRecovery|TaskExecutionsProjectsRecoveryDiagnostics)' -count=1 -v` PASS.
- `go test -race ./internal/agentruntime/executor ./internal/agentruntime/orchestrator ./internal/projectmanager/service -run 'TestMonitor_Finalize|TestResetTask|TestStuckNode|TestReport_|TestValidCodeDelivery|TestReconcileOneExecutor|TestPlanExecutorReconcile' -count=1` PASS.

Full-suite result:

- `go test ./...` was not fully green because `internal/admin/api TestTaskInputPlan569_RealAdminHandlersEndToEnd` failed with `runtime stop: runtime lifecycle work: context deadline exceeded`.
- The same test failed the same way on the parent/main baseline, so this is classified as a pre-existing baseline/environment quality risk, not a candidate implementation failure for issue-8d1a1443.
- All packages shown by the full run other than `internal/admin/api` passed, including `internal/agentruntime`, `internal/agentruntime/executor`, `internal/agentruntime/orchestrator`, `internal/projectmanager`, `internal/projectmanager/service`, `internal/admin/api` focused tests, `internal/workerdaemon`, and `tests/e2e`.

Failure classification:

- Implementation failure: none found in the reviewed candidate for the three hard gates.
- Quality reject: not applied to this candidate; the only full-suite failure reproduces on the baseline.
- Non-delivery: not applicable to this review output; this report is committed on the executor branch and bound to the reviewed SHA above.
