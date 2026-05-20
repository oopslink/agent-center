package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/workforce"
)

// closedDBProvider exercises the ExecutorFromCtx fallback by closing the
// underlying DB before invoking a repo method.
func TestWorkerRepo_FindByID_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	_ = db.Close()
	_, err := repo.FindByID(context.Background(), "W-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProjectRepo_FindByID_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	_ = db.Close()
	_, err := repo.FindByID(context.Background(), "p")
	if err == nil {
		t.Fatal()
	}
}

func TestMappingRepo_FindByID_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewMappingRepo(db)
	_ = db.Close()
	_, err := repo.FindByID(context.Background(), "M-1")
	if err == nil {
		t.Fatal()
	}
}

func TestProposalRepo_FindByID_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewProposalRepo(db)
	_ = db.Close()
	_, err := repo.FindByID(context.Background(), "PR-1")
	if err == nil {
		t.Fatal()
	}
}

func TestWorkerRepo_FindAll_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	_ = db.Close()
	_, err := repo.FindAll(context.Background())
	if err == nil {
		t.Fatal()
	}
}

func TestMappingRepo_FindByWorkerID_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewMappingRepo(db)
	_ = db.Close()
	_, err := repo.FindByWorkerID(context.Background(), "W")
	if err == nil {
		t.Fatal()
	}
}

func TestMappingRepo_FindByProjectID_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewMappingRepo(db)
	_ = db.Close()
	_, err := repo.FindByProjectID(context.Background(), "p")
	if err == nil {
		t.Fatal()
	}
}

func TestMappingRepo_CountActiveByProjectID_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewMappingRepo(db)
	_ = db.Close()
	_, err := repo.CountActiveByProjectID(context.Background(), "p")
	if err == nil {
		t.Fatal()
	}
}

func TestProposalRepo_FindByWorkerID_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewProposalRepo(db)
	_ = db.Close()
	_, err := repo.FindByWorkerID(context.Background(), "W")
	if err == nil {
		t.Fatal()
	}
}

func TestProposalRepo_FindPending_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewProposalRepo(db)
	_ = db.Close()
	_, err := repo.FindPending(context.Background())
	if err == nil {
		t.Fatal()
	}
}

func TestWorkerRepo_FindByStatus_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	_ = db.Close()
	_, err := repo.FindByStatus(context.Background(), workforce.WorkerOffline)
	if err == nil {
		t.Fatal()
	}
}

func TestProjectRepo_FindAll_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewProjectRepo(db)
	_ = db.Close()
	_, err := repo.FindAll(context.Background(), workforce.ProjectFilter{})
	if err == nil {
		t.Fatal()
	}
}

func TestIndexOf_NotFound(t *testing.T) {
	if indexOf("abc", "z") != -1 {
		t.Fatal()
	}
	if indexOf("abc", "bc") != 1 {
		t.Fatal()
	}
}

func TestContains_Helpers(t *testing.T) {
	if !contains("abcdef", "cd") {
		t.Fatal()
	}
	if contains("a", "ab") {
		t.Fatal()
	}
	if !contains("abc", "abc") {
		t.Fatal()
	}
}

func TestIsForeignKeyViolation(t *testing.T) {
	if isForeignKeyViolation(nil) {
		t.Fatal()
	}
	if !isForeignKeyViolation(errors.New("FOREIGN KEY constraint failed")) {
		t.Fatal()
	}
	if isForeignKeyViolation(errors.New("unrelated")) {
		t.Fatal()
	}
}

func TestParseNullTime_EmptyValid(t *testing.T) {
	_, err := parseNullTime(sql.NullString{String: "", Valid: true})
	if err != nil {
		t.Fatal(err)
	}
}

func TestParseNullTime_Invalid(t *testing.T) {
	_, err := parseNullTime(sql.NullString{String: "not-a-time", Valid: true})
	if err == nil {
		t.Fatal()
	}
}

// Verify the workforce IsUniqueConstraint helper.
func TestIsUniqueConstraint_Nil(t *testing.T) {
	if IsUniqueConstraint(nil) {
		t.Fatal()
	}
	if !IsUniqueConstraint(errors.New("UNIQUE constraint failed")) {
		t.Fatal()
	}
}

func TestProposalRepo_FindByCandidatePath_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewProposalRepo(db)
	_ = db.Close()
	_, err := repo.FindByCandidatePath(context.Background(), "W", "/x")
	if err == nil {
		t.Fatal()
	}
}

// Tests that Worker.Save FK enforcement does NOT fire by accident
// (sanity for FK pragma being on).
func TestForeignKeyEnforced(t *testing.T) {
	db := openTestDB(t)
	mr := NewMappingRepo(db)
	// Insert mapping referring to non-existent worker → FK should fail.
	m, _ := workforce.NewWorkerProjectMapping(workforce.NewMappingInput{
		ID: "M-1", WorkerID: "W-DOESNT-EXIST", ProjectID: "p-DOESNT-EXIST",
		BasePath: "/x", AddedAt: time.Now(),
	})
	err := mr.Save(context.Background(), m)
	if err == nil {
		t.Fatal("expected FK error")
	}
}
