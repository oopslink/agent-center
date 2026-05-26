package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/workforce"
)

func setupProposalDeps(t *testing.T) *ProposalRepo {
	t.Helper()
	db := openTestDB(t)
	wr := NewWorkerRepo(db)
	if err := wr.Save(context.Background(), newWorker(t, "W-1")); err != nil {
		t.Fatal(err)
	}
	return NewProposalRepo(db)
}

func TestProposalRepo_SaveAndFindByID(t *testing.T) {
	repo := setupProposalDeps(t)
	p := newProposal(t, "W-1", "/x/agent-center")
	if err := repo.Save(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindByID(context.Background(), p.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status() != workforce.ProposalPending {
		t.Fatal()
	}
}

func TestProposalRepo_FindByID_NotFound(t *testing.T) {
	repo := setupProposalDeps(t)
	_, err := repo.FindByID(context.Background(), "PR-NEVER")
	if !errors.Is(err, workforce.ErrProposalNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestProposalRepo_Save_Duplicate(t *testing.T) {
	repo := setupProposalDeps(t)
	id := workforce.ProposalID(idgen.MustNewULID())
	p1, _ := workforce.NewWorkerProjectProposal(workforce.NewProposalInput{
		ID: id, WorkerID: "W-1", CandidatePath: "/x", SuggestedProjectID: "proj-deadbeef",
		ProposedAt: time.Now(),
	})
	_ = repo.Save(context.Background(), p1)
	err := repo.Save(context.Background(), p1)
	if !errors.Is(err, workforce.ErrProposalAlreadyExists) {
		t.Fatalf("got %v", err)
	}
}

func TestProposalRepo_Save_PartialUniqueCandidatePath(t *testing.T) {
	repo := setupProposalDeps(t)
	p1 := newProposal(t, "W-1", "/same/path")
	if err := repo.Save(context.Background(), p1); err != nil {
		t.Fatal(err)
	}
	p2 := newProposal(t, "W-1", "/same/path")
	err := repo.Save(context.Background(), p2)
	if !errors.Is(err, workforce.ErrProposalAlreadyExists) {
		t.Fatalf("got %v", err)
	}
}

func TestProposalRepo_FindByCandidatePath_Exists(t *testing.T) {
	repo := setupProposalDeps(t)
	p := newProposal(t, "W-1", "/x/y")
	_ = repo.Save(context.Background(), p)
	got, err := repo.FindByCandidatePath(context.Background(), "W-1", "/x/y")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != p.ID() {
		t.Fatal()
	}
}

func TestProposalRepo_FindByCandidatePath_NotExists(t *testing.T) {
	repo := setupProposalDeps(t)
	_, err := repo.FindByCandidatePath(context.Background(), "W-1", "/no")
	if !errors.Is(err, workforce.ErrProposalNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestProposalRepo_FindPending(t *testing.T) {
	repo := setupProposalDeps(t)
	for i := 0; i < 3; i++ {
		p := newProposal(t, "W-1", "/p/"+string(rune('a'+i)))
		_ = repo.Save(context.Background(), p)
	}
	got, err := repo.FindPending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d", len(got))
	}
}

func TestProposalRepo_FindPending_ExcludesNonPending(t *testing.T) {
	repo := setupProposalDeps(t)
	p := newProposal(t, "W-1", "/x")
	_ = repo.Save(context.Background(), p)
	_ = p.Ignore(time.Now(), "user:x")
	_ = repo.Update(context.Background(), p)
	got, _ := repo.FindPending(context.Background())
	if len(got) != 0 {
		t.Fatalf("got %d", len(got))
	}
}

func TestProposalRepo_Update_CASSuccess(t *testing.T) {
	repo := setupProposalDeps(t)
	p := newProposal(t, "W-1", "/x")
	_ = repo.Save(context.Background(), p)
	if err := p.Accept(time.Now(), "user:x", "M-1"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.FindByID(context.Background(), p.ID())
	if got.Status() != workforce.ProposalAccepted {
		t.Fatal()
	}
	if got.ResultingMappingID() != "M-1" {
		t.Fatal()
	}
}

func TestProposalRepo_Update_VersionConflict(t *testing.T) {
	repo := setupProposalDeps(t)
	p := newProposal(t, "W-1", "/x")
	_ = repo.Save(context.Background(), p)
	// Two parallel loads
	p1, _ := repo.FindByID(context.Background(), p.ID())
	p2, _ := repo.FindByID(context.Background(), p.ID())
	_ = p1.Ignore(time.Now(), "user:a")
	_ = repo.Update(context.Background(), p1)
	_ = p2.Accept(time.Now(), "user:b", "M-1")
	err := repo.Update(context.Background(), p2)
	if !errors.Is(err, workforce.ErrProposalVersionConflict) {
		t.Fatalf("got %v", err)
	}
}

func TestProposalRepo_Update_NotFound(t *testing.T) {
	repo := setupProposalDeps(t)
	p := newProposal(t, "W-1", "/x")
	// Don't save; force version > 1 by simulating transitions.
	_ = p.Accept(time.Now(), "user:x", "M-1")
	err := repo.Update(context.Background(), p)
	if !errors.Is(err, workforce.ErrProposalNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestProposalRepo_FindByWorkerID_AllStatuses(t *testing.T) {
	repo := setupProposalDeps(t)
	p1 := newProposal(t, "W-1", "/a")
	p2 := newProposal(t, "W-1", "/b")
	_ = repo.Save(context.Background(), p1)
	_ = repo.Save(context.Background(), p2)
	_ = p2.Ignore(time.Now(), "user:x")
	_ = repo.Update(context.Background(), p2)
	got, err := repo.FindByWorkerID(context.Background(), "W-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
}

func TestProposalRepo_FindByWorkerID_StatusFilter(t *testing.T) {
	repo := setupProposalDeps(t)
	p1 := newProposal(t, "W-1", "/a")
	p2 := newProposal(t, "W-1", "/b")
	_ = repo.Save(context.Background(), p1)
	_ = repo.Save(context.Background(), p2)
	_ = p2.Ignore(time.Now(), "user:x")
	_ = repo.Update(context.Background(), p2)
	got, _ := repo.FindByWorkerID(context.Background(), "W-1", workforce.ProposalPending)
	if len(got) != 1 || got[0].ID() != p1.ID() {
		t.Fatalf("filter pending: %v", got)
	}
}

func TestProposalRepo_Save_NilProposal(t *testing.T) {
	repo := setupProposalDeps(t)
	if err := repo.Save(context.Background(), nil); err == nil {
		t.Fatal()
	}
}

func TestProposalRepo_Update_NilProposal(t *testing.T) {
	repo := setupProposalDeps(t)
	if err := repo.Update(context.Background(), nil); err == nil {
		t.Fatal()
	}
}
