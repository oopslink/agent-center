package workforce

import (
	"testing"
	"time"
)

func TestWorkerGetters_AllFields(t *testing.T) {
	w, _ := NewWorker(NewWorkerInput{
		ID: "W-1", Capabilities: []string{"x"},
		EnrolledAt: time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC),
	})
	_ = w.Heartbeat(time.Date(2026, 5, 20, 11, 0, 0, 0, time.UTC), 60)
	w.MarkOnline(time.Date(2026, 5, 20, 11, 0, 0, 0, time.UTC))
	_ = w.MarkOffline(time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC), OfflineReasonShutdown, "user stopped")
	if w.ID() != "W-1" {
		t.Fatal()
	}
	if w.CreatedAt().IsZero() {
		t.Fatal()
	}
	if w.UpdatedAt().IsZero() {
		t.Fatal()
	}
	if w.EnrolledAt().IsZero() {
		t.Fatal()
	}
	if w.OnlineAt() == nil {
		t.Fatal("online_at should be set")
	}
	if w.OfflineAt() == nil {
		t.Fatal("offline_at should be set")
	}
}

func TestProjectGetters_AllFields(t *testing.T) {
	p, _ := NewProject(NewProjectInput{
		ID: "proj-aabbccdd", Name: "P",
		Description: "desc", Tags: []string{"coding", "ops"},
		CreatedByIdentityID: "user:hayang", CreatedAt: time.Now(),
	})
	if p.ID() != "proj-aabbccdd" {
		t.Fatal()
	}
	if p.Name() != "P" {
		t.Fatal()
	}
	tags := p.Tags()
	if len(tags) != 2 || tags[0] != "coding" || tags[1] != "ops" {
		t.Fatalf("tags = %v", tags)
	}
	if p.Description() != "desc" {
		t.Fatal()
	}
	if p.CreatedByIdentityID() != "user:hayang" {
		t.Fatal()
	}
	if p.CreatedAt().IsZero() {
		t.Fatal()
	}
	if p.UpdatedAt().IsZero() {
		t.Fatal()
	}
}

func TestMappingGetters_AllFields(t *testing.T) {
	m, _ := NewWorkerProjectMapping(NewMappingInput{
		ID: "M-1", WorkerID: "W-1", ProjectID: "p-1",
		BasePath: "/home/x", SourceProposalID: "PR-1",
		AddedAt: time.Now(),
	})
	if m.ID() != "M-1" {
		t.Fatal()
	}
	if m.WorkerID() != "W-1" {
		t.Fatal()
	}
	if m.ProjectID() != "p-1" {
		t.Fatal()
	}
	if m.BasePath() != "/home/x" {
		t.Fatal()
	}
	if m.SourceProposalID() != "PR-1" {
		t.Fatal()
	}
	if m.AddedAt().IsZero() {
		t.Fatal()
	}
	if m.CreatedAt().IsZero() {
		t.Fatal()
	}
	if m.UpdatedAt().IsZero() {
		t.Fatal()
	}
	if m.InvalidatedAt() != nil {
		t.Fatal()
	}
	_ = m.Invalidate(time.Now(), InvalidateReasonPathMissing, "x")
	if m.InvalidatedAt() == nil {
		t.Fatal()
	}
}

func TestProposalGetters_AllFields(t *testing.T) {
	p, _ := NewWorkerProjectProposal(NewProposalInput{
		ID: "PR-1", WorkerID: "W-1", CandidatePath: "/x",
		SuggestedProjectID: "p",
		CandidateMetadata:  CandidateMetadata{GitRemoteURL: "url"},
		ProposedAt:         time.Now(),
	})
	if p.ID() != "PR-1" {
		t.Fatal()
	}
	if p.WorkerID() != "W-1" {
		t.Fatal()
	}
	if p.CandidatePath() != "/x" {
		t.Fatal()
	}
	if p.SuggestedProjectID() != "p" {
		t.Fatal()
	}
	if p.CandidateMetadata().GitRemoteURL != "url" {
		t.Fatal()
	}
	if p.ProposedAt().IsZero() {
		t.Fatal()
	}
	if p.CreatedAt().IsZero() {
		t.Fatal()
	}
	if p.UpdatedAt().IsZero() {
		t.Fatal()
	}
	if p.ReviewedAt() != nil {
		t.Fatal()
	}
	if p.ReviewedByIdentityID() != "" {
		t.Fatal()
	}
	if p.ResultingMappingID() != "" {
		t.Fatal()
	}
	_ = p.Accept(time.Now(), "user:x", "M-1")
	if p.ReviewedAt() == nil {
		t.Fatal()
	}
	if p.ResultingMappingID() != "M-1" {
		t.Fatal()
	}
}
