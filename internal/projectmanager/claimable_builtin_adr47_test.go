package projectmanager

import (
	"testing"
	"time"
)

// ADR-0047 claimable predicate class-guard. claimable(t) :=
//
//	!archived ∧ status==open ∧ assignee!="" ∧ planID!="" ∧ nodeStatus==dispatched.
//
// Every other combination is NOT claimable.
func TestADR47_Claimable_Predicate(t *testing.T) {
	const (
		notArch = false
		arch    = true
	)
	// The one fully-claimable baseline.
	if !Claimable(notArch, TaskOpen, "agent:a", "plan-1", NodeDispatched) {
		t.Fatal("baseline (open + assigned + in-plan + dispatched + not archived) must be claimable")
	}
	// Each single condition flipped → not claimable.
	notClaimable := []struct {
		name      string
		archived  bool
		status    TaskStatus
		assignee  IdentityRef
		planID    PlanID
		nodeState NodeStatus
	}{
		{"archived", arch, TaskOpen, "agent:a", "plan-1", NodeDispatched},
		{"running (already claimed)", notArch, TaskRunning, "agent:a", "plan-1", NodeRunning},
		{"completed", notArch, TaskCompleted, "agent:a", "plan-1", NodeDone},
		{"discarded", notArch, TaskDiscarded, "agent:a", "plan-1", NodeFailed},
		{"reopened", notArch, TaskReopened, "agent:a", "plan-1", NodeDispatched},
		{"no assignee", notArch, TaskOpen, "", "plan-1", NodeDispatched},
		{"backlog (no plan)", notArch, TaskOpen, "agent:a", "", NodeReady},
		{"in plan but only ready (not dispatched)", notArch, TaskOpen, "agent:a", "plan-1", NodeReady},
		{"in plan but blocked", notArch, TaskOpen, "agent:a", "plan-1", NodeBlocked},
	}
	for _, c := range notClaimable {
		if Claimable(c.archived, c.status, c.assignee, c.planID, c.nodeState) {
			t.Errorf("%s: must NOT be claimable", c.name)
		}
	}
}

// ADR-0047 built-in pool invariants: always-started (cannot stop / mark done).
func TestADR47_BuiltinPlan_Immutable(t *testing.T) {
	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	p, err := NewPlan(NewPlanInput{
		ID: "plan-builtin", ProjectID: "proj-1", Name: "[Built-in]",
		CreatorRef: "system", Builtin: true, CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !p.IsBuiltin() {
		t.Fatal("plan must report builtin")
	}
	_ = p.Start(now) // builtin is started by the service; allowed
	if err := p.Stop(now); err != ErrBuiltinPlanImmutable {
		t.Fatalf("Stop on builtin = %v, want ErrBuiltinPlanImmutable", err)
	}
	if err := p.MarkDone(now); err != ErrBuiltinPlanImmutable {
		t.Fatalf("MarkDone on builtin = %v, want ErrBuiltinPlanImmutable", err)
	}

	// A normal (non-builtin) plan can stop/mark-done as before.
	np, _ := NewPlan(NewPlanInput{ID: "plan-2", ProjectID: "proj-1", Name: "Sprint", CreatorRef: "user:o", CreatedAt: now})
	_ = np.Start(now)
	if err := np.Stop(now); err != nil {
		t.Fatalf("Stop on normal plan should work, got %v", err)
	}
}
