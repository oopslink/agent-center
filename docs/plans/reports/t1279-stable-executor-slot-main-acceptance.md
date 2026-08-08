# T1279 stable Executor Slot acceptance on main

- Verdict: **PASS**
- Accepted ref: `origin/main@26bc44d2b807df211b38601bcf68d0b66ee36534`
- Date: 2026-08-09 (Asia/Shanghai)
- Scope: evidence-only independent acceptance; no production implementation changed.

## Executable contract results

| Contract | Evidence | Result |
|---|---|---|
| cap=3 launches receive stable lowest-free slots `#0/#1`; releasing `#0` lets a new execution reuse `#0` without changing execution identity | `TestPool_LaunchAssignsLowestFreeSlot`, `TestPool_ReleaseFreesSlot`, `TestReleaseSlot_FreesSlotIdempotently`; activity rendering keeps the full executor id in the expanded lifecycle row while previewing `Executor #N` | PASS |
| runtime restart preserves surviving slot assignments | `TestPool_Launch_WritesRecoveryRecord`, `TestReconciler_BackfillsRecordFromInputSlot`, `TestReconciler_LegacyBackfillStableSlots`, `TestMonitor_Recover_FinalizesAndAdopts`, boot reconcile tests | PASS |
| shrink drains high slots without killing or moving active runs | `TestPool_ResizeShrinkDrainsWithoutMovingRuns`, `TestPool_ResizeExpandAppendsAdmissibleSlots`, `TestSnapshotAgentConcurrency_FullSlotsAfterLiveResize` and concurrency snapshot assertions | PASS |
| duplicate and out-of-range slots fail loud | `TestPool_Adopt_PreferredSlotConflictsFailLoud`, `TestReconciler_DuplicateAndOutOfRangeSlotsFailLoud`, `TestAPI_AgentConcurrency_DegradedDuplicateSlot` | PASS |
| fresh/stale/offline/no-data and legacy mixed-version payloads never fabricate Idle or stable numbering | `TestAPI_AgentConcurrency_StaleSnapshotDoesNotAssertIdle`, `...LegacySnapshotWithoutSlotFields`, `...NoSnapshot_Stale`, `...OnlineNoSnapshot_NoData`, `...WorkerOffline_NotReachable`, `...RunningFallback_NoSnapshot`; `ExecutorSlotPanel` legacy/degraded cases | PASS |
| Agent Detail, Activity sidebar/task overlay, and Agent list agree on stable slot semantics | focused Vitest: `ExecutorSlotPanel`, `AgentTasks`, `AgentActivityRow`, `SenderDetailSidebar`, `AgentDetail`, `Agents`, concurrency hook: 7 files / 136 tests | PASS |

## Repository-wide gates

All commands were run from the clean isolated worktree at the accepted SHA.

```text
make test       PASS (includes tests/e2e and tests/integration)
make test-race  PASS (-race -count=10 ./internal/agentruntime/...)
pnpm web test   PASS (186 files, 1704 tests)
make build      PASS (SPA + agent-center + fakeagent)
make lint       PASS (vet, gofmt, repository guards, tsc, eslint)
```

The Web suite emitted pre-existing non-fatal test warnings (MSW unmatched-request, React `act`, and an invalid test DOM nesting warning). The suite exit code was zero, all 1704 tests passed, and the focused slot UI rerun also passed 136/136.

## Decision

No blocking discrepancy was found on the integrated main SHA. Stable slot allocation, restart recovery, live shrink/draining, integrity degradation, backward-compatible no-fabrication behavior, and the three required UI surfaces satisfy the acceptance contract.
