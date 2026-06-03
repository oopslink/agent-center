package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oopslink/agent-center/internal/blobstore"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/files"
	filesservice "github.com/oopslink/agent-center/internal/files/service"
	filessql "github.com/oopslink/agent-center/internal/files/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// =============================================================================
// v2.7 post-D3 (task #104) — agent file MCP tools. These tests stand up the REAL
// admin server + AuthMiddleware over the writeToolsFixture's full pm → outbox →
// projector pipeline (so the agent's WorkItem + the task Conversation exist as in
// production) plus a temp-blobstore files Service. They exercise the agent-domain
// reachability authz model: an agent reaches a blob only through the scopes it
// can enumerate from its OWN work (own agent scope + own task + that task's
// derived issue + bound conversation).
// =============================================================================

// attachAgentFilesSvc constructs a files Service over the fixture's DB and a
// fresh temp blobstore root, then wires it into the fixture's deps. Returns the
// svc. Mirrors the webconsole D3-d test harness (attachFilesSvc).
func (f *writeToolsFixture) attachAgentFilesSvc(t *testing.T) *filesservice.Service {
	t.Helper()
	root := t.TempDir()
	store, err := blobstore.NewLocalDir(root)
	if err != nil {
		t.Fatal(err)
	}
	svc := filesservice.New(filesservice.Deps{
		DB:         f.db,
		Sessions:   filessql.NewFileTransferSessionRepo(f.db),
		References: filessql.NewFileReferenceRepo(f.db),
		Resolver:   files.NewLocalResolver(""),
		BlobStore:  store,
		IDGen:      idgen.NewGenerator(clock.SystemClock{}),
		Clock:      clock.SystemClock{},
	}).SetGCRepo(filessql.NewBlobMetadataRepo(f.db))
	f.deps.FilesSvc = svc
	return svc
}

// filesServer rebuilds the test HTTP server AFTER deps.FilesSvc has been wired
// (f.server() captures deps by value through the middleware closure, so the svc
// must be attached before this is called).
func (f *writeToolsFixture) filesServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := NewServerWithDeps("", ServerDeps{})
	h := AuthMiddleware(f.verifier)(WithDeps(f.deps)(srv.Handler()))
	httpsrv := httptest.NewServer(h)
	t.Cleanup(httpsrv.Close)
	return httpsrv
}

// putBearer streams raw bytes via PUT with a bearer header. Returns status + the
// decoded JSON body (best-effort).
func putBearer(t *testing.T, base, path, bearer string, content []byte) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, base+path, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// getRawBearer GETs path and returns status + Content-Type + raw body bytes.
func getRawBearer(t *testing.T, base, path, bearer string) (int, string, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Content-Type"), b
}

// uploadViaAgent runs upload_file → PUT → complete for the agent, scoped to the
// given {scope, scope_id}. Returns the file ULID.
func uploadViaAgent(t *testing.T, base, bearer, agentID, scope, scopeID string, content []byte) string {
	t.Helper()
	status, body := postBearer(t, base, "/admin/agent-tools/upload_file", bearer, map[string]any{
		"agent_id": agentID, "content_type": "text/plain", "size": len(content),
		"scope": scope, "scope_id": scopeID,
	})
	if status != http.StatusOK {
		t.Fatalf("upload_file status = %d, want 200; body = %v", status, body)
	}
	transferID, _ := body["transfer_id"].(string)
	fileURI, _ := body["file_uri"].(string)
	if transferID == "" || fileURI == "" {
		t.Fatalf("missing ids in upload_file response: %v", body)
	}
	putStatus, putBody := putBearer(t, base, "/admin/files/transfer/"+transferID+"?agent_id="+agentID, bearer, content)
	if putStatus != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200; body = %v", putStatus, putBody)
	}
	cStatus, cBody := postBearer(t, base, "/admin/files/transfer/"+transferID+"/complete", bearer, map[string]any{
		"agent_id": agentID, "size": len(content), "scope": scope, "scope_id": scopeID,
	})
	if cStatus != http.StatusOK {
		t.Fatalf("complete status = %d, want 200; body = %v", cStatus, cBody)
	}
	return files.FileURI(fileURI).ULID()
}

// --- upload→put→complete→download round-trip (own task) ----------------------

func TestAgentFiles_RoundTrip_OwnTask(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t) // AG1 holds a WorkItem for this task.
	f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	content := []byte("hello agent file")
	ulid := uploadViaAgent(t, srv.URL, "acat_w1", atAgent1, "task", tid, content)

	status, ct, body := getRawBearer(t, srv.URL, "/admin/files/"+ulid+"?agent_id="+atAgent1, "acat_w1")
	if status != http.StatusOK {
		t.Fatalf("download status = %d, want 200; body = %s", status, body)
	}
	if !bytes.Equal(body, content) {
		t.Fatalf("download body = %q, want %q", body, content)
	}
	if ct != "text/plain" {
		t.Fatalf("Content-Type = %q, want text/plain (from the reference MimeType)", ct)
	}
}

