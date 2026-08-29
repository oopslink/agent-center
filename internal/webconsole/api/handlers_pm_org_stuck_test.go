package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func TestOrgTasks_FailedReason_HTTP(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	pid, tid := f6MkRunningTask(t, s.URL, sess)
	actor := pm.IdentityRef("user:" + sess.IdentityID)
	if err := deps.PM.FailTask(context.Background(), pm.TaskID(tid), "which branch?", actor); err != nil {
		t.Fatal(err)
	}

	// Org aggregation row carries the terminal failure reason.
	var row map[string]any
	for _, it := range decodeItems(t, orgScopedGet(t, s.URL+"/api/tasks?status=all", sess)) {
		if it["id"] == tid {
			row = it
		}
	}
	if row == nil {
		t.Fatalf("failed task %s not found in org tasks aggregation", tid)
	}
	if row["status"] != "failed" || row["failed_reason"] != "which branch?" {
		t.Fatalf("org row failed status/reason=%v/%v", row["status"], row["failed_reason"])
	}

	// Single-task DTO also carries the failure reason.
	resp := orgScopedGet(t, s.URL+"/api/projects/"+pid+"/tasks/"+tid, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get task status=%d", resp.StatusCode)
	}
	var tk map[string]any
	json.NewDecoder(resp.Body).Decode(&tk)
	if tk["status"] != "failed" || tk["failed_reason"] != "which branch?" {
		t.Fatalf("task DTO failed status/reason=%v/%v", tk["status"], tk["failed_reason"])
	}
}
