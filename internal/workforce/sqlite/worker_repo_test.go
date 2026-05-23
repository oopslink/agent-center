package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
)

func TestWorkerRepo_SaveAndFindByID(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	w := newWorker(t, "W-1")
	if err := repo.Save(context.Background(), w); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.FindByID(context.Background(), "W-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.ID() != "W-1" || got.Status() != workforce.WorkerOffline {
		t.Fatalf("got: id=%s status=%s", got.ID(), got.Status())
	}
	if got.Version() != 1 {
		t.Fatalf("version: %d", got.Version())
	}
	if caps := got.Capabilities(); len(caps) != 1 || caps[0] != "claude-code" {
		t.Fatalf("capabilities: %v", caps)
	}
}

func TestWorkerRepo_Save_Duplicate(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	w := newWorker(t, "W-1")
	if err := repo.Save(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	w2 := newWorker(t, "W-1")
	err := repo.Save(context.Background(), w2)
	if !errors.Is(err, workforce.ErrWorkerAlreadyExists) {
		t.Fatalf("expected ErrWorkerAlreadyExists, got %v", err)
	}
}

func TestWorkerRepo_FindByID_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	_, err := repo.FindByID(context.Background(), "W-MISSING")
	if !errors.Is(err, workforce.ErrWorkerNotFound) {
		t.Fatalf("expected ErrWorkerNotFound, got %v", err)
	}
}

func TestWorkerRepo_UpdateStatus_CASSuccess(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	w := newWorker(t, "W-1")
	_ = repo.Save(context.Background(), w)
	err := repo.UpdateStatus(context.Background(), "W-1", workforce.WorkerOffline, workforce.WorkerOnline, 1)
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	got, _ := repo.FindByID(context.Background(), "W-1")
	if got.Status() != workforce.WorkerOnline {
		t.Fatalf("status: %s", got.Status())
	}
	if got.Version() != 2 {
		t.Fatalf("version: %d", got.Version())
	}
}

