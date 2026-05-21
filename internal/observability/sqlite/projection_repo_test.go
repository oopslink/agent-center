package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/observability/projection"
	"github.com/oopslink/agent-center/internal/taskruntime"
)

func mustNewProjectionRepo(t *testing.T) *ProjectionRepo {
	t.Helper()
	db := openTestDB(t)
	return NewProjectionRepo(db)
}

func TestProjectionRepo_Upsert_Fresh_Insert(t *testing.T) {
	repo := mustNewProjectionRepo(t)
	id := taskruntime.TaskExecutionID("E-1")
	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	upd := projection.ProjectionUpdate{
		CurrentActivity:   "edit foo.go",
		CurrentActivityAt: now,
		TotalToolCalls:    3,
		TotalTokensInput:  100,
		TotalTokensOutput: 50,
		LastPushAt:        now,
	}
	row, fresh, err := repo.UpsertIfFresh(context.Background(), id, upd)
	if err != nil || !fresh {
		t.Fatalf("first UPSERT: fresh=%v err=%v", fresh, err)
	}
	if row.CurrentActivity != "edit foo.go" {
		t.Fatalf("activity mismatch: %q", row.CurrentActivity)
	}
	got, err := repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.TotalToolCalls != 3 || got.TotalTokensInput != 100 || got.TotalTokensOutput != 50 {
		t.Fatalf("counters: %+v", got)
	}
}

func TestProjectionRepo_Upsert_Update_Path(t *testing.T) {
	repo := mustNewProjectionRepo(t)
	id := taskruntime.TaskExecutionID("E-1")
	t0 := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	first := projection.ProjectionUpdate{LastPushAt: t0, TotalToolCalls: 1}
	if _, _, err := repo.UpsertIfFresh(context.Background(), id, first); err != nil {
		t.Fatalf("first: %v", err)
	}
	second := projection.ProjectionUpdate{LastPushAt: t1, TotalToolCalls: 5, CurrentActivity: "tests"}
	row, fresh, err := repo.UpsertIfFresh(context.Background(), id, second)
	if err != nil || !fresh {
		t.Fatalf("second: fresh=%v err=%v", fresh, err)
	}
	if row.TotalToolCalls != 5 || row.CurrentActivity != "tests" {
		t.Fatalf("update mismatch: %+v", row)
	}
}

func TestProjectionRepo_Upsert_Stale_Drop(t *testing.T) {
	repo := mustNewProjectionRepo(t)
	id := taskruntime.TaskExecutionID("E-1")
	t0 := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	tEarlier := t0.Add(-time.Minute)
	if _, _, err := repo.UpsertIfFresh(context.Background(), id, projection.ProjectionUpdate{LastPushAt: t0, TotalToolCalls: 9}); err != nil {
		t.Fatalf("first: %v", err)
	}
	existing, fresh, err := repo.UpsertIfFresh(context.Background(), id, projection.ProjectionUpdate{LastPushAt: tEarlier, TotalToolCalls: 1})
	if !errors.Is(err, projection.ErrProjectionStale) {
		t.Fatalf("expected ErrProjectionStale, got %v", err)
	}
	if fresh {
		t.Fatal("fresh must be false on stale")
	}
	if existing.TotalToolCalls != 9 {
		t.Fatalf("existing snapshot off: %+v", existing)
	}
	// Stored row must still be the fresher value.
	got, err := repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.TotalToolCalls != 9 {
		t.Fatalf("stored row got overwritten: %+v", got)
	}
}

func TestProjectionRepo_FindByID_NotFound(t *testing.T) {
	repo := mustNewProjectionRepo(t)
	_, err := repo.FindByID(context.Background(), taskruntime.TaskExecutionID("missing"))
	if !errors.Is(err, projection.ErrProjectionNotFound) {
		t.Fatalf("expected ErrProjectionNotFound, got %v", err)
	}
}

func TestProjectionRepo_FindByIDs_PartialHits(t *testing.T) {
	repo := mustNewProjectionRepo(t)
	ctx := context.Background()
	t0 := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	for _, id := range []string{"E-1", "E-2"} {
		if _, _, err := repo.UpsertIfFresh(ctx, taskruntime.TaskExecutionID(id), projection.ProjectionUpdate{LastPushAt: t0}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	got, err := repo.FindByIDs(ctx, []taskruntime.TaskExecutionID{"E-1", "E-2", "E-missing"})
	if err != nil {
		t.Fatalf("FindByIDs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(got))
	}
	if _, ok := got["E-1"]; !ok {
		t.Fatal("E-1 missing")
	}
	if _, ok := got["E-2"]; !ok {
		t.Fatal("E-2 missing")
	}
}

func TestProjectionRepo_FindByIDs_Empty(t *testing.T) {
	repo := mustNewProjectionRepo(t)
	got, err := repo.FindByIDs(context.Background(), nil)
	if err != nil {
		t.Fatalf("FindByIDs empty: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %d", len(got))
	}
}

func TestProjectionRepo_Upsert_ValidationFails(t *testing.T) {
	repo := mustNewProjectionRepo(t)
	_, _, err := repo.UpsertIfFresh(context.Background(), "E-1", projection.ProjectionUpdate{})
	if err == nil {
		t.Fatal("expected validation error on zero LastPushAt")
	}
}

func TestProjectionRepo_Upsert_HighFrequency_NoLeak(t *testing.T) {
	repo := mustNewProjectionRepo(t)
	id := taskruntime.TaskExecutionID("E-100")
	base := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	// 100 sequential UPSERTs, each advancing last_push_at by 1ms.
	for i := 0; i < 100; i++ {
		upd := projection.ProjectionUpdate{
			LastPushAt:     base.Add(time.Duration(i) * time.Millisecond),
			TotalToolCalls: int64(i + 1),
		}
		if _, _, err := repo.UpsertIfFresh(context.Background(), id, upd); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
	got, err := repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.TotalToolCalls != 100 {
		t.Fatalf("expected 100 tool calls, got %d", got.TotalToolCalls)
	}
}
