package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"testing"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/team"
	teamservice "github.com/oopslink/agent-center/internal/team/service"
	teamsql "github.com/oopslink/agent-center/internal/team/sqlite"
)

// seedAgentIdentity provisions an agent identity + org membership and returns its
// canonical member ref ("agent:<id>").
func seedAgentIdentity(t *testing.T, db *sql.DB, orgID, name string) (string, team.MemberRef) {
	t.Helper()
	ctx := context.Background()
	idRepo := identity.NewSQLiteIdentityRepo(db)
	memberRepo := identity.NewSQLiteMemberRepo(db)
	ident, err := identity.IdentityFactory{}.NewAgent(name, "seeded agent")
	if err != nil {
		t.Fatalf("new agent identity: %v", err)
	}
	if err := idRepo.Save(ctx, ident); err != nil {
		t.Fatalf("save agent identity: %v", err)
	}
	m, err := identity.MemberFactory{}.New(orgID, ident.ID(), identity.RoleMember, nil)
	if err != nil {
		t.Fatalf("new agent member: %v", err)
	}
	if err := memberRepo.Save(ctx, m); err != nil {
		t.Fatalf("save agent member: %v", err)
	}
	return ident.ID(), team.MemberRef("agent:" + ident.ID())
}

// spyTeamRepo wraps a real team.Repository to count the membership reads the
// directory rollup performs, and to inject read failures. The embedded interface
// forwards everything we do not override.
type spyTeamRepo struct {
	team.Repository
	perTeamReads int // ListMembers(one team) calls — the N+1 signature
	batchReads   int // ListMembersByTeams(all teams) calls — the batched read
	batchErr     error
}

func (s *spyTeamRepo) ListMembers(ctx context.Context, id team.TeamID) ([]*team.TeamMember, error) {
	s.perTeamReads++
	return s.Repository.ListMembers(ctx, id)
}

func (s *spyTeamRepo) ListMembersByTeams(ctx context.Context, ids []team.TeamID) ([]*team.TeamMember, error) {
	s.batchReads++
	if s.batchErr != nil {
		return nil, s.batchErr
	}
	return s.Repository.ListMembersByTeams(ctx, ids)
}

// TestDirectoryAgents_CanonicalTeamIDs locks that the directory exposes the
// CANONICAL team id (team_ids), not just the display name, and that two teams do
// not cross-contaminate each other's rollup.
//
// Why ids and not names: the add-member modal builds the agent-exclusivity
// `migrate_from` from this field. Keying that off the NAME forces a name→id
// reverse lookup against a separately-fetched teams list, so a team renamed
// between the two fetches resolves to the wrong id (or to none) — i.e. the
// confirm dialog names one team while the write path moves the agent out of
// another. team_ids removes the lookup entirely.
//
// Note: a same-org duplicate NAME is not reachable here — idx_teams_org_name is
// UNIQUE(org_id, name) and CreateTeam rejects the collision with
// ErrTeamNameTaken — so the cross-contamination arm below uses distinct names.
func TestDirectoryAgents_CanonicalTeamIDs(t *testing.T) {
	deps, db, sess := setupTeamsAPI(t)
	ctx := context.Background()

	core := seedTeam(t, deps, sess.OrgID, "Agent Core", implRole)
	edge := seedTeam(t, deps, sess.OrgID, "Agent Edge", implRole)

	adaID, adaRef := seedAgentIdentity(t, db, sess.OrgID, "Ada")
	bobID, bobRef := seedAgentIdentity(t, db, sess.OrgID, "Bob")
	if _, err := deps.TeamService.AddMember(ctx, core.ID(), adaRef, "impl"); err != nil {
		t.Fatalf("add Ada to core: %v", err)
	}
	if _, err := deps.TeamService.AddMember(ctx, edge.ID(), bobRef, "impl"); err != nil {
		t.Fatalf("add Bob to edge: %v", err)
	}

	ts := newTestServer(t, deps)
	defer ts.Close()

	resp := orgScopedGet(t, ts.URL+"/api/directory/agents", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("agents = %d, want 200", resp.StatusCode)
	}
	byID := map[string]map[string]any{}
	for _, e := range decodeArray(t, resp) {
		m := e.(map[string]any)
		byID[m["ref"].(string)] = m
	}

	for _, c := range []struct{ ref, wantID, wantName string }{
		{"agent:" + adaID, string(core.ID()), "Agent Core"},
		{"agent:" + bobID, string(edge.ID()), "Agent Edge"},
	} {
		got, ok := byID[c.ref]
		if !ok {
			t.Fatalf("%s missing from directory", c.ref)
		}
		ids, ok := got["team_ids"].([]any)
		if !ok {
			t.Fatalf("%s: team_ids = %#v, want []any (canonical team ids)", c.ref, got["team_ids"])
		}
		if len(ids) != 1 || ids[0] != c.wantID {
			t.Errorf("%s: team_ids = %v, want [%s]", c.ref, ids, c.wantID)
		}
		// teams (names) stays for display — the TEAMS column renders it.
		names := got["teams"].([]any)
		if len(names) != 1 || names[0] != c.wantName {
			t.Errorf("%s: teams = %v, want [%s]", c.ref, names, c.wantName)
		}
	}
}

