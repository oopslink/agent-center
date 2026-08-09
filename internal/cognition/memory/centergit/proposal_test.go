package centergit

import (
	"context"
	"testing"
	"time"
)

func TestTeamMemoryProposal_PromotionOnlyAffectsFutureRuleReads(t *testing.T) {
	host := NewHost(t.TempDir(), nil)
	git := NewTeamMemoryGit(host, nil,
		WithProposalIDGen(func() string { return "tmprop-01HZY000000000000000000000" }),
		WithCanonicalIDGen(func() string { return "01HZY000000000000000000001" }),
	)
	ctx := context.Background()
	enabled := true
	proposed, err := git.Propose(ctx, "team-rules", ProposeInput{
		ActorRef:       "agent:agent-1",
		IdempotencyKey: "task-1/rule",
		Operation:      ProposalAdd,
		TargetKind:     ProposalTargetRule,
		Candidate: &ProposalCandidate{
			Slug:        "review-rigor",
			Description: "review with rigor",
			Body:        "Check edge cases before approving.",
			Enabled:     &enabled,
			AppliesTo:   []string{"review"},
		},
		Rationale: "review misses repeated edge cases",
		CreatedAt: time.Date(2026, 8, 9, 1, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}

	before, err := NewTeamMemoryConsumer(host, nil).ReadTeamRules(ctx, "team-rules", "review")
	if err != nil {
		t.Fatalf("ReadTeamRules before: %v", err)
	}
	if len(before.Rules) != 0 {
		t.Fatalf("proposal leaked into rules before promotion: %+v", before.Rules)
	}

	promoted, err := git.Review(ctx, "team-rules", ReviewInput{
		ActorRef:               "agent:curator",
		ProposalID:             proposed.ProposalID,
		Action:                 "promote",
		ExpectedRepoCommit:     proposed.RepoCommit,
		ExpectedProposalStatus: ProposalPending,
		Comment:                "Accepted for future review runs.",
		ReviewedAt:             time.Date(2026, 8, 9, 1, 5, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Review promote: %v", err)
	}
	if promoted.Status != ProposalPromoted || promoted.RepoCommit == proposed.RepoCommit {
		t.Fatalf("bad promotion metadata: before=%s after=%s status=%s", proposed.RepoCommit, promoted.RepoCommit, promoted.Status)
	}

	after, err := NewTeamMemoryConsumer(host, nil).ReadTeamRules(ctx, "team-rules", "review")
	if err != nil {
		t.Fatalf("ReadTeamRules after: %v", err)
	}
	if len(after.Rules) != 1 || after.Rules[0].Slug != "review-rigor" {
		t.Fatalf("promoted rules = %+v, want review-rigor", after.Rules)
	}
	if before.Commit == "" || after.Commit == "" || before.Commit == after.Commit {
		t.Fatalf("rule snapshots should be commit-distinct before=%q after=%q", before.Commit, after.Commit)
	}
}
