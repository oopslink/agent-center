package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/observability"
)

// v2.7 #158: inviting a participant with a malformed identity (no user:/agent:
// prefix) must return 400 (client validation error), not 500 — the malformed ref
// was reaching the Invite service whose validation error bubbled up as an opaque
// internal error. Same input-validation hygiene class as #148.
func TestAPI_InviteParticipant_MalformedIdentity_400Not500(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	sref := conversation.IdentityRef("user:" + sess.IdentityID)
	cres, err := deps.ChannelMgmtSvc.CreateChannel(context.Background(), convservice.CreateChannelCommand{
		Name: "invite-validate", OrganizationID: sess.OrgID, CreatedBy: sref, Actor: observability.Actor(sref),
	})
	if err != nil {
		t.Fatal(err)
	}
	cid := string(cres.ConversationID)
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedPost(t, s.URL+"/api/conversations/"+cid+"/participants",
		`{"identity_id":"not-a-valid-ref","role":"member"}`, sess)
	body := responseBytes(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed invite: status=%d body=%s (want 400)", resp.StatusCode, body)
	}
}
