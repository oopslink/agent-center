package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/outbox"
)

// T460: @mention must resolve by agent id/ref, not only by exact display_name —
// the silent-failure fix. The reported break: a handle written as an id/ref or a
// near-miss ("@agent-center-ba6bc42a" for the agent whose member id is
// "agent-ba6bc42a", display_name "agent-center-integration-dev") matched nobody
// and woke no one. These tests pin the wake-side resolution (text id-fragment,
// bare colon-ref, and the structural mention_refs bypass), plus the scope guard
// that mention_refs never widen the reachable set.

// messageAddedEventMentionRefs builds a message_added event carrying structural
// mention_refs (map[string]any so the []string survives JSON, unlike the
// string-only messageAddedEventOwner helper).
func messageAddedEventMentionRefs(id, convID, ownerRef, msgID, sender, text string, refs []string) outbox.Event {
	pl, err := json.Marshal(map[string]any{
		"conversation_id": convID,
		"owner_ref":       ownerRef,
		"message_id":      msgID,
		"sender":          sender,
		"text":            text,
		"mention_refs":    refs,
	})
	if err != nil {
		panic(err)
	}
	return outbox.Event{
		ID:        id,
		EventType: convservice.EvtConversationMessageAdded,
		Payload:   string(pl),
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
}

// (1) the exact reported case: a HUMAN writes a near-miss handle that contains the
// target's member-id fragment ("ba6bc42a") but does NOT match its display_name.
// Pre-T460 this woke no one; now the id-fragment resolves it.
func TestWakeProjector_T460_IDFragmentMention_WakesTarget(t *testing.T) {
	f := newWakeFixture(t)
	// entity id ENT-INT, member id "agent-ba6bc42a", display_name set via resolver.
	f.saveAgentMember(t, "ENT-INT", "WINT", "agent-ba6bc42a", true)
	f.saveConv(t, "chan-1", conversation.ConversationKindChannel, "general",
		agentPart("ENT-INT"), userPart("pd"))
	// The display_name is the REAL name, which the typo'd handle does NOT match —
	// so resolution must fall through to the id fragment.
	p := f.projWith(map[string]string{
		"agent:agent-ba6bc42a": "agent-center-integration-dev",
		"agent:ENT-INT":        "agent-center-integration-dev",
	}, nil)

	ev := convMessageEvent("EV1", "chan-1", "m1", "user:pd", "@agent-center-ba6bc42a please re-review")
	if err := p.Project(f.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if c := f.commandsFor(t, "WINT"); len(c) != 1 || c[0].CommandType() != "agent.converse" {
		t.Fatalf("id-fragment @mention should wake the integrator once, got %d (%v)", len(c), c)
	}
}

// (2) a bare "agent:<id>" colon-ref in the text resolves the target (T460 ②).
func TestWakeProjector_T460_BareColonRefMention_WakesTarget(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgentMember(t, "ENT-INT", "WINT", "agent-ba6bc42a", true)
	f.saveConv(t, "chan-1", conversation.ConversationKindChannel, "general",
		agentPart("ENT-INT"), userPart("pd"))
	p := f.projWith(map[string]string{"agent:agent-ba6bc42a": "agent-center-integration-dev"}, nil)

	ev := convMessageEvent("EV1", "chan-1", "m1", "user:pd", "ping agent:agent-ba6bc42a to re-review")
	if err := p.Project(f.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if c := f.commandsFor(t, "WINT"); len(c) != 1 {
		t.Fatalf("bare colon-ref should wake the integrator once, got %d", len(c))
	}
}

// (3) structural mention_refs wake a participant with NO matching @display_name in
// the text — the typo-proof machine path (T460 ①).
func TestWakeProjector_T460_MentionRefs_WakesParticipant(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgentMember(t, "ENT-INT", "WINT", "agent-ba6bc42a", true)
	f.saveConv(t, "chan-1", conversation.ConversationKindChannel, "general",
		agentPart("ENT-INT"), userPart("pd"))
	p := f.projWith(map[string]string{"agent:agent-ba6bc42a": "agent-center-integration-dev"}, nil)

	// No @mention in the text at all — only the structural ref.
	ev := messageAddedEventMentionRefs("EV1", "chan-1", "", "m1", "user:pd",
		"please take another look", []string{"agent:agent-ba6bc42a"})
	if err := p.Project(f.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if c := f.commandsFor(t, "WINT"); len(c) != 1 {
		t.Fatalf("mention_refs should wake the named participant once, got %d", len(c))
	}
}

// (4) scope guard: a mention_ref naming an agent that is NOT in the conversation's
// wake scope (not a participant / project member) wakes NO ONE — refs typo-proof
// the mention, they never widen who is reachable (承载性 handoff dispatch out of scope).
func TestWakeProjector_T460_MentionRefs_OutsideScope_NoWake(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgentMember(t, "ENT-INT", "WINT", "agent-ba6bc42a", true)
	f.saveRunningAgent(t, "OUTSIDER", "WOUT") // exists but is NOT a participant
	f.saveConv(t, "chan-1", conversation.ConversationKindChannel, "general",
		agentPart("ENT-INT"), userPart("pd"))
	p := f.projWith(map[string]string{"agent:agent-ba6bc42a": "agent-center-integration-dev"}, nil)

	ev := messageAddedEventMentionRefs("EV1", "chan-1", "", "m1", "user:pd",
		"hello", []string{"agent:OUTSIDER"})
	if err := p.Project(f.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if c := f.commandsFor(t, "WOUT"); len(c) != 0 {
		t.Fatalf("mention_ref outside the wake scope must wake no one, got %d", len(c))
	}
}

// (5) regression: a correct @display_name still wakes (no behavior change for the
// happy path the bug never touched).
func TestWakeProjector_T460_DisplayNameMention_StillWakes(t *testing.T) {
	f := newWakeFixture(t)
	f.saveAgentMember(t, "ENT-INT", "WINT", "agent-ba6bc42a", true)
	f.saveConv(t, "chan-1", conversation.ConversationKindChannel, "general",
		agentPart("ENT-INT"), userPart("pd"))
	p := f.projWith(map[string]string{"agent:agent-ba6bc42a": "integration-dev"}, nil)

	ev := convMessageEvent("EV1", "chan-1", "m1", "user:pd", "@integration-dev please re-review")
	if err := p.Project(f.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if c := f.commandsFor(t, "WINT"); len(c) != 1 {
		t.Fatalf("correct @display_name should still wake once, got %d", len(c))
	}
}
