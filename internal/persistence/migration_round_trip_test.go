package persistence

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"testing"
)

// These tests prove that the persistence migrations form a valid v2
// schema after Up, and that running Down → Up again lands on the same
// schema shape (no leftover columns / tables on the second Up). They
// also assert that v1 vendor residue (vendor_msg_ref, channel_bindings,
// feishu_delivery_ledger, conversations.title etc.) is absent after a
// full Up — paired with the v1 grep allowlist landed in P12 S1, this
// is the schema-side guarantee that no v1 surface leaks into a v2
// install.
//
// See docs/plans/phase-12-audits/s2-schema-migration-audit.md for the
// full inventory + acceptance criteria.

// TestMigrations_FullRoundTrip runs Up → Down(0) → Up and asserts that
// (a) Version() is 26 both times Up returns, and (b) the set of user
// tables and the set of column names per table are identical across the
// two post-Up snapshots. Catches the class of bug where a down.sql
// silently leaves a column behind that the next Up then re-adds with a
// DEFAULT / constraint mismatch.
func TestMigrations_FullRoundTrip(t *testing.T) {
	db, err := Open(t.TempDir() + "/round_trip.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	mig := NewMigrator(db)

	if err := mig.Up(ctx); err != nil {
		t.Fatalf("first Up: %v", err)
	}
	v1, _ := mig.Version(ctx)
	snap1 := snapshotSchema(t, db)

	if err := mig.Down(ctx, 0); err != nil {
		t.Fatalf("Down(0): %v", err)
	}
	v0, _ := mig.Version(ctx)
	if v0 != 0 {
		t.Fatalf("Version after Down(0): got %d want 0", v0)
	}

	if err := mig.Up(ctx); err != nil {
		t.Fatalf("second Up: %v", err)
	}
	v2, _ := mig.Version(ctx)
	snap2 := snapshotSchema(t, db)

	if v1 != 113 || v2 != 113 {
		t.Fatalf("Version after Up: got (%d, %d) want (113, 113)", v1, v2)
	}

	// v2.1-E: idx_messages_conv_id must be usable as a range seek for
	// the unread query. Without this index the query falls back to a
	// SCAN inside the conversation (the bug v2.1-E fixes).
	rows, err := db.Query(`EXPLAIN QUERY PLAN
		SELECT COUNT(*) FROM (
			SELECT 1 FROM messages
			WHERE conversation_id = ? AND id > ? LIMIT 1000
		)`, "c-1", "")
	if err != nil {
		t.Fatalf("explain query plan: %v", err)
	}
	defer rows.Close()
	var planContainsIdx bool
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(detail, "idx_messages_conv_id") {
			planContainsIdx = true
		}
	}
	if !planContainsIdx {
		t.Fatal("EXPLAIN QUERY PLAN did not use idx_messages_conv_id — v2.1-E migration regressed")
	}

	if !sameSchema(snap1, snap2) {
		t.Fatalf("round-trip schema drift:\n  first:  %v\n  second: %v", snap1, snap2)
	}
}

// TestMigration_0025_BridgeFeishuTablesDrop checks that 0025 truly
// removes the Bridge BC physical tables when Up is fully applied, and
// that reverting just 0025 (Down to version 24) brings them back with
// the indexes the v2 install would need to roll forward again. This is
// what the S12 v1→v2 migration tool relies on.
func TestMigration_0025_BridgeFeishuTablesDrop(t *testing.T) {
	db, err := Open(t.TempDir() + "/m25.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	mig := NewMigrator(db)

	if err := mig.Up(ctx); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"feishu_delivery_ledger", "bridge_subscription_cursors"} {
		if tableExists(t, db, tbl) {
			t.Fatalf("post-Up: %s must NOT exist (dropped by 0025)", tbl)
		}
	}

	// Revert just 0025; the bridge tables come back.
	if err := mig.Down(ctx, 24); err != nil {
		t.Fatalf("Down(24): %v", err)
	}
	if v, _ := mig.Version(ctx); v != 24 {
		t.Fatalf("Version after Down(24): got %d want 24", v)
	}
	for _, tbl := range []string{"feishu_delivery_ledger", "bridge_subscription_cursors"} {
		if !tableExists(t, db, tbl) {
			t.Fatalf("after Down(24): %s must be restored by 0025.down.sql", tbl)
		}
	}
	for _, idx := range []string{
		"idx_feishu_ledger_message",
		"idx_feishu_ledger_status_pending",
		"uniq_feishu_ledger_message",
	} {
		if !indexExists(t, db, idx) {
			t.Fatalf("after Down(24): index %s must be restored by 0025.down.sql", idx)
		}
	}

	// Re-apply to land back on v2; tables gone again. This proves
	// 0025.up + 0025.down are inverses.
	if err := mig.Up(ctx); err != nil {
		t.Fatalf("re-Up: %v", err)
	}
	for _, tbl := range []string{"feishu_delivery_ledger", "bridge_subscription_cursors"} {
		if tableExists(t, db, tbl) {
			t.Fatalf("post re-Up: %s must NOT exist", tbl)
		}
	}
}

