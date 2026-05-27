package sqlite

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

var refTime = time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)

func mkTask(t *testing.T) *task.Task {
	t.Helper()
	tt, err := task.New(task.NewInput{
		ID:        "T-1",
		ProjectID: "P-1",
		Title:     "do thing",
		CreatedBy: "user:hayang",
		Priority:  task.PriorityMedium,
		Now:       refTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	return tt
}

// Covers the Rehydrate branch at task.go:155-158 (EtaAt copy) which only
// runs when EtaAt is non-nil on RehydrateInput. mkTask() leaves EtaAt nil
// so the copy path stays cold without this round-trip.
func TestTaskRepo_RoundtripPreservesEta(t *testing.T) {
	db := openTestDB(t)
	repo := NewTaskRepo(db)
	ctx := context.Background()
	eta := refTime.Add(3 * time.Hour)
	tt, err := task.New(task.NewInput{
		ID:        "T-eta",
		ProjectID: "P-1",
		Title:     "with-eta",
		CreatedBy: "user:hayang",
		Priority:  task.PriorityMedium,
		EtaAt:     &eta,
		Now:       refTime,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := repo.Save(ctx, tt); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := repo.FindByID(ctx, "T-eta")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.EtaAt() == nil || !got.EtaAt().Equal(eta.UTC()) {
		t.Fatalf("eta roundtrip: got %v want %v", got.EtaAt(), eta)
	}
}

func TestTaskRepo_SaveAndFindByID(t *testing.T) {
	db := openTestDB(t)
	repo := NewTaskRepo(db)
	ctx := context.Background()
	tt := mkTask(t)
	if err := repo.Save(ctx, tt); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := repo.FindByID(ctx, "T-1")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.ID() != tt.ID() || got.ProjectID() != tt.ProjectID() {
		t.Fatalf("mismatch: %+v", got)
	}
}

func TestTaskRepo_Save_DuplicateRejected(t *testing.T) {
	db := openTestDB(t)
	repo := NewTaskRepo(db)
	ctx := context.Background()
	tt := mkTask(t)
	if err := repo.Save(ctx, tt); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, tt); !errors.Is(err, task.ErrTaskAlreadyExists) {
		t.Fatalf("expected already_exists: %v", err)
	}
}

func TestTaskRepo_Save_NilGuard(t *testing.T) {
	db := openTestDB(t)
	repo := NewTaskRepo(db)
	if err := repo.Save(context.Background(), nil); err == nil {
		t.Fatal("expected nil error")
	}
	if err := repo.Update(context.Background(), nil); err == nil {
		t.Fatal("expected nil error")
	}
}

func TestTaskRepo_FindByID_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewTaskRepo(db)
	if _, err := repo.FindByID(context.Background(), "T-NOT"); !errors.Is(err, task.ErrTaskNotFound) {
		t.Fatalf("expected not_found: %v", err)
	}
}

func TestTaskRepo_Update_HappyAndCASConflict(t *testing.T) {
	db := openTestDB(t)
	repo := NewTaskRepo(db)
	ctx := context.Background()
	tt := mkTask(t)
	if err := repo.Save(ctx, tt); err != nil {
		t.Fatal(err)
	}
	if err := tt.Suspend(refTime.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(ctx, tt); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := repo.FindByID(ctx, "T-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status() != task.StatusSuspended || got.Version() != 2 {
		t.Fatalf("wrong: %s/%d", got.Status(), got.Version())
	}

	// Stale: tt thinks version=2 still but db has 2; if we suspend twice
	// without saving, version becomes 3 in mem; CAS against 2; OK.
	// Construct a stale copy and update.
	stale, _ := repo.FindByID(ctx, "T-1") // version=2
	// Update through one path:
	_ = tt.Resume(refTime.Add(time.Second))
	if err := repo.Update(ctx, tt); err != nil {
		t.Fatal(err)
	}
	// stale.Resume would not be valid (suspended→open expected; but stale is suspended)
	_ = stale.Resume(refTime.Add(time.Second))
	err = repo.Update(ctx, stale)
	if !errors.Is(err, task.ErrTaskVersionConflict) {
		t.Fatalf("expected version conflict: %v", err)
	}
}

func TestTaskRepo_Update_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewTaskRepo(db)
	tt := mkTask(t)
	// version=1 in-mem; not yet saved.
	_ = tt.Suspend(refTime.Add(time.Second))
	if err := repo.Update(context.Background(), tt); !errors.Is(err, task.ErrTaskNotFound) {
		t.Fatalf("expected not_found: %v", err)
	}
}

func TestTaskRepo_FindByProject_FilterStatus(t *testing.T) {
	db := openTestDB(t)
	repo := NewTaskRepo(db)
	ctx := context.Background()
	for i, status := range []task.Status{task.StatusOpen, task.StatusOpen, task.StatusDone} {
		tt, err := task.New(task.NewInput{
			ID:        taskruntime.TaskID("T-" + string(rune('1'+i))),
			ProjectID: "P-1",
			Title:     "x",
			CreatedBy: "u",
			Now:       refTime,
		})
		if err != nil {
			t.Fatal(err)
		}
		if status == task.StatusDone {
			_ = tt.MarkDone(refTime)
		}
		if err := repo.Save(ctx, tt); err != nil {
			t.Fatal(err)
		}
	}
	all, err := repo.FindByProject(ctx, "P-1", task.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("len: %d", len(all))
	}
	open := task.StatusOpen
	got, err := repo.FindByProject(ctx, "P-1", task.Filter{Status: &open})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len: %d", len(got))
	}
	got, err = repo.FindByStatus(ctx, task.StatusDone, task.Filter{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len: %d", len(got))
	}
	if _, err := repo.FindByStatus(ctx, "garbage", task.Filter{}); !errors.Is(err, task.ErrInvalidStatus) {
		t.Fatalf("expected invalid status")
	}
}

// v2.5.15 (#70): FindAll returns every task, optionally narrowed by
// status / limit. Backs the Web Console "All projects" filter.
func TestTaskRepo_FindAll(t *testing.T) {
	db := openTestDB(t)
	repo := NewTaskRepo(db)
	ctx := context.Background()
	seeds := []struct {
		id, proj string
		status   task.Status
	}{
		{"T-1", "P-1", task.StatusOpen},
		{"T-2", "P-2", task.StatusOpen},
		{"T-3", "P-3", task.StatusDone},
	}
	for _, s := range seeds {
		tt, err := task.New(task.NewInput{
			ID: taskruntime.TaskID(s.id), ProjectID: s.proj, Title: "x",
			CreatedBy: "u", Now: refTime,
		})
		if err != nil {
			t.Fatal(err)
		}
		if s.status == task.StatusDone {
			_ = tt.MarkDone(refTime)
		}
		if err := repo.Save(ctx, tt); err != nil {
			t.Fatal(err)
		}
	}
	all, err := repo.FindAll(ctx, task.Filter{})
	if err != nil || len(all) != 3 {
		t.Fatalf("FindAll count: %d err=%v", len(all), err)
	}
	open := task.StatusOpen
	got, err := repo.FindAll(ctx, task.Filter{Status: &open})
	if err != nil || len(got) != 2 {
		t.Fatalf("FindAll open count: %d err=%v", len(got), err)
	}
	capped, err := repo.FindAll(ctx, task.Filter{Limit: 1})
	if err != nil || len(capped) != 1 {
		t.Fatalf("FindAll capped: %d err=%v", len(capped), err)
	}
}

func TestTaskRepo_FindBlockedBy(t *testing.T) {
	db := openTestDB(t)
	repo := NewTaskRepo(db)
	ctx := context.Background()
	t1, _ := task.New(task.NewInput{ID: "T-A", ProjectID: "P-1", Title: "a", CreatedBy: "u", Now: refTime})
	if err := repo.Save(ctx, t1); err != nil {
		t.Fatal(err)
	}
	t2, _ := task.New(task.NewInput{
		ID: "T-B", ProjectID: "P-1", Title: "b", CreatedBy: "u", Now: refTime,
		DependsOnTaskIDs: []taskruntime.TaskID{"T-A"},
	})
	if err := repo.Save(ctx, t2); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindBlockedBy(ctx, "T-A")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID() != "T-B" {
		t.Fatalf("blocked-by: %+v", got)
	}
	got, err = repo.FindBlockedBy(ctx, "T-NONEXIST")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0, got %d", len(got))
	}
}

// Concurrent CAS race.
func TestTaskRepo_Update_CASRace(t *testing.T) {
	db := openTestDB(t)
	repo := NewTaskRepo(db)
	ctx := context.Background()
	tt := mkTask(t)
	if err := repo.Save(ctx, tt); err != nil {
		t.Fatal(err)
	}
	a, _ := repo.FindByID(ctx, "T-1")
	b, _ := repo.FindByID(ctx, "T-1")
	_ = a.Suspend(refTime.Add(time.Second))
	_ = b.Suspend(refTime.Add(time.Second))
	var wg sync.WaitGroup
	wg.Add(2)
	results := make([]error, 2)
	go func() { defer wg.Done(); results[0] = repo.Update(ctx, a) }()
	go func() { defer wg.Done(); results[1] = repo.Update(ctx, b) }()
	wg.Wait()
	var won, lost int
	for _, e := range results {
		if e == nil {
			won++
		} else if errors.Is(e, task.ErrTaskVersionConflict) {
			lost++
		} else {
			t.Fatalf("unexpected: %v", e)
		}
	}
	if won != 1 || lost != 1 {
		t.Fatalf("expected 1 won 1 lost; got %d/%d", won, lost)
	}
}
