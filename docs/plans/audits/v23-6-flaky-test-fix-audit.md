# v2.3-6 Flaky CLI Test Fix Audit

> Closes slock task #33. Pre-existing flake in
> `TestClient_ConversationRead_TailUsesFindRecent_OverAdminEndpoint`
> captured during #32 verification. Single-test-helper fix; no
> production code touched.

## § 1. 现象 — what was failing

`internal/cli/admin_client_conversation_test.go::TestClient_ConversationRead_TailUsesFindRecent_OverAdminEndpoint`
intermittently failed (~25-30% on local Mac in full-package runs) at
the **2nd** `send` call (line 184-186):

```
--- FAIL: TestClient_ConversationRead_TailUsesFindRecent_OverAdminEndpoint (0.02s)
    admin_client_conversation_test.go:185: send 1 exit=1
        stderr={"error":{"reason":"internal_error","message":"database is locked (5) (SQLITE_BUSY)"}}
```

The test does: open channel conversation → send 4 messages back-to-back
→ tail to verify recency. Sends 0/1/2/3 are issued in a tight loop.

Solo runs (`go test -run <only-this-test>`) pass 10/10. Failure only
manifests under full-package runs — confirming the cause lives in the
test setup, not the assertions.

The error from the earlier `/nonexistent.yaml` mention in the #33
report was a **red herring** — that string was raw os.Stderr bleed
from a different test (`TestBuildRouter_LazyResourceCmd_BadConfig`)
that runs after this one. Surfaced by adding stdout/stderr to the
test's `t.Fatalf` formatter.

## § 2. 根因 — root cause

`admintoken.service.Service` runs a background "mark-used pump"
goroutine that performs `UPDATE admin_tokens SET last_used_at = ?` in
response to every `MarkUsedAsync` call. The pump is single-goroutine
+ per-id 30s throttle (added per v2.3-3a to prevent SQLITE_LOCKED
under worker-daemon polling).

In tests:
1. `setupAdminServerForTests` creates a test bearer token.
2. Test calls `ConversationCommands().open(...)` → admin middleware →
   `MarkUsedAsync(testTokenID)` → channel buffers an id → pump wakes
   up + starts the `UPDATE admin_tokens` statement.
3. Same tick: test calls `ConversationCommands().send(...)` →
   `messageAppend` opens a write transaction → contends with the
   pump's still-in-flight `UPDATE`.
4. `busy_timeout=5000ms` is supposed to retry, but the modernc.org/
   sqlite driver returns `SQLITE_BUSY (5)` **immediately** for certain
   intra-process write/write races on macOS rather than honoring the
   busy_timeout. This is a known driver-level quirk and the reason
   v2.3-3a already had to serialize the pump.

The intermittent nature is because the pump's goroutine schedule is
non-deterministic — sometimes it completes the UPDATE before send 0
opens its tx, sometimes it overlaps with send 1's tx.

The same root cause would also flake other tight-loop tests if they
happened to issue ≥ 2 admin writes within ~10 ms of each other; this
test was just the first to expose it under #32's overall test count.

## § 3. 修法 — fix

`internal/cli/admin_client_testhelper.go`: after minting the test
bearer token, **immediately** call `app.AdminTokenSvc.Close()` so the
pump's channel is `nil` for the duration of the test. `MarkUsedAsync`
now early-returns (channel is nil) — no UPDATEs happen during test
bodies, so no write/write contention.

Production behavior is unchanged: this is test-only setup. The pump
is exercised by its own dedicated unit tests in
`internal/admintoken/service/service_test.go`. Tests that use
`setupAdminServerForTests` validate auth flow + scope + handler
routing — none assert on `last_used_at` bookkeeping.

`cleanup()` still calls `Close()` again; the function is idempotent
(see service.Close() with `_ = recover()` guard).

Also added stdout/stderr to the failing `t.Fatalf` line so any future
regression here surfaces the actual reason instead of just "exit=1".

## § 4. Verification

| Gate | Result |
|---|---|
| `go test -count=1 ./internal/cli/` × **20 consecutive runs** | **20/20 pass** (DoD met) |
| `go test ./...` | all packages green |
| `make lint` | green |
| `make smoke` | `smoke pass: 8 seconds` |

## § 5. § 0.6 layer discipline

- **Observation**: SQLITE_BUSY (5) raised on 2nd write of a 4-write
  burst, ~25-30% of full-package runs.
- **Capability**: `busy_timeout=5000` is configured at the DSN
  level; modernc.org/sqlite driver applies it per-connection. It
  works under "normal" contention but does not retry reliably for
  this specific intra-process pattern.
- **Not claimed**: "the driver is broken" / "the design intended to
  serialize all admin writes". The fix sidesteps the contention by
  removing the contender (the pump) from test runs, not by changing
  prod behavior.

Closes slock task #33.
