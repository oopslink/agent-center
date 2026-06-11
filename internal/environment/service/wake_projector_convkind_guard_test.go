package service

import (
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
)

// TestMentionWake_ProjectConvKindsWired is the #266-style class-guard for the
// projectConversationMessage kind-gate: every PROJECT-conversation kind that
// should support @mention-wake (Issue, Plan) must pass the gate so a participant
// agent is woken on @mention. The plan-conv bug (run-real caught: a participant
// in a plan conversation also-not-woken) was exactly a kind (Plan) silently
// dropped by the kind-gate UPSTREAM of the candidate logic. This guards the
// class: a new/removed project conv-kind dropped from mention-wake.
//
// Inverse-mutation: drop a kind from the projectConversationMessage kind-gate
// → that subtest FAILS with "NOT wired".
func TestMentionWake_ProjectConvKindsWired(t *testing.T) {
	for _, kind := range []conversation.ConversationKind{
		conversation.ConversationKindIssue,
		conversation.ConversationKindPlan,
	} {
		t.Run(string(kind), func(t *testing.T) {
			f := newWakeFixture(t)
			f.saveRunningAgent(t, "AG1", "W1")
			f.saveConv(t, "c1", kind, "Conv", agentPart("AG1"), userPart("bob"))
			p := f.projWith(map[string]string{"agent:AG1": "Helper"}, nil)
			if err := p.Project(f.ctx, convMessageEvent("EV1", "c1", "m1", "user:bob", "@helper please look")); err != nil {
				t.Fatalf("Project: %v", err)
			}
			cmds := f.commandsFor(t, "W1")
			if len(cmds) != 1 || cmds[0].CommandType() != "agent.converse" {
				t.Fatalf("project conv-kind %q: @mention-wake NOT wired (want 1 agent.converse, got %d) — dropped by projectConversationMessage kind-gate", kind, len(cmds))
			}
		})
	}
}
