package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func newFindingRepo(t *testing.T) (context.Context, *PlanFindingRepo) {
	t.Helper()
	d, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(d).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return context.Background(), NewPlanFindingRepo(d)
}

func mkFinding(t *testing.T, id pm.PlanFindingID, plan pm.PlanID, task pm.TaskID, kind pm.PlanFindingKind, content string, at time.Time) *pm.PlanFinding {
	t.Helper()
	f, err := pm.NewPlanFinding(pm.NewPlanFindingInput{
		ID: id, PlanID: plan, TaskID: task, ProjectID: "P1",
		AuthorRef: "agent:ag1", Kind: kind, Content: content, CreatedAt: at,
	})
	if err != nil {
		t.Fatalf("NewPlanFinding: %v", err)
	}
	return f
}

func TestPlanFindingRepo_RoundTrip(t *testing.T) {
	ctx, repo := newFindingRepo(t)
	f := mkFinding(t, "PF-1", "PL-1", "T-1", pm.FindingFact, "the real bug is on the tuple path", t0)
	if err := repo.Save(ctx, f); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.FindByID(ctx, "PF-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Content() != f.Content() || got.Kind() != f.Kind() || got.PlanID() != "PL-1" ||
		got.TaskID() != "T-1" || got.ProjectID() != "P1" || got.AuthorRef() != "agent:ag1" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if !got.CreatedAt().Equal(t0) {
		t.Fatalf("created_at mismatch: %v want %v", got.CreatedAt(), t0)
	}
}

func TestPlanFindingRepo_Save_DuplicateID(t *testing.T) {
	ctx, repo := newFindingRepo(t)
	f := mkFinding(t, "PF-dup", "PL-1", "T-1", pm.FindingFact, "x", t0)
	if err := repo.Save(ctx, f); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	// a second Save with the same id hits the unique-violation branch → CONFLICT
	// (review #5: not a misleading not-found).
	if err := repo.Save(ctx, f); err != pm.ErrPlanFindingExists {
		t.Fatalf("duplicate Save: want ErrPlanFindingExists, got %v", err)
	}
}

func TestPlanFindingRepo_CountAndListLatest(t *testing.T) {
	ctx, repo := newFindingRepo(t)
	// 5 findings in PL-1 with increasing created_at (so "latest" is deterministic).
	for i := 0; i < 5; i++ {
		_ = repo.Save(ctx, mkFinding(t, pm.PlanFindingID("PF-"+string(rune('0'+i))), "PL-1", "T-1", pm.FindingFact, "c"+string(rune('0'+i)), t0.Add(time.Duration(i)*time.Hour)))
	}
	_ = repo.Save(ctx, mkFinding(t, "PF-x", "PL-2", "T-9", pm.FindingFact, "other", t0))

	if n, err := repo.CountByPlan(ctx, "PL-1"); err != nil || n != 5 {
		t.Fatalf("CountByPlan PL-1: n=%d err=%v want 5", n, err)
	}
	if n, _ := repo.CountByPlan(ctx, "PL-empty"); n != 0 {
		t.Fatalf("CountByPlan empty: n=%d want 0", n)
	}

	// latest 3, re-ordered OLDEST-first for display: PF-2, PF-3, PF-4.
	latest, err := repo.ListLatestByPlan(ctx, "PL-1", 3)
	if err != nil || len(latest) != 3 {
		t.Fatalf("ListLatestByPlan: len=%d err=%v want 3", len(latest), err)
	}
	if latest[0].ID() != "PF-2" || latest[1].ID() != "PF-3" || latest[2].ID() != "PF-4" {
		t.Fatalf("latest window/order wrong: %s,%s,%s", latest[0].ID(), latest[1].ID(), latest[2].ID())
	}
	// limit >= count returns all (oldest-first); limit<=0 returns empty.
	if all, _ := repo.ListLatestByPlan(ctx, "PL-1", 100); len(all) != 5 || all[0].ID() != "PF-0" {
		t.Fatalf("ListLatestByPlan all: len=%d first=%v", len(all), all[0].ID())
	}
	if z, _ := repo.ListLatestByPlan(ctx, "PL-1", 0); len(z) != 0 {
		t.Fatalf("ListLatestByPlan limit 0 should be empty, got %d", len(z))
	}
}

func TestPlanFindingRepo_FindByID_NotFound(t *testing.T) {
	ctx, repo := newFindingRepo(t)
	if _, err := repo.FindByID(ctx, "missing"); err != pm.ErrPlanFindingNotFound {
		t.Fatalf("want ErrPlanFindingNotFound, got %v", err)
	}
}

func TestPlanFindingRepo_ListByPlan_OrderAndScope(t *testing.T) {
	ctx, repo := newFindingRepo(t)
	// Two findings in PL-1 (out-of-order created_at to verify ORDER BY), one in PL-2.
	_ = repo.Save(ctx, mkFinding(t, "PF-2", "PL-1", "T-1", pm.FindingFailure, "later", t0.Add(2*time.Hour)))
	_ = repo.Save(ctx, mkFinding(t, "PF-1", "PL-1", "T-1", pm.FindingFact, "earlier", t0.Add(1*time.Hour)))
	_ = repo.Save(ctx, mkFinding(t, "PF-3", "PL-2", "T-9", pm.FindingConstraint, "other plan", t0))

	got, err := repo.ListByPlan(ctx, "PL-1")
	if err != nil {
		t.Fatalf("ListByPlan: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 findings in PL-1, got %d", len(got))
	}
	// oldest-first (created_at, id): PF-1 (earlier) then PF-2 (later).
	if got[0].ID() != "PF-1" || got[1].ID() != "PF-2" {
		t.Fatalf("order wrong: %s, %s", got[0].ID(), got[1].ID())
	}

	// Empty plan → empty slice, no error.
	empty, err := repo.ListByPlan(ctx, "PL-EMPTY")
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty plan: got %d findings err %v", len(empty), err)
	}
}

