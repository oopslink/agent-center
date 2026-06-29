package sqlite

import (
	"context"
	"database/sql"
	"testing"

	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/usage"
)

// rollupEnv is a migrated in-memory DB plus the repos the rollup tests need.
type rollupEnv struct {
	ctx    context.Context
	db     *sql.DB
	events *UsageEventRepo
	roll   *Rollup
	daily  *AgentActivityDailyRepo
}

func setupRollup(t *testing.T) rollupEnv {
	t.Helper()
	d, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(d).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return rollupEnv{
		ctx: context.Background(), db: d,
		events: NewUsageEventRepo(d), roll: NewRollup(d), daily: NewAgentActivityDailyRepo(d),
	}
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

// insertTask / insertActionLog / insertIssue / insertPlan / insertCommit /
// insertConv / insertMessage write minimal valid rows directly (the rollup reads
// these tables raw; going through every BC's domain layer would be noise here).
func insertTask(t *testing.T, db *sql.DB, id, project, createdBy, at string) {
	mustExec(t, db, `INSERT INTO pm_tasks (id, project_id, title, status, created_by, created_at, updated_at, version)
	                 VALUES (?,?,?,?,?,?,?,1)`, id, project, "t", "open", createdBy, at, at)
}
func insertActionLog(t *testing.T, db *sql.DB, id, taskID, agentRef, at string) {
	mustExec(t, db, `INSERT INTO pm_task_action_logs (id, task_id, occurred_at, action, actor_ref, agent_ref, note)
	                 VALUES (?,?,?,?,?,?,'')`, id, taskID, at, "started", agentRef, agentRef)
}
func insertIssue(t *testing.T, db *sql.DB, id, project, createdBy, at string) {
	mustExec(t, db, `INSERT INTO pm_issues (id, project_id, title, status, created_by, created_at, updated_at, version)
	                 VALUES (?,?,?,?,?,?,?,1)`, id, project, "i", "open", createdBy, at, at)
}
func insertPlan(t *testing.T, db *sql.DB, id, project, creatorRef, at string) {
	mustExec(t, db, `INSERT INTO pm_plans (id, project_id, name, status, creator_ref, created_at, updated_at, version)
	                 VALUES (?,?,?,?,?,?,?,1)`, id, project, "p", "active", creatorRef, at, at)
}
func insertCommit(t *testing.T, db *sql.DB, id string, seq int, actor, refsJSON, at string) {
	mustExec(t, db, `INSERT INTO events (id, occurred_at, seq, event_type, refs, actor, payload, created_at)
	                 VALUES (?,?,?,'commit',?,?,'{}',?)`, id, at, seq, refsJSON, actor, at)
}
func insertConv(t *testing.T, db *sql.DB, id, kind, ownerRef, at string) {
	var owner any = ownerRef
	if ownerRef == "" {
		owner = nil
	}
	mustExec(t, db, `INSERT INTO conversations (id, kind, status, owner_ref, opened_at, created_at, updated_at, version)
	                 VALUES (?,?,?,?,?,?,?,1)`, id, kind, "open", owner, at, at, at)
}
func insertMessage(t *testing.T, db *sql.DB, id, convID, sender, at string) {
	mustExec(t, db, `INSERT INTO messages (id, conversation_id, sender_identity_id, content_kind, content, direction, posted_at, created_at)
	                 VALUES (?,?,?,'text','hi','outbound',?,?)`, id, convID, sender, at, at)
}

func appendUsage(t *testing.T, env rollupEnv, id, agentRef, project, model, at string, in, out, cr, cw int64) {
	t.Helper()
	ev := usage.UsageEvent{
		ID: id, AgentRef: agentRef, ProjectID: project, Model: model,
		Tokens:     usage.TokenCounts{Input: in, Output: out, CacheRead: cr, CacheWrite: cw},
		CostMicros: 1000, TS: tm(at), Source: usage.SourceReport,
	}
	if err := env.events.Append(env.ctx, ev); err != nil {
		t.Fatal(err)
	}
}

// TestRollupAllSources is the headline round-trip: one agent on one day in one
// project produces exactly one rollup row fusing the five activity sources
// (events_count = 5) with the usage token/cost totals. Human (user:) rows in the
// same slice are excluded.
func TestRollupAllSources(t *testing.T) {
	env := setupRollup(t)
	const ag = "agent:agent-aaa"
	const day = "2026-06-20"
	const at = "2026-06-20T10:00:00Z"

	insertTask(t, env.db, "task-1", "p1", ag, at)
	insertActionLog(t, env.db, "L1", "task-1", ag, at)
	insertIssue(t, env.db, "issue-1", "p1", ag, at)
	insertPlan(t, env.db, "plan-1", "p1", ag, at)
	insertCommit(t, env.db, "e1", 1, ag, `{"project_id":"p1"}`, at)
	insertConv(t, env.db, "conv-1", "task", "pm://tasks/task-1", at)
	insertMessage(t, env.db, "m1", "conv-1", ag, at)
	appendUsage(t, env, "u1", ag, "p1", "claude-opus-4-8", at, 100, 50, 10, 5)
	appendUsage(t, env, "u2", ag, "p1", "claude-opus-4-8", at, 200, 80, 0, 0)

	// noise: a human-authored issue + a human commit must NOT count.
	insertIssue(t, env.db, "issue-h", "p1", "user:bob", at)
	insertCommit(t, env.db, "e-h", 2, "user:bob", `{"project_id":"p1"}`, at)

	if _, err := env.roll.RunIncremental(env.ctx); err != nil {
		t.Fatal(err)
	}

	rows, err := env.daily.ListByAgent(env.ctx, ag, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1: %+v", len(rows), rows)
	}
	got := rows[0]
	if got.AgentRef != ag || got.Day != day || got.ProjectID != "p1" {
		t.Fatalf("key = (%s,%s,%s)", got.AgentRef, got.Day, got.ProjectID)
	}
	if got.EventsCount != 5 {
		t.Fatalf("events_count = %d, want 5 (action+issue+plan+commit+message; human excluded)", got.EventsCount)
	}
	if got.TokensIn != 300 || got.TokensOut != 130 || got.CacheTokens != 15 || got.CostMicros != 2000 {
		t.Fatalf("usage totals wrong: in=%d out=%d cache=%d cost=%d", got.TokensIn, got.TokensOut, got.CacheTokens, got.CostMicros)
	}
}

// TestRollupIdempotentAndIncremental verifies a re-run does not double-count, and
// a newly-appended event recomputes only its bucket to the correct total.
func TestRollupIdempotentAndIncremental(t *testing.T) {
	env := setupRollup(t)
	const ag = "agent:agent-bbb"
	appendUsage(t, env, "u1", ag, "p1", "claude-opus-4-8", "2026-06-20T10:00:00Z", 100, 0, 0, 0)

	if _, err := env.roll.RunIncremental(env.ctx); err != nil {
		t.Fatal(err)
	}
	// Re-run with no new rows: same totals, no double-count.
	if _, err := env.roll.RunIncremental(env.ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ := env.daily.ListByAgent(env.ctx, ag, "", "")
	if len(rows) != 1 || rows[0].TokensIn != 100 {
		t.Fatalf("after re-run: %+v (want 1 row, tokens_in=100)", rows)
	}

	// Append a second event to the same bucket; incremental run must reach 300.
	appendUsage(t, env, "u2", ag, "p1", "claude-opus-4-8", "2026-06-20T12:00:00Z", 200, 0, 0, 0)
	st, err := env.roll.RunIncremental(env.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.BucketsRecomputed != 1 {
		t.Fatalf("buckets recomputed = %d, want 1 (only the touched slice)", st.BucketsRecomputed)
	}
	rows, _ = env.daily.ListByAgent(env.ctx, ag, "", "")
	if len(rows) != 1 || rows[0].TokensIn != 300 {
		t.Fatalf("after incremental: %+v (want tokens_in=300)", rows)
	}
}

// TestRollupAgentRefNormalization pins the member-id-vs-entity-id concern: one row
// per source for the SAME canonical ref must all fold into one agent_ref bucket.
func TestRollupAgentRefNormalization(t *testing.T) {
	env := setupRollup(t)
	const ag = "agent:agent-ccc"
	const at = "2026-06-21T08:00:00Z"
	insertTask(t, env.db, "task-2", "p2", ag, at)
	insertActionLog(t, env.db, "L2", "task-2", ag, at)
	insertIssue(t, env.db, "issue-2", "p2", ag, at)
	insertPlan(t, env.db, "plan-2", "p2", ag, at)
	insertCommit(t, env.db, "e2", 1, ag, `{"project_id":"p2"}`, at)
	insertConv(t, env.db, "conv-2", "task", "pm://tasks/task-2", at)
	insertMessage(t, env.db, "m2", "conv-2", ag, at)

	if _, err := env.roll.RunIncremental(env.ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ := env.daily.ListByAgent(env.ctx, ag, "", "")
	if len(rows) != 1 || rows[0].EventsCount != 5 {
		t.Fatalf("normalization: want one bucket with events_count=5, got %+v", rows)
	}
}

// TestRollupProjectBucketing checks project derivation: a no-project usage event
// and a DM message (no project owner) land in the "" bucket, distinct from p1.
func TestRollupProjectBucketing(t *testing.T) {
	env := setupRollup(t)
	const ag = "agent:agent-ddd"
	const at = "2026-06-22T09:00:00Z"
	// p1 usage + a no-project (converse) usage event.
	appendUsage(t, env, "u1", ag, "p1", "claude-opus-4-8", at, 10, 0, 0, 0)
	appendUsage(t, env, "u2", ag, "", "claude-opus-4-8", at, 20, 0, 0, 0)
	// DM message (owner_ref NULL) → "" bucket activity.
	insertConv(t, env.db, "dm-1", "dm", "", at)
	insertMessage(t, env.db, "m1", "dm-1", ag, at)

	if _, err := env.roll.RunIncremental(env.ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ := env.daily.ListByAgent(env.ctx, ag, "", "")
	if len(rows) != 2 {
		t.Fatalf("want 2 buckets (p1 + \"\"), got %+v", rows)
	}
	// ListByAgent orders by (day, project_id); "" sorts before "p1".
	empty, p1 := rows[0], rows[1]
	if empty.ProjectID != "" || empty.TokensIn != 20 || empty.EventsCount != 1 {
		t.Fatalf("\"\" bucket wrong: %+v", empty)
	}
	if p1.ProjectID != "p1" || p1.TokensIn != 10 || p1.EventsCount != 0 {
		t.Fatalf("p1 bucket wrong: %+v", p1)
	}
}

// TestRollupTokenCostByTaskProject is the T580 regression: a task-scoped usage
// event whose ingest-time project_id was lost ("" — report_usage's silent fallback)
// is bucketed by the TASK's real project (via LEFT JOIN pm_tasks on task_id), NOT by
// the empty usage_events.project_id. A converse event (no task_id) still lands in "".
func TestRollupTokenCostByTaskProject(t *testing.T) {
	env := setupRollup(t)
	const ag = "agent:agent-t580"
	const at = "2026-06-23T09:00:00Z"
	insertTask(t, env.db, "task-x", "px", ag, at)

	// Task-scoped usage whose project_id was wrongly stored as "" at ingest.
	if err := env.events.Append(env.ctx, usage.UsageEvent{
		ID: "u-task", AgentRef: ag, ProjectID: "", TaskID: "task-x", Model: "claude-opus-4-8",
		Tokens: usage.TokenCounts{Input: 100, Output: 40}, CostMicros: 1000, TS: tm(at), Source: usage.SourceReport,
	}); err != nil {
		t.Fatal(err)
	}
	// Converse usage (no task) → stays in the no-project bucket.
	if err := env.events.Append(env.ctx, usage.UsageEvent{
		ID: "u-conv", AgentRef: ag, ProjectID: "", TaskID: "", Model: "claude-opus-4-8",
		Tokens: usage.TokenCounts{Input: 7}, CostMicros: 500, TS: tm(at), Source: usage.SourceReport,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := env.roll.RunIncremental(env.ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ := env.daily.ListByAgent(env.ctx, ag, "", "")
	byProject := map[string]usage.AgentActivityDaily{}
	for _, r := range rows {
		byProject[r.ProjectID] = r
	}
	// The task-scoped tokens/cost reattribute to "px" (NOT "").
	px, ok := byProject["px"]
	if !ok || px.TokensIn != 100 || px.TokensOut != 40 || px.CostMicros != 1000 {
		t.Fatalf("px bucket = %+v (want in=100 out=40 cost=1000)", px)
	}
	// The converse tokens stay in the "" bucket; the task ones must NOT leak here.
	empty := byProject[""]
	if empty.TokensIn != 7 || empty.CostMicros != 500 {
		t.Fatalf("\"\" bucket = %+v (want only the converse: in=7 cost=500)", empty)
	}
}
