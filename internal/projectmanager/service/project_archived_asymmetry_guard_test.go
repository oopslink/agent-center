package service

import (
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TestProjectArchived_PlanLifecycle_Asymmetry guards the PD-ruled asymmetry of
// the archived-project read-only guard (#297): "archived = no new work starts +
// no edits, NOT freeze in-flight". StartPlan IS guarded (no NEW plan starts on
// an archived project), but StopPlan/AdvancePlan are intentionally NOT guarded
// (a plan already running when the project was archived must still wind down —
// guarding them would deadlock the in-flight plan). This guard pins that the
// requireProjectMutable weaving did NOT over-reach onto the lifecycle ops.
//
// Inverse-mutation: add requireProjectMutable to StopPlan → the StopPlan
// assertion FAILS (it would return ErrProjectArchived).
func TestProjectArchived_PlanLifecycle_Asymmetry(t *testing.T) {
	svc, _, _, _, _, ctx := planSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	t1, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "T1", CreatedBy: "user:a"})
	t2, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "T2", CreatedBy: "user:a"})

	// a DRAFT plan (for the StartPlan-guarded check) ...
	draftPlan, _ := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "Draft", CreatedBy: "user:a"})
	if err := svc.SelectTaskIntoPlan(ctx, draftPlan, t1, "user:a"); err != nil {
		t.Fatal(err)
	}
	// ... and a RUNNING plan (for the StopPlan-not-guarded check), started while active.
	runPlan, _ := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "Running", CreatedBy: "user:a"})
	if err := svc.SelectTaskIntoPlan(ctx, runPlan, t2, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AssignTask(ctx, t2, "user:a", "user:a"); err != nil {
		t.Fatalf("AssignTask: %v", err)
	}
	if err := svc.StartPlan(ctx, runPlan, "user:a"); err != nil {
		t.Fatalf("StartPlan on active project should succeed: %v", err)
	}

	// Archive the project (after one plan is running).
	if err := svc.ArchiveProject(ctx, pid, "user:a"); err != nil {
		t.Fatal(err)
	}

	// GUARDED: starting a NEW plan on an archived project → ErrProjectArchived.
	if err := svc.StartPlan(ctx, draftPlan, "user:a"); err != pm.ErrProjectArchived {
		t.Fatalf("StartPlan on archived → want ErrProjectArchived (guarded), got %v", err)
	}
	// ASYMMETRY (PD ruling): StopPlan on archived must NOT be guarded (the
	// already-running plan winds down). Must NOT return ErrProjectArchived.
	if err := svc.StopPlan(ctx, runPlan, "user:a"); err == pm.ErrProjectArchived {
		t.Fatalf("StopPlan on archived must NOT be guarded (wind-down allowed) — got ErrProjectArchived (asymmetry broken)")
	}
}
