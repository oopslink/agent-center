package service

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/workforce"
)

// Tests that constructors accept nil clock (defaulting to SystemClock).
func TestNewWorkerEnrollService_NilClock(t *testing.T) {
	s := setupSuite(t)
	enroll := NewWorkerEnrollService(s.db, s.workerRepo, s.sink, nil)
	res, err := enroll.Enroll(context.Background(), EnrollCommand{
		WorkerID:      "W-1",
		ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.WorkerID != "W-1" {
		t.Fatal()
	}
}

func TestNewProjectDiscoveryService_NilClock(t *testing.T) {
	s := setupSuite(t)
	disc := NewProjectDiscoveryService(s.projectRepo, s.sink, nil)
	if disc == nil {
		t.Fatal()
	}
}

func TestNewProjectCRUDService_NilClock(t *testing.T) {
	s := setupSuite(t)
	svc := NewProjectCRUDService(s.db, s.projectRepo, s.mappingRepo, s.sink, nil)
	if svc == nil {
		t.Fatal()
	}
}

func TestNewProposalAcceptanceService_NilClock(t *testing.T) {
	s := setupSuite(t)
	disc := NewProjectDiscoveryService(s.projectRepo, s.sink, s.clock)
	svc := NewProposalAcceptanceService(s.db, s.proposalRepo, s.mappingRepo,
		s.projectRepo, disc, s.sink, s.idgen, nil)
	if svc == nil {
		t.Fatal()
	}
}

// changedFields helper coverage — all v2.5.5 fields set.
func TestChangedFields_AllFields(t *testing.T) {
	name := "n"
	desc := "d"
	tags := []string{"a"}
	got := changedFields(workforce.ProjectUpdateFields{
		Name: &name, Description: &desc, Tags: &tags,
	})
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
}

func TestProposalAcceptanceService_Guard_Misconfigured(t *testing.T) {
	s := &ProposalAcceptanceService{}
	if err := s.guard(); err == nil {
		t.Fatal()
	}
}

// Exercise the Propose error path: empty CandidatePath flows into
// NewWorkerProjectProposal which returns "candidate_path required".
func TestPropose_BadCandidatePath(t *testing.T) {
	s := setupSuite(t)
	svc := acceptanceService(s)
	_, err := svc.Propose(context.Background(), ProposeCommand{
		WorkerID:           "W-1",
		CandidatePath:      "",
		SuggestedProjectID: "p",
		Actor:              "user:x",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAccept_BadActor(t *testing.T) {
	s := setupSuite(t)
	svc := acceptanceService(s)
	_, err := svc.Accept(context.Background(), AcceptCommand{
		ProposalID: "PR-X",
		Actor:      "bogus:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestIgnore_BadActor(t *testing.T) {
	s := setupSuite(t)
	svc := acceptanceService(s)
	_, err := svc.Ignore(context.Background(), IgnoreCommand{
		ProposalID: "PR-X",
		Actor:      "bogus:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestUnignore_BadActor(t *testing.T) {
	s := setupSuite(t)
	svc := acceptanceService(s)
	_, err := svc.Unignore(context.Background(), IgnoreCommand{
		ProposalID: "PR-X",
		Actor:      "bogus:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestProjectCRUD_Add_BadActor(t *testing.T) {
	s := setupSuite(t)
	svc := NewProjectCRUDService(s.db, s.projectRepo, s.mappingRepo, s.sink, s.clock)
	_, err := svc.Add(context.Background(), AddCommand{
		Name: "P", Actor: "bogus:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestProjectCRUD_Update_BadActor(t *testing.T) {
	s := setupSuite(t)
	svc := NewProjectCRUDService(s.db, s.projectRepo, s.mappingRepo, s.sink, s.clock)
	n := "x"
	_, err := svc.Update(context.Background(), UpdateCommand{
		ID: "p", Version: 1, Fields: workforce.ProjectUpdateFields{Name: &n}, Actor: "bogus:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestProjectCRUD_Remove_BadActor(t *testing.T) {
	s := setupSuite(t)
	svc := NewProjectCRUDService(s.db, s.projectRepo, s.mappingRepo, s.sink, s.clock)
	_, err := svc.Remove(context.Background(), RemoveCommand{ID: "p", Actor: "bogus:x"})
	if err == nil {
		t.Fatal()
	}
}

// Exercise the project-not-found path of Remove.
func TestProjectCRUD_Update_NotFound(t *testing.T) {
	s := setupSuite(t)
	svc := NewProjectCRUDService(s.db, s.projectRepo, s.mappingRepo, s.sink, s.clock)
	n := "x"
	_, err := svc.Update(context.Background(), UpdateCommand{
		ID: "nope", Version: 1, Fields: workforce.ProjectUpdateFields{Name: &n}, Actor: "user:x",
	})
	if err == nil {
		t.Fatal()
	}
}

// Exercise propose returning the existing pending proposal with no event.
func TestPropose_DedupNoExtraEvent(t *testing.T) {
	s := setupSuite(t)
	_ = s.workerRepo.Save(context.Background(), newW(t, "W-1"))
	svc := acceptanceService(s)
	cmd := ProposeCommand{
		WorkerID:           "W-1",
		CandidatePath:      "/x",
		SuggestedProjectID: "p",
		Actor:              "user:x",
	}
	r1, _ := svc.Propose(context.Background(), cmd)
	r2, _ := svc.Propose(context.Background(), cmd)
	if r1.ProposalID != r2.ProposalID {
		t.Fatal("dedup should return same id")
	}
	if !r2.AlreadyExists {
		t.Fatal()
	}
	// Check observability has exactly 1 event for this proposal.
	pID := string(r1.ProposalID)
	events, _ := s.eventRepo.Find(context.Background(), observability.EventQueryFilter{
		Refs: observability.EventRefsFilter{ProposalID: pID},
	})
	if len(events) != 1 {
		t.Fatalf("got %d", len(events))
	}
}
