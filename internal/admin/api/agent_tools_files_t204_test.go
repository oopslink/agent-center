package api

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
)

// =============================================================================
// T204 — an agent may place file attachments into ISSUE and TASK conversations
// it PARTICIPATES in, finishing the "all five conversation kinds have consistent
// attachment send/receive" alignment (channel/DM/plan were already covered;
// T167 added plan).
//
// Before the fix agentParticipantConvScopes enumerated only channel/DM/plan, so
// an ISSUE conversation never entered the agent's own-domain unless the agent
// happened to hold a work-item for a task derived from that issue — and even then
// only the issue *scope* (not the issue *conversation* scope) was derived. So the
// PD discussing on an issue (a participant, no work-item) hit 403
// scope_not_in_agent_domain on upload_file(scope=conversation) — exactly the
// owner-reported mockup-attach failure. Participant enumeration now grants the
// {ScopeConversation, convID} at the SAME participant boundary post_message
// enforces, and the fail-closed guarantees (non-participant / cross-org) still hold.
// =============================================================================

// --- ISSUE: upload_file scope=conversation succeeds for a participant ---------

func TestAgentFiles_UploadToParticipantIssue_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	// An issue conversation (ConversationKindIssue, owner pm://issues/{id}) the
	// agent participates in as an issue subscriber (e.g. the issue creator / a
	// commenter) — independent of holding any work-item.
	convID := f.seedChannel(t, "issue-conv-1", "I4", atTestOrg, conversation.ConversationKindIssue, "agent:"+atAgent1, "user:alice")
	svc := f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	content := []byte("issue mockup screenshot bytes")
	ulid := uploadViaAgent(t, srv.URL, "acat_w1", atAgent1, "conversation", string(convID), content)

	if got := liveConvRefs(t, svc, ulid, string(convID)); got != 1 {
		t.Fatalf("live conversation refs = %d, want 1 (upload placed it)", got)
	}
	// Receive side: the agent can download it back (reachability via the participant issue conv).
	status, _, body := getRawBearer(t, srv.URL, "/admin/files/"+ulid+"?agent_id="+atAgent1, "acat_w1")
	if status != http.StatusOK {
		t.Fatalf("download status = %d, want 200; body = %s", status, body)
	}
	if !bytes.Equal(body, content) {
		t.Fatalf("download body = %q, want %q", body, content)
	}
}

// --- ISSUE: upload_file scope=conversation REJECTED for a non-participant ------

func TestAgentFiles_UploadToNonParticipantIssue_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	// An issue conversation in the agent's org, but the agent is NOT a participant.
	convID := f.seedChannel(t, "issue-conv-secret", "secret-issue", atTestOrg, conversation.ConversationKindIssue, "user:alice", "agent:"+atAgent2)
	f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/upload_file", "acat_w1", map[string]any{
		"agent_id": atAgent1, "content_type": "text/plain", "size": 3,
		"scope": "conversation", "scope_id": string(convID),
	})
	if status != http.StatusForbidden || body["error"] != "scope_not_in_agent_domain" {
		t.Fatalf("status = %d err=%v, want 403 scope_not_in_agent_domain (non-participant issue fail-closed)", status, body["error"])
	}
}

// --- ISSUE: cross-org participation is still fail-closed (§5.7) ----------------

func TestAgentFiles_UploadToCrossOrgIssue_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	// Even listed as a participant of an issue conversation in ANOTHER org, the agent
	// only enumerates a.OrganizationID(), so the scope never appears.
	convID := f.seedChannel(t, "issue-conv-other-org", "other", "org-2", conversation.ConversationKindIssue, "agent:"+atAgent1)
	f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/upload_file", "acat_w1", map[string]any{
		"agent_id": atAgent1, "content_type": "text/plain", "size": 3,
		"scope": "conversation", "scope_id": string(convID),
	})
	if status != http.StatusForbidden || body["error"] != "scope_not_in_agent_domain" {
		t.Fatalf("status = %d err=%v, want 403 scope_not_in_agent_domain (cross-org issue fail-closed)", status, body["error"])
	}
}

