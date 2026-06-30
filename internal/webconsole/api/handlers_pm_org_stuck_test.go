package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TestOrgTasks_BlockedReasonType_HTTP asserts the org-scoped tasks aggregation
// (GET /api/tasks) AND the single-task DTO expose blocked_reason +
// blocked_reason_type. The global "stuck" Alerts rail (useStuckTasks) reads the
// aggregation to surface tasks waiting on the user and classify them
// (input_required vs obstacle), so the type must be on the wire.
func TestOrgTasks_BlockedReasonType_HTTP(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	pid, tid := f6MkRunningTask(t, s.URL, sess)
	actor := pm.IdentityRef("user:" + sess.IdentityID)
	// Block input_required via the service (the agent path; the HTTP /block endpoint
	// only emits obstacle).
	if err := deps.PM.BlockTask(context.Background(), pm.TaskID(tid), "which branch?", pm.BlockReasonInputRequired, actor); err != nil {
		t.Fatal(err)
	}

	// Org aggregation row carries the block annotation + its classification.
	var row map[string]any
	for _, it := range decodeItems(t, orgScopedGet(t, s.URL+"/api/tasks", sess)) {
		if it["id"] == tid {
			row = it
		}
	}
	if row == nil {
		t.Fatalf("blocked task %s not found in org tasks aggregation", tid)
	}
	if row["blocked_reason"] != "which branch?" {
		t.Fatalf("org row blocked_reason=%v, want %q", row["blocked_reason"], "which branch?")
	}
	if row["blocked_reason_type"] != string(pm.BlockReasonInputRequired) {
		t.Fatalf("org row blocked_reason_type=%v, want input_required", row["blocked_reason_type"])
	}

	// Single-task DTO (the alert deep-link target) also carries the type.
	resp := orgScopedGet(t, s.URL+"/api/projects/"+pid+"/tasks/"+tid, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get task status=%d", resp.StatusCode)
	}
	var tk map[string]any
	json.NewDecoder(resp.Body).Decode(&tk)
	if tk["blocked_reason_type"] != string(pm.BlockReasonInputRequired) {
		t.Fatalf("task DTO blocked_reason_type=%v, want input_required", tk["blocked_reason_type"])
	}
}
