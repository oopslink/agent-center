package service

import (
	"errors"
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TestUpdatePlan_PartialUpdate_NilFieldsUnchanged guards the partial-update
// contract A3's edit modal relies on: UpdatePlan touches ONLY fields whose
// pointer is non-nil — a nil Name/Description must leave the existing value.
func TestUpdatePlan_PartialUpdate_NilFieldsUnchanged(t *testing.T) {
	svc, _, plans, _, _, ctx := planSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "Orig", CreatedBy: "user:a"})

	// Description-only update → Name must stay "Orig".
	desc := "D1"
	if err := svc.UpdatePlan(ctx, UpdatePlanCommand{PlanID: planID, Description: &desc, Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	p, _ := plans.FindByID(ctx, planID)
	if p.Name() != "Orig" {
		t.Fatalf("Name = %q after Description-only update, want unchanged Orig", p.Name())
	}
	if p.Description() != "D1" {
		t.Fatalf("Description = %q, want D1", p.Description())
	}

	// Name-only update → Description must stay "D1".
	name := "N2"
	if err := svc.UpdatePlan(ctx, UpdatePlanCommand{PlanID: planID, Name: &name, Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	p, _ = plans.FindByID(ctx, planID)
	if p.Name() != "N2" {
		t.Fatalf("Name = %q, want N2", p.Name())
	}
	if p.Description() != "D1" {
		t.Fatalf("Description = %q after Name-only update, want unchanged D1", p.Description())
	}
}

// TestUpdatePlan_TargetDateSet_ThreeStates guards the subtle TargetDateSet
// semantics: the flag distinguishes set-a-value / clear (nil) / leave-unchanged
// (flag=false). Without the flag, "clear" and "don't touch" are indistinguishable.
func TestUpdatePlan_TargetDateSet_ThreeStates(t *testing.T) {
	svc, _, plans, _, _, ctx := planSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "Orig", CreatedBy: "user:a"})

	// 1) flag=true + value → set.
	d := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	if err := svc.UpdatePlan(ctx, UpdatePlanCommand{PlanID: planID, TargetDateSet: true, TargetDate: &d, Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	p, _ := plans.FindByID(ctx, planID)
	if p.TargetDate() == nil || !p.TargetDate().Equal(d) {
		t.Fatalf("TargetDate = %v, want %v", p.TargetDate(), d)
	}

	// 2) flag=false → UNCHANGED (still d) even though the command carries another date.
	other := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := svc.UpdatePlan(ctx, UpdatePlanCommand{PlanID: planID, TargetDateSet: false, TargetDate: &other, Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	p, _ = plans.FindByID(ctx, planID)
	if p.TargetDate() == nil || !p.TargetDate().Equal(d) {
		t.Fatalf("TargetDate = %v after flag=false update, want unchanged %v", p.TargetDate(), d)
	}

	// 3) flag=true + nil → CLEARED.
	if err := svc.UpdatePlan(ctx, UpdatePlanCommand{PlanID: planID, TargetDateSet: true, TargetDate: nil, Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	p, _ = plans.FindByID(ctx, planID)
	if p.TargetDate() != nil {
		t.Fatalf("TargetDate = %v after clear, want nil", p.TargetDate())
	}
}

// TestUpdatePlan_NonDraft_NameDescEditable_TargetDateRejected guards T238: name +
// description (descriptive metadata) ARE editable on a running plan, but
// target_date (scheduling, §9.4) is still draft-only.
func TestUpdatePlan_NonDraft_NameDescEditable_TargetDateRejected(t *testing.T) {
	svc, _, plans, _, _, ctx := planSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "Orig", CreatedBy: "user:a"})
	taskA, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "A", CreatedBy: "user:a"})
	if err := svc.SelectTaskIntoPlan(ctx, planID, taskA, "user:a"); err != nil {
		t.Fatal(err)
	}
	p, _ := plans.FindByID(ctx, planID)
	if err := p.Start(svc.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := plans.Update(ctx, p); err != nil {
		t.Fatal(err)
	}

	// name + description on a RUNNING plan → allowed.
	name, desc := "Renamed", "new goal"
	if err := svc.UpdatePlan(ctx, UpdatePlanCommand{PlanID: planID, Name: &name, Description: &desc, Actor: "user:a"}); err != nil {
		t.Fatalf("UpdatePlan(name+desc) on running plan = %v, want nil", err)
	}
	got, _ := plans.FindByID(ctx, planID)
	if got.Name() != "Renamed" || got.Description() != "new goal" {
		t.Fatalf("running-plan edit: name=%q desc=%q, want Renamed/new goal", got.Name(), got.Description())
	}

	// target_date on a RUNNING plan → still rejected (draft-only, §9.4).
	d := svc.clock.Now()
	if err := svc.UpdatePlan(ctx, UpdatePlanCommand{PlanID: planID, TargetDateSet: true, TargetDate: &d, Actor: "user:a"}); !errors.Is(err, pm.ErrPlanNotDraft) {
		t.Fatalf("UpdatePlan(target_date) on running plan = %v, want ErrPlanNotDraft", err)
	}
}

// TestUpdatePlan_RejectsArchived guards T238: an archived (terminal, read-only)
// plan rejects every edit, including name/description.
func TestUpdatePlan_RejectsArchived(t *testing.T) {
	svc, _, plans, _, _, ctx := planSetup(t)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "Orig", CreatedBy: "user:a"})
	p, _ := plans.FindByID(ctx, planID)
	if err := p.Archive(svc.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := plans.Update(ctx, p); err != nil {
		t.Fatal(err)
	}
	name := "X"
	if err := svc.UpdatePlan(ctx, UpdatePlanCommand{PlanID: planID, Name: &name, Actor: "user:a"}); !errors.Is(err, pm.ErrPlanArchived) {
		t.Fatalf("UpdatePlan on archived plan = %v, want ErrPlanArchived", err)
	}
}
