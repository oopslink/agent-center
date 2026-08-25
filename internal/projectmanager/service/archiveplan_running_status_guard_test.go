package service

import (
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// Archive is terminal-only and orthogonal: active Plan/Task state must first be
// settled through DiscardPlan, which permanently closes remaining Task history.
func TestArchivePlan_TerminalOnly(t *testing.T) {
	archivable := func(t *testing.T) (*planRemovalHarness, pm.PlanID, pm.TaskID) {
		h := planRemovalSetup(t)
		pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
		planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "p", CreatedBy: "user:a", OwnerRef: "user:a"})
		h.drain(t)
		a := h.seedTaskInPlan(t, pid, planID, "a", "user:x")
		return h, planID, a
	}

	t.Run("active_plan_rejected", func(t *testing.T) {
		h, planID, a := archivable(t)
		if err := h.svc.SetTaskStatus(h.ctx, a, pm.TaskRunning, "user:a"); err != nil {
			t.Fatal(err)
		}
		if err := h.svc.ArchivePlan(h.ctx, planID, "user:a"); err != pm.ErrPlanNotTerminal {
			t.Fatalf("active plan → want ErrPlanNotTerminal, got %v", err)
		}
	})

	t.Run("discard_then_archive", func(t *testing.T) {
		h, planID, a := archivable(t)
		if err := h.svc.SetTaskStatus(h.ctx, a, pm.TaskRunning, "user:a"); err != nil {
			t.Fatal(err)
		}
		if err := h.svc.DiscardPlan(h.ctx, planID, "user:a"); err != nil {
			t.Fatal(err)
		}
		if err := h.svc.ArchivePlan(h.ctx, planID, "user:a"); err != nil {
			t.Fatal(err)
		}
	})
}
