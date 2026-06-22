package service

// T255 (I19/OQ2): the converse brief header must show the owning object's LIVE
// title for issue/task chats (mirroring the plan name), resolved through the
// single OwnerContext table. OQ1: project chat has no converse wake path, so no
// project-title resolver is wired (resolveOwnerName returns false for it).

import (
	"context"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
)

// projWithTitles builds a projector wired with the #185 conversational deps plus
// the T255 issue/task title resolvers (backed by the given id→title maps).
func (f *wakeFixture) projWithTitles(displayName map[string]string, issueTitles, taskTitles map[string]string) *WakeProjector {
	return NewWakeProjector(WakeProjectorDeps{
		DB: f.db, Agents: f.agents,
		ControlLog: f.control, Applied: f.applied, Clock: f.clk,
		ConvRepo: f.convs, MsgRepo: f.msgs, ReadState: f.readState,
		DisplayName: func(_ context.Context, ref string) (string, bool) {
			n, ok := displayName[ref]
			return n, ok
		},
		IssueTitle: func(_ context.Context, id string) (string, bool) {
			n, ok := issueTitles[id]
			return n, ok
		},
		TaskTitle: func(_ context.Context, id string) (string, bool) {
			n, ok := taskTitles[id]
			return n, ok
		},
	})
}

// An ISSUE chat @mention enqueues a converse whose conv_name is the LIVE issue
// title — overriding the (possibly stale) stored conversation name. This is what
// lets the daemon brief render [Issue chat — "<title>" (issue_id=...)].
func TestWakeProjector_IssueChat_ConvNameIsLiveTitle_T255(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	// Stored conversation name is deliberately stale to prove the live override.
	f.saveConv(t, "issue-conv-1", conversation.ConversationKindIssue, "stale conv name",
		agentPart("AG1"), userPart("bob"))
	p := f.projWithTitles(
		map[string]string{"agent:AG1": "Helper"},
		map[string]string{"issue-1": "Login is broken"}, nil)

	ev := messageAddedEventOwner("EV1", "issue-conv-1", string(conversation.NewIssueOwnerRef("issue-1")),
		"m1", "user:bob", "@helper please look")
	if err := p.Project(f.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 1 || cmds[0].CommandType() != "agent.converse" {
		t.Fatalf("want 1 agent.converse, got %d", len(cmds))
	}
	pl := cmds[0].Payload()
	if !strings.Contains(pl, `"conv_name":"Login is broken"`) {
		t.Fatalf("converse payload must carry the live issue title, got: %s", pl)
	}
	if strings.Contains(pl, "stale conv name") {
		t.Fatalf("live title must override the stored conv name, got: %s", pl)
	}
	if !strings.Contains(pl, `"owner_ref":"pm://issues/issue-1"`) {
		t.Fatalf("converse payload must carry the issue owner_ref, got: %s", pl)
	}
}

// A TASK chat @mention (collaborator pulled into a task conversation) likewise
// carries the live task title in conv_name.
func TestWakeProjector_TaskChat_ConvNameIsLiveTitle_T255(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "task-conv-1", conversation.ConversationKindTask, "",
		agentPart("AG1"), userPart("bob"))
	p := f.projWithTitles(
		map[string]string{"agent:AG1": "Helper"}, nil,
		map[string]string{"task-1": "Refactor the brief"})

	ev := messageAddedEventOwner("EV1", "task-conv-1", string(conversation.NewTaskOwnerRef("task-1")),
		"m1", "user:bob", "@helper take a look")
	if err := p.Project(f.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 1 || cmds[0].CommandType() != "agent.converse" {
		t.Fatalf("want 1 agent.converse, got %d", len(cmds))
	}
	if pl := cmds[0].Payload(); !strings.Contains(pl, `"conv_name":"Refactor the brief"`) {
		t.Fatalf("converse payload must carry the live task title, got: %s", pl)
	}
}

// Title miss must NOT block the wake — the converse is still enqueued, with the
// stored conv name as the fallback (the daemon brief then falls back to the id).
func TestWakeProjector_IssueChat_TitleMiss_StillWakes_T255(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "issue-conv-2", conversation.ConversationKindIssue, "fallback name",
		agentPart("AG1"), userPart("bob"))
	// No entry for issue-9 → resolver miss.
	p := f.projWithTitles(map[string]string{"agent:AG1": "Helper"}, map[string]string{}, nil)

	ev := messageAddedEventOwner("EV1", "issue-conv-2", string(conversation.NewIssueOwnerRef("issue-9")),
		"m1", "user:bob", "@helper ping")
	if err := p.Project(f.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 1 {
		t.Fatalf("title miss must still wake, got %d converse cmds", len(cmds))
	}
	if pl := cmds[0].Payload(); !strings.Contains(pl, `"conv_name":"fallback name"`) {
		t.Fatalf("title miss must fall back to the stored conv name, got: %s", pl)
	}
}

// resolveOwnerName routes each owner kind to its resolver via the OwnerContext
// table, and returns false for kinds/refs with no live title (channel, dm,
// unknown) — and for PROJECT (OQ1: no wake path → no resolver wired).
func TestResolveOwnerName_DispatchAndOQ1_T255(t *testing.T) {
	p := NewWakeProjector(WakeProjectorDeps{
		PlanName:   func(_ context.Context, id string) (string, bool) { return "plan:" + id, true },
		IssueTitle: func(_ context.Context, id string) (string, bool) { return "issue:" + id, true },
		TaskTitle:  func(_ context.Context, id string) (string, bool) { return "task:" + id, true },
	})
	ctx := context.Background()
	cases := []struct {
		ownerRef string
		want     string
		ok       bool
	}{
		{string(conversation.NewPlanOwnerRef("p1")), "plan:p1", true},
		{string(conversation.NewIssueOwnerRef("i1")), "issue:i1", true},
		{string(conversation.NewTaskOwnerRef("t1")), "task:t1", true},
		// OQ1: project chat has no wake path → no resolver → false.
		{string(conversation.NewProjectOwnerRef("proj1")), "", false},
		// channel / dm / unknown → no live title.
		{string(conversation.NewOrgOwnerRef("org1")), "", false},
		{"", "", false},
		{"pm://bogus/x", "", false},
	}
	for _, c := range cases {
		got, ok := p.resolveOwnerName(ctx, c.ownerRef)
		if ok != c.ok || got != c.want {
			t.Fatalf("resolveOwnerName(%q) = (%q,%v), want (%q,%v)", c.ownerRef, got, ok, c.want, c.ok)
		}
	}
}

// A nil resolver for a recognised kind must degrade to a clean miss (no panic),
// so an unwired deployment never crashes the wake path.
func TestResolveOwnerName_NilResolver_NoPanic_T255(t *testing.T) {
	p := NewWakeProjector(WakeProjectorDeps{}) // no title resolvers wired
	if got, ok := p.resolveOwnerName(context.Background(), string(conversation.NewIssueOwnerRef("i1"))); ok || got != "" {
		t.Fatalf("nil resolver must miss cleanly, got (%q,%v)", got, ok)
	}
}
