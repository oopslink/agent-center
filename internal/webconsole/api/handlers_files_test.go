package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/blobstore"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/files"
	filesservice "github.com/oopslink/agent-center/internal/files/service"
	filessql "github.com/oopslink/agent-center/internal/files/sqlite"
	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// --- helpers ---------------------------------------------------------------

func mustURI(t *testing.T, ulid string) files.FileURI {
	t.Helper()
	u, err := files.NewFileURI(ulid)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// orgScopedPut executes a PUT with the session cookie + ?org_slug attached.
func orgScopedPut(t *testing.T, url string, body []byte, sess testSession) *http.Response {
	t.Helper()
	url = orgScopedURL(url, sess.OrgSlug)
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(sess.Cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// attachFilesSvc constructs a files Service over the same DB used by deps and a
// fresh temp blobstore root, then assigns it to deps.FilesSvc. Returns the svc.
func attachFilesSvc(t *testing.T, deps *HandlerDeps, db *sql.DB) *filesservice.Service {
	t.Helper()
	root := t.TempDir()
	store, err := blobstore.NewLocalDir(root)
	if err != nil {
		t.Fatal(err)
	}
	svc := filesservice.New(filesservice.Deps{
		DB:         db,
		Sessions:   filessql.NewFileTransferSessionRepo(db),
		References: filessql.NewFileReferenceRepo(db),
		Resolver:   files.NewLocalResolver(""),
		BlobStore:  store,
		IDGen:      idgen.NewGenerator(clock.SystemClock{}),
		Clock:      clock.SystemClock{},
	}).SetGCRepo(filessql.NewBlobMetadataRepo(db))
	deps.FilesSvc = svc
	return svc
}

// uploadBlob runs the create→put→complete flow and returns the file ULID.
func uploadBlob(t *testing.T, baseURL string, sess testSession, content []byte) string {
	t.Helper()
	// create
	resp := orgScopedPost(t, baseURL+"/api/files", `{"content_type":"text/plain","size":`+itoa(len(content))+`}`, sess)
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create upload: status=%d body=%s", resp.StatusCode, b)
	}
	var created map[string]any
	json.NewDecoder(resp.Body).Decode(&created)
	transferID, _ := created["transfer_id"].(string)
	fileURI, _ := created["file_uri"].(string)
	if transferID == "" || fileURI == "" {
		t.Fatalf("missing ids in create response: %v", created)
	}
	// put bytes
	resp = orgScopedPut(t, baseURL+"/api/files/transfer/"+transferID, content, sess)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("put blob: status=%d body=%s", resp.StatusCode, b)
	}
	// complete
	resp = orgScopedPost(t, baseURL+"/api/files/transfer/"+transferID+"/complete", `{"size":`+itoa(len(content))+`}`, sess)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("complete: status=%d body=%s", resp.StatusCode, b)
	}
	return files.FileURI(fileURI).ULID()
}

