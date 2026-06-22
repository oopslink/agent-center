package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// f6MkRunningTask creates a project + task and drives it to running so block/unblock
// are reachable, returning (projectID, taskID).
func f6MkRunningTask(t *testing.T, s string, sess testSession) (string, string) {
	t.Helper()
	presp := orgScopedPost(t, s+"/api/projects", `{"name":"P"}`, sess)
	if presp.StatusCode != http.StatusOK {
		t.Fatalf("create project status=%d", presp.StatusCode)
	}
	var pc map[string]any
	json.NewDecoder(presp.Body).Decode(&pc)
	pid := pc["id"].(string)

	tresp := orgScopedPost(t, s+"/api/projects/"+pid+"/tasks", `{"title":"do"}`, sess)
	if tresp.StatusCode != http.StatusOK {
		t.Fatalf("create task status=%d", tresp.StatusCode)
	}
	var tc map[string]any
	json.NewDecoder(tresp.Body).Decode(&tc)
	tid := tc["id"].(string)

	rresp := orgScopedPost(t, s+"/api/projects/"+pid+"/tasks/"+tid+"/status", `{"status":"running"}`, sess)
	if rresp.StatusCode != http.StatusOK {
		t.Fatalf("set running status=%d", rresp.StatusCode)
	}
	return pid, tid
}

// TestPM_UnblockObstacle_HTTP is the F6 obstacle path: a task blocked via the
// /block endpoint (obstacle) is unblocked via POST /unblock with a `comment`,
// returning 200 + the cleared-block task DTO.
func TestPM_UnblockObstacle_HTTP(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	pid, tid := f6MkRunningTask(t, s.URL, sess)

	bresp := orgScopedPost(t, s.URL+"/api/projects/"+pid+"/tasks/"+tid+"/block", `{"reason":"needs a prod key"}`, sess)
	if bresp.StatusCode != http.StatusOK {
		t.Fatalf("block status=%d", bresp.StatusCode)
	}

	uresp := orgScopedPost(t, s.URL+"/api/projects/"+pid+"/tasks/"+tid+"/unblock", `{"comment":"key added"}`, sess)
	if uresp.StatusCode != http.StatusOK {
		t.Fatalf("unblock status=%d", uresp.StatusCode)
	}
	tk, err := deps.PM.GetTask(context.Background(), pm.TaskID(tid))
	if err != nil {
		t.Fatal(err)
	}
	if tk.BlockedReason() != "" {
		t.Fatalf("unblock must clear the block, got %q", tk.BlockedReason())
	}
	if tk.BlockedComment() != "key added" {
		t.Fatalf("unblock must record the comment, got %q", tk.BlockedComment())
	}
}

// TestPM_UnblockInputRequired_HTTP is the F6 input_required path: a task blocked
// with reasonType=input_required (via the service, the agent path) is unblocked
// via POST /unblock carrying input_request_message_id + comment, returning 200 +
// the cleared-block task DTO.
func TestPM_UnblockInputRequired_HTTP(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	pid, tid := f6MkRunningTask(t, s.URL, sess)

	// Block with reasonType=input_required directly via the service (the agent path);
	// the HTTP /block endpoint only emits obstacle. Actor is the session owner.
	actor := pm.IdentityRef("user:" + sess.IdentityID)
	if err := deps.PM.BlockTask(context.Background(), pm.TaskID(tid), "which branch?", pm.BlockReasonInputRequired, actor); err != nil {
		t.Fatal(err)
	}

	uresp := orgScopedPost(t, s.URL+"/api/projects/"+pid+"/tasks/"+tid+"/unblock",
		`{"input_request_message_id":"01J0000000REQUEST0000000","comment":"use main"}`, sess)
	if uresp.StatusCode != http.StatusOK {
		t.Fatalf("unblock status=%d", uresp.StatusCode)
	}
	tk, err := deps.PM.GetTask(context.Background(), pm.TaskID(tid))
	if err != nil {
		t.Fatal(err)
	}
	if tk.BlockedReason() != "" || tk.BlockedReasonType() != "" {
		t.Fatalf("unblock must clear reason+type, got %q/%q", tk.BlockedReason(), tk.BlockedReasonType())
	}
	if tk.BlockedComment() != "use main" {
		t.Fatalf("unblock must record the comment, got %q", tk.BlockedComment())
	}
}