// TestDirectoryHumans_CanonicalTeamIDs is the humans-side counterpart: a human may
// be on many teams, so team_ids must carry every one, positionally aligned with
// teams.
func TestDirectoryHumans_CanonicalTeamIDs(t *testing.T) {
	deps, _, sess := setupTeamsAPI(t)
	ctx := context.Background()
	core := seedTeam(t, deps, sess.OrgID, "Agent Core", implRole)
	edge := seedTeam(t, deps, sess.OrgID, "Agent Edge", implRole)
	ref := team.MemberRef("user:" + sess.IdentityID)
	if _, err := deps.TeamService.AddMember(ctx, core.ID(), ref, "impl"); err != nil {
		t.Fatalf("add to core: %v", err)
	}
	if _, err := deps.TeamService.AddMember(ctx, edge.ID(), ref, "impl"); err != nil {
		t.Fatalf("add to edge: %v", err)
	}

	ts := newTestServer(t, deps)
	defer ts.Close()

	resp := orgScopedGet(t, ts.URL+"/api/directory/humans", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("humans = %d, want 200", resp.StatusCode)
	}
	arr := decodeArray(t, resp)
	if len(arr) != 1 {
		t.Fatalf("humans = %d, want 1", len(arr))
	}
	h := arr[0].(map[string]any)
	ids, ok := h["team_ids"].([]any)
	if !ok {
		t.Fatalf("team_ids = %#v, want []any", h["team_ids"])
	}
	want := map[string]bool{string(core.ID()): true, string(edge.ID()): true}
	if len(ids) != 2 {
		t.Fatalf("team_ids = %v, want 2 canonical ids", ids)
	}
	for _, id := range ids {
		if !want[id.(string)] {
			t.Errorf("team_ids contains %v, not a canonical team id", id)
		}
	}
	if len(h["teams"].([]any)) != len(ids) {
		t.Errorf("teams %v and team_ids %v must be positionally aligned", h["teams"], ids)
	}
}

// TestDirectory_MembershipReadFailurePropagates locks the rollup's error contract:
// a failed membership read must surface as an ERROR, never as a successful
// response that renders the agent as team-less.
//
// Why this is a correctness bug and not just a UX nit: "no team" is exactly the
// signal the add-member modal uses to decide that NO migration confirm is needed.
// Silently degrading an unreadable rollup to "no team" therefore moves an agent
// out of its real team without ever showing the second confirm.
func TestDirectory_MembershipReadFailurePropagates(t *testing.T) {
	for _, path := range []string{"/api/directory/agents", "/api/directory/humans"} {
		t.Run(path, func(t *testing.T) {
			deps, db, sess := setupTeamsAPI(t)
			ctx := context.Background()
			tm := seedTeam(t, deps, sess.OrgID, "Agent Core", implRole)
			if _, err := deps.TeamService.AddMember(ctx, tm.ID(), team.MemberRef("user:"+sess.IdentityID), "impl"); err != nil {
				t.Fatalf("seed member: %v", err)
			}
			// re-wire the service over a repo whose membership read fails.
			spy := &spyTeamRepo{Repository: teamsql.NewRepo(db), batchErr: errors.New("boom")}
			deps.TeamService = teamservice.New(spy, db, idgen.NewGenerator(clock.SystemClock{}), clock.SystemClock{})

			ts := newTestServer(t, deps)
			defer ts.Close()

			resp := orgScopedGet(t, ts.URL+path, sess)
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				t.Fatalf("%s with a failing membership read = 200; an unreadable rollup must not be served as 'no team'", path)
			}
			if resp.StatusCode != http.StatusInternalServerError {
				t.Errorf("%s = %d, want 500", path, resp.StatusCode)
			}
		})
	}
}

// TestDirectory_MembershipRollupIsBatched guards the rollup against regressing to
// the per-team read loop (N+1): the whole org's membership must come from ONE
// batched read regardless of team count.
func TestDirectory_MembershipRollupIsBatched(t *testing.T) {
	deps, db, sess := setupTeamsAPI(t)
	ctx := context.Background()
	for _, name := range []string{"Team A", "Team B", "Team C"} {
		tm := seedTeam(t, deps, sess.OrgID, name, implRole)
		_, ref := seedAgentIdentity(t, db, sess.OrgID, name+" agent")
		if _, err := deps.TeamService.AddMember(ctx, tm.ID(), ref, "impl"); err != nil {
			t.Fatalf("seed member: %v", err)
		}
	}
	spy := &spyTeamRepo{Repository: teamsql.NewRepo(db)}
	deps.TeamService = teamservice.New(spy, db, idgen.NewGenerator(clock.SystemClock{}), clock.SystemClock{})

	ts := newTestServer(t, deps)
	defer ts.Close()

	resp := orgScopedGet(t, ts.URL+"/api/directory/agents", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("agents = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	if spy.perTeamReads != 0 {
		t.Errorf("per-team ListMembers = %d, want 0 (N+1 regressed: the rollup must batch)", spy.perTeamReads)
	}
	if spy.batchReads != 1 {
		t.Errorf("batched ListMembersByTeams = %d, want exactly 1", spy.batchReads)
	}
}
