package api

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/files"
	filesservice "github.com/oopslink/agent-center/internal/files/service"
)

// =============================================================================
// T44 — an agent may place file attachments into channels/DMs it PARTICIPATES
// in (the agent-side dual of the human chat-box attachment). These tests prove:
//   - agentOwnDomainScopes now includes the agent's active-participant channel/
//     DM conversations, so upload_file/attach_file with scope=conversation +
//     download all succeed for a participant conversation;
//   - a NON-participant conversation (same org) and a cross-org conversation are
//     fail-closed (403 scope_not_in_agent_domain), §5.7 org-isolation;
//   - post_message carries attachments → the message stores them AND exactly one
//     live {ScopeConversation, convID} reference is placed (idempotent: no dup
//     when the file was already uploaded scope=conversation), so other
//     participants can download.
// =============================================================================

// seedChannel persists a channel (or DM) of the given org with the supplied
// active participant refs, so the agent's participation can be exercised through
// the real ConvRepo.Find path used by agentParticipantConvScopes.
func (f *writeToolsFixture) seedChannel(t *testing.T, id, name, orgID string, kind conversation.ConversationKind, participants ...string) conversation.ConversationID {
	t.Helper()
	conv, err := conversation.NewConversation(conversation.NewConversationInput{
		ID: conversation.ConversationID(id), Kind: kind, Name: name,
		OrganizationID: orgID, CreatedBy: "user:alice", OpenedAt: atNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	parts := make([]conversation.ParticipantElement, 0, len(participants))
	for _, p := range participants {
		parts = append(parts, conversation.ParticipantElement{
			IdentityID: conversation.IdentityRef(p), Role: "member", JoinedAt: "t",
		})
	}
	conv.SetParticipants(parts, atNow)
	if err := f.convRepo.Save(context.Background(), conv); err != nil {
		t.Fatal(err)
	}
	return conv.ID()
}

// liveConvRefs counts live {ScopeConversation, convID} references on a file.
func liveConvRefs(t *testing.T, svc interface {
	ListReferences(context.Context, files.FileURI) ([]files.FileReference, error)
}, ulid, convID string) int {
	t.Helper()
	uri, _ := files.NewFileURI(ulid)
	refs, err := svc.ListReferences(context.Background(), uri)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range refs {
		if r.Scope == files.ScopeConversation && r.ScopeID == convID && r.IsLive() {
			n++
		}
	}
	return n
}

// --- upload_file scope=conversation succeeds for a participant channel -------

func TestAgentFiles_UploadToParticipantChannel_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	convID := f.seedChannel(t, "ch-general", "general", atTestOrg, conversation.ConversationKindChannel, "agent:"+atAgent1, "user:alice")
	svc := f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	// Pre-T44 this returned 403 scope_not_in_agent_domain; the agent is now an
	// active participant so the conversation scope is in its own domain.
	content := []byte("agent report")
	ulid := uploadViaAgent(t, srv.URL, "acat_w1", atAgent1, "conversation", string(convID), content)

	if got := liveConvRefs(t, svc, ulid, string(convID)); got != 1 {
		t.Fatalf("live conversation refs = %d, want 1 (upload placed it)", got)
	}
	// And the agent can download it back (reachability via the participant conv).
	status, _, body := getRawBearer(t, srv.URL, "/admin/files/"+ulid+"?agent_id="+atAgent1, "acat_w1")
	if status != http.StatusOK {
		t.Fatalf("download status = %d, want 200; body = %s", status, body)
	}
	if !bytes.Equal(body, content) {
		t.Fatalf("download body = %q, want %q", body, content)
	}
}

// --- upload_file scope=conversation REJECTED for a non-participant channel ----

func TestAgentFiles_UploadToNonParticipantChannel_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	// Channel in the agent's org, but the agent is NOT a participant.
	convID := f.seedChannel(t, "ch-secret", "secret", atTestOrg, conversation.ConversationKindChannel, "user:alice", "agent:"+atAgent2)
	f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/upload_file", "acat_w1", map[string]any{
		"agent_id": atAgent1, "content_type": "text/plain", "size": 3,
		"scope": "conversation", "scope_id": string(convID),
	})
	if status != http.StatusForbidden || body["error"] != "scope_not_in_agent_domain" {
		t.Fatalf("status = %d err=%v, want 403 scope_not_in_agent_domain (non-participant fail-closed)", status, body["error"])
	}
}

// --- cross-org participation is still fail-closed (§5.7) ----------------------

func TestAgentFiles_UploadToCrossOrgChannel_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	// Even if the agent were listed as a participant of a channel in ANOTHER org,
	// agentParticipantConvScopes only enumerates a.OrganizationID(), so the scope
	// never appears — the cross-org channel is unreachable.
	convID := f.seedChannel(t, "ch-other-org", "general", "org-2", conversation.ConversationKindChannel, "agent:"+atAgent1)
	f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/upload_file", "acat_w1", map[string]any{
		"agent_id": atAgent1, "content_type": "text/plain", "size": 3,
		"scope": "conversation", "scope_id": string(convID),
	})
	if status != http.StatusForbidden || body["error"] != "scope_not_in_agent_domain" {
		t.Fatalf("status = %d err=%v, want 403 scope_not_in_agent_domain (cross-org fail-closed)", status, body["error"])
	}
}