// --- TASK: upload_file scope=conversation succeeds for a participant w/o work-item

func TestAgentFiles_UploadToParticipantTaskNoWorkItem_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	// A task conversation the agent participates in (e.g. a task subscriber) but
	// holds NO work-item for — previously reachable only via the work-item
	// derivation, so a non-assignee participant was excluded.
	convID := f.seedChannel(t, "task-conv-1", "task-chat", atTestOrg, conversation.ConversationKindTask, "agent:"+atAgent1, "user:alice")
	svc := f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	content := []byte("task attachment bytes")
	ulid := uploadViaAgent(t, srv.URL, "acat_w1", atAgent1, "conversation", string(convID), content)

	if got := liveConvRefs(t, svc, ulid, string(convID)); got != 1 {
		t.Fatalf("live conversation refs = %d, want 1 (upload placed it)", got)
	}
	status, _, body := getRawBearer(t, srv.URL, "/admin/files/"+ulid+"?agent_id="+atAgent1, "acat_w1")
	if status != http.StatusOK || !bytes.Equal(body, content) {
		t.Fatalf("download status = %d body=%q, want 200 + original bytes", status, body)
	}
}

// --- TASK: non-participant still fail-closed -----------------------------------

func TestAgentFiles_UploadToNonParticipantTask_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	convID := f.seedChannel(t, "task-conv-secret", "secret-task", atTestOrg, conversation.ConversationKindTask, "user:alice", "agent:"+atAgent2)
	f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/upload_file", "acat_w1", map[string]any{
		"agent_id": atAgent1, "content_type": "text/plain", "size": 3,
		"scope": "conversation", "scope_id": string(convID),
	})
	if status != http.StatusForbidden || body["error"] != "scope_not_in_agent_domain" {
		t.Fatalf("status = %d err=%v, want 403 scope_not_in_agent_domain (non-participant task fail-closed)", status, body["error"])
	}
}

// --- ISSUE: end-to-end post_message with an attachment stores it + one ref -----

func TestAgentPostMessage_WithAttachment_Issue_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	convID := f.seedChannel(t, "issue-conv-team", "issue-team", atTestOrg, conversation.ConversationKindIssue, "agent:"+atAgent1, "user:alice")
	svc := f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	content := []byte("issue design mock png bytes")
	ulid := uploadViaAgent(t, srv.URL, "acat_w1", atAgent1, "conversation", string(convID), content)
	fileURI := "ac://files/" + ulid

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/post_message", "acat_w1", map[string]any{
		"agent_id": atAgent1, "target": map[string]any{"type": "conversation", "id": string(convID)}, "content": "here is the mock",
		"attachments": []map[string]any{{
			"uri": fileURI, "filename": "mock.png", "mime_type": "image/png", "size": len(content),
		}},
	})
	if status != http.StatusOK {
		t.Fatalf("post_message status = %d, want 200; body = %v", status, body)
	}

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
	if len(atts) != 1 || atts[0].URI != fileURI || atts[0].Filename != "mock.png" {
		t.Fatalf("message attachments = %+v, want one mock.png", atts)
	}

	// upload(scope=conversation) + post_message must leave EXACTLY ONE live ref.
	if got := liveConvRefs(t, svc, ulid, string(convID)); got != 1 {
		t.Fatalf("live conversation refs = %d, want 1 (post_message must not duplicate)", got)
	}

	dstatus, _, dbody := getRawBearer(t, srv.URL, "/admin/files/"+ulid+"?agent_id="+atAgent1, "acat_w1")
	if dstatus != http.StatusOK || !bytes.Equal(dbody, content) {
		t.Fatalf("download status = %d body=%q, want 200 + original bytes", dstatus, dbody)
	}
}
