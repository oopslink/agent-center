package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/observability"
)

// openSystemDM opens a DM whose active participants are exactly {system, agent:X}
// — the shape a reminder delivery creates (internal/cli/reminder_wiring.go). The
// human session user is NOT a participant; they merely observe it in the org.
func openSystemDM(t *testing.T, deps HandlerDeps, orgID, targetAgentRef string) conversation.ConversationID {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := deps.MessageWriter.OpenConversation(context.Background(), convservice.OpenCommand{
		Kind:           conversation.ConversationKindDM,
		Name:           "Reminder",
		OrganizationID: orgID,
		Participants: []conversation.ParticipantElement{
			{IdentityID: "system", Role: "owner", JoinedAt: now, JoinedBy: "system"},
			{IdentityID: conversation.IdentityRef(targetAgentRef), Role: "member", JoinedAt: now, JoinedBy: "system"},
		},
		CreatedBy: "system",
		Actor:     observability.Actor("system"),
	})
	if err != nil {
		t.Fatalf("open system DM: %v", err)
	}
	return res.ConversationID
}

// TestAPI_Attention_SystemDMNotSurfaced is the bug regression (oopslink 2026-07-02):
// a reminder delivered to agent:tester3 (a system↔agent DM) must NOT leak into an
// UNRELATED human's "Needs your attention" panel. The org-wide conversation scan
// sees the DM, but the human is not an active participant, so the directed gate
// must drop it. A real DM the human IS a party to still surfaces (positive control).
func TestAPI_Attention_SystemDMNotSurfaced(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)

	srv := newTestServer(t, deps)
	defer srv.Close()

	// (A) System→agent reminder DM the human only observes → must NOT surface.
	sysDM := openSystemDM(t, deps, sess.OrgID, "agent:tester3")
	addAgentMessage(t, deps, sysDM, "system", "[进度汇报] every 5 minutes report ...")

	// (B) A real DM the human IS a participant of, with an unread agent message →
	// MUST still surface (proves the gate didn't over-block real directed DMs).
	createResp := orgScopedPost(t, srv.URL+"/api/conversations", `{"kind":"dm","members":["agent:AG9"]}`, sess)
	var created map[string]any
	_ = json.NewDecoder(createResp.Body).Decode(&created)
	myDM := conversation.ConversationID(created["conversation_id"].(string))
	addAgentMessage(t, deps, myDM, "agent:AG9", "ping — need your input")

	items := attentionItems(t, orgScopedGet(t, srv.URL+"/api/attention", sess))

	var sawSystem, sawMine bool
	for _, it := range items {
		switch it["conversation_id"] {
		case string(sysDM):
			sawSystem = true
		case string(myDM):
			sawMine = true
		}
	}
	if sawSystem {
		t.Errorf("system→agent reminder DM %s leaked into attention; items=%v", sysDM, items)
	}
	if !sawMine {
		t.Errorf("the human's own unread DM %s should surface in attention; items=%v", myDM, items)
	}
}

// TestDM_SystemDMClassification: enrichDMProjection tags a {system, agent:X} DM as
// dm_type=system_dm and exposes X as the peer, so the UI groups it under
// "System DMs" and labels the row with the target (e.g. "@tester3").
func TestDM_SystemDMClassification(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)

	sysDM := openSystemDM(t, deps, sess.OrgID, "agent:tester3")

	srv := newTestServer(t, deps)
	defer srv.Close()
	listResp := orgScopedGet(t, srv.URL+"/api/conversations?kind=dm", sess)
	var rows []map[string]any
	if err := json.NewDecoder(listResp.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, row := range rows {
		if row["id"] != string(sysDM) {
			continue
		}
		found = true
		if row["dm_type"] != "system_dm" {
			t.Errorf("dm_type=%v want system_dm", row["dm_type"])
		}
		if row["peer_identity_id"] != "tester3" {
			t.Errorf("peer_identity_id=%v want tester3 (the delivery target)", row["peer_identity_id"])
		}
	}
	if !found {
		t.Fatalf("system DM row %s not found in %+v", sysDM, rows)
	}
}

// TestSingleNonSystemParticipant covers the pure classifier: exactly {system, X}
// yields X; anything else (no system, two non-system, or multi-party) yields !ok.
func TestSingleNonSystemParticipant(t *testing.T) {
	tests := []struct {
		name    string
		refs    []conversation.IdentityRef
		wantRef conversation.IdentityRef
		wantOK  bool
	}{
		{"system+agent", []conversation.IdentityRef{"system", "agent:tester3"}, "agent:tester3", true},
		{"agent+system order-independent", []conversation.IdentityRef{"agent:x", "system"}, "agent:x", true},
		{"two agents (a2a, not system)", []conversation.IdentityRef{"agent:a", "agent:b"}, "", false},
		{"human+agent (normal DM)", []conversation.IdentityRef{"user:h", "agent:a"}, "", false},
		{"single participant", []conversation.IdentityRef{"system"}, "", false},
		{"multi-party with system", []conversation.IdentityRef{"system", "agent:a", "agent:b"}, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := singleNonSystemParticipant(tc.refs)
			if ok != tc.wantOK || got != tc.wantRef {
				t.Errorf("singleNonSystemParticipant(%v)=(%q,%v) want (%q,%v)", tc.refs, got, ok, tc.wantRef, tc.wantOK)
			}
		})
	}
}
