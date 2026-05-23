# P12 S10 — e2e NACK→Issue + IR respond + DM start audit

> Run 2026-05-24 · per x9527 M3 oversight: 3 sub-scenarios per the
> S10 plan slot, with narrowing decisions transparent in § 0 per
> the S9 pattern. Audit log + test commit separated.

## § 0. Scope refinement (transparent per oversight ⑦)

x9527 enumerated three sub-scenarios:

| Sub-scenario | In S10? | Why / Why not |
|---|---|---|
| **NACK→Issue** (worker NACK → supervisor auto-spawn Issue) | ❌ **narrowed to v3** | Requires (a) a live worker daemon to receive the dispatch + send NACK, (b) a live supervisor agent to react to the `task_execution.dispatch_nacked` event. Both blockers are the same as S9's worker chain. The behavior IS covered by `tests/e2e/phase7_test.go` (Go in-process) + `internal/taskruntime/dispatch/*_test.go`. Adding it to v2 e2e would require docker compose + fake agent harness — v3 candidate. |
| **IR respond** (UI → API → DB state) | ✅ via direct sqlite seed | The IR + execution + task can be seeded directly; the respond happy-path through `/api/input_requests/{id}/respond` exercises the API + service contract. We don't need a live worker because the test asserts the *DB state transition* (IR.status / execution.status), not the agent-side resume. |
| **DM start** (multi-peer DM → send msg → SSE receive) | ✅ pure API + SSE | DM creation is a pure HTTP POST; SSE is a real-time event channel that the binary already serves on `/api/sse`. Both are testable from Playwright without worker. |

S10 ships **2 of the 3** sub-scenarios; the narrowing decision +
reason are right here per oversight ⑦.

## § 1. Seed strategy

Reusing the S9 codified lesson: **direct sqlite INSERT for pre-seed;
never CLI subprocess while server runs.** S10's seeds:

For IR respond test:
- `projects` row (1)
- `tasks` row (1; status=open)
- `task_executions` row (1; status=input_required)
- `input_requests` row (1; status=pending)

For DM test:
- 2 user identities (so DM has named peers); but actually the identity
  table auto-grows on conversation activity, so we just send messages
  with the right `sender_identity_id` strings.

Each test seeds before invoking the API, with all IDs randomized
(`-${randomUUID().slice(0, 8)}`) to keep tests independent.

## § 2. IR respond test plan (oversight ②)

`tests/respond-input-request.spec.ts`:

1. Seed project / task / task_execution(status=input_required) / IR(status=pending).
2. GET /api/input_requests → list contains 1 entry; verify `status: pending`, `question`, no plaintext leak.
3. POST /api/input_requests/{id}/respond {answer, decided_by} → 200 with `{answered: true}`.
4. GET /api/input_requests → empty (the list shows pending only).
5. Direct DB read: `input_requests.status === 'responded'`; `task_executions.status === 'working'` (left input_required per service).

Error path: POST /api/input_requests/{bogus-id}/respond → 404 `not_found`.

## § 3. DM start test plan (oversight ③)

`tests/dm-flow.spec.ts`:

1. POST /api/conversations {kind: "dm", members: ["user:peer1", "user:peer2"]} → 201.
2. POST /api/conversations/{dmId}/messages × 2.
3. GET /api/conversations/{dmId}/messages → both messages returned with correct order.
4. Parallel SSE subscriber: open `EventSource` on /api/sse before message #2 is sent; assert that a `conversation.message_added` event for the second message arrives within `expect.poll`'s default timeout. **DOM-observable assertion per oversight ④**: we wait on an event-stream condition, not a wall-clock sleep.

Error path: POST /api/conversations {kind: "dm"} with no members → 400 invalid_input.

## § 4. SSE assertion strategy

The Playwright `request` fixture does HTTP — for SSE we use Node's
`EventSource` via the `eventsource` npm package, OR a manual `fetch`
with `response.body` stream reader. Picking the **manual fetch +
ReadableStream** path to avoid an extra dep + because it lets us
fully control timeout / cleanup. Helper `subscribeSSE()` in
`helpers/sse.ts` returns an `AsyncIterable<event>` that the test
consumes with `for await`.

The SSE protocol is `\n\n`-delimited events with `event:` /
`data:` / `id:` lines. Our helper parses minimally and yields
`{event, data, id}` records.

## § 5. Anti-flake (oversight ⑤)

- All seed IDs randomized per test (uuid suffix).
- SSE wait uses `expect.poll(() => ...).toMatchObject(...)` with the
  Playwright auto-retry policy (actionTimeout 10s from
  playwright.config.ts).
- Tests fully parallel (workers: 2) but each has its own DB +
  port — no shared state.

