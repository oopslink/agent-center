package projectmanager

import (
	"testing"
	"time"
)

// newTaskWithStatus builds a Task in the given status for derivation tests.
func newTaskWithStatus(t *testing.T, id string, status TaskStatus) *Task {
	t.Helper()
	now := time.Now()
	tk, err := NewTask(NewTaskInput{
		ID: TaskID(id), ProjectID: "p1", Title: "t-" + id, CreatedBy: IdentityRef("user:c"), CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}
	// Drive to the target status via the free SetStatus (no adjacency needed here;
	// SetStatus allows any valid target).
	if status != TaskOpen {
		if err := tk.SetStatus(status, now); err != nil {
			t.Fatalf("SetStatus(%s): %v", status, err)
		}
	}
	return tk
}

func TestDeriveNodeStatus_FiveStates(t *testing.T) {
	cases := []struct {
		name            string
		taskStatus      TaskStatus
		upstreamAllDone bool
		dispatched      bool
		want            NodeStatus
	}{
		{"done-completed", TaskCompleted, true, true, NodeDone},
		{"failed-discarded", TaskDiscarded, true, true, NodeFailed},
		{"running", TaskRunning, true, true, NodeRunning},
		{"blocked-upstream-not-done", TaskOpen, false, false, NodeBlocked},
		{"blocked-even-if-dispatched-flag", TaskOpen, false, true, NodeBlocked},
		{"ready-all-upstream-done-not-dispatched", TaskOpen, true, false, NodeReady},
		{"dispatched-all-upstream-done-dispatched", TaskOpen, true, true, NodeDispatched},
		{"reopened-upstream-not-done-blocked", TaskReopened, false, false, NodeBlocked},
		// done/failed take precedence over upstream gating.
		{"done-precedence-over-blocked", TaskCompleted, false, false, NodeDone},
		{"failed-precedence-over-blocked", TaskDiscarded, false, false, NodeFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DeriveNodeStatus(c.taskStatus, c.upstreamAllDone, c.dispatched)
			if got != c.want {
				t.Fatalf("DeriveNodeStatus(%s,%v,%v)=%s want %s", c.taskStatus, c.upstreamAllDone, c.dispatched, got, c.want)
			}
		})
	}
}

// A→{B,C}: A done → B and C are ready (both branches). No dispatch records yet.
func TestComputePlanView_ReadySet_FanOut(t *testing.T) {
	a := newTaskWithStatus(t, "A", TaskCompleted)
	b := newTaskWithStatus(t, "B", TaskOpen)
	c := newTaskWithStatus(t, "C", TaskOpen)
	tasks := []*Task{a, b, c}
	edges := []Dependency{
		{PlanID: "pl", FromTaskID: "B", ToTaskID: "A"}, // B depends_on A
		{PlanID: "pl", FromTaskID: "C", ToTaskID: "A"}, // C depends_on A
	}
	view := ComputePlanView(tasks, edges, nil)
	if len(view.ReadySet) != 2 {
		t.Fatalf("ready-set=%v want [B C]", view.ReadySet)
	}
	gotReady := map[TaskID]bool{}
	for _, id := range view.ReadySet {
		gotReady[id] = true
	}
	if !gotReady["B"] || !gotReady["C"] {
		t.Fatalf("ready-set=%v want B and C", view.ReadySet)
	}
	if view.Progress.Done != 1 || view.Progress.Total != 3 {
		t.Fatalf("progress=%+v want {1 3}", view.Progress)
	}
	if view.AllDone {
		t.Fatal("AllDone should be false")
	}
}