// --- download fail-closed: no live ref --------------------------------------

func TestAgentFiles_Download_NoRef_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.seedRunningTask(t)
	svc := f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	// Upload a blob with NO scope → no reference is ever created.
	content := []byte("orphan")
	ulid := uploadViaAgent(t, srv.URL, "acat_w1", atAgent1, "", "", content)
	// Sanity: there are no live references.
	uri, _ := files.NewFileURI(ulid)
	refs, _ := svc.ListReferences(context.Background(), uri)
	if len(refs) != 0 {
		t.Fatalf("expected 0 live refs, got %d", len(refs))
	}

	status, _, _ := getRawBearer(t, srv.URL, "/admin/files/"+ulid+"?agent_id="+atAgent1, "acat_w1")
	if status != http.StatusForbidden {
		t.Fatalf("download status = %d, want 403 (no live ref, fail-closed)", status)
	}
}

// --- download fail-closed: ref in a scope outside the agent's domain --------

func TestAgentFiles_Download_RefOutsideDomain_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.seedRunningTask(t)
	svc := f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	// Upload a blob (no scope), then attach it ONLY to a project scope the agent
	// cannot enumerate (own-domain is agent/task/issue/conversation, never project).
	content := []byte("project doc")
	ulid := uploadViaAgent(t, srv.URL, "acat_w1", atAgent1, "", "", content)
	uri, _ := files.NewFileURI(ulid)
	if _, err := svc.AddReference(context.Background(), filesservice.AddReferenceCmd{
		FileURI: uri, Scope: files.ScopeProject, ScopeID: "some-project", CreatedBy: "user:owner",
	}); err != nil {
		t.Fatal(err)
	}

	status, _, _ := getRawBearer(t, srv.URL, "/admin/files/"+ulid+"?agent_id="+atAgent1, "acat_w1")
	if status != http.StatusForbidden {
		t.Fatalf("download status = %d, want 403 (ref in non-domain scope)", status)
	}
}

// --- attach_file own task → 200 ----------------------------------------------

func TestAgentFiles_Attach_OwnTask_OK(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	// Upload an unscoped blob, then attach it to the agent's own task.
	ulid := uploadViaAgent(t, srv.URL, "acat_w1", atAgent1, "", "", []byte("attachme"))
	uri, _ := files.NewFileURI(ulid)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/attach_file", "acat_w1", map[string]any{
		"agent_id": atAgent1, "file_uri": string(uri), "scope": "task", "scope_id": tid,
	})
	if status != http.StatusOK {
		t.Fatalf("attach status = %d, want 200; body = %v", status, body)
	}
	if body["reference_id"] == nil || body["reference_id"] == "" {
		t.Fatalf("no reference_id in attach response: %v", body)
	}
	// The attach made the blob reachable: the agent can now download it.
	dl, _, _ := getRawBearer(t, srv.URL, "/admin/files/"+ulid+"?agent_id="+atAgent1, "acat_w1")
	if dl != http.StatusOK {
		t.Fatalf("post-attach download status = %d, want 200", dl)
	}
}

// --- attach_file to a non-own scope → 403 ------------------------------------

func TestAgentFiles_Attach_ForeignTask_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.seedRunningTask(t)
	f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	// A task the agent does NOT hold a WorkItem for.
	ctx := context.Background()
	owner := pm.IdentityRef("user:owner")
	pid, _ := f.pmSvc.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: atTestOrg, Name: "P2", CreatedBy: owner})
	otherTid, _ := f.pmSvc.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "other", CreatedBy: owner})
	f.drain(t)

	ulid := uploadViaAgent(t, srv.URL, "acat_w1", atAgent1, "", "", []byte("x"))
	uri, _ := files.NewFileURI(ulid)

	// Attach to a foreign task → 403.
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/attach_file", "acat_w1", map[string]any{
		"agent_id": atAgent1, "file_uri": string(uri), "scope": "task", "scope_id": string(otherTid),
	})
	if status != http.StatusForbidden || body["error"] != "scope_not_in_agent_domain" {
		t.Fatalf("foreign-task attach status = %d err=%v, want 403 scope_not_in_agent_domain", status, body["error"])
	}
	// Attach to a project scope (never in an agent's domain) → 403.
	status2, body2 := postBearer(t, srv.URL, "/admin/agent-tools/attach_file", "acat_w1", map[string]any{
		"agent_id": atAgent1, "file_uri": string(uri), "scope": "project", "scope_id": string(pid),
	})
	if status2 != http.StatusForbidden || body2["error"] != "scope_not_in_agent_domain" {
		t.Fatalf("project attach status = %d err=%v, want 403 scope_not_in_agent_domain", status2, body2["error"])
	}
}

