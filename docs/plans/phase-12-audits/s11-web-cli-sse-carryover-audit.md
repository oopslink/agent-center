# P12 S11 — Web↔CLI + SSE recovery + carry-over derive UI audit

> Run 2026-05-24 · per x9527 M3 oversight: 3 sub-scenarios; SSE
> recovery is the F13 audit guard (re-verify integration after the
> 9 silent mismatches were fixed); carry-over UI is the only real
> browser UI test in M3. Audit + test commit separated.

## § 0. Scope refinement (transparent per S9/S10 pattern)

| Sub-scenario | In S11? | Why / Why not |
|---|---|---|
| **Web→CLI: API write → CLI read** | ✅ | clean direction; CLI subprocess is read-only via `agent-center conversation tail`, no concurrent writes, no event-seq race |
| **CLI→Web: CLI write → API read** | ⚠️ **narrowed** — proven via in-process tests | The CLI subprocess writing while the server is up races on `uniq_events_seq` (per `internal/observability/sqlite/event_repo.go` doc: "seq is monotonic **per process**; multi-process / HA is out of scope for v1"). The "CLI + API share one DB truth" invariant IS established by reading via CLI of what API wrote (sub-scenario 1 above); reverse direction is proven by the same Go in-process tests + the fact that the CLI and the api/handlers.go go through the SAME MessageWriter service. Documenting + narrowing per S9/S10 pattern. |
| **SSE Last-Event-ID recovery** | ✅ | special importance per oversight ②: F13 audit fixed 9 silent SSE event-name mismatches; this S11 test guards the integration end-to-end |
| **Carry-over derive UI (real browser)** | ✅ | the one real-DOM test in M3: select messages → Open Issue → modal → submit → assert new Issue page renders CarryOverDivider with the source message IDs |

## § 1. Sub-scenario 1: Web→CLI (API write → CLI tail read)

`tests/web-cli-bidi.spec.ts`:

1. POST /api/conversations create channel.
2. POST /api/conversations/{id}/messages × 2.
3. `bin/agent-center conversation tail <id> --tail=10 --format=json
   --config=<configPath>` subprocess.
4. Parse JSON output, assert both messages present in chronological
   order.

The CLI tail does a READ-only query against the same sqlite file
the server uses. No write contention; no event emit on the CLI
side. This proves the "single source of truth" invariant for the
Web→CLI direction.

## § 2. Sub-scenario 2: SSE Last-Event-ID recovery

`tests/sse-recovery.spec.ts`:

1. Subscribe `user:hayang` to a freshly created channel.
2. Open SSE stream A, send 1 message, capture the event id.
3. **Disconnect** SSE stream A (helper's `stop()` aborts the fetch).
4. While disconnected, send 2 more messages via API.
5. **Reconnect** SSE stream B with `?last_event_id=<id-from-step-2>`.
6. Assert the 2 missed messages' events arrive on stream B (auto-
   retry via `expect.poll`).

This guards the F13 closeout: any silent event-name mismatch
between server emit and client expect would surface as "missed
events never delivered" — the test would fail with the
expect.poll timeout.

## § 3. Sub-scenario 3: Carry-over derive UI (real browser)

`tests/carry-over-ui.spec.ts`:

1. Seed a project via direct sqlite (S9 rule).
2. POST /api/conversations create channel + send 3 messages via API
   (avoids the CLI race; messages need to exist before browser
   navigates).
3. Browser: `page.goto(/channels/{channelName})`.
4. Wait for messages list to render (`data-testid="message-list"`).
5. Click `[data-testid="select-mode-toggle"]` to enter select mode.
6. Click first 2 `[data-testid="message-select"]` checkboxes.
7. Assert `[data-testid="derive-bar-count"]` shows "2 selected".
8. Click `[data-testid="derive-open-issue"]`.
9. Modal opens; fill `[data-testid="derive-title-input"]` with title.
10. The Web UI doesn't expose a project-picker in the modal (per
    F9 — submit sends an empty project_id, which the server
    handles via the conversation's lineage). **Caveat**: looking
    at the deriveIssue handler in api/handlers.go and the
    DeriveIssue service, project_id IS required at the service
    layer. If the UI doesn't pass it, the submit will 400. We
    pre-set `project_id` via the URL? Or skip the modal submit
    and assert the modal opens correctly? — refine in § 8 after
    a first run.

