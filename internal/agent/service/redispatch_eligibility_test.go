package service

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

// elgFixture wires the eligibility adapter over real in-memory agent repos.
type elgFixture struct {
	adapter   *RedispatchEligibility
	agents    agent.Repository
	workItems agent.WorkItemRepository
	ctx       context.Context
}

func newElgFixture(t *testing.T) *elgFixture {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	agents := agentsql.NewAgentRepo(db)
	workItems := agentsql.NewWorkItemRepo(db)
	return &elgFixture{
		adapter:   NewRedispatchEligibility(agents, workItems),
		agents:    agents,
		workItems: workItems,
		ctx:       context.Background(),
	}
}

var elgT0 = time.Unix(1_700_000_000, 0).UTC()

// seedAgent saves an agent with the given lifecycle (running iff run==true) and an
// optional identity-member id.
func (f *elgFixture) seedAgent(t *testing.T, id, memberID string, run bool) {
	t.Helper()
	a, err := agent.NewAgent(agent.NewAgentInput{
		ID: agent.AgentID(id), OrganizationID: "org-1",
		Profile: agent.Profile{Name: "bot"}, WorkerID: "w-1",
		CreatedBy: "user:a", IdentityMemberID: memberID, CreatedAt: elgT0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run {
		if err := a.Start(elgT0); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.agents.Save(f.ctx, a); err != nil {
		t.Fatal(err)
	}
}

// seedWorkItem saves a work item for taskRef owned by agentID in the given status.
func (f *elgFixture) seedWorkItem(t *testing.T, id, agentID, taskRef string, status agent.WorkItemStatus) {
	t.Helper()
	wi, err := agent.RehydrateWorkItem(agent.RehydrateWorkItemInput{
		ID: id, AgentID: agent.AgentID(agentID), TaskRef: taskRef, Status: status,
		CreatedAt: elgT0, UpdatedAt: elgT0, Version: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.workItems.Save(f.ctx, wi); err != nil {
		t.Fatal(err)
	}
}

func (f *elgFixture) eligible(t *testing.T, agentRef, taskRef string) bool {
	t.Helper()
	ok, err := f.adapter.Eligible(f.ctx, agentRef, taskRef)
	if err != nil {
		t.Fatalf("Eligible(%q,%q): unexpected error: %v", agentRef, taskRef, err)
	}
	return ok
}

// TestEligible_RunningAgentNoLiveWorkItem: a running agent whose task has no live
// WorkItem (none, or only a terminal/failed one from the stale release) is eligible.
func TestEligible_RunningAgentNoLiveWorkItem(t *testing.T) {
	f := newElgFixture(t)
	f.seedAgent(t, "ag-1", "", true)

	// No work item at all → eligible.
	if !f.eligible(t, "agent:ag-1", "pm://tasks/T1") {
		t.Fatal("expected eligible with no work item")
	}

	// Only a terminal (failed — the stale-release outcome) WI → still eligible.
	f.seedWorkItem(t, "wi-failed", "ag-1", "pm://tasks/T1", agent.WorkItemFailed)
	if !f.eligible(t, "agent:ag-1", "pm://tasks/T1") {
		t.Fatal("expected eligible when only a terminal work item exists")
	}
}

// TestEligible_LiveWorkItemBlocks: a queued or active WorkItem already in flight
// for the task makes it ineligible (no re-mint/re-wake spam every tick).
func TestEligible_LiveWorkItemBlocks(t *testing.T) {
	for _, st := range []agent.WorkItemStatus{agent.WorkItemQueued, agent.WorkItemActive, agent.WorkItemWaitingInput} {
		f := newElgFixture(t)
		f.seedAgent(t, "ag-1", "", true)
		f.seedWorkItem(t, "wi-live", "ag-1", "pm://tasks/T1", st)
		if f.eligible(t, "agent:ag-1", "pm://tasks/T1") {
			t.Fatalf("expected NOT eligible with a live (%s) work item in flight", st)
		}
	}
}

// TestEligible_AgentNotRunning: a stopped/failed agent is not redispatched to
// (we wait for it to come back).
func TestEligible_AgentNotRunning(t *testing.T) {
	f := newElgFixture(t)
	f.seedAgent(t, "ag-1", "", false) // stopped
	if f.eligible(t, "agent:ag-1", "pm://tasks/T1") {
		t.Fatal("expected NOT eligible for a stopped agent")
	}
}

// TestEligible_NonAgentAndUnresolved: human assignees and unresolved agent refs are
// ineligible WITHOUT error (one bad ref never stalls the sweep).
func TestEligible_NonAgentAndUnresolved(t *testing.T) {
	f := newElgFixture(t)
	if f.eligible(t, "user:alice", "pm://tasks/T1") {
		t.Fatal("human assignee must be ineligible")
	}
	if f.eligible(t, "", "pm://tasks/T1") {
		t.Fatal("empty assignee must be ineligible")
	}
	// Unresolved agent → (false, nil), not an error.
	ok, err := f.adapter.Eligible(f.ctx, "agent:ghost", "pm://tasks/T1")
	if err != nil {
		t.Fatalf("unresolved agent must not error: %v", err)
	}
	if ok {
		t.Fatal("unresolved agent must be ineligible")
	}
}

// TestEligible_ResolvesByIdentityMemberID: the assign path carries the identity-
// member id ("agent-<ulid>"); the adapter must resolve it via the member→entity
// bridge, not only the entity id.
func TestEligible_ResolvesByIdentityMemberID(t *testing.T) {
	f := newElgFixture(t)
	f.seedAgent(t, "ag-entity-1", "agent-member-1", true)
	if !f.eligible(t, "agent:agent-member-1", "pm://tasks/T1") {
		t.Fatal("expected the identity-member id to resolve to the running entity")
	}
}