func TestPlanFindingRepo_Delete(t *testing.T) {
	ctx, repo := newFindingRepo(t)
	_ = repo.Save(ctx, mkFinding(t, "PF-1", "PL-1", "T-1", pm.FindingFact, "x", t0))

	if err := repo.Delete(ctx, "PF-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.FindByID(ctx, "PF-1"); err != pm.ErrPlanFindingNotFound {
		t.Fatalf("want gone, got %v", err)
	}
	// deleting a missing finding → ErrPlanFindingNotFound
	if err := repo.Delete(ctx, "PF-1"); err != pm.ErrPlanFindingNotFound {
		t.Fatalf("want ErrPlanFindingNotFound on re-delete, got %v", err)
	}
}

func TestPlanFindingRepo_DeleteByPlan(t *testing.T) {
	ctx, repo := newFindingRepo(t)
	_ = repo.Save(ctx, mkFinding(t, "PF-1", "PL-1", "T-1", pm.FindingFact, "a", t0))
	_ = repo.Save(ctx, mkFinding(t, "PF-2", "PL-1", "T-2", pm.FindingFailure, "b", t0))
	_ = repo.Save(ctx, mkFinding(t, "PF-3", "PL-2", "T-9", pm.FindingFact, "c", t0))

	if err := repo.DeleteByPlan(ctx, "PL-1"); err != nil {
		t.Fatalf("DeleteByPlan: %v", err)
	}
	left, _ := repo.ListByPlan(ctx, "PL-1")
	if len(left) != 0 {
		t.Fatalf("PL-1 should be empty, got %d", len(left))
	}
	other, _ := repo.ListByPlan(ctx, "PL-2")
	if len(other) != 1 {
		t.Fatalf("PL-2 untouched, got %d", len(other))
	}
	// cascade over a plan with no findings is a no-op (no error).
	if err := repo.DeleteByPlan(ctx, "PL-NONE"); err != nil {
		t.Fatalf("DeleteByPlan empty: %v", err)
	}
}