func setupNamedTestSession(t *testing.T, db *sql.DB, username, orgSlug string) testSession {
	t.Helper()
	ctx := context.Background()
	hash, _ := identity.HashPasscode("123456")
	ident, err := identity.IdentityFactory{}.NewUser(username, hash)
	if err != nil {
		t.Fatal(err)
	}
	idRepo := identity.NewSQLiteIdentityRepo(db)
	orgRepo := identity.NewSQLiteOrganizationRepo(db)
	memberRepo := identity.NewSQLiteMemberRepo(db)
	if err := idRepo.Save(ctx, ident); err != nil {
		t.Fatal(err)
	}
	org, err := identity.OrganizationFactory{}.New(orgSlug, orgSlug, ident.ID())
	if err != nil {
		t.Fatal(err)
	}
	if err := orgRepo.Save(ctx, org); err != nil {
		t.Fatal(err)
	}
	member, err := identity.MemberFactory{}.New(org.ID(), ident.ID(), identity.RoleOwner, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := memberRepo.Save(ctx, member); err != nil {
		t.Fatal(err)
	}
	jwt, err := identity.MintJWT(ident.ID(), testSigningKey)
	if err != nil {
		t.Fatal(err)
	}
	return testSession{
		IdentityID: ident.ID(),
		OrgID:      org.ID(),
		OrgSlug:    org.Slug(),
		Cookie:     &http.Cookie{Name: "ac_session", Value: jwt},
	}
}

func seedParticipantDM(t *testing.T, deps HandlerDeps, sess testSession) string {
	t.Helper()
	owner := conversation.IdentityRef("user:" + sess.IdentityID)
	res, err := deps.MessageWriter.OpenConversation(context.Background(), convservice.OpenCommand{
		Kind:           conversation.ConversationKindDM,
		OrganizationID: sess.OrgID,
		Participants: []conversation.ParticipantElement{
			{IdentityID: owner, Role: "owner", JoinedAt: "t", JoinedBy: owner},
		},
		CreatedBy: owner,
		Actor:     observability.Actor(owner),
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(res.ConversationID)
}

func sendAttachmentMessage(t *testing.T, baseURL, convID string, sess testSession, fileURI string) (int, []byte) {
	t.Helper()
	body := fmt.Sprintf(`{"content":"see attached","attachments":[{"uri":%q,"filename":"note.txt","mime_type":"text/plain","size":4}]}`, fileURI)
	resp := orgScopedPost(t, baseURL+"/api/conversations/"+convID+"/messages", body, sess)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func responseBytes(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// =============================================================================
// Upload → download round-trip (conversation scope the caller participates in)
// =============================================================================

func TestAPI_Files_UploadDownload_ConversationRoundTrip(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	svc := attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()

	// A conversation the caller participates in (creator = owner participant).
	owner := "user:" + sess.IdentityID
	openRes, err := deps.MessageWriter.OpenConversation(ctx, convservice.OpenCommand{
		Kind:           conversation.ConversationKindDM,
		OrganizationID: sess.OrgID,
		Participants: []conversation.ParticipantElement{
			{IdentityID: conversation.IdentityRef(owner), Role: "owner", JoinedAt: "t", JoinedBy: conversation.IdentityRef(owner)},
		},
		CreatedBy: conversation.IdentityRef(owner),
		Actor:     observability.Actor(owner),
	})
	if err != nil {
		t.Fatal(err)
	}

	content := []byte("hello transport")
	ulid := uploadBlob(t, s.URL, sess, content)

	// Attach a reference on the conversation the caller participates in.
	if _, err := svc.AddReference(ctx, filesservice.AddReferenceCmd{
		FileURI: mustURI(t, ulid), Scope: files.ScopeConversation, ScopeID: string(openRes.ConversationID),
		Filename: "note.txt", MimeType: "text/plain", CreatedBy: owner,
	}); err != nil {
		t.Fatal(err)
	}

	resp := orgScopedGet(t, s.URL+"/api/files/"+ulid, sess)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("download: status=%d body=%s", resp.StatusCode, b)
	}
	got, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(got, content) {
		t.Fatalf("body mismatch: %q != %q", got, content)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/plain" {
		t.Fatalf("content-type: %q", ct)
	}
}

// =============================================================================
// Fail-closed: no references / unreachable scope → 403
// =============================================================================

func TestAPI_Files_Download_NoRefs_403(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()

	ulid := uploadBlob(t, s.URL, sess, []byte("orphan"))
	// No reference attached → fail-closed.
	resp := orgScopedGet(t, s.URL+"/api/files/"+ulid, sess)
	if resp.StatusCode != 403 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403, got %d body=%s", resp.StatusCode, b)
	}
}

func TestAPI_Files_Download_ProjectNonMember_403(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	svc := attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()

	// A project the caller is NOT a member of (created by someone else).
	pid, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: sess.OrgID, Name: "Foreign", CreatedBy: pm.IdentityRef("user:someone-else"),
	})
	if err != nil {
		t.Fatal(err)
	}

	ulid := uploadBlob(t, s.URL, sess, []byte("secret"))
	if _, err := svc.AddReference(ctx, filesservice.AddReferenceCmd{
		FileURI: mustURI(t, ulid), Scope: files.ScopeProject, ScopeID: string(pid),
		Filename: "x", CreatedBy: "user:someone-else",
	}); err != nil {
		t.Fatal(err)
	}

	resp := orgScopedGet(t, s.URL+"/api/files/"+ulid, sess)
	if resp.StatusCode != 403 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403 for non-member project, got %d body=%s", resp.StatusCode, b)
	}
}

