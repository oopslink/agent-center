package service

// v2.7.1 #224: an agent that is a MEMBER of an issue/task's owning project is a
// valid @mention wake target even when it is NOT an explicit conversation
// participant. Non-members (org-only) are not woken; an agent that is both a
// participant and a project member is woken exactly once (dedup).

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
)

// projWithMembers builds a projector with the #224 ProjectAgentMembers dep backed
// by a fixed owner_ref → agent-rawID map.
func (f *wakeFixture) projWithMembers(displayName map[string]string, byOwner map[string][]string) *WakeProjector {
	return NewWakeProjector(WakeProjectorDeps{
		DB: f.db, Agents: f.agents,
		ControlLog: f.control, Applied: f.applied, Clock: f.clk,
		ConvRepo: f.convs, MsgRepo: f.msgs, ReadState: f.readState,
		DisplayName: func(_ context.Context, ref string) (string, bool) {
			n, ok := displayName[ref]
			return n, ok
		},
		ProjectAgentMembers: func(_ context.Context, ownerRef string) ([]string, error) {
			return byOwner[ownerRef], nil
		},
	})
}

func TestWakeProjector_IssueProjectMember_NonParticipant_Mention_Wakes(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	// Issue conversation WITHOUT AG1 as a participant — only bob.
	f.saveConv(t, "iss-1", conversation.ConversationKindIssue, "", userPart("bob"))
	// AG1 IS a member of the issue's owning project.
	p := f.projWithMembers(
		map[string]string{"agent:AG1": "Helper"},
		map[string][]string{"pm://issues/iss-1": {"AG1"}},
	)

	ev := messageAddedEventOwner("EV1", "iss-1", "pm://issues/iss-1", "m1", "user:bob", "hey @helper look")
	if err := p.Project(f.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 1 || cmds[0].CommandType() != "agent.converse" {
		t.Fatalf("project-member @mention should wake, got %d", len(cmds))
	}
}

// v2.9 ② (@oopslink symmetric-broaden): a PLAN conversation's @mention candidates
// broaden to the plan's project agent-members too — so a human can @ a project-member
// agent that is NOT a plan-conversation participant and wake it, exactly like an
// issue/task conversation. (The resolver maps pm://plans/{id} → plan's project.)
func TestWakeProjector_PlanProjectMember_NonParticipant_Mention_Wakes(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	// Plan conversation WITHOUT AG1 as a participant — only bob.
	f.saveConv(t, "plan-1", conversation.ConversationKindPlan, "", userPart("bob"))
	// AG1 IS a member of the plan's owning project.
	p := f.projWithMembers(
		map[string]string{"agent:AG1": "Helper"},
		map[string][]string{"pm://plans/plan-1": {"AG1"}},
	)

	ev := messageAddedEventOwner("EV1", "plan-1", "pm://plans/plan-1", "m1", "user:bob", "hey @helper look at this plan")
	if err := p.Project(f.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 1 || cmds[0].CommandType() != "agent.converse" {
		t.Fatalf("plan project-member @mention should wake (② symmetric-broaden), got %d", len(cmds))
	}
}

func TestWakeProjector_IssueNonMember_Mention_NoWake(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "iss-1", conversation.ConversationKindIssue, "", userPart("bob"))
	// AG1 is NOT a participant and NOT a project member (only an org member, say).
	p := f.projWithMembers(map[string]string{"agent:AG1": "Helper"}, map[string][]string{})

	ev := messageAddedEventOwner("EV1", "iss-1", "pm://issues/iss-1", "m1", "user:bob", "hey @helper look")
	if err := p.Project(f.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("non-member @mention must NOT wake, got %d", len(cmds))
	}
}

func TestWakeProjector_TaskProjectMember_NonParticipant_Mention_Wakes(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "task-1", conversation.ConversationKindTask, "", userPart("bob"))
	owner := string(conversation.NewTaskOwnerRef("task-1"))
	p := f.projWithMembers(
		map[string]string{"agent:AG1": "Helper"},
		map[string][]string{owner: {"AG1"}},
	)

	ev := messageAddedEventOwner("EV1", "task-1", owner, "m1", "user:bob", "hey @helper please")
	if err := p.Project(f.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 1 || cmds[0].CommandType() != "agent.converse" {
		t.Fatalf("task project-member @mention should wake, got %d", len(cmds))
	}
}

func TestWakeProjector_ParticipantAndMember_Mention_WakesOnce(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	// AG1 is BOTH an explicit participant AND a project member.
	f.saveConv(t, "iss-1", conversation.ConversationKindIssue, "", agentPart("AG1"), userPart("bob"))
	p := f.projWithMembers(
		map[string]string{"agent:AG1": "Helper"},
		map[string][]string{"pm://issues/iss-1": {"AG1"}},
	)

	ev := messageAddedEventOwner("EV1", "iss-1", "pm://issues/iss-1", "m1", "user:bob", "hey @helper")
	if err := p.Project(f.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 1 {
		t.Fatalf("participant+member must wake exactly once (dedup), got %d", len(cmds))
	}
}
