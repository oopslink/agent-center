package api

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/files"
	filesservice "github.com/oopslink/agent-center/internal/files/service"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// =============================================================================
// TASK-conversation file reachability for a PROJECT MEMBER holding no work-item.
//
// THE BUG (reproduced by TestAgentFiles_Download_ProjectMemberNoWorkItem_Task_OK):
// an agent that is a MEMBER of a task's project — but is NOT the task's assignee
// and NOT a formal active participant of the task conversation (the @mentioned
// PD/reviewer case) — could post_message a reply (requireTaskAccess admits the
// project member, T183) yet got 403 file_not_reachable when it tried to
// download an attachment posted in that same conversation. The download domain
// (agentOwnDomainScopes) only enumerated the agent's ASSIGNED tasks + active-
// participant conversations, so a member-but-not-assignee/participant had no
// {ScopeConversation, taskConvID}. agentProjectMemberConvScopes closes the gap at
// the SAME project-member boundary post_message enforces.
//
// The fail-closed guarantees are guarded too:
//   - a NON-member of the same-org project is still denied (cross-project);
//   - a member of a DIFFERENT-org project is still denied (org isolation);
//   - a totally unrelated agent is still denied.
// =============================================================================

// seedTaskConvWithAttachment creates a project (in orgID) with an unassigned task,
// drains the projector so the bound task Conversation exists, then places a file
// attachment into that {ScopeConversation, convID} as user:owner (simulating a
// human/other agent posting an attachment the member never uploaded). Returns the
// task id, the conversation id, the file ULID and the attachment bytes. When
// memberRef != "" that ref is added as a project member first.
func (f *writeToolsFixture) seedTaskConvWithAttachment(
	t *testing.T, orgID, memberRef string, srvURL, bearer, uploaderAgent string,
) (taskID, convID, ulid string, content []byte) {
	t.Helper()
	ctx := context.Background()
	owner := pm.IdentityRef("user:owner")
	pid, err := f.pmSvc.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: orgID, Name: "P-" + orgID, CreatedBy: owner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if memberRef != "" {
		if _, err := f.pmSvc.AddProjectMember(ctx, pmservice.AddProjectMemberCommand{
			ProjectID: pid, IdentityID: pm.IdentityRef(memberRef), Actor: owner,
		}); err != nil {
			t.Fatal(err)
		}
	}
	tid, err := f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{
		ProjectID: pid, Title: "attach here", CreatedBy: owner,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.drain(t) // participant projector creates the bound task Conversation.
	conv, err := f.convRepo.FindByOwnerRef(ctx, conversation.NewTaskOwnerRef(string(tid)))
	if err != nil {
		t.Fatalf("task conv not found: %v", err)
	}
	// Create an orphan blob (no scope), then place it into the task conversation
	// scope as user:owner — the attachment the member did NOT upload.
	content = []byte("task attachment for a member to view")
	ulid = uploadViaAgent(t, srvURL, bearer, uploaderAgent, "", "", content)
	uri, _ := files.NewFileURI(ulid)
	if _, err := f.deps.FilesSvc.AddReference(ctx, filesservice.AddReferenceCmd{
		FileURI: uri, Scope: files.ScopeConversation, ScopeID: string(conv.ID()),
		MimeType: "text/plain", SizeBytes: int64(len(content)), CreatedBy: "user:owner",
	}); err != nil {
		t.Fatal(err)
	}
	return string(tid), string(conv.ID()), ulid, content
}

// RED before the fix / GREEN after: a project MEMBER with no work-item and no
// participant seat can download a task-conversation attachment.
func TestAgentFiles_Download_ProjectMemberNoWorkItem_Task_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	// AG1 is a MEMBER of the task's project — but is NOT the assignee and NOT a
	// participant of the task conversation.
	_, convID, ulid, content := f.seedTaskConvWithAttachment(
		t, atTestOrg, "agent:"+atAgent1, srv.URL, "acat_w1", atAgent1)

	// Guard the preconditions: AG1 is NOT an active participant of the conversation.
	conv, err := f.convRepo.FindByID(context.Background(), conversation.ConversationID(convID))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range conv.Participants() {
		if string(p.IdentityID) == "agent:"+atAgent1 && p.IsActive() {
			t.Fatalf("precondition violated: AG1 is an active participant of the task conv")
		}
	}

	status, _, body := getRawBearer(t, srv.URL, "/admin/files/"+ulid+"?agent_id="+atAgent1, "acat_w1")
	if status != http.StatusOK {
		t.Fatalf("download status = %d, want 200 (project member may download); body = %s", status, body)
	}
	if !bytes.Equal(body, content) {
		t.Fatalf("download body = %q, want %q", body, content)
	}
}

// Fail-closed: an agent that is NOT a member of the same-org project is still
// denied — the member-broadening never leaks across projects.
func TestAgentFiles_Download_NonMemberTask_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	// No member added → AG1 is not a member of this project.
	_, _, ulid, _ := f.seedTaskConvWithAttachment(
		t, atTestOrg, "", srv.URL, "acat_w1", atAgent1)

	status, _, _ := getRawBearer(t, srv.URL, "/admin/files/"+ulid+"?agent_id="+atAgent1, "acat_w1")
	if status != http.StatusForbidden {
		t.Fatalf("download status = %d, want 403 (non-member cross-project fail-closed)", status)
	}
}

// Fail-closed org isolation: even if the agent's ref is listed as a member of a
// project in ANOTHER org, the download domain only enumerates a.OrganizationID(),
// so the cross-org task conversation never appears → still denied.
func TestAgentFiles_Download_CrossOrgMemberTask_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	// Project + task in org-2; AG1's ref is added as a member there anyway.
	_, _, ulid, _ := f.seedTaskConvWithAttachment(
		t, "org-2", "agent:"+atAgent1, srv.URL, "acat_w1", atAgent1)

	status, _, _ := getRawBearer(t, srv.URL, "/admin/files/"+ulid+"?agent_id="+atAgent1, "acat_w1")
	if status != http.StatusForbidden {
		t.Fatalf("download status = %d, want 403 (cross-org member fail-closed)", status)
	}
}

// Fail-closed: a totally unrelated agent (not a member, not a participant, not the
// assignee) is still denied on the same-org task conversation.
func TestAgentFiles_Download_UnrelatedAgentTask_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.addWorkerToken(t, "acat_w2", atWorker2)
	f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	// AG1 is the project member; AG2 is unrelated.
	_, _, ulid, _ := f.seedTaskConvWithAttachment(
		t, atTestOrg, "agent:"+atAgent1, srv.URL, "acat_w1", atAgent1)

	status, _, _ := getRawBearer(t, srv.URL, "/admin/files/"+ulid+"?agent_id="+atAgent2, "acat_w2")
	if status != http.StatusForbidden {
		t.Fatalf("download status = %d, want 403 (unrelated agent fail-closed)", status)
	}
}
