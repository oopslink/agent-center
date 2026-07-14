package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/team"
	teamservice "github.com/oopslink/agent-center/internal/team/service"
	teamsql "github.com/oopslink/agent-center/internal/team/sqlite"
)

// refAllowlist is a MemberResolver test double: MemberExists is true only for
// refs it was seeded to accept (models "identity exists as a matching-kind org
// member"). Used to drive the handler's add-member reject/accept mapping without
// standing up the full identity directory.
type refAllowlist map[team.MemberRef]bool

func (a refAllowlist) MemberExists(_ context.Context, _ string, ref team.MemberRef) (bool, error) {
	return a[ref], nil
}

// TestAddMember_UnresolvableRef_404NotPersisted locks the end-to-end hardening at
// the web facade: an unresolvable member_ref → 404 identity_not_found and is NOT
// written to team_members (tester3's `agent:04c1…` pollution), while a resolvable
// ref still succeeds (contract tightened without breaking the happy path).
func TestAddMember_UnresolvableRef_404NotPersisted(t *testing.T) {
	deps, db, sess := setupTeamsAPI(t)
	// Wire a resolver that accepts only one known-good agent ref.
	good := team.MemberRef("agent:agent-good")
	deps.TeamService = teamservice.New(teamsql.NewRepo(db), db, idgen.NewGenerator(clock.SystemClock{}), clock.SystemClock{}).
		WithMemberResolver(refAllowlist{good: true})
	tm := seedTeam(t, deps, sess.OrgID, "Agent Core", implRole) // declares role "impl"
	ts := newTestServer(t, deps)
	defer ts.Close()

	base := ts.URL + "/api/teams/" + string(tm.ID()) + "/members"

	// (1) tester3's malformed/nonexistent ref → 404 identity_not_found.
	resp := orgScopedPost(t, base, `{"member_ref":"agent:04c1…","role":"impl"}`, sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unresolvable ref = %d, want 404", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	if body["error"] != "identity_not_found" {
		t.Errorf("error code = %v, want identity_not_found", body["error"])
	}

	// (2) NOT persisted — team_members untouched by the rejected ref.
	listResp := orgScopedGet(t, base, sess)
	if arr := decodeArray(t, listResp); len(arr) != 0 {
		t.Fatalf("rejected ref must not persist, members = %v", arr)
	}

	// (3) positive: a resolvable ref still adds (201).
	okResp := orgScopedPost(t, base, `{"member_ref":"agent:agent-good","role":"impl"}`, sess)
	if okResp.StatusCode != http.StatusCreated {
		t.Fatalf("resolvable ref = %d, want 201", okResp.StatusCode)
	}
	if arr := decodeArray(t, orgScopedGet(t, base, sess)); len(arr) != 1 {
		t.Fatalf("resolvable ref should persist, members = %v", arr)
	}
}

// TestAddMember_MigrateAcrossTeams locks the end-to-end agent migration flow at the
// web facade: POST with migrate_from atomically switches the agent's team (source
// emptied, destination gains it) WITHOUT tripping the exclusivity 409 — the live
// blocker tester3 found (migration silently 409'd because migrate_from was dropped).
func TestAddMember_MigrateAcrossTeams(t *testing.T) {
	deps, db, sess := setupTeamsAPI(t)
	agent := team.MemberRef("agent:agent-migrant")
	deps.TeamService = teamservice.New(teamsql.NewRepo(db), db, idgen.NewGenerator(clock.SystemClock{}), clock.SystemClock{}).
		WithMemberResolver(refAllowlist{agent: true})
	a := seedTeam(t, deps, sess.OrgID, "Team A", implRole)
	b := seedTeam(t, deps, sess.OrgID, "Team B", implRole)
	ts := newTestServer(t, deps)
	defer ts.Close()

	aBase := ts.URL + "/api/teams/" + string(a.ID()) + "/members"
	bBase := ts.URL + "/api/teams/" + string(b.ID()) + "/members"

	// seed the agent on team A.
	if resp := orgScopedPost(t, aBase, `{"member_ref":"agent:agent-migrant","role":"impl"}`, sess); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed add to A = %d, want 201", resp.StatusCode)
	}

	// migrate A → B (migrate_from set): must succeed, not 409.
	migBody := `{"member_ref":"agent:agent-migrant","role":"impl","migrate_from":"` + string(a.ID()) + `"}`
	migResp := orgScopedPost(t, bBase, migBody, sess)
	if migResp.StatusCode != http.StatusCreated {
		body := decodeBody(t, migResp)
		t.Fatalf("migration = %d (%v), want 201 (must not self-trip exclusivity)", migResp.StatusCode, body)
	}

	// source emptied, destination holds the agent.
	if arr := decodeArray(t, orgScopedGet(t, aBase, sess)); len(arr) != 0 {
		t.Fatalf("source team A must be empty after migration, got %v", arr)
	}
	if arr := decodeArray(t, orgScopedGet(t, bBase, sess)); len(arr) != 1 {
		t.Fatalf("dest team B must hold the migrated agent, got %v", arr)
	}
}
