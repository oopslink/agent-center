# P12 S12 — v1→v2 migration tool audit

> Run 2026-05-24 · per x9527 M4 oversight: `agent-center migrate
> v1-to-v2` subcommand (P12 open question 5 decided); dry-run +
> apply modes; idempotent; bridge tables export to JSON archive
> before drop; tests with seeded v1 sqlite fixture. Audit log +
> code commit separated.

## § 0. Scope

S12 packages the v1→v2 sqlite migration as a one-shot CLI
subcommand. It composes:

1. **Schema migration**: replay `internal/persistence/migrations/`
   0001 → 0025 via `persistence.Migrator.Up()`. This is what S2
   already verifies via round-trip tests.
2. **Bridge archive**: BEFORE applying migration 0025 (which drops
   `feishu_delivery_ledger` + `bridge_subscription_cursors`),
   export every row to `<output-dir>/bridge-archive-<ts>.json`.
   This preserves the v1 vendor audit trail per x9527 oversight ①
   "v1 用户 vendor 历史保留 audit 用".
3. **Identity refactor verification**: 0021 already DELETEs
   v1-kind identities (kind ∉ {user,agent,system}). S12 verifies
   the post-state matches v2 invariants (no leftover v1 kinds; all
   ids have `kind:id` prefix — flagging bare ids as warnings since
   v1 sometimes wrote `hayang` without `user:`).
4. **Conversation kind rename**: 0024 renames `group_thread →
   channel`. Verified by post-migration query.

## § 1. Architecture decisions

### 1.1 CLI placement

Per P12 open question 5 → `agent-center migrate v1-to-v2`. The
existing `migrate` command is a leaf (Run handler); the router
forbids leaf-with-subcommands. Refactor:

- **Before**: `migrate` (leaf with `--target=N`)
- **After**: `migrate` (group) → `migrate up` (leaf, existing
  behavior) + `migrate v1-to-v2` (leaf, new)

Users invoking the old `migrate --target=N` now must use `migrate
up --target=N`. This is a small breaking change for v2 (acceptable
in a major version; called out in v2.0 release notes by S14).

### 1.2 Dry-run vs apply

- `--dry-run`: compute + print the planned operations + counts
  (bridge rows to archive, current schema version, target version)
  WITHOUT mutating anything.
- `--apply`: actually run the operations.
- Default (neither flag): print usage error requesting one
  explicitly. **Refuses to act silently.**

### 1.3 Idempotency

If `Version() == 25` already, the tool reports "already at v2; no
action" and exits 0. The bridge-archive step is skipped (tables
don't exist anymore). Apply twice = same outcome.

### 1.4 Bridge archive path

`--archive-dir <path>`: defaults to `<sqlite-dir>/migration-archive`.
JSON file `bridge-archive-<UTC-rfc3339>.json` with shape:
```json
{
  "exported_at": "2026-05-24T07:13:00Z",
  "sqlite_path": "/var/lib/agent-center/agent-center.db",
  "schema_version_before": 24,
  "feishu_delivery_ledger": [/* rows */],
  "bridge_subscription_cursors": [/* rows */]
}
```

If the tables don't exist (db never had v1 bridge migration; or
already at v2), the archive file is NOT written.

## § 2. CLI surface

```
agent-center migrate v1-to-v2 [--config=<path>] [--dry-run | --apply]
                              [--archive-dir=<path>]
```

Examples (per ADR-0038 help-discoverability):
- Dry-run: `agent-center migrate v1-to-v2 --config=/etc/agent-center/config.yaml --dry-run`
- Apply : `agent-center migrate v1-to-v2 --config=/etc/agent-center/config.yaml --apply`

## § 3. Tests (oversight ②)

`internal/cli/handlers_migrate_v1_to_v2_test.go`:

| Case | What it asserts |
|---|---|
| `TestMigrateV1ToV2_DryRunReportsCounts` | Seed a v1 sqlite (run migrations to v6 + INSERT bridge rows); dry-run reports planned-archive row counts; DB unchanged afterwards |
| `TestMigrateV1ToV2_ApplyArchivesAndUpgrades` | Same seed; apply; assert archive JSON file exists + contains the seeded rows; assert `Version() == 25`; assert bridge tables gone via PRAGMA table_list |
| `TestMigrateV1ToV2_Idempotent` | Apply once; apply again; second invocation exits 0 with "already at v2" message + no new archive file |
| `TestMigrateV1ToV2_RefusesSilently` | Neither --dry-run nor --apply → exit 2 usage error |
| `TestMigrateV1ToV2_DryRunOnFreshV2` | Apply against a freshly-migrated v2 DB → "already at v2" + no archive |

## § 4. Acceptance criteria

- Audit log committed first.
- Code commit lands second; CLI subcommand + tests + Makefile
  smoke if appropriate.
- All 5 tests pass + `go test ./...` overall green + `go vet`
  clean + `make lint-vendor` clean.
- Help output for `agent-center migrate v1-to-v2` shows usage +
  examples per ADR-0038.

## § 5. What S12 does NOT do

- Identity prefix transform (bare id → kind:id): v1 already used
  `kind:id` formal form per `internal/conversation/identity` tests;
  if a v1 install has bare ids, the post-migration sanity check
  surfaces them as a warning + lists rows for manual fix. Not
  automated because the kind for a bare id is ambiguous (was
  `hayang` a user, agent, or system?).
- Worker / AgentInstance / Secret data migration: these tables
  didn't exist in v1 (added in v2 migrations 0007-0012); they just
  start empty.
- Backup: S13 documents the user-side backup procedure (`cp
  agent-center.db agent-center.db.pre-migration`) before invoking
  the tool. The tool does NOT auto-backup — that's the operator's
  responsibility per documented procedure.

## § 6. Execution log

To be appended by the code commit.