// =============================================================================
// Per-scope resolver correctness
// =============================================================================

func TestAPI_Files_Download_TaskInMemberProject_200(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	svc := attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	// Project owned by the caller (→ owner member) + a task in it.
	pid, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: sess.OrgID, Name: "Mine", CreatedBy: caller,
	})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{
		ProjectID: pid, Title: "t", CreatedBy: caller,
	})
	if err != nil {
		t.Fatal(err)
	}

	ulid := uploadBlob(t, s.URL, sess, []byte("task file"))
	if _, err := svc.AddReference(ctx, filesservice.AddReferenceCmd{
		FileURI: mustURI(t, ulid), Scope: files.ScopeTask, ScopeID: string(tid),
		Filename: "x", CreatedBy: string(caller),
	}); err != nil {
		t.Fatal(err)
	}

	resp := orgScopedGet(t, s.URL+"/api/files/"+ulid, sess)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 for task in member project, got %d body=%s", resp.StatusCode, b)
	}
}

func TestAPI_Files_Download_AgentScopeOnly_403(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	svc := attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()

	ulid := uploadBlob(t, s.URL, sess, []byte("agent only"))
	// Only live reference is agent-scope → not human-accessible.
	if _, err := svc.AddReference(ctx, filesservice.AddReferenceCmd{
		FileURI: mustURI(t, ulid), Scope: files.ScopeAgent, ScopeID: "AG1",
		Filename: "x", CreatedBy: "agent:AG1",
	}); err != nil {
		t.Fatal(err)
	}

	resp := orgScopedGet(t, s.URL+"/api/files/"+ulid, sess)
	if resp.StatusCode != 403 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403 for agent-only ref, got %d body=%s", resp.StatusCode, b)
	}
}

// TestAPI_Files_Download_ChannelNonParticipant_200 is the T244 fix: a CHANNEL is
// readable by every org member (the channel list + message-read gate don't check
// participation), so its attachments — including ones an AGENT posted — must be
// downloadable by any org member even when they are not an explicit participant.
// Before the fix this returned 403 ("no reachable reference grants download")
// although the same member could read the message and see the attachment.
func TestAPI_Files_Download_ChannelNonParticipant_200(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	svc := attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()

	// A channel owned by SOMEONE ELSE — the caller (sess) is NOT a participant.
	// An agent posts the attachment (CreatedBy agent), the caller is a plain org
	// member who can see the channel.
	other := "user:someone-else"
	openRes, err := deps.MessageWriter.OpenConversation(ctx, convservice.OpenCommand{
		Kind:           conversation.ConversationKindChannel,
		Name:           "general",
		OrganizationID: sess.OrgID,
		Participants: []conversation.ParticipantElement{
			{IdentityID: conversation.IdentityRef(other), Role: "owner", JoinedAt: "t", JoinedBy: conversation.IdentityRef(other)},
		},
		CreatedBy: conversation.IdentityRef(other),
		Actor:     observability.Actor(other),
	})
	if err != nil {
		t.Fatal(err)
	}

	content := []byte("agent attachment in a channel")
	ulid := uploadBlob(t, s.URL, sess, content)
	if _, err := svc.AddReference(ctx, filesservice.AddReferenceCmd{
		FileURI: mustURI(t, ulid), Scope: files.ScopeConversation, ScopeID: string(openRes.ConversationID),
		Filename: "report.txt", MimeType: "text/plain", CreatedBy: "agent:AG1",
	}); err != nil {
		t.Fatal(err)
	}

	resp := orgScopedGet(t, s.URL+"/api/files/"+ulid, sess)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("channel non-participant download: status=%d body=%s, want 200", resp.StatusCode, b)
	}
	got, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(got, content) {
		t.Fatalf("body mismatch: %q != %q", got, content)
	}
}

