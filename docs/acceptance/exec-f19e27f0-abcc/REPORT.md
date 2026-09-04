# Isolated Executor A/B/C Acceptance Continuation

Verdict: REJECT.

This run did not satisfy the requested deployed A/B/C acceptance contract. I preserved the useful evidence instead of claiming pass.

## Baseline

- Fetched `origin/main` at execution time and pinned `226de72569b18913ccb09119922a6feed9ad174a`.
- Proved `f2a3bf2d` is an ancestor of pinned `origin/main`.
- Built current main with `make build`.
- Raw evidence:
  - `raw/00-fetch-pin-sha.log`
  - `raw/03-make-build.log`

## Isolated Instance

- Created isolated test instance `abcc-f19e27f0` under `/Users/oopslink/.agent-center-test/abcc-f19e27f0`.
- Ports: web `53759`, server `53760`, admin `53761`.
- Center config: `/Users/oopslink/.agent-center-test/abcc-f19e27f0/center/etc/config.yaml`.
- Workers:
  - `test-abcc-f19e27f0-w1` from `install test-instance --with-seed`
  - `worker-f528306d` from org-scoped `mint-enroll` plus isolated `install worker`
- Restarted isolated center and both workers using isolated launchd labels; health returned HTTP 200 after restart.
- Raw evidence:
  - `json/07-install-test-instance-with-seed.jsonlog`
  - `json/17-create-runtime-model.http`
  - `json/18-mint-org-worker2.http`
  - `raw/19-install-worker2.log`
  - `proc/25-config-provenance.log`
  - `proc/27-kickstart-restart.log`
  - `json/28-health-after-restart.http`
  - `proc/29-processes-after-restart.log`

## Explicitly Excluded Fault

`install test-instance --with-agent` failed before A/B/C due to `runtime_model_not_found` for hardcoded model `claude-opus-4-8`. That was recorded as an unrelated seed/config failure and was not counted as an acceptance result.

Evidence: `json/05-install-test-instance.jsonlog`.

## Adjacent Green Evidence

Targeted package tests passed for dirty/non_delivery mapping, safe executor ref push behavior, push failure downgrade, lease heartbeat/reset, and writeback classification.

Evidence: `raw/20-green-targeted-tests.log`.

Existing deployed e2e passed for:

- real deployed fork executor path through `worker agent-runtime` and control socket
- worker SIGKILL session resume/message reinjection

Evidence: `raw/24-existing-deployed-e2e.log`.

These are adjacent gates only. They do not replace the requested deployed A/B/C scenarios.

## Red Endpoint

Attempted red endpoint by running the selected tests at `f2a3bf2d^`. It exited `0`; therefore it is invalid as red evidence.

Evidence: `raw/23-red-prefix-targeted-tests.log`.

## Gate Results

- A dirty cleanup: REJECT. No real isolated executor dirty repair and unrecoverable dirty delivery path was driven through delivery preflight.
- B safe ref: REJECT. No deployed executor ahead commit was proven to push only to the system executor ref with protected branch tips unchanged.
- C stale/lease: REJECT. No terminal/stale/recovery_required executor state was manufactured across real reaper ticks and restart.
- Negative/regression: REJECT. Normal clean push, implementation/test failure, quality-gate reject, and delivery non_delivery were not all driven as deployed isolated scenarios.

## Remote Refs

Remote refs before and after were captured:

- `refs/01-remote-refs-before.log`
- `refs/31-remote-refs-after.log`

No deployed B scenario was completed, so these snapshots are provenance only, not PASS proof.

## Remaining Work

Continue in a follow-up executor with a purpose-built deployed harness that can:

1. drive real forked executors inside the isolated center/worker/runtime,
2. inject dirty repair, dirty non-repair, safe-ref push failure, and stale lease states through public test controls or a test-only runner,
3. capture API/MCP/audit readbacks for each A/B/C verdict,
4. provide a valid red endpoint by disabling only the target mechanism or running a genuinely pre-fix revision that fails the same deployed predicates.

Production deployment or production smoke remains out of scope and requires separate authorization.
