# R9 runtime stop lifecycle evidence

Base: immutable `091c15ee2d4a61bab3ca48c57b59ef2ba95168eb`.

Ancestry verified present: R5 `790ab2b0`, R6 `8a79f00f`, R7 `039c5fea`, T1685 `091c15ee`.

Identified admitted wait chain:

`LocalRuntime.SpawnExecutor` admitted runtime lifecycle work with `beginRuntimeWork`, then used the caller context for center/tool calls. During `Stop`, `closeRuntimeAdmission` prevented new work and `lifecycleCancel` fired, but the already admitted `SpawnExecutor -> get_task` wait did not observe runtime lifecycle cancellation unless its caller context happened to be canceled. `Stop` then waited in `waitOwnedBackground -> lifeWG` until its own deadline.

Fix:

`SpawnExecutor` now wraps its caller context with a lifecycle-bound context immediately after successful runtime-work admission. The wrapped context is canceled when either the caller context or the runtime lifecycle context is canceled, so `Stop` can cancel and join admitted spawn work without extending timeouts or skipping joins.

Command logs:

- `red-stop-cancels-inflight-center-read.log`: deterministic red test on immutable production code plus the new test only; failed with `runtime lifecycle work: context deadline exceeded`.
- `red-immutable-task-input-plan569-e2e-race-x10.log`: original named immutable E2E `-race -count=10`; did not fail on this machine, but logs show the same clone re-drive/start-task cancellation window.
- `green-stop-lifecycle-targeted.log`: targeted stop before/during/after tests.
- `green-stop-lifecycle-race-x10.log`: targeted stop lifecycle `-race -count=10`.
- `green-task-input-plan569-e2e.log`: named admin task-input E2E once.
- `green-task-input-plan569-e2e-race-x10.log`: named admin task-input E2E `-race -count=10`.
- `green-touched-packages.log`: `go test ./internal/agentruntime ./internal/admin/api -count=1`.
- `green-full-go-test.log`: full `go test ./...`.