// TestAPI_Files_Download_DMNonParticipant_403 is the security boundary the T244
// fix must NOT cross: a DM stays strictly participant-gated, so a non-participant
// org member cannot download a DM attachment.
func TestAPI_Files_Download_DMNonParticipant_403(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	svc := attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()

	// A DM between two OTHER identities — the caller (sess) is not a participant.
	a, b := "user:alice", "user:bob"
	openRes, err := deps.MessageWriter.OpenConversation(ctx, convservice.OpenCommand{
		Kind:           conversation.ConversationKindDM,
		OrganizationID: sess.OrgID,
		Participants: []conversation.ParticipantElement{
			{IdentityID: conversation.IdentityRef(a), Role: "owner", JoinedAt: "t", JoinedBy: conversation.IdentityRef(a)},
			{IdentityID: conversation.IdentityRef(b), Role: "member", JoinedAt: "t", JoinedBy: conversation.IdentityRef(a)},
		},
		CreatedBy: conversation.IdentityRef(a),
		Actor:     observability.Actor(a),
	})
	if err != nil {
		t.Fatal(err)
	}

	ulid := uploadBlob(t, s.URL, sess, []byte("private dm file"))
	if _, err := svc.AddReference(ctx, filesservice.AddReferenceCmd{
		FileURI: mustURI(t, ulid), Scope: files.ScopeConversation, ScopeID: string(openRes.ConversationID),
		Filename: "secret.txt", MimeType: "text/plain", CreatedBy: a,
	}); err != nil {
		t.Fatal(err)
	}

	resp := orgScopedGet(t, s.URL+"/api/files/"+ulid, sess)
	if resp.StatusCode != 403 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("DM non-participant download: status=%d body=%s, want 403", resp.StatusCode, b)
	}
}

