package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/observability/projection"
)

func mustNewAgentWorkItemProjectionRepo(t *testing.T) *AgentWorkItemProjectionRepo {
	t.Helper()
	db := openTestDB(t)
	return NewAgentWorkItemProjectionRepo(db)
}

func TestAgentWorkItemProjectionRepo_Upsert_Fresh_Insert(t *testing.T) {
	repo := mustNewAgentWorkItemProjectionRepo(t)
	id := "WI-1"
	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	upd := projection.AgentWorkItemProjectionUpdate{
		AgentID:                   "A-1",
		Status:                    "active",
		CurrentActivity:           "edit foo.go",
		CurrentActivityAt:         now,
		TotalToolCalls:            3,
		TotalTokensInput:          100,
		TotalTokensOutput:         50,
		WorkingSecondsAccumulated: 12,
		LastActivityAt:            now,
	}
	row, fresh, err := repo.UpsertIfFresh(context.Background(), id, upd)
	if err != nil || !fresh {
		t.Fatalf("first UPSERT: fresh=%v err=%v", fresh, err)
	}
	if row.CurrentActivity != "edit foo.go" || row.AgentID != "A-1" || row.Status != "active" {
		t.Fatalf("row mismatch: %+v", row)
	}
	got, err := repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.TotalToolCalls != 3 || got.TotalTokensInput != 100 || got.TotalTokensOutput != 50 || got.WorkingSecondsAccumulated != 12 {
		t.Fatalf("counters: %+v", got)
	}
	if got.AgentID != "A-1" || got.Status != "active" {
		t.Fatalf("denormalized cols: %+v", got)
	}
}

func TestAgentWorkItemProjectionRepo_Upsert_Stale_Drop(t *testing.T) {
	repo := mustNewAgentWorkItemProjectionRepo(t)
	id := "WI-1"
	t0 := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	tEarlier := t0.Add(-time.Minute)
	if _, _, err := repo.UpsertIfFresh(context.Background(), id, projection.AgentWorkItemProjectionUpdate{
		AgentID: "A-1", Status: "active", LastActivityAt: t0, TotalToolCalls: 9,
	}); err != nil {
		t.Fatalf("first: %v", err)
	}
	existing, fresh, err := repo.UpsertIfFresh(context.Background(), id, projection.AgentWorkItemProjectionUpdate{
		AgentID: "A-1", Status: "done", LastActivityAt: tEarlier, TotalToolCalls: 1,
	})
	if !errors.Is(err, projection.ErrProjectionStale) {
		t.Fatalf("expected ErrProjectionStale, got %v", err)
	}
	if fresh {
		t.Fatal("fresh must be false on stale")
	}
	if existing.TotalToolCalls != 9 {
		t.Fatalf("existing snapshot off: %+v", existing)
	}
	// Stored row must still be the fresher value (not overwritten).
	got, err := repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.TotalToolCalls != 9 || got.Status != "active" {
		t.Fatalf("stored row got overwritten: %+v", got)
	}
}

func TestAgentWorkItemProjectionRepo_Upsert_Fresh_Overwrites(t *testing.T) {
	repo := mustNewAgentWorkItemProjectionRepo(t)
	id := "WI-1"
	t0 := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	first := projection.AgentWorkItemProjectionUpdate{AgentID: "A-1", Status: "active", LastActivityAt: t0, TotalToolCalls: 1}
	if _, _, err := repo.UpsertIfFresh(context.Background(), id, first); err != nil {
		t.Fatalf("first: %v", err)
	}
	second := projection.AgentWorkItemProjectionUpdate{AgentID: "A-1", Status: "done", LastActivityAt: t1, TotalToolCalls: 5, CurrentActivity: "tests"}
	row, fresh, err := repo.UpsertIfFresh(context.Background(), id, second)
	if err != nil || !fresh {
		t.Fatalf("second: fresh=%v err=%v", fresh, err)
	}
	if row.TotalToolCalls != 5 || row.CurrentActivity != "tests" || row.Status != "done" {
		t.Fatalf("update mismatch: %+v", row)
	}
	got, err := repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.TotalToolCalls != 5 || got.Status != "done" {
		t.Fatalf("stored row not overwritten: %+v", got)
	}
}

