package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/blobstore"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/files"
	filesservice "github.com/oopslink/agent-center/internal/files/service"
	filessql "github.com/oopslink/agent-center/internal/files/sqlite"
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
	if !strings.Contains(url, "?") {
		url += "?org_slug=" + sess.OrgSlug
	} else {
		url += "&org_slug=" + sess.OrgSlug
	}
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
	// No cookie, no org_slug.
	resp, err := http.Get(s.URL + "/api/files/" + ulid)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == 200 {
		t.Fatalf("expected non-200 without session, got 200")
	}
}
