package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/usage"
	usagesql "github.com/oopslink/agent-center/internal/usage/sqlite"
)

// I28/F4: the per-agent analytics endpoint + decision-5 authz (owner/admin see
// any agent; a plain member sees only agents they created). These tests drive
// the real Analytics service over the test DB and exercise the authz matrix —
// especially the required negative: a member reading another's agent → 403.

// seedAnalyticsAgent creates a stopped agent owned by creatorIdentityID and
// returns its entity id (usable as the {id} path param; ResolveAgent accepts the
// raw entity id) and its canonical usage agent_ref ("agent:<entity-id>", since
// IdentityMemberID is left empty so agentFacingID falls back to the entity id).
func seedAnalyticsAgent(t *testing.T, deps HandlerDeps, orgID, creatorIdentityID, workerID string) (string, string) {
	t.Helper()
	id, err := deps.AgentSvc.CreateAgent(context.Background(), agentsvc.CreateAgentCommand{
		OrganizationID: orgID,
		Name:           "seed-" + workerID,
		Model:          "claude",
		CLI:            "claude-code",
		WorkerID:       workerID,
		CreatedBy:      agentpkg.IdentityRef("user:" + creatorIdentityID),
	})
	if err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	return string(id), "agent:" + string(id)
}

// seedMemberSession provisions a fresh identity with the given role in orgID and
// returns a session (cookie) for it, reusing the owner session's org slug.
func seedMemberSession(t *testing.T, db *sql.DB, owner testSession, name string, role identity.MemberRole) testSession {
	t.Helper()
	ctx := context.Background()
	hash, _ := identity.HashPasscode("123456")
	ident, err := identity.IdentityFactory{}.NewUser(name, hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.NewSQLiteIdentityRepo(db).Save(ctx, ident); err != nil {
		t.Fatal(err)
	}
	m, err := identity.MemberFactory{}.New(owner.OrgID, ident.ID(), role, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.NewSQLiteMemberRepo(db).Save(ctx, m); err != nil {
		t.Fatal(err)
	}
	jwt, err := identity.MintJWT(ident.ID(), testSigningKey)
	if err != nil {
		t.Fatal(err)
	}
	return testSession{
		IdentityID: ident.ID(), OrgID: owner.OrgID, OrgSlug: owner.OrgSlug,
		Cookie: &http.Cookie{Name: "ac_session", Value: jwt},
	}
}

func TestAPI_Analytics_F4_OwnerSeesAgentWithData(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.Analytics = usagesql.NewAnalytics(db)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-an")
	agentID, agentRef := seedAnalyticsAgent(t, deps, sess.OrgID, sess.IdentityID, "w-an")

	// Seed real usage on today (so the overview "today" card is non-zero) + a
	// task-scoped event so Top-Cost-Tasks has a row, then run the rollup.
	today := time.Now().UTC().Format("2006-01-02")
	at := today + "T10:00:00Z"
	tt, _ := time.Parse(time.RFC3339, at)
	er := usagesql.NewUsageEventRepo(db)
	ctx := context.Background()
	mustAppend(t, er, ctx, usage.UsageEvent{ID: "ue1", AgentRef: agentRef, ProjectID: "p1", TaskID: "task-Z", Model: "claude-opus-4-8",
		Tokens: usage.TokenCounts{Input: 100, Output: 50}, CostMicros: 900, TS: tt, Source: usage.SourceReport})
	mustAppend(t, er, ctx, usage.UsageEvent{ID: "ue2", AgentRef: agentRef, ProjectID: "p1", Model: "claude-opus-4-8",
		Tokens: usage.TokenCounts{Input: 20, Output: 10}, CostMicros: 100, TS: tt, Source: usage.SourceReport})
	if _, err := usagesql.NewRollup(db).RunIncremental(ctx); err != nil {
		t.Fatal(err)
	}

	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/agents/"+agentID+"/analytics", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner analytics: got %d, want 200", resp.StatusCode)
	}
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)

	if got["agent_ref"] != agentRef {
		t.Errorf("agent_ref = %v, want %s", got["agent_ref"], agentRef)
	}
	// heatmap cell for today with summed tokens (120 in).
	hm, _ := got["heatmap"].([]any)
	if len(hm) != 1 {
		t.Fatalf("heatmap = %v, want 1 cell", got["heatmap"])
	}
	cell := hm[0].(map[string]any)
	if cell["day"] != today || cell["tokens_in"].(float64) != 120 {
		t.Errorf("heatmap cell wrong: %+v", cell)
	}
	// overview.today reflects the same totals.
	ov := got["overview"].(map[string]any)
	tw := ov["today"].(map[string]any)
	if tw["tokens_in"].(float64) != 120 || tw["cost_micros"].(float64) != 1000 {
		t.Errorf("overview today wrong: %+v", tw)
	}
	// top_tasks: only the task-scoped event → task-Z, cost 900.
	tt2, _ := got["top_tasks"].([]any)
	if len(tt2) != 1 {
		t.Fatalf("top_tasks = %v, want 1", got["top_tasks"])
	}
	row := tt2[0].(map[string]any)
	if row["task_id"] != "task-Z" || row["cost_micros"].(float64) != 900 {
		t.Errorf("top task wrong: %+v", row)
	}
	// model trend present (one model).
	trends := got["trends"].(map[string]any)
	if len(trends["by_model"].([]any)) != 1 {
		t.Errorf("by_model = %v, want 1", trends["by_model"])
	}
}

