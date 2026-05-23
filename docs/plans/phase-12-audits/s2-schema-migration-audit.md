# P12 S2 — schema migration round-trip + v1 column-absence audit

> Run 2026-05-24 · per x9527 oversight: write round-trip tests that
> prove (a) `up` then `down` then `up` lands on a stable schema, (b)
> migration 0025 truly drops the Bridge BC tables, and (c) v1
> vendor columns like `vendor_msg_ref`, `feishu_at`, `vendor_user_id`
> are absent after full `up`. Audit log commit lands SEPARATELY from
> the test commit.

## § 0. Scope

S1 (v1 vendor grep audit) already confirmed:

- All v1 vendor references in code / contrib / lint allowlist accounted for.
- "**Schema cleanup is already complete from P10** — no NEW migration needed in S2 (this audit confirms it; S2 ST can be downscoped to 'verify migration up/down still pass' rather than write a new drop migration)." — S1 § 2.2.

S2 turns that bullet into executable assertions so any future regression
that re-introduces v1 columns / tables is caught by `go test`.

## § 1. Migration inventory

| # | name | role |
|---|---|---|
| 0001 | init | v1 base — events / workers / projects / mappings / proposals / conversations / messages |
| 0002 | taskruntime | v1 — tasks / task_executions / input_requests / supervisor_invocations |
| 0003 | discussion_issues | v1 — discussion_issues |
| 0004 | observability_projections | v1 — projections |
| **0005** | **bridge_feishu_outbound** | **v1 — identities + channel_bindings (v2 still needs identities) + feishu_delivery_ledger + bridge_subscription_cursors** |
| 0006 | cognition | v1 — decisions / memory / scheduler |
| 0007 | v2_worker_config_fields | v2 — workers.{concurrency_json, discovery_json, capabilities_json}; drops workers.capabilities |
| 0008 | v2_bootstrap_tokens | v2 |
| 0009 | v2_agent_instances | v2 |
| 0010 | v2_task_executions_agent_instance | v2 — task_executions.agent_instance_id |
| 0011 | v2_supervisor_invocations_agent_instance | v2 — supervisor_invocations.agent_instance_id |
| 0012 | v2_user_secrets | v2 |
| **0020** | **v2_conversation_schema_reset** | **v2 — drops conversations.{primary_channel_hint, primary_channel_thread_key, title}; adds {name, description, parent_conversation_id, participants, created_by, archived_at, archived_by}** |
| **0021** | **v2_identity_simplify** | **v2 — drops `channel_bindings` table; deletes identities rows whose kind ∉ {user, agent, system}** |
| 0022 | v2_conversation_message_reference | v2 — `conversation_message_reference` |
| **0023** | **v2_message_strip_vendor** | **v2 — drops messages.vendor_msg_ref + its UNIQUE index** |
| 0024 | v2_kind_rename | v2 — `UPDATE conversations SET kind='channel' WHERE kind='group_thread'` |
| **0025** | **v2_drop_bridge_feishu_tables** | **v2 — drops `feishu_delivery_ledger` + `bridge_subscription_cursors` (Bridge BC physical tables)** |

Highest applied version after a full Up == **25**. The migrator's existing
`TestMigrator_VersionTracksApplied` already asserts that landmark.

## § 2. Expected post-`Up` schema state (assertions to write)

### 2.1 Tables that MUST NOT exist after full Up

| Table | Dropped by |
|---|---|
| `channel_bindings` | 0021 |
| `feishu_delivery_ledger` | 0025 |
| `bridge_subscription_cursors` | 0025 |

### 2.2 Columns that MUST NOT exist after full Up

| Table | Column | Dropped by |
|---|---|---|
| `workers` | `capabilities` | 0007 (replaced by `capabilities_json`) |
| `conversations` | `primary_channel_hint` | 0020 |
| `conversations` | `primary_channel_thread_key` | 0020 |
| `conversations` | `title` | 0020 (replaced by `name` + `description`) |
| `messages` | `vendor_msg_ref` | 0023 |

### 2.3 Data invariants after full Up

- `conversations.kind` never equals `'group_thread'` (renamed to `'channel'` by 0024).
- `identities.kind` only ever ∈ `{'user', 'agent', 'system'}` (0021 DELETE).

### 2.4 Tables / columns that MUST exist after full Up

Already covered by `TestMigrator_UpCreatesAllPhase1Tables` +
`TestMigrator_UpCreatesV2Tables`. S2 will not duplicate these — it
focuses on the *negative* assertions in 2.1-2.3, plus the round-trip
state shape.

## § 3. Round-trip property

Sequence: `Up → Down(0) → Up`.

Assertions across the two "post-Up" snapshots:

1. `Version()` == 25 in both.
2. The set of user tables (excluding `schema_migrations`) is identical.
3. For each table, the set of column names is identical.

This catches a class of bug where a `down.sql` forgets to undo something
the `up.sql` did, leaving residue after the second Up.

## § 4. Migration 0025 specifically (Bridge BC drop)

Two assertions:

1. **Post-`Up`** (final state): `feishu_delivery_ledger` and
   `bridge_subscription_cursors` are absent from `sqlite_master`.
