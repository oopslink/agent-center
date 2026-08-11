# T1322 DAG Evolution Atomicity Acceptance

## Envelope

- verdict: PASS
- candidate_sha: `cd9d2b2757595a7734722d44537dcaa6c0fcb16b`
- assigned_branch: `ac-exec/task-4ab7224c/exec-777cb223`
- execution_worktree: detached `../acceptance-cd9d2b27`
- product_code_modified: no
- evidence_written_to: `docs/plans/evidence/t1322-dag-evolution-acceptance/`

## Raw Evidence

- `git-metadata.log` records assigned branch, detached candidate SHA, and evidence-branch pre-commit HEAD.
- `service.log`: PASS, 20 targeted ProjectManager service tests.
- `api-t1315.log`: PASS, Web Console generation API tests plus independent T1315 atomicity harness.
- `sqlite.log`: PASS, persisted generation snapshot/idempotency/activation round trip.
- `web-plan-detail.log`: PASS, PlanDetail generation/evolution UI tests. Non-fatal MSW warnings appeared for an unrelated `related-issues` request; Vitest reported `1 passed`, `3 passed | 104 skipped`.

## Coverage Matrix

| Requirement | Evidence |
| --- | --- |
| Concurrent evolution | `TestEvolvePlanGeneration_ConcurrentSiblingsOnlyOneActivates`; `TestOrchestrator_ConcurrentDispatch` |
| Idempotent duplicate/replay | `TestEvolvePlanGeneration_RunningAtomicDispatchIdempotencyAndSnapshot`; `TestPlanGenerationAPI_G0GnLineageSnapshotReplayAndStaleConflicts`; remediation replay in `TestStageReject_WhilePausedRecordsFactsThenAppendsOnceAfterResume` |
| In-flight preserve/hold/supersede | preserve path in `TestEvolvePlanGeneration_RunningAtomicDispatchIdempotencyAndSnapshot`; supersede/edge/hold rejection in `TestEvolvePlanGeneration_InFlightConflictDecisions` |
| Blocked final gate | `TestPlanCompletion_UnremediatedFailureBlocksAutoAndManualCompletion`; `TestPlanCompletion_LegacyFailedLeafWithoutRecoveryChainStillBlocks`; `TestPlanCompletion_ReadyDispatchedAndRunningWorkBlockManualCompletion` |
| Remediation stage | `TestStageReject_AppendsIncrementalStageWithoutReopeningHistory`; `TestStageReject_WhilePausedRecordsFactsThenAppendsOnceAfterResume` |
| Crash/restart recovery | `TestReconcile_DispatchesMissedReadyNode`; `TestOrchestrator_ReplayIdempotent`; `TestOrchestrator_FailureReplayNotifiesOnce`; `TestOrchestrator_AgentCreatorWakeReplayOnce`; paused-reject resume replay |
| Generations / active_generation / node ownership | `TestGetPlanGenerations_ReadsPersistedG0GnSnapshotsAndOwnership`; `TestLoadPlanGenerationLineage_FailsClosed`; API history assertions |
| Dispatch event/record atomicity | `TestEvolvePlanGeneration_RunningAtomicDispatchIdempotencyAndSnapshot`; `TestOrchestrator_ReplayIdempotent`; T1315 harness dispatch row assertion |
| UI/API | Web Console API generation tests and PlanDetail generation/evolution UI tests |

## T1315 Reproduction

The independent temporary harness `TestT1315AtomicEvolutionAcceptanceHarness` was added only inside the detached candidate worktree, run, and then deleted. It exercised the product HTTP API and local test SQLite tables.

Success path:

- setup: running plan with `T1314 fixed parent` completed while `T1316 keeps plan running`.
- request: `POST /api/projects/{project}/plans/{plan}/evolution` with `parent_generation_id=G0`, `base_version=current`, task draft `T1315`, and edge draft `from=T1315`, `to=T1314`, `kind=seq`.
- assertions: HTTP 200, `duplicate=false`, new `G1 != G0`, generation count increased by exactly one, `pm_plans.active_generation_id=G1`, version advanced by one, `snapshot_json` contains `T1315 -> T1314`, response `dispatched` contains T1315, `pm_plan_dispatch_records` has exactly one T1315 row, `pm_task_dependencies` has exactly one T1315-to-T1314 edge, `GET /generations` returns G0/G1, node ownership is T1314->G0 and T1315->G1.

Reject path:

- setup: running plan with T1314 already dispatched.
- request: invalid in-flight rewrite attempting to add task `T1315 rejected partial` and edge `from=T1314`, `to=T1315`.
- assertions: HTTP 409, generation count unchanged, `pm_plans.active_generation_id` unchanged at G0, plan version unchanged, no rejected T1315 task row, no dependency row.

This satisfies the T1315 rule: accept only an atomic T1315-to-T1314 new generation, or reject the whole request with zero active-generation change.

