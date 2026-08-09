package centergit

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newTestTeamMemoryService(t *testing.T, ids ...string) (*TeamMemoryService, *Host) {
	t.Helper()
	host := NewHost(t.TempDir(), nil)
	svc := NewTeamMemoryService(host, nil,
		WithTeamMemoryIDGen(mustDeterministicIDs(ids...)),
		WithTeamMemoryClock(func() time.Time {
			return time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
		}),
	)
	return svc, host
}

func TestTeamMemoryService_ProposalPromoteEntry(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestTeamMemoryService(t, "01HXPROPOSAL", "01HXUUID", "01HXTARGET")
	author := Author{Name: "Owner", Email: "owner@example.test"}

	created, err := svc.CreateProposal(ctx, "team-alpha", CreateMemoryProposalInput{
		TargetKind:          MemoryItemEntry,
		Slug:                "ci-runbook",
		Title:               "CI runbook",
		Description:         "How this team handles CI",
		Body:                "Use the shared pipeline.",
		WarningAcknowledged: true,
		AuthorRef:           "user:user-owner",
		Author:              author,
	})
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	if created.ID != "proposal-01hxproposal" || created.Status != ProposalStatusPending {
		t.Fatalf("created = %#v", created)
	}
	if created.SourcePath != "proposals/proposal-01hxproposal.md" || created.UUID != "01HXUUID" || created.Commit == "" {
		t.Fatalf("created metadata = %#v", created)
	}
	if created.Diff == "" || created.WarningAcknowledged != true {
		t.Fatalf("created diff/ack missing: %#v", created)
	}

	promoted, err := svc.PromoteProposal(ctx, "team-alpha", PromoteMemoryProposalInput{
		ProposalID: created.ID,
		ActorRef:   "user:user-owner",
		Author:     author,
	})
	if err != nil {
		t.Fatalf("PromoteProposal: %v", err)
	}
	if promoted.Status != ProposalStatusPromoted || promoted.PromotedPath == "" || promoted.TargetUUID == "" {
		t.Fatalf("promoted = %#v", promoted)
	}

	snap, err := svc.List(ctx, "team-alpha")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if snap.Commit == "" || snap.EffectHint == "" {
		t.Fatalf("snapshot metadata missing: %#v", snap)
	}
	if len(snap.Entries) != 1 || snap.Entries[0].Slug != "ci-runbook" || snap.Entries[0].UUID == "" {
		t.Fatalf("entries = %#v", snap.Entries)
	}
	if len(snap.Proposals) != 1 || snap.Proposals[0].Status != ProposalStatusPromoted {
		t.Fatalf("proposals = %#v", snap.Proposals)
	}

	doc, err := svc.GetDocument(ctx, "team-alpha", MemoryItemEntry, "ci-runbook")
	if err != nil {
		t.Fatalf("GetDocument entry: %v", err)
	}
	if doc.Path == "" || doc.UUID == "" || doc.Commit == "" || doc.Body != "Use the shared pipeline." {
		t.Fatalf("doc = %#v", doc)
	}
}

func TestTeamMemoryService_ProposalRejectAndAckRequired(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestTeamMemoryService(t, "01HXREJECT", "01HXUUID")
	author := Author{Name: "Owner", Email: "owner@example.test"}

	_, err := svc.CreateProposal(ctx, "team-alpha", CreateMemoryProposalInput{
		TargetKind:  MemoryItemEntry,
		Slug:        "unsafe",
		Description: "missing ack",
		Body:        "body",
		Author:      author,
	})
	if !errors.Is(err, ErrTeamMemoryWarningAckRequired) {
		t.Fatalf("want ErrTeamMemoryWarningAckRequired, got %v", err)
	}

	p, err := svc.CreateProposal(ctx, "team-alpha", CreateMemoryProposalInput{
		TargetKind:          MemoryItemRule,
		Slug:                "review-policy",
		Description:         "Review policy",
		Body:                "Block unsafe changes.",
		Enabled:             true,
		AppliesTo:           []string{"review"},
		WarningAcknowledged: true,
		AuthorRef:           "user:user-owner",
		Author:              author,
	})
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	rejected, err := svc.RejectProposal(ctx, "team-alpha", RejectMemoryProposalInput{
		ProposalID: p.ID,
		Reason:     "too broad",
		ActorRef:   "user:user-owner",
		Author:     author,
	})
	if err != nil {
		t.Fatalf("RejectProposal: %v", err)
	}
	if rejected.Status != ProposalStatusRejected || rejected.RejectReason != "too broad" {
		t.Fatalf("rejected = %#v", rejected)
	}
	if _, err := svc.PromoteProposal(ctx, "team-alpha", PromoteMemoryProposalInput{
		ProposalID: p.ID,
		Author:     author,
	}); !errors.Is(err, ErrTeamMemoryProposalNotPending) {
		t.Fatalf("promote rejected proposal err = %v, want not pending", err)
	}
}

func TestTeamMemoryService_SettingsAndAgentSelfGrant(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestTeamMemoryService(t)
	author := Author{Name: "Owner", Email: "owner@example.test"}

	if _, err := svc.UpdateSettings(ctx, "team-alpha", UpdateTeamMemorySettingsInput{
		CuratorAgents: []string{"agent:agent-a"},
		Policy:        "curator_review",
		ActorRef:      "agent:agent-a",
		ActorKind:     "agent",
		Author:        author,
	}); !errors.Is(err, ErrTeamMemoryAgentSelfGrant) {
		t.Fatalf("agent self grant err = %v, want ErrTeamMemoryAgentSelfGrant", err)
	}

	settings, err := svc.UpdateSettings(ctx, "team-alpha", UpdateTeamMemorySettingsInput{
		CuratorAgents: []string{"agent:agent-b", "user:not-agent", "agent:agent-b", "agent:agent-a"},
		Policy:        "curator_review",
		ActorRef:      "user:user-owner",
		ActorKind:     "user",
		Author:        author,
	})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if settings.Policy != "curator_review" || settings.Commit == "" || settings.EffectHint == "" {
		t.Fatalf("settings metadata = %#v", settings)
	}
	if got, want := settings.CuratorAgents, []string{"agent:agent-a", "agent:agent-b"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("curator agents = %#v, want %#v", got, want)
	}

	roundTrip, err := svc.GetSettings(ctx, "team-alpha")
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if roundTrip.Policy != "curator_review" || len(roundTrip.CuratorAgents) != 2 || roundTrip.Commit == "" {
		t.Fatalf("roundTrip = %#v", roundTrip)
	}
}
