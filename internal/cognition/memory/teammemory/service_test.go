package teammemory

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestProposalStateMachineTerminal(t *testing.T) {
	p := &Proposal{ProposalID: "tmprop-1", Status: StatusPending}
	if err := p.Promote("agent:curator", "ok", "c1", time.Unix(1, 0)); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if p.Status != StatusPromoted || p.ReviewerRef != "agent:curator" || p.PromotionCommit != "c1" {
		t.Fatalf("bad promoted proposal: %+v", p)
	}
	if err := p.Reject("agent:curator", "late", time.Unix(2, 0)); !errors.Is(err, ErrProposalNotPending) {
		t.Fatalf("terminal reject err=%v want proposal_not_pending", err)
	}
}

func TestServiceAuthorizesProposeAndReview(t *testing.T) {
	ctx := context.Background()
	repo := &fakeRepo{}
	auth := &fakeAuth{
		teams: map[string]string{"agent:member": "team-1", "agent:curator": "team-1"},
		members: map[string]map[string]bool{
			"team-1": {"agent:member": true, "agent:curator": true},
		},
		reviewers: map[string]map[string]bool{
			"team-1": {"agent:curator": true},
		},
	}
	svc := NewService(repo, auth)

	if _, err := svc.Propose(ctx, ProposeCommand{ActorRef: "agent:member", IdempotencyKey: "k"}); err != nil {
		t.Fatalf("member propose: %v", err)
	}
	if repo.proposedTeam != "team-1" {
		t.Fatalf("proposed team=%q", repo.proposedTeam)
	}
	if _, err := svc.Review(ctx, "team-1", ReviewCommand{ActorRef: "agent:member"}); !errors.Is(err, ErrNotMemoryCurator) {
		t.Fatalf("member review err=%v want not_memory_curator", err)
	}
	if _, err := svc.Review(ctx, "team-1", ReviewCommand{ActorRef: "agent:curator"}); err != nil {
		t.Fatalf("curator review: %v", err)
	}
	auth.reviewers["team-1"]["agent:curator"] = false
	if _, err := svc.Review(ctx, "team-1", ReviewCommand{ActorRef: "agent:curator"}); !errors.Is(err, ErrNotMemoryCurator) {
		t.Fatalf("revoked curator err=%v want not_memory_curator", err)
	}
}

type fakeRepo struct {
	proposedTeam string
}

func (f *fakeRepo) Propose(_ context.Context, teamID string, _ ProposeCommand) (Result, error) {
	f.proposedTeam = teamID
	return Result{TeamID: teamID, ProposalID: "tmprop-1", Status: StatusPending}, nil
}

func (f *fakeRepo) List(_ context.Context, teamID string, _ Filter) (ListResult, error) {
	return ListResult{TeamID: teamID}, nil
}

func (f *fakeRepo) Get(_ context.Context, teamID, proposalID string) (ProposalView, error) {
	return ProposalView{Proposal: Proposal{TeamID: teamID, ProposalID: proposalID}}, nil
}

func (f *fakeRepo) Review(_ context.Context, teamID string, _ ReviewCommand) (Result, error) {
	return Result{TeamID: teamID, ProposalID: "tmprop-1", Status: StatusPromoted}, nil
}

type fakeAuth struct {
	teams     map[string]string
	members   map[string]map[string]bool
	reviewers map[string]map[string]bool
}

func (f *fakeAuth) ResolveActorTeam(_ context.Context, actorRef string) (string, bool, error) {
	teamID, ok := f.teams[actorRef]
	return teamID, ok, nil
}

func (f *fakeAuth) CanPropose(_ context.Context, teamID, actorRef string) (bool, error) {
	return f.members[teamID][actorRef], nil
}

func (f *fakeAuth) CanRead(_ context.Context, teamID, actorRef string) (bool, error) {
	return f.members[teamID][actorRef], nil
}

func (f *fakeAuth) CanReview(_ context.Context, teamID, actorRef string) (bool, error) {
	return f.reviewers[teamID][actorRef], nil
}
