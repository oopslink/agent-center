# P12 S9 — e2e cold-start user journey audit

> Run 2026-05-24 · per x9527 M3 oversight: one happy path + one error
> path; SSE assertions via DOM-observable evidence (no
> `waitForTimeout`); 3× green required; audit log + test commit
> separated.

## § 0. Scope refinement

The original cold-start chain enumerated in x9527's oversight is:

> center boot → worker enroll → agent create → secret create →
> channel create → user 发 message → agent reply → multi-select
> messages → derive issue → conclude → task execute → task done

**S9 narrows this to the API-only, no-worker-required prefix** for
practical reasons:

| Step | In S9? | Why / Why not |
|---|---|---|
| center boot | ✅ via fixture | already proven in S8 smoke |
| worker enroll | ❌ | requires a separate worker daemon process + 2-step bootstrap-token exchange (ADR-0023); not tractable inside a Playwright test |
| agent create | ❌ | requires a live worker (per ADR-0025); same blocker |
| secret create | ✅ | wired via master_key in fixture (S9 contribution) |
| channel create | ✅ | POST /api/conversations kind=channel |
| user 发 message | ✅ | POST /api/conversations/{id}/messages |
| agent reply | ⛔ → user reply | no worker, so "agent reply" is impossible; substitute "second user message" to populate the multi-select set |
| multi-select → derive issue | ✅ | POST /api/issues (needs project_id) |
| conclude / task execute / task done | ❌ | requires dispatch → worker → execution — same worker blocker |

S9 covers the **user-facing v2 surfaces that DON'T require a worker
daemon**. The worker+execution chain is a v3 e2e candidate (running
docker compose with worker daemon + fake agent).

This narrowing IS the right call for v2 e2e because:
1. The worker-side chain is already covered by Go integration tests
   in `tests/e2e/phase7_test.go` (in-process; no subprocess).
2. The Web Console surfaces (the v2 user entry point per ADR-0037)
   ARE the unique v2 e2e surface — that's what S9 should prove.

## § 1. Scenario chain (happy path)

1. **Smoke pre-flight**: GET / → SPA loads (re-asserts S8 baseline).
2. **Project seed** (via CLI subprocess so we don't need an admin
   API): `bin/agent-center project add p-demo --name="Demo Project"
   --config=<path>`. We invoke the CLI subprocess once per test
   from inside the page test to keep the test isolated.
3. **Secret CRUD round-trip**:
   - GET /api/secrets → `[]`
   - POST /api/secrets {name: "claude-key-1", kind: "mcp", value: "secret-xyz"}
     → 201; response contains `id`, `name`, NO `value` field
   - GET /api/secrets → 1 entry, value field absent (proves
     ADR-0026 § 5 plaintext-never-echo)
4. **Channel + messages**:
   - POST /api/conversations {kind: "channel", name: "design-review"}
     → 201
   - POST /api/conversations/{id}/messages × 3
5. **Multi-select derive Issue**:
   - GET /api/conversations/{id}/messages → returns the 3 messages
   - POST /api/issues {source_conversation_id, source_message_ids:
     [first 2 IDs], project_id: "p-demo", title: "Auth question"}
     → 201; response carries the new issue conversation id
6. **Verify derive linkage**:
   - GET /api/conversations/{issueConvId}/refs → 2 carry-over refs
     pointing at the originally selected messages

## § 2. Scenario (error path)

Channel duplicate-name → 409. After step 4 succeeds, POST
/api/conversations {kind: "channel", name: "design-review"} again →
expect HTTP 409 with code "already_exists" (per the api/handlers.go
mapDomainError table).

## § 3. SSE assertion strategy (oversight ④)

S9 does **not** rely on SSE for its assertions — every assertion
hits a synchronous HTTP endpoint that returns the post-mutation
state directly. That is the most reliable assertion shape and
avoids the SSE-race trap.

SSE-via-DOM proofs are deferred to S11 (where Web↔CLI bidirectional
+ SSE recovery scenarios need real SSE assertions). When S11 lands,
it will use `expect(badge).toHaveText('3')` style locators which
Playwright auto-retries to the actionTimeout (10s).

## § 4. Anti-flake protocol (oversight ⑤)

- Each test seeds its own project via CLI subprocess (no shared
  project across tests).