Run 3× back-to-back; all 3 must pass with identical counts. Record
in § 8.

## § 6. Acceptance criteria

- Audit log committed first.
- Test commit lands second; 2 spec files (IR + DM) with happy +
  error each = 4 new cases. Plus the 5 existing cases.
- 3× green / 0 retries.
- Smoke + S9 cold-start still pass (no regression).

## § 7. What S10 does NOT do

- NACK→Issue (narrowed; § 0).
- Web→CLI bidirectional (S11).
- SSE Last-Event-ID recovery (S11).

## § 8. Execution log

### 8.1 Audit commit
`81f84e6 docs(p12 S10) IR + DM e2e audit + NACK→Issue v3-narrowing`
— this file (§ 0-7).

### 8.2 Test commit

Files added:
- `tests/e2e/v2/helpers/sse.ts` — minimal SSE subscriber via fetch +
  ReadableStream + manual `\n\n` record parser; returns a `stop()`
  function. ~85 lines, no extra npm dep.
- `tests/e2e/v2/tests/respond-input-request.spec.ts` — 2 tests:
  happy path (seed full task/exec/IR chain via sqlite; respond
  succeeds; verify IR.status='responded' + exec.status='working'
  via direct DB read) + error path (404 for nonexistent IR).
- `tests/e2e/v2/tests/dm-flow.spec.ts` — 2 tests: happy path
  (create DM; subscribe SSE; send message; assert SSE event
  arrived via `expect.poll`) + error path (400 for DM create
  without members).

### 8.3 Wins on first try (no bug fixes needed)

S10 ran green on the first execution. Two factors:

1. **S9 codified rules** held: direct sqlite INSERT for pre-seed
   avoided the event-seq race that bit S9 mid-flight; `body.error`
   field name applied throughout (no `body.code` re-mistakes).
2. **SSE helper deliberately conservative**: opening the
   EventSource BEFORE the trigger event + tiny 100ms settle ensures
   the stream is ready when the event fires; `expect.poll` provides
   auto-retry so we never `waitForTimeout` and never miss events.

### 8.4 Anti-flake gate (oversight ⑤)

```
=== Run 1: 9 passed (4.0s) ===
=== Run 2: 9 passed (4.0s) ===
=== Run 3: 9 passed (4.0s) ===
```

3/3 green; total runtime variance < 50ms; 0 retries.

### 8.5 SSE assertion notes

The DM test exercises the real-time path:

1. POST /api/sse/subscribe — bus enrolls user/conversation pair
2. fetch /api/sse?user_id=... — open EventSource stream
3. 100ms settle window (the only synthetic delay; it's a handshake
   barrier, NOT an assertion-on-time)
4. POST /api/conversations/{id}/messages — triggers
   `conversation.message_added` event
5. `expect.poll(() => events.some(...))` — auto-retries until the
   event lands or the actionTimeout (10s) expires

If the event never arrives (e.g. SSE wiring breaks in some refactor),
the poll fails after 10s with a clear "expected SSE event
conversation.message_added for the DM" message — root cause is
obvious from the failure.

### 8.6 Artifact size after green run

```
$ du -sh tests/e2e/v2/artifacts/
544K
```

Stable (~540K from S9; +4K is the new test cases in the report
HTML).

### 8.7 S11 readiness

S11 will exercise (a) Web↔CLI bidirectional sync — CLI subprocess
sends a message, SSE delivers to Web; Web sends, CLI tail shows it;
and (b) SSE Last-Event-ID reconnect recovery. Both build on the
SSE helper landed here. The first scenario adds a wrinkle: the CLI
subprocess WILL emit events, which is the same race condition that
bit S9 — but only WITHIN a controlled, expected step (not pre-seed),
so it's fine.

For the SSE-reconnect scenario, we'll need to extend the helper to
expose the last event ID (already in `SSEEvent.id`) and accept a
`Last-Event-ID` query param on subscribe. Minor extension.

### 8.8 NACK→Issue narrowing — for record

Per § 0, NACK→Issue (worker NACK a dispatch → supervisor auto-spawn
Issue) requires both a worker daemon and a live supervisor. Same
blocker class as S9's worker chain narrowing. The behavior IS
covered by:

- `tests/e2e/phase7_test.go` — center-side end-to-end (Go-in-process)
- `internal/taskruntime/dispatch/reconcile_test.go` — NACK handling
- `internal/observability/escalator/*_test.go` — supervisor wake on
  task_execution.dispatch_nacked event

Adding it to the Playwright e2e would require docker compose +
fakeagent + worker daemon orchestration — that's a v3 "deployment
e2e" candidate. Documented openly per oversight ⑦.
