package service

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/observability"
)

// T288 — OpenConversation get-or-create for DMs: one (org, participant set) maps to
// exactly ONE DM, reused on every open regardless of participant order; different
// peers or different orgs are distinct DMs.
func TestOpenConversation_DMDedup(t *testing.T) {
	w := setup(t)
	ctx := context.Background()
	pair := func(a, b string) []conversation.ParticipantElement {
		return []conversation.ParticipantElement{
			{IdentityID: conversation.IdentityRef(a), Role: "owner", JoinedAt: "t", JoinedBy: conversation.IdentityRef(a)},
			{IdentityID: conversation.IdentityRef(b), Role: "member", JoinedAt: "t", JoinedBy: conversation.IdentityRef(a)},
		}
	}
	open := func(org string, parts []conversation.ParticipantElement) OpenResult {
		t.Helper()
		res, err := w.OpenConversation(ctx, OpenCommand{
			Kind: conversation.ConversationKindDM, OrganizationID: org, Participants: parts,
			CreatedBy: conversation.IdentityRef("user:hayang"), Actor: observability.Actor("user:hayang"),
		})
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return res
	}

	r1 := open("org1", pair("user:hayang", "agent:pd"))
	if r1.Existing || r1.ConversationID == "" {
		t.Fatalf("first open should CREATE, got %+v", r1)
	}

	// Same pair again → reuse (no duplicate), Existing=true, no opened event.
	r2 := open("org1", pair("user:hayang", "agent:pd"))
	if !r2.Existing || r2.ConversationID != r1.ConversationID || r2.EventID != "" {
		t.Fatalf("second open should REUSE %s, got %+v", r1.ConversationID, r2)
	}

	// Reversed participant order → same DM (order-independent key).
	r3 := open("org1", pair("agent:pd", "user:hayang"))
	if r3.ConversationID != r1.ConversationID {
		t.Fatalf("reversed-order open should reuse %s, got %s", r1.ConversationID, r3.ConversationID)
	}

	// Different peer → a DISTINCT DM.
	r4 := open("org1", pair("user:hayang", "agent:tester1"))
	if r4.ConversationID == r1.ConversationID {
		t.Fatal("different peer must be a distinct DM")
	}

	// Same pair in a DIFFERENT org → distinct DM (dedup is org-scoped).
	r5 := open("org2", pair("user:hayang", "agent:pd"))
	if r5.ConversationID == r1.ConversationID {
		t.Fatal("same pair in another org must be a distinct DM")
	}
}
