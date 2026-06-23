package service

// v2.7.1 #220: issue/task conversations are conversations too — an @mention of an
// agent participant wakes it (agent.converse), reusing the channel @mention policy.
// For task, this is the wake path (v2.14.0 F7 retired the old WorkItem-keyed wake),
// under the same applied-mark.

import (
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
)

func TestWakeProjector_Issue_Mention_EnqueuesConverse(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "iss-1", conversation.ConversationKindIssue, "", agentPart("AG1"), userPart("bob"))
	p := f.projWith(map[string]string{"agent:AG1": "Helper"}, nil)

	if err := p.Project(f.ctx, convMessageEvent("EV1", "iss-1", "m1", "user:bob", "hey @helper look at this issue")); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 1 || cmds[0].CommandType() != "agent.converse" {
		t.Fatalf("issue @mention want 1 agent.converse, got %d", len(cmds))
	}
}

func TestWakeProjector_Issue_NoMention_NoWake(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "iss-1", conversation.ConversationKindIssue, "", agentPart("AG1"), userPart("bob"))
	p := f.projWith(map[string]string{"agent:AG1": "Helper"}, nil)

	if err := p.Project(f.ctx, convMessageEvent("EV1", "iss-1", "m1", "user:bob", "just discussing, nobody pinged")); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("un-mentioned issue message must not wake, got %d", len(cmds))
	}
}

func TestWakeProjector_Task_Mention_EnqueuesConverse(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "task-1", conversation.ConversationKindTask, "", agentPart("AG1"), userPart("bob"))
	p := f.projWith(map[string]string{"agent:AG1": "Helper"}, nil)

	// Task event carries the pm://tasks/ owner_ref → the task branch (no
	// waiting_input WorkItems here, so the WorkItem wake is a no-op) + the new
	// conversational @mention wake.
	ev := messageAddedEventOwner("EV1", "task-1", string(conversation.NewTaskOwnerRef("task-1")), "m1", "user:bob", "hey @helper please look")
	if err := p.Project(f.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 1 || cmds[0].CommandType() != "agent.converse" {
		t.Fatalf("task @mention want 1 agent.converse, got %d", len(cmds))
	}
}

func TestWakeProjector_Task_NoMention_NoConverse(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "task-1", conversation.ConversationKindTask, "", agentPart("AG1"), userPart("bob"))
	p := f.projWith(map[string]string{"agent:AG1": "Helper"}, nil)

	ev := messageAddedEventOwner("EV1", "task-1", string(conversation.NewTaskOwnerRef("task-1")), "m1", "user:bob", "no ping here")
	if err := p.Project(f.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if cmds := f.commandsFor(t, "W1"); len(cmds) != 0 {
		t.Fatalf("un-mentioned task message must not conversational-wake, got %d", len(cmds))
	}
}
