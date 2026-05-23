package cognition_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cognition"
	cognitiondb "github.com/oopslink/agent-center/internal/persistence/cognition"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

// =============================================================================
// scanInvocationRow error paths via injected bad rows
// =============================================================================

func TestInvocationRepo_Scan_BadStartedAt(t *testing.T) {
	db := openTestDB(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO supervisor_invocations
		(id, scope_kind, scope_key, trigger_event_ids, status, hard_timeout_seconds,
		 started_at, token_usage, decisions_made, prompt_blob_ref,
		 created_at, updated_at, version)
		VALUES ('01HBS', 'task', 'T-1', '["E1"]', 'running', 180,
		        'not-a-time', '{}', 0, '',
		        '2026-05-22T00:00:00Z', '2026-05-22T00:00:00Z', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	repo := cognitiondb.NewInvocationRepo(db)
	if _, err := repo.FindByID(context.Background(), "01HBS"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestInvocationRepo_Scan_BadCreatedAt(t *testing.T) {
	db := openTestDB(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO supervisor_invocations
		(id, scope_kind, scope_key, trigger_event_ids, status, hard_timeout_seconds,
		 started_at, token_usage, decisions_made, prompt_blob_ref,
		 created_at, updated_at, version)
		VALUES ('01HBC', 'task', 'T-1', '["E1"]', 'running', 180,
		        '2026-05-22T00:00:00Z', '{}', 0, '',
		        'not-a-time', '2026-05-22T00:00:00Z', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	repo := cognitiondb.NewInvocationRepo(db)
	if _, err := repo.FindByID(context.Background(), "01HBC"); err == nil {
		t.Fatal()
	}
}

func TestInvocationRepo_Scan_BadEndedAt(t *testing.T) {
	db := openTestDB(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO supervisor_invocations
		(id, scope_kind, scope_key, trigger_event_ids, status, hard_timeout_seconds,
		 started_at, ended_at, token_usage, decisions_made, prompt_blob_ref,
		 created_at, updated_at, version)
		VALUES ('01HBE', 'task', 'T-1', '["E1"]', 'succeeded', 180,
		        '2026-05-22T00:00:00Z', 'not-a-time', '{}', 0, '',
		        '2026-05-22T00:00:00Z', '2026-05-22T00:00:00Z', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	repo := cognitiondb.NewInvocationRepo(db)
	if _, err := repo.FindByID(context.Background(), "01HBE"); err == nil {
		t.Fatal()
	}
}

func TestInvocationRepo_Scan_BadTokenUsage(t *testing.T) {
	db := openTestDB(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO supervisor_invocations
		(id, scope_kind, scope_key, trigger_event_ids, status, hard_timeout_seconds,
		 started_at, token_usage, decisions_made, prompt_blob_ref,
		 created_at, updated_at, version)
		VALUES ('01HBT', 'task', 'T-1', '["E1"]', 'running', 180,
		        '2026-05-22T00:00:00Z', '{bad json', 0, '',
		        '2026-05-22T00:00:00Z', '2026-05-22T00:00:00Z', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	repo := cognitiondb.NewInvocationRepo(db)
	if _, err := repo.FindByID(context.Background(), "01HBT"); err == nil {
		t.Fatal()
	}
}

func TestInvocationRepo_Scan_BadTriggerEvents(t *testing.T) {
	db := openTestDB(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO supervisor_invocations
		(id, scope_kind, scope_key, trigger_event_ids, status, hard_timeout_seconds,
		 started_at, token_usage, decisions_made, prompt_blob_ref,
		 created_at, updated_at, version)
		VALUES ('01HBTE', 'task', 'T-1', '{bad', 'running', 180,
		        '2026-05-22T00:00:00Z', '{}', 0, '',
		        '2026-05-22T00:00:00Z', '2026-05-22T00:00:00Z', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	repo := cognitiondb.NewInvocationRepo(db)
	if _, err := repo.FindByID(context.Background(), "01HBTE"); err == nil {
		t.Fatal()
	}
}

// =============================================================================
// Closed-DB error paths for InvocationRepo
// =============================================================================

func TestInvocationRepo_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	_ = db.Close()
	if _, err := repo.FindByID(context.Background(), "x"); err == nil {
		t.Fatal()
	}
	inv := newSpawn(t, cognition.ScopeTask, "T-X", time.Now())
	if err := repo.Save(context.Background(), inv); err == nil {
		t.Fatal()
	}
	if _, err := repo.FindRunning(context.Background()); err == nil {
		t.Fatal()
	}
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-X")
	if _, err := repo.FindRunningByScope(context.Background(), scope); err == nil {
		t.Fatal()
	}
}

// =============================================================================
// Save: unique violation (running per scope)
// =============================================================================

func TestInvocationRepo_UniqueRunningPerScope(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	ctx := context.Background()
	inv1 := newSpawn(t, cognition.ScopeTask, "T-DUP", time.Now())
	if err := repo.Save(ctx, inv1); err != nil {
		t.Fatal(err)
	}
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-DUP")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E2"})
	inv2, _ := cognition.Spawn(cognition.SpawnInput{
		ID: cognition.InvocationID(idGen()), Scope: scope,
		TriggerEvents: tes, StartedAt: time.Now(),
	})
	err := repo.Save(ctx, inv2)
	if !errors.Is(err, cognition.ErrScopeKeyRunningExists) {
		t.Fatalf("expected unique violation, got %v", err)
	}
}

// =============================================================================
// UpdateStatusToTerminal — non-terminal status check + version conflict
// =============================================================================

func TestUpdateStatusToTerminal_NonTerminalRejected(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	inv := newSpawn(t, cognition.ScopeTask, "T-NT", time.Now())
	_ = repo.Save(context.Background(), inv)
	// inv is still in 'running' (non-terminal) → UpdateStatusToTerminal rejects
	err := repo.UpdateStatusToTerminal(context.Background(), inv)
	if err == nil {
		t.Fatal()
	}
}

// =============================================================================
// Decision repo helpers
// =============================================================================

func TestDecisionRepo_FindByInvocationID_Empty(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewDecisionRepo(db)
	got, err := repo.FindByInvocationID(context.Background(), "01H-NEVER")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatal()
	}
}

func TestDecisionRepo_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewDecisionRepo(db)
	_ = db.Close()
	if _, err := repo.FindByID(context.Background(), "x"); err == nil {
		t.Fatal()
	}
	if _, err := repo.FindByInvocationID(context.Background(), "x"); err == nil {
		t.Fatal()
	}
}

// Empty timestamp path on parseTime: insert a row whose started_at is "".
// SQLite NOT NULL constraint may block but we can test via UpdateStatus with
// empty ended_at. Actually parseTime is internal so simpler: insert row with
// empty ended_at via direct SQL update (allowed since ended_at is nullable
// TEXT, but the column accepts empty string differently than NULL).
func TestInvocationRepo_EmptyEndedAt_NoCrash(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	ctx := context.Background()
	inv := newSpawn(t, cognition.ScopeTask, "T-EE", time.Now())
	_ = repo.Save(ctx, inv)
	// Overwrite ended_at to empty string (not NULL).
	if _, err := db.ExecContext(ctx, `UPDATE supervisor_invocations SET ended_at = '' WHERE id = ?`, string(inv.ID())); err != nil {
		t.Fatal(err)
	}
	// FindByID should still succeed (empty string → nil time per scan logic).
	got, err := repo.FindByID(ctx, inv.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got.EndedAt() != nil {
		t.Fatal("ended_at empty string should produce nil time")
	}
}

// UpdateStatusToTerminal — version mismatch path: simulate a parallel update
// by directly modifying the row's version in DB.
func TestUpdateStatusToTerminal_VersionConflict(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	ctx := context.Background()
	inv := newSpawn(t, cognition.ScopeTask, "T-VC", time.Now())
	_ = repo.Save(ctx, inv)
	// Externally bump version so the upcoming Update CAS misses.
	if _, err := db.ExecContext(ctx, `UPDATE supervisor_invocations SET version = 99 WHERE id = ?`, string(inv.ID())); err != nil {
		t.Fatal(err)
	}
	// Now move inv to a terminal state in memory.
	if err := inv.MarkSucceeded(time.Now(), cognition.TokenUsage{}, 0); err != nil {
		t.Fatal(err)
	}
	err := repo.UpdateStatusToTerminal(ctx, inv)
	if !errors.Is(err, cognition.ErrInvocationVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

// DecisionRepo scan error path
func TestDecisionRepo_Scan_BadCreatedAt(t *testing.T) {
	db := openTestDB(t)
	// Inject a malformed created_at directly.
	_, err := db.ExecContext(context.Background(), `INSERT INTO decision_records
		(id, invocation_id, kind, target_refs, rationale, outcome, outcome_message, created_at)
		VALUES ('01HD-BC', 'INV-1', 'dispatch', '[]', 'r', 'committed', NULL, 'not-a-time')`)
	if err != nil {
		t.Fatal(err)
	}
	repo := cognitiondb.NewDecisionRepo(db)
	if _, err := repo.FindByID(context.Background(), "01HD-BC"); err == nil {
		t.Fatal()
	}
}

// NilInv defensive guards
func TestSave_NilInv(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	if err := repo.Save(context.Background(), nil); err == nil {
		t.Fatal()
	}
}

func TestUpdateStatusToTerminal_NilInv(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	if err := repo.UpdateStatusToTerminal(context.Background(), nil); err == nil {
		t.Fatal()
	}
}

// Find: closed-DB for additional methods + Find() with filter status
func TestInvocationRepo_Find_Filtered(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	ctx := context.Background()
	inv := newSpawn(t, cognition.ScopeTask, "T-F1", time.Now())
	_ = repo.Save(ctx, inv)
	st := cognition.StatusRunning
	got, err := repo.Find(ctx, cognition.InvocationFilter{
		Status: &st,
		Limit:  10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal()
	}
}

func TestInvocationRepo_Find_LimitTooLarge(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	_, err := repo.Find(context.Background(), cognition.InvocationFilter{
		Limit: cognition.MaxInvocationLimit + 1,
	})
	if !errors.Is(err, cognition.ErrInvocationLimitTooLarge) {
		t.Fatalf("expected limit too large, got %v", err)
	}
}

// Use persistence package to ensure import side-effects compile
var _ = persistence.RunInTx

// Use strings to keep import.
var _ = strings.Contains
