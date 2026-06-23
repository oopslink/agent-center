package cli

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// --- fakes -----------------------------------------------------------------

type fakePMReads struct {
	loads    map[pm.IdentityRef]pm.AgentTaskLoad
	runnable map[pm.IdentityRef][]*pm.Task
}

func (f fakePMReads) AgentTaskLoads(context.Context) (map[pm.IdentityRef]pm.AgentTaskLoad, error) {
	return f.loads, nil
}
func (f fakePMReads) ListRunnableAgentTasks(_ context.Context, a pm.IdentityRef) ([]*pm.Task, error) {
	return f.runnable[a], nil
}

type fakeAgentReads struct {
	byMember map[string]*agent.Agent
	byID     map[string]*agent.Agent
}

func (f fakeAgentReads) FindByIdentityMemberID(_ context.Context, id string) (*agent.Agent, error) {
	if a, ok := f.byMember[id]; ok {
		return a, nil
	}
	return nil, agent.ErrAgentNotFound
}
func (f fakeAgentReads) FindByID(_ context.Context, id agent.AgentID) (*agent.Agent, error) {
	if a, ok := f.byID[string(id)]; ok {
		return a, nil
	}
	return nil, agent.ErrAgentNotFound
}

func mustAgent(t *testing.T, entityID, memberID, worker string, lc agent.AgentLifecycle) *agent.Agent {
	t.Helper()
	a, err := agent.RehydrateAgent(agent.RehydrateAgentInput{
		ID:               agent.AgentID(entityID),
		OrganizationID:   "org-1",
		WorkerID:         worker,
		Lifecycle:        lc,
		IdentityMemberID: memberID,
		Version:          1,
	})
	if err != nil {
		t.Fatalf("rehydrate agent: %v", err)
	}
	return a
}

func openTask(t *testing.T, id string) *pm.Task {
	t.Helper()
	tk, err := pm.RehydrateTask(pm.RehydrateTaskInput{
		ID:        pm.TaskID(id),
		ProjectID: "proj-1",
		Title:     "t",
		Status:    pm.TaskOpen,
		Version:   1,
	})
	if err != nil {
		t.Fatalf("rehydrate task: %v", err)
	}
	return tk
}

// registers an agent under BOTH its identity-member ref and entity id so the
// builder's resolveSweepAgent finds it regardless of which the assignee ref carries.
func (f fakeAgentReads) put(a *agent.Agent) {
	if m := a.IdentityMemberID(); m != "" {
		f.byMember[m] = a
	}
	f.byID[string(a.ID())] = a
}

func newAgentReads() fakeAgentReads {
	return fakeAgentReads{byMember: map[string]*agent.Agent{}, byID: map[string]*agent.Agent{}}
}

// --- tests -----------------------------------------------------------------

// A desired-running agent with 0 running + a runnable open task IS a candidate, and
// the candidate carries the ENTITY id (not the identity-member ref the assignee uses).
func TestBuildSweepCandidates_DownQueued_Included(t *testing.T) {
	ref := pm.IdentityRef("agent:member-1") // agentActor ref = identity-member
	ag := mustAgent(t, "entity-1", "member-1", "W1", agent.LifecycleRunning)
	ars := newAgentReads()
	ars.put(ag)
	pmr := fakePMReads{
		loads:    map[pm.IdentityRef]pm.AgentTaskLoad{ref: {Running: 0, Pending: 1}},
		runnable: map[pm.IdentityRef][]*pm.Task{ref: {openTask(t, "T1")}},
	}

	got, err := buildSweepCandidates(pmr, ars)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 candidate, got %d", len(got))
	}
	c := got[0]
	if c.AgentID != "entity-1" || c.WorkerID != "W1" || c.TaskID != "T1" {
		t.Fatalf("candidate = %+v, want entity-1/W1/T1", c)
	}
}

