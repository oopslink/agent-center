package persistence

import (
	"context"
	"testing"
)

// TestMigration0059_BuiltinPlanBackfill proves the ADR-0047 data migration
// (0059_v291_builtin_plan — renumbered from 0058 on the v2.9.1 merge, since
// thread=0057 and ADR-0046 task-state=0058 hold those slots) backfills correctly:
//   - for each existing project, a builtin pool plan (status='running',
//     is_builtin=1, deterministic id 'plan-builtin-'||project_id) is created;
//   - existing backlog tasks with an assignee AND a non-terminal status
//     (plan_id=” AND assignee!=” AND status NOT IN (completed,discarded)) are
//     MOVED into that pool (plan_id set);
//   - unassigned backlog tasks stay in the real backlog (plan_id=”);
//   - terminal (completed/discarded) assigned-backlog tasks stay in the backlog.
//
// Strategy: Down to 56 (pm_plans/pm_tasks/plan_id all exist, builtin column not
// yet). We seed one project + tasks (assigned-non-terminal, unassigned,
// completed-assigned, already-in-another-plan) then full Up to 59 which applies
// 0057+0058+0059; 0059 is the builtin schema + backfill. Assert each task's plan_id.
func TestMigration0059_BuiltinPlanBackfill(t *testing.T) {
	db, err := Open(t.TempDir() + "/adr47.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	mig := NewMigrator(db)

	if err := mig.Up(ctx); err != nil {
		t.Fatalf("first Up: %v", err)
	}
	// Revert to 56: 0057/0058 are data/forward-only; the schema after Down(56) is
	// the pre-0058 shape (no is_builtin column), so we can seed legacy rows.
	if err := mig.Down(ctx, 56); err != nil {
		t.Fatalf("Down(56): %v", err)
	}
	if v, _ := mig.Version(ctx); v != 56 {
		t.Fatalf("version after Down(56): got %d want 56", v)
	}

	// Seed a project.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pm_projects (id, organization_id, name, status, created_by, created_at, updated_at)
		VALUES ('P1', 'org-1', 'Proj', 'active', 'user:a', '2026-06-13T00:00:00Z', '2026-06-13T00:00:00Z')`); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	insert := func(id, status, assignee, planID string) {
		t.Helper()
		_, err := db.ExecContext(ctx, `
			INSERT INTO pm_tasks (id, project_id, title, status, assignee, plan_id, created_by, created_at, updated_at)
			VALUES (?, 'P1', 'do', ?, ?, ?, 'user:a', '2026-06-13T00:00:00Z', '2026-06-13T00:00:00Z')`,
			id, status, assignee, planID)
		if err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	// A pre-existing non-builtin plan that an already-planned task belongs to.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pm_plans (id, project_id, name, description, status, creator_ref, conversation_id, target_date, created_at, updated_at, version)
		VALUES ('plan-x', 'P1', 'Existing', '', 'draft', 'user:a', '', '', '2026-06-13T00:00:00Z', '2026-06-13T00:00:00Z', 1)`); err != nil {
		t.Fatalf("seed plan-x: %v", err)
	}
	insert("T-assigned-open", "open", "agent:a1", "")       // → should MOVE into pool
	insert("T-assigned-running", "running", "agent:a2", "") // → should MOVE into pool (non-terminal)
	insert("T-unassigned", "open", "", "")                  // → stays in backlog
	insert("T-assigned-done", "completed", "agent:a3", "")  // → terminal, stays in backlog
	insert("T-assigned-discarded", "discarded", "agent:a4", "")
	insert("T-in-other-plan", "open", "agent:a5", "plan-x") // → already planned, untouched

	// Apply 0057+0058+0059.
	if err := mig.Up(ctx); err != nil {
		t.Fatalf("re-Up (apply 0057+0058+0059): %v", err)
	}
	if v, _ := mig.Version(ctx); v != 108 {
		t.Fatalf("version after re-Up: got %d want 108", v)
	}

	// The builtin pool exists for P1: status running, is_builtin=1, deterministic id.
	var poolStatus string
	var poolBuiltin int
	if err := db.QueryRowContext(ctx,
		`SELECT status, is_builtin FROM pm_plans WHERE id = 'plan-builtin-P1' AND project_id = 'P1'`).
		Scan(&poolStatus, &poolBuiltin); err != nil {
		t.Fatalf("read builtin pool: %v", err)
	}
	if poolStatus != "running" || poolBuiltin != 1 {
		t.Fatalf("builtin pool = (status=%q, is_builtin=%d), want (running, 1)", poolStatus, poolBuiltin)
	}

	planOf := func(id string) string {
		t.Helper()
		var pid string
		if err := db.QueryRowContext(ctx, `SELECT plan_id FROM pm_tasks WHERE id = ?`, id).Scan(&pid); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		return pid
	}

	// Assigned-non-terminal backlog tasks moved into the pool.
	if got := planOf("T-assigned-open"); got != "plan-builtin-P1" {
		t.Fatalf("T-assigned-open plan_id=%q, want plan-builtin-P1", got)
	}
	if got := planOf("T-assigned-running"); got != "plan-builtin-P1" {
		t.Fatalf("T-assigned-running plan_id=%q, want plan-builtin-P1", got)
	}
	// Unassigned backlog task stays in the real backlog.
	if got := planOf("T-unassigned"); got != "" {
		t.Fatalf("T-unassigned plan_id=%q, want '' (backlog)", got)
	}
	// Terminal assigned-backlog tasks stay in the backlog.
	if got := planOf("T-assigned-done"); got != "" {
		t.Fatalf("T-assigned-done plan_id=%q, want '' (backlog)", got)
	}
	if got := planOf("T-assigned-discarded"); got != "" {
		t.Fatalf("T-assigned-discarded plan_id=%q, want '' (backlog)", got)
	}
	// Already-planned task is untouched.
	if got := planOf("T-in-other-plan"); got != "plan-x" {
		t.Fatalf("T-in-other-plan plan_id=%q, want plan-x", got)
	}
}
