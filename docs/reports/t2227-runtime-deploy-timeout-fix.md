# T2227 Runtime Deploy Timeout Fix

Date: 2026-09-16

## Candidate lineage

- Exact base: `e59d7d52db3a5eb30e8bd24df54c3ba4b817d819` (`origin/main` at task start).
- Preserved ancestors: product `724c9b8660c1c269bf6e05913f2815d97828d40b`, harness `6060270a`, and acceptance `686baeaa`.
- Delivery is an intermediate candidate for independent review; it is not merged or deployed by this task.

## Change

- `runtime_deploy_restart` now uses a dedicated 70-second worker-to-center HTTP budget, while ordinary admin calls retain their existing 30-second budget. The deploy budget is strictly greater than the center's 60-second synchronous remote-ref verification deadline.
- The center returns HTTP 504 with `remote_ref_verification_timeout` when that verification deadline expires. No control command is created on this path.
- The MCP description now states the real contract: remote refs are verified synchronously, and an attempt exists only after verification and durable enqueue succeed.
- Verify-before-dispatch, `(worker, agent, caller key)` idempotency, changed-payload conflict rejection, and after-commit publication semantics are unchanged.

## Isolated production-chain test

The new integration tests exercise the real `workerdaemon.AdminClient` over a Unix admin transport into the real authenticated admin handler and SQLite-backed environment-control service.

- Purpose: isolated test only; no production state or production endpoint.
- Source tree: this candidate, based on the exact SHA above.
- Work/install directory: task worktree; per-test temporary directories.
- Listener: per-test temporary Unix socket under `/tmp`; no TCP port.
- Database: a fresh in-memory SQLite database per test fixture.
- Runtime identity: fixture worker `W1`, fixture agent `AG1`, fixture bearer only.
- Session/cookie namespace: none; bearer-authenticated admin route.
- Data source: deterministic test fixtures and fake remote verifier.
- Start method: `go test` / `go test -race`; test HTTP servers are created and shut down by the test process.
- Verification date: 2026-09-16.

Covered outcomes:

- a 25ms ordinary AdminClient timeout no longer cuts off deploy verification; a scaled 60ms handler deadline returns structured 504 before the deploy-specific transport cutoff;
- verification timeout dispatches zero commands;
- identical key/payload replays one command and returns the same command ID;
- changed payload with the same key returns `idempotency_conflict`;
- loss of the HTTP response after durable commit is recoverable through `runtime_deploy_status`, and retry returns the original attempt without republishing.

Mutation check: disabling deploy-specific client selection makes the first test fail with a 25ms transport `context deadline exceeded`; restoring it makes the test pass.

## Verification

- `GOTOOLCHAIN=go1.25.11 go build ./...`: pass.
- `GOTOOLCHAIN=go1.25.11 go vet ./...`: pass.
- `GOTOOLCHAIN=go1.25.11 go test ./...`: pass, including E2E and integration packages.
- `go test -race -count=1 ./internal/admin/api -run '^TestRuntimeDeploy(AdminClient|RestartHandler|StatusHandler)' -v`: 12/12 pass.
- Broader race command passed `./internal/environment/...`, `./internal/mcphost`, `./internal/workerdaemon`, and `./internal/runtimedeploy` with no data race. The whole `internal/admin/api` race package exceeded its existing 10-minute aggregate timeout while applying SQLite migrations; the focused runtime-deploy subset above passed in 39.584s.
- `make gen-mcp-docs`: regenerated the checked-in MCP reference from the corrected description.

## Bootstrap order and risk

Both runtime components must consume this candidate:

1. The **worker daemon** must be upgraded first so its AdminClient carries the 70-second deploy budget.
2. The **center** must then be upgraded so it emits the structured verification timeout and advertises the corrected MCP contract.

The formal prerequisite is a production `runtime_deploy_restart` capability that supports `mode=worker`, followed by `runtime_deploy_status` under the same original agent identity, worker, caller key, and payload. The currently deployed worker still has the defective 30-second client, so the worker-first formal request can bootstrap only if the old path completes remote verification within 30 seconds and returns an accepted attempt. If it again times out and same-caller status remains not-found, the formal runtime plane cannot cross its own timeout defect: stop and require an owner/operator-authorized worker install/restart path, then resume formal center deployment. Do not change agent identity or key, and do not infer success from a transport timeout.

After the worker reports the exact candidate SHA, deploy the center through the formal MCP, poll the same key to a terminal success, and require exact running SHA/version plus healthy readback before the I166 production read-only smoke. This task performs no production deployment.