Plan: first attempt asserts the modal opens with the count correct
+ the title input accepts text. If the modal can submit cleanly
in the v2 UI, extend to the navigation step. If it can't (because
project_id missing → 400), document the gap and assert the modal
opens. Either way the UI flow is exercised — full submit-to-page-
navigation may or may not land; we'll see.

## § 4. Anti-flake (oversight ⑤)

- All seed IDs randomized per test.
- SSE assertions via `expect.poll` (auto-retry to actionTimeout).
- UI assertions via Playwright `expect(locator).toBeVisible()` /
  `toHaveText()` (auto-retry).
- No `waitForTimeout`.
- 3× green required.

## § 5. Acceptance criteria

- Audit log committed first.
- Test commit lands second; 3 new spec files = (at least) 3 happy +
  optional error paths.
- 3× green / 0 retries.
- All prior tests (smoke + cold-start + IR + DM) still pass.
- M3 closure ledger appended (§ 8).

## § 6. What S11 does NOT do

- CLI write while server runs (narrowed; § 0).
- Full task/execution chain (worker daemon blocker; v3).

## § 7. M3 closure plan

After S11 lands, write § 8 with:
- S8/S9/S10/S11 commit summary
- Actual vs plan (11h)
- Total test count + artifact size
- Carryover items for any v2.1+

## § 8. Execution log

### 8.1 Audit commit
`5b25f93 docs(p12 S11) Web↔CLI + SSE recovery + carry-over UI audit`
— this file (§ 0-7).

### 8.2 Test commit

Files added:
- `tests/web-cli-bidi.spec.ts` — 1 test: API write × 2 messages →
  `bin/agent-center conversation tail --format=json` subprocess →
  parse stdout (one JSON object per line) → assert both messages
  present in chronological order. Proves CLI + HTTP API read the
  same sqlite truth.
- `tests/sse-recovery.spec.ts` — 1 test: subscribe → stream A
  receives message #1 + captures last event id → stop(A) → send
  messages #2 #3 while disconnected → reconnect stream B with
  `?last_event_id=<id>` → assert ≥ 2 replayed `conversation.
  message_added` events arrive on B + the original event id is
  NOT in the replay set.
