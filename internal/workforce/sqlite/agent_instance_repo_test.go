package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/workforce"
)

func aiWID(s string) *workforce.WorkerID { v := workforce.WorkerID(s); return &v }

func newAI(t *testing.T, id workforce.AgentInstanceID, name string, workerID string) *workforce.AgentInstance {
	t.Helper()
	a, err := workforce.NewAgentInstance(workforce.NewAgentInstanceInput{
		ID:        id,
		Name:      name,
		AgentCLI:  "claude-code",
		WorkerID:  aiWID(workerID),
		CreatedAt: time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAgentInstanceRepo_SaveAndFindByID(t *testing.T) {
	db := openTestDB(t)
	repo := NewAgentInstanceRepo(db)
	a := newAI(t, "01HAGI1", "coder", "W-1")
	if err := repo.Save(context.Background(), a); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.FindByID(context.Background(), "01HAGI1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Name() != "coder" {
		t.Fatalf("name: %s", got.Name())
	}
	if *got.WorkerID() != "W-1" {
		t.Fatalf("worker_id: %s", *got.WorkerID())
	}
	if got.State() != workforce.AgentInstanceIdle {
		t.Fatalf("state: %s", got.State())
	}
}

func TestAgentInstanceRepo_Save_DuplicateName(t *testing.T) {
	db := openTestDB(t)
	repo := NewAgentInstanceRepo(db)
	if err := repo.Save(context.Background(), newAI(t, "01HAGI1", "coder", "W-1")); err != nil {
		t.Fatal(err)
	}
	err := repo.Save(context.Background(), newAI(t, "01HAGI2", "coder", "W-2"))
	if !errors.Is(err, workforce.ErrAgentInstanceNameTaken) {
		t.Fatalf("expected name taken, got %v", err)
	}
}

func TestAgentInstanceRepo_FindByName(t *testing.T) {
	db := openTestDB(t)
	repo := NewAgentInstanceRepo(db)
	if err := repo.Save(context.Background(), newAI(t, "01HAGI1", "coder-mbp", "W-1")); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindByName(context.Background(), "coder-mbp")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != "01HAGI1" {
		t.Fatalf("id: %s", got.ID())
	}
}

func TestAgentInstanceRepo_FindByName_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewAgentInstanceRepo(db)
	_, err := repo.FindByName(context.Background(), "nope")
	if !errors.Is(err, workforce.ErrAgentInstanceNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestAgentInstanceRepo_FindAll_Filtered(t *testing.T) {
	db := openTestDB(t)
	repo := NewAgentInstanceRepo(db)
	_ = repo.Save(context.Background(), newAI(t, "01HA", "a1", "W-1"))
	_ = repo.Save(context.Background(), newAI(t, "01HB", "a2", "W-2"))
	// Builtin supervisor (no worker_id).
	sup, _ := workforce.NewAgentInstance(workforce.NewAgentInstanceInput{
		ID: "01HC", Name: "supervisor", AgentCLI: "claude-code",
		IsBuiltin: true, CreatedAt: time.Now(),
	})
	_ = repo.Save(context.Background(), sup)

	all, err := repo.FindAll(context.Background(), workforce.AgentInstanceFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}
	// Filter by worker.
	wid := workforce.WorkerID("W-1")
	w1Only, _ := repo.FindAll(context.Background(), workforce.AgentInstanceFilter{WorkerID: &wid})
	if len(w1Only) != 1 || w1Only[0].Name() != "a1" {
		t.Fatalf("W-1 filter: %v", w1Only)
	}
	// Filter by is_builtin=true.
	yes := true
	builtinOnly, _ := repo.FindAll(context.Background(), workforce.AgentInstanceFilter{IsBuiltin: &yes})
	if len(builtinOnly) != 1 || builtinOnly[0].Name() != "supervisor" {
		t.Fatalf("builtin filter: %v", builtinOnly)
	}
}

func TestAgentInstanceRepo_UpdateState_HappyPath(t *testing.T) {
	db := openTestDB(t)
	repo := NewAgentInstanceRepo(db)
	if err := repo.Save(context.Background(), newAI(t, "01HA", "a1", "W-1")); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateState(context.Background(), "01HA", workforce.AgentInstanceIdle, workforce.AgentInstanceActive, 1); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}
	got, _ := repo.FindByID(context.Background(), "01HA")
	if got.State() != workforce.AgentInstanceActive {
		t.Fatalf("state: %s", got.State())
	}
	if got.Version() != 2 {
		t.Fatalf("version: %d", got.Version())
	}
}

func TestAgentInstanceRepo_UpdateState_VersionConflict(t *testing.T) {
	db := openTestDB(t)
	repo := NewAgentInstanceRepo(db)
	_ = repo.Save(context.Background(), newAI(t, "01HA", "a1", "W-1"))
	err := repo.UpdateState(context.Background(), "01HA", workforce.AgentInstanceIdle, workforce.AgentInstanceActive, 99)
	if !errors.Is(err, workforce.ErrAgentInstanceVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

func TestAgentInstanceRepo_Archive_Happy(t *testing.T) {
	db := openTestDB(t)
	repo := NewAgentInstanceRepo(db)
	_ = repo.Save(context.Background(), newAI(t, "01HA", "a1", "W-1"))
	at := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	if err := repo.Archive(context.Background(), "01HA", at, workforce.AgentInstanceArchivedReasonManual, "user", 1); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	got, _ := repo.FindByID(context.Background(), "01HA")
	if got.State() != workforce.AgentInstanceArchived {
		t.Fatalf("state: %s", got.State())
	}
	if got.ArchivedAt() == nil || !got.ArchivedAt().Equal(at) {
		t.Fatalf("archived_at: %v", got.ArchivedAt())
	}
}

func TestAgentInstanceRepo_Archive_RejectsBuiltin(t *testing.T) {
	db := openTestDB(t)
	repo := NewAgentInstanceRepo(db)
	sup, _ := workforce.NewAgentInstance(workforce.NewAgentInstanceInput{
		ID: "01HSUP", Name: "supervisor", AgentCLI: "claude-code",
		IsBuiltin: true, CreatedAt: time.Now(),
	})
	_ = repo.Save(context.Background(), sup)
	err := repo.Archive(context.Background(), "01HSUP", time.Now(), workforce.AgentInstanceArchivedReasonManual, "test", 1)
	if !errors.Is(err, workforce.ErrAgentInstanceIsBuiltin) {
		t.Fatalf("expected is-builtin, got %v", err)
	}
}

func TestAgentInstanceRepo_Archive_RejectsActive(t *testing.T) {
	db := openTestDB(t)
	repo := NewAgentInstanceRepo(db)
	_ = repo.Save(context.Background(), newAI(t, "01HA", "a1", "W-1"))
	_ = repo.UpdateState(context.Background(), "01HA", workforce.AgentInstanceIdle, workforce.AgentInstanceActive, 1)
	err := repo.Archive(context.Background(), "01HA", time.Now(), workforce.AgentInstanceArchivedReasonManual, "test", 2)
	if err == nil {
		t.Fatal("expected archive rejection from active")
	}
}

func TestAgentInstanceRepo_BulkUpdateStateByWorker(t *testing.T) {
	db := openTestDB(t)
	repo := NewAgentInstanceRepo(db)
	for _, name := range []string{"a1", "a2", "a3"} {
		_ = repo.Save(context.Background(), newAI(t, workforce.AgentInstanceID("01H_"+name), name, "W-1"))
	}
	// Mark a2 active so bulk(idle→sleeping) skips it.
	_ = repo.UpdateState(context.Background(), "01H_a2", workforce.AgentInstanceIdle, workforce.AgentInstanceActive, 1)
	n, err := repo.BulkUpdateStateByWorker(context.Background(), "W-1", workforce.AgentInstanceIdle, workforce.AgentInstanceSleeping)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 updated, got %d", n)
	}
}