// A busy agent (>=1 running task) is NOT a candidate — no false nudge of a working
// session.
func TestBuildSweepCandidates_LiveBusy_Skipped(t *testing.T) {
	ref := pm.IdentityRef("agent:member-1")
	ag := mustAgent(t, "entity-1", "member-1", "W1", agent.LifecycleRunning)
	ars := newAgentReads()
	ars.put(ag)
	pmr := fakePMReads{
		loads:    map[pm.IdentityRef]pm.AgentTaskLoad{ref: {Running: 1, Pending: 2}},
		runnable: map[pm.IdentityRef][]*pm.Task{ref: {openTask(t, "T1")}},
	}

	got, err := buildSweepCandidates(pmr, ars)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("busy agent must not be a candidate, got %+v", got)
	}
}

// A desired-stopped (intentionally down) agent is NOT resurrected by the sweep.
func TestBuildSweepCandidates_DesiredStopped_Skipped(t *testing.T) {
	ref := pm.IdentityRef("agent:member-1")
	ag := mustAgent(t, "entity-1", "member-1", "W1", agent.LifecycleStopped)
	ars := newAgentReads()
	ars.put(ag)
	pmr := fakePMReads{
		loads:    map[pm.IdentityRef]pm.AgentTaskLoad{ref: {Running: 0, Pending: 1}},
		runnable: map[pm.IdentityRef][]*pm.Task{ref: {openTask(t, "T1")}},
	}

	got, err := buildSweepCandidates(pmr, ars)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("desired-stopped agent must not be a candidate, got %+v", got)
	}
}

// Pending>0 but NO runnable open task (all pending tasks are dependency-blocked) → not
// a candidate: a relaunch would drain nothing.
func TestBuildSweepCandidates_NoRunnable_Skipped(t *testing.T) {
	ref := pm.IdentityRef("agent:member-1")
	ag := mustAgent(t, "entity-1", "member-1", "W1", agent.LifecycleRunning)
	ars := newAgentReads()
	ars.put(ag)
	pmr := fakePMReads{
		loads:    map[pm.IdentityRef]pm.AgentTaskLoad{ref: {Running: 0, Pending: 3}},
		runnable: map[pm.IdentityRef][]*pm.Task{ref: nil}, // deps unsatisfied → empty
	}

	got, err := buildSweepCandidates(pmr, ars)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("no-runnable agent must not be a candidate, got %+v", got)
	}
}

// A user: assignee (not an agent session) is ignored.
func TestBuildSweepCandidates_UserAssignee_Ignored(t *testing.T) {
	ref := pm.IdentityRef("user:alice")
	pmr := fakePMReads{
		loads:    map[pm.IdentityRef]pm.AgentTaskLoad{ref: {Running: 0, Pending: 1}},
		runnable: map[pm.IdentityRef][]*pm.Task{ref: {openTask(t, "T1")}},
	}
	got, err := buildSweepCandidates(pmr, newAgentReads())(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("user assignee must be ignored, got %+v", got)
	}
}

// An agent whose assignee ref has NO identity-member binding (agentActor fell back to
// the entity id) still resolves via FindByID.
func TestBuildSweepCandidates_EntityIDRef_Resolves(t *testing.T) {
	ref := pm.IdentityRef("agent:entity-1") // no member id → ref carries the entity id
	ag := mustAgent(t, "entity-1", "", "W1", agent.LifecycleRunning)
	ars := newAgentReads()
	ars.put(ag)
	pmr := fakePMReads{
		loads:    map[pm.IdentityRef]pm.AgentTaskLoad{ref: {Running: 0, Pending: 1}},
		runnable: map[pm.IdentityRef][]*pm.Task{ref: {openTask(t, "T9")}},
	}
	got, err := buildSweepCandidates(pmr, ars)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].AgentID != "entity-1" || got[0].TaskID != "T9" {
		t.Fatalf("want one candidate entity-1/T9, got %+v", got)
	}
}
