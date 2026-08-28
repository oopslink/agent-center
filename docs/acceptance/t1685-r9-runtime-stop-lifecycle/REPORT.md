# R9 Runtime Stop Lifecycle Fix

Base immutable ref:

`origin/immutable/t1685-runtime-work-fence-091c15ee` resolved via `git ls-remote` to `091c15ee2d4a61bab3ca48c57b59ef2ba95168eb`.

## Wait Chain

The failing admitted work is the clone-redrive executor fork chain:

`deferForClone` background work -> `redriveDeferredClone` -> `SpawnExecutor` -> `launchExecutorNow` -> executor drain -> `reconcileOneExecutor`.

Under the race, `Stop` cancels `lifecycleCtx` while the admitted executor drain is deciding point recovery. The old path still performed `get_task`, could decide `should-continue`, and could enter recovery/relaunch or delayed fused finalization while `Stop` was already waiting on `lifeWG`.

The fix makes runtime stop a hard point-recovery fence. Once `lifecycleCtx` is cancelled, executor drain performs local stop cleanup only: no center reads, no writeback, no recovery event, and no relaunch.

## Evidence Logs

- `raw/00-red-stop-cancelled-drain.log` - red regression on the baseline.
- `raw/01-fetch-prune.log` - required `git fetch origin --prune`.
- `raw/02-ls-remote-immutable.log` - immutable remote SHA verification.
- `raw/03-ancestry.log` - R5 -> R6 -> R7 -> T1685 ancestry.
- `raw/04-green-redline.log` - red test green after fix.
- `raw/05-stop-before-during-after-slow-rejected-postclose.log` - stop lifecycle matrix.
- `raw/06-targeted-race-x10.log` - targeted race x10.
- `raw/07-admin-task-input-e2e-race-x10.log` - admin task-input E2E race x10.
- `raw/08-package-regression.log` - package regression for touched packages.
