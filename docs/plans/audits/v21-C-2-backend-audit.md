# v2.1-C-2 — unread tracking backend service + endpoints + SSE audit

> Run 2026-05-24 · v2.1-C ST 2 of 3 (per-ST P12 cadence per x9527
> rule for ≥3h work with schema/BC/wire scope). Audit log first;
> impl commit second.

## § 0. Scope

Schema landed in C-1 (`user_conversation_read_state`, migration 0026).
This ST wires the backend so the UI can ask "what have I seen" and
post "I have now seen up to X":

1. `UserConversationReadStateRepository` interface + sqlite impl
   (UPSERT on PK with version CAS; FindByUserAndConv for unread count;
   FindByUserBatch for the per-user dashboard load).
2. `ReadStateService` (domain service in
   `internal/conversation/service/read_state.go`) that owns the
   business invariants:
   - "mark seen up to" only moves forward (never regresses
     `last_seen_message_id`).
   - Emit `conversation.read_state.changed` event inside the same tx
     as the UPSERT (ADR-0014 § 2 same-tx double-write pattern).
3. Two HTTP endpoints on the Web Console:
   - `GET /api/conversations/{id}/unread?user_id=…` → returns
     `{last_seen_message_id, unread_count}`.
   - `POST /api/conversations/{id}/seen` body
     `{user_id, last_seen_message_id}` → bumps the row.
4. **No new SSE plumbing** — the existing
   `EventFanout` already polls `events` table and republishes onto
   the `Bus`, so the new event type rides for free as long as it has
   `conversation_id` in `EventRefs`.
5. App / wiring layer (`internal/cli/app.go` + `webconsole_wiring.go`)
   exposes the repo + service through `HandlerDeps`.

Out of scope for this ST:
- Frontend hook / display badge — that is C-3.
- Backfill of read-state rows for existing conversations: design is
  "never seen = row absent" (per C-1 audit § 1), so no backfill
  needed.
- Multi-device sync semantics — single-user system; tab-vs-tab is
  handled by version CAS retry, not in this ST.

## § 1. Repository surface

New file: `internal/conversation/read_state.go` (port).

```go
type UserConversationReadState struct {
    UserID             IdentityRef
    ConversationID     ConversationID
    LastSeenMessageID  MessageID
    UpdatedAt          time.Time
    Version            int
}

type UserConversationReadStateRepository interface {
    // FindByUserAndConv returns the row or ErrReadStateNotFound when
    // absent (= "never seen").
    FindByUserAndConv(ctx context.Context, userID IdentityRef,
        convID ConversationID) (*UserConversationReadState, error)

    // FindByUserBatch returns every row for a user (for dashboard
    // load — typical N ~ 50, hard-capped to DefaultReadStateLimit).
    FindByUserBatch(ctx context.Context, userID IdentityRef) (
        []*UserConversationReadState, error)

    // Upsert inserts when version == 0; otherwise CAS on (PK, version).
    // Bumps version on success. Returns ErrReadStateVersionConflict
    // when version mismatches.
    Upsert(ctx context.Context, s *UserConversationReadState) error
}
```

Sentinel errors:
- `ErrReadStateNotFound`
- `ErrReadStateVersionConflict`
- `ErrReadStateMessageNotInConversation` — message id refers to a
  conversation other than the one in the URL (raised by the service
  layer when validating the seen-up-to message).

`IdentityRef` already validates the `kind:id` shape (P10), so we lean
on its `Validate()` rather than duplicate.

### 1.1 SQLite impl

File: `internal/conversation/sqlite/read_state_repo.go`.

- `Upsert` uses `INSERT … ON CONFLICT(user_id, conversation_id) DO
  UPDATE SET last_seen_message_id = excluded.last_seen_message_id,
  updated_at = excluded.updated_at, version = version + 1 WHERE
  version = ?`. (SQLite supports `excluded.*` since 3.24.) The CAS
  predicate `WHERE version = ?` makes the optimistic-concurrency
  retry path work; `RowsAffected() == 0` after a successful
  `ON CONFLICT` branch means a concurrent writer bumped the row
  first → `ErrReadStateVersionConflict`.
