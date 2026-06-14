package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/environment"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

type fakeWI struct {
	items []*agentpkg.AgentWorkItem
	byID  map[string]*agentpkg.AgentWorkItem
}

func (f *fakeWI) ListByTask(_ context.Context, _ string) ([]*agentpkg.AgentWorkItem, error) {
	return f.items, nil
}
func (f *fakeWI) FindByID(_ context.Context, id string) (*agentpkg.AgentWorkItem, error) {
	return f.byID[id], nil
}

type fakeResumer struct {
	resumed string
	agentID agentpkg.AgentID
	err     error
}

func (f *fakeResumer) ResumeWorkByOperator(_ context.Context, id string) (agentpkg.AgentID, error) {
	f.resumed = id
	return f.agentID, f.err
}

type fakeAgents struct {
	a   *agentpkg.Agent
	err error
}

func (f *fakeAgents) FindByID(_ context.Context, _ agentpkg.AgentID) (*agentpkg.Agent, error) {
	return f.a, f.err
}

type fakeWaker struct{ cmds []environment.AppendCommandInput }

func (f *fakeWaker) EnqueueCommand(_ context.Context, in environment.AppendCommandInput) (*environment.WorkerControlEvent, error) {
	f.cmds = append(f.cmds, in)
	return nil, nil
}

func wi(t *testing.T, id string, status agentpkg.WorkItemStatus) *agentpkg.AgentWorkItem {
	t.Helper()
	w, err := agentpkg.RehydrateWorkItem(agentpkg.RehydrateWorkItemInput{
		ID: id, AgentID: "AG1", TaskRef: "pm://tasks/task-1", Status: status,
		CreatedAt: time.Unix(0, 0), UpdatedAt: time.Unix(0, 0), Version: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func agentWithWorker(t *testing.T, worker string) *agentpkg.Agent {
	t.Helper()
	a, err := agentpkg.NewAgent(agentpkg.NewAgentInput{
		ID: "AG1", OrganizationID: "org-1", Profile: agentpkg.Profile{Name: "AG1"},
		WorkerID: worker, CreatedBy: "system", CreatedAt: time.Unix(0, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// T53: the adapter resumes the task's paused work item then wakes its agent.
func TestNodeResumerAdapter_ResumesAndWakes(t *testing.T) {
	paused := wi(t, "wi-1", agentpkg.WorkItemPaused)
	f := &fakeWI{items: []*agentpkg.AgentWorkItem{paused}, byID: map[string]*agentpkg.AgentWorkItem{"wi-1": paused}}
	res := &fakeResumer{agentID: "AG1"}
	waker := &fakeWaker{}
	a := NewNodeResumerAdapter(f, res, &fakeAgents{a: agentWithWorker(t, "W1")}, waker)

	if err := a.ResumePausedNode(context.Background(), "pm://tasks/task-1"); err != nil {
		t.Fatalf("ResumePausedNode: %v", err)
	}
	if res.resumed != "wi-1" {
		t.Fatalf("resumed=%q want wi-1", res.resumed)
	}
	if len(waker.cmds) != 1 {
		t.Fatalf("wake cmds=%d want 1", len(waker.cmds))
	}
	if waker.cmds[0].CommandType != commandTypeWorkAvailable || waker.cmds[0].WorkerID != "W1" {
		t.Fatalf("wake cmd=%+v want work_available to W1", waker.cmds[0])
	}
}

// No paused item → ErrNodeNotPaused, and no resume/wake happens.
func TestNodeResumerAdapter_NoPaused(t *testing.T) {
	active := wi(t, "wi-1", agentpkg.WorkItemActive)
	f := &fakeWI{items: []*agentpkg.AgentWorkItem{active}}
	res := &fakeResumer{agentID: "AG1"}
	waker := &fakeWaker{}
	a := NewNodeResumerAdapter(f, res, &fakeAgents{a: agentWithWorker(t, "W1")}, waker)

	if err := a.ResumePausedNode(context.Background(), "pm://tasks/task-1"); !errors.Is(err, pmservice.ErrNodeNotPaused) {
		t.Fatalf("err=%v want ErrNodeNotPaused", err)
	}
	if res.resumed != "" || len(waker.cmds) != 0 {
		t.Fatalf("no resume/wake expected; resumed=%q cmds=%d", res.resumed, len(waker.cmds))
	}
}

// When the agent can't be resolved for the wake → resume still succeeds, the wake
// is skipped best-effort (the item is active; AgentControlProjector re-delivers on
// lifecycle→running) — a wake failure never masks the successful resume.
func TestNodeResumerAdapter_UnresolvedAgent_SkipsWake(t *testing.T) {
	paused := wi(t, "wi-1", agentpkg.WorkItemPaused)
	f := &fakeWI{items: []*agentpkg.AgentWorkItem{paused}, byID: map[string]*agentpkg.AgentWorkItem{"wi-1": paused}}
	res := &fakeResumer{agentID: "AG1"}
	waker := &fakeWaker{}
	a := NewNodeResumerAdapter(f, res, &fakeAgents{err: errors.New("gone")}, waker)

	if err := a.ResumePausedNode(context.Background(), "pm://tasks/task-1"); err != nil {
		t.Fatalf("ResumePausedNode: %v", err)
	}
	if res.resumed != "wi-1" {
		t.Fatalf("resume should still happen; resumed=%q", res.resumed)
	}
	if len(waker.cmds) != 0 {
		t.Fatalf("wake must be skipped when the agent is unresolved; cmds=%d", len(waker.cmds))
	}
}