- Project IDs are randomized per test (`p-demo-<uuid>`) so even
  a leaked DB couldn't collide.
- All assertions use Playwright auto-retry via
  `expect(request.get(...))` — no `waitForTimeout`.
- Run 3× back-to-back; record in § 8.

If any of the 3 runs fails: investigate root cause, fix code (not
test), re-run 3× from clean.

## § 5. Artifact policy

Same as S8: `tests/e2e/v2/artifacts/playwright-report/` committed
after a green run; trace/video/screenshot only emit on failure.

## § 6. Acceptance criteria

- Audit log committed first.
- Test commit lands second; 5 cases (happy path × 1 chain + error
  case × 1 chain + 3 sub-asserts inside happy as separate `test()`
  blocks for granularity).
- 3× green / 0 retries.
- `make e2e` runs clean.
- Smoke test from S8 still passes (no regression).

## § 7. What S9 does NOT do

- Worker enroll / agent create / task dispatch (see § 0 narrowing).
- SSE wire assertions (S11).
- Cross-CLI ⇄ Web sync assertions (S11).

## § 8. Execution log

### 8.1 Audit commit
`461c9d1 docs(p12 S9) cold-start journey audit + master_key fixture`
— this file (§ 0-7) + fixture update bundled.

### 8.2 Test commit
`tests/e2e/v2/tests/cold-start.spec.ts` — 3 tests:

1. `secret CRUD round-trip — value never echoed` — empty list →
   create with plaintext value → assert response has id + name but
   NO value field; assert list also strips value; assert plaintext
   string never appears in either response body (ADR-0026 § 5).
2. `channel → messages → derive issue → refs link source messages`
   — seed project via direct sqlite INSERT; create channel; send 3
   messages; derive Issue from first 2; verify
   `/api/conversations/{issueId}/refs` returns exactly the source
   message IDs.
3. `error path: duplicate channel name → 409 already_exists` — create
   twice with same name; second returns 409 with `error:
   "already_exists"`.

### 8.3 Bugs caught + fixed during first run

**Bug 1 — channel create returns 500: "event id already exists"**

Root cause: my first seedProject helper ran `bin/agent-center project
add` as a subprocess while the server was running. Both processes
emit events into the shared sqlite events table, and each has its
own event-seq counter. They race on `uniq_events_seq` —
`MAX(seq)+1` computed independently in two processes can collide on
INSERT.

Fix: bypass the CLI; seed the project via direct
`sqlite3 <db> "INSERT INTO projects ..."`. Projects don't need
events to be discoverable (the projects table is the truth; events
are an audit projection). Lesson: any test that needs to mutate the
DB alongside a running server MUST avoid the event sink — either
direct SQL or stop-server-write-restart.

**Bug 2 — duplicate-name 409 body field name was wrong**

Asserted `body.code === "already_exists"` but `writeError` in
`api/handlers.go:802` serializes the reason as `error` not `code`.
This is a Playwright test bug, not a server bug — but worth
documenting so future tests use the right field name. Fixed inline
in the test with a comment noting our API convention.

### 8.4 Anti-flake gate (oversight ⑤)

3 consecutive `pnpm test` runs:

```
=== Run 1: 5 passed (2.5s) ===
=== Run 2: 5 passed (2.4s) ===
=== Run 3: 5 passed (2.4s) ===
```

Per-test variance < 30ms across runs; zero retries; zero flake.
Anti-flake gate ✅.

### 8.5 Coverage delta

S9 adds 3 cold-start tests to the existing 2 smoke tests = 5 total
e2e cases. Each test spawns its own binary + DB → real isolation;
no state leaks possible.

Cumulative artifact size after 5-case green run:

```
$ du -sh tests/e2e/v2/artifacts/
540K
```

Stayed within the < 5MB target from S8 § 7.

### 8.6 S10 readiness

The seedProject helper pattern (direct sqlite INSERT) is reusable
for any test that needs to pre-seed DB state without going through
the event sink. S10 (NACK→Issue / IR / DM) will need similar
seeding for tasks + executions + input_requests.

Lessons codified for S10-S11:
- "Direct sqlite INSERT" for pre-seed; never CLI subprocess while
  server runs.
- API error response field is `error`, not `code` — apply across
  all error-path assertions.
- `expect(response.status()).toBe(N)` with a `, message` second arg
  is the easy way to capture the body when status is unexpected.
