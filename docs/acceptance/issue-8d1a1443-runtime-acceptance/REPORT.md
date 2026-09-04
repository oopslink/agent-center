# issue-8d1a1443 Runtime Acceptance

Verdict: PASS

Scope: evidence-only runtime acceptance from the execution-time latest `origin/main`.
No production center, production worker, production agent-runtime, production DB, production
runtime state, production cookies, or production refs were reused or restarted.

## Target

- Execution time: 2026-09-04T06:28:00Z
- Latest `origin/main`: `226de72569b18913ccb09119922a6feed9ad174a`
- Required candidate: `f2a3bf2d81437892b7e320f0a5736e5d798dc3cf`
- Ancestry proof: `git merge-base --is-ancestor f2a3bf2d81437892b7e320f0a5736e5d798dc3cf refs/remotes/origin/main` returned `0`
- Executor delivery ref used for this evidence: `ac-exec/task-0e398848/exec-c6d16f2d`
- Source diff: evidence-only docs/logs; no production source changes.

Raw proof:

- `raw/00-main-lock.txt`
- `raw/01-remote-refs-before.txt`
- `raw/70-remote-refs-after-before-delivery-push.txt`
- `raw/71-remote-refs-diff-before-delivery-push.txt`
- `raw/80-evidence-push.log`
- `raw/81-evidence-push-origin-push.log`
- `raw/82-post-push-readback.txt`
- `raw/83-remote-refs-after-evidence-push.txt`
- `raw/84-remote-refs-diff-after-evidence-push.txt`

Delivery note: the first push attempt through `origin` failed because that remote is
configured as `mirror=true` and Git rejects mirror push with an explicit refspec. The
evidence was then pushed through the non-mirror `origin-push` remote to the same safe
executor ref only.

## Isolated Instance Provenance

Runtime instance purpose: test.

Deployment-level restart/recovery e2e:

- Command: `go test ./tests/e2e -run TestE2E_RestartRecovery_DeployLevel -count=1 -v`
- Exit code: `0`
- Sandbox root: `/tmp/p46-restart-recovery-1062708250`
- Center binary: freshly built `agent-center` from current main via the e2e harness
- Center config: `/tmp/p46-restart-recovery-1062708250/config.yaml`
- Center DB: `/tmp/p46-restart-recovery-1062708250/agent-center.db`
- Center admin socket: `/tmp/p46-restart-recovery-1062708250/admin.sock`
- Center listen address: `127.0.0.1:0` (ephemeral test port)
- Master key: `/tmp/p46-restart-recovery-1062708250/master.key`
- Bootstrap token file: `/tmp/p46-restart-recovery-1062708250/bootstrap_token`
- Worker identity: `w-p46`
- Agent/runtime identity: `agent-p46-0001`
- Organization/test data: `organization-p46aa001`, `conv-p46-dm-0001`, `msg-p46-0000000001`
- Agent session file: `/tmp/p46-restart-recovery-1062708250/agents/agent-p46-0001/session.instance`
- Fake claude log: `/tmp/p46-restart-recovery-1062708250/fakeclaude.log`
- Start mode: real `agent-center server`, real `agent-center worker run`, fake `claude` first on `PATH`
- Restart mode: `SIGKILL` worker before reply, reap child stand-ins, restart worker with same isolated config/home.

Deployment-level runtime/executor e2e:

- Command: `go test ./tests/e2e -run TestForkExecutorDeployedBinary_AlreadyRunningTask -count=1 -v`
- Exit code: `0`
- Boundary: real `agent-center worker agent-runtime`, Unix control socket, `agent.fork_executor`, real `agent-center worker executor`, real `/usr/bin/true` runner, durable `output.json`
- Test center boundary: fake HTTP center over a per-test Unix socket, with isolated token `test-worker-token`
- Worker identity: `worker-deployed-fork`
- Agent/runtime identity: `agent-deployed-fork`
- Task/test data: `task-deployed-running`

Raw proof:

- `raw/30-deployed-runtime-executor-e2e.log`
- `raw/40-deployed-restart-recovery-e2e.log`

## Acceptance A: Dirty Finalization

Verdict: PASS

Evidence:

- `TestMonitor_Finalize_NonDeliveryGate` PASS
- `TestMonitor_Finalize_CommittedButUnpushed_Downgraded` PASS
- `TestMonitor_Finalize_DirtyPushedHeadIsNonDeliveryWithPaths` PASS
- `TestReport_CodeExitZeroWithoutDurableDeliveryIsNonDelivery` PASS

Key observed behavior:

- A success-shaped executor result without durable clean delivery is downgraded before writeback.
- Dirty pushed HEAD is structured `non_delivery`, retryable, with dirty paths captured.
- Retained worktree is asserted for dirty/non-delivery recovery.
- Code exit 0 without durable delivery is judged `non_delivery`, not `success -> crashed`.

