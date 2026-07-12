package sqlite

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func stageSetup(t *testing.T) (context.Context, *StageRepo, *TaskRepo) {
	t.Helper()
	d, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(d).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return context.Background(), NewStageRepo(d), NewTaskRepo(d)
}

func TestStageRepo_RoundTrip(t *testing.T) {
	ctx, sr, _ := stageSetup(t)
	st, err := pm.NewStage(pm.NewStageInput{
		ID: "st-1", PlanID: "plan-1", Name: "Dev batch",
		DependsOnStages: []pm.StageID{"st-0"}, MaxRounds: 5, CreatedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	st.SetGateNodeID("gate-node", t0)
	if err := sr.Save(ctx, st); err != nil {
		t.Fatalf("save: %v", err)
	}
	// duplicate save → ErrStageExists.
	if err := sr.Save(ctx, st); err != pm.ErrStageExists {
		t.Fatalf("dup save = %v, want ErrStageExists", err)
	}
	got, err := sr.FindByID(ctx, "st-1")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.Name() != "Dev batch" || got.PlanID() != "plan-1" || got.MaxRounds() != 5 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.GateNodeID() != "gate-node" {
		t.Fatalf("gate = %q, want gate-node", got.GateNodeID())
	}
	deps := got.DependsOnStages()
	if len(deps) != 1 || deps[0] != "st-0" {
		t.Fatalf("deps = %v, want [st-0]", deps)
	}

	// Update path.
	if err := got.Rename("Dev batch v2", t0); err != nil {
		t.Fatal(err)
	}
	if err := sr.Update(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	re, _ := sr.FindByID(ctx, "st-1")
	if re.Name() != "Dev batch v2" {
		t.Fatalf("name = %q after update", re.Name())
	}

	// ListByPlan + DeleteByPlan.
	st2, _ := pm.NewStage(pm.NewStageInput{ID: "st-2", PlanID: "plan-1", Name: "QA", CreatedAt: t0})
	if err := sr.Save(ctx, st2); err != nil {
		t.Fatal(err)
	}
	list, err := sr.ListByPlan(ctx, "plan-1")
	if err != nil || len(list) != 2 {
		t.Fatalf("ListByPlan = %d stages (%v)", len(list), err)
	}
	if err := sr.DeleteByPlan(ctx, "plan-1"); err != nil {
		t.Fatal(err)
	}
	after, _ := sr.ListByPlan(ctx, "plan-1")
	if len(after) != 0 {
		t.Fatalf("after DeleteByPlan = %d stages, want 0", len(after))
	}
}

func TestStageRepo_NotFound(t *testing.T) {
	ctx, sr, _ := stageSetup(t)
	if _, err := sr.FindByID(ctx, "ghost"); err != pm.ErrStageNotFound {
		t.Fatalf("find ghost = %v, want ErrStageNotFound", err)
	}
}

// TestTask_StageIDRoundTrip locks the pm_tasks.stage_id column round-trip (§4.1) and
// the §8 default: a task created with no stage reads back "" (stageless).
func TestTask_StageIDRoundTrip(t *testing.T) {
	ctx, _, tr := stageSetup(t)
	staged, _ := pm.NewTask(pm.NewTaskInput{
		ID: "task-staged", ProjectID: "proj", Title: "staged", CreatedBy: "user:u",
		CreatedAt: t0, StageID: "st-1",
	})
	if err := tr.Save(ctx, staged); err != nil {
		t.Fatal(err)
	}
	plain, _ := pm.NewTask(pm.NewTaskInput{
		ID: "task-plain", ProjectID: "proj", Title: "plain", CreatedBy: "user:u", CreatedAt: t0,
	})
	if err := tr.Save(ctx, plain); err != nil {
		t.Fatal(err)
	}
	got, _ := tr.FindByID(ctx, "task-staged")
	if got.StageID() != "st-1" {
		t.Fatalf("staged task stage = %q, want st-1", got.StageID())
	}
	gotPlain, _ := tr.FindByID(ctx, "task-plain")
	if gotPlain.StageID() != "" {
		t.Fatalf("plain task stage = %q, want empty (§8)", gotPlain.StageID())
	}
	// Update path: SetStage then persist.
	if err := gotPlain.SetStage("st-2", t0); err != nil {
		t.Fatal(err)
	}
	if err := tr.Update(ctx, gotPlain); err != nil {
		t.Fatal(err)
	}
	re, _ := tr.FindByID(ctx, "task-plain")
	if re.StageID() != "st-2" {
		t.Fatalf("stage after update = %q, want st-2", re.StageID())
	}
}