// Dispatched node is NodeDispatched (not ready, not running) until task starts.
func TestComputePlanView_DispatchedNotReady(t *testing.T) {
	a := newTaskWithStatus(t, "A", TaskCompleted)
	b := newTaskWithStatus(t, "B", TaskOpen)
	tasks := []*Task{a, b}
	edges := []Dependency{{PlanID: "pl", FromTaskID: "B", ToTaskID: "A"}}
	records := []DispatchRecord{{PlanID: "pl", TaskID: "B", DispatchedAt: time.Now(), DispatchMessageID: "m1"}}
	view := ComputePlanView(tasks, edges, records)
	if len(view.ReadySet) != 0 {
		t.Fatalf("ready-set=%v want empty (B already dispatched)", view.ReadySet)
	}
	var bNode *PlanNodeView
	for i := range view.Nodes {
		if view.Nodes[i].TaskID == "B" {
			bNode = &view.Nodes[i]
		}
	}
	if bNode == nil || bNode.NodeStatus != NodeDispatched {
		t.Fatalf("B node=%+v want dispatched", bNode)
	}
}

// §9.7: a failed node blocks ONLY its dependent subtree; independent branch
// still advances. DAG: A→B (B depends A), and an independent chain X→Y. A failed.
//
//	A(failed) → B should be blocked.
//	X(done)   → Y should be ready (independent branch advances).
func TestComputePlanView_FailureIsolation(t *testing.T) {
	a := newTaskWithStatus(t, "A", TaskDiscarded) // failed
	b := newTaskWithStatus(t, "B", TaskOpen)
	x := newTaskWithStatus(t, "X", TaskCompleted) // done
	y := newTaskWithStatus(t, "Y", TaskOpen)
	tasks := []*Task{a, b, x, y}
	edges := []Dependency{
		{PlanID: "pl", FromTaskID: "B", ToTaskID: "A"}, // B depends_on A (failed)
		{PlanID: "pl", FromTaskID: "Y", ToTaskID: "X"}, // Y depends_on X (done)
	}
	view := ComputePlanView(tasks, edges, nil)
	byID := map[TaskID]NodeStatus{}
	for _, n := range view.Nodes {
		byID[n.TaskID] = n.NodeStatus
	}
	if byID["A"] != NodeFailed {
		t.Fatalf("A=%s want failed", byID["A"])
	}
	if byID["B"] != NodeBlocked {
		t.Fatalf("B=%s want blocked (downstream of failed A)", byID["B"])
	}
	if byID["Y"] != NodeReady {
		t.Fatalf("Y=%s want ready (independent branch advances)", byID["Y"])
	}
	if !view.HasFailed {
		t.Fatal("HasFailed should be true")
	}
	// ready-set is exactly {Y}.
	if len(view.ReadySet) != 1 || view.ReadySet[0] != "Y" {
		t.Fatalf("ready-set=%v want [Y]", view.ReadySet)
	}
}

// §9.1: AllDone only when EVERY node done; a failed node keeps it not-done.
func TestComputePlanView_AllDone(t *testing.T) {
	t.Run("all done", func(t *testing.T) {
		a := newTaskWithStatus(t, "A", TaskCompleted)
		b := newTaskWithStatus(t, "B", TaskCompleted)
		view := ComputePlanView([]*Task{a, b}, nil, nil)
		if !view.AllDone {
			t.Fatal("AllDone should be true when all nodes done")
		}
		if view.Progress.Done != 2 || view.Progress.Total != 2 {
			t.Fatalf("progress=%+v want {2 2}", view.Progress)
		}
	})
	t.Run("one failed keeps not-done", func(t *testing.T) {
		a := newTaskWithStatus(t, "A", TaskCompleted)
		b := newTaskWithStatus(t, "B", TaskDiscarded)
		view := ComputePlanView([]*Task{a, b}, nil, nil)
		if view.AllDone {
			t.Fatal("AllDone must be false when a node is failed (§9.1)")
		}
		if !view.HasFailed {
			t.Fatal("HasFailed should be true")
		}
	})
	t.Run("empty plan is not all-done", func(t *testing.T) {
		view := ComputePlanView(nil, nil, nil)
		if view.AllDone {
			t.Fatal("an empty plan is not AllDone")
		}
	})
}