// savePlanScopedConv builds and persists a PROJECT-SCOPED conversation (plan/task
// /issue owner_ref) whose ONLY participant is an agent — so a human caller can
// VIEW it (org/project read) but is NOT a participant. Mirrors what the
// PlanParticipantProjector / task ParticipantProjector produce: an additive
// participant set that omits non-@mentioned project members.
func savePlanScopedConv(t *testing.T, deps HandlerDeps, id string, kind conversation.ConversationKind, owner conversation.OwnerRef, orgID string) conversation.ConversationID {
	t.Helper()
	conv, err := conversation.NewConversation(conversation.NewConversationInput{
		ID:             conversation.ConversationID(id),
		Kind:           kind,
		OwnerRef:       owner,
		OrganizationID: orgID,
		CreatedBy:      conversation.IdentityRef("system"),
		OpenedAt:       time.Now().UTC(),
		Participants: []conversation.ParticipantElement{
			{IdentityID: "agent:AG1", Role: "member", JoinedAt: "t", JoinedBy: conversation.IdentityRef("system")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := deps.ConvRepo.Save(context.Background(), conv); err != nil {
		t.Fatal(err)
	}
	return conv.ID()
}

// TestAPI_Files_Download_PlanConversation_ProjectMemberNonParticipant_200 is the
// plan-chat 403 fix (T244 follow-up): a PLAN conversation's participant set is only
// its creator + the @mention-dispatched selected-task assignees (additive), but it
// is READABLE by any member of its owning project. So a project member who opens
// the plan chat but was never @mentioned (e.g. the human owner/PD) is NOT a
// participant and used to get 403 ("no reachable reference grants download") on an
// attachment — including one an AGENT posted — that they can plainly see. Download
// must now mirror read: a project member reaches it. This is the SAME gate the
// ScopeTask/ScopeIssue file references already use.
func TestAPI_Files_Download_PlanConversation_ProjectMemberNonParticipant_200(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	deps = setupPlanAPI(t, deps).deps // wire the Plans repo so GetPlan resolves
	svc := attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	// Project owned by the caller (→ owner member) + a plan in it.
	pid, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: sess.OrgID, Name: "Mine", CreatedBy: caller,
	})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := deps.PM.CreatePlan(ctx, pmservice.CreatePlanCommand{
		ProjectID: pid, Name: "P1", CreatedBy: caller,
	})
	if err != nil {
		t.Fatal(err)
	}
	convID := savePlanScopedConv(t, deps, "PLAN-CONV-1", conversation.ConversationKindPlan,
		conversation.NewPlanOwnerRef(string(planID)), sess.OrgID)

	content := []byte("agent attachment in a plan chat")
	ulid := uploadBlob(t, s.URL, sess, content)
	if _, err := svc.AddReference(ctx, filesservice.AddReferenceCmd{
		FileURI: mustURI(t, ulid), Scope: files.ScopeConversation, ScopeID: string(convID),
		Filename: "design.txt", MimeType: "text/plain", CreatedBy: "agent:AG1",
	}); err != nil {
		t.Fatal(err)
	}

	resp := orgScopedGet(t, s.URL+"/api/files/"+ulid, sess)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("plan-chat project-member non-participant download: status=%d body=%s, want 200", resp.StatusCode, b)
	}
	got, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(got, content) {
		t.Fatalf("body mismatch: %q != %q", got, content)
	}
}

// TestAPI_Files_Download_PlanConversation_NonProjectMember_403 is the security
// boundary the plan-chat fix must NOT cross: a plan conversation in a project the
// caller is NOT a member of (and is not a participant of) stays fail-closed.
func TestAPI_Files_Download_PlanConversation_NonProjectMember_403(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	deps = setupPlanAPI(t, deps).deps // wire the Plans repo so GetPlan resolves
	svc := attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()

	// Plan in a FOREIGN project (created by someone else) — caller is neither a
	// project member nor a participant.
	other := pm.IdentityRef("user:someone-else")
	pid, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: sess.OrgID, Name: "Foreign", CreatedBy: other,
	})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := deps.PM.CreatePlan(ctx, pmservice.CreatePlanCommand{
		ProjectID: pid, Name: "P1", CreatedBy: other,
	})
	if err != nil {
		t.Fatal(err)
	}
	convID := savePlanScopedConv(t, deps, "PLAN-CONV-FOREIGN", conversation.ConversationKindPlan,
		conversation.NewPlanOwnerRef(string(planID)), sess.OrgID)

	ulid := uploadBlob(t, s.URL, sess, []byte("foreign plan secret"))
	if _, err := svc.AddReference(ctx, filesservice.AddReferenceCmd{
		FileURI: mustURI(t, ulid), Scope: files.ScopeConversation, ScopeID: string(convID),
		Filename: "x.txt", MimeType: "text/plain", CreatedBy: "agent:AG1",
	}); err != nil {
		t.Fatal(err)
	}

	resp := orgScopedGet(t, s.URL+"/api/files/"+ulid, sess)
	if resp.StatusCode != 403 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("plan-chat non-project-member download: status=%d body=%s, want 403", resp.StatusCode, b)
	}
}

// TestAPI_Files_Download_TaskConversation_ProjectMemberNonParticipant_200 is the
// class-guard: the same plan-chat fix must hold for a TASK conversation (owner_ref
// pm://tasks/...), whose participant set is likewise additive. A project member who
// is not an explicit participant reaches its attachments.
func TestAPI_Files_Download_TaskConversation_ProjectMemberNonParticipant_200(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	svc := attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: sess.OrgID, Name: "Mine", CreatedBy: caller,
	})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{
		ProjectID: pid, Title: "t", CreatedBy: caller,
	})
	if err != nil {
		t.Fatal(err)
	}
	convID := savePlanScopedConv(t, deps, "TASK-CONV-1", conversation.ConversationKindTask,
		conversation.NewTaskOwnerRef(string(tid)), sess.OrgID)

	content := []byte("agent attachment in a task chat")
	ulid := uploadBlob(t, s.URL, sess, content)
	if _, err := svc.AddReference(ctx, filesservice.AddReferenceCmd{
		FileURI: mustURI(t, ulid), Scope: files.ScopeConversation, ScopeID: string(convID),
		Filename: "x.txt", MimeType: "text/plain", CreatedBy: "agent:AG1",
	}); err != nil {
		t.Fatal(err)
	}

	resp := orgScopedGet(t, s.URL+"/api/files/"+ulid, sess)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("task-chat project-member non-participant download: status=%d body=%s, want 200", resp.StatusCode, b)
	}
}

