package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"

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

func TestWorkerRepo_FindAll_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewWorkerRepo(db)
	_ = db.Close()
	_, err := repo.FindAll(context.Background())
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
