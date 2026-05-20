package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/workforce"
)

func setupWorkerAndProject(t *testing.T) (*WorkerRepo, *ProjectRepo, *MappingRepo) {
	t.Helper()
	db := openTestDB(t)
	wr := NewWorkerRepo(db)
	pr := NewProjectRepo(db)
	mr := NewMappingRepo(db)
	if err := wr.Save(context.Background(), newWorker(t, "W-1")); err != nil {
		t.Fatal(err)
	}
	if err := pr.Save(context.Background(), newProject(t, "p-1")); err != nil {
		t.Fatal(err)
	}
	return wr, pr, mr
}

func newMapping(t *testing.T, worker workforce.WorkerID, project workforce.ProjectID) *workforce.WorkerProjectMapping {
	t.Helper()
	m, err := workforce.NewWorkerProjectMapping(workforce.NewMappingInput{
		ID:               workforce.MappingID(idgen.MustNewULID()),
		WorkerID:         worker,
		ProjectID:        project,
		BasePath:         "/home/u/p",
		SourceProposalID: "PR-1",
		AddedAt:          time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMappingRepo_SaveAndFindByID(t *testing.T) {
	_, _, mr := setupWorkerAndProject(t)
	m := newMapping(t, "W-1", "p-1")
	if err := mr.Save(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	got, err := mr.FindByID(context.Background(), m.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status() != workforce.MappingActive {
		t.Fatal()
	}
}

func TestMappingRepo_FindByID_NotFound(t *testing.T) {
	_, _, mr := setupWorkerAndProject(t)
	_, err := mr.FindByID(context.Background(), "M-NEVER")
	if !errors.Is(err, workforce.ErrMappingNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestMappingRepo_Save_DuplicateActive(t *testing.T) {
	_, _, mr := setupWorkerAndProject(t)
	_ = mr.Save(context.Background(), newMapping(t, "W-1", "p-1"))
	err := mr.Save(context.Background(), newMapping(t, "W-1", "p-1"))
	if !errors.Is(err, workforce.ErrMappingAlreadyActive) {
		t.Fatalf("got %v", err)
	}
}

func TestMappingRepo_Invalidate_Happy(t *testing.T) {
	_, _, mr := setupWorkerAndProject(t)
	m := newMapping(t, "W-1", "p-1")
	_ = mr.Save(context.Background(), m)
	err := mr.Invalidate(context.Background(), m.ID(), workforce.InvalidateReasonPathMissing, "gone", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := mr.FindByID(context.Background(), m.ID())
	if got.Status() != workforce.MappingInvalidated {
		t.Fatal()
	}
	if got.InvalidateReason() != workforce.InvalidateReasonPathMissing {
		t.Fatal()
	}
}

func TestMappingRepo_Invalidate_NotFound(t *testing.T) {
	_, _, mr := setupWorkerAndProject(t)
	err := mr.Invalidate(context.Background(), "M-NEVER", workforce.InvalidateReasonPathMissing, "x", time.Now())
	if !errors.Is(err, workforce.ErrMappingNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestMappingRepo_Invalidate_AlreadyInvalidated(t *testing.T) {
	_, _, mr := setupWorkerAndProject(t)
	m := newMapping(t, "W-1", "p-1")
	_ = mr.Save(context.Background(), m)
	_ = mr.Invalidate(context.Background(), m.ID(), workforce.InvalidateReasonPathMissing, "x", time.Now())
	err := mr.Invalidate(context.Background(), m.ID(), workforce.InvalidateReasonPathMissing, "x", time.Now())
	if !errors.Is(err, workforce.ErrMappingNotActive) {
		t.Fatalf("got %v", err)
	}
}

func TestMappingRepo_Invalidate_BadReason(t *testing.T) {
	_, _, mr := setupWorkerAndProject(t)
	err := mr.Invalidate(context.Background(), "M-1", "bogus", "x", time.Now())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMappingRepo_Invalidate_NoMessage(t *testing.T) {
	_, _, mr := setupWorkerAndProject(t)
	err := mr.Invalidate(context.Background(), "M-1", workforce.InvalidateReasonPathMissing, "", time.Now())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMappingRepo_FindByWorkerID(t *testing.T) {
	_, pr, mr := setupWorkerAndProject(t)
	// add a second project
	_ = pr.Save(context.Background(), newProject(t, "p-2"))
	_ = mr.Save(context.Background(), newMapping(t, "W-1", "p-1"))
	_ = mr.Save(context.Background(), newMapping(t, "W-1", "p-2"))
	got, err := mr.FindByWorkerID(context.Background(), "W-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
}

func TestMappingRepo_FindByProjectID(t *testing.T) {
	_, _, mr := setupWorkerAndProject(t)
	_ = mr.Save(context.Background(), newMapping(t, "W-1", "p-1"))
	got, err := mr.FindByProjectID(context.Background(), "p-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatal()
	}
}

func TestMappingRepo_FindByWorkerAndProject_Active(t *testing.T) {
	_, _, mr := setupWorkerAndProject(t)
	m := newMapping(t, "W-1", "p-1")
	_ = mr.Save(context.Background(), m)
	got, err := mr.FindByWorkerAndProject(context.Background(), "W-1", "p-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != m.ID() {
		t.Fatal()
	}
}

func TestMappingRepo_FindByWorkerAndProject_Invalidated(t *testing.T) {
	_, _, mr := setupWorkerAndProject(t)
	m := newMapping(t, "W-1", "p-1")
	_ = mr.Save(context.Background(), m)
	_ = mr.Invalidate(context.Background(), m.ID(), workforce.InvalidateReasonPathMissing, "x", time.Now())
	_, err := mr.FindByWorkerAndProject(context.Background(), "W-1", "p-1")
	if !errors.Is(err, workforce.ErrMappingNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestMappingRepo_CountActiveByProjectID(t *testing.T) {
	_, _, mr := setupWorkerAndProject(t)
	n, err := mr.CountActiveByProjectID(context.Background(), "p-1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal()
	}
	_ = mr.Save(context.Background(), newMapping(t, "W-1", "p-1"))
	n, _ = mr.CountActiveByProjectID(context.Background(), "p-1")
	if n != 1 {
		t.Fatalf("got %d", n)
	}
}

func TestMappingRepo_Save_NilMapping(t *testing.T) {
	_, _, mr := setupWorkerAndProject(t)
	if err := mr.Save(context.Background(), nil); err == nil {
		t.Fatal()
	}
}