Raw proof:

- `raw/10-ab-finalize-tests.log`
- `raw/50-negative-adjacent-classification.log`

## Acceptance B: Safe Ref

Verdict: PASS

Evidence:

- `TestMonitor_Finalize_EagerPush_UnexpectedLocalBranchPushesSafeExecutorRef` PASS
- `TestMonitor_Finalize_RecordedWorkspace_PushesActualHeadToSafeRef` PASS
- `TestMonitor_Finalize_RecordedWorkspace_RemoteBranchMismatchPushesSafeRef` PASS
- `TestMonitor_Finalize_ExactOriginMainStillRefused` PASS
- `TestMonitor_Finalize_EagerPush_PushFailureDowngrades` PASS
- `TestValidCodeDelivery_RejectsProtectedBranch` PASS

Key observed behavior:

- HEAD on a non-system local branch is pushed only to `ac-exec/<task>/<exec>`.
- Local business branch refs are not created on the remote by delivery finalization.
- Protected `main` delivery is refused even when the commit is also on an executor ref.
- Push failure is structured retryable `non_delivery` with push error and retained worktree.
- Remote full refs before/after the acceptance run were byte-identical before the final evidence push: `diff_exit=0`.

Raw proof:

- `raw/10-ab-finalize-tests.log`
- `raw/50-negative-adjacent-classification.log`
- `raw/71-remote-refs-diff-before-delivery-push.txt`

## Acceptance C: Stale/Lease

Verdict: PASS

Evidence:

- `TestResetToOpen_LiveLeaseRejected` PASS
- `TestResetToOpen_BypassLeaseResetsLiveLease` PASS
- `TestResetTask_OwnerConfirmedDeadBypassesLiveLease` PASS
- `TestResetTask_StrangerConfirmedDeadStillRejectedOnLiveLease` PASS
- `TestResetTask_ConcurrentSingleIncrement` PASS
- `TestStuckNode_UpdatedAtStale_ReopenedWithoutLeaseLapse` PASS
- `TestStuckNode_LiveOwnerIdleWithoutExecutor_AutoReopenedDespiteLeaseRenewal` PASS
- `TestStuckNode_FruitlessReopens_DurableAcrossRestart` PASS
- `TestClassifyExecutor` PASS
- `TestReconcileOneExecutor_*` PASS
- Race focused finalizer/reaper/reset run PASS.

Deployment-level restart evidence:

- `completed_turn=true` persisted before crash.
- Worker was `SIGKILL`ed before the directed-message reply.
- Restarted runtime autonomously relaunched and resumed the previous session.
- The outstanding directed message was re-injected after restart.

Key observed behavior:

- Owner-confirmed-dead recovery can bypass the stale live lease dead zone.
- Stranger-confirmed-dead with a live lease is still rejected.
- Stale `updated_at` is recoverable without waiting for lease lapse.
- Repeated/focused finalizer, reaper, reset, and reconcile paths were race-clean.

Raw proof:

- `raw/20-c-stale-lease-tests.log`
- `raw/40-deployed-restart-recovery-e2e.log`
- `raw/60-race-focused.log`

## Negative And Adjacent Gates

Verdict: PASS

Evidence:

- Normal clean pushed delivery stayed `succeeded`: `TestMonitor_Finalize_PushedDeliveryStaysSucceeded` PASS.
- Implementation/test failure stayed `failed`: `TestReport_Failed_BlocksTask`, `TestMonitor_Finalize_FailedRunNeverCreatesOriginDelivery`, and `TestMonitor_Finalize_FailedRunDiscoversPartialOriginDelivery` PASS.
- Quality/stage-gate reject stayed distinct and idempotent: `TestCompleteTask_StageGateRejectAppendsRemediationAndReplayIsIdempotent`, `TestStageReject_AppendsIncrementalStageWithoutReopeningHistory`, and `TestEnsureTaskRunnable_MergeToMain_RejectVerdict_Blocked` PASS.
- Delivery non-delivery stayed mechanically distinct: `TestMapDomainError_TaskNonDeliveryIsExplicitConflict` and `TestReport_CodeExitZeroWithoutDurableDeliveryIsNonDelivery` PASS.
- Unattached executor-engine regressions were not used as PASS evidence; current main already contains `226de725 fix(runtime): reattach missing executor engine`.

Raw proof:

- `raw/20-c-stale-lease-tests.log`
- `raw/50-negative-adjacent-classification.log`

## Production Follow-up

No production deployment was performed. Production release smoke still needs separate
authorization for: production center/worker rollout, production runtime restart smoke,
production worker capability/readback smoke, and production protected-ref readback after
deployment.