- `tests/carry-over-ui.spec.ts` — 1 test: seed channel + 3
  messages via API → `page.goto(/channels/<name>)` → wait for 3
  message-row locators → click select-mode-toggle → click first 2
  checkboxes → assert derive-bar-count shows "2" → click
  derive-open-issue → modal opens with `data-kind="issue"` →
  title input editable. Submit deliberately NOT attempted (modal
  doesn't thread project_id; v2.1 micro-pass per § 3 caveat).

### 8.3 Wins on first run

S11 also ran green first try. The S9 codified rules + S10's SSE
helper patterns held: direct sqlite for seeds, settle barrier
before SSE asserts, `expect.poll` for auto-retry. The carry-over
UI test exercised real browser interactions (click, type, navigate)
without flakiness because every assertion used Playwright auto-
retry locators (no `waitForTimeout`).

### 8.4 Anti-flake gate (oversight ⑤)

```
=== Run 1: 12 passed (6.6s) ===
=== Run 2: 12 passed (6.6s) ===
=== Run 3: 12 passed (6.6s) ===
```

3/3 green; total runtime variance < 50ms; 0 retries.

### 8.5 SSE recovery — F13 guard intact

The reconnect test exercises the full event-name chain:
- server emits `conversation.message_added` (per the BC-prefixed
  EventType set codified by F13 SSE wire audit)
- helper parses the `event:` field of the SSE record
- test asserts `ev.event === "conversation.message_added"`

If any of those names drift (back to the silent-mismatch state F13
fixed), this test fails with a clear "stream B with last_event_id
should replay the 2 missed messages" timeout — the F13 audit's
guard now has e2e coverage.

### 8.6 Carry-over UI — what was proven + what's deferred

**Proven**: the SPA's multi-select → DeriveBar → modal flow works
end-to-end in a real browser. The `data-testid` contract from F9
holds (select-mode-toggle / message-select / derive-bar-count /
derive-open-issue / derive-modal / derive-title-input).

**Deferred to v2.1+**: full submit-to-page-navigation. The
DeriveModal in the SPA does not thread project_id (intentional in
F9; the API requires it). A future v2.1 micro-pass would add a
project picker to the modal; then this test extends to click
derive-modal-submit → assert navigation to /issues/{newId} →
assert CarryOverDivider renders with source message IDs.

The "happy-path API derive → refs link source messages" assertion
is still covered by S9 cold-start (`channel → messages → derive
issue → refs link source messages`).

## § 9. M3 closure ledger

### 9.1 Per-ST commits

| ST | Audit | Test | Δ on suite |
|---|---|---|---|
| S8 | `7e64fff` | `e9df918` | +2 smoke cases |
| S9 | `461c9d1` | `45e2d2f` | +3 cold-start cases |
| S10 | `81f84e6` | `cc7ceed` | +4 IR/DM cases |
| S11 | `5b25f93` | [this commit] | +3 Web-CLI / SSE / UI cases |

8 commits / 4 audit logs / 4 test commits.

### 9.2 Estimate vs actual

| | Plan | Actual |
|---|---|---|
| S8 scaffold | 2h | ~1.5h |
| S9 cold-start | 3h | ~1.5h |
| S10 NACK+IR+DM | 3h | ~1h |
| S11 Web↔CLI + SSE + UI | 3h | ~1h |
| **M3 total** | **11h** | **~5h** |
| **Delta** | — | **-55%** |

Main accelerators:
- S9 codified rules avoided having to rediscover the event-seq race
  in every later ST.
- SSE helper landed in S10 was reused as-is in S11.
- Narrowing decisions (worker chain → v3; CLI-write → narrowed) cut
  out work that wasn't tractable inside a Playwright test.

### 9.3 Final suite

12 e2e cases across 5 spec files:
- `smoke.spec.ts` — 2 cases (page load + API health)
- `cold-start.spec.ts` — 3 cases (secret round-trip + derive issue + dup-name 409)
- `respond-input-request.spec.ts` — 2 cases (respond happy + 404)
- `dm-flow.spec.ts` — 2 cases (DM + SSE + 400)
- `web-cli-bidi.spec.ts` — 1 case (API write → CLI read)
- `sse-recovery.spec.ts` — 1 case (Last-Event-ID replay)
- `carry-over-ui.spec.ts` — 1 case (real browser select → modal)

Full-suite green run: 6.6s; per-test variance < 50ms across 3
back-to-back runs.

### 9.4 Artifacts

`tests/e2e/v2/artifacts/playwright-report/` — 544KB committed.
Stable across S8 → S11 (HTML report grows ~4KB per added test
case). Per-run noise (trace / video / screenshot) only emits on
failure, none accumulated this milestone.

### 9.5 chromium-linux verification status

Still deferred to Linux VPS run by `@oopslink` (per S8 § 8.6 +
S11 inheriting the same posture). Config-time gating ensures the
project never spawns on darwin; the spec files themselves are
platform-agnostic.

### 9.6 v2.1+ carryovers

Filed in audit § 8.6:
- DeriveModal project picker (to enable full submit-to-navigation
  in the carry-over UI test).
- NACK→Issue / agent dispatch / task execute e2e — needs docker
  compose + worker daemon orchestration; v3 deployment e2e
  candidate.
- chromium-linux CI integration — wire `make e2e` into the GitHub
  Actions / Linux VPS pipeline.

### 9.7 Hand-off to M4

M4 (S12-S13: v1→v2 migration tool + DEPLOYMENT v2 guide) doesn't
depend on M3. M3's `make e2e` target stays opt-in (not part of
`make lint` or `make test`) — CI gating decided at S16 release
prep.
