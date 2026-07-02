package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/workforce"
)

// T752: system_info survives Save → FindByID round-trip, and an existing
// (pre-T752) row scans to a zero SystemInfo (the column defaults to '{}').
func TestWorkerRepo_SystemInfo_SaveRoundTrip(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	ctx := context.Background()

	// Fresh worker → zero system info persists as "{}" and reads back zero.
	w := newWorker(t, "W-1")
	if err := repo.Save(ctx, w); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.FindByID(ctx, "W-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if !got.SystemInfo().IsZero() {
		t.Fatalf("fresh worker system info should be zero, got %+v", got.SystemInfo())
	}

	// A worker carrying system info persists + reads it back verbatim.
	info := workforce.SystemInfo{
		Hostname:           "dev001.local",
		OS:                 "darwin",
		Arch:               "arm64",
		AgentCenterVersion: "v2.10.2",
		InstallPath:        "/usr/local/bin/agent-center",
		WorkerVersion:      "v2.10.2+abc1234",
	}
	w2, err := workforce.RehydrateWorker(workforce.RehydrateWorkerInput{
		ID:         "W-2",
		Status:     workforce.WorkerOffline,
		SystemInfo: info,
		EnrolledAt: time.Now(),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		Version:    1,
	})
	if err != nil {
		t.Fatalf("RehydrateWorker: %v", err)
	}
	if err := repo.Save(ctx, w2); err != nil {
		t.Fatalf("Save w2: %v", err)
	}
	got2, err := repo.FindByID(ctx, "W-2")
	if err != nil {
		t.Fatalf("FindByID w2: %v", err)
	}
	if got2.SystemInfo() != info {
		t.Fatalf("system info round-trip mismatch: got %+v want %+v", got2.SystemInfo(), info)
	}
}

func TestWorkerRepo_UpdateSystemInfo_CAS(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	ctx := context.Background()
	w := newWorker(t, "W-1")
	if err := repo.Save(ctx, w); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info := workforce.SystemInfo{Hostname: "h1", OS: "linux", Arch: "amd64", WorkerVersion: "v2"}
	if err := repo.UpdateSystemInfo(ctx, "W-1", info, 1); err != nil {
		t.Fatalf("UpdateSystemInfo: %v", err)
	}
	got, err := repo.FindByID(ctx, "W-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.SystemInfo() != info {
		t.Fatalf("updated info mismatch: got %+v", got.SystemInfo())
	}
	if got.Version() != 2 {
		t.Fatalf("version should bump to 2, got %d", got.Version())
	}

	// Stale version → CAS conflict.
	if err := repo.UpdateSystemInfo(ctx, "W-1", info, 1); !errors.Is(err, workforce.ErrWorkerVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}

	// Unknown worker → not found.
	if err := repo.UpdateSystemInfo(ctx, "W-MISSING", info, 1); !errors.Is(err, workforce.ErrWorkerNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}
