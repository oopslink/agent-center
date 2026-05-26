package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := t.TempDir() + "/test.db"
	db, err := persistence.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatalf("Up: %v", err)
	}
	return db
}

func newWorker(t *testing.T, id workforce.WorkerID) *workforce.Worker {
	t.Helper()
	w, err := workforce.NewWorker(workforce.NewWorkerInput{
		ID:           id,
		Capabilities: []string{"claude-code"},
		EnrolledAt:   time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	return w
}

func newProject(t *testing.T, id workforce.ProjectID) *workforce.Project {
	t.Helper()
	p, err := workforce.NewProject(workforce.NewProjectInput{
		ID:                  id,
		Name:                "Test Project",
		Tags:                []string{"coding"},
		CreatedByIdentityID: "user:hayang",
		CreatedAt:           time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewProject: %v", err)
	}
	return p
}

func newProposal(t *testing.T, workerID workforce.WorkerID, candidatePath string) *workforce.WorkerProjectProposal {
	t.Helper()
	p, err := workforce.NewWorkerProjectProposal(workforce.NewProposalInput{
		ID:                 workforce.ProposalID(idgen.MustNewULID()),
		WorkerID:           workerID,
		CandidatePath:      candidatePath,
		SuggestedProjectID: "proj-deadbeef",
		ProposedAt:         time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewWorkerProjectProposal: %v", err)
	}
	return p
}
