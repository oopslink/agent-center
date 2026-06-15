package service

import (
	"context"
	"errors"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// fakeNodeResumer records the taskRef it was asked to resume + an optional error.
type fakeNodeResumer struct {
	gotTaskRef string
	calls      int
	err        error
}

func (f *fakeNodeResumer) ResumePausedNode(_ context.Context, taskRef string) error {
	f.calls++
	f.gotTaskRef = taskRef
	return f.err
}

// fakePausedPort is a test PausedTaskPort: it reports the configured task ids as
// paused (intersected with the requested set, like the real adapter).
type fakePausedPort struct{ paused map[string]bool }

func (f fakePausedPort) PausedTasks(_ context.Context, taskIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, id := range taskIDs {
		if f.paused[id] {
			out[id] = true
		}
	}
	return out, nil
}

func nodeStatusOf(d *PlanDetail, tid pm.TaskID) pm.NodeStatus {
	for _, n := range d.View.Nodes {
		if n.TaskID == tid {
			return n.NodeStatus
		}
	}
	return ""
}

// T53: a RUNNING plan node whose work item is paused derives node_status=paused
// through the read model (GetPlanDetail), once the PausedTaskPort is wired — and
// stays `running` without it. This is the display-truth half of the fix: the DAG
// stops mis-showing a set-aside node as running.
func TestGetPlanDetail_PausedOverlay(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "alpha", CreatedBy: "user:a"})
	h.drain(t)
	tRun := h.seedAssignedTask(t, pid, planID, "running-node", "agent:AG1")
	h.setTaskStatus(t, tRun, pm.TaskRunning)

	// No provider wired → the running node shows `running` (pre-T53 behavior, and the
	// nil-safe default).
	detail, err := h.svc.GetPlanDetail(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if got := nodeStatusOf(detail, tRun); got != pm.NodeRunning {
		t.Fatalf("no-provider node_status=%s want running", got)
	}

	// Wire a provider marking the task paused → the SAME running task now derives
	// `paused` in the read model.
	h.svc.SetPausedTaskProvider(fakePausedPort{paused: map[string]bool{string(tRun): true}})
	detail, err = h.svc.GetPlanDetail(h.ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if got := nodeStatusOf(detail, tRun); got != pm.NodePaused {
		t.Fatalf("with-provider node_status=%s want paused (work item paused)", got)
	}
	if detail.View.AllDone {
		t.Fatal("AllDone must stay false while a node is paused")
	}

	// ListPlanSummaries (the Work Board batched read) overlays paused too.
	summaries, err := h.svc.ListPlanSummaries(h.ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, d := range summaries {
		if d.Plan.ID() == planID {
			found = true
			if got := nodeStatusOf(d, tRun); got != pm.NodePaused {
				t.Fatalf("summary node_status=%s want paused", got)
			}
		}
	}
	if !found {
		t.Fatal("plan missing from summaries")
	}
}

// T53 Part B: ResumePausedNode authorizes (project member + plan running + task in
// plan) then delegates the cross-BC resume+wake to the NodeResumer port.
func TestResumePausedNode_AuthzAndDelegation(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "alpha", CreatedBy: "user:a"})
	h.drain(t)
	tid := h.seedAssignedTask(t, pid, planID, "node", "agent:AG1")

	// No resumer wired → unavailable.
	if err := h.svc.ResumePausedNode(h.ctx, planID, tid, "user:a"); !errors.Is(err, ErrNodeResumerUnavailable) {
		t.Fatalf("no-resumer err=%v want ErrNodeResumerUnavailable", err)
	}

	fake := &fakeNodeResumer{}
	h.svc.SetNodeResumer(fake)

	// Plan not running yet (draft) → ErrPlanNotRunning, port NOT called.
	if err := h.svc.ResumePausedNode(h.ctx, planID, tid, "user:a"); !errors.Is(err, pm.ErrPlanNotRunning) {
		t.Fatalf("draft-plan err=%v want ErrPlanNotRunning", err)
	}
	if fake.calls != 0 {
		t.Fatalf("port must not be called when plan not running; calls=%d", fake.calls)
	}

	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}

	// Non-member actor → rejected (requireProjectMember), port NOT called.
	if err := h.svc.ResumePausedNode(h.ctx, planID, tid, "user:stranger"); err == nil {
		t.Fatal("non-member must be rejected")
	}
	if fake.calls != 0 {
		t.Fatalf("port must not be called for a non-member; calls=%d", fake.calls)
	}

	// Task not in this plan → ErrTaskNotInPlan.
	otherTask, _ := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "loose", CreatedBy: "user:a"})
	if err := h.svc.ResumePausedNode(h.ctx, planID, otherTask, "user:a"); !errors.Is(err, ErrTaskNotInPlan) {
		t.Fatalf("foreign-task err=%v want ErrTaskNotInPlan", err)
	}

	// Happy path: member + running + in-plan → the port is called with the task ref.
	if err := h.svc.ResumePausedNode(h.ctx, planID, tid, "user:a"); err != nil {
		t.Fatalf("ResumePausedNode happy path: %v", err)
	}
	if fake.calls != 1 || fake.gotTaskRef != "pm://tasks/"+string(tid) {
		t.Fatalf("port call=%d ref=%q want 1 call with pm://tasks/%s", fake.calls, fake.gotTaskRef, tid)
	}

	// The port's error propagates (e.g. nothing paused).
	fake.err = ErrNodeNotPaused
	if err := h.svc.ResumePausedNode(h.ctx, planID, tid, "user:a"); !errors.Is(err, ErrNodeNotPaused) {
		t.Fatalf("propagated err=%v want ErrNodeNotPaused", err)
	}
}