func TestWorkerRepo_UpdateStatus_FromMismatch(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	_ = repo.Save(context.Background(), newWorker(t, "W-1"))
	// "from" is online but worker is offline → CAS fails
	err := repo.UpdateStatus(context.Background(), "W-1", workforce.WorkerOnline, workforce.WorkerOffline, 1)
	if !errors.Is(err, workforce.ErrWorkerVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

func TestWorkerRepo_UpdateStatus_VersionMismatch(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	_ = repo.Save(context.Background(), newWorker(t, "W-1"))
	err := repo.UpdateStatus(context.Background(), "W-1", workforce.WorkerOffline, workforce.WorkerOnline, 99)
	if !errors.Is(err, workforce.ErrWorkerVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

func TestWorkerRepo_UpdateStatus_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	err := repo.UpdateStatus(context.Background(), "W-NEVER", workforce.WorkerOffline, workforce.WorkerOnline, 1)
	if !errors.Is(err, workforce.ErrWorkerNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestWorkerRepo_UpdateStatus_InvalidStatus(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	err := repo.UpdateStatus(context.Background(), "W-1", "bogus", workforce.WorkerOnline, 1)
	if !errors.Is(err, workforce.ErrWorkerInvalidStatus) {
		t.Fatalf("expected invalid status, got %v", err)
	}
}

func TestWorkerRepo_UpdateLastHeartbeatAt(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	_ = repo.Save(context.Background(), newWorker(t, "W-1"))
	at := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	if err := repo.UpdateLastHeartbeatAt(context.Background(), "W-1", at, 60); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.FindByID(context.Background(), "W-1")
	if got.LastHeartbeatAt() == nil || !got.LastHeartbeatAt().Equal(at) {
		t.Fatalf("heartbeat: %v", got.LastHeartbeatAt())
	}
	if got.WorkingSeconds() != 60 {
		t.Fatalf("working_seconds: %d", got.WorkingSeconds())
	}
}

func TestWorkerRepo_UpdateLastHeartbeatAt_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	err := repo.UpdateLastHeartbeatAt(context.Background(), "W-NEVER", time.Now(), 1)
	if !errors.Is(err, workforce.ErrWorkerNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestWorkerRepo_FindByStatus(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	w1 := newWorker(t, "W-1")
	w2 := newWorker(t, "W-2")
	_ = repo.Save(context.Background(), w1)
	_ = repo.Save(context.Background(), w2)
	_ = repo.UpdateStatus(context.Background(), "W-1", workforce.WorkerOffline, workforce.WorkerOnline, 1)

	got, err := repo.FindByStatus(context.Background(), workforce.WorkerOnline)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID() != "W-1" {
		t.Fatalf("FindByStatus online: %v", got)
	}
	got, _ = repo.FindByStatus(context.Background(), workforce.WorkerOffline)
	if len(got) != 1 || got[0].ID() != "W-2" {
		t.Fatalf("FindByStatus offline: %v", got)
	}
}

func TestWorkerRepo_FindByStatus_Invalid(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	_, err := repo.FindByStatus(context.Background(), "bogus")
	if !errors.Is(err, workforce.ErrWorkerInvalidStatus) {
		t.Fatalf("expected invalid status, got %v", err)
	}
}

func TestWorkerRepo_FindAll(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	for _, id := range []workforce.WorkerID{"W-1", "W-2", "W-3"} {
		_ = repo.Save(context.Background(), newWorker(t, id))
	}
	got, err := repo.FindAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("FindAll: %d workers", len(got))
	}
}

func TestWorkerRepo_Save_RespectsTx(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	w := newWorker(t, "W-1")
	tx, _ := db.BeginTx(context.Background(), nil)
	ctx := persistence.WithTx(context.Background(), tx)
	if err := repo.Save(ctx, w); err != nil {
		t.Fatal(err)
	}
	// outside tx not visible
	if _, err := repo.FindByID(context.Background(), "W-1"); !errors.Is(err, workforce.ErrWorkerNotFound) {
		t.Fatalf("expected not visible: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindByID(context.Background(), "W-1"); !errors.Is(err, workforce.ErrWorkerNotFound) {
		t.Fatal("expected gone after rollback")
	}
}

func TestWorkerRepo_Save_NilWorker(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	if err := repo.Save(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil worker")
	}
}

// ---- v2 (P8 § 3.1) tests for UpdateConfig + UpdateCapabilities ----

func TestWorkerRepo_UpdateConfig_ConcurrencyOnly(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	_ = repo.Save(context.Background(), newWorker(t, "W-1"))
	newC := workforce.WorkerConcurrency{PerAgentType: 5}
	if err := repo.UpdateConfig(context.Background(), "W-1",
		workforce.WorkerConfigFields{Concurrency: &newC}, 1); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	got, _ := repo.FindByID(context.Background(), "W-1")
	if got.Concurrency().PerAgentType != 5 {
		t.Fatalf("concurrency: %+v", got.Concurrency())
	}
	if got.Version() != 2 {
		t.Fatalf("version: %d", got.Version())
	}
}

func TestWorkerRepo_UpdateConfig_DiscoveryOnly(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	_ = repo.Save(context.Background(), newWorker(t, "W-1"))
	newD := workforce.WorkerDiscovery{
		ScanPaths:    []string{"/home/dev/projects"},
		Exclude:      []string{".cache"},
		ScanInterval: "2h",
	}
	if err := repo.UpdateConfig(context.Background(), "W-1",
		workforce.WorkerConfigFields{Discovery: &newD}, 1); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	got, _ := repo.FindByID(context.Background(), "W-1")
	if len(got.Discovery().ScanPaths) != 1 || got.Discovery().ScanPaths[0] != "/home/dev/projects" {
		t.Fatalf("discovery: %+v", got.Discovery())
	}
}

func TestWorkerRepo_UpdateConfig_VersionConflict(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	_ = repo.Save(context.Background(), newWorker(t, "W-1"))
	newC := workforce.WorkerConcurrency{PerAgentType: 3}
	err := repo.UpdateConfig(context.Background(), "W-1",
		workforce.WorkerConfigFields{Concurrency: &newC}, 99)
	if !errors.Is(err, workforce.ErrWorkerVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

func TestWorkerRepo_UpdateConfig_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	newC := workforce.WorkerConcurrency{PerAgentType: 3}
	err := repo.UpdateConfig(context.Background(), "W-NEVER",
		workforce.WorkerConfigFields{Concurrency: &newC}, 1)
	if !errors.Is(err, workforce.ErrWorkerNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestWorkerRepo_UpdateConfig_NoFields(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	_ = repo.Save(context.Background(), newWorker(t, "W-1"))
	err := repo.UpdateConfig(context.Background(), "W-1",
		workforce.WorkerConfigFields{}, 1)
	if err == nil {
		t.Fatal("expected error when no fields")
	}
}

func TestWorkerRepo_UpdateCapabilities_AutoProbe_AddsNewCLI(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	// Initialise worker with claude-code only (default Enabled=true).
	if err := repo.Save(context.Background(), newWorker(t, "W-1")); err != nil {
		t.Fatal(err)
	}
	// Worker auto-probe: claude-code still there, codex newly discovered.
	detected := []workforce.Capability{
		{AgentCLI: "claude-code", Detected: true},
		{AgentCLI: "codex", Detected: true},
	}
	if err := repo.UpdateCapabilities(context.Background(), "W-1", detected, 1); err != nil {
		t.Fatalf("UpdateCapabilities: %v", err)
	}
	got, _ := repo.FindByID(context.Background(), "W-1")
	if got.Version() != 2 {
		t.Fatalf("version: %d", got.Version())
	}
	caps := got.CapabilityList()
	if len(caps) != 2 {
		t.Fatalf("expected 2 caps, got %d: %+v", len(caps), caps)
	}
	for _, c := range caps {
		// Both should default to Enabled=Detected on first probe.
		if !c.Detected || !c.Enabled {
			t.Fatalf("expected detected=true enabled=true, got %+v", c)
		}
	}
}

func TestWorkerRepo_UpdateCapabilities_AutoProbe_PreservesEnabled(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	// Seed worker rich list with claude-code Enabled=false (simulating prior
	// user disable persisted by a future SetCapabilityEnabled path).
	w, err := workforce.NewWorker(workforce.NewWorkerInput{
		ID: "W-1",
		CapabilityList: []workforce.Capability{
			{AgentCLI: "claude-code", Detected: true, Enabled: false},
		},
		EnrolledAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	// Auto-probe re-reports claude-code still detected; should preserve
	// Enabled=false (user choice wins).
	detected := []workforce.Capability{
		{AgentCLI: "claude-code", Detected: true},
	}
	if err := repo.UpdateCapabilities(context.Background(), "W-1", detected, 1); err != nil {
		t.Fatalf("UpdateCapabilities: %v", err)
	}
	got, _ := repo.FindByID(context.Background(), "W-1")
	caps := got.CapabilityList()
	if len(caps) != 1 {
		t.Fatalf("expected 1 cap, got %d", len(caps))
	}
	if caps[0].Enabled != false {
		t.Fatalf("expected Enabled=false preserved, got %+v", caps[0])
	}
}

func TestWorkerRepo_UpdateCapabilities_VersionConflict(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	_ = repo.Save(context.Background(), newWorker(t, "W-1"))
	err := repo.UpdateCapabilities(context.Background(), "W-1",
		[]workforce.Capability{{AgentCLI: "claude-code", Detected: true}}, 99)
	if !errors.Is(err, workforce.ErrWorkerVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

func TestWorkerRepo_UpdateCapabilities_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	err := repo.UpdateCapabilities(context.Background(), "W-NEVER",
		[]workforce.Capability{{AgentCLI: "claude-code", Detected: true}}, 1)
	if !errors.Is(err, workforce.ErrWorkerNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}