// =============================================================================
// Live-only: soft-deleted reference does not grant access
// =============================================================================

func TestAPI_Files_Download_SoftDeletedRef_403(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	svc := attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: sess.OrgID, Name: "Mine2", CreatedBy: caller,
	})
	if err != nil {
		t.Fatal(err)
	}
	ulid := uploadBlob(t, s.URL, sess, []byte("gone"))
	ref, err := svc.AddReference(ctx, filesservice.AddReferenceCmd{
		FileURI: mustURI(t, ulid), Scope: files.ScopeProject, ScopeID: string(pid),
		Filename: "x", CreatedBy: string(caller),
	})
	if err != nil {
		t.Fatal(err)
	}
	// 200 while live.
	if resp := orgScopedGet(t, s.URL+"/api/files/"+ulid, sess); resp.StatusCode != 200 {
		t.Fatalf("expected 200 while live, got %d", resp.StatusCode)
	}
	// Soft-delete the only reference → fail-closed.
	if err := svc.SoftDeleteReference(ctx, ref.ID); err != nil {
		t.Fatal(err)
	}
	resp := orgScopedGet(t, s.URL+"/api/files/"+ulid, sess)
	if resp.StatusCode != 403 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403 after soft-delete, got %d body=%s", resp.StatusCode, b)
	}
}

// =============================================================================
// Auth: no session → 401/403/404 via requireOrgMember
// =============================================================================

func TestAPI_Files_Download_NoSession_Unauthorized(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()
	ulid := uploadBlob(t, s.URL, sess, []byte("x"))
	// Valid org slug in the path, but no session cookie → requireOrgMember 401.
	resp, err := http.Get(s.URL + "/api/orgs/" + sess.OrgSlug + "/files/" + ulid)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == 200 {
		t.Fatalf("expected non-200 without session, got 200")
	}
}

func TestAPI_Files_CompleteUpload_NonInitiatorForbiddenNoUploaderRef(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	ownerSess := setupTestSession(t, db, deps)
	otherSess := setupNamedTestSession(t, db, "completeother", "complete-other")
	svc := attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedPost(t, s.URL+"/api/files", `{"content_type":"text/plain","size":4}`, ownerSess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status=%d body=%s", resp.StatusCode, responseBytes(t, resp))
	}
	var created map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	transferID := created["transfer_id"].(string)
	fileURI := created["file_uri"].(string)
	ulid := files.FileURI(fileURI).ULID()

	put := orgScopedPut(t, s.URL+"/api/files/transfer/"+transferID, []byte("data"), ownerSess)
	if put.StatusCode != http.StatusOK {
		t.Fatalf("put: status=%d body=%s", put.StatusCode, responseBytes(t, put))
	}
	complete := orgScopedPost(t, s.URL+"/api/files/transfer/"+transferID+"/complete", `{"size":4}`, otherSess)
	if complete.StatusCode != http.StatusForbidden {
		t.Fatalf("non-initiator complete: status=%d body=%s", complete.StatusCode, responseBytes(t, complete))
	}
	refs, err := svc.ListReferences(context.Background(), mustURI(t, ulid))
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 {
		t.Fatalf("non-initiator complete wrote refs: %+v", refs)
	}
}

