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

## § 8. Execution log + M3 closure

To be filled by the test commit.
