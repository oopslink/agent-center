package cli

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/team"
	teamservice "github.com/oopslink/agent-center/internal/team/service"
)

// openMigratedDB opens a fresh migrated sqlite DB for a test.
func openMigratedDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := persistence.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatalf("Up: %v", err)
	}
	return db
}

// seedIdentityMember saves an identity and a joined org member for it, returning
// the identity id.
func seedIdentityMember(t *testing.T, ids identity.IdentityRepository, members identity.MemberRepository, ident *identity.Identity, orgID string) string {
	t.Helper()
	ctx := context.Background()
	if err := ids.Save(ctx, ident); err != nil {
		t.Fatalf("save identity: %v", err)
	}
	m, err := identity.MemberFactory{}.New(orgID, ident.ID(), identity.RoleMember, nil)
	if err != nil {
		t.Fatalf("new member: %v", err)
	}
	if err := members.Save(ctx, m); err != nil {
		t.Fatalf("save member: %v", err)
	}
	return ident.ID()
}

// TestIdentityMemberResolver_Checks locks the three write-path invariants of the
// concrete resolver against real identity repos: existence-in-org, kind match,
// and org scoping — plus the exact tester3 pollution case (nonexistent ref).
func TestIdentityMemberResolver_Checks(t *testing.T) {
	db := openMigratedDB(t)
	ids := identity.NewSQLiteIdentityRepo(db)
	members := identity.NewSQLiteMemberRepo(db)
	resolver := newIdentityMemberResolver(ids, members)
	if resolver == nil {
		t.Fatal("resolver should be non-nil when both repos wired")
	}
	ctx := context.Background()
	const orgA, orgB = "org-A", "org-B"

	agent, err := identity.IdentityFactory{}.NewAgent("Ada", "backend agent")
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	agentID := seedIdentityMember(t, ids, members, agent, orgA)

	human, err := identity.IdentityFactory{}.NewUser("jane", "hash")
	if err != nil {
		t.Fatalf("new user: %v", err)
	}
	humanID := seedIdentityMember(t, ids, members, human, orgA)

	cases := []struct {
		name string
		org  string
		ref  team.MemberRef
		want bool
	}{
		{"valid agent", orgA, team.MemberRef("agent:" + agentID), true},
		{"valid human", orgA, team.MemberRef("user:" + humanID), true},
		{"nonexistent ref (tester3 pollution)", orgA, team.MemberRef("agent:04c1…"), false},
		{"cross-org agent", orgB, team.MemberRef("agent:" + agentID), false},
		{"kind mismatch: user prefix on agent id", orgA, team.MemberRef("user:" + agentID), false},
		{"kind mismatch: agent prefix on user id", orgA, team.MemberRef("agent:" + humanID), false},
		{"malformed ref (no prefix)", orgA, team.MemberRef("bogus"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolver.MemberExists(ctx, tc.org, tc.ref)
			if err != nil {
				t.Fatalf("MemberExists: %v", err)
			}
			if got != tc.want {
				t.Errorf("MemberExists(%q, %q) = %v, want %v", tc.org, tc.ref, got, tc.want)
			}
		})
	}
}

// TestNewIdentityMemberResolver_NilWhenUnwired locks the degrade contract: an
// unwired repo yields a nil resolver so the team service skips the check rather
// than failing closed.
func TestNewIdentityMemberResolver_NilWhenUnwired(t *testing.T) {
	if r := newIdentityMemberResolver(nil, nil); r != nil {
		t.Errorf("nil repos should yield nil resolver, got %v", r)
	}
}

// TestNewHardenedTeamService_WiresResolver is the composition-root lock: the SINGLE
// Team-service constructor every wiring site shares must actually wire the resolver
// so add-member is hardened. This is what a handler test that self-injects a
// resolver cannot prove — it catches the "web facade constructed a bare service
// with resolver==nil" gap (dead 404 mapping) directly at the constructor, so no
// wiring site can silently drop the check.
func TestNewHardenedTeamService_WiresResolver(t *testing.T) {
	db := openMigratedDB(t)
	ids := identity.NewSQLiteIdentityRepo(db)
	members := identity.NewSQLiteMemberRepo(db)
	app := &App{
		DB:                 db,
		Clock:              clock.SystemClock{},
		IDGen:              idgen.NewGenerator(clock.SystemClock{}),
		IdentityRepo:       ids,
		IdentityMemberRepo: members,
	}
	svc := newHardenedTeamService(app)
	ctx := context.Background()
	const orgID = "org-hardened"

	agent, err := identity.IdentityFactory{}.NewAgent("Ada", "")
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	agentID := seedIdentityMember(t, ids, members, agent, orgID)

	tm, err := svc.CreateTeam(ctx, teamservice.CreateTeamInput{
		OrgID: orgID, Name: "Squad",
		Roles: []team.RoleConfig{{Role: "dev", CLI: "claude-code", MaxConcurrency: 1}},
	})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	// Dangling ref → rejected: proves the resolver is actually wired on the
	// production constructor path (not resolver==nil pass-through).
	if _, err := svc.AddMember(ctx, tm.ID(), team.MemberRef("agent:agent-nonexistent"), "dev"); !errors.Is(err, team.ErrMemberIdentityNotFound) {
		t.Fatalf("hardened service must reject dangling ref, got %v", err)
	}
	// Real org member → accepted: hardening does not misfire on the happy path.
	if _, err := svc.AddMember(ctx, tm.ID(), team.MemberRef("agent:"+agentID), "dev"); err != nil {
		t.Fatalf("hardened service must accept a real org member, got %v", err)
	}
}