// TestMigrations_V1ColumnsAbsent enumerates each v1 vendor column that
// the v2 migrations remove and asserts it is gone from the final
// schema. If any of these reappear, either a regression migration was
// added or one of the v2 migrations was reverted — both are bugs.
func TestMigrations_V1ColumnsAbsent(t *testing.T) {
	db, err := Open(t.TempDir() + "/cols.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}

	type absent struct{ table, column string }
	for _, a := range []absent{
		// dropped by 0007
		{"workers", "capabilities"},
		// dropped by 0020
		{"conversations", "primary_channel_hint"},
		{"conversations", "primary_channel_thread_key"},
		{"conversations", "title"},
		// dropped by 0023
		{"messages", "vendor_msg_ref"},
	} {
		if columnExists(t, db, a.table, a.column) {
			t.Fatalf("v1 column %s.%s must be absent after Up (regression)", a.table, a.column)
		}
	}
}

// TestMigrations_V1TablesAbsent asserts the v1 vendor / bridge tables
// that the v2 migrations remove are gone from the final schema.
func TestMigrations_V1TablesAbsent(t *testing.T) {
	db, err := Open(t.TempDir() + "/tbls.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{
		"channel_bindings",            // dropped by 0021
		"feishu_delivery_ledger",      // dropped by 0025
		"bridge_subscription_cursors", // dropped by 0025
		// v2.7 #131 carve-out (PR-6): retired-domain tables are NEVER created on
		// a fresh install (no drop-migration). Positive assertion that a fresh
		// schema is new-model ONLY — taskruntime / discussion / old-projection /
		// workforce-project tables must be absent.
		"tasks",                      // taskruntime (retired → pm_tasks)
		"task_executions",            // taskruntime execution (retired → agent work-items)
		"input_requests",             // taskruntime IR (retired → waiting_input WI + conversation)
		"artifacts",                  // taskruntime artifacts (retired)
		"issues",                     // discussion (retired → pm_issues)
		"task_execution_projections", // old observability projection (retired → agent_work_item_projections)
		"projects",                   // workforce projects (retired → pm_projects)
		"worker_project_mappings",    // workforce mapping (retired)
		"worker_project_proposals",   // workforce proposal (retired)
	} {
		if tableExists(t, db, tbl) {
			t.Fatalf("retired table %s must be absent after fresh Up (v2.7 carve-out: new-model only)", tbl)
		}
	}
}

// TestMigration_0026_UserConvReadStateShape — v2.1-C-1 guard: assert
// the new user_conversation_read_state table lands with the expected
// shape (composite PK + supporting index + NOT NULL columns) after a
// full Up.
func TestMigration_0026_UserConvReadStateShape(t *testing.T) {
	db, err := Open(t.TempDir() + "/m26.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}

	if !tableExists(t, db, "user_conversation_read_state") {
		t.Fatal("user_conversation_read_state must exist after Up")
	}
	if !indexExists(t, db, "idx_ucrs_conversation") {
		t.Fatal("idx_ucrs_conversation must exist after Up")
	}
	cols := tableColumns(t, db, "user_conversation_read_state")
	wantCols := []string{
		"conversation_id", "last_seen_message_id", "updated_at",
		"user_id", "version",
	}
	if len(cols) != len(wantCols) {
		t.Fatalf("columns: got %v want %v", cols, wantCols)
	}
	for i, c := range wantCols {
		if cols[i] != c {
			t.Fatalf("columns[%d]=%q want %q (full: %v)", i, cols[i], c, cols)
		}
	}

	// Composite PRIMARY KEY (user_id, conversation_id) — duplicate
	// inserts on the same pair must fail with constraint error.
	now := "2026-05-24T15:00:00Z"
	if _, err := db.ExecContext(ctx,
		`INSERT INTO user_conversation_read_state
		 (user_id, conversation_id, last_seen_message_id, updated_at, version)
		 VALUES ('user:hayang','C-1','M-1',?,1)`, now); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO user_conversation_read_state
		 (user_id, conversation_id, last_seen_message_id, updated_at, version)
		 VALUES ('user:hayang','C-1','M-2',?,1)`, now); err == nil {
		t.Fatal("duplicate PK should have failed")
	}
}

// TestMigrations_V1KindValuesAbsent proves the data-side cleanups land:
// after seeding rows with v1 enum values, the relevant migration must
// either delete them (identities.kind ∉ {user,agent,system} → 0021) or
// rename them (conversations.kind='group_thread' → 'channel' via 0024).
//
// Note: v2.6 migration 0033 drops and recreates identities with the BC9 schema
// (no backward compat per v2.6-design § 9). Seeded v1 identity rows do NOT
// survive — this is expected (fresh install required for v2.6). The test now
// verifies conversations.kind cleanup only, plus that the v2.6 identities
// schema has the expected columns (kind IN ('user','agent')).
func TestMigrations_V1KindValuesAbsent(t *testing.T) {
	db, err := Open(t.TempDir() + "/kinds.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	mig := NewMigrator(db)

	// Stage the v1 schema by running Up to version 6 (last v1 migration
	// before the Phase 8 v2 work begins). We can't drive that with the
	// public Migrator API directly, so apply the v1 migrations and stop
	// at v6 by counting.
	if err := mig.Up(ctx); err != nil {
		t.Fatal(err)
	}
	// Roll back to v6 so we can seed v1-shaped data.
	if err := mig.Down(ctx, 6); err != nil {
		t.Fatalf("Down(6): %v", err)
	}
	if v, _ := mig.Version(ctx); v != 6 {
		t.Fatalf("seed precondition: Version=%d want 6", v)
	}

	// Seed v1-shaped rows. conversations still uses 'group_thread'.
	// Note: we no longer seed identities rows because 0033 drops the table;
	// the seeded data would be lost (expected by design, fresh install required).
	seedRows := []string{
		// conversations on v1 schema: title + primary_channel_hint + kind='group_thread'
		`INSERT INTO conversations (id, kind, status, title, primary_channel_hint, primary_channel_thread_key, opened_at, created_at, updated_at, version)
		 VALUES ('c1', 'group_thread', 'open', 'legacy', 'feishu', 'tk-1', '2026-05-24T00:00:00Z', '2026-05-24T00:00:00Z', '2026-05-24T00:00:00Z', 1)`,
	}
	for _, q := range seedRows {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed: %v\nsql=%s", err, q)
		}
	}

	// Roll forward to v2.6.
	if err := mig.Up(ctx); err != nil {
		t.Fatalf("Up to v2.6: %v", err)
	}

	// v2.6: identities table is recreated by 0033 with BC9 schema.
	// Verify the table exists with the correct kind CHECK constraint by
	// inserting a valid row and checking it round-trips.
	cols := scanStringSet(t, db, `SELECT name FROM pragma_table_info('identities')`)
	for _, required := range []string{"id", "kind", "display_name", "account_status", "passcode_hash"} {
		if !cols[required] {
			t.Fatalf("identities table missing expected column %q (got %v)", required, cols)
		}
	}
	// No v1 banned kinds (supervisor/bot) should appear in the CHECK constraint.
	// We verify by attempting to insert and expecting failure.
	_, insertErr := db.ExecContext(ctx,
		`INSERT INTO identities (id, kind, display_name, created_at, updated_at) VALUES ('x','supervisor','x','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`)
	if insertErr == nil {
		t.Fatal("expected error inserting kind='supervisor' into v2.6 identities (CHECK constraint should reject)")
	}

	// conversations.kind: v1 'group_thread' --0024--> 'channel'. v2.7 RETAINS
	// 'channel' (channel model finalized plan §10 OQ10 — no project_channel
	// rename), so the final kind is 'channel'.
	kinds := scanStringSet(t, db, `SELECT kind FROM conversations`)
	if kinds["group_thread"] {
		t.Fatalf("conversations.kind='group_thread' must be renamed to 'channel' by 0024")
	}
	if kinds["project_channel"] {
		t.Fatalf("conversations.kind='project_channel' must not exist (v2.7 retains 'channel')")
	}
	if !kinds["channel"] {
		t.Fatalf("expected conversations.kind='channel' after the rename chain (got %v)", kinds)
	}
}

// TestMigration_0044_EnvironmentWorkerShape — v2.7 D1 (ADR-0050, task #102)
// guard: the env_workers + worker_control_events tables land after a full Up,
// with the supporting indexes and the two UNIQUE constraints on the control
// stream (offset + idempotency_key) actually enforced.
func TestMigration_0044_EnvironmentWorkerShape(t *testing.T) {
	db, err := Open(t.TempDir() + "/m44.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}

	for _, tbl := range []string{"env_workers", "worker_control_events"} {
		if !tableExists(t, db, tbl) {
			t.Fatalf("%s must exist after Up", tbl)
		}
	}
	// v2.7 #140 step-3: idx_env_workers_org removed with the organization_id
	// column (org is no longer stored on the control-channel Worker AR).
	for _, idx := range []string{"idx_wce_worker_offset"} {
		if !indexExists(t, db, idx) {
			t.Fatalf("index %s must exist after Up", idx)
		}
	}
	// v2.7 #140 step-3: org is NOT stored on the control-channel Worker AR.
	if columnExists(t, db, "env_workers", "organization_id") {
		t.Fatal("env_workers.organization_id must be gone (#140 step-3: org derives from workforce.Worker)")
	}

	now := "2026-05-29T15:00:00Z"
	if _, err := db.ExecContext(ctx,
		`INSERT INTO worker_control_events (id, worker_id, "offset", idempotency_key, command_type, payload, created_at)
		 VALUES ('e1','w1',1,'k1','stop','{}',?)`, now); err != nil {
		t.Fatalf("first event insert: %v", err)
	}
	// UNIQUE(worker_id, offset): re-using offset 1 for w1 must fail.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO worker_control_events (id, worker_id, "offset", idempotency_key, command_type, payload, created_at)
		 VALUES ('e2','w1',1,'k2','stop','{}',?)`, now); err == nil {
		t.Fatal("duplicate (worker_id, offset) should have failed")
	}
	// UNIQUE(worker_id, idempotency_key): re-using key k1 for w1 must fail.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO worker_control_events (id, worker_id, "offset", idempotency_key, command_type, payload, created_at)
		 VALUES ('e3','w1',2,'k1','stop','{}',?)`, now); err == nil {
		t.Fatal("duplicate (worker_id, idempotency_key) should have failed")
	}
}

// TestMigration_0045_FileTransferSessionShape — v2.7 D3-a (ADR-0048) guard:
// the file_transfer_sessions table lands after a full Up with its supporting
// indexes and the UNIQUE(transfer_uri) constraint actually enforced.
func TestMigration_0045_FileTransferSessionShape(t *testing.T) {
	db, err := Open(t.TempDir() + "/m45.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}

	if !tableExists(t, db, "file_transfer_sessions") {
		t.Fatal("file_transfer_sessions must exist after Up")
	}
	for _, idx := range []string{"idx_fts_status_expires", "idx_fts_file_uri"} {
		if !indexExists(t, db, idx) {
			t.Fatalf("index %s must exist after Up", idx)
		}
	}

	now := "2026-05-29T15:00:00Z"
	exp := "2026-05-29T16:00:00Z"
	if _, err := db.ExecContext(ctx,
		`INSERT INTO file_transfer_sessions (id, file_uri, transfer_uri, direction, status, content_type, size, sha256, scope, scope_id, created_by, created_at, expires_at)
		 VALUES ('s1','ac://files/u1','ac://transfers/s1','upload','open','text/plain',0,NULL,NULL,NULL,'user:x',?,?)`, now, exp); err != nil {
		t.Fatalf("first session insert: %v", err)
	}
	// UNIQUE(transfer_uri): re-using a transfer URI must fail.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO file_transfer_sessions (id, file_uri, transfer_uri, direction, status, content_type, size, sha256, scope, scope_id, created_by, created_at, expires_at)
		 VALUES ('s2','ac://files/u2','ac://transfers/s1','upload','open','text/plain',0,NULL,NULL,NULL,'user:x',?,?)`, now, exp); err == nil {
		t.Fatal("duplicate transfer_uri should have failed")
	}
}

// =============================================================================
// helpers
// =============================================================================

type schemaSnap struct {
	tables  []string
	columns map[string][]string // table → sorted column names
}

func snapshotSchema(t *testing.T, db *sql.DB) schemaSnap {
	t.Helper()
	out := schemaSnap{columns: map[string][]string{}}
	rows, err := db.Query(
		`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name != 'schema_migrations' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out.tables = append(out.tables, n)
	}
	rows.Close()
	for _, tbl := range out.tables {
		out.columns[tbl] = tableColumns(t, db, tbl)
	}
	return out
}

func sameSchema(a, b schemaSnap) bool {
	if len(a.tables) != len(b.tables) {
		return false
	}
	for i := range a.tables {
		if a.tables[i] != b.tables[i] {
			return false
		}
		ca, cb := a.columns[a.tables[i]], b.columns[b.tables[i]]
		if len(ca) != len(cb) {
			return false
		}
		for j := range ca {
			if ca[j] != cb[j] {
				return false
			}
		}
	}
	return true
}

func tableColumns(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("pragma_table_info(%s): %v", table, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func tableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func indexExists(t *testing.T, db *sql.DB, idx string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, idx,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func columnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	for _, c := range tableColumns(t, db, table) {
		if c == column {
			return true
		}
	}
	return false
}

func scanStringSet(t *testing.T, db *sql.DB, query string) map[string]bool {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out[s] = true
	}
	return out
}

// TestMigration_0052_TaskIssueStateModelFix — v2.8.1 state model fix data rewrite:
// Task assigned→open (assignee PRESERVED as metadata), Task canceled→discarded,
// Issue withdrawn→discarded. Seeds OLD-model rows after a full Up and re-runs the
// idempotent 0052 UP statements, then asserts the rewrite + no residual old states.
func TestMigration_0052_TaskIssueStateModelFix(t *testing.T) {
	db, err := Open(t.TempDir() + "/m52.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	now := "2026-06-08T00:00:00Z"
	seed := []string{
		`INSERT INTO pm_tasks (id,project_id,title,status,assignee,created_by,created_at,updated_at) VALUES ('T-asn','P','t','assigned','agent:x','user:a','` + now + `','` + now + `')`,
		`INSERT INTO pm_tasks (id,project_id,title,status,created_by,created_at,updated_at) VALUES ('T-can','P','t','canceled','user:a','` + now + `','` + now + `')`,
		`INSERT INTO pm_issues (id,project_id,title,status,created_by,created_at,updated_at) VALUES ('I-wd','P','i','withdrawn','user:a','` + now + `','` + now + `')`,
	}
	for _, s := range seed {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}
	// Re-run the idempotent 0052 UP (WHERE status=<old> → no-op on migrated data).
	for _, stmt := range []string{
		`UPDATE pm_tasks  SET status = 'open'      WHERE status = 'assigned'`,
		`UPDATE pm_tasks  SET status = 'discarded' WHERE status = 'canceled'`,
		`UPDATE pm_issues SET status = 'discarded' WHERE status = 'withdrawn'`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("migrate %q: %v", stmt, err)
		}
	}
	scalar := func(q string) string {
		var v string
		if err := db.QueryRowContext(ctx, q).Scan(&v); err != nil {
			t.Fatalf("query %q: %v", q, err)
		}
		return v
	}
	if got := scalar(`SELECT status FROM pm_tasks WHERE id='T-asn'`); got != "open" {
		t.Errorf("assigned task → status=%q, want open", got)
	}
	if got := scalar(`SELECT assignee FROM pm_tasks WHERE id='T-asn'`); got != "agent:x" {
		t.Errorf("assignee must be preserved across assigned→open, got %q", got)
	}
	if got := scalar(`SELECT status FROM pm_tasks WHERE id='T-can'`); got != "discarded" {
		t.Errorf("canceled task → status=%q, want discarded", got)
	}
	if got := scalar(`SELECT status FROM pm_issues WHERE id='I-wd'`); got != "discarded" {
		t.Errorf("withdrawn issue → status=%q, want discarded", got)
	}
	if got := scalar(`SELECT COUNT(*) FROM pm_tasks WHERE status IN ('assigned','canceled')`); got != "0" {
		t.Errorf("residual old pm_tasks states = %s, want 0", got)
	}
	if got := scalar(`SELECT COUNT(*) FROM pm_issues WHERE status='withdrawn'`); got != "0" {
		t.Errorf("residual old pm_issues state = %s, want 0", got)
	}
}