func TestAPI_SendMessage_AttachmentOwnUploadCreatesConversationRefAndDownload(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	svc := attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()

	convID := seedParticipantDM(t, deps, sess)
	ulid := uploadBlob(t, s.URL, sess, []byte("mine"))
	fileURI := "ac://files/" + ulid

	status, body := sendAttachmentMessage(t, s.URL, convID, sess, fileURI)
	if status != http.StatusCreated {
		t.Fatalf("send attachment: status=%d body=%s", status, body)
	}
	msgs, err := deps.MsgRepo.FindByConversationID(context.Background(), conversation.ConversationID(convID), conversation.MessageFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || len(msgs[0].Attachments()) != 1 || msgs[0].Attachments()[0].URI != fileURI {
		t.Fatalf("message attachment not persisted: %+v", msgs)
	}
	refs, err := svc.ListReferences(context.Background(), mustURI(t, ulid))
	if err != nil {
		t.Fatal(err)
	}
	var conversationRefs int
	for _, ref := range refs {
		if ref.Scope == files.ScopeConversation && ref.ScopeID == convID {
			conversationRefs++
		}
	}
	if conversationRefs != 1 {
		t.Fatalf("conversation ref count = %d, refs=%+v", conversationRefs, refs)
	}
	resp := orgScopedGet(t, s.URL+"/api/files/"+ulid, sess)
	got := responseBytes(t, resp)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(got, []byte("mine")) {
		t.Fatalf("download after attach: status=%d body=%s", resp.StatusCode, got)
	}
}

func TestAPI_SendMessage_AttachmentUnreachableWritesNothingAndIsOpaque(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	otherSess := setupNamedTestSession(t, db, "otheruser", "otherorg")
	svc := attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()

	convID := seedParticipantDM(t, deps, sess)
	foreignULID := uploadBlob(t, s.URL, otherSess, []byte("away"))
	foreignURI := "ac://files/" + foreignULID
	missingURI := "ac://files/" + idgen.MustNewULID()

	foreignStatus, foreignBody := sendAttachmentMessage(t, s.URL, convID, sess, foreignURI)
	missingStatus, missingBody := sendAttachmentMessage(t, s.URL, convID, sess, missingURI)
	if foreignStatus != http.StatusForbidden || missingStatus != http.StatusForbidden {
		t.Fatalf("statuses: foreign=%d body=%s missing=%d body=%s", foreignStatus, foreignBody, missingStatus, missingBody)
	}
	if !bytes.Equal(foreignBody, missingBody) {
		t.Fatalf("unreachable attach responses differ:\nforeign=%s\nmissing=%s", foreignBody, missingBody)
	}
	msgs, err := deps.MsgRepo.FindByConversationID(context.Background(), conversation.ConversationID(convID), conversation.MessageFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("unreachable attachment wrote messages: %+v", msgs)
	}
	refs, err := svc.ListReferences(context.Background(), mustURI(t, foreignULID))
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs {
		if ref.Scope == files.ScopeConversation && ref.ScopeID == convID {
			t.Fatalf("unreachable attachment wrote conversation ref: %+v", refs)
		}
	}
}

func TestAPI_Files_Download_UnreachableResponsesAreOpaque(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	otherSess := setupNamedTestSession(t, db, "downloadother", "download-other")
	attachFilesSvc(t, &deps, db)
	s := newTestServer(t, deps)
	defer s.Close()

	foreignULID := uploadBlob(t, s.URL, otherSess, []byte("foreign"))
	missingULID := idgen.MustNewULID()

	foreignResp := orgScopedGet(t, s.URL+"/api/files/"+foreignULID, sess)
	foreignBody := responseBytes(t, foreignResp)
	missingResp := orgScopedGet(t, s.URL+"/api/files/"+missingULID, sess)
	missingBody := responseBytes(t, missingResp)
	if foreignResp.StatusCode != http.StatusForbidden || missingResp.StatusCode != http.StatusForbidden {
		t.Fatalf("statuses: foreign=%d body=%s missing=%d body=%s", foreignResp.StatusCode, foreignBody, missingResp.StatusCode, missingBody)
	}
	if !bytes.Equal(foreignBody, missingBody) {
		t.Fatalf("unreachable download responses differ:\nforeign=%s\nmissing=%s", foreignBody, missingBody)
	}
}
