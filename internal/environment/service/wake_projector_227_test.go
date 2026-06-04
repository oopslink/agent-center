package service

// v2.7.1 #227: when an @mention wakes a project-member agent that is NOT yet a
// conversation participant, it is auto-joined as a participant so the downstream
// emit + post gates (which require participancy) pass — the end-to-end seal for #224.

import (
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
)

func agentActiveCount(conv *conversation.Conversation, ref string) int {
	n := 0
	for _, p := range conv.Participants() {
		if p.IsActive() && string(p.IdentityID) == ref {
			n++
		}
	}
	return n
}

func TestWakeProjector_227_AutoJoinProjectMember_Idempotent(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	// Issue conversation WITHOUT AG1 as a participant — AG1 is a project member.
	f.saveConv(t, "iss-1", conversation.ConversationKindIssue, "", userPart("bob"))
	p := f.projWithMembers(
		map[string]string{"agent:AG1": "Helper"},
		map[string][]string{"pm://issues/iss-1": {"AG1"}},
	)

	ev := messageAddedEventOwner("EV1", "iss-1", "pm://issues/iss-1", "m1", "user:bob", "hey @helper")
	if err := p.Project(f.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 1 {
		t.Fatalf("want 1 converse, got %d", len(cmds))
	}
	// AG1 is now an active participant (auto-joined → post gate will pass).
	conv, err := f.convs.FindByID(f.ctx, conversation.ConversationID("iss-1"))
	if err != nil {
		t.Fatal(err)
	}
	if !conv.HasActiveParticipant(conversation.IdentityRef("agent:AG1")) {
		t.Fatalf("AG1 should be auto-joined: %+v", conv.Participants())
	}

	// Second @mention (new event) must NOT duplicate the participant.
	ev2 := messageAddedEventOwner("EV2", "iss-1", "pm://issues/iss-1", "m2", "user:bob", "again @helper")
	if err := p.Project(f.ctx, ev2); err != nil {
		t.Fatalf("Project EV2: %v", err)
	}
	conv2, _ := f.convs.FindByID(f.ctx, conversation.ConversationID("iss-1"))
	if got := agentActiveCount(conv2, "agent:AG1"); got != 1 {
		t.Fatalf("AG1 must appear exactly once after 2 @mentions (idempotent), got %d", got)
	}
}

func TestWakeProjector_227_NonMember_NoAutoJoin(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "iss-1", conversation.ConversationKindIssue, "", userPart("bob"))
	// AG1 is neither a participant nor a project member (org-only).
	p := f.projWithMembers(map[string]string{"agent:AG1": "Helper"}, map[string][]string{})

	ev := messageAddedEventOwner("EV1", "iss-1", "pm://issues/iss-1", "m1", "user:bob", "hey @helper")
	if err := p.Project(f.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	conv, _ := f.convs.FindByID(f.ctx, conversation.ConversationID("iss-1"))
	if conv.HasActiveParticipant(conversation.IdentityRef("agent:AG1")) {
		t.Fatalf("non-member must NOT be auto-joined: %+v", conv.Participants())
	}
}

func TestWakeProjector_227_ExistingParticipant_NoDoubleJoin(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	// AG1 is ALREADY a participant AND a project member.
	f.saveConv(t, "task-1", conversation.ConversationKindTask, "", agentPart("AG1"), userPart("bob"))
	owner := string(conversation.NewTaskOwnerRef("task-1"))
	p := f.projWithMembers(
		map[string]string{"agent:AG1": "Helper"},
		map[string][]string{owner: {"AG1"}},
	)

	ev := messageAddedEventOwner("EV1", "task-1", owner, "m1", "user:bob", "hey @helper")
	if err := p.Project(f.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	conv, _ := f.convs.FindByID(f.ctx, conversation.ConversationID("task-1"))
	if got := agentActiveCount(conv, "agent:AG1"); got != 1 {
		t.Fatalf("already-participant + member must stay single, got %d", got)
	}
}
