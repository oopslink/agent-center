package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	// File-based temp DB so WAL allows tx + concurrent reads.
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

func newRepo(t *testing.T, db *sql.DB) *EventRepo {
	t.Helper()
	r, err := NewEventRepo(context.Background(), db)
	if err != nil {
		t.Fatalf("NewEventRepo: %v", err)
	}
	return r
}

func newTestEvent(t *testing.T, seq int64, eventType string, refs observability.EventRefs) *observability.Event {
	t.Helper()
	fc := clock.NewFakeClock(time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC))
	g := idgen.NewGenerator(fc)
	e, err := observability.NewEvent(observability.NewEventInput{
		ID:         observability.EventID(g.NewULID()),
		OccurredAt: fc.Now(),
		Seq:        seq,
		EventType:  observability.EventType(eventType),
		Refs:       refs,
		Actor:      "user:hayang",
		Payload:    map[string]any{"k": "v"},
	})
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	return e
}

func TestEventRepo_Append_Happy(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := newRepo(t, db)
	e := newTestEvent(t, repo.NextSeq(), "workforce.worker.enrolled", observability.EventRefs{WorkerID: "W-1"})
	if err := repo.Append(context.Background(), e); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := repo.FindByID(context.Background(), e.ID())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.ID() != e.ID() {
		t.Fatal("id mismatch")
	}
	if got.Type() != e.Type() {
		t.Fatal("type mismatch")
	}
}

