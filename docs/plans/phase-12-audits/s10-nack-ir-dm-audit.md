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

To be filled in by the test commit.
