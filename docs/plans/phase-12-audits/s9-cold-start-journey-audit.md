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

To be appended by the test commit.
