# T1850 Worker Authoritative Readback Replay

Date: 2026-08-31

## Candidate Lineage

- Fresh base: `3b2b45f480c297f44b0e2deb877ebc6cdad7f5f5`
- Source implementation replayed: `65eab385a93199d98fe3b367d4d07bc627cda7f2`
- Source parent: `3b2b45f480c297f44b0e2deb877ebc6cdad7f5f5`
- Required production-chain anchor: `7f4cfcc43e0360f31e756bb453e675db50cc26c6`
- Merge-base with source implementation before replay:
  `3b2b45f480c297f44b0e2deb877ebc6cdad7f5f5`
- Merge-base with production-chain anchor before replay:
  `7f4cfcc43e0360f31e756bb453e675db50cc26c6`

## Chain Verification

`7f4cfcc43e0360f31e756bb453e675db50cc26c6` is an ancestor of the fresh base,
so the current candidate preserves its production consumer chain. The replayed
T1850 implementation is a direct child of the fresh base and only changes the
worker-daemon authoritative readback path.

## Task Input Package

The supervisor-declared `task-input/v1/README.md` and
`task-input/v1/manifest.json` were not present in the workspace at executor
start. This candidate materializes them locally with an empty attachment list;
no attachments were present to hash or classify.

## Implemented Behavior

- Worker-mode `runtime_deploy_restart` no longer trusts pre-restart daemon
  process options for running build identity.
- After the worker upgrade command returns, the daemon reads the restarted
  worker projection through the authenticated admin control plane:
  `/admin/workforce/worker/find-by-id`.
- The readback requires a full expected target SHA and retries until the center
  reports the target worker online with matching full `system_info.build_commit`.
- Stale SHA, missing SHA/version, unavailable worker projection, unhealthy
  status, timeout, and disconnected admin transport fail closed.

## Evidence Commands

The final delivery report should include raw command output for:

- `git merge-base HEAD 65eab385a93199d98fe3b367d4d07bc627cda7f2`
- `git merge-base HEAD 7f4cfcc43e0360f31e756bb453e675db50cc26c6`
- Focused workerdaemon tests for runtime deploy readback.
- Full `go test ./...`.
- Race target.
- `go vet ./...`.
- Branch push result.
