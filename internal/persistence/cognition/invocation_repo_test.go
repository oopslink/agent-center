package cognition_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cognition"
	cognitiondb "github.com/oopslink/agent-center/internal/persistence/cognition"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	// Use a file DB in a temp dir so WAL works and partial unique indexes
	// fire correctly.
	dir := t.TempDir()
	dsn := persistence.FileDSN(dir + "/test.db")
	db, err := persistence.Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	m := persistence.NewMigrator(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newSpawn(t *testing.T, kind cognition.ScopeKind, key string, at time.Time) *cognition.SupervisorInvocation {
	t.Helper()
	scope := cognition.MustNewInvocationScope(kind, key)
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E1"})
	inv, err := cognition.Spawn(cognition.SpawnInput{
		ID:            cognition.InvocationID(idGen()),
		Scope:         scope,
		TriggerEvents: tes,
		StartedAt:     at,
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	return inv
}

var idSeed = 0

func idGen() string {
	idSeed++
	return "01HXXXXX" + string(rune('A'+(idSeed-1)%26)) + string(rune('A'+(idSeed/26)%26))
}

func TestInvocationRepo_SaveFind(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	ctx := context.Background()
	now := time.Date(2026, 5, 22, 1, 0, 0, 0, time.UTC)
	inv := newSpawn(t, cognition.ScopeTask, "T-1", now)
	if err := repo.Save(ctx, inv); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := repo.FindByID(ctx, inv.ID())
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.Status() != cognition.StatusRunning {
		t.Errorf("status %s", got.Status())
	}
	if got.Scope().Kind() != cognition.ScopeTask || got.Scope().Key() != "T-1" {
		t.Errorf("scope %+v", got.Scope())
	}
	if got.HardTimeoutSeconds() != 180 {
		t.Errorf("ht %d", got.HardTimeoutSeconds())
	}
	if !got.StartedAt().Equal(now) {
		t.Errorf("startedAt %v vs %v", got.StartedAt(), now)
	}
	if got.Version() != 1 {
		t.Errorf("version %d", got.Version())
	}
}

func TestInvocationRepo_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	if _, err := repo.FindByID(context.Background(), "nonexistent"); !errors.Is(err, cognition.ErrInvocationNotFound) {
		t.Fatalf("expected ErrInvocationNotFound, got %v", err)
	}
}

func TestInvocationRepo_ScopeKeyRunningExists(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	a := newSpawn(t, cognition.ScopeTask, "T-1", now)
	b := newSpawn(t, cognition.ScopeTask, "T-1", now.Add(1*time.Second))
	if err := repo.Save(ctx, a); err != nil {
		t.Fatalf("save a: %v", err)
	}
	if err := repo.Save(ctx, b); !errors.Is(err, cognition.ErrScopeKeyRunningExists) {
		t.Fatalf("save b: expected ErrScopeKeyRunningExists, got %v", err)
	}
}

func TestInvocationRepo_UpdateTerminalCAS(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	inv := newSpawn(t, cognition.ScopeTask, "T-1", now)
	if err := repo.Save(ctx, inv); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := inv.MarkSucceeded(now.Add(30*time.Second), cognition.TokenUsage{Input: 5}, 1); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if err := repo.UpdateStatusToTerminal(ctx, inv); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := repo.FindByID(ctx, inv.ID())
	if err != nil {
		t.Fatalf("re-find: %v", err)
	}
	if got.Status() != cognition.StatusSucceeded {
		t.Errorf("status %s", got.Status())
	}
	if got.TokenUsage().Input != 5 {
		t.Errorf("tokens %+v", got.TokenUsage())
	}
	if got.Version() != 2 {
		t.Errorf("version %d", got.Version())
	}
}

func TestInvocationRepo_UpdateTerminalConflict(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	inv := newSpawn(t, cognition.ScopeTask, "T-1", now)
	_ = repo.Save(ctx, inv)
	// stalecopy that does NOT bump version on the AR
	stale, _ := repo.FindByID(ctx, inv.ID())
	// first updater bumps to version 2
	if err := inv.MarkSucceeded(now.Add(1*time.Second), cognition.TokenUsage{}, 0); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if err := repo.UpdateStatusToTerminal(ctx, inv); err != nil {
		t.Fatalf("update: %v", err)
	}
	// now the stale (version 1) AR tries to mark — should conflict at CAS
	if err := stale.MarkFailed(cognition.FailedReasonOOM, "x", now.Add(2*time.Second)); err != nil {
		t.Fatalf("mark stale: %v", err)
	}
	if err := repo.UpdateStatusToTerminal(ctx, stale); !errors.Is(err, cognition.ErrInvocationVersionConflict) {
		t.Fatalf("expected ErrInvocationVersionConflict, got %v", err)
	}
}

func TestInvocationRepo_UpdateTerminalRejectRunning(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	ctx := context.Background()
	inv := newSpawn(t, cognition.ScopeTask, "T-1", time.Now().UTC())
	_ = repo.Save(ctx, inv)
	// status still running → method should refuse
	if err := repo.UpdateStatusToTerminal(ctx, inv); err == nil {
		t.Fatal("expected error for non-terminal status")
	}
}

func TestInvocationRepo_FindRunningByScope(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	scope := cognition.MustNewInvocationScope(cognition.ScopeIssue, "I-1")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"X"})
	inv, _ := cognition.Spawn(cognition.SpawnInput{ID: "INV1", Scope: scope, TriggerEvents: tes, StartedAt: now})
	if err := repo.Save(ctx, inv); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindRunningByScope(ctx, scope)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.ID() != "INV1" {
		t.Errorf("got %s", got.ID())
	}
	// scope without running
	other := cognition.MustNewInvocationScope(cognition.ScopeIssue, "I-2")
	if _, err := repo.FindRunningByScope(ctx, other); !errors.Is(err, cognition.ErrInvocationNotFound) {
		t.Fatalf("expected ErrInvocationNotFound, got %v", err)
	}
}

