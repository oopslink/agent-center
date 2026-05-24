# v2.1-C-1 — unread tracking schema migration audit

> Run 2026-05-24 · v2.1-C ST 1 of 3 (per-ST P12 cadence per x9527
> rule for ≥3h work with schema/BC/wire scope). Audit log first;
> impl commit second.

## § 0. Scope

v2.1-C overall: `user_conversation_read_state` table + backend
service + 2 HTTP endpoints + SSE event + frontend wire. Split
into 3 STs.

This ST (**C-1**) lands the schema layer only:

1. New migration `0026_user_conversation_read_state.up.sql` +
   `.down.sql`.
2. Round-trip test in `migration_round_trip_test.go` (S2 pattern):
   the existing `TestMigrations_FullRoundTrip` already validates
   round-trip stability for whatever migrations exist; adding the
   new pair gets picked up automatically. We additionally add a
   `TestMigration_0026_UserConvReadStateShape` to assert the
   table has the expected columns + indexes after Up.
3. **No repo / service yet** — that's C-2.

## § 1. Table shape

```sql
CREATE TABLE user_conversation_read_state (
    user_id                  TEXT NOT NULL,
    conversation_id          TEXT NOT NULL,
    last_seen_message_id     TEXT NOT NULL,
    updated_at               TEXT NOT NULL,
    version                  INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (user_id, conversation_id)
);
CREATE INDEX idx_ucrs_conversation
    ON user_conversation_read_state (conversation_id);
```

Decisions:
- **Composite PK** `(user_id, conversation_id)` — each user has at
  most one read-state row per conv (UPSERT pattern in C-2).
- **`last_seen_message_id TEXT NOT NULL`** — the message id the
  user has seen up to (per message ULID ordering). NOT NULL because
  there's no point inserting a row before the user has actually
  seen anything; "never seen" = row absent.
- **`updated_at`** — bump on every UPSERT for audit / SSE event
  ordering.
- **`version`** — sqlite version-CAS pattern per repo conventions.
  Concurrency from multi-tab simultaneous mark-seen is rare but
  handled.
- **`idx_ucrs_conversation`** — supports the planned
  `WHERE conversation_id = ?` query when computing per-conv unread
  count for a specific user (also covers the read-state-changed
  SSE fanout in C-2 which needs to list all interested users for a
  conv).
- **No FK declarations** per `conventions § 9.w` — referential
  integrity enforced at app layer.
- **No `seen_at` separate from `updated_at`** — they're the same
  event; collapse to one column to avoid a redundant audit-log
  field.

## § 2. Tests

### 2.1 Augment `internal/persistence/migration_round_trip_test.go`

The existing `TestMigrations_FullRoundTrip` snapshots the schema
and validates Up→Down(0)→Up stability. Adding 0026 to the
migrations dir will be picked up automatically; the test will
fail if Down(0026) doesn't cleanly drop the table.

### 2.2 New `TestMigration_0026_UserConvReadStateShape`

Asserts after `Up()`:
- Table `user_conversation_read_state` exists.
- Columns: `user_id` / `conversation_id` / `last_seen_message_id` /
  `updated_at` / `version` (PRAGMA table_info).
- Indexes: `idx_ucrs_conversation` exists in sqlite_master.
- PRIMARY KEY enforced on `(user_id, conversation_id)` — verified
  by INSERT-DELETE pattern: INSERT a row, attempt INSERT with
  same PK → must fail.

### 2.3 Update `Version()` baseline expectation

`migrator_test.go` `TestMigrator_VersionTracksApplied` currently
asserts `Version() == 25`. Bump to 26 in this commit.

## § 3. Acceptance criteria

- Audit log committed first.
- Schema commit lands second; 0026 up/down + 1 new round-trip test
  + 1 migrator_test version bump.
- `go test ./internal/persistence/...` green (all migration tests).
- `go test ./...` green overall (schema is additive; no callsites
  yet).
- `make lint-vendor` clean.

## § 4. Execution log

**Files landed:**

- `internal/persistence/migrations/0026_user_conversation_read_state.up.sql`
  — CREATE TABLE + `idx_ucrs_conversation` index, exactly as § 1.
- `internal/persistence/migrations/0026_user_conversation_read_state.down.sql`
  — DROP INDEX + DROP TABLE (both `IF EXISTS` for clean re-run).
- `internal/persistence/migration_round_trip_test.go` — new
  `TestMigration_0026_UserConvReadStateShape` asserts table presence,
  five-column shape (`user_id` / `conversation_id` /
  `last_seen_message_id` / `updated_at` / `version`), the
  `idx_ucrs_conversation` index, and PK enforcement via duplicate
  INSERT. Also bumped two embedded numeric checks (Up baseline 25→26).

**Version bump cascade (25 → 26):**

The migration count is referenced in several places — every one
flipped 25→26 in the same impl commit so the test suite stays
consistent:

| File | Spot |
|------|------|
| `internal/persistence/migrator_test.go` | `TestMigrator_VersionTracksApplied` final assertion |
| `internal/persistence/migration_round_trip_test.go` | Up baseline check + comment |
| `internal/cli/handlers_migrate_v1_to_v2.go` | `targetSchemaVersion` const + comment now explains "carries install to latest v2.x schema" |
| `internal/cli/handlers_migrate_v1_to_v2_test.go` | "target schema version: 26" / "new schema version: 26" + post-apply assertion |
| `tests/integration/integration_test.go` | `TestINT2_MigrationIdempotent` post-Up assertion |
| `docs/migration/v1-to-v2.md` | All operator-facing example output (`replace_all` on "target  schema version: 25" → 26) |

The frozen S12 migration-tool audit was intentionally left untouched.

**Verification:**

- `go test ./internal/persistence/...` — green (round-trip + shape +
  version-tracks-applied all pass).
- `go test ./internal/cli/... ./tests/integration/...` — green
  (migrate v1→v2 dry-run / apply / idempotent / refuses / fresh-v2
  all assert 26).
- `go test ./...` — green overall (no callsites yet; schema is purely
  additive, no behavior changed).
- `make lint-vendor` — clean.

**Cadence:** audit log shipped first (commit `ff0920e`); impl shipped
as a second commit per the per-ST P12 cadence rule for ≥3h
schema/BC/wire work. Next ST is **C-2** — backend service + repo +
2 HTTP endpoints + SSE event (`task #93`).