// --- post_message with an attachment: stores it + single conv ref + download --

func TestAgentPostMessage_WithAttachment_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	convID := f.seedChannel(t, "ch-team", "team", atTestOrg, conversation.ConversationKindChannel, "agent:"+atAgent1, "user:alice")
	svc := f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	content := []byte("acceptance report pdf bytes")
	ulid := uploadViaAgent(t, srv.URL, "acat_w1", atAgent1, "conversation", string(convID), content)
	fileURI := "ac://files/" + ulid

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/post_message", "acat_w1", map[string]any{
		"agent_id": atAgent1, "conversation_id": string(convID), "content": "here is the report",
		"attachments": []map[string]any{{
			"uri": fileURI, "filename": "report.pdf", "mime_type": "application/pdf", "size": len(content),
		}},
	})
	if status != http.StatusOK {
		t.Fatalf("post_message status = %d, want 200; body = %v", status, body)
	}

	// The message persisted the attachment metadata (what the FE renders as a card).
	msgs, err := f.msgRepo.FindByConversationID(context.Background(), convID, conversation.MessageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var found *conversation.Message
	for _, m := range msgs {
		if string(m.ID()) == body["message_id"].(string) {
			found = m
		}
	}
	if found == nil {
		t.Fatalf("posted message %v not found", body["message_id"])
	}
	atts := found.Attachments()
	if len(atts) != 1 || atts[0].URI != fileURI || atts[0].Filename != "report.pdf" {
		t.Fatalf("message attachments = %+v, want one report.pdf", atts)
	}

	// Idempotent reference: upload(scope=conversation) + post_message must leave
	// EXACTLY ONE live conversation ref (no duplicate).
	if got := liveConvRefs(t, svc, ulid, string(convID)); got != 1 {
		t.Fatalf("live conversation refs = %d, want 1 (post_message must not duplicate)", got)
	}

	// The file is downloadable (a human reaches it via conversation membership;
	// here we prove the agent participant can pull it back).
	dstatus, _, dbody := getRawBearer(t, srv.URL, "/admin/files/"+ulid+"?agent_id="+atAgent1, "acat_w1")
	if dstatus != http.StatusOK || !bytes.Equal(dbody, content) {
		t.Fatalf("download status = %d body=%q, want 200 + original bytes", dstatus, dbody)
	}
}

// post_message attachment where the file was uploaded to the agent's PRIVATE
// scope (not yet the conversation) → post_message must place the conversation
// reference so other participants can download.
func TestAgentPostMessage_AttachmentFromPrivateScope_PlacesConvRef(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	convID := f.seedChannel(t, "ch-priv", "priv", atTestOrg, conversation.ConversationKindChannel, "agent:"+atAgent1)
	svc := f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	// Upload to the agent's own private scope (always in domain), NOT the conv.
	content := []byte("private then shared")
	ulid := uploadViaAgent(t, srv.URL, "acat_w1", atAgent1, "agent", atAgent1, content)
	if got := liveConvRefs(t, svc, ulid, string(convID)); got != 0 {
		t.Fatalf("pre-post conversation refs = %d, want 0", got)
	}
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/post_message", "acat_w1", map[string]any{
		"agent_id": atAgent1, "conversation_id": string(convID), "content": "sharing",
		"attachments": []map[string]any{{
			"uri": "ac://files/" + ulid, "filename": "n.txt", "mime_type": "text/plain", "size": len(content),
		}},
	})
	if status != http.StatusOK {
		t.Fatalf("post_message status = %d, want 200; body = %v", status, body)
	}
	if got := liveConvRefs(t, svc, ulid, string(convID)); got != 1 {
		t.Fatalf("post conversation refs = %d, want 1 (post_message placed it)", got)
	}
}

// post_message rejects an attachment the agent cannot reach in its own domain,
// BEFORE any message is written (atomic).
func TestAgentPostMessage_AttachmentNotReachable_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	convID := f.seedChannel(t, "ch-x", "x", atTestOrg, conversation.ConversationKindChannel, "agent:"+atAgent1)
	svc := f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	// A blob referenced ONLY in a project scope the agent cannot enumerate.
	content := []byte("foreign")
	ulid := uploadViaAgent(t, srv.URL, "acat_w1", atAgent1, "", "", content)
	uri, _ := files.NewFileURI(ulid)
	if _, err := svc.AddReference(context.Background(), filesservice.AddReferenceCmd{
		FileURI: uri, Scope: files.ScopeProject, ScopeID: "some-project", CreatedBy: "user:owner",
	}); err != nil {
		t.Fatal(err)
	}

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/post_message", "acat_w1", map[string]any{
		"agent_id": atAgent1, "conversation_id": string(convID), "content": "try",
		"attachments": []map[string]any{{
			"uri": "ac://files/" + ulid, "filename": "f", "mime_type": "text/plain", "size": len(content),
		}},
	})
	if status != http.StatusForbidden || body["error"] != "attachment_not_reachable" {
		t.Fatalf("status = %d err=%v, want 403 attachment_not_reachable", status, body["error"])
	}
	// Atomic: no message landed.
	msgs, _ := f.msgRepo.FindByConversationID(context.Background(), convID, conversation.MessageFilter{})
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after rejected attachment, got %d", len(msgs))
	}
}