2. **`Up → Down(24)`** (revert just 0025): both tables come back, with
   the schema from `0025_v2_drop_bridge_feishu_tables.down.sql`. We
   verify by checking `sqlite_master` table existence + presence of the
   defining indexes (`idx_feishu_ledger_message`,
   `idx_feishu_ledger_status_pending`, `uniq_feishu_ledger_message`).

The second assertion is what makes 0025 a true round-trip — a regression
in its down.sql would break the v1→v2 migration rollback path that the
S12 migration tool depends on (per phase-12-plan-detail § M4).

## § 5. Test plan

New file: `internal/persistence/migration_round_trip_test.go`

Tests:

| Name | What it asserts |
|---|---|
| `TestMigrations_FullRoundTrip` | § 3: Up → Down(0) → Up → schema/table/column sets stable; Version()==25 both times |
| `TestMigration_0025_BridgeFeishuTablesDrop` | § 4: post-Up tables absent; after Down to 24 they reappear with expected indexes |
| `TestMigrations_V1ColumnsAbsent` | § 2.2: PRAGMA table_info over the affected tables; each listed v1 column NOT present |
| `TestMigrations_V1TablesAbsent` | § 2.1: each listed v1 table NOT present |
| `TestMigrations_V1KindValuesAbsent` | § 2.3: `conversations.kind` never `'group_thread'`, `identities.kind` only in 3-set after up (sanity on a freshly-up DB — empty rows, so this is a schema-level guarantee via the 0021/0024 statements running clean) |

We re-use the in-memory sqlite pattern from existing migrator_test.go
(t.TempDir + Open + NewMigrator).

## § 6. Acceptance criteria

- S2 audit log committed first (this file).
- Tests committed second; `go test ./internal/persistence/...` green.
- `go test ./...` overall still green (no collateral breakage from added tests).
- `go vet ./...` clean.
- `make lint-vendor` still clean (S1 wiring preserved).

## § 7. What S2 does NOT do

- **No new migration files** — schema cleanup was completed in P10.
  Adding a new drop migration now would only delete things that are
  already gone post-0025, i.e. a noop.
- **No data-shape verification of in-tree rows** — these tests run on
  empty DBs; they prove the migration scripts themselves are correct,
  not that any seeded data conforms.
- **No change to the migrator itself** — `Up` / `Down` / `Version()` API
  is stable; we just call them more.

## § 8. Execution log

### 8.1 Audit commit
`b5ead7d docs(p12 S2) schema migration round-trip + v1 column-absence audit` — this file as initially drafted (§ 0-7).

### 8.2 Test commit
`internal/persistence/migration_round_trip_test.go` — 5 tests + 6 helpers.

Test results (`go test ./internal/persistence/ -run TestMigration -v -count=1`):

```
=== RUN   TestMigrations_FullRoundTrip
--- PASS: TestMigrations_FullRoundTrip (0.06s)
=== RUN   TestMigration_0025_BridgeFeishuTablesDrop
--- PASS: TestMigration_0025_BridgeFeishuTablesDrop (0.01s)
=== RUN   TestMigrations_V1ColumnsAbsent
--- PASS: TestMigrations_V1ColumnsAbsent (0.01s)
=== RUN   TestMigrations_V1TablesAbsent
--- PASS: TestMigrations_V1TablesAbsent (0.01s)
=== RUN   TestMigrations_V1KindValuesAbsent
--- PASS: TestMigrations_V1KindValuesAbsent (0.03s)
PASS
ok      github.com/oopslink/agent-center/internal/persistence  0.339s
```

Full suite + lint sweep:
- `go test ./...` — all packages green.
- `go vet ./...` — clean.
- `make lint-vendor` — `clean (all hits whitelisted)` (S1 wiring preserved).

### 8.3 Seed fixture note
`TestMigrations_V1KindValuesAbsent` required the v1 conversations row
to include `opened_at` (NOT NULL on v1 `conversations` table) — caught
on first run, fixed in the test commit. The v1 column inventory in § 1
references this implicitly via "v1 base — conversations / messages",
but worth flagging: any future test that seeds v1 rows must consult
`0001_init.up.sql` for the full NOT NULL set.

### 8.4 What changed in v1 schema that the v2 migrations rely on

Documented here for the S12 v1→v2 migration tool:

| Migration | Operation | Why |
|---|---|---|
| 0007 | DROP `workers.capabilities` | replaced by `capabilities_json` |
| 0020 | DROP `conversations.{primary_channel_hint, primary_channel_thread_key, title}` | vendor撤回 + name/description shape |
| 0021 | DROP `channel_bindings` table | vendor撤回 |
| 0021 | DELETE `identities WHERE kind NOT IN (user, agent, system)` | ADR-0033 (4 → 3 kinds) |
| 0023 | DROP `messages.vendor_msg_ref` + uniq idx | vendor撤回 |
| 0024 | UPDATE `conversations.kind 'group_thread' → 'channel'` | ADR-0032 |
| 0025 | DROP `feishu_delivery_ledger` + `bridge_subscription_cursors` | Bridge BC撤回 |

S12 acceptance test will replay this list against a real v1 snapshot.