func TestEventRepo_Append_DuplicateID(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := newRepo(t, db)
	e := newTestEvent(t, repo.NextSeq(), "x.y", observability.EventRefs{})
	if err := repo.Append(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if err := repo.Append(context.Background(), e); !errors.Is(err, observability.ErrEventAlreadyExists) {
		t.Fatalf("expected ErrEventAlreadyExists, got %v", err)
	}
}

func TestEventRepo_Append_RespectsTxCtx(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := newRepo(t, db)
	e := newTestEvent(t, repo.NextSeq(), "x.y", observability.EventRefs{})
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	txCtx := persistence.WithTx(context.Background(), tx)
	if err := repo.Append(txCtx, e); err != nil {
		t.Fatal(err)
	}
	// Outside the tx, the row must not yet be visible.
	if _, err := repo.FindByID(context.Background(), e.ID()); !errors.Is(err, observability.ErrEventNotFound) {
		t.Fatalf("expected not visible outside tx; got %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	// After rollback the row is gone.
	if _, err := repo.FindByID(context.Background(), e.ID()); !errors.Is(err, observability.ErrEventNotFound) {
		t.Fatalf("expected not found after rollback; got %v", err)
	}
}

func TestEventRepo_Append_NoTxFallsBackToDB(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := newRepo(t, db)
	e := newTestEvent(t, repo.NextSeq(), "x.y", observability.EventRefs{})
	if err := repo.Append(context.Background(), e); err != nil {
		t.Fatalf("Append: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 event committed, got %d", count)
	}
}

func TestEventRepo_FindByID_NotFound(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := newRepo(t, db)
	_, err := repo.FindByID(context.Background(), observability.EventID(idgen.MustNewULID()))
	if !errors.Is(err, observability.ErrEventNotFound) {
		t.Fatalf("expected ErrEventNotFound, got %v", err)
	}
}

func TestEventRepo_Find_FilterByType(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := newRepo(t, db)
	for i := 0; i < 3; i++ {
		e := newTestEvent(t, repo.NextSeq(), "workforce.worker.enrolled", observability.EventRefs{})
		_ = repo.Append(context.Background(), e)
	}
	e := newTestEvent(t, repo.NextSeq(), "project.created", observability.EventRefs{})
	_ = repo.Append(context.Background(), e)
	want := observability.EventType("workforce.worker.enrolled")
	got, err := repo.Find(context.Background(), observability.EventQueryFilter{EventType: &want})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
	for _, e := range got {
		if e.Type() != want {
			t.Fatalf("wrong type %s", e.Type())
		}
	}
}

func TestEventRepo_Find_CursorPagination(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := newRepo(t, db)
	var all []observability.EventID
	for i := 0; i < 5; i++ {
		e := newTestEvent(t, repo.NextSeq(), "x.y", observability.EventRefs{})
		_ = repo.Append(context.Background(), e)
		all = append(all, e.ID())
	}
	// page 1: limit 2
	page1, err := repo.Find(context.Background(), observability.EventQueryFilter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 len: %d", len(page1))
	}
	// page 2: cursor after page1[-1]
	cursor := page1[1].ID()
	page2, err := repo.Find(context.Background(), observability.EventQueryFilter{Cursor: &cursor, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 2 {
		t.Fatalf("page2 len: %d", len(page2))
	}
	for _, e := range page2 {
		for _, prev := range page1 {
			if e.ID() == prev.ID() {
				t.Fatalf("cursor leaked id %s", e.ID())
			}
		}
	}
	_ = all
}

func TestEventRepo_Find_FilterByRefs(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := newRepo(t, db)
	e1 := newTestEvent(t, repo.NextSeq(), "x.y", observability.EventRefs{WorkerID: "W-1"})
	_ = repo.Append(context.Background(), e1)
	e2 := newTestEvent(t, repo.NextSeq(), "x.y", observability.EventRefs{WorkerID: "W-2"})
	_ = repo.Append(context.Background(), e2)
	got, err := repo.Find(context.Background(), observability.EventQueryFilter{
		Refs: observability.EventRefsFilter{WorkerID: "W-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID() != e1.ID() {
		t.Fatalf("expected only W-1 event; got %d", len(got))
	}
}

func TestEventRepo_Find_FilterBySince(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := newRepo(t, db)
	fc := clock.NewFakeClock(time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC))
	g := idgen.NewGenerator(fc)
	mk := func() *observability.Event {
		e, _ := observability.NewEvent(observability.NewEventInput{
			ID:         observability.EventID(g.NewULID()),
			OccurredAt: fc.Now(),
			Seq:        repo.NextSeq(),
			EventType:  "x.y",
			Actor:      "system",
			Payload:    map[string]any{},
		})
		return e
	}
	older := mk()
	_ = repo.Append(context.Background(), older)
	fc.Advance(time.Hour)
	newer := mk()
	_ = repo.Append(context.Background(), newer)

	cutoff := time.Date(2026, 5, 20, 0, 30, 0, 0, time.UTC)
	got, err := repo.Find(context.Background(), observability.EventQueryFilter{Since: &cutoff})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID() != newer.ID() {
		t.Fatalf("since filter wrong: got %d events", len(got))
	}
}

func TestEventRepo_Find_FilterByCorrelation(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := newRepo(t, db)
	fc := clock.NewFakeClock(time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC))
	g := idgen.NewGenerator(fc)
	mk := func(corrID string) *observability.Event {
		e, _ := observability.NewEvent(observability.NewEventInput{
			ID:            observability.EventID(g.NewULID()),
			OccurredAt:    fc.Now(),
			Seq:           repo.NextSeq(),
			EventType:     "x.y",
			Actor:         "system",
			Payload:       map[string]any{},
			CorrelationID: corrID,
		})
		return e
	}
	a := mk("C-1")
	_ = repo.Append(context.Background(), a)
	b := mk("C-2")
	_ = repo.Append(context.Background(), b)
	corr := "C-1"
	got, _ := repo.Find(context.Background(), observability.EventQueryFilter{CorrelationID: &corr})
	if len(got) != 1 || got[0].ID() != a.ID() {
		t.Fatalf("correlation filter wrong: got %d", len(got))
	}
}

func TestEventRepo_NextSeq_RecoversFromExistingRows(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := newRepo(t, db)
	for i := 0; i < 3; i++ {
		e := newTestEvent(t, repo.NextSeq(), "x.y", observability.EventRefs{})
		if err := repo.Append(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	// Re-init repo on the same DB; seq should resume from 3.
	repo2, err := NewEventRepo(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if next := repo2.NextSeq(); next != 4 {
		t.Fatalf("expected next seq 4, got %d", next)
	}
}

func TestEventRepo_NextSeq_Concurrent(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := newRepo(t, db)
	const G = 20
	const perG = 50
	results := make([]int64, 0, G*perG)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < G; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				s := repo.NextSeq()
				mu.Lock()
				results = append(results, s)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	seen := map[int64]struct{}{}
	for _, s := range results {
		if _, dup := seen[s]; dup {
			t.Fatalf("dup seq %d", s)
		}
		seen[s] = struct{}{}
	}
	if len(seen) != G*perG {
		t.Fatalf("expected %d unique seqs, got %d", G*perG, len(seen))
	}
}

func TestEventRepo_NewEventRepo_NilDB(t *testing.T) {
	if _, err := NewEventRepo(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestEventRepo_Append_NilEvent(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := newRepo(t, db)
	if err := repo.Append(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil event")
	}
}

func TestEventRepo_Find_DefaultLimit(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := newRepo(t, db)
	for i := 0; i < 5; i++ {
		_ = repo.Append(context.Background(), newTestEvent(t, repo.NextSeq(), "x.y", observability.EventRefs{}))
	}
	got, err := repo.Find(context.Background(), observability.EventQueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d events", len(got))
	}
}

func TestIsUniqueConstraint(t *testing.T) {
	if !isUniqueConstraint(errors.New("UNIQUE constraint failed: x")) {
		t.Fatal("missed UNIQUE constraint")
	}
	if isUniqueConstraint(errors.New("unrelated")) {
		t.Fatal("false positive")
	}
	if isUniqueConstraint(nil) {
		t.Fatal("nil should be false")
	}
}

func TestEventRepo_RoundTripPreservesCorrelationAndDecision(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	repo := newRepo(t, db)
	fc := clock.NewFakeClock(time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC))
	id := idgen.NewGenerator(fc).NewULID()
	e, _ := observability.NewEvent(observability.NewEventInput{
		ID:            observability.EventID(id),
		OccurredAt:    fc.Now(),
		Seq:           repo.NextSeq(),
		EventType:     "x.y",
		Actor:         "system",
		Payload:       map[string]any{"k": 1},
		CorrelationID: "corr-1",
		DecisionID:    "dec-1",
	})
	if err := repo.Append(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindByID(context.Background(), e.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got.CorrelationID() != "corr-1" {
		t.Fatalf("correlation: %s", got.CorrelationID())
	}
	if got.DecisionID() != "dec-1" {
		t.Fatalf("decision: %s", got.DecisionID())
	}
}

func TestEventRepo_TableNotPresent(t *testing.T) {
	// Open DB without migrations → Append should error.
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo, err := NewEventRepo(context.Background(), db)
	if err == nil {
		t.Fatal("expected error: events table missing")
	}
	if repo != nil {
		t.Fatal("expected nil repo on error")
	}
	if !strings.Contains(err.Error(), "no such table") {
		t.Logf("got: %v", err)
	}
}