- Insert branch (version == 0) uses straight `INSERT` and converts a
  unique-constraint failure into `ErrReadStateVersionConflict` (means
  another writer raced us to the first INSERT — caller retries by
  re-reading and trying again).

## § 2. Service surface

File: `internal/conversation/service/read_state.go`.

```go
type ReadStateService struct {
    db        *sql.DB
    rsRepo    conversation.UserConversationReadStateRepository
    msgRepo   conversation.MessageRepository
    sink      *observability.EventSink
    clock     clock.Clock
}

type MarkSeenCommand struct {
    UserID            conversation.IdentityRef
    ConversationID    conversation.ConversationID
    LastSeenMessageID conversation.MessageID
    Actor             observability.Actor
}

type MarkSeenResult struct {
    LastSeenMessageID conversation.MessageID
    Version           int
    EventID           observability.EventID
    Bumped            bool   // false when target ≤ existing (no-op)
}

func (s *ReadStateService) MarkSeen(ctx context.Context,
    cmd MarkSeenCommand) (MarkSeenResult, error)

type UnreadSummary struct {
    LastSeenMessageID conversation.MessageID
    UnreadCount       int
}

func (s *ReadStateService) Unread(ctx context.Context,
    userID conversation.IdentityRef,
    convID conversation.ConversationID) (UnreadSummary, error)
```

### 2.1 MarkSeen invariants (only-forward)

1. Validate Actor + UserID identity refs.
2. Look up the message to confirm `m.ConversationID() == convID`;
   return `ErrReadStateMessageNotInConversation` otherwise. Confirms
   the URL path is consistent with the body (prevents a buggy client
   from poisoning read-state across conversations).
3. Open tx. Inside:
   a. Read existing row (or none).
   b. If `existing != nil && existing.LastSeenMessageID >=
      cmd.LastSeenMessageID` (ULID lexical compare) → return
      `Bumped: false` without UPSERT or event emit. This is the
      common case for multi-tab: tab A already moved the cursor
      forward.
   c. Otherwise UPSERT (Insert when no row; CAS-Update otherwise).
   d. Emit `conversation.read_state.changed` event with payload
      `{user_id, last_seen_message_id, previous_last_seen_message_id}`.
      `Refs.ConversationID` set so the SSE fanout reaches the right
      subscribers. `Refs.MessageID = last_seen_message_id` for the
      tail query path.
4. On version conflict, the service does NOT auto-retry — it returns
   the sentinel so the handler can map to 409 and the client can
   choose to refetch. (Pattern matches existing
   `UpdateParticipants`.)

### 2.2 Unread query

`Unread` returns:
- `LastSeenMessageID`: from the row, or empty when absent.
- `UnreadCount`: count of messages in conv with `id >
  last_seen_message_id` (lexical ULID compare; empty `last_seen_*`
  means "everything is unread" → count of every message). Caps result
  at `MaxUnreadCount = 999` so the frontend can render a `999+` badge
  without an expensive scan.

Implementation: a single `SELECT COUNT(*) FROM messages WHERE
conversation_id = ? AND id > ? LIMIT ?` (capped) — the existing
messages PK index on `id` makes this O(log n + k) where k is the
unread tail (typically small).

## § 3. HTTP surface

### 3.1 `GET /api/conversations/{id}/unread`

Query: `?user_id=identity-ref`. Default to handler's `Actor` when
omitted (single-user system; current actor is the canonical reader).

Response 200:
```json
{
  "conversation_id": "01HVK…",
  "user_id": "user:hayang",
  "last_seen_message_id": "01HVK…",
  "unread_count": 7
}
```

When the row is absent: `last_seen_message_id: ""`, `unread_count` =
total messages (capped at 999).

Errors:
- 400 `invalid_user_id` — identity ref fails validation.
- 404 `not_found` — conversation missing.
- 500 `find_failed` — repo error.

### 3.2 `POST /api/conversations/{id}/seen`

Body:
```json
{ "user_id": "user:hayang", "last_seen_message_id": "01HVK…" }
```

Response 200:
```json
{
  "last_seen_message_id": "01HVK…",
  "version": 5,
  "bumped": true,
  "event_id": "01HVK…"
}
```

Errors:
- 400 `invalid_json` / `invalid_user_id` / `missing_message_id`.
- 404 `not_found` — message id (or its conversation) missing.
- 409 `version_conflict` — concurrent bump; client refetches.
- 422 `message_not_in_conversation` — body inconsistency.