// --- upload_file authz-first: scope in domain → session; not in domain → 403 --

func TestAgentFiles_Upload_ScopeInDomain_CreatesSession(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/upload_file", "acat_w1", map[string]any{
		"agent_id": atAgent1, "content_type": "text/plain", "size": 3, "scope": "task", "scope_id": tid,
	})
	if status != http.StatusOK {
		t.Fatalf("upload_file status = %d, want 200; body = %v", status, body)
	}
	if body["transfer_id"] == nil || body["transfer_id"] == "" {
		t.Fatalf("expected a transfer_id (session created): %v", body)
	}
}

func TestAgentFiles_Upload_ScopeNotInDomain_403_NoSession(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.seedRunningTask(t)
	f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/upload_file", "acat_w1", map[string]any{
		"agent_id": atAgent1, "content_type": "text/plain", "size": 3,
		"scope": "task", "scope_id": "not-my-task",
	})
	if status != http.StatusForbidden || body["error"] != "scope_not_in_agent_domain" {
		t.Fatalf("status = %d err=%v, want 403 scope_not_in_agent_domain", status, body["error"])
	}
	// NO session was created: there is no transfer_id and the body carries only
	// the error envelope.
	if body["transfer_id"] != nil {
		t.Fatalf("a session was created despite the authz failure: %v", body)
	}
}

// --- write-once: second PUT → 409 --------------------------------------------

func TestAgentFiles_WriteOnce_SecondPut_409(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/upload_file", "acat_w1", map[string]any{
		"agent_id": atAgent1, "content_type": "text/plain", "size": 5, "scope": "task", "scope_id": tid,
	})
	if status != http.StatusOK {
		t.Fatalf("upload_file status = %d; body = %v", status, body)
	}
	transferID, _ := body["transfer_id"].(string)
	content := []byte("first")
	p1, _ := putBearer(t, srv.URL, "/admin/files/transfer/"+transferID+"?agent_id="+atAgent1, "acat_w1", content)
	if p1 != http.StatusOK {
		t.Fatalf("first PUT status = %d, want 200", p1)
	}
	p2, b2 := putBearer(t, srv.URL, "/admin/files/transfer/"+transferID+"?agent_id="+atAgent1, "acat_w1", content)
	if p2 != http.StatusConflict {
		t.Fatalf("second PUT status = %d, want 409 (write-once); body = %v", p2, b2)
	}
}

// --- cross-worker guardrail still applies (requireAgentOnWorker) -------------

func TestAgentFiles_CrossWorker_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	f.seedRunningTask(t)
	f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	// W1 token operating AG2 (bound to W2) → guardrail 403 on upload_file.
	status, body := postBearer(t, srv.URL, "/admin/agent-tools/upload_file", "acat_w1", map[string]any{
		"agent_id": atAgent2, "content_type": "text/plain", "size": 1,
	})
	if status != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("upload_file cross-worker status = %d err=%v, want 403 agent_not_bound_to_worker", status, body["error"])
	}
}

// --- soft-deleted ref no longer grants download (fail-closed) ----------------

func TestAgentFiles_SoftDeletedRef_NoDownload_403(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	tid := f.seedRunningTask(t)
	svc := f.attachAgentFilesSvc(t)
	srv := f.filesServer(t)

	// Upload+attach to the own task → reachable.
	ulid := uploadViaAgent(t, srv.URL, "acat_w1", atAgent1, "task", tid, []byte("soft"))
	uri, _ := files.NewFileURI(ulid)
	dl, _, _ := getRawBearer(t, srv.URL, "/admin/files/"+ulid+"?agent_id="+atAgent1, "acat_w1")
	if dl != http.StatusOK {
		t.Fatalf("pre-delete download status = %d, want 200", dl)
	}
	// Soft-delete the only reference.
	refs, err := svc.ListReferences(context.Background(), uri)
	if err != nil || len(refs) != 1 {
		t.Fatalf("expected 1 live ref, got %d (err=%v)", len(refs), err)
	}
	if err := svc.SoftDeleteReference(context.Background(), refs[0].ID); err != nil {
		t.Fatal(err)
	}
	// Now fail-closed: no LIVE ref grants the download.
	dl2, _, _ := getRawBearer(t, srv.URL, "/admin/files/"+ulid+"?agent_id="+atAgent1, "acat_w1")
	if dl2 != http.StatusForbidden {
		t.Fatalf("post-soft-delete download status = %d, want 403", dl2)
	}
}
