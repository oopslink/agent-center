package workforce

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func newTestProposal(t *testing.T) *WorkerProjectProposal {
	t.Helper()
	p, err := NewWorkerProjectProposal(NewProposalInput{
		ID:                 "PR-1",
		WorkerID:           "W-1",
		CandidatePath:      "/home/u/agent-center",
		SuggestedProjectID: "proj-cafefade",
		CandidateMetadata: CandidateMetadata{
			GitRemoteURL: "https://github.com/x/x",
			CommitCount:  100,
		},
		ProposedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("NewWorkerProjectProposal: %v", err)
	}
	return p
}

func TestProposal_New_Happy(t *testing.T) {
	p := newTestProposal(t)
	if p.Status() != ProposalPending {
		t.Fatal()
	}
	if p.Version() != 1 {
		t.Fatal()
	}
	if p.CandidateMetadata().GitRemoteURL == "" {
		t.Fatal()
	}
}

func TestProposal_New_BadInputs(t *testing.T) {
	cases := []NewProposalInput{
		{},
		{ID: "PR-1"},
		{ID: "PR-1", WorkerID: "W-1"},
		{ID: "PR-1", WorkerID: "W-1", CandidatePath: "/x"},
		{ID: "PR-1", WorkerID: "W-1", CandidatePath: "/x", SuggestedProjectID: "p"}, // missing ProposedAt
	}
	for i, in := range cases {
		if _, err := NewWorkerProjectProposal(in); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}

func TestProposal_Accept_Happy(t *testing.T) {
	p := newTestProposal(t)
	err := p.Accept(time.Now(), "user:hayang", "M-1")
	if err != nil {
		t.Fatal(err)
	}
	if p.Status() != ProposalAccepted {
		t.Fatal()
	}
	if p.ReviewedAt() == nil {
		t.Fatal()
	}
	if p.ResultingMappingID() != "M-1" {
		t.Fatal()
	}
	if p.Version() != 2 {
		t.Fatal()
	}
}

func TestProposal_Accept_FromAccepted(t *testing.T) {
	p := newTestProposal(t)
	_ = p.Accept(time.Now(), "user:x", "M-1")
	err := p.Accept(time.Now(), "user:x", "M-2")
	if !errors.Is(err, ErrProposalAlreadyTerminated) {
		t.Fatalf("got %v", err)
	}
}

func TestProposal_Accept_NoReviewer(t *testing.T) {
	p := newTestProposal(t)
	if err := p.Accept(time.Now(), "  ", "M-1"); err == nil {
		t.Fatal("expected error")
	}
}

func TestProposal_Accept_NoMapping(t *testing.T) {
	p := newTestProposal(t)
	if err := p.Accept(time.Now(), "user:x", ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestProposal_Ignore(t *testing.T) {
	p := newTestProposal(t)
	err := p.Ignore(time.Now(), "user:x")
	if err != nil {
		t.Fatal(err)
	}
	if p.Status() != ProposalIgnored {
		t.Fatal()
	}
}

func TestProposal_Ignore_FromAccepted(t *testing.T) {
	p := newTestProposal(t)
	_ = p.Accept(time.Now(), "user:x", "M-1")
	err := p.Ignore(time.Now(), "user:x")
	if !errors.Is(err, ErrProposalAlreadyTerminated) {
		t.Fatalf("got %v", err)
	}
}

func TestProposal_Ignore_RequiresReviewer(t *testing.T) {
	p := newTestProposal(t)
	if err := p.Ignore(time.Now(), ""); err == nil {
		t.Fatal()
	}
}

func TestProposal_Unignore_Happy(t *testing.T) {
	p := newTestProposal(t)
	_ = p.Ignore(time.Now(), "user:x")
	err := p.Unignore(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if p.Status() != ProposalPending {
		t.Fatal()
	}
	if p.ReviewedAt() != nil {
		t.Fatal()
	}
}

func TestProposal_Unignore_FromPending(t *testing.T) {
	p := newTestProposal(t)
	err := p.Unignore(time.Now())
	if !errors.Is(err, ErrProposalInvalidTransition) {
		t.Fatalf("got %v", err)
	}
}

func TestProposal_Supersede(t *testing.T) {
	p := newTestProposal(t)
	err := p.Supersede(time.Now(), "system")
	if err != nil {
		t.Fatal(err)
	}
	if p.Status() != ProposalSuperseded {
		t.Fatal()
	}
}

func TestProposal_Supersede_FromAccepted(t *testing.T) {
	p := newTestProposal(t)
	_ = p.Accept(time.Now(), "user:x", "M-1")
	err := p.Supersede(time.Now(), "system")
	if !errors.Is(err, ErrProposalInvalidTransition) {
		t.Fatalf("got %v", err)
	}
}

func TestProposalStatus_Validation(t *testing.T) {
	for _, s := range []ProposalStatus{ProposalPending, ProposalAccepted, ProposalIgnored, ProposalSuperseded} {
		if !s.IsValid() {
			t.Fatalf("%v should be valid", s)
		}
	}
	if ProposalStatus("nope").IsValid() {
		t.Fatal()
	}
	if !ProposalAccepted.IsTerminal() {
		t.Fatal()
	}
	if !ProposalSuperseded.IsTerminal() {
		t.Fatal()
	}
	if ProposalPending.IsTerminal() {
		t.Fatal()
	}
	if ProposalIgnored.IsTerminal() {
		t.Fatal()
	}
	if ProposalPending.String() != "pending" {
		t.Fatal()
	}
}

func TestCandidateMetadata_Marshal(t *testing.T) {
	m := CandidateMetadata{GitRemoteURL: "https://x", CommitCount: 5}
	b, err := m.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var got CandidateMetadata
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got != m {
		t.Fatalf("roundtrip: got %+v want %+v", got, m)
	}
}

func TestProposal_RehydrateBadStatus(t *testing.T) {
	_, err := RehydrateWorkerProjectProposal(RehydrateProposalInput{Status: "bogus", Version: 1})
	if err == nil {
		t.Fatal()
	}
}

func TestProposal_RehydrateBadVersion(t *testing.T) {
	_, err := RehydrateWorkerProjectProposal(RehydrateProposalInput{Status: ProposalPending, Version: 0})
	if err == nil {
		t.Fatal()
	}
}

func TestProposalID_String(t *testing.T) {
	if ProposalID("PR-1").String() != "PR-1" {
		t.Fatal()
	}
	if MappingID("M-1").String() != "M-1" {
		t.Fatal()
	}
}