`mapDomainError` extended for the three new sentinels.

## § 4. SSE event shape

EventType: `conversation.read_state.changed`.
Refs: `{conversation_id, message_id}` (message_id = the new
last_seen). Payload:
```json
{
  "conversation_id": "01HVK…",
  "user_id": "user:hayang",
  "last_seen_message_id": "01HVK…",
  "previous_last_seen_message_id": "01HVJ…"
}
```

Why include `previous_*`: lets a future multi-user collaborator UI
show "person X read up to here" without a separate fetch. For now
the v2 single-user case ignores it; included since the cost is one
field.

`EventFanout` republishes via `Bus.Publish`. Per `Bus` semantics, the
event only reaches users with an active `/api/sse/subscribe` for
this `conversation_id`. The C-3 frontend will subscribe on conv
detail mount, so a tab opening the conversation will pick up cursor
moves from a parallel tab.

## § 5. Tests

### 5.1 sqlite repo (`read_state_repo_test.go`)

- `TestReadStateRepo_UpsertInsertThenUpdate` — insert v0, then bump
  with v1 → success, row updated to v2.
- `TestReadStateRepo_UpsertVersionConflict` — pass wrong version → CAS
  fails → `ErrReadStateVersionConflict`.
- `TestReadStateRepo_FindByUserAndConv_Absent` — returns
  `ErrReadStateNotFound`.
- `TestReadStateRepo_FindByUserBatch` — seed 3 conversations for one
  user + 1 for another; assert only the 3 come back.

### 5.2 service (`read_state_test.go`)

- `TestMarkSeen_FirstTime_Inserts` — no row → row created with v=1,
  event emitted with empty `previous_*`.
- `TestMarkSeen_Forward_Bumps` — seed v=1, mark with later message →
  v=2, event emitted with previous filled.
- `TestMarkSeen_Backward_NoOp` — seed v=1 with later message, mark
  with earlier → `Bumped: false`, NO event emitted, row unchanged.
- `TestMarkSeen_SameMessage_NoOp` — seed v=1 with msg X, mark with
  msg X → `Bumped: false`.
- `TestMarkSeen_MessageInWrongConv` →
  `ErrReadStateMessageNotInConversation`.
- `TestMarkSeen_MessageNotFound` → `ErrMessageNotFound` propagated.
- `TestUnread_NoRow_AllUnread` — no row, conv has 3 msgs → unread = 3,
  last_seen = "".
- `TestUnread_WithRow_PartialUnread` — row points at msg #1 of 3 →
  unread = 2.
- `TestUnread_Cap_999_Plus` — seed >999 messages → returns 999.

### 5.3 HTTP handler (`coverage_test.go` additions)

- `TestUnreadHandler_HappyPath` — seeds 2 messages + row pointing at
  msg 1 → JSON has `unread_count: 1`.
- `TestUnreadHandler_AbsentRow` — no row → `last_seen_message_id: ""`.
- `TestUnreadHandler_NotFoundConv` — 404.
- `TestUnreadHandler_RepoUnwired_501` — defensive 501 when
  `ReadStateRepo` nil (mirrors `ProjectRepo` pattern in
  `listProjectsHandler`).
- `TestSeenHandler_HappyPath` — POST bumps row, 200 with
  `event_id` non-empty.
- `TestSeenHandler_InvalidJSON` — 400.
- `TestSeenHandler_MessageNotInConv` — 422.
- `TestSeenHandler_VersionConflict` — pre-seed v=5, POST a
  hand-crafted prior-version payload → 409. (Service does not take
  a version param; we hit this by racing two parallel POSTs via
  `parallel goroutines` — simpler: directly call `Upsert` with stale
  version on the repo to trigger the sentinel + a single POST after.
  Actually the service path always reads current version first, so
  this only fires on a concurrent race. Skip handler-level test for
  this; covered at service level.)
- `TestSeenHandler_DefaultsActorWhenUserIDMissing` — body omits
  `user_id` → uses `d.Actor`.

### 5.4 Migration safety

Already covered in C-1; no new migration test needed.

## § 6. Acceptance criteria

