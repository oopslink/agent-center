package teammemory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition/memory/centergit"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/team"
	teamservice "github.com/oopslink/agent-center/internal/team/service"
	teamsqlite "github.com/oopslink/agent-center/internal/team/sqlite"
)

func TestService_ReviewRequiresTeamMemoryCurator(t *testing.T) {
	ctx := context.Background()
	teams, host := newServiceFixture(t)
	tm, err := teams.CreateTeam(ctx, teamservice.CreateTeamInput{
		OrgID: "org-1",
		Name:  "memory-team",
		Roles: []team.RoleConfig{{Role: "dev", CLI: "codex", MaxConcurrency: 1}},
	})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if _, err := teams.AddMember(ctx, tm.ID(), "agent:agent-1", "dev"); err != nil {
		t.Fatalf("AddMember agent-1: %v", err)
	}
	if _, err := teams.AddMember(ctx, tm.ID(), "agent:agent-2", "dev"); err != nil {
		t.Fatalf("AddMember agent-2: %v", err)
	}

	git := centergit.NewTeamMemoryGit(host, nil,
		centergit.WithProposalIDGen(func() string { return "tmprop-01HZY000000000000000000010" }),
		centergit.WithCanonicalIDGen(func() string { return "01HZY000000000000000000011" }),
	)
	svc := NewService(teams, git, nil)
	member := Actor{OrgID: "org-1", MemberRef: "agent:agent-1"}
	curator := Actor{OrgID: "org-1", MemberRef: "agent:agent-2"}

	prop, err := svc.Propose(ctx, member, centergit.ProposeInput{
		IdempotencyKey: "task-7/lesson",
		Operation:      centergit.ProposalAdd,
		TargetKind:     centergit.ProposalTargetEntry,
		Candidate: &centergit.ProposalCandidate{
			Slug:        "prefer-small-tests",
			Description: "small tests isolate failures",
			Body:        "Prefer focused tests around the changed contract.",
		},
		Rationale: "the team repeatedly debugged broad failing tests",
		CreatedAt: time.Date(2026, 8, 9, 2, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}

	_, err = svc.Review(ctx, member, centergit.ReviewInput{
		ProposalID:             prop.ProposalID,
		Action:                 "reject",
		ExpectedRepoCommit:     prop.RepoCommit,
		ExpectedProposalStatus: centergit.ProposalPending,
		Comment:                "member should not be able to review",
	})
	if ReasonOf(err) != ReasonNotMemoryCurator {
		t.Fatalf("member review err=%v reason=%q, want %s", err, ReasonOf(err), ReasonNotMemoryCurator)
	}

	if _, err := teams.SetMemoryPolicy(ctx, tm.ID(), team.TeamMemoryPolicy{
		Mode:             team.TeamMemoryPolicyCuratorAuto,
		CuratorAgentRefs: []team.MemberRef{"agent:agent-2"},
	}); err != nil {
		t.Fatalf("SetMemoryPolicy: %v", err)
	}
	rejected, err := svc.Review(ctx, curator, centergit.ReviewInput{
		ProposalID:             prop.ProposalID,
		Action:                 "reject",
		ExpectedRepoCommit:     prop.RepoCommit,
		ExpectedProposalStatus: centergit.ProposalPending,
		Comment:                "Not broad enough for canonical memory.",
	})
	if err != nil {
		t.Fatalf("curator Review: %v", err)
	}
	if rejected.Status != centergit.ProposalRejected || rejected.ReviewerRef != "agent:agent-2" {
		t.Fatalf("bad rejected view: %+v", rejected)
	}
}

func TestService_SecretHardReject(t *testing.T) {
	ctx := context.Background()
	teams, host := newServiceFixture(t)
	tm, err := teams.CreateTeam(ctx, teamservice.CreateTeamInput{
		OrgID: "org-1",
		Name:  "memory-team",
		Roles: []team.RoleConfig{{Role: "dev", CLI: "codex", MaxConcurrency: 1}},
	})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if _, err := teams.AddMember(ctx, tm.ID(), "agent:agent-1", "dev"); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	svc := NewService(teams, centergit.NewTeamMemoryGit(host, nil), nil)
	_, err = svc.Propose(ctx, Actor{OrgID: "org-1", MemberRef: "agent:agent-1"}, centergit.ProposeInput{
		IdempotencyKey: "task-8/secret",
		Operation:      centergit.ProposalAdd,
		TargetKind:     centergit.ProposalTargetEntry,
		Candidate: &centergit.ProposalCandidate{
			Slug:        "bad-secret",
			Description: "must be rejected",
			Body:        "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----",
		},
		Rationale: "should not persist",
	})
	var tmerr *Error
	if !errors.As(err, &tmerr) || tmerr.Reason != ReasonSecretDetected {
		t.Fatalf("err=%v reason=%q, want %s", err, ReasonOf(err), ReasonSecretDetected)
	}
}

func newServiceFixture(t *testing.T) (*teamservice.Service, *centergit.Host) {
	t.Helper()
	db, err := persistence.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC))
	teams := teamservice.New(teamsqlite.NewRepo(db), db, idgen.NewGenerator(clk), clk)
	return teams, centergit.NewHost(t.TempDir(), nil)
}
