package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// v2.8 #272 (archived)-chip data: enrichAssignee exposes assignee_lifecycle for
// an agent assignee (so the UI renders "(archived)") and leaves it "" for a user
// assignee. Closes the "(archived) chip data" gate: ref-retention is necessary
// but not sufficient — the DTO must expose the lifecycle.
func TestEnrichAssignee_AssigneeLifecycle_272(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	srv := newTestServer(t, deps)
	defer srv.Close()
	memberID := createAgentViaAPI(t, srv, sess, "w-1") // fresh agent = stopped

	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	ref := "agent:" + memberID

	// Agent assignee → lifecycle present (fresh = stopped).
	got := s.enrichAssignee(req, deps, ref)
	if got == nil {
		t.Fatal("agent assignee should enrich non-nil")
	}
	if got["assignee_lifecycle"] != "stopped" {
		t.Fatalf("assignee_lifecycle=%v want stopped", got["assignee_lifecycle"])
	}

	// Archive the agent → lifecycle="archived" (the (archived) chip source).
	a, err := deps.AgentSvc.ResolveAgent(context.Background(), memberID)
	if err != nil {
		t.Fatal(err)
	}
	if err := deps.AgentSvc.ArchiveAgent(context.Background(), a.ID()); err != nil {
		t.Fatal(err)
	}
	got = s.enrichAssignee(req, deps, ref)
	if got["assignee_lifecycle"] != "archived" {
		t.Fatalf("after archive assignee_lifecycle=%v want archived", got["assignee_lifecycle"])
	}
	// ref + member_id preserved across archive (assignee history points unchanged).
	if got["ref"] != ref || got["member_id"] != memberID {
		t.Fatalf("assignee ref/member_id must persist after archive: %v", got)
	}

	// User assignee → no lifecycle ("").
	uref := "user:" + sess.IdentityID
	gu := s.enrichAssignee(req, deps, uref)
	if gu["assignee_lifecycle"] != "" {
		t.Fatalf("user assignee_lifecycle=%v want empty", gu["assignee_lifecycle"])
	}
}
