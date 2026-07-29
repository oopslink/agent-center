# T1247 AI Runtime Stage 1 — Loopback Test Plan and Report

## Scope

- Object under test: the production `ProjectManager` lifecycle integration with
  immutable AI Runtime execution snapshots.
- Delivery tree: `feat/t1247-ai-runtime-stage1`.
- Exit contract: a fresh installed server must freeze one snapshot when a task
  starts, then preserve the exact persisted bytes across Catalog mutation,
  retry/resume, and reassign.

## Test plan

| # | Layer | Case | Expected result |
|---|---|---|---|
| 1 | Unit | Resolver sees an existing execution snapshot before mutable Catalog state | Existing snapshot is returned without resolving the current Catalog |
| 2 | Integration | SQLite-backed `StartTask`, `BlockTask`/`UnblockTask`, and `AssignTask` lifecycle | One canonical snapshot row; bytes remain unchanged |
| 3 | Deployed-binary smoke | Fresh server and worker binaries, real Unix socket, real agent-tool lifecycle | Snapshot is created at `start_task`; Catalog mutation plus retry/resume/reassign does not change its raw bytes |
| 4 | Repository gate | Full Go test suite on the final delivery SHA | Zero assertion failures, or a reproducible and explicitly classified infrastructure failure |

## Results

| # | Status | Evidence |
|---|---|---|
| 1 | PASS | `go test ./internal/airuntime/...` includes `TestExecutionFreezerReadsFrozenSnapshotBeforeMutableCatalog` |
| 2 | PASS | `go test ./internal/projectmanager/service -run TestRuntimeSnapshotProductionLifecycleIsByteStable`; the same case also passed under `-race` |
| 3 | PASS | `pnpm exec playwright test tests/v22-deployed-pipeline.spec.ts`: 1 passed. Fresh binaries started a temporary server/worker instance; the test asserted `snapshot_json` byte equality before and after Catalog revision, block/unblock, and reassign |
| 4 | Pending | Filled after the final full-suite run |

## Layered inventory

| Layer | Count | Entry |
|---|---:|---|
| Unit (in-package) | 1 focused package suite | `go test ./internal/airuntime/...` |
| Integration with real SQLite | 1 lifecycle case | `internal/projectmanager/service/runtime_execution_integration_test.go` |
| Deployed-binary smoke | 1 Playwright case | `tests/e2e/v2/tests/v22-deployed-pipeline.spec.ts` |

## Environment note

On the shared worker, a fresh database needed about 50 seconds to complete all
migrations before writing `bootstrap_token`; the admin socket and worker CLI
probe became ready later. The deployed smoke therefore uses bounded 70-second
server readiness and 90-second worker enrollment windows. Both waits remain
fail-closed and report the exact failed stage.

## Conclusion

Pending the final repository-wide test result.
