package api

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
)

// =============================================================================
// T167 — an agent may place file attachments into PLAN conversations it
// PARTICIPATES in, closing the last gap that left Plan chat unable to send/
// receive attachments while channel/DM/task/issue already worked.
//
// Before the fix agentParticipantConvScopes enumerated only channel + DM, so a
// plan conversation never entered the agent's own-domain: upload_file(scope=
// conversation) returned 403 scope_not_in_agent_domain (the PD's reported
// "Plan chat can't attach"), and post_message carrying such a file returned 403
// attachment_not_reachable. Plan participation is independent of work-items (the
// PD opening its own plan chat holds no work-item for a task in that plan), so the
// scope MUST come from participant enumeration. These tests prove the participant
// plan path now matches the channel/DM contract, and that the fail-closed
// guarantees (non-participant / cross-org) still hold for the plan kind.
// =============================================================================

// --- upload_file scope=conversation succeeds for a participant PLAN conv ------

func TestAgentFiles_UploadToParticipantPlan_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	// A plan conversation (ConversationKindPlan, owner pm://plans/{id}) the agent
	// participates in — e.g. as the plan creator or a dispatched assignee.
	convID := f.seedChannel(t, "plan-conv-1", "v2.10.3", atTestOrg, conversation.ConversationKindPlan, "agent:"+atAgent1, "user:alice")
	svc := f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	content := []byte("plan chat screenshot bytes")
	ulid := uploadViaAgent(t, srv.URL, "acat_w1", atAgent1, "conversation", string(convID), content)

	if got := liveConvRefs(t, svc, ulid, string(convID)); got != 1 {
		t.Fatalf("live conversation refs = %d, want 1 (upload placed it)", got)
	}
	// And the agent can download it back (reachability via the participant plan conv).
	status, _, body := getRawBearer(t, srv.URL, "/admin/files/"+ulid+"?agent_id="+atAgent1, "acat_w1")
	if status != http.StatusOK {
		t.Fatalf("download status = %d, want 200; body = %s", status, body)
	}
	if !bytes.Equal(body, content) {
		t.Fatalf("download body = %q, want %q", body, content)
	}
}

// --- upload_file scope=conversation REJECTED for a non-participant PLAN conv ---

func TestAgentFiles_UploadToNonParticipantPlan_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	// A plan conversation in the agent's org, but the agent is NOT a participant.
	convID := f.seedChannel(t, "plan-conv-secret", "secret-plan", atTestOrg, conversation.ConversationKindPlan, "user:alice", "agent:"+atAgent2)
	f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/upload_file", "acat_w1", map[string]any{
		"agent_id": atAgent1, "content_type": "text/plain", "size": 3,
		"scope": "conversation", "scope_id": string(convID),
	})
	if status != http.StatusForbidden || body["error"] != "scope_not_in_agent_domain" {
		t.Fatalf("status = %d err=%v, want 403 scope_not_in_agent_domain (non-participant plan fail-closed)", status, body["error"])
	}
}

// --- cross-org plan participation is still fail-closed (§5.7) -----------------

func TestAgentFiles_UploadToCrossOrgPlan_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	// Even listed as a participant of a plan conversation in ANOTHER org, the agent
	// only enumerates a.OrganizationID(), so the scope never appears.
	convID := f.seedChannel(t, "plan-conv-other-org", "other", "org-2", conversation.ConversationKindPlan, "agent:"+atAgent1)
	f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/upload_file", "acat_w1", map[string]any{
		"agent_id": atAgent1, "content_type": "text/plain", "size": 3,
		"scope": "conversation", "scope_id": string(convID),
	})
	if status != http.StatusForbidden || body["error"] != "scope_not_in_agent_domain" {
		t.Fatalf("status = %d err=%v, want 403 scope_not_in_agent_domain (cross-org plan fail-closed)", status, body["error"])
	}
}

// --- post_message with an attachment into a PLAN conv: stores it + one ref ----

func TestAgentPostMessage_WithAttachment_Plan_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	convID := f.seedChannel(t, "plan-conv-team", "plan-team", atTestOrg, conversation.ConversationKindPlan, "agent:"+atAgent1, "user:alice")
	svc := f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	content := []byte("plan design mock png bytes")
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
	if len(atts) != 1 || atts[0].URI != fileURI || atts[0].Filename != "mock.png" {
		t.Fatalf("message attachments = %+v, want one mock.png", atts)
	}

	// Idempotent reference: upload(scope=conversation) + post_message must leave
	// EXACTLY ONE live conversation ref (no duplicate).
	if got := liveConvRefs(t, svc, ulid, string(convID)); got != 1 {
		t.Fatalf("live conversation refs = %d, want 1 (post_message must not duplicate)", got)
	}

	// The file is downloadable by the participant agent (other participants — human
	// or agent — reach it the same way via conversation membership).
	dstatus, _, dbody := getRawBearer(t, srv.URL, "/admin/files/"+ulid+"?agent_id="+atAgent1, "acat_w1")
	if dstatus != http.StatusOK || !bytes.Equal(dbody, content) {
		t.Fatalf("download status = %d body=%q, want 200 + original bytes", dstatus, dbody)
	}
}