func TestAPI_Analytics_F4_MemberCannotSeeOthersAgent(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.Analytics = usagesql.NewAnalytics(db)
	owner := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, owner.OrgID, "w-o")
	// agent created BY the owner.
	agentID, _ := seedAnalyticsAgent(t, deps, owner.OrgID, owner.IdentityID, "w-o")

	// a plain member of the same org.
	member := seedMemberSession(t, db, owner, "plainmember", identity.RoleMember)

	s := newTestServer(t, deps)
	defer s.Close()

	// REQUIRED NEGATIVE: member reading an agent they did not create → 403.
	resp := orgScopedGet(t, s.URL+"/api/agents/"+agentID+"/analytics", member)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member reading other's agent: got %d, want 403", resp.StatusCode)
	}
	// and the drill-down is gated the same way.
	resp = orgScopedGet(t, s.URL+"/api/agents/"+agentID+"/analytics/tasks/task-Z", member)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member drill-down on other's agent: got %d, want 403", resp.StatusCode)
	}
}

func TestAPI_Analytics_F4_MemberSeesOwnAgent(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.Analytics = usagesql.NewAnalytics(db)
	owner := setupTestSession(t, db, deps)
	member := seedMemberSession(t, db, owner, "owner-of-agent", identity.RoleMember)
	saveWorkerInOrg(t, db, owner.OrgID, "w-m")
	// agent created BY the member → the member may view it.
	agentID, _ := seedAnalyticsAgent(t, deps, owner.OrgID, member.IdentityID, "w-m")

	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/agents/"+agentID+"/analytics", member)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("member reading own agent: got %d, want 200", resp.StatusCode)
	}
}

func TestAPI_Analytics_F4_AdminSeesAnyAgent(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.Analytics = usagesql.NewAnalytics(db)
	owner := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, owner.OrgID, "w-a")
	agentID, _ := seedAnalyticsAgent(t, deps, owner.OrgID, owner.IdentityID, "w-a")
	admin := seedMemberSession(t, db, owner, "an-admin", identity.RoleAdmin)

	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/agents/"+agentID+"/analytics", admin)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin reading any agent: got %d, want 200", resp.StatusCode)
	}
}

func TestAPI_Analytics_F4_Unauthenticated(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.Analytics = usagesql.NewAnalytics(db)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-u")
	agentID, _ := seedAnalyticsAgent(t, deps, sess.OrgID, sess.IdentityID, "w-u")

	s := newTestServer(t, deps)
	defer s.Close()
	// no cookie → 401.
	url := orgScopedURL(s.URL+"/api/agents/"+agentID+"/analytics", sess.OrgSlug)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no cookie: got %d, want 401", resp.StatusCode)
	}
}

func TestAPI_Analytics_F4_NotFoundAndNotWired(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	deps.Analytics = usagesql.NewAnalytics(db)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	// unknown agent id → 404.
	resp := orgScopedGet(t, s.URL+"/api/agents/agent-does-not-exist/analytics", sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown agent: got %d, want 404", resp.StatusCode)
	}

	// Analytics not wired → 501 (guards the optional-dependency contract).
	deps2, db2 := setupAPIWithAuth(t)
	_ = db2
	sess2 := setupTestSession(t, db2, deps2)
	saveWorkerInOrg(t, db2, sess2.OrgID, "w-nw")
	agentID, _ := seedAnalyticsAgent(t, deps2, sess2.OrgID, sess2.IdentityID, "w-nw")
	deps2.Analytics = nil // explicitly unwired
	s2 := newTestServer(t, deps2)
	defer s2.Close()
	resp = orgScopedGet(t, s2.URL+"/api/agents/"+agentID+"/analytics", sess2)
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("not wired: got %d, want 501", resp.StatusCode)
	}
}

// --- small test plumbing ---

func mustAppend(t *testing.T, er *usagesql.UsageEventRepo, ctx context.Context, ev usage.UsageEvent) {
	t.Helper()
	if err := er.Append(ctx, ev); err != nil {
		t.Fatalf("append usage: %v", err)
	}
}
