package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	agentbc "github.com/oopslink/agent-center/internal/agent"
)

// v2.7 #157 [2/n]: POST /api/members/agent with execution fields (worker_id +
// model/cli) does the UNIFIED one-step create — agent identity + Member +
// execution Agent, atomically, linked (agent.IdentityMemberID == member.identity_id).
func TestAPI_AddAgentMember_UnifiedCreate(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"Bot","model":"claude","cli":"claudecode","worker_id":"w-1"}`, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("unified create: got %d, want 201", resp.StatusCode)
	}
	var res map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&res)
	agentID, _ := res["agent_id"].(string)
	identityID, _ := res["identity_id"].(string)
	if agentID == "" {
		t.Fatalf("unified create must return agent_id (execution agent created): %v", res)
	}
	if identityID == "" {
		t.Fatalf("missing identity_id: %v", res)
	}
	// The execution Agent must exist AND be linked to the agent identity-member.
	a, err := deps.AgentSvc.GetAgent(context.Background(), agentbc.AgentID(agentID))
	if err != nil {
		t.Fatalf("execution agent not found: %v", err)
	}
	if a.IdentityMemberID() != identityID {
		t.Fatalf("agent.IdentityMemberID = %q, want member identity_id %q (link broken)", a.IdentityMemberID(), identityID)
	}
}

// §-1 both-or-neither: if execution-agent create fails (worker not in org), the
// WHOLE transaction rolls back — NO orphan agent identity / Member is left.
func TestAPI_AddAgentMember_RollbackOnBadWorker(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	// NOTE: no saveWorkerInOrg — "w-ghost" does not exist in the org.
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedPost(t, s.URL+"/api/members/agent",
		`{"display_name":"Bot","model":"claude","cli":"claudecode","worker_id":"w-ghost"}`, sess)
	if resp.StatusCode < 400 || resp.StatusCode >= 500 {
		t.Fatalf("bad-worker unified create must be 4xx, got %d", resp.StatusCode)
	}
	// Rollback proof: no agent-kind Member was persisted in the org.
	members, err := deps.MemberRepo.ListByOrganization(context.Background(), sess.OrgID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range members {
		if len(m.IdentityID()) >= 6 && m.IdentityID()[:6] == "agent-" {
			t.Fatalf("orphan agent member left after rollback: %s", m.IdentityID())
		}
	}
}