func TestInvocationRepo_FindRunningAll(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	for i, key := range []string{"T-1", "T-2", "T-3"} {
		inv := newSpawn(t, cognition.ScopeTask, key, now.Add(time.Duration(i)*time.Second))
		if err := repo.Save(ctx, inv); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.FindRunning(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("got %d", len(got))
	}
}

func TestInvocationRepo_FindWithFilter(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	// insert running task + finished issue
	t1 := newSpawn(t, cognition.ScopeTask, "T-1", now)
	_ = repo.Save(ctx, t1)
	i1 := newSpawn(t, cognition.ScopeIssue, "I-1", now.Add(1*time.Second))
	_ = repo.Save(ctx, i1)
	_ = i1.MarkSucceeded(now.Add(2*time.Second), cognition.TokenUsage{}, 0)
	_ = repo.UpdateStatusToTerminal(ctx, i1)

	// filter by status
	st := cognition.StatusSucceeded
	rows, err := repo.Find(ctx, cognition.InvocationFilter{Status: &st, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID() != i1.ID() {
		t.Errorf("by status: %+v", rows)
	}
	// filter by kind+key
	kind := cognition.ScopeTask
	key := "T-1"
	rows, _ = repo.Find(ctx, cognition.InvocationFilter{ScopeKind: &kind, ScopeKey: &key, Limit: 10})
	if len(rows) != 1 {
		t.Errorf("by scope: %d", len(rows))
	}
	// since/until
	past := now.Add(-1 * time.Hour)
	future := now.Add(1 * time.Hour)
	rows, _ = repo.Find(ctx, cognition.InvocationFilter{Since: &past, Until: &future, Limit: 10})
	if len(rows) != 2 {
		t.Errorf("since/until: %d", len(rows))
	}
	// limit too large
	if _, err := repo.Find(ctx, cognition.InvocationFilter{Limit: cognition.MaxInvocationLimit + 1}); !errors.Is(err, cognition.ErrInvocationLimitTooLarge) {
		t.Fatalf("expected ErrInvocationLimitTooLarge, got %v", err)
	}
}

func TestInvocationRepo_FindCursor(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	var ids []cognition.InvocationID
	for i, key := range []string{"T-1", "T-2", "T-3"} {
		inv := newSpawn(t, cognition.ScopeTask, key, now.Add(time.Duration(i)*time.Second))
		_ = repo.Save(ctx, inv)
		ids = append(ids, inv.ID())
	}
	cursor := ids[0]
	rows, err := repo.Find(ctx, cognition.InvocationFilter{Cursor: &cursor, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) >= 3 {
		t.Errorf("cursor did not paginate: %d", len(rows))
	}
}

func TestInvocationRepo_TimedOutRehydrate(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	inv := newSpawn(t, cognition.ScopeTask, "T-1", now)
	_ = repo.Save(ctx, inv)
	if err := inv.MarkTimedOut(now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatusToTerminal(ctx, inv); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindByID(ctx, inv.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status() != cognition.StatusTimedOut {
		t.Errorf("status %s", got.Status())
	}
	if got.TimedOutAt() == nil {
		t.Errorf("timed_out_at nil")
	}
}

func TestInvocationRepo_FailedRehydrate(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	inv := newSpawn(t, cognition.ScopeTask, "T-1", now)
	_ = repo.Save(ctx, inv)
	if err := inv.MarkFailed(cognition.FailedReasonClaudeNonZero, "exit 1", now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatusToTerminal(ctx, inv); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindByID(ctx, inv.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got.FailedReason() != cognition.FailedReasonClaudeNonZero {
		t.Errorf("reason %s", got.FailedReason())
	}
	if got.FailedMessage() != "exit 1" {
		t.Errorf("msg %s", got.FailedMessage())
	}
}

func TestInvocationRepo_NilGuard(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	if err := repo.Save(context.Background(), nil); err == nil {
		t.Error("nil save")
	}
	if err := repo.UpdateStatusToTerminal(context.Background(), nil); err == nil {
		t.Error("nil update")
	}
}