func TestAgentWorkItemProjectionRepo_FindByID_NotFound(t *testing.T) {
	repo := mustNewAgentWorkItemProjectionRepo(t)
	_, err := repo.FindByID(context.Background(), "missing")
	if !errors.Is(err, projection.ErrProjectionNotFound) {
		t.Fatalf("expected ErrProjectionNotFound, got %v", err)
	}
}

func TestAgentWorkItemProjectionRepo_FindByIDs_PartialHits(t *testing.T) {
	repo := mustNewAgentWorkItemProjectionRepo(t)
	ctx := context.Background()
	t0 := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	for _, id := range []string{"WI-1", "WI-2"} {
		if _, _, err := repo.UpsertIfFresh(ctx, id, projection.AgentWorkItemProjectionUpdate{
			AgentID: "A-1", Status: "active", LastActivityAt: t0,
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	got, err := repo.FindByIDs(ctx, []string{"WI-1", "WI-2", "WI-missing"})
	if err != nil {
		t.Fatalf("FindByIDs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(got))
	}
	if _, ok := got["WI-1"]; !ok {
		t.Fatal("WI-1 missing")
	}
	if _, ok := got["WI-2"]; !ok {
		t.Fatal("WI-2 missing")
	}
}

func TestAgentWorkItemProjectionRepo_FindByIDs_Empty(t *testing.T) {
	repo := mustNewAgentWorkItemProjectionRepo(t)
	got, err := repo.FindByIDs(context.Background(), nil)
	if err != nil {
		t.Fatalf("FindByIDs empty: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %d", len(got))
	}
}

func TestAgentWorkItemProjectionRepo_Upsert_ValidationFails(t *testing.T) {
	repo := mustNewAgentWorkItemProjectionRepo(t)
	_, _, err := repo.UpsertIfFresh(context.Background(), "WI-1", projection.AgentWorkItemProjectionUpdate{})
	if err == nil {
		t.Fatal("expected validation error on zero LastActivityAt")
	}
}

func TestAgentWorkItemProjectionRepo_List(t *testing.T) {
	repo := mustNewAgentWorkItemProjectionRepo(t)
	ctx := context.Background()
	t0 := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	seed := func(id, agentID, status string, lastAct time.Time) {
		if _, _, err := repo.UpsertIfFresh(ctx, id, projection.AgentWorkItemProjectionUpdate{
			AgentID: agentID, Status: status, TotalToolCalls: 1, LastActivityAt: lastAct,
		}); err != nil {
			t.Fatal(err)
		}
	}
	seed("WI-1", "A1", "active", t0.Add(2*time.Minute))
	seed("WI-2", "A1", "waiting_input", t0.Add(3*time.Minute))
	seed("WI-3", "A2", "done", t0.Add(1*time.Minute)) // terminal → excluded by live filter
	seed("WI-4", "A2", "active", t0.Add(1*time.Minute))

	// live status set, ORDER BY last_activity_at DESC → WI-2(3m), WI-1(2m), WI-4(1m); WI-3(done) excluded.
	live, err := repo.List(ctx, projection.AgentWorkItemProjectionFilter{Statuses: []string{"queued", "active", "waiting_input"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 3 {
		t.Fatalf("live filter want 3, got %d", len(live))
	}
	if live[0].WorkItemID != "WI-2" || live[1].WorkItemID != "WI-1" || live[2].WorkItemID != "WI-4" {
		t.Fatalf("order want WI-2,WI-1,WI-4 (last_activity_at DESC), got %s,%s,%s", live[0].WorkItemID, live[1].WorkItemID, live[2].WorkItemID)
	}
	if live[0].Status != "waiting_input" || live[0].TotalToolCalls != 1 {
		t.Fatalf("row fields not hydrated: %+v", live[0])
	}

	// agent filter
	a1, err := repo.List(ctx, projection.AgentWorkItemProjectionFilter{AgentID: "A1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(a1) != 2 {
		t.Fatalf("agent filter A1 want 2, got %d", len(a1))
	}

	// no filter → all 4
	all, err := repo.List(ctx, projection.AgentWorkItemProjectionFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Fatalf("no filter want 4, got %d", len(all))
	}
}
