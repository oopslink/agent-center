package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/discussion"
	"github.com/oopslink/agent-center/internal/persistence"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newIssue(t *testing.T, id, projID, title string) *discussion.Issue {
	t.Helper()
	i, err := discussion.NewIssue(discussion.NewIssueInput{
		ID:                 discussion.IssueID(id),
		ProjectID:          projID,
		Title:              title,
		OpenedByIdentityID: "user:h",
		Origin:             discussion.OriginCLI,
		OpenedAt:           time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return i
}

func TestIssueRepo_SaveAndFindByID(t *testing.T) {
	db := openDB(t)
	r := NewIssueRepo(db)
	i := newIssue(t, "ISS-1", "P-1", "hello")
	if err := r.Save(context.Background(), i); err != nil {
		t.Fatal(err)
	}
	got, err := r.FindByID(context.Background(), i.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != i.ID() || got.ProjectID() != "P-1" || got.Title() != "hello" ||
		got.Status() != discussion.StatusOpen || got.Origin() != discussion.OriginCLI {
		t.Fatalf("roundtrip: %+v", got)
	}
}

func TestIssueRepo_Save_NilAndDuplicate(t *testing.T) {
	db := openDB(t)
	r := NewIssueRepo(db)
	if err := r.Save(context.Background(), nil); err == nil {
		t.Fatal("nil issue should err")
	}
	i := newIssue(t, "ISS-X", "P-1", "x")
	if err := r.Save(context.Background(), i); err != nil {
		t.Fatal(err)
	}
	err := r.Save(context.Background(), i)
	if !errors.Is(err, discussion.ErrIssueAlreadyExists) {
		t.Fatalf("expected already exists, got %v", err)
	}
}

func TestIssueRepo_FindByID_NotFound(t *testing.T) {
	db := openDB(t)
	r := NewIssueRepo(db)
	_, err := r.FindByID(context.Background(), discussion.IssueID("ghost"))
	if !errors.Is(err, discussion.ErrIssueNotFound) {
		t.Fatalf("expected not_found, got %v", err)
	}
}

func TestIssueRepo_UpdateStatus_CASAndConflict(t *testing.T) {
	db := openDB(t)
	r := NewIssueRepo(db)
	i := newIssue(t, "ISS-S", "P-1", "x")
	if err := r.Save(context.Background(), i); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	// success
	if err := r.UpdateStatus(context.Background(), i.ID(), discussion.StatusOpen, discussion.StatusUnderDiscussion, 1, now); err != nil {
		t.Fatal(err)
	}
	// stale version
	err := r.UpdateStatus(context.Background(), i.ID(), discussion.StatusUnderDiscussion, discussion.StatusConcluded, 1, now)
	if !errors.Is(err, discussion.ErrIssueVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
	// from-status mismatch (version=2 but from=open)
	err = r.UpdateStatus(context.Background(), i.ID(), discussion.StatusOpen, discussion.StatusConcluded, 2, now)
	if !errors.Is(err, discussion.ErrIssueVersionConflict) {
		t.Fatalf("expected version conflict on from mismatch, got %v", err)
	}
	// not found
	err = r.UpdateStatus(context.Background(), discussion.IssueID("ghost"), discussion.StatusOpen, discussion.StatusUnderDiscussion, 1, now)
	if !errors.Is(err, discussion.ErrIssueNotFound) {
		t.Fatalf("expected not_found, got %v", err)
	}
	// invalid status
	err = r.UpdateStatus(context.Background(), i.ID(), "bogus", discussion.StatusUnderDiscussion, 2, now)
	if err == nil {
		t.Fatal("expected err on invalid from")
	}
}

func TestIssueRepo_UpdateConversationID_NullToNonNullOnly(t *testing.T) {
	db := openDB(t)
	r := NewIssueRepo(db)
	i := newIssue(t, "ISS-C", "P-1", "x")
	if err := r.Save(context.Background(), i); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	// success
	if err := r.UpdateConversationID(context.Background(), i.ID(), "conv-1", 1, now); err != nil {
		t.Fatal(err)
	}
	// rebind blocked at SQL level (conversation_id IS NULL filter)
	err := r.UpdateConversationID(context.Background(), i.ID(), "conv-2", 2, now)
	if !errors.Is(err, discussion.ErrIssueVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
	// empty
	err = r.UpdateConversationID(context.Background(), i.ID(), "", 2, now)
	if err == nil {
		t.Fatal("empty conv id should err")
	}
	// not found
	err = r.UpdateConversationID(context.Background(), "ghost", "x", 1, now)
	if !errors.Is(err, discussion.ErrIssueNotFound) {
		t.Fatalf("expected not_found, got %v", err)
	}
}

func TestIssueRepo_UpdateConclusion(t *testing.T) {
	db := openDB(t)
	r := NewIssueRepo(db)
	i := newIssue(t, "ISS-K", "P-1", "x")
	if err := r.Save(context.Background(), i); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := r.UpdateConclusion(context.Background(), i.ID(), "summary", "user:h", now, 1); err != nil {
		t.Fatal(err)
	}
	got, _ := r.FindByID(context.Background(), i.ID())
	if got.ConclusionSummary() != "summary" || got.ConcludedByIdentityID() != "user:h" {
		t.Fatalf("conclusion fields: %+v", got)
	}
	// missing args
	if err := r.UpdateConclusion(context.Background(), i.ID(), "", "x", now, 2); err == nil {
		t.Fatal("empty summary should err")
	}
	if err := r.UpdateConclusion(context.Background(), i.ID(), "s", "", now, 2); err == nil {
		t.Fatal("empty concluded_by should err")
	}
	// CAS conflict (version=2 expected; pass 1)
	err := r.UpdateConclusion(context.Background(), i.ID(), "s", "user:h", now, 1)
	if !errors.Is(err, discussion.ErrIssueVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

func TestIssueRepo_UpdateRelatedConversationIDs(t *testing.T) {
	db := openDB(t)
	r := NewIssueRepo(db)
	i := newIssue(t, "ISS-R", "P-1", "x")
	if err := r.Save(context.Background(), i); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := r.UpdateRelatedConversationIDs(context.Background(), i.ID(), nil, 1, now); err != nil {
		t.Fatal(err)
	}
	// CAS conflict
	err := r.UpdateRelatedConversationIDs(context.Background(), i.ID(), nil, 1, now)
	if !errors.Is(err, discussion.ErrIssueVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

func TestIssueRepo_UpdateWithdraw(t *testing.T) {
	db := openDB(t)
	r := NewIssueRepo(db)
	i := newIssue(t, "ISS-W", "P-1", "x")
	if err := r.Save(context.Background(), i); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := r.UpdateWithdraw(context.Background(), i.ID(), "dup", "of #5", "user:h", now, 1); err != nil {
		t.Fatal(err)
	}
	got, _ := r.FindByID(context.Background(), i.ID())
	if got.Status() != discussion.StatusWithdrawn || got.WithdrawReason() != "dup" {
		t.Fatalf("withdrawn fields: %+v", got)
	}
	// second withdraw on already-withdrawn → 0 rows (SQL filter NOT IN)
	err := r.UpdateWithdraw(context.Background(), i.ID(), "x", "y", "user:h", now, 2)
	if !errors.Is(err, discussion.ErrIssueVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
	// missing args
	if err := r.UpdateWithdraw(context.Background(), i.ID(), "", "y", "user:h", now, 2); err == nil {
		t.Fatal("missing reason should err")
	}
	if err := r.UpdateWithdraw(context.Background(), i.ID(), "x", "", "user:h", now, 2); err == nil {
		t.Fatal("missing message should err")
	}
	if err := r.UpdateWithdraw(context.Background(), i.ID(), "x", "y", "", now, 2); err == nil {
		t.Fatal("missing by should err")
	}
}

// Covers the cursor branch in issue_repo.go:114-117 (`AND id > ?`). The
// other FindByProject tests pass no Cursor so this WHERE clause never gets
// appended.
func TestIssueRepo_FindByProject_Cursor(t *testing.T) {
	db := openDB(t)
	r := NewIssueRepo(db)
	for i, id := range []string{"AA", "BB", "CC"} {
		iss, err := discussion.NewIssue(discussion.NewIssueInput{
			ID:                 discussion.IssueID(id),
			ProjectID:          "P-1",
			Title:              "t",
			OpenedByIdentityID: "user:h",
			Origin:             discussion.OriginCLI,
			OpenedAt:           time.Date(2026, 5, 21, 12, i, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Save(context.Background(), iss); err != nil {
			t.Fatal(err)
		}
	}
	cursor := discussion.IssueID("AA")
	got, err := r.FindByProject(context.Background(), "P-1", discussion.IssueFilter{Cursor: &cursor})
	if err != nil {
		t.Fatal(err)
	}
	for _, iss := range got {
		if iss.ID() == "AA" {
			t.Fatalf("cursor should exclude AA, got %v", iss.ID())
		}
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 issues after cursor, got %d", len(got))
	}
}

func TestIssueRepo_FindByProject_StatusOpener(t *testing.T) {
	db := openDB(t)
	r := NewIssueRepo(db)
	for i, payload := range []struct{ id, proj, opener string }{
		{"A", "P-1", "user:h"},
		{"B", "P-1", "user:h"},
		{"C", "P-2", "user:x"},
	} {
		iss, err := discussion.NewIssue(discussion.NewIssueInput{
			ID:                 discussion.IssueID(payload.id),
			ProjectID:          payload.proj,
			Title:              "t",
			OpenedByIdentityID: payload.opener,
			Origin:             discussion.OriginCLI,
			OpenedAt:           time.Date(2026, 5, 21, 12, i, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Save(context.Background(), iss); err != nil {
			t.Fatal(err)
		}
	}
	// FindByProject
	p1, err := r.FindByProject(context.Background(), "P-1", discussion.IssueFilter{})
	if err != nil || len(p1) != 2 {
		t.Fatalf("p1 count: %d err=%v", len(p1), err)
	}
	// with status
	stat := discussion.StatusOpen
	p1Open, _ := r.FindByProject(context.Background(), "P-1", discussion.IssueFilter{Status: &stat})
	if len(p1Open) != 2 {
		t.Fatalf("p1 open count: %d", len(p1Open))
	}
	// FindByStatus
	allOpen, err := r.FindByStatus(context.Background(), discussion.StatusOpen, discussion.IssueFilter{Limit: 5})
	if err != nil || len(allOpen) != 3 {
		t.Fatalf("all open: %d err=%v", len(allOpen), err)
	}
	if _, err := r.FindByStatus(context.Background(), "bogus", discussion.IssueFilter{}); err == nil {
		t.Fatal("bogus status should err")
	}
	// FindByOpener
	hOpener, err := r.FindByOpener(context.Background(), "user:h")
	if err != nil || len(hOpener) != 2 {
		t.Fatalf("opener count: %d err=%v", len(hOpener), err)
	}
}

// v2.5.15 (#68): FindAll returns every issue, optionally narrowed by
// status / cursor. Backs the Web Console "All projects" filter.
func TestIssueRepo_FindAll(t *testing.T) {
	db := openDB(t)
	r := NewIssueRepo(db)
	seeds := []struct{ id, proj string }{{"A", "P-1"}, {"B", "P-2"}, {"C", "P-3"}}
	for i, s := range seeds {
		iss, err := discussion.NewIssue(discussion.NewIssueInput{
			ID:                 discussion.IssueID(s.id),
			ProjectID:          s.proj,
			Title:              "t",
			OpenedByIdentityID: "user:h",
			Origin:             discussion.OriginCLI,
			OpenedAt:           time.Date(2026, 5, 21, 12, i, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Save(context.Background(), iss); err != nil {
			t.Fatal(err)
		}
	}
	// No filter → all 3 issues across all projects.
	all, err := r.FindAll(context.Background(), discussion.IssueFilter{})
	if err != nil || len(all) != 3 {
		t.Fatalf("FindAll count: %d err=%v", len(all), err)
	}
	// Status filter applies cross-project (every seed is StatusOpen).
	stat := discussion.StatusOpen
	open, err := r.FindAll(context.Background(), discussion.IssueFilter{Status: &stat})
	if err != nil || len(open) != 3 {
		t.Fatalf("FindAll open count: %d err=%v", len(open), err)
	}
	// Limit caps the result set.
	capped, err := r.FindAll(context.Background(), discussion.IssueFilter{Limit: 2})
	if err != nil || len(capped) != 2 {
		t.Fatalf("FindAll capped count: %d err=%v", len(capped), err)
	}
}

func TestIssueRepo_DescriptionRoundTripWithBlob(t *testing.T) {
	db := openDB(t)
	r := NewIssueRepo(db)
	iss, _ := discussion.NewIssue(discussion.NewIssueInput{
		ID:                 "ISS-BLOB",
		ProjectID:          "P-1",
		Title:              "t",
		Description:        "", // empty inline
		DescriptionBlobRef: "blob://abc/def",
		OpenedByIdentityID: "user:h",
		Origin:             discussion.OriginCLI,
		OpenedAt:           time.Now().UTC(),
	})
	if err := r.Save(context.Background(), iss); err != nil {
		t.Fatal(err)
	}
	got, _ := r.FindByID(context.Background(), iss.ID())
	if got.Description() != "" || got.DescriptionBlobRef() != "blob://abc/def" {
		t.Fatalf("blob roundtrip: %+v", got)
	}
}

func TestIssueRepo_TxCtxHonored(t *testing.T) {
	db := openDB(t)
	r := NewIssueRepo(db)
	i := newIssue(t, "ISS-TX", "P-1", "x")
	err := persistence.RunInTx(context.Background(), db, func(txCtx context.Context) error {
		return r.Save(txCtx, i)
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := r.FindByID(context.Background(), i.ID())
	if got == nil {
		t.Fatal("expected row after commit")
	}
	// Rollback path: tx fn errors
	err = persistence.RunInTx(context.Background(), db, func(txCtx context.Context) error {
		j := newIssue(t, "ISS-TX-RB", "P-1", "x")
		if err := r.Save(txCtx, j); err != nil {
			return err
		}
		return errors.New("forced rollback")
	})
	if err == nil || err.Error() != "forced rollback" {
		t.Fatalf("expected forced rollback err, got %v", err)
	}
	if _, err := r.FindByID(context.Background(), "ISS-TX-RB"); !errors.Is(err, discussion.ErrIssueNotFound) {
		t.Fatalf("expected rolled back row absent: %v", err)
	}
}
