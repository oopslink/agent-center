package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

func TestArtifactRepo_AppendAndFinds(t *testing.T) {
	db := openTestDB(t)
	mkTaskInDB(t, db, "T-1")
	if err := NewTaskExecutionRepo(db).Save(context.Background(), mkExec(t, "E-1", "T-1")); err != nil {
		t.Fatal(err)
	}
	repo := NewArtifactRepo(db)
	ctx := context.Background()
	a, err := execution.NewArtifact(execution.NewArtifactInput{
		ID: "A-1", TaskID: "T-1", ExecutionID: "E-1",
		Kind: "pr_url", Title: "feat: thing", URL: "https://github.com/x/y/pull/1",
		CreatedBy: "agent:E-1", Now: refTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Append(ctx, a); err != nil {
		t.Fatalf("append: %v", err)
	}
	got, err := repo.FindByID(ctx, "A-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.URL() != "https://github.com/x/y/pull/1" {
		t.Fatalf("url: %s", got.URL())
	}
	byExec, err := repo.FindByExecutionID(ctx, "E-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(byExec) != 1 {
		t.Fatalf("by exec: %d", len(byExec))
	}
	byTask, err := repo.FindByTaskID(ctx, "T-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(byTask) != 1 {
		t.Fatalf("by task: %d", len(byTask))
	}
}

func TestArtifactRepo_NotFoundNilDup(t *testing.T) {
	db := openTestDB(t)
	repo := NewArtifactRepo(db)
	if _, err := repo.FindByID(context.Background(), "A-NONE"); !errors.Is(err, execution.ErrArtifactNotFound) {
		t.Fatalf("expected not_found: %v", err)
	}
	if err := repo.Append(context.Background(), nil); err == nil {
		t.Fatal("expected nil error")
	}
	// duplicate id
	mkTaskInDB(t, db, "T-1")
	if err := NewTaskExecutionRepo(db).Save(context.Background(), mkExec(t, "E-1", "T-1")); err != nil {
		t.Fatal(err)
	}
	a, _ := execution.NewArtifact(execution.NewArtifactInput{
		ID: "A-1", TaskID: "T-1", ExecutionID: "E-1",
		Kind: "k", Title: "t", CreatedBy: "u", Now: refTime,
	})
	if err := repo.Append(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if err := repo.Append(context.Background(), a); err == nil {
		t.Fatal("expected duplicate id error")
	}
}
