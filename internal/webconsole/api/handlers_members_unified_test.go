package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
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
		`{"display_name":"Bot","model":"claude","cli":"claude-code","worker_id":"w-1"}`, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("unified create: got %d, want 201", resp.StatusCode)
	}
	var res map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&res)
	// v2.7 #185: the response must NOT leak the execution-entity id; the agent's
	// business id is identity_id ("agent-<ulid>", == agent.IdentityMemberID).
	if _, leaked := res["agent_id"]; leaked {
		t.Fatalf("response must not expose the execution-entity agent_id: %v", res)
	}
	identityID, _ := res["identity_id"].(string)
	if identityID == "" {
		t.Fatalf("missing identity_id: %v", res)
	}
	// The execution Agent must exist, be resolvable by its member id (the bridge),
	// AND be linked to the agent identity-member.
	a, err := deps.AgentSvc.ResolveAgent(context.Background(), identityID)
	if err != nil {
		t.Fatalf("execution agent not resolvable by member id %q: %v", identityID, err)
	}
	if a.IdentityMemberID() != identityID {
		t.Fatalf("agent.IdentityMemberID = %q, want member identity_id %q (link broken)", a.IdentityMemberID(), identityID)
	}
}

// v2.7 #185 / no-middle-state: agent provision REQUIRES worker_id. Missing or
// blank worker_id → 400 worker_id_required, BEFORE any identity/member is
// provisioned (no orphan identity-only "agent" that can't run). The required
// worker_id is a v2.7 business-layer policy, not a domain invariant.
func TestAPI_AddAgentMember_WorkerIDRequired(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	for _, body := range []string{
		`{"display_name":"Bot","model":"claude","cli":"claude-code"}`,                   // worker_id absent
		`{"display_name":"Bot","model":"claude","cli":"claude-code","worker_id":"   "}`, // blank
	} {
		resp := orgScopedPost(t, s.URL+"/api/members/agent", body, sess)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("missing worker_id must be 400, got %d (body=%s)", resp.StatusCode, body)
		}
		var e map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e["error"] != "worker_id_required" {
			t.Fatalf("want error worker_id_required, got %v", e)
		}
	}

	// No agent-kind Member must have been provisioned (validated before the tx).
	members, err := deps.MemberRepo.ListByOrganization(context.Background(), sess.OrgID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range members {
		if len(m.IdentityID()) >= 6 && m.IdentityID()[:6] == "agent-" {
			t.Fatalf("no agent member should be provisioned on worker_id-missing reject: %s", m.IdentityID())
		}
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
		`{"display_name":"Bot","model":"claude","cli":"claude-code","worker_id":"w-ghost"}`, sess)
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
