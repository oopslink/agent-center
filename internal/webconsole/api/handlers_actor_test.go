package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
)

// v2.7 #146: the webconsole used a STATIC d.Actor (observability.Actor of the
// configured default_user) to stamp domain identity on every write — conversation
// owner, message sender, CreatedBy, etc. — regardless of who was logged in. The
// #142 download gate correctly checks live participation against the REAL session
// identity (filesCallerRef(caller)), so a creator who opened a DM through the
// handler was stamped as default_user and could not download their own attachment
// (F142-2 ship-blocker). The fix derives the per-request actor from the
// authenticated session inside hd(r). These tests pin that behavior end-to-end
// through the real handler chain (auth middleware wired => session identity in
// context), so they FAIL on the static-actor code and PASS once it is per-request.

// createDMViaHandler opens a DM through POST /api/conversations as sess and
// returns the new conversation id. The owner participant is stamped by the
// real handler path (the thing #146 fixes), not pre-seeded.
func createDMViaHandler(t *testing.T, baseURL string, sess testSession, members ...string) string {
	t.Helper()
	mb, _ := json.Marshal(members)
	body := fmt.Sprintf(`{"kind":"dm","members":%s}`, mb)
	resp := orgScopedPost(t, baseURL+"/api/conversations", body, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create dm: status=%d body=%s", resp.StatusCode, responseBytes(t, resp))
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return out["conversation_id"].(string)
}

// TestAPI_CreateDM_OwnerStampedWithSessionIdentity is the direct unit proof of
// the #146 fix: a DM opened through the handler must record the logged-in
// session identity as owner, NOT the static configured actor ("user:hayang"
// in the test deps). Before the fix the owner is "user:hayang"; after, it is
// "user:<session-id>".
func TestAPI_CreateDM_OwnerStampedWithSessionIdentity(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	convID := createDMViaHandler(t, s.URL, sess, "user:peer")

	conv, err := deps.ConvRepo.FindByID(context.Background(), conversation.ConversationID(convID))
	if err != nil || conv == nil {
		t.Fatalf("find conv: %v", err)
	}
	sessionRef := conversation.IdentityRef("user:" + sess.IdentityID)
	if !conv.HasActiveParticipant(sessionRef) {
		t.Fatalf("session identity %q is not an active participant; participants=%+v", sessionRef, conv.Participants())
	}
	// The static configured actor must NOT have been stamped as a participant.
	if conv.HasActiveParticipant(conversation.IdentityRef(string(deps.Actor))) {
		t.Fatalf("static configured actor %q was stamped as participant (regression to default_user stamping)", deps.Actor)
	}
}

// TestAPI_SendMessage_CreatorDownloadsOwnHandlerCreatedDMAttachment is F142-2:
// the creator opens a DM through the handler, uploads a blob, attaches it, and
// downloads their own attachment. Must be 200 + byte-exact. Fails on static
// d.Actor (owner stamped as default_user => download gate 403).
func TestAPI_SendMessage_CreatorDownloadsOwnHandlerCreatedDMAttachment(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()

	convID := createDMViaHandler(t, s.URL, sess, "user:peer")
	ulid := uploadBlob(t, s.URL, sess, []byte("mine"))
	fileURI := "ac://files/" + ulid

	status, body := sendAttachmentMessage(t, s.URL, convID, sess, fileURI)
	if status != http.StatusCreated {
		t.Fatalf("send attachment: status=%d body=%s", status, body)
	}
	resp := orgScopedGet(t, s.URL+"/api/files/"+ulid, sess)
	got := responseBytes(t, resp)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(got, []byte("mine")) {
		t.Fatalf("creator download own attachment: status=%d body=%s (want 200 \"mine\")", resp.StatusCode, got)
	}
}

// TestAPI_SendMessage_ClientSuppliedSenderIgnored locks the §-1 hardening:
// the webconsole send endpoint is human-session-only (no delegated-send), so a
// client-supplied sender_identity_id must be IGNORED and the message stamped
// with the session identity. Prevents sender spoofing.
func TestAPI_SendMessage_ClientSuppliedSenderIgnored(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	convID := createDMViaHandler(t, s.URL, sess, "user:peer")
	body := `{"content":"hi","sender_identity_id":"user:attacker"}`
	resp := orgScopedPost(t, s.URL+"/api/conversations/"+convID+"/messages", body, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("send: status=%d body=%s", resp.StatusCode, responseBytes(t, resp))
	}
	resp.Body.Close()

	msgs, err := deps.MsgRepo.FindByConversationID(context.Background(), conversation.ConversationID(convID), conversation.MessageFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("want 1 message, got %d", len(msgs))
	}
	want := conversation.IdentityRef("user:" + sess.IdentityID)
	if got := msgs[0].SenderIdentityID(); got != want {
		t.Fatalf("sender = %q, want session %q (client-supplied sender must be ignored)", got, want)
	}
}