- Audit log committed first.
- Impl commit lands second: repo + service + 2 handlers + wiring +
  tests for all three layers + `mapDomainError` extension.
- `go test ./internal/conversation/...` — green, ≥ 90% coverage on
  the two new files (per F1 90% bar; small enough surface this is
  practically forced).
- `go test ./internal/webconsole/api/...` — green, the four handler
  tests added.
- `go test ./...` — green overall.
- `make lint-vendor` — clean.
- No SSE wiring changes — the existing `EventFanout` carries the new
  event type automatically; verify by adding a single
  service-level test that publishes via the real sink and observes
  via `EventRepo.Find` that the row was written with the right
  `EventType` + `Refs.ConversationID`.

## § 7. Execution log

**Files landed:**

- `internal/conversation/read_state.go` — port (struct +
  `UserConversationReadStateRepository` interface + 3 sentinels).
- `internal/conversation/sqlite/read_state_repo.go` — UPSERT impl
  (separate INSERT/UPDATE branches with version-CAS; unique-constraint
  failure maps to `ErrReadStateVersionConflict`).
- `internal/conversation/service/read_state.go` — `ReadStateService`
  + `MarkSeen` (only-forward, message-in-conv guard) + `Unread`
  (capped at `MaxUnreadCount = 999` via `LIMIT cap+1` trick).
- `internal/webconsole/api/handlers.go` — `unreadHandler` +
  `markSeenHandler` + `readStateUserID` helper + extended
  `mapDomainError` for the two new sentinels (409 + 422). Two new
  fields on `HandlerDeps`.
- `internal/webconsole/api/server.go` — two new routes.
- `internal/cli/app.go` — repo + service wired into `App`.
- `internal/cli/webconsole_wiring.go` — both wiring sites updated
  (build + run paths).
- `internal/webconsole/api/server_test.go` — `setupAPI` wires the
  read-state deps.

**Tests added:**

- `internal/conversation/sqlite/read_state_repo_test.go` — 7 cases
  (insert→update, version conflict, duplicate-insert conflict,
  absent, batch query, empty batch, nil rejection).
- `internal/conversation/service/read_state_test.go` — 19 cases
  covering: first-time insert, forward bump, backward / same no-op,
  message-not-in-conv guard, message-not-found propagation, invalid
  actor / user_id / missing fields, unread (no row / partial / all
  read / 999+ cap), invalid user_id on Unread, event emit shape, +
  three fake-repo failure-injection tests (find err, upsert err,
  Unread find err) + nil-clock constructor default.
- `internal/webconsole/api/read_state_test.go` — 15 cases covering:
  happy path GET + POST, absent row, query-string user_id, invalid
  user_id, 501 when service unwired, no-op backward, invalid JSON,
  missing message id, invalid user_id POST, 422 message-in-wrong-conv,
  404 message-not-found, defaults user_id to actor (verified by
  reading back the repo row).

**Coverage (per file, post-commit):**

- `read_state.go` (service): `NewReadStateService 100%` /
  `MarkSeen 97.2%` / `Unread 91.7%` / `countUnread 87.5%`. The
  remaining 12.5% on `countUnread` is the driver-level QueryRow
  failure path (only reachable by closing the db mid-test — out of
  scope for service-level coverage).
- `read_state_repo.go` (sqlite): `NewReadStateRepo 100%` /
  `FindByUserAndConv 100%` / `FindByUserBatch 83.3%` /
  `Upsert 90.5%` / `scanReadState 85.7%`.
- Package totals: `internal/conversation/service` 92.6% /
  `internal/conversation/sqlite` 94.5% — both comfortably above the
  90% DoD bar.

**Verification:**

- `go test ./internal/conversation/...` — green.
- `go test ./internal/webconsole/api/...` — green (all 15 new
  read-state HTTP tests pass).
- `go test ./...` — green overall (includes the e2e + integration
  suites; nothing else regressed because the schema + service +
  routes are additive).
- `make lint-vendor` — clean.

**Cadence:** audit log shipped first (commit `f53c790`); impl shipped
as a second commit per the per-ST P12 cadence rule. Next ST is **C-3**
— frontend `useUnread` hook + per-conversation badge + auto-mark-seen
on conv mount + e2e (`task #94`).
