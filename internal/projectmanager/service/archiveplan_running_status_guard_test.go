package service

import (
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TestArchivePlan_RunningTaskOnly_StatusCoverage is the #299 class-guard for the
// ArchivePlan running-task precondition (@oopslink directive): archiving a plan
// is blocked ONLY when a member task is in TaskRunning (an in-flight task an
// agent is executing would be orphaned). PD specced the precise set: open/
// completed/discarded/reopened do NOT count as running (ADR-0046: blocked/
// verified deleted). This guards against the precondition OVER-reaching (e.g.
// treating a non-executing active task — reopened — as blocking) or under-checking.
//
// Inverse-mutation: drop the running-task check in ArchivePlan → running_blocks
// FAILS. Broaden it to also block TaskReopened → reopened_does_not_block FAILS.
func TestArchivePlan_RunningTaskOnly_StatusCoverage(t *testing.T) {
	archivable := func(t *testing.T) (*planRemovalHarness, pm.PlanID, pm.TaskID) {
		h := planRemovalSetup(t)
		pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
		planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "p", CreatedBy: "user:a"})
		h.drain(t)
		a := h.seedTaskInPlan(t, pid, planID, "a", "user:x")
		return h, planID, a
	}

	t.Run("running_blocks", func(t *testing.T) {
		h, planID, a := archivable(t)
		if err := h.svc.SetTaskStatus(h.ctx, a, pm.TaskRunning, "user:a"); err != nil {
			t.Fatal(err)
		}
		if err := h.svc.ArchivePlan(h.ctx, planID, "user:a"); err != pm.ErrPlanHasRunningTasks {
			t.Fatalf("running member task → want ErrPlanHasRunningTasks, got %v", err)
		}
	})

	t.Run("reopened_does_not_block", func(t *testing.T) {
		h, planID, a := archivable(t)
		if err := h.svc.SetTaskStatus(h.ctx, a, pm.TaskReopened, "user:a"); err != nil {
			t.Fatal(err)
		}
		// reopened is active-but-not-executing → must NOT trip the running-task gate.
		if err := h.svc.ArchivePlan(h.ctx, planID, "user:a"); err == pm.ErrPlanHasRunningTasks {
			t.Fatalf("reopened member task must NOT block archive (only TaskRunning blocks) — got ErrPlanHasRunningTasks")
		}
	})
}
