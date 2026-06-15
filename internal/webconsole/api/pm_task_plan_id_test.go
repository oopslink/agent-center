package api

import (
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// T106: the task DTO carries plan_id when the task is in a plan (so the Task
// detail sidebar can show + link the owning plan), and omits it for a backlog
// task (zero-noise; the UI hides the Plan section).
func TestPmTaskMap_PlanID(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	tk, err := pm.NewTask(pm.NewTaskInput{
		ID: "task-1", ProjectID: "proj-1", Title: "x", CreatedBy: "user:a", CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Backlog (no plan) → plan_id omitted.
	if _, ok := pmTaskMap(tk)["plan_id"]; ok {
		t.Fatal("backlog task DTO must omit plan_id")
	}
	// Selected into a plan → plan_id present.
	if err := tk.SetPlan("plan-9", now); err != nil {
		t.Fatal(err)
	}
	if got := pmTaskMap(tk)["plan_id"]; got != "plan-9" {
		t.Fatalf("plan_id=%v, want plan-9", got)
	}
}
